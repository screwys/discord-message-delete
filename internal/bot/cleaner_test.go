package bot

import (
	"strings"
	"testing"
)

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
		MessageRegexes: []RegexRuleConfig{{Name: "blocked phrase", Pattern: `\bblocked phrase\b`}},
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

func TestMessageRegexesAreCaseInsensitiveByDefault(t *testing.T) {
	cleaner := newTestCleaner(t, Config{
		MessageRegexes: []RegexRuleConfig{{Name: "blocked word", Pattern: `Word`}},
	})

	decision := cleaner.Decide(Message{AuthorID: "member", Content: "word"})
	if !decision.Delete {
		t.Fatal("Delete = false, want true")
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
	check := decision.MessageCheck
	if check == nil || check.OriginalMatched || !check.NormalizationChanged || !check.NormalizedMatched {
		t.Fatalf("MessageCheck = %+v, want normalized-only match", check)
	}
}

func TestMessageRegexesMatchCommonASCIIConfusables(t *testing.T) {
	cleaner := newTestCleaner(t, Config{
		MessageRegexes: []RegexRuleConfig{{Name: "blocked term", Pattern: `\bmarble\b`}},
	})

	for _, content := range []string{"m@rble", "m4rble", "marb1e"} {
		decision := cleaner.Decide(Message{AuthorID: "member", Content: content})
		if !decision.Delete {
			t.Fatalf("Delete = false for neutral confusable fixture %q", content)
		}
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
		EmojiRules:         []string{":thumbsup:"},
	})

	decision := cleaner.Decide(Message{
		AuthorID:    "ignored-user",
		Content:     "matches any non-empty message 👍",
		Attachments: []Attachment{{Filename: "SPOILER_photo.png", ContentType: "image/png"}},
	})
	if decision.Delete {
		t.Fatalf("Delete = true, want false")
	}
	if decision.MessageCheck == nil || !decision.MessageCheck.Ignored {
		t.Fatalf("MessageCheck = %+v, want ignored check", decision.MessageCheck)
	}
}

func TestMessageCheckExplainsRegexDecision(t *testing.T) {
	cleaner := newTestCleaner(t, Config{
		MessageRegexes: []RegexRuleConfig{{Name: "blocked term", Pattern: `ExampleTerm`}},
	})

	decision := cleaner.Decide(Message{AuthorID: "member", Content: "exampleterm"})
	if !decision.Delete {
		t.Fatal("Delete = false, want true")
	}
	check := decision.MessageCheck
	if check == nil {
		t.Fatal("MessageCheck = nil, want check details")
	}
	if check.Ignored || check.RegexRules != 1 || !check.SearchableText || !check.RegexEvaluated || !check.RegexMatched {
		t.Fatalf("MessageCheck = %+v, want evaluated matching rule", *check)
	}
}

func TestCompileConfigRejectsInvalidRegex(t *testing.T) {
	_, err := CompileConfig(Config{
		MessageRegexes: []RegexRuleConfig{{Name: "bad pattern", Pattern: `private-term[`}},
	})
	if err == nil {
		t.Fatal("CompileConfig returned nil error for invalid regex")
	}
	if strings.Contains(err.Error(), "private-term") {
		t.Fatalf("CompileConfig error exposed the regex pattern: %q", err)
	}
}

func TestSpoilerCheckExplainsDecision(t *testing.T) {
	cleaner := newTestCleaner(t, Config{SpoilerImageUserID: "target-user"})
	decision := cleaner.Decide(Message{
		AuthorID: "target-user",
		Attachments: []Attachment{
			{Filename: "image.png", ContentType: "image/png", Spoiler: true},
			{Filename: "SPOILER_notes.txt", ContentType: "text/plain"},
			{Filename: "ordinary.webp", ContentType: "image/webp"},
		},
	})

	if !decision.Delete {
		t.Fatal("Delete = false, want true")
	}
	check := decision.SpoilerCheck
	if check == nil {
		t.Fatal("SpoilerCheck = nil, want check details")
	}
	if check.Attachments != 3 ||
		check.FlaggedAttachments != 1 ||
		check.LegacyMarkers != 1 ||
		check.ImageAttachments != 2 ||
		check.MatchingAttachments != 1 {
		t.Fatalf("SpoilerCheck = %+v, want attachment counts 3/1/1/2/1", *check)
	}
}

