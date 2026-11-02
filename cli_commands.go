package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

var slackPermalinkPattern = regexp.MustCompile(`^/archives/([A-Z0-9]+)/p([0-9]+)$`)

func parseReadOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	opts.countPerPage = 20
	flags := flag.NewFlagSet("slack-cli read", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.IntVar(&opts.countPerPage, "limit", 20, "number of messages to read, 1-200")
	flags.BoolVar(&opts.threads, "threads", false, "expand thread replies inline")
	flags.BoolVar(&opts.json, "json", false, "print machine-readable JSON")
	flags.BoolVar(&opts.help, "h", false, "show help")
	flags.BoolVar(&opts.help, "help", false, "show help")
	if err := flags.Parse(reorderKnownFlags(tokens, map[string]bool{"limit": true}, map[string]bool{"threads": true, "json": true, "h": true, "help": true})); err != nil {
		return opts, err
	}
	if flags.NArg() > 1 {
		return opts, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args()[1:], " "))
	}
	if flags.NArg() == 1 {
		opts.channel = flags.Arg(0)
	}
	return opts, nil
}

func parseThreadOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	flags := flag.NewFlagSet("slack-cli thread", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.BoolVar(&opts.json, "json", false, "print machine-readable JSON")
	flags.BoolVar(&opts.help, "h", false, "show help")
	flags.BoolVar(&opts.help, "help", false, "show help")
	if err := flags.Parse(reorderKnownFlags(tokens, nil, map[string]bool{"json": true, "h": true, "help": true})); err != nil {
		return opts, err
	}
	if flags.NArg() == 1 {
		channel, threadTS, err := parseSlackPermalink(flags.Arg(0))
		if err != nil {
			return opts, err
		}
		opts.channel = channel
		opts.threadTS = threadTS
		return opts, nil
	}
	if flags.NArg() == 2 {
		opts.channel = flags.Arg(0)
		opts.threadTS = flags.Arg(1)
		return opts, nil
	}
	if opts.help {
		return opts, nil
	}
	return opts, errors.New("thread requires PERMALINK or CHANNEL TS")
}

func parseSlackPermalink(value string) (string, string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || !strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".slack.com") {
		return "", "", fmt.Errorf("thread permalink must be an https://*.slack.com/archives/... URL")
	}
	matches := slackPermalinkPattern.FindStringSubmatch(parsed.Path)
	if len(matches) != 3 || len(matches[2]) <= 6 {
		return "", "", fmt.Errorf("invalid Slack thread permalink: %s", value)
	}
	digits := matches[2]
	return matches[1], digits[:len(digits)-6] + "." + digits[len(digits)-6:], nil
}

func parseInteractiveOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	flags := flag.NewFlagSet("slack-cli interactive", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&opts.channel, "channel", "", "default #channel or ID for this session")
	flags.BoolVar(&opts.help, "h", false, "show help")
	flags.BoolVar(&opts.help, "help", false, "show help")
	if err := flags.Parse(tokens); err != nil {
		return opts, err
	}
	if flags.NArg() != 0 {
		return opts, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	return opts, nil
}

func parseWriteOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	flags := flag.NewFlagSet("slack-cli write", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&opts.format, "format", "mrkdwn", "input format: mrkdwn or markdown")
	flags.StringVar(&opts.inputFile, "file", "", "read message text from a file")
	flags.BoolVar(&opts.readStdin, "stdin", false, "read message text from stdin")
	flags.StringVar(&opts.blocksFile, "blocks-file", "", "Block Kit JSON file to send as the message blocks")
	flags.BoolVar(&opts.help, "h", false, "show help")
	flags.BoolVar(&opts.help, "help", false, "show help")
	if err := flags.Parse(reorderKnownFlags(tokens, map[string]bool{"format": true, "file": true, "blocks-file": true}, map[string]bool{"stdin": true, "h": true, "help": true})); err != nil {
		return opts, err
	}
	tokens = flags.Args()
	if len(tokens) == 0 && opts.inputFile == "" && !opts.readStdin && opts.blocksFile == "" && !opts.help {
		return opts, errors.New("write requires TEXT or TARGET TEXT")
	}
	if len(tokens) > 0 && tokens[0] == "--" {
		opts.text = strings.TrimSpace(strings.Join(tokens[1:], " "))
	} else if len(tokens) == 1 && isExplicitWriteTarget(tokens[0]) && (opts.inputFile != "" || opts.readStdin || opts.blocksFile != "") {
		opts.channel = tokens[0]
	} else if len(tokens) > 1 && isExplicitWriteTarget(tokens[0]) {
		opts.channel = tokens[0]
		textStart := 1
		if tokens[textStart] == "--" {
			textStart++
		}
		opts.text = strings.TrimSpace(strings.Join(tokens[textStart:], " "))
	} else if len(tokens) > 0 {
		opts.text = strings.TrimSpace(strings.Join(tokens, " "))
	}
	if opts.inputFile != "" && opts.readStdin {
		return opts, errors.New("use either --file or --stdin, not both")
	}
	switch opts.format {
	case "", "mrkdwn":
		opts.format = "mrkdwn"
	case "markdown":
	default:
		return opts, fmt.Errorf("unsupported --format %q", opts.format)
	}
	if opts.inputFile != "" {
		content, err := os.ReadFile(opts.inputFile)
		if err != nil {
			return opts, fmt.Errorf("read message file: %w", err)
		}
		opts.text = strings.TrimSpace(string(content))
	}
	if opts.readStdin {
		content, err := os.ReadFile("/dev/stdin")
		if err != nil {
			return opts, fmt.Errorf("read stdin: %w", err)
		}
		opts.text = strings.TrimSpace(string(content))
	}
	if opts.blocksFile != "" {
		if opts.format == "markdown" {
			return opts, errors.New("use either --format markdown or --blocks-file, not both")
		}
		blocks, err := os.ReadFile(opts.blocksFile)
		if err != nil {
			return opts, fmt.Errorf("read blocks file: %w", err)
		}
		if !json.Valid(blocks) {
			return opts, errors.New("--blocks-file must contain valid JSON")
		}
		opts.blocks = append(opts.blocks[:0], blocks...)
	}
	if opts.format == "markdown" {
		blocks, fallback, err := markdownToSlackBlocks(opts.text)
		if err != nil {
			return opts, err
		}
		opts.blocks = blocks
		opts.text = fallback
	}
	if opts.text == "" && len(opts.blocks) == 0 && !opts.help {
		return opts, errors.New("write requires message text")
	}
	return opts, nil
}

func parseReplyOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	if len(tokens) < 3 {
		return opts, errors.New("reply requires CHANNEL THREAD_TS TEXT")
	}
	opts.channel = tokens[0]
	opts.threadTS = tokens[1]
	textStart := 2
	if tokens[textStart] == "--" {
		textStart++
	}
	opts.text = strings.TrimSpace(strings.Join(tokens[textStart:], " "))
	if opts.text == "" {
		return opts, errors.New("reply requires message text")
	}
	return opts, nil
}

func parseSearchOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	opts.countPerPage = 20
	flags := flag.NewFlagSet("slack-cli search", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&opts.channel, "channel", "", "limit search to a channel name or ID")
	flags.IntVar(&opts.countPerPage, "limit", 20, "number of search results, 1-100")
	flags.BoolVar(&opts.json, "json", false, "print machine-readable JSON")
	flags.BoolVar(&opts.help, "h", false, "show help")
	flags.BoolVar(&opts.help, "help", false, "show help")
	if err := flags.Parse(reorderKnownFlags(tokens, map[string]bool{"channel": true, "limit": true}, map[string]bool{"json": true, "h": true, "help": true})); err != nil {
		return opts, err
	}
	opts.query = strings.TrimSpace(strings.Join(flags.Args(), " "))
	if opts.query == "" && !opts.help {
		return opts, errors.New("search requires QUERY")
	}
	return opts, nil
}

func parseJoinOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	if len(tokens) != 1 {
		return opts, errors.New("join requires CHANNEL")
	}
	opts.channel = tokens[0]
	return opts, nil
}

func parseLeaveOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	flags := flag.NewFlagSet("slack-cli leave", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.BoolVar(&opts.yes, "yes", false, "confirm leaving the channel")
	flags.BoolVar(&opts.help, "h", false, "show help")
	flags.BoolVar(&opts.help, "help", false, "show help")
	if err := flags.Parse(reorderKnownFlags(tokens, nil, map[string]bool{"yes": true, "h": true, "help": true})); err != nil {
		return opts, err
	}
	if flags.NArg() != 1 && !opts.help {
		return opts, errors.New("leave requires CHANNEL")
	}
	if flags.NArg() == 1 {
		opts.channel = flags.Arg(0)
	}
	return opts, nil
}

func parseCreateOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	flags := flag.NewFlagSet("slack-cli create", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.BoolVar(&opts.private, "private", false, "create a private channel")
	flags.BoolVar(&opts.help, "h", false, "show help")
	flags.BoolVar(&opts.help, "help", false, "show help")
	if err := flags.Parse(reorderKnownFlags(tokens, nil, map[string]bool{"private": true, "h": true, "help": true})); err != nil {
		return opts, err
	}
	if flags.NArg() != 1 && !opts.help {
		return opts, errors.New("create requires CHANNEL")
	}
	if flags.NArg() == 1 {
		opts.channel = flags.Arg(0)
	}
	return opts, nil
}

func parseDeleteMessageOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	if len(tokens) != 2 {
		return opts, errors.New("delete-message requires CHANNEL TS")
	}
	opts.channel = tokens[0]
	opts.threadTS = tokens[1]
	return opts, nil
}

func parseArchiveOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	flags := flag.NewFlagSet("slack-cli archive", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.BoolVar(&opts.yes, "yes", false, "confirm archiving the channel")
	flags.BoolVar(&opts.help, "h", false, "show help")
	flags.BoolVar(&opts.help, "help", false, "show help")
	if err := flags.Parse(reorderKnownFlags(tokens, nil, map[string]bool{"yes": true, "h": true, "help": true})); err != nil {
		return opts, err
	}
	if flags.NArg() != 1 && !opts.help {
		return opts, errors.New("archive requires CHANNEL")
	}
	if flags.NArg() == 1 {
		opts.channel = flags.Arg(0)
	}
	return opts, nil
}

func handleInteractive(ctx context.Context, configPath string, opts commandOptions) error {
	config, client, err := commandClient(ctx, configPath)
	if err != nil {
		return err
	}
	defaultChannel := firstNonEmpty(opts.channel, config.DefaultChannel)
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("interactive mode requires a terminal")
	}
	program := tea.NewProgram(newInteractiveModel(client, config, defaultChannel), tea.WithContext(ctx))
	_, err = program.Run()
	return err
}

func handleRead(ctx context.Context, configPath string, opts commandOptions) error {
	config, client, err := commandClient(ctx, configPath)
	if err != nil {
		return err
	}
	channel, err := resolveCommandChannel(ctx, client, config, opts.channel)
	if err != nil {
		return err
	}
	if opts.json || !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		messages, err := client.ReadMessages(ctx, channel.ID, readMessagesOptions{Limit: opts.countPerPage, IncludeThreads: opts.threads})
		if err != nil {
			return err
		}
		if opts.json {
			return printIndentedJSON(messages)
		}
		fmt.Print(formatMessageLines(messages))
		return nil
	}
	program := tea.NewProgram(newReadModel(client, channel, opts.countPerPage, opts.threads), tea.WithContext(ctx))
	_, err = program.Run()
	return err
}

func handleThread(ctx context.Context, configPath string, opts commandOptions) error {
	_, client, err := commandClient(ctx, configPath)
	if err != nil {
		return err
	}
	channel, err := client.ResolveChannel(ctx, opts.channel)
	if err != nil {
		return err
	}
	thread, err := client.ReadThread(ctx, channel.ID, opts.threadTS)
	if err != nil {
		return err
	}
	if opts.json {
		return printIndentedJSON(thread)
	}
	fmt.Print(formatMessageLines(todayMessages{Users: thread.Users, Messages: thread.Messages}))
	return nil
}

func isExplicitWriteTarget(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "#") || strings.HasPrefix(value, "@") || isChannelID(value)
}

