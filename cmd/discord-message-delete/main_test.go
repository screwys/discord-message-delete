package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"discord-message-delete/internal/bot"
)

func TestAttachmentsFromDiscordPreservesSpoilerFlag(t *testing.T) {
	attachments := attachmentsFromDiscord([]*discordgo.MessageAttachment{{
		Filename:    "photo.png",
		ContentType: "image/png",
		Flags:       1 << 3,
	}})

	if len(attachments) != 1 {
		t.Fatalf("len(attachments) = %d, want 1", len(attachments))
	}
	if !attachments[0].Spoiler {
		t.Fatal("Spoiler = false, want true")
	}
}

func TestReadyLogContainsOnlyGuildCount(t *testing.T) {
	var output bytes.Buffer
	logger := log.New(&output, "", 0)
	logReady(logger, &discordgo.Ready{Guilds: []*discordgo.Guild{{}, {}}})

	want := "connected guilds=2\n"
	if output.String() != want {
		t.Fatalf("log output = %q, want %q", output.String(), want)
	}
}

func TestConfigureGatewayUsesRequiredIntentsAndInvisiblePresence(t *testing.T) {
	var identify discordgo.Identify
	configureGateway(&identify)

	want := discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMessageReactions |
		discordgo.IntentsMessageContent
	if identify.Intents != want {
		t.Fatalf("Intents = %d, want %d", identify.Intents, want)
	}
	if identify.Presence.Status != string(discordgo.StatusInvisible) {
		t.Fatalf("Presence.Status = %q, want %q", identify.Presence.Status, discordgo.StatusInvisible)
	}
}

func TestMessageFromDiscordDetectsSpoileredVisualComponent(t *testing.T) {
	var message discordgo.Message
	err := json.Unmarshal([]byte(`{
		"author": {"id": "member"},
		"components": [{
			"type": 17,
			"spoiler": false,
			"components": [{
				"type": 12,
				"items": [{
					"media": {"url": "attachment://image.png"},
					"spoiler": true
				}]
			}]
		}]
	}`), &message)
	if err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	converted := messageFromDiscord(&message)
	if !converted.SpoileredVisualMedia {
		t.Fatal("SpoileredVisualMedia = false, want true")
	}
}

func TestMessageFromDiscordIncludesForwardedAttachments(t *testing.T) {
	var message discordgo.Message
	err := json.Unmarshal([]byte(`{
		"author": {"id": "member"},
		"message_snapshots": [{
			"message": {
				"attachments": [{
					"filename": "image.png",
					"content_type": "image/png",
					"flags": 8
				}]
			}
		}]
	}`), &message)
	if err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	converted := messageFromDiscord(&message)
	if len(converted.Attachments) != 1 {
		t.Fatalf("len(Attachments) = %d, want 1", len(converted.Attachments))
	}
	if !converted.Attachments[0].Spoiler {
		t.Fatal("Spoiler = false, want true")
	}
}

func TestSpoilerCheckLogContainsOnlySafeMetadata(t *testing.T) {
	var output bytes.Buffer
	logger := log.New(&output, "", 0)
	logSpoilerCheck(logger, "create", bot.Decision{
		Delete: true,
		Kind:   bot.DecisionSpoilerMedia,
		SpoilerCheck: &bot.SpoilerCheck{
			Attachments:          2,
			FlaggedAttachments:   1,
			LegacyMarkers:        0,
			ImageAttachments:     2,
			MatchingAttachments:  1,
			SpoileredVisualMedia: false,
		},
	})

	want := "spoiler check event=create attachments=2 flags=1 legacy_markers=0 images=2 matching_attachments=1 visual_components=false delete=true\n"
	if output.String() != want {
		t.Fatalf("log output = %q, want %q", output.String(), want)
	}
}

