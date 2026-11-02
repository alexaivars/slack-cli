# slack-cli CLI Plan

## Goal

Build `slack-cli` into a basic, practical Slack CLI for day-to-day read/write workflows while keeping the existing browser-based auth, saved session config, `today` view, and low-level `api METHOD` escape hatch.

## Commands

Primary commands:

```sh
slack-cli interactive [--channel #CHANNEL]
slack-cli read [#CHANNEL] [--limit 20] [--threads] [--json]
slack-cli write ['#CHANNEL'|@USER] TEXT [--format mrkdwn|markdown] [--file FILE] [--stdin] [--blocks-file FILE]
slack-cli reply #CHANNEL THREAD_TS TEXT
slack-cli search QUERY [--channel #CHANNEL] [--limit 20] [--json]
slack-cli join #CHANNEL
slack-cli leave #CHANNEL [--yes]
slack-cli create #CHANNEL [--private]
```

Cleanup and test-support commands:

```sh
slack-cli delete-message #CHANNEL TS
slack-cli archive #CHANNEL --yes
```

Keep these existing commands:

```sh
slack-cli auth login
slack-cli auth status
slack-cli auth logout
slack-cli config get|set|unset
slack-cli api METHOD
slack-cli today
slack-cli posts month
```

## Slack API Mapping

- `read`: `conversations.history`
- `read --threads`: `conversations.history` plus `conversations.replies`
- `write`: `chat.postMessage`
- `reply`: `chat.postMessage` with `thread_ts`
- `search`: `search.messages`
- `join`: `conversations.join`
- `leave`: `conversations.leave`
- `create`: `conversations.create`
- `delete-message`: `chat.delete`
- `archive`: `conversations.archive`

## Slack API Reference Audit

Before adding or changing Slack API behavior, compare the implementation against
Slack's official docs:

- Slack API reference: https://docs.slack.dev/reference
- Web API methods: https://docs.slack.dev/reference/methods
- Slack `mrkdwn` formatting: https://docs.slack.dev/messaging/formatting-message-text
- `chat.postMessage`: https://docs.slack.dev/reference/methods/chat.postMessage/
- Rich text blocks: https://docs.slack.dev/reference/block-kit/blocks/rich-text-block/
- Block Kit reference: https://docs.slack.dev/reference/block-kit

Current coverage:

- `auth.test` verifies saved credentials.
- `conversations.list` and `conversations.info` resolve channel names and IDs.
- `users.list`, `users.info`, and `conversations.open` support `@user` write targets.
- `conversations.history` and `conversations.replies` support read/thread flows.
- `chat.postMessage` supports Slack `mrkdwn`, replies, Block Kit `blocks`, and
  Markdown input converted to Block Kit rich text with `write --format markdown`.
- `search.messages`, `conversations.join`, `conversations.leave`,
  `conversations.create`, `chat.delete`, and `conversations.archive` cover the
  current workflow commands.

Known improvements to consider:

- Prefer JSON request bodies for complex write calls. Slack supports JSON POSTs
  for Web API write methods, which avoids form-encoding structured fields.
- Broaden `write --format markdown` if the daily workflow needs more Markdown
  constructs. It currently covers paragraphs, heading-style emphasis, inline
  bold/italic/strike/code/link text, and unordered/ordered list runs.
- Expose `chat.update` for editing sent messages, especially for updating test
  posts and generated summaries.
- Expose `chat.getPermalink` after writes/deletes when users need stable links.
- Consider `reactions.add` / `reactions.remove` for quick acknowledgement flows.
- Consider `pins.add` / `pins.remove` and bookmarks only if they become part of
  the daily workflow.

## Channel Resolution

Every command that accepts a channel should accept both IDs and `#name` channel targets.

- If the value starts with `#`, strip it before looking up the channel name.
- If the value starts with `C`, `G`, or `D`, treat it as an ID.
- Otherwise call `conversations.list` and match `name` or `name_normalized`.
- Resolve names once per command and keep the cache in memory only.
- Prefer explicit `--channel` or positional channel, then `SLACK_CHANNEL`, then saved `default-channel`.

