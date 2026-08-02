package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"spoiler-cleaner/internal/bot"
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