func TestMessageCheckLogContainsOnlySafeMetadata(t *testing.T) {
	var output bytes.Buffer
	logger := log.New(&output, "", 0)
	logMessageCheck(logger, "create", bot.Decision{
		Delete: true,
		Kind:   bot.DecisionMessageRegex,
		MessageCheck: &bot.MessageCheck{
			RegexRules:      2,
			SearchableText:  true,
			RegexEvaluated:  true,
			OriginalMatched: true,
			RegexMatched:    true,
		},
	})

	want := "message check event=create ignored=false rules_loaded=2 searchable_text=true regex_evaluated=true original_matched=true normalization_changed=false normalized_matched=false regex_matched=true delete=true\n"
	if output.String() != want {
		t.Fatalf("log output = %q, want %q", output.String(), want)
	}
	for _, sensitive := range []string{"ExampleTerm", "channel", "message_id", "author"} {
		if strings.Contains(output.String(), sensitive) {
			t.Fatalf("log output exposed %q: %q", sensitive, output.String())
		}
	}
}

func TestEmojiCheckLogContainsOnlySafeMetadata(t *testing.T) {
	var output bytes.Buffer
	logger := log.New(&output, "", 0)
	logEmojiCheck(logger, "create", bot.Decision{
		Delete: true,
		Kind:   bot.DecisionEmoji,
		EmojiCheck: &bot.EmojiCheck{
			RulesLoaded:    2,
			SearchableText: true,
			Matched:        true,
		},
	})

	want := "emoji check event=create rules_loaded=2 searchable_text=true matched=true delete=true\n"
	if output.String() != want {
		t.Fatalf("log output = %q, want %q", output.String(), want)
	}
	for _, sensitive := range []string{"👍", "channel", "message_id", "author"} {
		if strings.Contains(output.String(), sensitive) {
			t.Fatalf("log output exposed %q: %q", sensitive, output.String())
		}
	}
}

func TestDeleteFailureLogContainsOnlyStatusCodes(t *testing.T) {
	var output bytes.Buffer
	logger := log.New(&output, "", 0)
	logDeleteFailure(logger, "create", bot.DecisionSpoilerMedia, &discordgo.RESTError{
		Response: &http.Response{StatusCode: http.StatusForbidden},
		Message:  &discordgo.APIErrorMessage{Code: 50013, Message: "Missing Permissions"},
	})

	want := "delete failed event=create rule=spoiler_media http_status=403 discord_code=50013\n"
	if output.String() != want {
		t.Fatalf("log output = %q, want %q", output.String(), want)
	}
	for _, sensitive := range []string{"Missing Permissions", "channel", "message", "author"} {
		if strings.Contains(output.String(), sensitive) {
			t.Fatalf("log output exposed %q: %q", sensitive, output.String())
		}
	}
}

func TestServiceCommandUsesRenamedUserUnit(t *testing.T) {
	command, args, handled, err := serviceCommand([]string{"restart"})
	if err != nil {
		t.Fatalf("serviceCommand: %v", err)
	}
	if !handled {
		t.Fatal("handled = false, want true")
	}
	if command != "systemctl" {
		t.Fatalf("command = %q, want systemctl", command)
	}
	want := []string{"--user", "restart", "discord-message-delete.service"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("args = %q, want %q", args, want)
	}
}

func TestServiceCommandLeavesBotFlagsAlone(t *testing.T) {
	_, _, handled, err := serviceCommand([]string{"-config", "config.json"})
	if err != nil {
		t.Fatalf("serviceCommand: %v", err)
	}
	if handled {
		t.Fatal("handled = true, want false")
	}
}

func TestParseRuleCommandAcceptsAddAndDelete(t *testing.T) {
	command, err := parseRuleCommand([]string{"add", `\bexample\b`})
	if err != nil {
		t.Fatalf("parseRuleCommand add: %v", err)
	}
	if command.action != "add" || command.kind != "regex" || command.value != `\bexample\b` {
		t.Fatalf("command = %+v, want regex add", command)
	}

	command, err = parseRuleCommand([]string{"delete", `\bexample\b`})
	if err != nil {
		t.Fatalf("parseRuleCommand delete: %v", err)
	}
	if command.action != "delete" || command.kind != "regex" || command.value != `\bexample\b` {
		t.Fatalf("command = %+v, want regex delete", command)
	}
}