## Output Modes

Default output should be human-readable and compact:

```text
14:32 casey  message text
14:34 casey  reply text
```

Thread replies should be indented:

```text
14:32 casey  parent message
  14:34 casey  reply message
```

Commands that return data should also support `--json` where practical. The JSON shape should preserve Slack IDs, timestamps, user IDs, display names, and text.

## Bubble Tea Requirement

This tool uses Charmbracelet Bubble Tea for interactive terminal flows.

- Framework: `github.com/charmbracelet/bubbletea`
- Import path in this project: `charm.land/bubbletea/v2`
- Docs to consult before implementing or changing Bubble Tea code: https://pkg.go.dev/charm.land/bubbletea/v2

Use Bubble Tea for:

- Top-level interactive command mode.
- Interactive `read` browsing.
- Thread expansion and navigation.
- Search result browsing.
- Confirmations for actions such as `leave` when `--yes` is not provided.
- Future compose flows if plain command arguments become too limiting.

Keep scriptable flows non-interactive:

- `write`
- `reply`
- `join`
- `create`
- `leave --yes`
- `delete-message`
- `archive --yes`

Interactive command mode should use vi-style `:` commands for local tool actions:

```text
:help
:status
:channels
:use #test-cli
:read #test-cli --limit 10
:write hello from the current channel
:write #test-cli "hello :smile:"
:write @casey "hello direct"
:write "/deploy status"
:quit
```

Only a leading `:` starts a local command. Colons inside message text remain normal Slack text, so emoji such as `:smile:` is safe. Use `:write` to send any message that begins with `/` or `:`.
Unqualified `:write` text posts to the current channel. Use an explicit leading `#channel` or `@user` target to write somewhere else.

Interactive command input should use the Bubbles text input component for autocomplete:

```go
import "charm.land/bubbles/v2/textinput"
```

The command prompt should enable `ShowSuggestions` and call `SetSuggestions` with the supported `:` commands.

## Typed Client Methods

Move common Slack operations behind typed client methods instead of scattering raw `callAPI` use through command handlers.

Suggested methods:

```go
ListConversations(ctx context.Context, opts listConversationsOptions) ([]conversation, error)
ResolveChannel(ctx context.Context, value string) (conversation, error)
ReadMessages(ctx context.Context, channel string, opts readMessagesOptions) ([]slackMessage, error)
PostMessage(ctx context.Context, channel, text string, blocks json.RawMessage) (postedMessage, error)
ReplyMessage(ctx context.Context, channel, threadTS, text string) (postedMessage, error)
SearchMessages(ctx context.Context, query string, opts searchOptions) ([]searchResult, error)
JoinChannel(ctx context.Context, channel string) (conversation, error)
LeaveChannel(ctx context.Context, channel string) error
CreateChannel(ctx context.Context, name string, private bool) (conversation, error)
DeleteMessage(ctx context.Context, channel, ts string) error
ArchiveChannel(ctx context.Context, channel string) error
```

`api METHOD` should remain available as the low-level escape hatch.

## User Lookup

Reuse the existing user resolution work:

- Collect senders, thread reply users, and `<@U...>` mentions.
- Try the channel-scoped Edge users list endpoint first when `teamId` is available.
- Fall back to `users.info` for missing IDs.
- Prefer display name, then real name, then username, then ID.

## Implementation Order

1. Add command parsing and help text for `read`, `write`, `reply`, `search`, `join`, `leave`, `create`, `delete-message`, and `archive`.
2. Add typed Slack client methods and tests with mocked HTTP transports.
3. Implement `read`, `write`, and `reply`.
4. Add channel name resolution.
5. Implement `join`, `leave`, and `create`.
6. Implement `search`.
7. Implement cleanup commands.
8. Add Bubble Tea interactive browsing where it improves command usability.
9. Run unit tests and integration smoke tests against the dev workspace.

## Integration Tests

Use a workspace you own and explicitly authorize before running live tests.
Create dedicated test channels. Only delete messages or archive channels created
by the current test run.
