package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"regexp/syntax"
	"strings"
	"unicode"

	"github.com/kyokomi/emoji/v2"
)

var (
	customEmojiPattern       = regexp.MustCompile(`^<(?:a)?:([A-Za-z0-9_~]{2,32}):([0-9]+)>$`)
	customEmojiInTextPattern = regexp.MustCompile(`<(?:a)?:([A-Za-z0-9_~]{2,32}):([0-9]+)>`)
	customEmojiNamePattern   = regexp.MustCompile(`^:([A-Za-z0-9_~]{2,32}):$`)
	emojiCodeMap             = emoji.CodeMap()
	emojiReverseCodeMap      = emoji.RevCodeMap()
)

const blockedWordsRuleName = "blocked words"

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
	Unicode    string
	CustomID   string
	CustomName string
}

func LoadConfig(path string) (*CompiledConfig, error) {
	config, err := readConfig(path)
	if err != nil {
		return nil, err
	}
	return CompileConfig(config)
}

func NormalizeConfig(path string) (bool, error) {
	config, err := readConfig(path)
	if err != nil {
		return false, err
	}
	original := config
	config.MessageRegexes, err = canonicalizeRegexRules(config.MessageRegexes)
	if err != nil {
		return false, err
	}
	config.EmojiRules, _, err = canonicalizeEmojiRules(config.EmojiRules)
	if err != nil {
		return false, err
	}
	if _, err := CompileConfig(config); err != nil {
		return false, err
	}
	if reflect.DeepEqual(config, original) {
		return false, nil
	}
	return true, writeConfig(path, config)
}

func AddRegexRule(path string, pattern string) (bool, error) {
	config, err := readConfig(path)
	if err != nil {
		return false, err
	}

	expanded, err := expandBlockedWords(config.MessageRegexes)
	if err != nil {
		return false, err
	}
	added := !containsManagedRegexRule(expanded, pattern)
	if added {
		expanded = append(expanded, RegexRuleConfig{
			Name:    pattern,
			Pattern: pattern,
		})
	}
	config.MessageRegexes, err = canonicalizeRegexRules(expanded)
	if err != nil {
		return false, err
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

	removed := 0
	for index := range config.MessageRegexes {
		rule := &config.MessageRegexes[index]
		if rule.Name != blockedWordsRuleName {
			continue
		}
		updated, matches := removeWordFromBlockedPattern(rule.Pattern, pattern)
		if matches > 0 {
			rule.Pattern = updated
			removed += matches
		}
	}
	config.MessageRegexes = slicesWithoutEmptyPatterns(config.MessageRegexes)

	expanded, err := expandBlockedWords(config.MessageRegexes)
	if err != nil {
		return 0, err
	}
	rules := make([]RegexRuleConfig, 0, len(expanded))
	for _, rule := range expanded {
		if managedRegexRulesEqual(rule, pattern) {
			removed++
			continue
		}
		rules = append(rules, rule)
	}
	if removed == 0 {
		return 0, fmt.Errorf("rule %q was not found", pattern)
	}
	config.MessageRegexes, err = canonicalizeRegexRules(rules)
	if err != nil {
		return 0, err
	}
	if _, err := CompileConfig(config); err != nil {
		return 0, err
	}
	return removed, writeConfig(path, config)
}

func removeWordFromBlockedPattern(pattern string, target string) (string, int) {
	expression, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return pattern, 0
	}
	branches := []*syntax.Regexp{expression}
	if expression.Op == syntax.OpAlternate {
		branches = expression.Sub
	}

	remainingBranches := make([]string, 0, len(branches))
	removed := 0
	for _, branch := range branches {
		alternatives, finite := literalRegexAlternatives(branch)
		if !finite {
			remainingBranches = append(remainingBranches, branch.String())
			continue
		}
		remaining := make([]string, 0, len(alternatives))
		branchRemoved := 0
		for _, alternative := range alternatives {
			if blockedWordsEqual(alternative, target) {
				branchRemoved++
				continue
			}
			remaining = append(remaining, alternative)
		}
		if branchRemoved == 0 {
			remainingBranches = append(remainingBranches, branch.String())
			continue
		}
		removed += branchRemoved
		if len(remaining) > 0 {
			remainingBranches = append(remainingBranches, blockedWordsPattern(remaining))
		}
	}
	if removed == 0 {
		return pattern, 0
	}
	if len(remainingBranches) == 0 {
		return "", removed
	}
	if len(remainingBranches) == 1 {
		return remainingBranches[0], removed
	}
	for index := range remainingBranches {
		remainingBranches[index] = "(?:" + remainingBranches[index] + ")"
	}
	return strings.Join(remainingBranches, "|"), removed
}

