package bot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	config, err := readConfig(path)
	if err != nil {
		return nil, err
	}
	return CompileConfig(config)
}

func AppendRegexRule(path string, pattern string) error {
	config, err := readConfig(path)
	if err != nil {
		return err
	}

	config.MessageRegexes = append(config.MessageRegexes, RegexRuleConfig{
		Name:    pattern,
		Pattern: pattern,
	})
	if _, err := CompileConfig(config); err != nil {
		return err
	}
	return writeConfig(path, config)
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

	return &CompiledConfig{
		SpoilerImageUserID: config.SpoilerImageUserID,
		IgnoredUserIDs:     ignoredUsers,
		MessageRegexes:     regexRules,
	}, nil
}