func handleWrite(ctx context.Context, configPath string, opts commandOptions) error {
	config, client, err := commandClient(ctx, configPath)
	if err != nil {
		return err
	}
	channel, err := resolveCommandWriteTarget(ctx, client, config, opts.channel)
	if err != nil {
		return err
	}
	posted, err := client.PostMessage(ctx, channel.ID, opts.text, opts.blocks)
	if err != nil {
		return err
	}
	fmt.Printf("Posted message to %s at %s\n", conversationLabel(channel), posted.TS)
	return nil
}

func handleReply(ctx context.Context, configPath string, opts commandOptions) error {
	_, client, err := commandClient(ctx, configPath)
	if err != nil {
		return err
	}
	channel, err := client.ResolveChannel(ctx, opts.channel)
	if err != nil {
		return err
	}
	posted, err := client.ReplyMessage(ctx, channel.ID, opts.threadTS, opts.text)
	if err != nil {
		return err
	}
	fmt.Printf("Posted reply to %s at %s\n", conversationLabel(channel), posted.TS)
	return nil
}

func handleSearch(ctx context.Context, configPath string, opts commandOptions) error {
	_, client, err := commandClient(ctx, configPath)
	if err != nil {
		return err
	}
	query := opts.query
	searchChannel := ""
	if opts.channel != "" {
		channel, err := client.ResolveChannel(ctx, opts.channel)
		if err != nil {
			return err
		}
		searchChannel = channel.ID
		if channel.Name != "" && !strings.Contains(query, " in:") {
			query += " in:#" + channel.Name
		}
	}
	results, err := client.SearchMessages(ctx, query, searchOptions{Channel: searchChannel, Limit: opts.countPerPage})
	if err != nil {
		return err
	}
	if opts.json {
		return printIndentedJSON(results)
	}
	printSearchResults(results)
	return nil
}

func handleJoin(ctx context.Context, configPath string, opts commandOptions) error {
	_, client, err := commandClient(ctx, configPath)
	if err != nil {
		return err
	}
	channel, err := client.ResolveChannel(ctx, opts.channel)
	if err != nil {
		return err
	}
	joined, err := client.JoinChannel(ctx, channel.ID)
	if err != nil {
		return err
	}
	if joined.ID == "" {
		joined = channel
	}
	fmt.Printf("Joined %s\n", conversationLabel(joined))
	return nil
}

func handleLeave(ctx context.Context, configPath string, opts commandOptions) error {
	_, client, err := commandClient(ctx, configPath)
	if err != nil {
		return err
	}
	channel, err := client.ResolveChannel(ctx, opts.channel)
	if err != nil {
		return err
	}
	if !opts.yes {
		confirmed, err := confirmAction("Leave " + conversationLabel(channel) + "?")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("Canceled.")
			return nil
		}
	}
	if err := client.LeaveChannel(ctx, channel.ID); err != nil {
		return err
	}
	fmt.Printf("Left %s\n", conversationLabel(channel))
	return nil
}

func handleCreate(ctx context.Context, configPath string, opts commandOptions) error {
	_, client, err := commandClient(ctx, configPath)
	if err != nil {
		return err
	}
	channel, err := client.CreateChannel(ctx, opts.channel, opts.private)
	if err != nil {
		return err
	}
	fmt.Printf("Created %s\n", conversationLabel(channel))
	return nil
}

func handleDeleteMessage(ctx context.Context, configPath string, opts commandOptions) error {
	_, client, err := commandClient(ctx, configPath)
	if err != nil {
		return err
	}
	channel, err := client.ResolveChannel(ctx, opts.channel)
	if err != nil {
		return err
	}
	if err := client.DeleteMessage(ctx, channel.ID, opts.threadTS); err != nil {
		return err
	}
	fmt.Printf("Deleted message %s from %s\n", opts.threadTS, conversationLabel(channel))
	return nil
}

func handleArchive(ctx context.Context, configPath string, opts commandOptions) error {
	_, client, err := commandClient(ctx, configPath)
	if err != nil {
		return err
	}
	channel, err := client.ResolveChannel(ctx, opts.channel)
	if err != nil {
		return err
	}
	if !opts.yes {
		confirmed, err := confirmAction("Archive " + conversationLabel(channel) + "?")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("Canceled.")
			return nil
		}
	}
	if err := client.ArchiveChannel(ctx, channel.ID); err != nil {
		return err
	}
	fmt.Printf("Archived %s\n", conversationLabel(channel))
	return nil
}

