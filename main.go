package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"
)

var channelParamMethods = map[string]bool{
	"bookmarks.list":              true,
	"canvases.access.delete":      true,
	"canvases.access.set":         true,
	"canvases.create":             true,
	"canvases.edit":               true,
	"canvases.sections.lookup":    true,
	"chat.delete":                 true,
	"chat.deleteScheduledMessage": true,
	"chat.getPermalink":           true,
	"chat.meMessage":              true,
	"chat.postEphemeral":          true,
	"chat.postMessage":            true,
	"chat.scheduleMessage":        true,
	"chat.unfurl":                 true,
	"chat.update":                 true,
	"conversations.archive":       true,
	"conversations.history":       true,
	"conversations.info":          true,
	"conversations.join":          true,
	"conversations.kick":          true,
	"conversations.leave":         true,
	"conversations.mark":          true,
	"conversations.members":       true,
	"conversations.rename":        true,
	"conversations.replies":       true,
	"conversations.setPurpose":    true,
	"conversations.setTopic":      true,
	"files.info":                  true,
	"files.remote.share":          true,
	"pins.add":                    true,
	"pins.list":                   true,
	"pins.remove":                 true,
	"reactions.add":               true,
	"reactions.get":               true,
	"reactions.remove":            true,
}

type commandOptions struct {
	help         bool
	json         bool
	raw          bool
	readStdin    bool
	command      string
	subcommand   string
	method       string
	configFile   string
	configKey    string
	configValue  string
	channel      string
	workspace    string
	verbose      bool
	headless     bool
	keepProfile  bool
	timeout      time.Duration
	scheduleAt   time.Time
	params       map[string]any
	month        string
	output       string
	file         string
	text         string
	format       string
	inputFile    string
	blocksFile   string
	blocks       json.RawMessage
	query        string
	threadTS     string
	countPerPage int
	date         string
	threads      bool
	yes          bool
	private      bool
	overwrite    bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", terminalText(err.Error()))
		os.Exit(1)
	}
}

func run(arguments []string) error {
	opts, err := parseOptions(arguments)
	if err != nil {
		return err
	}
	if opts.help {
		printHelp(os.Stdout)
		return nil
	}

	configPath, err := configuredPath(opts.configFile)
	if err != nil {
		return err
	}
	switch opts.command {
	case "auth":
		return handleAuth(context.Background(), configPath, opts)
	case "config":
		return handleConfig(configPath, opts)
	case "api":
		return handleAPI(context.Background(), configPath, opts)
	case "interactive", "shell", "i":
		return handleInteractive(context.Background(), configPath, opts)
	case "read":
		return handleRead(context.Background(), configPath, opts)
	case "thread":
		return handleThread(context.Background(), configPath, opts)
	case "write":
		return handleWrite(context.Background(), configPath, opts)
	case "schedule":
		return handleSchedule(context.Background(), configPath, opts)
	case "reply":
		return handleReply(context.Background(), configPath, opts)
	case "search":
		return handleSearch(context.Background(), configPath, opts)
	case "join":
		return handleJoin(context.Background(), configPath, opts)
	case "leave":
		return handleLeave(context.Background(), configPath, opts)
	case "create":
		return handleCreate(context.Background(), configPath, opts)
	case "delete-message":
		return handleDeleteMessage(context.Background(), configPath, opts)
	case "archive":
		return handleArchive(context.Background(), configPath, opts)
	case "posts":
		return handlePosts(context.Background(), configPath, opts)
	case "today":
		return handleToday(context.Background(), configPath, opts)
	case "files":
		return handleFiles(context.Background(), configPath, opts)
	default:
		printHelp(os.Stderr)
		return errors.New("missing command")
	}
}

