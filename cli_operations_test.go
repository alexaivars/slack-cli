package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestResolveChannelMatchesName(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/conversations.list" {
			t.Fatalf("path is %q", request.URL.Path)
		}
		return jsonResponse(`{"ok":true,"channels":[{"id":"C1","name":"test-cli","name_normalized":"test-cli"},{"id":"C2","name":"other"}]}`), nil
	})

	client := newSlackClient("https://slack.test/api", "xoxc-test")
	client.httpClient.Transport = transport
	got, err := client.ResolveChannel(t.Context(), "test-cli")
	if err != nil {
		t.Fatalf("ResolveChannel returned an error: %v", err)
	}
	if got.ID != "C1" || got.Name != "test-cli" {
		t.Fatalf("ResolveChannel returned %#v", got)
	}
}

func TestResolveWriteTargetOpensUserDM(t *testing.T) {
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
		case "/api/users.list":
			return jsonResponse(`{"ok":true,"members":[{"id":"U1","name":"casey","profile":{"display_name":"Casey"}}]}`), nil
		case "/api/conversations.open":
			if values.Get("users") != "U1" {
				t.Fatalf("users param is %q, want U1", values.Get("users"))
			}
			return jsonResponse(`{"ok":true,"channel":{"id":"D1","is_im":true}}`), nil
		default:
			t.Fatalf("path is %q", request.URL.Path)
		}
		return jsonResponse(`{"ok":false,"error":"unexpected_request"}`), nil
	})

	client := newSlackClient("https://slack.test/api", "xoxc-test")
	client.httpClient.Transport = transport
	got, err := client.ResolveWriteTarget(t.Context(), "@casey")
	if err != nil {
		t.Fatalf("ResolveWriteTarget returned an error: %v", err)
	}
	if got.ID != "D1" || !got.IsIM || got.Name != "Casey" {
		t.Fatalf("ResolveWriteTarget returned %#v", got)
	}
}

func TestReadMessagesResolvesUsersAndThreads(t *testing.T) {
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
			if values.Get("channel") != "C1" || values.Get("limit") != "2" {
				t.Fatalf("history params are %s", string(body))
			}
			return jsonResponse(`{"ok":true,"messages":[{"type":"message","user":"U2","text":"reply parent","ts":"2.0","reply_count":1},{"type":"message","user":"U1","text":"first <@U3>","ts":"1.0"}]}`), nil
		case "/api/conversations.replies":
			if values.Get("ts") != "2.0" {
				t.Fatalf("thread ts is %q", values.Get("ts"))
			}
			return jsonResponse(`{"ok":true,"messages":[{"type":"message","user":"U2","text":"reply parent","ts":"2.0"},{"type":"message","user":"U3","text":"thread reply","ts":"3.0","thread_ts":"2.0"}]}`), nil
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
	got, err := client.ReadMessages(t.Context(), "C1", readMessagesOptions{Limit: 2, IncludeThreads: true})
	if err != nil {
		t.Fatalf("ReadMessages returned an error: %v", err)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("message count is %d, want 3", len(got.Messages))
	}
	if got.Messages[0].DisplayName != "Casey" || got.Messages[1].DisplayName != "Sam" || got.Messages[2].DisplayName != "Lisa" {
		t.Fatalf("display names were not resolved: %#v", got.Messages)
	}
	if !got.Messages[2].IsThreadReply || got.Messages[2].ParentTS != "2.0" {
		t.Fatalf("thread reply was not marked: %#v", got.Messages[2])
	}
}

func TestReadThreadUsesExactPermalinkTimestamp(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/api/users.info" {
			return jsonResponse(`{"ok":true,"user":{"id":"U1","name":"tester","profile":{"display_name":"Tester"}}}`), nil
		}
		if request.URL.Path != "/api/conversations.replies" {
			t.Fatalf("path is %q", request.URL.Path)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if values.Get("channel") != "C234" || values.Get("ts") != "1700000000.123456" {
			t.Fatalf("thread request = %#v", values)
		}
		return jsonResponse(`{"ok":true,"messages":[{"type":"message","user":"U1","text":"parent","ts":"1700000000.000001"},{"type":"message","user":"U2","text":"reply","ts":"1700000000.123456","thread_ts":"1700000000.000001"}]}`), nil
	})
	client := newSlackClient("https://slack.test/api", "xoxc-test")
	client.httpClient.Transport = transport

	got, err := client.ReadThread(t.Context(), "C234", "1700000000.123456")
	if err != nil {
		t.Fatalf("ReadThread returned an error: %v", err)
	}
	if len(got.Messages) != 2 || got.Messages[0].IsThreadReply || !got.Messages[1].IsThreadReply || got.Messages[1].ParentTS != "1700000000.000001" {
		t.Fatalf("ReadThread returned %#v", got.Messages)
	}
}

