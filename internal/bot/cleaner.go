package bot

import (
	"fmt"
	"mime"
	"path/filepath"
	"strings"
)

type Cleaner struct {
	config *CompiledConfig
}

type Message struct {
	AuthorID    string
	Content     string
	Attachments []Attachment
	Embeds      []Embed
}

type Attachment struct {
	Filename    string
	ContentType string
	Width       int
	Height      int
}

type Embed struct {
	Title        string
	Description  string
	URL          string
	AuthorName   string
	AuthorURL    string
	ProviderName string
	ProviderURL  string
	FooterText   string
	Fields       []EmbedField
}

type EmbedField struct {
	Name  string
	Value string
}

type Decision struct {
	Delete bool
	Reason string
}

func NewCleaner(config *CompiledConfig) *Cleaner {
	return &Cleaner{config: config}
}

func (cleaner *Cleaner) Decide(message Message) Decision {
	if cleaner == nil || cleaner.config == nil {
		return Decision{}
	}
	if _, ignored := cleaner.config.IgnoredUserIDs[message.AuthorID]; ignored {
		return Decision{}
	}
	if cleaner.config.SpoilerImageUserID != "" &&
		message.AuthorID == cleaner.config.SpoilerImageUserID &&
		hasSpoileredImageAttachment(message.Attachments) {
		return Decision{Delete: true, Reason: "spoilered image from configured user"}
	}

	searchText := messageSearchText(message)
	for _, rule := range cleaner.config.MessageRegexes {
		if rule.Regex.MatchString(searchText) {
			return Decision{Delete: true, Reason: fmt.Sprintf("message regex matched: %s", rule.Label())}
		}
	}
	return Decision{}
}

func messageSearchText(message Message) string {
	var builder strings.Builder
	appendPart := func(value string) {
		if value == "" {
			return
		}
		if builder.Len() > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(value)
	}

	appendPart(message.Content)
	for _, embed := range message.Embeds {
		appendPart(embed.Title)
		appendPart(embed.Description)
		appendPart(embed.URL)
		appendPart(embed.AuthorName)
		appendPart(embed.AuthorURL)
		appendPart(embed.ProviderName)
		appendPart(embed.ProviderURL)
		appendPart(embed.FooterText)
		for _, field := range embed.Fields {
			appendPart(field.Name)
			appendPart(field.Value)
		}
	}

	return builder.String()
}

func hasSpoileredImageAttachment(attachments []Attachment) bool {
	for _, attachment := range attachments {
		if isSpoilerFilename(attachment.Filename) && isImageAttachment(attachment) {
			return true
		}
	}
	return false
}

func isSpoilerFilename(filename string) bool {
	return strings.HasPrefix(strings.ToUpper(filename), "SPOILER_")
}

func isImageAttachment(attachment Attachment) bool {
	contentType := strings.TrimSpace(attachment.ContentType)
	if contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err == nil && strings.HasPrefix(strings.ToLower(mediaType), "image/") {
			return true
		}
	}
	switch strings.ToLower(filepath.Ext(attachment.Filename)) {
	case ".avif", ".bmp", ".gif", ".heic", ".heif", ".jpeg", ".jpg", ".png", ".tif", ".tiff", ".webp":
		return true
	default:
		return false
	}
}
