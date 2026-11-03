package main

import (
	"errors"
	"strings"
	"testing"
)

func TestTerminalOutputRemovesUntrustedControls(t *testing.T) {
	text := "hello \x1b[31mworld\x1b[0m\x1b]52;c;YXVkaXQ=\a\r"
	name := "Example\x1b[31m User\x1b[0m"
	messages := todayMessages{Messages: []slackMessage{{User: "U1", DisplayName: name, Text: text, TS: "1.0"}}}
	for _, tc := range []struct {
		name   string
		render func() string
	}{
		{"message lines", func() string { return formatMessageLines(messages) }},
		{"search results", func() string {
			return formatSearchResults([]searchResult{{DisplayName: name, Text: text, TS: "1.0"}})
		}},
		{"read view", func() string {
			return (readModel{messages: messages, width: 200, height: 40}).View().Content
		}},
		{"today view", func() string {
			return (todayModel{messages: messages, width: 200, height: 40}).View().Content
		}},
		{"read error", func() string {
			return (readModel{err: errors.New(text)}).View().Content
		}},
		{"interactive output", func() string {
			model := newInteractiveModel(nil, slackConfig{Team: name}, "C123")
			model.width, model.height = 200, 40
			updated, _ := model.Update(interactiveResultMsg{output: text})
			return updated.(interactiveModel).View().Content
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.render()
			if strings.Contains(got, "\x1b[31m") || strings.Contains(got, "\x1b]52;") || strings.ContainsAny(got, "\a\r") {
				t.Fatal("terminal output contains untrusted control characters")
			}
			if !strings.Contains(got, "hello world") {
				t.Fatalf("readable text was lost: %q", got)
			}
			if messages.Messages[0].Text != text || messages.Messages[0].DisplayName != name {
				t.Fatal("terminal rendering changed the original message data")
			}
		})
	}
}