func blockedWordsEqual(left string, right string) bool {
	canonicalLeft, leftManaged := canonicalBlockedWord(left)
	canonicalRight, rightManaged := canonicalBlockedWord(right)
	if leftManaged && rightManaged {
		return canonicalLeft == canonicalRight
	}
	return left == right
}

func blockedWordsPattern(words []string) string {
	escaped := make([]string, 0, len(words))
	seen := make(map[string]struct{}, len(words))
	for _, word := range words {
		canonical, managed := canonicalBlockedWord(word)
		if managed {
			word = canonical
		}
		if _, exists := seen[word]; exists {
			continue
		}
		seen[word] = struct{}{}
		escaped = append(escaped, regexp.QuoteMeta(word))
	}
	return `(?i)\b(?:` + strings.Join(escaped, "|") + `)\b`
}

func slicesWithoutEmptyPatterns(rules []RegexRuleConfig) []RegexRuleConfig {
	kept := rules[:0]
	for _, rule := range rules {
		if rule.Pattern != "" {
			kept = append(kept, rule)
		}
	}
	return kept
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
	if rules == nil {
		return nil, 0, nil
	}
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
	if customEmojiNamePattern.MatchString(value) {
		return value, nil
	}
	return "", errors.New("use an :emoji_name:, a Unicode emoji, or a custom emoji mention")
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
	if match := customEmojiNamePattern.FindStringSubmatch(value); len(match) != 0 {
		return "custom-name:" + match[1]
	}
	return "unicode:" + value
}

func canonicalizeRegexRules(rules []RegexRuleConfig) ([]RegexRuleConfig, error) {
	expanded, err := expandBlockedWords(rules)
	if err != nil {
		return nil, err
	}
	words := make([]string, 0, len(expanded))
	otherRules := make([]RegexRuleConfig, 0, len(expanded))
	customBlockedPatterns := make([]string, 0, 1)
	seenWords := make(map[string]struct{}, len(expanded))
	seenPatterns := make(map[string]struct{}, len(expanded))
	for _, rule := range expanded {
		if rule.Name == blockedWordsRuleName {
			if _, exists := seenPatterns[rule.Pattern]; !exists {
				seenPatterns[rule.Pattern] = struct{}{}
				customBlockedPatterns = append(customBlockedPatterns, rule.Pattern)
			}
			continue
		}
		word, managed := managedBlockedWord(rule)
		if managed {
			if _, exists := seenWords[word]; !exists {
				seenWords[word] = struct{}{}
				words = append(words, word)
			}
			continue
		}
		if _, exists := seenPatterns[rule.Pattern]; exists {
			continue
		}
		seenPatterns[rule.Pattern] = struct{}{}
		otherRules = append(otherRules, rule)
	}

	canonical := make([]RegexRuleConfig, 0, len(otherRules)+1)
	blockedPattern, err := combineBlockedWordPatterns(customBlockedPatterns, words)
	if err != nil {
		return nil, err
	}
	if blockedPattern != "" {
		canonical = append(canonical, RegexRuleConfig{
			Name:    blockedWordsRuleName,
			Pattern: blockedPattern,
		})
	}
	canonical = append(canonical, otherRules...)
	return canonical, nil
}

func combineBlockedWordPatterns(customPatterns []string, words []string) (string, error) {
	compiledCustom := make([]*regexp.Regexp, 0, len(customPatterns))
	for _, pattern := range customPatterns {
		compiled, err := regexp.Compile("(?i:" + pattern + ")")
		if err != nil {
			return "", errors.New("the blocked words rule is invalid")
		}
		compiledCustom = append(compiledCustom, compiled)
	}

	unmatchedWords := make([]string, 0, len(words))
	for _, word := range words {
		matched := false
		for _, compiled := range compiledCustom {
			if compiled.MatchString(word) {
				matched = true
				break
			}
		}
		if !matched {
			unmatchedWords = append(unmatchedWords, word)
		}
	}
	if len(customPatterns) == 0 {
		if len(unmatchedWords) == 0 {
			return "", nil
		}
		return `(?i)\b(?:` + strings.Join(unmatchedWords, "|") + `)\b`, nil
	}
	if len(customPatterns) == 1 && len(unmatchedWords) == 0 {
		return customPatterns[0], nil
	}

	parts := make([]string, 0, len(customPatterns)+1)
	for _, pattern := range customPatterns {
		parts = append(parts, "(?:"+pattern+")")
	}
	if len(unmatchedWords) > 0 {
		parts = append(parts, `(?:\b(?:`+strings.Join(unmatchedWords, "|")+`)\b)`)
	}
	return strings.Join(parts, "|"), nil
}

