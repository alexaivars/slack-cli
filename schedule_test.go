package main

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"testing"
	"time"
)

func TestScheduleDraftUsesBrowserRequest(t *testing.T) {
	at := time.Now().Add(time.Hour).Truncate(time.Second)
	client := newSlackClient("https://slack.test/api", "xoxc-test")
	client.cookies = map[string]string{"d": "fake-session"}
	client.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != "POST" || req.URL.String() != "https://example.slack.com/api/drafts.create" ||
			req.Header.Get("Cookie") != "d=fake-session" || req.Header.Get("Authorization") != "" {
			t.Fatal("draft request did not use the workspace and browser authentication")
		}
		if err := req.ParseMultipartForm(4096); err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]string{
			"token": "xoxc-test", "destinations": `[{"channel_id":"DFIXTURE"}]`,
			"date_scheduled": strconv.FormatInt(at.Unix(), 10), "file_ids": "[]", "is_from_composer": "true",
			"blocks": `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"*literal*"}]}]}]`,
		} {
			if got := req.FormValue(key); got != want {
				t.Errorf("%s = %q, want %q", key, got, want)
			}
		}
		if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(req.FormValue("client_msg_id")) {
			t.Error("client_msg_id is not a v4 UUID")
		}
		return jsonResponse(fmt.Sprintf(`{"ok":true,"draft":{"id":"DrFIXTURE","date_scheduled":%d}}`, at.Unix())), nil
	})
	id, err := client.scheduleDraft(t.Context(), authInfo{URL: "https://example.slack.com/"}, "DFIXTURE", "*literal*", at)
	if err != nil || id != "DrFIXTURE" {
		t.Fatalf("scheduleDraft returned %q, %v", id, err)
	}
}
