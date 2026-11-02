package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testChannel = "C123"

func TestParseAPIOptions(t *testing.T) {
	got, err := parseOptions([]string{"api", "conversations.history", "--param", "limit=10", "--raw"})
	if err != nil {
		t.Fatalf("parseOptions returned an error: %v", err)
	}
	if got.command != "api" || got.method != "conversations.history" || !got.raw {
		t.Fatalf("parseOptions returned %#v", got)
	}
	if got.params["limit"] != "10" {
		t.Fatalf("limit param is %#v, want unmodified string", got.params["limit"])
	}
}

func TestParseAPIOptionsPreservesSlackTimestamp(t *testing.T) {
	got, err := parseOptions([]string{"api", "conversations.replies", "--param", "ts=1700000000.123456"})
	if err != nil {
		t.Fatalf("parseOptions returned an error: %v", err)
	}
	if got.params["ts"] != "1700000000.123456" {
		t.Fatalf("ts param is %#v, want exact timestamp", got.params["ts"])
	}
}

func TestParseThreadPermalink(t *testing.T) {
	got, err := parseOptions([]string{"thread", "https://example.slack.com/archives/C234/p1700000000123456", "--json"})
	if err != nil {
		t.Fatalf("parseOptions returned an error: %v", err)
	}
	if got.channel != "C234" || got.threadTS != "1700000000.123456" || !got.json {
		t.Fatalf("parseOptions returned %#v", got)
	}
}

func TestParseOptionsRejectsUnexpectedArgs(t *testing.T) {
	if _, err := parseOptions([]string{"api", "auth.test", "extra"}); err == nil {
		t.Fatal("parseOptions accepted unexpected positional argument")
	}
}

func TestParseTodayOptions(t *testing.T) {
	got, err := parseOptions([]string{"today", "--channel", "C123", "--date", "2026-06-18", "--json"})
	if err != nil {
		t.Fatalf("parseOptions returned an error: %v", err)
	}
	if got.command != "today" || got.channel != "C123" || got.date != "2026-06-18" || !got.json {
		t.Fatalf("parseOptions returned %#v", got)
	}
}

func TestParseFilesDownload(t *testing.T) {
	got, err := parseOptions([]string{"files", "download", "F123", "/tmp/screenshot.png", "--overwrite"})
	if err != nil {
		t.Fatalf("parseOptions returned an error: %v", err)
	}
	if got.command != "files" || got.subcommand != "download" || got.file != "F123" || got.output != "/tmp/screenshot.png" || !got.overwrite {
		t.Fatalf("parseOptions returned %#v", got)
	}
}

func TestParseReadOptionsAllowsFlagsAfterChannel(t *testing.T) {
	got, err := parseOptions([]string{"read", "#test-cli", "--limit", "10", "--threads", "--json"})
	if err != nil {
		t.Fatalf("parseOptions returned an error: %v", err)
	}
	if got.command != "read" || got.channel != "#test-cli" || got.countPerPage != 10 || !got.threads || !got.json {
		t.Fatalf("parseOptions returned %#v", got)
	}
}

func TestParseInteractiveOptions(t *testing.T) {
	got, err := parseOptions([]string{"interactive", "--channel", "test-cli"})
	if err != nil {
		t.Fatalf("parseOptions returned an error: %v", err)
	}
	if got.command != "interactive" || got.channel != "test-cli" {
		t.Fatalf("parseOptions returned %#v", got)
	}
}

func TestParseWriteOptionsTreatsBareWordsAsText(t *testing.T) {
	got, err := parseOptions([]string{"write", "test", "again"})
	if err != nil {
		t.Fatalf("parseOptions returned an error: %v", err)
	}
	if got.command != "write" || got.channel != "" || got.text != "test again" {
		t.Fatalf("parseOptions returned %#v", got)
	}
}

func TestParseWriteOptionsAllowsExplicitChannelTarget(t *testing.T) {
	got, err := parseOptions([]string{"write", "#test-cli", "hello", "world"})
	if err != nil {
		t.Fatalf("parseOptions returned an error: %v", err)
	}
	if got.command != "write" || got.channel != "#test-cli" || got.text != "hello world" {
		t.Fatalf("parseOptions returned %#v", got)
	}
}

func TestParseWriteOptionsAllowsExplicitUserTarget(t *testing.T) {
	got, err := parseOptions([]string{"write", "@casey", "hello", "direct"})
	if err != nil {
		t.Fatalf("parseOptions returned an error: %v", err)
	}
	if got.command != "write" || got.channel != "@casey" || got.text != "hello direct" {
		t.Fatalf("parseOptions returned %#v", got)
	}
}

func TestParseWriteOptionsReadsBlocksFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocks.json")
	blocks := `[{"type":"rich_text","elements":[{"type":"rich_text_list","style":"bullet","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"first"}]}]}]}]`
	if err := os.WriteFile(path, []byte(blocks), 0o600); err != nil {
		t.Fatalf("write blocks file: %v", err)
	}
	got, err := parseOptions([]string{"write", "#test-cli", "fallback text", "--blocks-file", path})
	if err != nil {
		t.Fatalf("parseOptions returned an error: %v", err)
	}
	if got.command != "write" || got.channel != "#test-cli" || got.text != "fallback text" || string(got.blocks) != blocks {
		t.Fatalf("parseOptions returned %#v", got)
	}
}

func TestParseWriteOptionsMarkdownFileBuildsBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "message.md")
	message := "some text\n\n**BOLD TEXT**\n\nmy list\n- 1\n- 2\n- 3\n"
	if err := os.WriteFile(path, []byte(message), 0o600); err != nil {
		t.Fatalf("write message file: %v", err)
	}
	got, err := parseOptions([]string{"write", "#test-cli", "--format", "markdown", "--file", path})
	if err != nil {
		t.Fatalf("parseOptions returned an error: %v", err)
	}
	if got.command != "write" || got.channel != "#test-cli" || got.text == "" || len(got.blocks) == 0 {
		t.Fatalf("parseOptions returned %#v", got)
	}
	if !strings.Contains(string(got.blocks), `"type":"rich_text_list"`) || !strings.Contains(string(got.blocks), `"style":"bullet"`) {
		t.Fatalf("blocks do not contain a native bullet list: %s", string(got.blocks))
	}
	if !strings.Contains(string(got.blocks), `"bold":true`) {
		t.Fatalf("blocks do not contain bold rich text: %s", string(got.blocks))
	}
}

func TestParseReplyOptions(t *testing.T) {
	got, err := parseOptions([]string{"reply", "test-cli", "1782060325.263859", "looks", "good"})
	if err != nil {
		t.Fatalf("parseOptions returned an error: %v", err)
	}
	if got.command != "reply" || got.channel != "test-cli" || got.threadTS != "1782060325.263859" || got.text != "looks good" {
		t.Fatalf("parseOptions returned %#v", got)
	}
}

func TestParseSearchOptionsAllowsFlagsAfterQuery(t *testing.T) {
	got, err := parseOptions([]string{"search", "deploy", "failed", "--channel", "test-cli", "--limit", "5", "--json"})
	if err != nil {
		t.Fatalf("parseOptions returned an error: %v", err)
	}
	if got.command != "search" || got.query != "deploy failed" || got.channel != "test-cli" || got.countPerPage != 5 || !got.json {
		t.Fatalf("parseOptions returned %#v", got)
	}
}

func TestParseArchiveAllowsYesAfterChannel(t *testing.T) {
	got, err := parseOptions([]string{"archive", "slack-cli-test", "--yes"})
	if err != nil {
		t.Fatalf("parseOptions returned an error: %v", err)
	}
	if got.command != "archive" || got.channel != "slack-cli-test" || !got.yes {
		t.Fatalf("parseOptions returned %#v", got)
	}
}

func TestApplyDefaultChannel(t *testing.T) {
	params := map[string]any{}
	applyDefaultChannel("conversations.history", params, slackConfig{DefaultChannel: testChannel})
	if params["channel"] != testChannel {
		t.Fatalf("channel param is %#v, want default", params["channel"])
	}
}

func TestLoadRequiredConfigUsesOnlyConfiguredChannel(t *testing.T) {
	for _, tc := range []struct {
		name, saved, env, want string
	}{
		{name: "no implicit channel"},
		{name: "saved channel", saved: "C123", want: "C123"},
		{name: "environment overrides saved channel", saved: "C123", env: "C234", want: "C234"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SLACK_TOKEN", "xoxb-test")
			t.Setenv("SLACK_CHANNEL", tc.env)
			path := filepath.Join(t.TempDir(), "config.json")
			if err := saveConfig(path, slackConfig{DefaultChannel: tc.saved}); err != nil {
				t.Fatal(err)
			}
			got, err := loadRequiredConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if got.DefaultChannel != tc.want {
				t.Fatalf("default channel is %q, want %q", got.DefaultChannel, tc.want)
			}
		})
	}
}
