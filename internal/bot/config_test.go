package bot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddRegexRulePreservesConfigAndAddsToBlockedWords(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "target-user",
  "ignored_user_ids": ["ignored-user"],
  "message_regexes": [{"name": "existing", "pattern": "existing"}]
}
`)

	added, err := AddRegexRule(path, "example")
	if err != nil {
		t.Fatalf("AddRegexRule: %v", err)
	}
	if !added {
		t.Fatal("added = false, want true")
	}

	config, err := readConfig(path)
	if err != nil {
		t.Fatalf("readConfig: %v", err)
	}
	if config.SpoilerImageUserID != "target-user" {
		t.Fatalf("SpoilerImageUserID = %q, want target-user", config.SpoilerImageUserID)
	}
	if len(config.IgnoredUserIDs) != 1 || config.IgnoredUserIDs[0] != "ignored-user" {
		t.Fatalf("IgnoredUserIDs = %q, want [ignored-user]", config.IgnoredUserIDs)
	}
	if len(config.MessageRegexes) != 1 {
		t.Fatalf("len(MessageRegexes) = %d, want 1", len(config.MessageRegexes))
	}
	blockedWords := config.MessageRegexes[0]
	if blockedWords.Name != "blocked words" || blockedWords.Pattern != `(?i)\b(?:existing|example)\b` {
		t.Fatalf("blocked words rule = %+v", blockedWords)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 600", info.Mode().Perm())
	}
}

func TestAddRegexRuleDeduplicatesRepeatedAdds(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "",
  "ignored_user_ids": [],
  "message_regexes": []
}
`)

	for index, pattern := range []string{"first", "second", "first", "third"} {
		added, err := AddRegexRule(path, pattern)
		if err != nil {
			t.Fatalf("AddRegexRule(%q): %v", pattern, err)
		}
		if added != (index != 2) {
			t.Fatalf("AddRegexRule(%q) added = %t at index %d", pattern, added, index)
		}
	}

	config, err := readConfig(path)
	if err != nil {
		t.Fatalf("readConfig after repeated adds: %v", err)
	}
	if len(config.MessageRegexes) != 1 {
		t.Fatalf("len(MessageRegexes) = %d, want 1", len(config.MessageRegexes))
	}
	if config.MessageRegexes[0].Pattern != `(?i)\b(?:first|second|third)\b` {
		t.Fatalf("blocked words pattern = %q", config.MessageRegexes[0].Pattern)
	}
	if _, err := CompileConfig(config); err != nil {
		t.Fatalf("CompileConfig after repeated adds: %v", err)
	}
}

func TestAddRegexRuleRejectsInvalidPatternWithoutChangingConfig(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "",
  "ignored_user_ids": [],
  "message_regexes": []
}
`)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile: %v", err)
	}

	if _, err := AddRegexRule(path, "["); err == nil {
		t.Fatal("AddRegexRule returned nil error for invalid regex")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile: %v", err)
	}
	if string(after) != string(original) {
		t.Fatal("invalid rule changed config")
	}
}

func TestAddRegexRuleNormalizesExistingWordsIntoOneRule(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "",
  "ignored_user_ids": [],
  "message_regexes": [
    {"name": "same", "pattern": "same"},
    {"name": "same", "pattern": "same"},
    {"name": "other", "pattern": "other"}
  ]
}
`)

	added, err := AddRegexRule(path, "same")
	if err != nil {
		t.Fatalf("AddRegexRule: %v", err)
	}
	if added {
		t.Fatal("added = true, want false")
	}
	config, err := readConfig(path)
	if err != nil {
		t.Fatalf("readConfig: %v", err)
	}
	if len(config.MessageRegexes) != 1 {
		t.Fatalf("len(MessageRegexes) = %d, want 1", len(config.MessageRegexes))
	}
	if config.MessageRegexes[0].Pattern != `(?i)\b(?:same|other)\b` {
		t.Fatalf("blocked words pattern = %q", config.MessageRegexes[0].Pattern)
	}
}