func parseOptions(arguments []string) (commandOptions, error) {
	opts := commandOptions{params: map[string]any{}, countPerPage: 100}
	tokens := append([]string(nil), arguments...)
	for len(tokens) > 0 {
		token := tokens[0]
		if token == "-h" || token == "--help" {
			opts.help = true
			return opts, nil
		}
		if token == "--config-file" {
			value, rest, err := takeValue(tokens[1:], token)
			if err != nil {
				return opts, err
			}
			opts.configFile = value
			tokens = rest
			continue
		}
		opts.command = token
		tokens = tokens[1:]
		break
	}

	switch opts.command {
	case "":
		return opts, nil
	case "auth":
		return parseAuthOptions(opts, tokens)
	case "config":
		return parseConfigOptions(opts, tokens)
	case "api":
		return parseAPIOptions(opts, tokens)
	case "interactive", "shell", "i":
		return parseInteractiveOptions(opts, tokens)
	case "read":
		return parseReadOptions(opts, tokens)
	case "thread":
		return parseThreadOptions(opts, tokens)
	case "write":
		return parseWriteOptions(opts, tokens)
	case "schedule":
		return parseScheduleOptions(opts, tokens)
	case "reply":
		return parseReplyOptions(opts, tokens)
	case "search":
		return parseSearchOptions(opts, tokens)
	case "join":
		return parseJoinOptions(opts, tokens)
	case "leave":
		return parseLeaveOptions(opts, tokens)
	case "create":
		return parseCreateOptions(opts, tokens)
	case "delete-message":
		return parseDeleteMessageOptions(opts, tokens)
	case "archive":
		return parseArchiveOptions(opts, tokens)
	case "posts":
		return parsePostsOptions(opts, tokens)
	case "today":
		return parseTodayOptions(opts, tokens)
	case "files":
		return parseFilesOptions(opts, tokens)
	default:
		return opts, fmt.Errorf("unknown command: %s", opts.command)
	}
}

func parseAuthOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	if len(tokens) == 0 {
		return opts, errors.New("auth requires one of: login, status, logout")
	}
	opts.subcommand = tokens[0]
	flags := flag.NewFlagSet("slack-cli auth "+opts.subcommand, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.BoolVar(&opts.help, "h", false, "show help")
	flags.BoolVar(&opts.help, "help", false, "show help")
	flags.BoolVar(&opts.json, "json", false, "print machine-readable JSON")
	flags.BoolVar(&opts.verbose, "verbose", false, "print browser capture details")
	flags.BoolVar(&opts.headless, "headless", false, "run the browser without a visible window")
	flags.BoolVar(&opts.keepProfile, "keep-browser-profile", false, "keep the browser profile when logging out")
	flags.StringVar(&opts.channel, "channel", "", "default Slack channel ID")
	flags.StringVar(&opts.workspace, "workspace", "", "Slack workspace subdomain")
	timeoutSeconds := flags.Int("timeout", 300, "browser login timeout in seconds")
	if err := flags.Parse(tokens[1:]); err != nil {
		return opts, err
	}
	opts.timeout = time.Duration(*timeoutSeconds) * time.Second
	if flags.NArg() != 0 {
		return opts, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	return opts, nil
}

func parseConfigOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	if len(tokens) == 0 {
		return opts, errors.New("config requires one of: get, set, unset")
	}
	opts.subcommand = tokens[0]
	rest := tokens[1:]
	switch opts.subcommand {
	case "get":
		if len(rest) > 1 {
			return opts, fmt.Errorf("unexpected argument: %s", rest[1])
		}
		if len(rest) == 1 {
			opts.configKey = rest[0]
		}
	case "set":
		if len(rest) != 2 {
			return opts, errors.New("config set requires KEY VALUE")
		}
		opts.configKey = rest[0]
		opts.configValue = rest[1]
	case "unset":
		if len(rest) != 1 {
			return opts, errors.New("config unset requires KEY")
		}
		opts.configKey = rest[0]
	default:
		return opts, errors.New("config requires one of: get, set, unset")
	}
	return opts, nil
}

func parseAPIOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	if len(tokens) == 0 {
		return opts, errors.New("api requires a Slack method")
	}
	opts.method = tokens[0]
	flags := flag.NewFlagSet("slack-cli api", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	paramValues := repeatFlag{}
	flags.Var(&paramValues, "param", "Slack parameter as KEY=VALUE")
	flags.BoolVar(&opts.readStdin, "stdin", false, "read parameters as a JSON object from stdin")
	flags.BoolVar(&opts.raw, "raw", false, "print Slack errors as JSON instead of failing")
	flags.BoolVar(&opts.help, "h", false, "show help")
	flags.BoolVar(&opts.help, "help", false, "show help")
	if err := flags.Parse(tokens[1:]); err != nil {
		return opts, err
	}
	if flags.NArg() != 0 {
		return opts, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	for _, raw := range paramValues {
		key, value, err := parseParam(raw)
		if err != nil {
			return opts, err
		}
		opts.params[key] = value
	}
	return opts, nil
}

func parsePostsOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	if len(tokens) == 0 {
		return opts, errors.New("posts requires command: month")
	}
	opts.subcommand = tokens[0]
	if opts.subcommand != "month" {
		return opts, errors.New("posts requires command: month")
	}
	flags := flag.NewFlagSet("slack-cli posts month", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&opts.month, "month", "", "month to export as YYYY-MM")
	flags.StringVar(&opts.output, "output", "", "output log file")
	flags.IntVar(&opts.countPerPage, "count-per-page", 100, "Slack search page size, 1-100")
	flags.BoolVar(&opts.json, "json", false, "write machine-readable JSON instead of a text log")
	flags.BoolVar(&opts.help, "h", false, "show help")
	flags.BoolVar(&opts.help, "help", false, "show help")
	if err := flags.Parse(tokens[1:]); err != nil {
		return opts, err
	}
	if flags.NArg() != 0 {
		return opts, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	return opts, nil
}

func parseTodayOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	flags := flag.NewFlagSet("slack-cli today", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&opts.channel, "channel", "", "Slack channel ID")
	flags.StringVar(&opts.date, "date", "", "date to list as YYYY-MM-DD, default today")
	flags.IntVar(&opts.countPerPage, "count-per-page", 200, "Slack history page size, 1-200")
	flags.BoolVar(&opts.json, "json", false, "print machine-readable JSON")
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

func parseFilesOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	if len(tokens) == 0 || tokens[0] != "download" {
		return opts, errors.New("files requires command: download")
	}
	opts.subcommand = "download"
	flags := flag.NewFlagSet("slack-cli files download", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.BoolVar(&opts.overwrite, "overwrite", false, "replace an existing destination file")
	flags.BoolVar(&opts.help, "h", false, "show help")
	flags.BoolVar(&opts.help, "help", false, "show help")
	if err := flags.Parse(reorderKnownFlags(tokens[1:], nil, map[string]bool{"overwrite": true, "h": true, "help": true})); err != nil {
		return opts, err
	}
	if flags.NArg() != 2 && !opts.help {
		return opts, errors.New("files download requires FILE DESTINATION")
	}
	if flags.NArg() == 2 {
		opts.file = flags.Arg(0)
		opts.output = flags.Arg(1)
	}
	return opts, nil
}

func handleAuth(ctx context.Context, configPath string, opts commandOptions) error {
	switch opts.subcommand {
	case "login":
		return loginWithBrowser(ctx, configPath, opts)
	case "status":
		config, err := loadRequiredConfig(configPath)
		if err != nil {
			return err
		}
		client := clientFromConfig(config)
		info, err := client.authTest(ctx)
		if err != nil {
			return err
		}
		config.UserID = info.UserID
		config.User = info.User
		config.TeamID = info.TeamID
		config.Team = info.Team
		if err := saveConfig(configPath, config); err != nil {
			return err
		}
		if opts.json {
			return printIndentedJSON(info)
		}
		fmt.Printf("Saved Slack token is valid (%s, %s, %s, %s)\n", terminalText(info.Team), terminalText(info.TeamID), terminalText(info.User), terminalText(info.UserID))
		return nil
	case "logout":
		if err := deleteConfig(configPath); err != nil {
			return err
		}
		if !opts.keepProfile {
			profileDir, err := slackBrowserProfileDir()
			if err != nil {
				return err
			}
			if err := os.RemoveAll(profileDir); err != nil {
				return fmt.Errorf("delete browser profile: %w", err)
			}
		}
		fmt.Println("Deleted saved Slack credentials.")
		if !opts.keepProfile {
			fmt.Println("Deleted browser profile.")
		}
		return nil
	default:
		return errors.New("auth requires one of: login, status, logout")
	}
}

func handleConfig(configPath string, opts commandOptions) error {
	config, _, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	key := normalizeConfigKey(opts.configKey)
	switch opts.subcommand {
	case "get":
		if key == "" {
			return printIndentedJSON(redactedConfig(config))
		}
		value, ok := getConfigValue(config, key)
		if !ok {
			return fmt.Errorf("unknown config key: %s", opts.configKey)
		}
		fmt.Println(terminalText(value))
		return nil
	case "set":
		next, err := setConfigValue(config, key, opts.configValue)
		if err != nil {
			return err
		}
		if err := saveConfig(configPath, next); err != nil {
			return err
		}
		fmt.Printf("Set %s=%s\n", terminalText(key), terminalText(safeConfigValue(key, opts.configValue)))
		return nil
	case "unset":
		next, err := unsetConfigValue(config, key)
		if err != nil {
			return err
		}
		if err := saveConfig(configPath, next); err != nil {
			return err
		}
		fmt.Printf("Unset %s\n", terminalText(key))
		return nil
	default:
		return errors.New("config requires one of: get, set, unset")
	}
}

func handleAPI(ctx context.Context, configPath string, opts commandOptions) error {
	config, err := loadValidatedConfig(ctx, configPath)
	if err != nil {
		return err
	}
	params := opts.params
	if opts.readStdin {
		stdinParams, err := readStdinParams(os.Stdin)
		if err != nil {
			return err
		}
		for key, value := range stdinParams {
			params[key] = value
		}
	}
	applyDefaultChannel(opts.method, params, config)

	payload, err := clientFromConfig(config).callAPI(ctx, opts.method, params)
	if err != nil {
		return err
	}
	if !opts.raw && !payload.OK {
		return slackAPIError(payload)
	}
	fmt.Println(prettyJSON(payload.Raw))
	return nil
}

func handlePosts(ctx context.Context, configPath string, opts commandOptions) error {
	config, err := loadValidatedConfig(ctx, configPath)
	if err != nil {
		return err
	}
	exported, total, output, err := exportMonthPosts(ctx, clientFromConfig(config), config.UserID, opts)
	if err != nil {
		return err
	}
	fmt.Printf("Saved %d posts to %s\n", exported, terminalText(output))
	if total != exported {
		fmt.Printf("Slack reported %d total matches.\n", total)
	}
	return nil
}

func handleToday(ctx context.Context, configPath string, opts commandOptions) error {
	config, err := loadValidatedConfig(ctx, configPath)
	if err != nil {
		return err
	}
	channel := firstNonEmpty(opts.channel, os.Getenv("SLACK_CHANNEL"), config.DefaultChannel)
	client := clientFromConfig(config)
	if opts.json {
		messages, err := client.loadTodayMessages(ctx, todayOptions{
			channel:      channel,
			date:         opts.date,
			countPerPage: opts.countPerPage,
		})
		if err != nil {
			return err
		}
		return printIndentedJSON(messages)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("interactive mode requires a terminal; use --json for non-interactive output")
	}
	program := tea.NewProgram(newTodayModel(client, channel, opts.date, opts.countPerPage))
	_, err = program.Run()
	return err
}

func loginWithBrowser(ctx context.Context, path string, opts commandOptions) error {
	profileDir, err := slackBrowserProfileDir()
	if err != nil {
		return err
	}
	session, err := captureSlackSession(browserLoginOptions{
		browserProfileDir: profileDir,
		workspace:         opts.workspace,
		headless:          opts.headless,
		timeout:           opts.timeout,
		verbose:           opts.verbose,
	})
	if err != nil {
		return err
	}
	config := slackConfig{
		Token:          session.token,
		Cookies:        session.cookies,
		TeamDomain:     session.teamDomain,
		SavedAt:        time.Now().UTC().Format(time.RFC3339),
		ExpiresAt:      session.expiresAt,
		DefaultChannel: opts.channel,
	}
	info, err := clientFromConfig(config).authTest(ctx)
	if err != nil {
		return fmt.Errorf("captured token was rejected by Slack: %w (token=%s, d_cookie=%t, cookies=%d)", err, tokenKind(config.Token), config.Cookies["d"] != "", len(config.Cookies))
	}
	config.UserID = info.UserID
	config.User = info.User
	config.TeamID = info.TeamID
	config.Team = info.Team
	if err := saveConfig(path, config); err != nil {
		return err
	}
	fmt.Printf("Saved Slack session to %s\n", terminalText(path))
	if config.ExpiresAt != 0 {
		fmt.Printf("Earliest cookie expiry: %s\n", time.Unix(config.ExpiresAt, 0).UTC().Format(time.RFC3339))
	}
	return nil
}

func tokenKind(token string) string {
	if index := strings.Index(token, "-"); index > 0 {
		return token[:index+1] + "..."
	}
	if token == "" {
		return "missing"
	}
	return "unknown"
}

func configuredPath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	return slackConfigPath()
}

