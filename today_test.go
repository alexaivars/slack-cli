package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestLoadTodayMessagesUsesChannelAndDateBounds(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse request body: %v", err)
		}
		switch request.URL.Path {
		case "/api/conversations.history":
			if values.Get("channel") != "C123" {
				t.Fatalf("channel is %q", values.Get("channel"))
			}
			if values.Get("oldest") != "1781733600" || values.Get("latest") != "1781820000" {
				t.Fatalf("bounds are oldest=%s latest=%s", values.Get("oldest"), values.Get("latest"))
			}
			return jsonResponse(`{"ok":true,"messages":[{"type":"message","user":"U2","text":"second","ts":"2.0"},{"type":"message","user":"U1","text":"first","ts":"1.0"}]}`), nil
		case "/api/users.info":
			switch values.Get("user") {
			case "U1":
				return jsonResponse(`{"ok":true,"user":{"id":"U1","name":"casey","profile":{"display_name":"Casey"}}}`), nil
			case "U2":
				return jsonResponse(`{"ok":true,"user":{"id":"U2","name":"sam","profile":{"real_name":"Sam Example"}}}`), nil
			default:
				t.Fatalf("unexpected user lookup %q", values.Get("user"))
			}
		default:
			t.Fatalf("path is %q", request.URL.Path)
		}
		return jsonResponse(`{"ok":false,"error":"unexpected_request"}`), nil
	})

	client := newSlackClient("https://slack.test/api", "xoxc-test")
	client.httpClient.Transport = transport
	got, err := client.loadTodayMessages(t.Context(), todayOptions{
		channel:      testChannel,
		date:         "2026-06-18",
		countPerPage: 200,
	})
	if err != nil {
		t.Fatalf("loadTodayMessages returned an error: %v", err)
	}
	if len(got.Messages) != 2 || got.Messages[0].Text != "first" || got.Messages[1].Text != "second" {
		t.Fatalf("messages were not sorted chronologically: %#v", got.Messages)
	}
	if got.Messages[0].DisplayName != "Casey" || got.Messages[1].DisplayName != "Sam Example" {
		t.Fatalf("display names were not resolved: %#v", got.Messages)
	}
}

func TestLoadTodayMessagesExpandsThreadsAndResolvesMentionedUsers(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse request body: %v", err)
		}
		switch request.URL.Path {
		case "/api/conversations.history":
			return jsonResponse(`{"ok":true,"messages":[{"type":"message","user":"U1","text":"parent <@U3>","ts":"1781737200.000000","reply_count":1,"reply_users":["U2"]}]}`), nil
		case "/api/conversations.replies":
			if values.Get("ts") != "1781737200.000000" {
				t.Fatalf("thread ts is %q", values.Get("ts"))
			}
			return jsonResponse(`{"ok":true,"messages":[{"type":"message","user":"U1","text":"parent","ts":"1781737200.000000"},{"type":"message","user":"U2","text":"reply","ts":"1781737300.000000","thread_ts":"1781737200.000000"}]}`), nil
		case "/api/users.info":
			switch values.Get("user") {
			case "U1":
				return jsonResponse(`{"ok":true,"user":{"id":"U1","name":"casey","profile":{"display_name":"Casey"}}}`), nil
			case "U2":
				return jsonResponse(`{"ok":true,"user":{"id":"U2","name":"sam","profile":{"display_name":"Sam"}}}`), nil
			case "U3":
				return jsonResponse(`{"ok":true,"user":{"id":"U3","name":"lisa","profile":{"display_name":"Lisa"}}}`), nil
			default:
				t.Fatalf("unexpected user lookup %q", values.Get("user"))
			}
		default:
			t.Fatalf("path is %q", request.URL.Path)
		}
		return jsonResponse(`{"ok":false,"error":"unexpected_request"}`), nil
	})

	client := newSlackClient("https://slack.test/api", "xoxc-test")
	client.httpClient.Transport = transport
	got, err := client.loadTodayMessages(t.Context(), todayOptions{
		channel:      testChannel,
		date:         "2026-06-18",
		countPerPage: 200,
	})
	if err != nil {
		t.Fatalf("loadTodayMessages returned an error: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("message count is %d, want parent plus reply", len(got.Messages))
	}
	if !got.Messages[1].IsThreadReply || got.Messages[1].ParentTS != "1781737200.000000" {
		t.Fatalf("reply was not marked as thread reply: %#v", got.Messages[1])
	}
	if got.Messages[0].DisplayName != "Casey" || got.Messages[1].DisplayName != "Sam" || got.Users["U3"] != "Lisa" {
		t.Fatalf("users were not resolved: messages=%#v users=%#v", got.Messages, got.Users)
	}
}

