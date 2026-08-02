package bot

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

type Config struct {
	SpoilerImageUserID string            `json:"spoiler_image_user_id"`
	IgnoredUserIDs     []string          `json:"ignored_user_ids"`
	MessageRegexes     []RegexRuleConfig `json:"message_regexes"`
}

type RegexRuleConfig struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
}

type CompiledConfig struct {
	SpoilerImageUserID string
	IgnoredUserIDs     map[string]struct{}
	MessageRegexes     []RegexRule
}

type RegexRule struct {
	Regex *regexp.Regexp
}

func LoadConfig(path string) (*CompiledConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}
	return CompileConfig(config)
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
		compiled, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return nil, fmt.Errorf("message_regexes[%d] is invalid", index)
		}
		regexRules = append(regexRules, RegexRule{Regex: compiled})
	}

	return &CompiledConfig{
		SpoilerImageUserID: config.SpoilerImageUserID,
		IgnoredUserIDs:     ignoredUsers,
		MessageRegexes:     regexRules,
	}, nil
}