func loadRequiredConfig(path string) (slackConfig, error) {
	config, _, err := loadConfig(path)
	if err != nil {
		return slackConfig{}, err
	}
	config.Token = firstNonEmpty(os.Getenv("SLACK_TOKEN"), config.Token)
	if os.Getenv("SLACK_TOKEN") != "" {
		config.Cookies = nil
	}
	config.DefaultChannel = firstNonEmpty(os.Getenv("SLACK_CHANNEL"), config.DefaultChannel)
	if config.Token == "" {
		return slackConfig{}, errors.New("Slack token is missing; run `slack-cli auth login` or set SLACK_TOKEN")
	}
	return config, nil
}

func loadValidatedConfig(ctx context.Context, path string) (slackConfig, error) {
	config, err := loadRequiredConfig(path)
	if err != nil {
		return slackConfig{}, err
	}
	info, err := clientFromConfig(config).authTest(ctx)
	if err != nil {
		return slackConfig{}, fmt.Errorf("saved Slack session is invalid: %w; run `slack-cli auth login`", err)
	}
	config.UserID = info.UserID
	config.User = info.User
	config.TeamID = info.TeamID
	config.Team = info.Team
	if os.Getenv("SLACK_TOKEN") == "" {
		if err := saveConfig(path, config); err != nil {
			return slackConfig{}, err
		}
	}
	return config, nil
}

