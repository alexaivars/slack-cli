package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestCallAPIUsesBearerTokenAndCookies(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost {
			t.Fatalf("method is %s, want POST", request.Method)
		}
		if request.Header.Get("Authorization") != "Bearer xoxc-test" {
			t.Fatalf("Authorization header is %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("Cookie") != "a=one; d=cookie" {
			t.Fatalf("Cookie header is %q", request.Header.Get("Cookie"))
		}
		if request.URL.Path != "/api/auth.test" {
			t.Fatalf("path is %q, want /api/auth.test", request.URL.Path)
		}
		return jsonResponse(`{"ok":true,"url":"https://example.slack.com/","team":"Example","user":"Casey","team_id":"T1","user_id":"U1"}`), nil
	})

	client := newSlackClient("https://slack.test/api", "xoxc-test")
	client.cookies = map[string]string{"d": "cookie", "a": "one"}
	client.httpClient.Transport = transport
	got, err := client.authTest(t.Context())
	if err != nil {
		t.Fatalf("authTest returned an error: %v", err)
	}
	if got.TeamID != "T1" || got.UserID != "U1" {
		t.Fatalf("authTest returned %#v", got)
	}
}

func TestSlackAPIErrorIncludesNeededScope(t *testing.T) {
	payload := apiPayload{Error: "missing_scope", Needed: "search:read", Provided: "channels:read"}
	err := slackAPIError(payload)
	if !strings.Contains(err.Error(), "needed: search:read") {
		t.Fatalf("error is %v, want needed scope", err)
	}
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
