package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type slackConfig struct {
	Token          string            `json:"token,omitempty"`
	Cookies        map[string]string `json:"cookies,omitempty"`
	TeamDomain     string            `json:"teamDomain,omitempty"`
	SavedAt        string            `json:"savedAt,omitempty"`
	ExpiresAt      int64             `json:"expiresAt,omitempty"`
	DefaultChannel string            `json:"defaultChannel,omitempty"`
	UserID         string            `json:"userId,omitempty"`
	User           string            `json:"user,omitempty"`
	TeamID         string            `json:"teamId,omitempty"`
	Team           string            `json:"team,omitempty"`
}

func slackBrowserProfileDir() (string, error) {
	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(configDirectory, "slack-cli", "browser-profile"), nil
}

func slackConfigPath() (string, error) {
	if path := os.Getenv("SLACK_CONFIG_FILE"); path != "" {
		return path, nil
	}
	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(configDirectory, "slack-cli", "config.json"), nil
}

func loadConfig(path string) (slackConfig, bool, error) {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return slackConfig{}, false, nil
	}
	if err != nil {
		return slackConfig{}, false, fmt.Errorf("read Slack config: %w", err)
	}

	var config slackConfig
	if err := json.Unmarshal(contents, &config); err != nil {
		return slackConfig{}, true, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	return config, true, nil
}

func saveConfig(path string, config slackConfig) error {
	contents, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Slack config: %w", err)
	}
	contents = append(contents, '\n')

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create Slack config directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".slack-cli-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary Slack config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure temporary Slack config: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return fmt.Errorf("write Slack config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close Slack config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace Slack config: %w", err)
	}
	return os.Chmod(path, 0o600)
}

func deleteConfig(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete Slack config: %w", err)
	}
	return nil
}

func redactedConfig(config slackConfig) map[string]string {
	result := map[string]string{}
	if config.Token != "" {
		result["token"] = "redacted"
	}
	if len(config.Cookies) > 0 {
		result["cookies"] = "redacted"
	}
	if config.TeamDomain != "" {
		result["teamDomain"] = config.TeamDomain
	}
	if config.SavedAt != "" {
		result["savedAt"] = config.SavedAt
	}
	if config.ExpiresAt != 0 {
		result["expiresAt"] = fmt.Sprintf("%d", config.ExpiresAt)
	}
	if config.DefaultChannel != "" {
		result["defaultChannel"] = config.DefaultChannel
	}
	if config.UserID != "" {
		result["userId"] = config.UserID
	}
	if config.User != "" {
		result["user"] = config.User
	}
	if config.TeamID != "" {
		result["teamId"] = config.TeamID
	}
	if config.Team != "" {
		result["team"] = config.Team
	}
	return result
}