func clientFromConfig(config slackConfig) *slackClient {
	client := newSlackClient(firstNonEmpty(os.Getenv("SLACK_API_BASE_URL"), defaultAPI), config.Token)
	client.cookies = config.Cookies
	client.teamID = config.TeamID
	return client
}

func readStdinParams(reader io.Reader) (map[string]any, error) {
	contents, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	var params map[string]any
	if err := json.Unmarshal(contents, &params); err != nil {
		return nil, fmt.Errorf("--stdin expects a JSON object: %w", err)
	}
	if params == nil {
		return nil, errors.New("--stdin expects a JSON object")
	}
	return params, nil
}

func applyDefaultChannel(method string, params map[string]any, config slackConfig) {
	if _, exists := params["channel"]; exists || !channelParamMethods[method] {
		return
	}
	if config.DefaultChannel != "" {
		params["channel"] = config.DefaultChannel
	}
}

func parseParam(raw string) (string, any, error) {
	index := strings.Index(raw, "=")
	if index <= 0 {
		return "", nil, errors.New("--param values must use KEY=VALUE syntax")
	}
	key := raw[:index]
	value := raw[index+1:]
	if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
		var parsed any
		if err := json.Unmarshal([]byte(value), &parsed); err == nil {
			return key, parsed, nil
		}
	}
	return key, value, nil
}

func normalizeConfigKey(key string) string {
	switch key {
	case "default-channel":
		return "defaultChannel"
	default:
		return key
	}
}

func getConfigValue(config slackConfig, key string) (string, bool) {
	switch key {
	case "token":
		if config.Token == "" {
			return "", true
		}
		return "redacted", true
	case "cookies":
		if len(config.Cookies) == 0 {
			return "", true
		}
		return "redacted", true
	case "teamDomain":
		return config.TeamDomain, true
	case "savedAt":
		return config.SavedAt, true
	case "expiresAt":
		if config.ExpiresAt == 0 {
			return "", true
		}
		return fmt.Sprintf("%d", config.ExpiresAt), true
	case "defaultChannel":
		return config.DefaultChannel, true
	case "userId":
		return config.UserID, true
	case "user":
		return config.User, true
	case "teamId":
		return config.TeamID, true
	case "team":
		return config.Team, true
	default:
		return "", false
	}
}

func setConfigValue(config slackConfig, key, value string) (slackConfig, error) {
	switch key {
	case "token":
		config.Token = value
	case "defaultChannel":
		config.DefaultChannel = value
	default:
		return slackConfig{}, fmt.Errorf("unknown or read-only config key: %s", key)
	}
	return config, nil
}