func TestRemoveRegexRuleRemovesEveryMatchAndDeduplicatesRemainingRules(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "",
  "ignored_user_ids": [],
  "message_regexes": [
    {"name": "remove", "pattern": "remove"},
    {"name": "keep", "pattern": "keep"},
    {"name": "remove", "pattern": "remove"},
    {"name": "keep", "pattern": "keep"}
  ]
}
`)

	removed, err := RemoveRegexRule(path, "remove")
	if err != nil {
		t.Fatalf("RemoveRegexRule: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
	config, err := readConfig(path)
	if err != nil {
		t.Fatalf("readConfig: %v", err)
	}
	if len(config.MessageRegexes) != 1 || config.MessageRegexes[0].Pattern != `(?i)\b(?:keep)\b` {
		t.Fatalf("MessageRegexes = %+v, want one blocked words rule", config.MessageRegexes)
	}
}

func TestNormalizeConfigCollapsesConfusableWordsAndPreservesNamedRegexes(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "",
  "ignored_user_ids": [],
  "message_regexes": [
    {"name": "blocked words", "pattern": "(?i)\\b(?:marble)\\b"},
    {"name": "m@rble", "pattern": "m@rble"},
    {"name": "m4rble", "pattern": "m4rble"},
    {"name": "marb1e", "pattern": "marb1e"},
    {"name": "invite links", "pattern": "discord\\.example/[a-z0-9]+"}
  ]
}
`)

	changed, err := NormalizeConfig(path)
	if err != nil {
		t.Fatalf("NormalizeConfig: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	config, err := readConfig(path)
	if err != nil {
		t.Fatalf("readConfig: %v", err)
	}
	want := []RegexRuleConfig{
		{Name: "blocked words", Pattern: `(?i)\b(?:marble)\b`},
		{Name: "invite links", Pattern: `discord\.example/[a-z0-9]+`},
	}
	if len(config.MessageRegexes) != len(want) {
		t.Fatalf("MessageRegexes = %+v, want %+v", config.MessageRegexes, want)
	}
	for index := range want {
		if config.MessageRegexes[index] != want[index] {
			t.Fatalf("MessageRegexes[%d] = %+v, want %+v", index, config.MessageRegexes[index], want[index])
		}
	}
	changed, err = NormalizeConfig(path)
	if err != nil {
		t.Fatalf("NormalizeConfig second pass: %v", err)
	}
	if changed {
		t.Fatal("second normalization changed an already canonical config")
	}
}

func TestNormalizeConfigParsesGroupedWordsAndCharacterClasses(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "",
  "ignored_user_ids": [],
  "message_regexes": [
    {"name": "blocked words", "pattern": "(?i)\\b(m[a@4]rble|cobalt)\\b"}
  ]
}
`)

	changed, err := NormalizeConfig(path)
	if err != nil {
		t.Fatalf("NormalizeConfig: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	config, err := readConfig(path)
	if err != nil {
		t.Fatalf("readConfig: %v", err)
	}
	want := `(?i)\b(?:marble|cobalt)\b`
	if len(config.MessageRegexes) != 1 || config.MessageRegexes[0].Pattern != want {
		t.Fatalf("MessageRegexes = %+v, want pattern %q", config.MessageRegexes, want)
	}
}

func TestNormalizeConfigPreservesCustomBlockedWordsRegex(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "",
  "ignored_user_ids": [],
  "message_regexes": [
    {"name": "blocked words", "pattern": "prefix.+suffix"}
  ]
}
`)

	changed, err := NormalizeConfig(path)
	if err != nil {
		t.Fatalf("NormalizeConfig: %v", err)
	}
	if changed {
		t.Fatal("custom regex was changed")
	}
}

func TestNormalizeConfigKeepsOneCustomBlockedWordsRuleWithoutSemanticDuplicates(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "",
  "ignored_user_ids": [],
  "message_regexes": [
    {"name": "blocked words", "pattern": "(?i)\\bmarble(?:s)?\\b"},
    {"name": "m@rble", "pattern": "m@rble"},
    {"name": "cobalt", "pattern": "cobalt"}
  ]
}
`)

	changed, err := NormalizeConfig(path)
	if err != nil {
		t.Fatalf("NormalizeConfig: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	config, err := readConfig(path)
	if err != nil {
		t.Fatalf("readConfig: %v", err)
	}
	want := `(?:(?i)\bmarble(?:s)?\b)|(?:\b(?:cobalt)\b)`
	if len(config.MessageRegexes) != 1 || config.MessageRegexes[0].Name != blockedWordsRuleName || config.MessageRegexes[0].Pattern != want {
		t.Fatalf("MessageRegexes = %+v, want one pattern %q", config.MessageRegexes, want)
	}
}

func TestRemoveRegexRuleLeavesConfigUnchangedWhenRuleIsMissing(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "",
  "ignored_user_ids": [],
  "message_regexes": [{"name": "keep", "pattern": "keep"}]
}
`)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile: %v", err)
	}

	if _, err := RemoveRegexRule(path, "missing"); err == nil {
		t.Fatal("RemoveRegexRule returned nil error for a missing rule")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile: %v", err)
	}
	if string(after) != string(original) {
		t.Fatal("missing removal changed config")
	}
}