func handleFiles(ctx context.Context, configPath string, opts commandOptions) error {
	if opts.subcommand != "download" {
		return errors.New("files requires command: download")
	}
	_, client, err := commandClient(ctx, configPath)
	if err != nil {
		return err
	}
	file, err := client.DownloadFile(ctx, opts.file, opts.output, opts.overwrite)
	if err != nil {
		return err
	}
	fmt.Printf("Downloaded %s to %s\n", file.Name, opts.output)
	return nil
}

func commandClient(ctx context.Context, configPath string) (slackConfig, *slackClient, error) {
	config, err := loadValidatedConfig(ctx, configPath)
	if err != nil {
		return slackConfig{}, nil, err
	}
	return config, clientFromConfig(config), nil
}

func resolveCommandChannel(ctx context.Context, client *slackClient, config slackConfig, value string) (conversation, error) {
	return client.ResolveChannel(ctx, firstNonEmpty(value, config.DefaultChannel))
}

func resolveCommandWriteTarget(ctx context.Context, client *slackClient, config slackConfig, value string) (conversation, error) {
	return client.ResolveWriteTarget(ctx, firstNonEmpty(value, config.DefaultChannel))
}

func reorderKnownFlags(tokens []string, valueFlags map[string]bool, boolFlags map[string]bool) []string {
	var flags []string
	var args []string
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		name, ok := knownFlagName(token)
		if !ok || (!valueFlags[name] && !boolFlags[name]) {
			args = append(args, token)
			continue
		}
		flags = append(flags, token)
		if valueFlags[name] && !strings.Contains(token, "=") && index+1 < len(tokens) {
			index++
			flags = append(flags, tokens[index])
		}
	}
	return append(flags, args...)
}

func knownFlagName(token string) (string, bool) {
	if !strings.HasPrefix(token, "-") || token == "-" || token == "--" {
		return "", false
	}
	name := strings.TrimLeft(token, "-")
	if index := strings.Index(name, "="); index >= 0 {
		name = name[:index]
	}
	return name, name != ""
}

func printMessageLines(messages todayMessages) {
	fmt.Print(formatMessageLines(messages))
}

func formatMessageLines(messages todayMessages) string {
	names := messageNameMap(messages)
	var output strings.Builder
	for _, message := range messages.Messages {
		indent := ""
		if message.IsThreadReply {
			indent = "  "
		}
		text := strings.ReplaceAll(normalizeSlackText(message.Text, names), "\n", " ")
		if text == "" {
			text = "(no text)"
		}
		fmt.Fprintf(&output, "%s%s %-12s %s\n", indent, shortMessageTime(message.TS), messageSender(message), text)
	}
	return output.String()
}

func printSearchResults(results []searchResult) {
	fmt.Print(formatSearchResults(results))
}

func formatSearchResults(results []searchResult) string {
	var output strings.Builder
	for _, result := range results {
		channel := result.ChannelID
		if result.ChannelName != "" {
			channel = "#" + result.ChannelName
		}
		text := strings.ReplaceAll(normalizeSlackText(result.Text, nil), "\n", " ")
		fmt.Fprintf(&output, "%s %-12s %-18s %s\n", shortMessageTime(result.TS), firstNonEmpty(result.DisplayName, result.User, "-"), channel, text)
	}
	return output.String()
}

func conversationLabel(channel conversation) string {
	name := firstNonEmpty(channel.NameNormalized, channel.Name)
	if channel.IsIM && name != "" {
		if channel.ID == "" {
			return "@" + strings.TrimPrefix(name, "@")
		}
		return fmt.Sprintf("@%s (%s)", strings.TrimPrefix(name, "@"), channel.ID)
	}
	if name == "" {
		return channel.ID
	}
	if channel.ID == "" {
		return "#" + name
	}
	return fmt.Sprintf("#%s (%s)", name, channel.ID)
}
