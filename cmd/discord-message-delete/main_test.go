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

func TestGatewayIntentsCoverGuildMessagesAndContent(t *testing.T) {
	want := discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsMessageContent
	if gatewayIntents() != want {
		t.Fatalf("gatewayIntents() = %d, want %d", gatewayIntents(), want)
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
	action, pattern, err := parseRuleCommand([]string{"add", `\bexample\b`})
	if err != nil {
		t.Fatalf("parseRuleCommand add: %v", err)
	}
	if action != "add" || pattern != `\bexample\b` {
		t.Fatalf("action, pattern = %q, %q, want add, \\bexample\\b", action, pattern)
	}

	action, pattern, err = parseRuleCommand([]string{"delete", `\bexample\b`})
	if err != nil {
		t.Fatalf("parseRuleCommand delete: %v", err)
	}
	if action != "delete" || pattern != `\bexample\b` {
		t.Fatalf("action, pattern = %q, %q, want delete, \\bexample\\b", action, pattern)
	}
}

func TestParseRuleCommandRejectsMissingRegex(t *testing.T) {
	if _, _, err := parseRuleCommand([]string{"add"}); err == nil {
		t.Fatal("parseRuleCommand returned nil error for missing regex")
	}
}
