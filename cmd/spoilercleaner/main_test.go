package main

import (
	"encoding/json"
	"testing"

	"github.com/bwmarrin/discordgo"
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
