package bot

import "testing"

func TestDeletesSpoileredImagesOnlyForConfiguredUser(t *testing.T) {
	cleaner := newTestCleaner(t, Config{SpoilerImageUserID: "target-user"})

	tests := []struct {
		name    string
		message Message
		delete  bool
	}{
		{
			name: "target user spoilered image",
			message: Message{
				AuthorID:    "target-user",
				Attachments: []Attachment{{Filename: "SPOILER_photo.png", ContentType: "image/png"}},
			},
			delete: true,
		},
		{
			name: "target user spoilered image detected by extension",
			message: Message{
				AuthorID:    "target-user",
				Attachments: []Attachment{{Filename: "SPOILER_photo.webp"}},
			},
			delete: true,
		},
		{
			name: "target user image marked with Discord spoiler flag",
			message: Message{
				AuthorID:    "target-user",
				Attachments: []Attachment{{Filename: "photo.png", ContentType: "image/png", Spoiler: true}},
			},
			delete: true,
		},
		{
			name: "target user flagged visual attachment without media metadata",
			message: Message{
				AuthorID:    "target-user",
				Attachments: []Attachment{{Filename: "attachment", Width: 1280, Height: 720, Spoiler: true}},
			},
			delete: true,
		},
		{
			name: "target user spoilered visual component",
			message: Message{
				AuthorID:             "target-user",
				SpoileredVisualMedia: true,
			},
			delete: true,
		},
		{
			name: "different user spoilered image",
			message: Message{
				AuthorID:    "other-user",
				Attachments: []Attachment{{Filename: "SPOILER_photo.png", ContentType: "image/png"}},
			},
			delete: false,
		},
		{
			name: "target user normal image",
			message: Message{
				AuthorID:    "target-user",
				Attachments: []Attachment{{Filename: "photo.png", ContentType: "image/png"}},
			},
			delete: false,
		},
		{
			name: "target user spoilered non-image",
			message: Message{
				AuthorID:    "target-user",
				Attachments: []Attachment{{Filename: "SPOILER_notes.txt", ContentType: "text/plain"}},
			},
			delete: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := cleaner.Decide(test.message)
			if decision.Delete != test.delete {
				t.Fatalf("Delete = %v, want %v", decision.Delete, test.delete)
			}
		})
	}
}

func TestMessageRegexesDeleteMatchingContent(t *testing.T) {
	cleaner := newTestCleaner(t, Config{
		MessageRegexes: []RegexRuleConfig{{Name: "blocked phrase", Pattern: `(?i)\bblocked phrase\b`}},
	})

	tests := []struct {
		name    string
		content string
		delete  bool
	}{
		{name: "case insensitive full phrase", content: "This has a BLOCKED PHRASE in it.", delete: true},
		{name: "partial word does not match", content: "blocked phrases are not the exact phrase", delete: false},
		{name: "unrelated message", content: "ordinary chat message", delete: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := cleaner.Decide(Message{AuthorID: "member", Content: test.content})
			if decision.Delete != test.delete {
				t.Fatalf("Delete = %v, want %v", decision.Delete, test.delete)
			}
		})
	}
}

func TestMessageRegexesMatchUnicodeConfusables(t *testing.T) {
	cleaner := newTestCleaner(t, Config{
		MessageRegexes: []RegexRuleConfig{{Name: "blocked term", Pattern: `(?i)\bsyntax\b`}},
	})

	decision := cleaner.Decide(Message{AuthorID: "member", Content: "ѕуntах"})
	if !decision.Delete {
		t.Fatal("Delete = false, want true")
	}
}

func TestMessageRegexesIgnoreInvisibleFormatting(t *testing.T) {
	cleaner := newTestCleaner(t, Config{
		MessageRegexes: []RegexRuleConfig{{Name: "blocked term", Pattern: `(?i)\bexample\b`}},
	})

	decision := cleaner.Decide(Message{AuthorID: "member", Content: "ex\u200bample"})
	if !decision.Delete {
		t.Fatal("Delete = false, want true")
	}
}

func TestMessageRegexesDeleteMatchingEmbedMetadata(t *testing.T) {
	cleaner := newTestCleaner(t, Config{
		MessageRegexes: []RegexRuleConfig{{Name: "blocked social username", Pattern: `(?i)\bword_i_want_to_block\b`}},
	})

	decision := cleaner.Decide(Message{
		AuthorID: "member",
		Content:  "https://example.test/post/123",
		Embeds: []Embed{{
			AuthorName: "@word_i_want_to_block",
		}},
	})
	if !decision.Delete {
		t.Fatalf("Delete = false, want true")
	}
}

func TestIgnoredUserOverridesAllRules(t *testing.T) {
	cleaner := newTestCleaner(t, Config{
		SpoilerImageUserID: "ignored-user",
		IgnoredUserIDs:     []string{"ignored-user"},
		MessageRegexes:     []RegexRuleConfig{{Name: "anything", Pattern: `.+`}},
	})

	decision := cleaner.Decide(Message{
		AuthorID:    "ignored-user",
		Content:     "matches any non-empty message",
		Attachments: []Attachment{{Filename: "SPOILER_photo.png", ContentType: "image/png"}},
	})
	if decision.Delete {
		t.Fatalf("Delete = true, want false")
	}
}

func TestCompileConfigRejectsInvalidRegex(t *testing.T) {
	_, err := CompileConfig(Config{
		MessageRegexes: []RegexRuleConfig{{Name: "bad pattern", Pattern: `[`}},
	})
	if err == nil {
		t.Fatal("CompileConfig returned nil error for invalid regex")
	}
}

func newTestCleaner(t *testing.T, config Config) *Cleaner {
	t.Helper()
	compiled, err := CompileConfig(config)
	if err != nil {
		t.Fatalf("CompileConfig: %v", err)
	}
	return NewCleaner(compiled)
}
