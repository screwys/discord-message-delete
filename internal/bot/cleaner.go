package bot

import (
	"mime"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/mtibben/confusables"
)

type Cleaner struct {
	config *CompiledConfig
}

type Message struct {
	AuthorID             string
	Content              string
	Attachments          []Attachment
	Embeds               []Embed
	SpoileredVisualMedia bool
}

type Attachment struct {
	Filename    string
	ContentType string
	Width       int
	Height      int
	Spoiler     bool
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
	Delete       bool
	Kind         DecisionKind
	SpoilerCheck *SpoilerCheck
}

type DecisionKind string

const (
	DecisionSpoilerMedia DecisionKind = "spoiler_media"
	DecisionMessageRegex DecisionKind = "message_regex"
)

type SpoilerCheck struct {
	Attachments          int
	FlaggedAttachments   int
	LegacyMarkers        int
	ImageAttachments     int
	MatchingAttachments  int
	SpoileredVisualMedia bool
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
	var spoilerCheck *SpoilerCheck
	if cleaner.config.SpoilerImageUserID != "" && message.AuthorID == cleaner.config.SpoilerImageUserID {
		check := inspectSpoilerMedia(message)
		spoilerCheck = &check
		if check.MatchingAttachments > 0 || check.SpoileredVisualMedia {
			return Decision{
				Delete:       true,
				Kind:         DecisionSpoilerMedia,
				SpoilerCheck: spoilerCheck,
			}
		}
	}

	searchText := messageSearchText(message)
	foldedSearchText := foldConfusableText(searchText)
	for _, rule := range cleaner.config.MessageRegexes {
		if rule.Regex.MatchString(searchText) ||
			(foldedSearchText != searchText && rule.Regex.MatchString(foldedSearchText)) {
			return Decision{
				Delete:       true,
				Kind:         DecisionMessageRegex,
				SpoilerCheck: spoilerCheck,
			}
		}
	}
	return Decision{SpoilerCheck: spoilerCheck}
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

func foldConfusableText(value string) string {
	needsFolding := false
	for _, char := range value {
		if char > unicode.MaxASCII || isDefaultIgnorable(char) {
			needsFolding = true
			break
		}
	}
	if !needsFolding {
		return value
	}

	var builder strings.Builder
	builder.Grow(len(value))
	for _, char := range value {
		if isDefaultIgnorable(char) {
			continue
		}
		if char <= unicode.MaxASCII {
			builder.WriteRune(char)
			continue
		}
		builder.WriteString(confusables.Skeleton(string(char)))
	}
	return builder.String()
}

func isDefaultIgnorable(char rune) bool {
	return unicode.Is(unicode.Cf, char) ||
		unicode.Is(unicode.Variation_Selector, char) ||
		unicode.Is(unicode.Other_Default_Ignorable_Code_Point, char)
}

func inspectSpoilerMedia(message Message) SpoilerCheck {
	check := SpoilerCheck{
		Attachments:          len(message.Attachments),
		SpoileredVisualMedia: message.SpoileredVisualMedia,
	}
	for _, attachment := range message.Attachments {
		flagged := attachment.Spoiler
		legacyMarked := isSpoilerFilename(attachment.Filename)
		image := isImageAttachment(attachment)
		if flagged {
			check.FlaggedAttachments++
		}
		if legacyMarked {
			check.LegacyMarkers++
		}
		if image {
			check.ImageAttachments++
		}
		if (flagged || legacyMarked) && image {
			check.MatchingAttachments++
		}
	}
	return check
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
		return contentType == "" && attachment.Width > 0 && attachment.Height > 0
	}
}