func TestLoadTodayMessagesResolvesUsersFromEdgeList(t *testing.T) {
	edgeCalls := 0
	userInfoCalls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		switch request.URL.Path {
		case "/api/conversations.history":
			values, err := url.ParseQuery(string(body))
			if err != nil {
				t.Fatalf("parse request body: %v", err)
			}
			if values.Get("channel") != "C345" {
				t.Fatalf("channel is %q", values.Get("channel"))
			}
			return jsonResponse(`{"ok":true,"messages":[{"type":"message","user":"U1","text":"hello <@U2> <@U3>","ts":"1781737200.000000"}]}`), nil
		case "/cache/T123/users/list":
			edgeCalls++
			if request.Header.Get("Content-Type") != "text/plain;charset=UTF-8" {
				t.Fatalf("edge content type is %q", request.Header.Get("Content-Type"))
			}
			var params struct {
				Token    string   `json:"token"`
				Channels []string `json:"channels"`
				Count    int      `json:"count"`
			}
			if err := json.Unmarshal(body, &params); err != nil {
				t.Fatalf("decode edge request: %v", err)
			}
			if params.Token != "xoxc-test" || len(params.Channels) != 1 || params.Channels[0] != "C345" {
				t.Fatalf("edge params are %#v", params)
			}
			return jsonResponse(`{"ok":true,"results":[{"id":"U1","name":"casey","profile":{"display_name_normalized":"casey"}},{"id":"U2","name":"jordan","profile":{"real_name_normalized":"Jordan"}},{"id":"U3","name":"bot","real_name":"Build Bot"}]}`), nil
		case "/api/users.info":
			userInfoCalls++
			t.Fatalf("users.info should not be called when edge list covers all IDs")
		default:
			t.Fatalf("path is %q", request.URL.Path)
		}
		return jsonResponse(`{"ok":false,"error":"unexpected_request"}`), nil
	})

	client := newSlackClient("https://slack.test/api", "xoxc-test")
	client.teamID = "T123"
	client.edgeBaseURL = "https://slack.test/cache"
	client.httpClient.Transport = transport
	got, err := client.loadTodayMessages(t.Context(), todayOptions{
		channel:      "C345",
		date:         "2026-06-18",
		countPerPage: 200,
	})
	if err != nil {
		t.Fatalf("loadTodayMessages returned an error: %v", err)
	}
	if edgeCalls != 1 || userInfoCalls != 0 {
		t.Fatalf("edgeCalls=%d userInfoCalls=%d", edgeCalls, userInfoCalls)
	}
	if got.Messages[0].DisplayName != "casey" || got.Users["U2"] != "Jordan" || got.Users["U3"] != "Build Bot" {
		t.Fatalf("users were not resolved from edge list: messages=%#v users=%#v", got.Messages, got.Users)
	}
}

func TestMessageTextNormalizesHTML(t *testing.T) {
	got := messageText(slackMessage{Text: " hello &lt;world&gt; \r\n"})
	if got != "hello <world>" {
		t.Fatalf("messageText returned %q", got)
	}
}

func TestMessageSenderPrefersDisplayName(t *testing.T) {
	got := messageSender(slackMessage{DisplayName: "Casey", Username: "caseybot", User: "U1"})
	if got != "Casey" {
		t.Fatalf("messageSender returned %q", got)
	}
}

func TestWrapText(t *testing.T) {
	got := wrapText("one two three four five six", 20)
	if !strings.Contains(got, "\n") {
		t.Fatalf("wrapText returned %q, want wrapped text", got)
	}
}

func TestRenderMessageDetailSplitsSlackPseudoLines(t *testing.T) {
	message := slackMessage{
		User:        "U1",
		DisplayName: "Casey",
		Text:        "> :memo: _Daily summary > _Today’s work: > • Updated the example page > • Highlighted important answers > *Sent using* <@U1>",
	}
	got := renderMessageDetail(message, todayMessages{
		Users:    map[string]string{"U1": "Casey"},
		Messages: []slackMessage{message},
	}, 80)
	for _, want := range []string{
		":memo: _Daily summary",
		"_Today’s work:",
		"• Updated the example page",
		"• Highlighted important answers",
		"*Sent using* @Casey",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("detail missing %q:\n%s", want, got)
		}
	}
}

func TestLoadTodayMessagesRequiresChannel(t *testing.T) {
	client := newSlackClient("https://slack.test/api", "xoxb-test")
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		t.Fatal("missing channel must be rejected before any Slack request")
		return nil, nil
	})
	_, err := client.loadTodayMessages(t.Context(), todayOptions{date: "2026-06-18"})
	if err == nil || !strings.Contains(err.Error(), "channel is required") {
		t.Fatalf("missing channel returned %v, want channel-required error", err)
	}
}
