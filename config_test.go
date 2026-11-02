package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveConfigIsPrivateAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "slack-cli.json")
	want := slackConfig{
		Token:          "xoxc-token",
		Cookies:        map[string]string{"d": "cookie"},
		DefaultChannel: testChannel,
		UserID:         "U1",
	}
	if err := saveConfig(path, want); err != nil {
		t.Fatalf("saveConfig returned an error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if permissions := info.Mode().Perm(); permissions != 0o600 {
		t.Fatalf("config permissions are %o, want 600", permissions)
	}

	got, exists, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig returned an error: %v", err)
	}
	if !exists || got.Token != want.Token || got.Cookies["d"] != "cookie" || got.UserID != "U1" || got.DefaultChannel != want.DefaultChannel {
		t.Fatalf("loadConfig returned %#v, %v; want %#v, true", got, exists, want)
	}
}

func TestSlackPathsUseStandaloneDirectory(t *testing.T) {
	t.Setenv("SLACK_CONFIG_FILE", "")
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err := slackConfigPath()
	want := filepath.Join(configDir, "slack-cli", "config.json")
	if err != nil || got != want {
		t.Fatalf("slackConfigPath returned %q, %v; want %q", got, err, want)
	}
	profile, err := slackBrowserProfileDir()
	want = filepath.Join(configDir, "slack-cli", "browser-profile")
	if err != nil || profile != want {
		t.Fatalf("slackBrowserProfileDir returned %q, %v; want %q", profile, err, want)
	}
	t.Setenv("SLACK_CONFIG_FILE", filepath.Join(t.TempDir(), "custom.json"))
	got, err = slackConfigPath()
	if err != nil || got != os.Getenv("SLACK_CONFIG_FILE") {
		t.Fatalf("slackConfigPath ignored SLACK_CONFIG_FILE: %q, %v", got, err)
	}
}