func expandBlockedWords(rules []RegexRuleConfig) ([]RegexRuleConfig, error) {
	expanded := make([]RegexRuleConfig, 0, len(rules))
	for _, rule := range rules {
		if rule.Name != blockedWordsRuleName {
			expanded = append(expanded, rule)
			continue
		}
		words, ok := parseBlockedWordsPattern(rule.Pattern)
		if !ok {
			expanded = append(expanded, rule)
			continue
		}
		for _, word := range words {
			expanded = append(expanded, RegexRuleConfig{Name: word, Pattern: word})
		}
	}
	return expanded, nil
}

func parseBlockedWordsPattern(pattern string) ([]string, bool) {
	expression, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, false
	}
	words, ok := literalRegexAlternatives(expression)
	if !ok || len(words) == 0 {
		return nil, false
	}
	for _, word := range words {
		if _, ok := canonicalBlockedWord(word); !ok {
			return nil, false
		}
	}
	return words, true
}

func literalRegexAlternatives(expression *syntax.Regexp) ([]string, bool) {
	const maxAlternatives = 4096
	if expression == nil {
		return nil, false
	}
	switch expression.Op {
	case syntax.OpEmptyMatch, syntax.OpBeginLine, syntax.OpEndLine, syntax.OpBeginText,
		syntax.OpEndText, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return []string{""}, true
	case syntax.OpLiteral:
		return []string{string(expression.Rune)}, true
	case syntax.OpCapture:
		return literalRegexAlternatives(expression.Sub[0])
	case syntax.OpCharClass:
		alternatives := make([]string, 0, len(expression.Rune)/2)
		for index := 0; index < len(expression.Rune); index += 2 {
			low, high := expression.Rune[index], expression.Rune[index+1]
			if high-low > 16 || len(alternatives)+int(high-low)+1 > maxAlternatives {
				return nil, false
			}
			for char := low; char <= high; char++ {
				alternatives = append(alternatives, string(char))
			}
		}
		return alternatives, true
	case syntax.OpAlternate:
		alternatives := make([]string, 0, len(expression.Sub))
		for _, child := range expression.Sub {
			childAlternatives, ok := literalRegexAlternatives(child)
			if !ok || len(alternatives)+len(childAlternatives) > maxAlternatives {
				return nil, false
			}
			alternatives = append(alternatives, childAlternatives...)
		}
		return alternatives, true
	case syntax.OpConcat:
		alternatives := []string{""}
		for _, child := range expression.Sub {
			childAlternatives, ok := literalRegexAlternatives(child)
			if !ok || len(alternatives)*len(childAlternatives) > maxAlternatives {
				return nil, false
			}
			combined := make([]string, 0, len(alternatives)*len(childAlternatives))
			for _, prefix := range alternatives {
				for _, suffix := range childAlternatives {
					combined = append(combined, prefix+suffix)
				}
			}
			alternatives = combined
		}
		return alternatives, true
	default:
		return nil, false
	}
}

func managedBlockedWord(rule RegexRuleConfig) (string, bool) {
	if rule.Name != rule.Pattern {
		return "", false
	}
	return canonicalBlockedWord(rule.Pattern)
}

func canonicalBlockedWord(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	for _, char := range value {
		if unicode.IsLetter(char) || unicode.IsNumber(char) || unicode.IsSpace(char) ||
			strings.ContainsRune("@$_-'", char) {
			continue
		}
		return "", false
	}
	return strings.ToLower(foldConfusableText(value)), true
}

func containsManagedRegexRule(rules []RegexRuleConfig, pattern string) bool {
	for _, rule := range rules {
		if managedRegexRulesEqual(rule, pattern) {
			return true
		}
	}
	return false
}

func managedRegexRulesEqual(rule RegexRuleConfig, pattern string) bool {
	targetWord, targetManaged := canonicalBlockedWord(pattern)
	ruleWord, ruleManaged := managedBlockedWord(rule)
	if targetManaged && ruleManaged {
		return targetWord == ruleWord
	}
	return rule.Pattern == pattern
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
		match = customEmojiNamePattern.FindStringSubmatch(value)
		if len(match) != 0 {
			emojiRules = append(emojiRules, EmojiRule{CustomName: match[1]})
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
