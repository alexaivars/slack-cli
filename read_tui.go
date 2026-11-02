package main

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type readLoadedMsg struct {
	messages todayMessages
	err      error
}

type readModel struct {
	client   *slackClient
	channel  conversation
	limit    int
	threads  bool
	messages todayMessages
	cursor   int
	width    int
	height   int
	loading  bool
	err      error
}

func newReadModel(client *slackClient, channel conversation, limit int, threads bool) readModel {
	return readModel{
		client:  client,
		channel: channel,
		limit:   limit,
		threads: threads,
		loading: true,
	}
}

func (m readModel) Init() tea.Cmd {
	return m.loadMessages()
}

func (m readModel) loadMessages() tea.Cmd {
	return func() tea.Msg {
		messages, err := m.client.ReadMessages(context.Background(), m.channel.ID, readMessagesOptions{
			Limit:          m.limit,
			IncludeThreads: m.threads,
		})
		return readLoadedMsg{messages: messages, err: err}
	}
}

func (m readModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
	case readLoadedMsg:
		m.loading = false
		m.err = message.err
		if message.err == nil {
			m.messages = message.messages
			if m.cursor >= len(m.messages.Messages) {
				m.cursor = max(0, len(m.messages.Messages)-1)
			}
		}
	case tea.KeyPressMsg:
		switch message.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "up", "k":
			if !m.loading && m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if !m.loading && m.cursor < len(m.messages.Messages)-1 {
				m.cursor++
			}
		case "home", "g":
			if !m.loading {
				m.cursor = 0
			}
		case "end", "G":
			if !m.loading && len(m.messages.Messages) > 0 {
				m.cursor = len(m.messages.Messages) - 1
			}
		case "r":
			if !m.loading {
				m.loading = true
				m.err = nil
				return m, m.loadMessages()
			}
		}
	}
	return m, nil
}

func (m readModel) View() tea.View {
	width := max(40, m.width)
	var content strings.Builder
	content.WriteString("Slack Read\n")
	content.WriteString(strings.Repeat("-", min(width, 88)))
	content.WriteString("\n\n")

	if m.loading {
		content.WriteString("Loading channel messages...\n")
	} else if m.err != nil {
		content.WriteString("Could not load Slack messages:\n")
		content.WriteString(m.err.Error())
		content.WriteString("\n\nPress r to retry.")
	} else {
		content.WriteString(fmt.Sprintf("%s (%d messages)\n\n", conversationLabel(m.channel), len(m.messages.Messages)))
		if len(m.messages.Messages) == 0 {
			content.WriteString("No messages.\n")
		} else {
			listRows := max(3, (m.height-12)*2/3)
			start := max(0, m.cursor-listRows+1)
			end := min(len(m.messages.Messages), start+listRows)
			for index := start; index < end; index++ {
				prefix := "  "
				if index == m.cursor {
					prefix = "> "
				}
				message := m.messages.Messages[index]
				indent := ""
				if message.IsThreadReply {
					indent = "  "
				}
				line := fmt.Sprintf("%s%s%s %-12s %s", prefix, indent, shortMessageTime(message.TS), messageSender(message), oneLineMessage(message))
				content.WriteString(truncateText(line, width))
				content.WriteByte('\n')
			}
			selected := m.messages.Messages[m.cursor]
			content.WriteString("\n")
			content.WriteString(strings.Repeat("-", min(width, 88)))
			content.WriteString("\n")
			content.WriteString(fmt.Sprintf("%s  %s\n", shortMessageTime(selected.TS), messageSender(selected)))
			detail := renderMessageDetail(selected, m.messages, width)
			if detail == "" {
				detail = "(no text)"
			}
			content.WriteString(detail)
			content.WriteByte('\n')
		}
	}

	content.WriteString("\nup/down or j/k navigate - r refresh - q quit")
	view := tea.NewView(content.String())
	view.AltScreen = true
	return view
}
