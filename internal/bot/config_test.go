package bot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendRegexRulePreservesConfigAndAppendsRule(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "target-user",
  "ignored_user_ids": ["ignored-user"],
  "message_regexes": [{"name": "existing", "pattern": "existing"}]
}
`)

	if err := AppendRegexRule(path, "example"); err != nil {
		t.Fatalf("AppendRegexRule: %v", err)
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
	added := config.MessageRegexes[1]
	if added.Name != "example" || added.Pattern != "example" {
		t.Fatalf("added rule = %+v, want name and pattern example", added)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 600", info.Mode().Perm())
	}
}

func TestAppendRegexRuleKeepsRepeatedAddsReloadable(t *testing.T) {
	path := writeTestConfig(t, `{
  "spoiler_image_user_id": "",
  "ignored_user_ids": [],
  "message_regexes": []
}
`)

	for _, pattern := range []string{"first", "second", "third"} {
		if err := AppendRegexRule(path, pattern); err != nil {
			t.Fatalf("AppendRegexRule(%q): %v", pattern, err)
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

func TestAppendRegexRuleRejectsInvalidPatternWithoutChangingConfig(t *testing.T) {
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

	if err := AppendRegexRule(path, "["); err == nil {
		t.Fatal("AppendRegexRule returned nil error for invalid regex")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile: %v", err)
	}
	if string(after) != string(original) {
		t.Fatal("invalid rule changed config")
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
