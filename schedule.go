package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var slackWorkspaceHost = regexp.MustCompile(`^[a-z0-9-]+\.slack\.com$`)

func parseScheduleOptions(opts commandOptions, tokens []string) (commandOptions, error) {
	flags := flag.NewFlagSet("slack-cli schedule", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	at := flags.String("at", "", "delivery time in RFC3339 format")
	flags.BoolVar(&opts.help, "h", false, "show help")
	flags.BoolVar(&opts.help, "help", false, "show help")
	if err := flags.Parse(tokens); err != nil || opts.help {
		return opts, err
	}
	if flags.NArg() < 2 {
		return opts, errors.New("schedule requires TARGET and TEXT")
	}
	opts.channel = strings.TrimSpace(flags.Arg(0))
	opts.text = strings.TrimSpace(strings.Join(flags.Args()[1:], " "))
	if opts.channel == "" || opts.text == "" {
		return opts, errors.New("schedule requires TARGET and TEXT")
	}
	var err error
	opts.scheduleAt, err = time.Parse(time.RFC3339, *at)
	if err != nil || opts.scheduleAt.Unix() <= time.Now().Unix() {
		return opts, errors.New("schedule requires --at with a future RFC3339 time, e.g. 2027-01-15T09:00:00+01:00")
	}
	return opts, nil
}

func handleSchedule(ctx context.Context, configPath string, opts commandOptions) error {
	config, err := loadRequiredConfig(configPath)
	if err != nil {
		return err
	}
	client := clientFromConfig(config)
	if !strings.HasPrefix(client.token, "xoxc-") || client.cookies["d"] == "" {
		return errors.New("schedule requires a browser session from slack-cli auth login")
	}
	info, err := client.authTest(ctx)
	if err != nil {
		return err
	}
	channel, err := client.ResolveWriteTarget(ctx, opts.channel)
	if err != nil {
		return err
	}
	id, err := client.scheduleDraft(ctx, info, channel.ID, opts.text, opts.scheduleAt)
	if err != nil {
		return err
	}
	fmt.Printf("Scheduled message to %s for %s (draft %s)\n", conversationLabel(channel), opts.scheduleAt.Format(time.RFC3339), terminalText(id))
	return nil
}

// shortcut: drafts.create is an undocumented web endpoint, replace it when we switch scheduling APIs.
func (client *slackClient) scheduleDraft(ctx context.Context, info authInfo, channel, text string, at time.Time) (string, error) {
	workspace, err := url.Parse(info.URL)
	if err != nil || workspace.Scheme != "https" || workspace.User != nil || !slackWorkspaceHost.MatchString(workspace.Host) {
		return "", errors.New("could not verify the Slack workspace URL for scheduling")
	}
	blocks, err := json.Marshal([]richTextBlock{{
		Type: "rich_text",
		Elements: []any{richTextSectionElement{
			Type:     "rich_text_section",
			Elements: []richTextElement{{Type: "text", Text: text}},
		}},
	}})
	if err != nil {
		return "", err
	}
	destinations, err := json.Marshal([]map[string]string{{"channel_id": channel}})
	if err != nil {
		return "", err
	}
	var id [16]byte
	rand.Read(id[:])
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	messageID := fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for key, value := range map[string]string{
		"token":            client.token,
		"blocks":           string(blocks),
		"destinations":     string(destinations),
		"date_scheduled":   strconv.FormatInt(at.Unix(), 10),
		"client_msg_id":    messageID,
		"file_ids":         "[]",
		"is_from_composer": "true",
	} {
		if err := form.WriteField(key, value); err != nil {
			return "", err
		}
	}
	if err := form.Close(); err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+workspace.Host+"/api/drafts.create", &body)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	request.Header.Set("Cookie", formatCookieHeader(client.cookies))
	if at.Unix() <= time.Now().Unix() {
		return "", errors.New("scheduled time has already passed")
	}
	httpClient := *client.httpClient
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("Slack drafts.create request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Slack drafts.create returned HTTP %d", response.StatusCode)
	}
	var result struct {
		apiPayload
		Draft struct {
			ID            string `json:"id"`
			DateScheduled int64  `json:"date_scheduled"`
		} `json:"draft"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8*1024*1024)).Decode(&result); err != nil {
		return "", fmt.Errorf("decode Slack drafts.create response: %w", err)
	}
	if !result.OK {
		return "", slackAPIError(result.apiPayload)
	}
	if result.Draft.ID == "" || result.Draft.DateScheduled != at.Unix() {
		return "", errors.New("Slack did not confirm the requested schedule; check Scheduled in Slack before retrying")
	}
	return result.Draft.ID, nil
}