func TestParseRuleCommandAcceptsEmojiRule(t *testing.T) {
	command, err := parseRuleCommand([]string{"add", "emoji", ":thumbsup:"})
	if err != nil {
		t.Fatalf("parseRuleCommand: %v", err)
	}
	if command.action != "add" || command.kind != "emoji" || command.value != ":thumbsup:" {
		t.Fatalf("command = %+v, want emoji add", command)
	}
}

func TestParseRuleCommandRejectsMissingRegex(t *testing.T) {
	if _, err := parseRuleCommand([]string{"add"}); err == nil {
		t.Fatal("parseRuleCommand returned nil error for missing regex")
	}
}

func TestHandleReactionRemovesAllMatchingEmojiReactionsFromMessage(t *testing.T) {
	compiled, err := bot.CompileConfig(bot.Config{EmojiRules: []string{":thumbsup:"}})
	if err != nil {
		t.Fatalf("CompileConfig: %v", err)
	}
	store := newCleanerStore(bot.NewCleaner(compiled))
	var channelID, messageID, emojiName string
	remove := func(channel string, message string, emoji string) error {
		channelID, messageID, emojiName = channel, message, emoji
		return nil
	}
	var output bytes.Buffer
	logger := log.New(&output, "", 0)

	handleReaction(remove, &discordgo.MessageReactionAdd{MessageReaction: &discordgo.MessageReaction{
		UserID:    "member",
		ChannelID: "channel",
		MessageID: "message",
		Emoji:     discordgo.Emoji{Name: "👍"},
	}}, "bot", store, logger)

	if channelID != "channel" || messageID != "message" || emojiName != "👍" {
		t.Fatalf("remove arguments = %q, %q, %q", channelID, messageID, emojiName)
	}
	if output.String() != "reaction remove succeeded rules_loaded=1\n" {
		t.Fatalf("log output = %q", output.String())
	}
}

func TestHandleReactionLeavesUnconfiguredEmojiAlone(t *testing.T) {
	compiled, err := bot.CompileConfig(bot.Config{EmojiRules: []string{":thumbsup:"}})
	if err != nil {
		t.Fatalf("CompileConfig: %v", err)
	}
	store := newCleanerStore(bot.NewCleaner(compiled))
	called := false
	remove := func(string, string, string) error {
		called = true
		return nil
	}

	handleReaction(remove, &discordgo.MessageReactionAdd{MessageReaction: &discordgo.MessageReaction{
		UserID: "member",
		Emoji:  discordgo.Emoji{Name: "👎"},
	}}, "bot", store, log.New(&bytes.Buffer{}, "", 0))

	if called {
		t.Fatal("remove was called for an unconfigured emoji")
	}
}

func TestHandleReactionUsesCustomEmojiAPIName(t *testing.T) {
	compiled, err := bot.CompileConfig(bot.Config{
		EmojiRules: []string{"<:party:123456789012345678>"},
	})
	if err != nil {
		t.Fatalf("CompileConfig: %v", err)
	}
	store := newCleanerStore(bot.NewCleaner(compiled))
	var apiName string
	remove := func(_ string, _ string, emoji string) error {
		apiName = emoji
		return nil
	}

	handleReaction(remove, &discordgo.MessageReactionAdd{MessageReaction: &discordgo.MessageReaction{
		UserID: "member",
		Emoji: discordgo.Emoji{
			ID:   "123456789012345678",
			Name: "renamed",
		},
	}}, "bot", store, log.New(&bytes.Buffer{}, "", 0))

	if apiName != "renamed:123456789012345678" {
		t.Fatalf("API emoji name = %q, want renamed:123456789012345678", apiName)
	}
}