func unsetConfigValue(config slackConfig, key string) (slackConfig, error) {
	switch key {
	case "token":
		config.Token = ""
	case "defaultChannel":
		config.DefaultChannel = ""
	default:
		return slackConfig{}, fmt.Errorf("unknown or read-only config key: %s", key)
	}
	return config, nil
}

func safeConfigValue(key, value string) string {
	if key == "token" && value != "" {
		return "redacted"
	}
	return value
}

func printIndentedJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func prettyJSON(raw json.RawMessage) string {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return strings.TrimSpace(string(raw))
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return strings.TrimSpace(string(raw))
	}
	return string(encoded)
}

func takeValue(tokens []string, option string) (string, []string, error) {
	if len(tokens) == 0 {
		return "", nil, fmt.Errorf("%s requires a value", option)
	}
	return tokens[0], tokens[1:], nil
}

type repeatFlag []string

func (values *repeatFlag) String() string {
	return strings.Join(*values, ",")
}

func (values *repeatFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func printHelp(writer io.Writer) {
	fmt.Fprintf(writer, `Usage:
  slack-cli [--config-file FILE] auth login [--channel ID]
  slack-cli [--config-file FILE] auth status [--json]
  slack-cli [--config-file FILE] auth logout
  slack-cli [--config-file FILE] config get [KEY]
  slack-cli [--config-file FILE] config set KEY VALUE
  slack-cli [--config-file FILE] config unset KEY
  slack-cli [--config-file FILE] api METHOD [--param KEY=VALUE] [--stdin] [--raw]
  slack-cli [--config-file FILE] interactive [--channel #CHANNEL]
  slack-cli [--config-file FILE] read [#CHANNEL] [--limit N] [--threads] [--json]
  slack-cli [--config-file FILE] thread PERMALINK [--json]
  slack-cli [--config-file FILE] thread #CHANNEL TS [--json]
  slack-cli [--config-file FILE] write [#CHANNEL|@USER] TEXT [--format mrkdwn|markdown] [--file FILE] [--stdin] [--blocks-file FILE]
  slack-cli [--config-file FILE] schedule --at RFC3339 TARGET TEXT
  slack-cli [--config-file FILE] reply #CHANNEL THREAD_TS TEXT
  slack-cli [--config-file FILE] search QUERY [--channel #CHANNEL] [--limit N] [--json]
  slack-cli [--config-file FILE] join #CHANNEL
  slack-cli [--config-file FILE] leave #CHANNEL [--yes]
  slack-cli [--config-file FILE] create #CHANNEL [--private]
  slack-cli [--config-file FILE] delete-message #CHANNEL TS
  slack-cli [--config-file FILE] archive #CHANNEL --yes
  slack-cli [--config-file FILE] files download FILE DESTINATION [--overwrite]
  slack-cli [--config-file FILE] today [--channel ID] [--date YYYY-MM-DD] [--json]
  slack-cli [--config-file FILE] posts month [--month YYYY-MM] [--output FILE] [--json]

Examples:
  slack-cli auth login --workspace my-workspace
  slack-cli auth status --json
  slack-cli config set default-channel C123
  slack-cli api auth.test
  slack-cli api conversations.history --param limit=10
  slack-cli files download F123 /tmp/screenshot.png
  slack-cli interactive
  slack-cli read '#test-cli' --limit 10
  slack-cli thread https://workspace.slack.com/archives/C123/p1700000000000000
  slack-cli write "hello from slack-cli"
  slack-cli write '#test-cli' "hello from slack-cli"
  slack-cli write '#test-cli' --format markdown --file changes.md
  slack-cli write @casey "hello direct"
  slack-cli reply '#test-cli' 1782060325.263859 "thread reply"
  slack-cli search "deploy failed" --channel '#test-cli'
  slack-cli create '#slack-cli-test' --private
  slack-cli today --channel C123
  slack-cli posts month --month 2026-06

Environment:
  SLACK_TOKEN          Override the configured Slack token
  SLACK_CHANNEL        Override the configured default channel
  SLACK_CONFIG_FILE    Override the config file location
  SLACK_API_BASE_URL   Override the Slack API base URL for tests
`)
}
