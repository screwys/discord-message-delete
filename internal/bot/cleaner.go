package bot

import (
	"mime"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

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
	MessageCheck *MessageCheck
	EmojiCheck   *EmojiCheck
}

type DecisionKind string

const (
	DecisionSpoilerMedia DecisionKind = "spoiler_media"
	DecisionMessageRegex DecisionKind = "message_regex"
	DecisionEmoji        DecisionKind = "emoji"
)

type SpoilerCheck struct {
	Attachments          int
	FlaggedAttachments   int
	LegacyMarkers        int
	ImageAttachments     int
	MatchingAttachments  int
	SpoileredVisualMedia bool
}

type MessageCheck struct {
	Ignored              bool
	RegexRules           int
	SearchableText       bool
	RegexEvaluated       bool
	OriginalMatched      bool
	NormalizationChanged bool
	NormalizedMatched    bool
	RegexMatched         bool
}

type EmojiCheck struct {
	RulesLoaded    int
	SearchableText bool
	Matched        bool
}

type ReactionEmoji struct {
	ID   string
	Name string
}

type ReactionDecision struct {
	Remove      bool
	RulesLoaded int
}

func NewCleaner(config *CompiledConfig) *Cleaner {
	return &Cleaner{config: config}
}

func (cleaner *Cleaner) Decide(message Message) Decision {
	if cleaner == nil || cleaner.config == nil {
		return Decision{}
	}
	searchText := messageSearchText(message)
	messageCheck := &MessageCheck{
		RegexRules:     len(cleaner.config.MessageRegexes),
		SearchableText: searchText != "",
	}
	emojiCheck := &EmojiCheck{
		RulesLoaded:    len(cleaner.config.EmojiRules),
		SearchableText: searchText != "",
	}
	if _, ignored := cleaner.config.IgnoredUserIDs[message.AuthorID]; ignored {
		messageCheck.Ignored = true
		return Decision{MessageCheck: messageCheck, EmojiCheck: emojiCheck}
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
				MessageCheck: messageCheck,
				EmojiCheck:   emojiCheck,
			}
		}
	}

	for _, rule := range cleaner.config.EmojiRules {
		if rule.matchesText(searchText) {
			emojiCheck.Matched = true
			return Decision{
				Delete:       true,
				Kind:         DecisionEmoji,
				SpoilerCheck: spoilerCheck,
				MessageCheck: messageCheck,
				EmojiCheck:   emojiCheck,
			}
		}
	}

	messageCheck.RegexEvaluated = true
	foldedSearchText := foldConfusableText(searchText)
	messageCheck.NormalizationChanged = foldedSearchText != searchText
	for _, rule := range cleaner.config.MessageRegexes {
		originalMatched := rule.Regex.MatchString(searchText)
		normalizedMatched := messageCheck.NormalizationChanged && rule.Regex.MatchString(foldedSearchText)
		messageCheck.OriginalMatched = messageCheck.OriginalMatched || originalMatched
		messageCheck.NormalizedMatched = messageCheck.NormalizedMatched || normalizedMatched
		if originalMatched || normalizedMatched {
			messageCheck.RegexMatched = true
			return Decision{
				Delete:       true,
				Kind:         DecisionMessageRegex,
				SpoilerCheck: spoilerCheck,
				MessageCheck: messageCheck,
				EmojiCheck:   emojiCheck,
			}
		}
	}
	return Decision{SpoilerCheck: spoilerCheck, MessageCheck: messageCheck, EmojiCheck: emojiCheck}
}

func (cleaner *Cleaner) DecideReaction(reaction ReactionEmoji) ReactionDecision {
	if cleaner == nil || cleaner.config == nil {
		return ReactionDecision{}
	}
	decision := ReactionDecision{RulesLoaded: len(cleaner.config.EmojiRules)}
	for _, rule := range cleaner.config.EmojiRules {
		if rule.matchesReaction(reaction) {
			decision.Remove = true
			return decision
		}
	}
	return decision
}

func (rule EmojiRule) matchesText(text string) bool {
	if rule.Unicode != "" {
		return containsExactEmoji(text, rule.Unicode)
	}
	for _, match := range customEmojiInTextPattern.FindAllStringSubmatch(text, -1) {
		if len(match) > 2 && (match[2] == rule.CustomID || match[1] == rule.CustomName) {
			return true
		}
	}
	return false
}

func containsExactEmoji(text string, emoji string) bool {
	for offset := 0; offset < len(text); {
		relative := strings.Index(text[offset:], emoji)
		if relative < 0 {
			return false
		}
		start := offset + relative
		end := start + len(emoji)
		joinedBefore := false
		if start > 0 {
			previous, _ := utf8.DecodeLastRuneInString(text[:start])
			joinedBefore = previous == '\u200d'
		}
		joinedAfter := false
		if end < len(text) {
			next, _ := utf8.DecodeRuneInString(text[end:])
			joinedAfter = next == '\u200d' || isVariationSelector(next) || isEmojiModifier(next)
		}
		if !joinedBefore && !joinedAfter {
			return true
		}
		offset = end
	}
	return false
}

func isVariationSelector(char rune) bool {
	return char >= '\ufe00' && char <= '\ufe0f' || char >= '\U000e0100' && char <= '\U000e01ef'
}

func isEmojiModifier(char rune) bool {
	return char >= '\U0001f3fb' && char <= '\U0001f3ff'
}

func (rule EmojiRule) matchesReaction(reaction ReactionEmoji) bool {
	if rule.CustomID != "" {
		return reaction.ID == rule.CustomID
	}
	if rule.CustomName != "" {
		return reaction.ID != "" && reaction.Name == rule.CustomName
	}
	return reaction.ID == "" && reaction.Name == rule.Unicode
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
	var builder strings.Builder
	builder.Grow(len(value))
	for _, char := range value {
		if isDefaultIgnorable(char) {
			continue
		}
		if replacement, replaced := asciiConfusable(char); replaced {
			builder.WriteRune(replacement)
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

func asciiConfusable(char rune) (rune, bool) {
	switch char {
	case '@', '4':
		return 'a', true
	case '3':
		return 'e', true
	case '1', '|':
		return 'l', true
	case '5', '$':
		return 's', true
	case '7':
		return 't', true
	case '8':
		return 'b', true
	case '0':
		return 'o', true
	default:
		return 0, false
	}
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
