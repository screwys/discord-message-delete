package main

import (
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
