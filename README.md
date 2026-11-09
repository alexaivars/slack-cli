# slack-cli

A Go CLI for reading, searching and writing Slack messages, with an interactive
terminal interface and JSON output for scripts.

Requires Go 1.26.10 or newer.

```sh
go build -o bin/slack-cli .
bin/slack-cli --help
go test ./...
```

Sign in through the browser:

```sh
bin/slack-cli auth login
```

Login saves your credentials locally for subsequent commands. To save a default
channel during login, add `--channel C123` with your channel ID. You can also set
it later with `bin/slack-cli config set default-channel C123` or pass a channel
to each command.

```sh
bin/slack-cli auth status --json
bin/slack-cli read --json
bin/slack-cli interactive
```

To schedule a plain text message with your browser login, pass a channel or DM
target and a future RFC3339 timestamp with its time zone:

```sh
bin/slack-cli schedule --at '2027-01-15T09:00:00+01:00' '#test-cli' 'Good morning'
```

Manage scheduled messages in Slack under **Drafts & sent → Scheduled**. This
command uses Slack's undocumented browser draft API.
