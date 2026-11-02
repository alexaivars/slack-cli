package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportMonthPostsExcludesOtherAuthors(t *testing.T) {
	client := newSlackClient("https://slack.test/api", "xoxb-test")
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/search.messages" {
			t.Fatalf("unexpected request path: %s", request.URL.Path)
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := request.Form.Get("query"); got != "from:<@U1> after:2026-06-01 before:2026-07-01" {
			t.Fatalf("search query = %q, want own posts in June", got)
		}
		return jsonResponse(`{"ok":true,"messages":{"total":2,"matches":[{"user":"U1","ts":"1.0","text":"own message","channel":{"id":"C123"}},{"user":"U2","ts":"2.0","text":"context from another author","channel":{"id":"C123"}}]}}`), nil
	})
	output := filepath.Join(t.TempDir(), "posts.json")
	exported, _, _, err := exportMonthPosts(t.Context(), client, "U1", commandOptions{
		month: "2026-06", output: output, json: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Messages []postMessage `json:"messages"`
	}
	if err := json.Unmarshal(contents, &saved); err != nil {
		t.Fatal(err)
	}
	if exported != 1 || len(saved.Messages) != 1 || saved.Messages[0].User != "U1" || saved.Messages[0].Text != "own message" {
		t.Fatalf("exported %d posts; saved messages = %#v, want only own message", exported, saved.Messages)
	}
}

func TestMonthBounds(t *testing.T) {
	after, before, err := monthBounds("2026-12")
	if err != nil {
		t.Fatalf("monthBounds returned an error: %v", err)
	}
	if after != "2026-12-01" || before != "2027-01-01" {
		t.Fatalf("bounds are %s %s, want December to January", after, before)
	}
}

func TestRenderPostsLogNormalizesAndSorts(t *testing.T) {
	matches := []postMessage{
		{TS: "200.000", Text: "second"},
		{TS: "100.000", Text: "&lt;first&gt;"},
	}
	matches[0].Channel.Name = "general"
	matches[1].Channel.ID = "C1"

	got := renderPostsLog("2026-06", "2026-06-01", "2026-07-01", "query", 2, matches)
	first := strings.Index(got, "<first>")
	second := strings.Index(got, "second")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("log did not normalize and sort messages:\n%s", got)
	}
}