func TestReplyMessagePostsThreadTS(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse request body: %v", err)
		}
		if request.URL.Path != "/api/chat.postMessage" {
			t.Fatalf("path is %q", request.URL.Path)
		}
		if values.Get("channel") != "C1" || values.Get("thread_ts") != "1.0" || values.Get("text") != "hello reply" {
			t.Fatalf("post params are %s", string(body))
		}
		return jsonResponse(`{"ok":true,"channel":"C1","ts":"2.0","message":{"type":"message","user":"U1","text":"hello reply","ts":"2.0","thread_ts":"1.0"}}`), nil
	})

	client := newSlackClient("https://slack.test/api", "xoxc-test")
	client.httpClient.Transport = transport
	got, err := client.ReplyMessage(t.Context(), "C1", "1.0", "hello reply")
	if err != nil {
		t.Fatalf("ReplyMessage returned an error: %v", err)
	}
	if got.Channel != "C1" || got.TS != "2.0" || !strings.Contains(got.Message.Text, "reply") {
		t.Fatalf("ReplyMessage returned %#v", got)
	}
}

func TestPostMessageSendsBlocks(t *testing.T) {
	blocks := json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_list","style":"bullet","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"first"}]}]}]}]`)
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse request body: %v", err)
		}
		if request.URL.Path != "/api/chat.postMessage" {
			t.Fatalf("path is %q", request.URL.Path)
		}
		if values.Get("channel") != "C1" || values.Get("text") != "fallback" || values.Get("blocks") != string(blocks) {
			t.Fatalf("post params are %s", string(body))
		}
		return jsonResponse(`{"ok":true,"channel":"C1","ts":"2.0","message":{"type":"message","user":"U1","text":"fallback","ts":"2.0"}}`), nil
	})

	client := newSlackClient("https://slack.test/api", "xoxc-test")
	client.httpClient.Transport = transport
	got, err := client.PostMessage(t.Context(), "C1", "fallback", blocks)
	if err != nil {
		t.Fatalf("PostMessage returned an error: %v", err)
	}
	if got.Channel != "C1" || got.TS != "2.0" {
		t.Fatalf("PostMessage returned %#v", got)
	}
}

func TestResolveUserPrefersUsernameAndRejectsAmbiguousNames(t *testing.T) {
	for _, tc := range []struct {
		name, secondPage, want string
	}{
		{"username on later page", `{"id":"U2","name":"example-user","profile":{"display_name":"Example User"}}`, "U2"},
		{"ambiguous display name", `{"id":"U2","name":"other","profile":{"display_name":"example-user"}}`, ""},
		{"unique display name", `{"id":"U2","name":"other","profile":{"display_name":"Other"}}`, "U1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newSlackClient("https://slack.test/api", "xoxb-test")
			client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path != "/api/users.list" {
					t.Fatalf("unexpected request: %s", request.URL.Path)
				}
				if err := request.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if request.Form.Get("cursor") == "" {
					return jsonResponse(`{"ok":true,"members":[{"id":"U1","name":"first","profile":{"display_name":"example-user"}}],"response_metadata":{"next_cursor":"next"}}`), nil
				}
				return jsonResponse(`{"ok":true,"members":[` + tc.secondPage + `]}`), nil
			})
			got, err := client.ResolveUser(t.Context(), "@example-user")
			if tc.want == "" {
				if err == nil || !strings.Contains(err.Error(), "ambiguous") {
					t.Fatalf("ambiguous recipient returned %q, %v", got.ID, err)
				}
			} else if err != nil || got.ID != tc.want {
				t.Fatalf("recipient = %q, %v; want %s", got.ID, err, tc.want)
			}
		})
	}
}