func TestRemoveRegexRuleDeletesLiteralFromCombinedBlockedWordsPattern(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "",
  "ignored_user_ids": [],
  "message_regexes": [
    {"name": "blocked words", "pattern": "(?:(?i)\\b(?:marble|marbles)\\b)|(?:\\b(?:cobalt)\\b)"}
  ]
}
`)

	removed, err := RemoveRegexRule(path, "marbles")
	if err != nil {
		t.Fatalf("RemoveRegexRule: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	config, err := readConfig(path)
	if err != nil {
		t.Fatalf("readConfig: %v", err)
	}
	if len(config.MessageRegexes) != 1 || config.MessageRegexes[0].Name != blockedWordsRuleName {
		t.Fatalf("MessageRegexes = %+v, want one blocked words rule", config.MessageRegexes)
	}
	compiled, err := CompileConfig(config)
	if err != nil {
		t.Fatalf("CompileConfig: %v", err)
	}
	cleaner := NewCleaner(compiled)
	if cleaner.Decide(Message{Content: "marbles"}).Delete {
		t.Fatal("deleted word still matches")
	}
	for _, content := range []string{"marble", "cobalt"} {
		if !cleaner.Decide(Message{Content: content}).Delete {
			t.Fatalf("remaining word %q no longer matches", content)
		}
	}
}

func TestAddEmojiRuleResolvesShortcodeAndDeduplicatesAliases(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "",
  "ignored_user_ids": [],
  "message_regexes": [],
  "emoji_rules": []
}
`)

	added, err := AddEmojiRule(path, ":thumbsup:")
	if err != nil {
		t.Fatalf("AddEmojiRule shortcode: %v", err)
	}
	if !added {
		t.Fatal("added = false, want true")
	}
	added, err = AddEmojiRule(path, ":+1:")
	if err != nil {
		t.Fatalf("AddEmojiRule alias: %v", err)
	}
	if added {
		t.Fatal("alias added = true, want false")
	}

	config, err := readConfig(path)
	if err != nil {
		t.Fatalf("readConfig: %v", err)
	}
	if len(config.EmojiRules) != 1 || config.EmojiRules[0] != "👍" {
		t.Fatalf("EmojiRules = %q, want [👍]", config.EmojiRules)
	}
}

func TestEmojiRuleAcceptsCustomEmojiAndDeletesBySameIdentity(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "",
  "ignored_user_ids": [],
  "message_regexes": [],
  "emoji_rules": []
}
`)

	added, err := AddEmojiRule(path, "<a:party:123456789012345678>")
	if err != nil {
		t.Fatalf("AddEmojiRule: %v", err)
	}
	if !added {
		t.Fatal("added = false, want true")
	}
	added, err = AddEmojiRule(path, "<:renamed:123456789012345678>")
	if err != nil {
		t.Fatalf("AddEmojiRule renamed: %v", err)
	}
	if added {
		t.Fatal("renamed custom emoji added = true, want false")
	}
	removed, err := RemoveEmojiRule(path, "<:renamed:123456789012345678>")
	if err != nil {
		t.Fatalf("RemoveEmojiRule: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
}

func TestAddEmojiRuleRejectsUnknownShortcodeWithoutChangingConfig(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "",
  "ignored_user_ids": [],
  "message_regexes": []
}
`)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile: %v", err)
	}

	if _, err := AddEmojiRule(path, ":not_a_known_emoji:"); err == nil {
		t.Fatal("AddEmojiRule returned nil error for unknown shortcode")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile after AddEmojiRule: %v", err)
	}
	if string(after) != string(original) {
		t.Fatal("invalid emoji rule changed config")
	}
}

func writeTestConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}
	return path
}