func TestEmojiRuleDeletesMessageContainingConfiguredEmoji(t *testing.T) {
	cleaner := newTestCleaner(t, Config{EmojiRules: []string{":thumbsup:"}})

	decision := cleaner.Decide(Message{AuthorID: "member", Content: "approved 👍"})
	if !decision.Delete || decision.Kind != DecisionEmoji {
		t.Fatalf("decision = %+v, want emoji deletion", decision)
	}
	if decision.EmojiCheck == nil || !decision.EmojiCheck.Matched || decision.EmojiCheck.RulesLoaded != 1 {
		t.Fatalf("EmojiCheck = %+v, want one matching rule", decision.EmojiCheck)
	}
}

func TestEmojiRuleIgnoresEmbedMetadata(t *testing.T) {
	cleaner := newTestCleaner(t, Config{EmojiRules: []string{":thumbsup:"}})

	decision := cleaner.Decide(Message{
		AuthorID: "member",
		Content:  "https://example.invalid/item",
		Embeds: []Embed{{
			Description: "decorative preview 👍",
		}},
	})
	if decision.Delete {
		t.Fatalf("Delete = true, want false")
	}
	if decision.EmojiCheck == nil || decision.EmojiCheck.Matched {
		t.Fatalf("EmojiCheck = %+v, want an unmatched content-only check", decision.EmojiCheck)
	}
}

func TestEmojiRuleDoesNotMatchDifferentEmoji(t *testing.T) {
	cleaner := newTestCleaner(t, Config{EmojiRules: []string{":thumbsup:"}})

	decision := cleaner.Decide(Message{AuthorID: "member", Content: "not this one 👎"})
	if decision.Delete {
		t.Fatalf("Delete = true, want false")
	}
}

func TestEmojiRuleDoesNotMatchDifferentSkinTone(t *testing.T) {
	cleaner := newTestCleaner(t, Config{EmojiRules: []string{":thumbsup:"}})

	decision := cleaner.Decide(Message{AuthorID: "member", Content: "different emoji 👍🏽"})
	if decision.Delete {
		t.Fatalf("Delete = true, want false")
	}
}

func TestEmojiRuleMatchesConfiguredSkinTone(t *testing.T) {
	cleaner := newTestCleaner(t, Config{EmojiRules: []string{":thumbsup_tone3:"}})

	decision := cleaner.Decide(Message{AuthorID: "member", Content: "matching emoji 👍🏽"})
	if !decision.Delete || decision.Kind != DecisionEmoji {
		t.Fatalf("decision = %+v, want emoji deletion", decision)
	}
}

func TestCustomEmojiRuleMatchesMessageByEmojiID(t *testing.T) {
	cleaner := newTestCleaner(t, Config{EmojiRules: []string{"<:party:123456789012345678>"}})

	decision := cleaner.Decide(Message{
		AuthorID: "member",
		Content:  "<a:renamed:123456789012345678>",
	})
	if !decision.Delete || decision.Kind != DecisionEmoji {
		t.Fatalf("decision = %+v, want custom emoji deletion", decision)
	}
}

func TestCustomEmojiNameRuleMatchesMessageAndReaction(t *testing.T) {
	cleaner := newTestCleaner(t, Config{EmojiRules: []string{":team_badge:"}})

	message := cleaner.Decide(Message{
		AuthorID: "member",
		Content:  "<a:team_badge:123456789012345678>",
	})
	if !message.Delete || message.Kind != DecisionEmoji {
		t.Fatalf("message decision = %+v, want custom emoji deletion", message)
	}
	reaction := cleaner.DecideReaction(ReactionEmoji{
		ID:   "123456789012345678",
		Name: "team_badge",
	})
	if !reaction.Remove {
		t.Fatalf("reaction decision = %+v, want removal", reaction)
	}
}

func TestEmojiRuleMatchesNewReactionsWithoutMessageHistory(t *testing.T) {
	cleaner := newTestCleaner(t, Config{
		EmojiRules: []string{":thumbsup:", "<:party:123456789012345678>"},
	})

	standard := cleaner.DecideReaction(ReactionEmoji{Name: "👍"})
	if !standard.Remove || standard.RulesLoaded != 2 {
		t.Fatalf("standard decision = %+v, want removal with two loaded rules", standard)
	}
	custom := cleaner.DecideReaction(ReactionEmoji{ID: "123456789012345678", Name: "renamed"})
	if !custom.Remove {
		t.Fatalf("custom decision = %+v, want removal", custom)
	}
	other := cleaner.DecideReaction(ReactionEmoji{Name: "👎"})
	if other.Remove {
		t.Fatalf("other decision = %+v, want no removal", other)
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
