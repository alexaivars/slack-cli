package main

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestInteractiveModelConfiguresCommandSuggestions(t *testing.T) {
	model := newInteractiveModel(nil, slackConfig{}, testChannel)
	if !model.input.Focused() {
		t.Fatal("interactive input is not focused")
	}
	suggestions := model.input.AvailableSuggestions()
	if len(suggestions) == 0 {
		t.Fatal("interactive model has no suggestions")
	}
	for _, want := range []string{":help", ":read ", ":write ", ":quit"} {
		found := false
		for _, got := range suggestions {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("suggestions %#v did not include %q", suggestions, want)
		}
	}
}

func TestInteractiveUpPrefersSuggestionsForPartialCommand(t *testing.T) {
	model := newInteractiveModel(nil, slackConfig{}, testChannel)
	model.Init()
	model.history = []string{":status"}
	model = typeInteractiveInput(t, model, ":h")
	if model.shouldUseHistoryNavigation() {
		t.Fatal("partial command with suggestions should not use history navigation")
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	model = updated.(interactiveModel)
	if got := model.input.Value(); got != ":h" {
		t.Fatalf("up changed partial command to %q, want :h", got)
	}
	if model.historyIndex != -1 {
		t.Fatalf("history index is %d, want -1", model.historyIndex)
	}
}

func TestInteractiveUpUsesHistoryForEmptyInput(t *testing.T) {
	model := newInteractiveModel(nil, slackConfig{}, testChannel)
	model.history = []string{":status"}
	if !model.shouldUseHistoryNavigation() {
		t.Fatal("empty input should use history navigation")
	}
	updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	model = updated.(interactiveModel)
	if got := model.input.Value(); got != ":status" {
		t.Fatalf("up changed input to %q, want :status", got)
	}
}

func typeInteractiveInput(t *testing.T, model interactiveModel, value string) interactiveModel {
	t.Helper()
	for _, char := range value {
		updated, _ := model.Update(tea.KeyPressMsg(tea.Key{Text: string(char), Code: char}))
		model = updated.(interactiveModel)
	}
	return model
}

func TestShellFieldsHandlesQuotesAndEscapes(t *testing.T) {
	got, err := shellFields(`write test-cli "hello world" escaped\ value`)
	if err != nil {
		t.Fatalf("shellFields returned an error: %v", err)
	}
	want := []string{"write", "test-cli", "hello world", "escaped value"}
	if len(got) != len(want) {
		t.Fatalf("shellFields returned %#v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("field %d is %q, want %q; all fields %#v", index, got[index], want[index], got)
		}
	}
}

func TestShellFieldsRejectsUnterminatedQuote(t *testing.T) {
	if _, err := shellFields(`write "missing end`); err == nil {
		t.Fatal("shellFields accepted unterminated quote")
	}
}

func TestInteractiveCommandFieldsRequiresColonPrefix(t *testing.T) {
	if _, err := interactiveCommandFields(`write test-cli hello`); err == nil {
		t.Fatal("interactiveCommandFields accepted a command without colon prefix")
	}
}

func TestInteractiveCommandFieldsStripsColonPrefix(t *testing.T) {
	got, err := interactiveCommandFields(`:write test-cli "hello :smile:"`)
	if err != nil {
		t.Fatalf("interactiveCommandFields returned an error: %v", err)
	}
	want := []string{"write", "test-cli", "hello :smile:"}
	if len(got) != len(want) {
		t.Fatalf("interactiveCommandFields returned %#v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("field %d is %q, want %q; all fields %#v", index, got[index], want[index], got)
		}
	}
}

func TestExecuteInteractiveReadAcceptsHashChannel(t *testing.T) {
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
		case "/api/conversations.list":
			return jsonResponse(`{"ok":true,"channels":[{"id":"C1","name":"general","name_normalized":"general"}]}`), nil
		case "/api/conversations.history":
			if values.Get("channel") != "C1" || values.Get("limit") != "1" {
				t.Fatalf("history params are %s", string(body))
			}
			return jsonResponse(`{"ok":true,"messages":[{"type":"message","text":"hello general","ts":"1.0"}]}`), nil
		default:
			t.Fatalf("path is %q", request.URL.Path)
		}
		return jsonResponse(`{"ok":false,"error":"unexpected_request"}`), nil
	})

	client := newSlackClient("https://slack.test/api", "xoxc-test")
	client.httpClient.Transport = transport
	output, _, err := executeInteractiveLine(t.Context(), client, slackConfig{}, testChannel, ":read #general --limit 1")
	if err != nil {
		t.Fatalf("executeInteractiveLine returned an error: %v", err)
	}
	if !strings.Contains(output, "hello general") {
		t.Fatalf("output is %q", output)
	}
}

func TestExecuteInteractiveWriteUsesDefaultChannelForBareText(t *testing.T) {
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
		case "/api/conversations.info":
			if values.Get("channel") != testChannel {
				t.Fatalf("conversation channel is %q, want %s", values.Get("channel"), testChannel)
			}
			return jsonResponse(`{"ok":true,"channel":{"id":"` + testChannel + `","name":"test-cli"}}`), nil
		case "/api/chat.postMessage":
			if values.Get("channel") != testChannel || values.Get("text") != "test again" {
				t.Fatalf("post params are %s", string(body))
			}
			return jsonResponse(`{"ok":true,"channel":"` + testChannel + `","ts":"1.0","message":{"type":"message","user":"U1","text":"test again","ts":"1.0"}}`), nil
		default:
			t.Fatalf("path is %q", request.URL.Path)
		}
		return jsonResponse(`{"ok":false,"error":"unexpected_request"}`), nil
	})

	client := newSlackClient("https://slack.test/api", "xoxc-test")
	client.httpClient.Transport = transport
	output, _, err := executeInteractiveLine(t.Context(), client, slackConfig{}, testChannel, ":write test again")
	if err != nil {
		t.Fatalf("executeInteractiveLine returned an error: %v", err)
	}
	if !strings.Contains(output, "Posted message to #test-cli") {
		t.Fatalf("output is %q", output)
	}
}

func TestInteractiveUpdateRunsHelpOnEnter(t *testing.T) {
	model := newInteractiveModel(nil, slackConfig{}, testChannel)
	model.input.SetValue(":help")
	updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = updated.(interactiveModel)
	if cmd == nil {
		t.Fatal("Update did not return a command for :help")
	}
	message := cmd()
	updated, _ = model.Update(message)
	model = updated.(interactiveModel)
	if model.running {
		t.Fatal("model is still running after help command result")
	}
	joined := strings.Join(model.output, "\n")
	if !strings.Contains(joined, "Commands:") {
		t.Fatalf("output did not include help text:\n%s", joined)
	}
}

func TestInteractiveUpdateRunsHelpOnReturn(t *testing.T) {
	model := newInteractiveModel(nil, slackConfig{}, testChannel)
	model.input.SetValue(":help")
	updated, cmd := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyReturn}))
	model = updated.(interactiveModel)
	if cmd == nil {
		t.Fatal("Update did not return a command for :help")
	}
	message := cmd()
	updated, _ = model.Update(message)
	model = updated.(interactiveModel)
	if !strings.Contains(strings.Join(model.output, "\n"), "Commands:") {
		t.Fatalf("output did not include help text: %#v", model.output)
	}
}
