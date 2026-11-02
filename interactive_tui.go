package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type interactiveResultMsg struct {
	line           string
	output         string
	err            error
	defaultChannel string
}

type interactiveModel struct {
	client         *slackClient
	config         slackConfig
	defaultChannel string
	input          textinput.Model
	output         []string
	history        []string
	historyIndex   int
	width          int
	height         int
	running        bool
}

func newInteractiveModel(client *slackClient, config slackConfig, defaultChannel string) interactiveModel {
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "type :help"
	input.ShowSuggestions = true
	input.SetSuggestions(interactiveCommandSuggestions())
	input.SetWidth(80)
	input.Focus()
	return interactiveModel{
		client:         client,
		config:         config,
		defaultChannel: defaultChannel,
		input:          input,
		output: []string{
			"slack-cli interactive",
			"Commands start with :. Type :help for commands. Type :quit to exit.",
		},
		historyIndex: -1,
	}
}

func (m interactiveModel) Init() tea.Cmd {
	return m.input.Focus()
}

func (m interactiveModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
		m.input.SetWidth(max(20, message.Width-2))
	case interactiveResultMsg:
		m.running = false
		if message.defaultChannel != "" {
			m.defaultChannel = message.defaultChannel
		}
		if message.err != nil {
			m.output = appendOutput(m.output, "Error: "+message.err.Error())
		} else if strings.TrimSpace(message.output) != "" {
			m.output = appendOutput(m.output, strings.TrimRight(message.output, "\n"))
		}
	case tea.KeyPressMsg:
		if m.running {
			switch message.String() {
			case "ctrl+c":
				return m, tea.Quit
			}
			return m, nil
		}
		switch {
		case message.String() == "ctrl+c":
			return m, tea.Quit
		case isEnterKey(message):
			line := strings.TrimSpace(m.input.Value())
			m.input.SetValue("")
			m.historyIndex = -1
			if line == "" {
				return m, nil
			}
			if line == ":quit" || line == ":exit" || line == ":q" {
				return m, tea.Quit
			}
			if line == ":clear" {
				m.output = nil
				return m, nil
			}
			if !strings.HasPrefix(line, ":") {
				m.output = appendOutput(m.output, "Commands start with :. Use :write to send messages.")
				return m, nil
			}
			m.history = append(m.history, line)
			m.output = appendOutput(m.output, "> "+line)
			m.running = true
			return m, m.runLine(line)
		case message.String() == "ctrl+u":
			m.input.SetValue("")
		case message.String() == "up" && m.shouldUseHistoryNavigation():
			if len(m.history) > 0 {
				if m.historyIndex == -1 {
					m.historyIndex = len(m.history) - 1
				} else if m.historyIndex > 0 {
					m.historyIndex--
				}
				m.input.SetValue(m.history[m.historyIndex])
				m.input.CursorEnd()
			}
		case message.String() == "down" && m.shouldUseHistoryNavigation():
			if m.historyIndex >= 0 {
				m.historyIndex++
				if m.historyIndex >= len(m.history) {
					m.historyIndex = -1
					m.input.SetValue("")
				} else {
					m.input.SetValue(m.history[m.historyIndex])
					m.input.CursorEnd()
				}
			}
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(message)
			return m, cmd
		}
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(message)
		return m, cmd
	}
	return m, nil
}

func (m interactiveModel) shouldUseHistoryNavigation() bool {
	return strings.TrimSpace(m.input.Value()) == "" || len(m.input.MatchedSuggestions()) == 0
}

func (m interactiveModel) runLine(line string) tea.Cmd {
	return func() tea.Msg {
		output, defaultChannel, err := executeInteractiveLine(context.Background(), m.client, m.config, m.defaultChannel, line)
		return interactiveResultMsg{line: line, output: output, err: err, defaultChannel: defaultChannel}
	}
}

func (m interactiveModel) View() tea.View {
	width := max(50, m.width)
	height := max(12, m.height)
	header := fmt.Sprintf("Workspace %s (%s) / user %s (%s) / default %s",
		firstNonEmpty(m.config.Team, "-"),
		firstNonEmpty(m.config.TeamID, "-"),
		firstNonEmpty(m.config.User, "-"),
		firstNonEmpty(m.config.UserID, "-"),
		m.defaultChannel,
	)
	var lines []string
	lines = append(lines, header)
	lines = append(lines, strings.Repeat("-", min(width, 100)))
	for _, block := range m.output {
		for _, line := range strings.Split(block, "\n") {
			lines = append(lines, truncateText(line, width))
		}
	}
	available := max(1, height-4)
	if len(lines) > available {
		lines = lines[len(lines)-available:]
	}
	prompt := m.input.View()
	if m.running {
		prompt = "... running"
	}
	lines = append(lines, strings.Repeat("-", min(width, 100)))
	lines = append(lines, prompt)
	return tea.NewView(strings.Join(lines, "\n"))
}

func isEnterKey(message tea.KeyPressMsg) bool {
	key := message.Key()
	return key.Code == tea.KeyEnter || key.Code == tea.KeyReturn || message.String() == "enter" || message.String() == "return"
}

