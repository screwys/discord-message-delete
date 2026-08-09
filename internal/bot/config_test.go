package bot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAddRegexRulePreservesConfigAndAppendsRule(t *testing.T) {
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
	if len(config.MessageRegexes) != 2 {
		t.Fatalf("len(MessageRegexes) = %d, want 2", len(config.MessageRegexes))
	}
	addedRule := config.MessageRegexes[1]
	if addedRule.Name != "example" || addedRule.Pattern != "example" {
		t.Fatalf("added rule = %+v, want name and pattern example", addedRule)
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
	if len(config.MessageRegexes) != 3 {
		t.Fatalf("len(MessageRegexes) = %d, want 3", len(config.MessageRegexes))
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

func TestAddRegexRuleNormalizesExistingDuplicates(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "",
  "ignored_user_ids": [],
  "message_regexes": [
    {"name": "first name", "pattern": "same"},
    {"name": "duplicate name", "pattern": "same"},
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
	if len(config.MessageRegexes) != 2 {
		t.Fatalf("len(MessageRegexes) = %d, want 2", len(config.MessageRegexes))
	}
	if config.MessageRegexes[0].Name != "first name" {
		t.Fatalf("first rule name = %q, want first name", config.MessageRegexes[0].Name)
	}
}

func TestRemoveRegexRuleRemovesEveryMatchAndDeduplicatesRemainingRules(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "",
  "ignored_user_ids": [],
  "message_regexes": [
    {"name": "remove", "pattern": "remove"},
    {"name": "keep", "pattern": "keep"},
    {"name": "remove again", "pattern": "remove"},
    {"name": "keep again", "pattern": "keep"}
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
	if len(config.MessageRegexes) != 1 || config.MessageRegexes[0].Pattern != "keep" {
		t.Fatalf("MessageRegexes = %+v, want one keep rule", config.MessageRegexes)
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
