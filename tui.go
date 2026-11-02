package main

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

type todayLoadedMsg struct {
	messages todayMessages
	err      error
}

type todayModel struct {
	client       *slackClient
	channel      string
	date         string
	countPerPage int
	messages     todayMessages
	cursor       int
	width        int
	height       int
	loading      bool
	err          error
}

func newTodayModel(client *slackClient, channel, date string, countPerPage int) todayModel {
	return todayModel{
		client:       client,
		channel:      channel,
		date:         date,
		countPerPage: countPerPage,
		loading:      true,
	}
}

func (m todayModel) Init() tea.Cmd {
	return m.loadMessages()
}

func (m todayModel) loadMessages() tea.Cmd {
	return func() tea.Msg {
		messages, err := m.client.loadTodayMessages(context.Background(), todayOptions{
			channel:      m.channel,
			date:         m.date,
			countPerPage: m.countPerPage,
		})
		return todayLoadedMsg{messages: messages, err: err}
	}
}

func (m todayModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
	case todayLoadedMsg:
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

func (m todayModel) View() tea.View {
	width := max(40, m.width)
	var content strings.Builder
	content.WriteString("Slack Today\n")
	content.WriteString(strings.Repeat("-", min(width, 88)))
	content.WriteString("\n\n")

	if m.loading {
		content.WriteString("Loading channel messages...\n")
	} else if m.err != nil {
		content.WriteString("Could not load Slack messages:\n")
		content.WriteString(m.err.Error())
		content.WriteString("\n\nPress r to retry.")
	} else {
		content.WriteString(fmt.Sprintf("Channel %s / %s (%d messages)\n\n", m.messages.Channel, m.messages.Date, len(m.messages.Messages)))
		if len(m.messages.Messages) == 0 {
			content.WriteString("No messages today.\n")
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
					indent = "  ↳ "
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

func oneLineMessage(message slackMessage) string {
	text := strings.ReplaceAll(normalizeSlackText(message.Text, nil), "\n", " ")
	if text == "" {
		return "(no text)"
	}
	return text
}

func shortMessageTime(ts string) string {
	seconds := slackTimestampSeconds(ts)
	if seconds == 0 {
		return ts
	}
	location, err := time.LoadLocation(stockholmTimeZone)
	if err != nil {
		location = time.Local
	}
	return time.Unix(seconds, 0).In(location).Format("15:04")
}

func truncateText(value string, width int) string {
	if width <= 3 || utf8.RuneCountInString(value) <= width {
		return value
	}
	characters := []rune(value)
	return string(characters[:width-3]) + "..."
}

func wrapText(value string, width int) string {
	width = max(20, width)
	words := strings.Fields(value)
	if len(words) == 0 {
		return ""
	}
	var lines []string
	line := words[0]
	for _, word := range words[1:] {
		if utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) > width {
			lines = append(lines, line)
			line = word
		} else {
			line += " " + word
		}
	}
	lines = append(lines, line)
	return strings.Join(lines, "\n")
}

func renderMessageDetail(message slackMessage, messages todayMessages, width int) string {
	names := messageNameMap(messages)
	text := normalizeSlackText(message.Text, names)
	text = splitSlackPseudoLines(text)
	return wrapPreservingLines(text, width)
}

func messageNameMap(messages todayMessages) map[string]string {
	names := map[string]string{}
	for userID, name := range messages.Users {
		if userID != "" && name != "" {
			names[userID] = name
		}
	}
	for _, message := range messages.Messages {
		if message.User != "" {
			names[message.User] = messageSender(message)
		}
	}
	return names
}

func normalizeSlackText(text string, names map[string]string) string {
	text = messageText(slackMessage{Text: text})
	text = strings.ReplaceAll(text, "\u00a0", " ")
	if names != nil {
		for userID, name := range names {
			if userID == "" || name == "" {
				continue
			}
			text = strings.ReplaceAll(text, "<@"+userID+">", "@"+name)
		}
	}
	return strings.TrimSpace(text)
}

func splitSlackPseudoLines(text string) string {
	text = strings.ReplaceAll(text, "\n>", "\n")
	text = strings.ReplaceAll(text, "\n >", "\n")
	text = strings.ReplaceAll(text, " > • ", "\n• ")
	text = strings.ReplaceAll(text, "> • ", "\n• ")
	text = strings.ReplaceAll(text, " > - ", "\n- ")
	text = strings.ReplaceAll(text, "> - ", "\n- ")
	text = strings.ReplaceAll(text, " > *", "\n*")
	text = strings.ReplaceAll(text, "> *", "\n*")
	text = strings.ReplaceAll(text, " > _", "\n_")
	text = strings.ReplaceAll(text, "> _", "\n_")
	text = strings.ReplaceAll(text, " > ", "\n")
	text = strings.TrimPrefix(text, "> ")
	return strings.TrimSpace(text)
}

func wrapPreservingLines(value string, width int) string {
	var wrapped []string
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			wrapped = append(wrapped, "")
			continue
		}
		prefix := ""
		body := line
		if strings.HasPrefix(line, "• ") || strings.HasPrefix(line, "- ") {
			prefix = line[:2]
			body = strings.TrimSpace(line[2:])
		}
		wrappedLine := wrapText(body, max(20, width-len(prefix)))
		if prefix != "" {
			parts := strings.Split(wrappedLine, "\n")
			for index, part := range parts {
				if index == 0 {
					parts[index] = prefix + part
				} else {
					parts[index] = "  " + part
				}
			}
			wrappedLine = strings.Join(parts, "\n")
		}
		wrapped = append(wrapped, wrappedLine)
	}
	return strings.Join(wrapped, "\n")
}