func executeInteractiveLine(ctx context.Context, client *slackClient, config slackConfig, defaultChannel, line string) (string, string, error) {
	fields, err := interactiveCommandFields(line)
	if err != nil {
		return "", "", err
	}
	if len(fields) == 0 {
		return "", "", nil
	}
	command := fields[0]
	args := fields[1:]
	switch command {
	case "help", "?":
		return interactiveHelp(), "", nil
	case "status":
		return fmt.Sprintf("Workspace %s (%s), user %s (%s), default channel %s\n",
			firstNonEmpty(config.Team, "-"),
			firstNonEmpty(config.TeamID, "-"),
			firstNonEmpty(config.User, "-"),
			firstNonEmpty(config.UserID, "-"),
			defaultChannel,
		), "", nil
	case "channels":
		return interactiveChannels(ctx, client)
	case "use":
		if len(args) != 1 {
			return "", "", errors.New("use requires CHANNEL")
		}
		channel, err := client.ResolveChannel(ctx, args[0])
		if err != nil {
			return "", "", err
		}
		return "Default channel is now " + conversationLabel(channel) + "\n", channel.ID, nil
	case "read":
		return interactiveRead(ctx, client, defaultChannel, args)
	case "write":
		return interactiveWrite(ctx, client, defaultChannel, args)
	case "reply":
		return interactiveReply(ctx, client, args)
	case "search":
		return interactiveSearch(ctx, client, args)
	case "join":
		return interactiveJoin(ctx, client, args)
	case "leave":
		return interactiveLeave(ctx, client, args)
	case "create":
		return interactiveCreate(ctx, client, args)
	case "delete-message":
		return interactiveDeleteMessage(ctx, client, args)
	case "archive":
		return interactiveArchive(ctx, client, args)
	default:
		return "", "", fmt.Errorf("unknown command: %s", command)
	}
}

func interactiveHelp() string {
	return strings.TrimSpace(`
Commands:
  :status
  :channels
  :use #CHANNEL
  :read [#CHANNEL] [--limit N] [--threads]
  :write [#CHANNEL|@USER] TEXT
  :reply #CHANNEL THREAD_TS TEXT
  :search QUERY [--channel #CHANNEL] [--limit N]
  :join #CHANNEL
  :leave #CHANNEL --yes
  :create #CHANNEL [--private]
  :delete-message #CHANNEL TS
  :archive #CHANNEL --yes
  :clear
  :quit

Unqualified text posts to the current channel. Use :write #channel TEXT or :write @user TEXT to override it.
Use quotes when needed. Use :write -- "message" to post text that looks like a flag.
Slack emoji remains safe inside messages, for example :write "hello :smile:".
`) + "\n"
}

func interactiveCommandSuggestions() []string {
	return []string{
		":help",
		":status",
		":channels",
		":use ",
		":read ",
		":write ",
		":reply ",
		":search ",
		":join ",
		":leave ",
		":create ",
		":delete-message ",
		":archive ",
		":clear",
		":quit",
	}
}

func interactiveChannels(ctx context.Context, client *slackClient) (string, string, error) {
	channels, err := client.ListConversations(ctx, listConversationsOptions{ExcludeArchived: true})
	if err != nil {
		return "", "", err
	}
	sort.Slice(channels, func(left, right int) bool {
		return conversationLabel(channels[left]) < conversationLabel(channels[right])
	})
	var output strings.Builder
	for _, channel := range channels {
		marker := " "
		if channel.IsMember {
			marker = "*"
		}
		fmt.Fprintf(&output, "%s %s\n", marker, conversationLabel(channel))
	}
	return output.String(), "", nil
}

func interactiveRead(ctx context.Context, client *slackClient, defaultChannel string, args []string) (string, string, error) {
	opts, err := parseReadOptions(commandOptions{}, args)
	if err != nil {
		return "", "", err
	}
	channel, err := client.ResolveChannel(ctx, firstNonEmpty(opts.channel, defaultChannel))
	if err != nil {
		return "", "", err
	}
	messages, err := client.ReadMessages(ctx, channel.ID, readMessagesOptions{Limit: opts.countPerPage, IncludeThreads: opts.threads})
	if err != nil {
		return "", "", err
	}
	output := formatMessageLines(messages)
	if output == "" {
		output = "(no messages)\n"
	}
	return output, "", nil
}

func interactiveWrite(ctx context.Context, client *slackClient, defaultChannel string, args []string) (string, string, error) {
	opts, err := parseWriteOptions(commandOptions{}, args)
	if err != nil {
		return "", "", err
	}
	channel, err := client.ResolveWriteTarget(ctx, firstNonEmpty(opts.channel, defaultChannel))
	if err != nil {
		return "", "", err
	}
	posted, err := client.PostMessage(ctx, channel.ID, opts.text, opts.blocks)
	if err != nil {
		return "", "", err
	}
	return fmt.Sprintf("Posted message to %s at %s\n", conversationLabel(channel), posted.TS), "", nil
}

