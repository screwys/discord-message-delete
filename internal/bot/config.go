package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kyokomi/emoji/v2"
)

var (
	customEmojiPattern       = regexp.MustCompile(`^<(?:a)?:([A-Za-z0-9_~]{2,32}):([0-9]+)>$`)
	customEmojiInTextPattern = regexp.MustCompile(`<(?:a)?:([A-Za-z0-9_~]{2,32}):([0-9]+)>`)
	emojiCodeMap             = emoji.CodeMap()
	emojiReverseCodeMap      = emoji.RevCodeMap()
)

type Config struct {
	SpoilerImageUserID string            `json:"spoiler_image_user_id"`
	IgnoredUserIDs     []string          `json:"ignored_user_ids"`
	MessageRegexes     []RegexRuleConfig `json:"message_regexes"`
	EmojiRules         []string          `json:"emoji_rules,omitempty"`
}

type RegexRuleConfig struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
}

type CompiledConfig struct {
	SpoilerImageUserID string
	IgnoredUserIDs     map[string]struct{}
	MessageRegexes     []RegexRule
	EmojiRules         []EmojiRule
}

type RegexRule struct {
	Regex *regexp.Regexp
}

type EmojiRule struct {
	Unicode  string
	CustomID string
}

func LoadConfig(path string) (*CompiledConfig, error) {
	config, err := readConfig(path)
	if err != nil {
		return nil, err
	}
	return CompileConfig(config)
}

func AddRegexRule(path string, pattern string) (bool, error) {
	config, err := readConfig(path)
	if err != nil {
		return false, err
	}

	config.MessageRegexes, _ = deduplicateRegexRules(config.MessageRegexes)
	added := !containsRegexRule(config.MessageRegexes, pattern)
	if added {
		config.MessageRegexes = append(config.MessageRegexes, RegexRuleConfig{
			Name:    pattern,
			Pattern: pattern,
		})
	}
	if _, err := CompileConfig(config); err != nil {
		return false, err
	}
	return added, writeConfig(path, config)
}

func RemoveRegexRule(path string, pattern string) (int, error) {
	config, err := readConfig(path)
	if err != nil {
		return 0, err
	}

	rules := make([]RegexRuleConfig, 0, len(config.MessageRegexes))
	removed := 0
	for _, rule := range config.MessageRegexes {
		if rule.Pattern == pattern {
			removed++
			continue
		}
		rules = append(rules, rule)
	}
	if removed == 0 {
		return 0, fmt.Errorf("rule %q was not found", pattern)
	}
	config.MessageRegexes, _ = deduplicateRegexRules(rules)
	if _, err := CompileConfig(config); err != nil {
		return 0, err
	}
	return removed, writeConfig(path, config)
}

func AddEmojiRule(path string, value string) (bool, error) {
	config, err := readConfig(path)
	if err != nil {
		return false, err
	}

	canonical, err := canonicalEmoji(value)
	if err != nil {
		return false, err
	}
	config.EmojiRules, _, err = canonicalizeEmojiRules(config.EmojiRules)
	if err != nil {
		return false, err
	}
	added := !containsEmoji(config.EmojiRules, canonical)
	if added {
		config.EmojiRules = append(config.EmojiRules, canonical)
	}
	if _, err := CompileConfig(config); err != nil {
		return false, err
	}
	return added, writeConfig(path, config)
}

func RemoveEmojiRule(path string, value string) (int, error) {
	config, err := readConfig(path)
	if err != nil {
		return 0, err
	}

	canonical, err := canonicalEmoji(value)
	if err != nil {
		return 0, err
	}
	config.EmojiRules, _, err = canonicalizeEmojiRules(config.EmojiRules)
	if err != nil {
		return 0, err
	}
	rules := make([]string, 0, len(config.EmojiRules))
	removed := 0
	targetIdentity := emojiIdentity(canonical)
	for _, rule := range config.EmojiRules {
		if emojiIdentity(rule) == targetIdentity {
			removed++
			continue
		}
		rules = append(rules, rule)
	}
	if removed == 0 {
		return 0, fmt.Errorf("emoji rule was not found")
	}
	config.EmojiRules = rules
	if _, err := CompileConfig(config); err != nil {
		return 0, err
	}
	return removed, writeConfig(path, config)
}