func interactiveReply(ctx context.Context, client *slackClient, args []string) (string, string, error) {
	opts, err := parseReplyOptions(commandOptions{}, args)
	if err != nil {
		return "", "", err
	}
	channel, err := client.ResolveChannel(ctx, opts.channel)
	if err != nil {
		return "", "", err
	}
	posted, err := client.ReplyMessage(ctx, channel.ID, opts.threadTS, opts.text)
	if err != nil {
		return "", "", err
	}
	return fmt.Sprintf("Posted reply to %s at %s\n", conversationLabel(channel), posted.TS), "", nil
}

func interactiveSearch(ctx context.Context, client *slackClient, args []string) (string, string, error) {
	opts, err := parseSearchOptions(commandOptions{}, args)
	if err != nil {
		return "", "", err
	}
	query := opts.query
	searchChannel := ""
	if opts.channel != "" {
		channel, err := client.ResolveChannel(ctx, opts.channel)
		if err != nil {
			return "", "", err
		}
		searchChannel = channel.ID
		if channel.Name != "" && !strings.Contains(query, " in:") {
			query += " in:#" + channel.Name
		}
	}
	results, err := client.SearchMessages(ctx, query, searchOptions{Channel: searchChannel, Limit: opts.countPerPage})
	if err != nil {
		return "", "", err
	}
	output := formatSearchResults(results)
	if output == "" {
		output = "(no results)\n"
	}
	return output, "", nil
}

func interactiveJoin(ctx context.Context, client *slackClient, args []string) (string, string, error) {
	opts, err := parseJoinOptions(commandOptions{}, args)
	if err != nil {
		return "", "", err
	}
	channel, err := client.ResolveChannel(ctx, opts.channel)
	if err != nil {
		return "", "", err
	}
	joined, err := client.JoinChannel(ctx, channel.ID)
	if err != nil {
		return "", "", err
	}
	if joined.ID == "" {
		joined = channel
	}
	return "Joined " + conversationLabel(joined) + "\n", "", nil
}

func interactiveLeave(ctx context.Context, client *slackClient, args []string) (string, string, error) {
	opts, err := parseLeaveOptions(commandOptions{}, args)
	if err != nil {
		return "", "", err
	}
	if !opts.yes {
		return "", "", errors.New("leave in interactive mode requires --yes")
	}
	channel, err := client.ResolveChannel(ctx, opts.channel)
	if err != nil {
		return "", "", err
	}
	if err := client.LeaveChannel(ctx, channel.ID); err != nil {
		return "", "", err
	}
	return "Left " + conversationLabel(channel) + "\n", "", nil
}

func interactiveCreate(ctx context.Context, client *slackClient, args []string) (string, string, error) {
	opts, err := parseCreateOptions(commandOptions{}, args)
	if err != nil {
		return "", "", err
	}
	channel, err := client.CreateChannel(ctx, opts.channel, opts.private)
	if err != nil {
		return "", "", err
	}
	return "Created " + conversationLabel(channel) + "\n", "", nil
}

func interactiveDeleteMessage(ctx context.Context, client *slackClient, args []string) (string, string, error) {
	opts, err := parseDeleteMessageOptions(commandOptions{}, args)
	if err != nil {
		return "", "", err
	}
	channel, err := client.ResolveChannel(ctx, opts.channel)
	if err != nil {
		return "", "", err
	}
	if err := client.DeleteMessage(ctx, channel.ID, opts.threadTS); err != nil {
		return "", "", err
	}
	return fmt.Sprintf("Deleted message %s from %s\n", opts.threadTS, conversationLabel(channel)), "", nil
}

func interactiveArchive(ctx context.Context, client *slackClient, args []string) (string, string, error) {
	opts, err := parseArchiveOptions(commandOptions{}, args)
	if err != nil {
		return "", "", err
	}
	if !opts.yes {
		return "", "", errors.New("archive in interactive mode requires --yes")
	}
	channel, err := client.ResolveChannel(ctx, opts.channel)
	if err != nil {
		return "", "", err
	}
	if err := client.ArchiveChannel(ctx, channel.ID); err != nil {
		return "", "", err
	}
	return "Archived " + conversationLabel(channel) + "\n", "", nil
}

func shellFields(line string) ([]string, error) {
	var fields []string
	var current strings.Builder
	var quote rune
	escaped := false
	for _, char := range line {
		if escaped {
			current.WriteRune(char)
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				current.WriteRune(char)
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case ' ', '\t', '\n':
			if current.Len() > 0 {
				fields = append(fields, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(char)
		}
	}
	if escaped {
		current.WriteRune('\\')
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}
	return fields, nil
}

func interactiveCommandFields(line string) ([]string, error) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, ":") {
		return nil, errors.New("commands must start with :")
	}
	line = strings.TrimSpace(strings.TrimPrefix(line, ":"))
	if line == "" {
		return nil, errors.New("command is missing after :")
	}
	return shellFields(line)
}

func appendOutput(output []string, value string) []string {
	output = append(output, value)
	if len(output) > 200 {
		output = output[len(output)-200:]
	}
	return output
}