func canonicalizeEmojiRules(rules []string) ([]string, int, error) {
	unique := make([]string, 0, len(rules))
	seen := make(map[string]struct{}, len(rules))
	removed := 0
	for index, rule := range rules {
		canonical, err := canonicalEmoji(rule)
		if err != nil {
			return nil, 0, fmt.Errorf("emoji_rules[%d] is invalid", index)
		}
		identity := emojiIdentity(canonical)
		if _, exists := seen[identity]; exists {
			removed++
			continue
		}
		seen[identity] = struct{}{}
		unique = append(unique, canonical)
	}
	return unique, removed, nil
}

func canonicalEmoji(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("emoji is required")
	}
	if match := customEmojiPattern.FindStringSubmatch(value); len(match) != 0 {
		return "<:" + match[1] + ":" + match[2] + ">", nil
	}
	if resolved, exists := emojiCodeMap[value]; exists {
		return resolved, nil
	}
	if _, exists := emojiReverseCodeMap[value]; exists {
		return value, nil
	}
	return "", errors.New("use a supported :shortcode:, a Unicode emoji, or a custom emoji mention")
}

func containsEmoji(values []string, value string) bool {
	identity := emojiIdentity(value)
	for _, candidate := range values {
		if emojiIdentity(candidate) == identity {
			return true
		}
	}
	return false
}

func emojiIdentity(value string) string {
	if match := customEmojiPattern.FindStringSubmatch(value); len(match) != 0 {
		return "custom:" + match[2]
	}
	return "unicode:" + value
}

func deduplicateRegexRules(rules []RegexRuleConfig) ([]RegexRuleConfig, int) {
	unique := make([]RegexRuleConfig, 0, len(rules))
	seen := make(map[string]struct{}, len(rules))
	removed := 0
	for _, rule := range rules {
		if _, exists := seen[rule.Pattern]; exists {
			removed++
			continue
		}
		seen[rule.Pattern] = struct{}{}
		unique = append(unique, rule)
	}
	return unique, removed
}

func containsRegexRule(rules []RegexRuleConfig, pattern string) bool {
	for _, rule := range rules {
		if rule.Pattern == pattern {
			return true
		}
	}
	return false
}

func readConfig(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func writeConfig(path string, config Config) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	contents, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	contents = append(contents, '\n')

	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".config-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func CompileConfig(config Config) (*CompiledConfig, error) {
	ignoredUsers := make(map[string]struct{}, len(config.IgnoredUserIDs))
	for _, userID := range config.IgnoredUserIDs {
		if userID != "" {
			ignoredUsers[userID] = struct{}{}
		}
	}

	regexRules := make([]RegexRule, 0, len(config.MessageRegexes))
	for index, rule := range config.MessageRegexes {
		if rule.Pattern == "" {
			return nil, fmt.Errorf("message_regexes[%d].pattern is required", index)
		}
		compiled, err := regexp.Compile("(?i:" + rule.Pattern + ")")
		if err != nil {
			return nil, fmt.Errorf("message_regexes[%d] is invalid", index)
		}
		regexRules = append(regexRules, RegexRule{Regex: compiled})
	}

	emojiValues, _, err := canonicalizeEmojiRules(config.EmojiRules)
	if err != nil {
		return nil, err
	}
	emojiRules := make([]EmojiRule, 0, len(emojiValues))
	for _, value := range emojiValues {
		match := customEmojiPattern.FindStringSubmatch(value)
		if len(match) != 0 {
			emojiRules = append(emojiRules, EmojiRule{CustomID: match[2]})
			continue
		}
		emojiRules = append(emojiRules, EmojiRule{Unicode: value})
	}

	return &CompiledConfig{
		SpoilerImageUserID: config.SpoilerImageUserID,
		IgnoredUserIDs:     ignoredUsers,
		MessageRegexes:     regexRules,
		EmojiRules:         emojiRules,
	}, nil
}
