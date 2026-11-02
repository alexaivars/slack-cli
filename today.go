package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const stockholmTimeZone = "Europe/Stockholm"

var slackMentionPattern = regexp.MustCompile(`<@([UW][A-Z0-9]+)>`)

type todayOptions struct {
	channel      string
	date         string
	countPerPage int
}

type todayMessages struct {
	Channel  string            `json:"channel"`
	Date     string            `json:"date"`
	Start    string            `json:"start"`
	End      string            `json:"end"`
	Users    map[string]string `json:"users,omitempty"`
	Messages []slackMessage    `json:"messages"`
}

type slackMessage struct {
	Type          string   `json:"type"`
	Subtype       string   `json:"subtype,omitempty"`
	User          string   `json:"user,omitempty"`
	Username      string   `json:"username,omitempty"`
	BotID         string   `json:"bot_id,omitempty"`
	Text          string   `json:"text,omitempty"`
	TS            string   `json:"ts"`
	ThreadTS      string   `json:"thread_ts,omitempty"`
	ReplyCount    int      `json:"reply_count,omitempty"`
	ReplyUsers    []string `json:"reply_users,omitempty"`
	DisplayName   string   `json:"display_name,omitempty"`
	IsThreadReply bool     `json:"is_thread_reply,omitempty"`
	ParentTS      string   `json:"parent_ts,omitempty"`
}

type conversationHistoryResponse struct {
	OK               bool           `json:"ok"`
	Messages         []slackMessage `json:"messages"`
	HasMore          bool           `json:"has_more"`
	PinCount         int            `json:"pin_count"`
	ResponseMetadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

func (client *slackClient) loadTodayMessages(ctx context.Context, opts todayOptions) (todayMessages, error) {
	channel := strings.TrimSpace(opts.channel)
	if channel == "" {
		return todayMessages{}, errors.New("channel is required; pass --channel or set a default channel")
	}
	date, start, end, err := dayBounds(opts.date)
	if err != nil {
		return todayMessages{}, err
	}
	pageSize := clampHistoryPageSize(opts.countPerPage)
	params := map[string]any{
		"channel":   channel,
		"oldest":    formatSlackUnixTime(start),
		"latest":    formatSlackUnixTime(end),
		"inclusive": true,
		"limit":     pageSize,
	}

	messages := []slackMessage{}
	for {
		payload, err := client.callAPI(ctx, "conversations.history", params)
		if err != nil {
			return todayMessages{}, err
		}
		if !payload.OK {
			return todayMessages{}, slackAPIError(payload)
		}
		var response conversationHistoryResponse
		if err := json.NewDecoder(bytes.NewReader(payload.Raw)).Decode(&response); err != nil {
			return todayMessages{}, fmt.Errorf("decode conversations.history response: %w", err)
		}
		messages = append(messages, response.Messages...)
		cursor := strings.TrimSpace(response.ResponseMetadata.NextCursor)
		if cursor == "" {
			break
		}
		params["cursor"] = cursor
	}

	sort.Slice(messages, func(left, right int) bool {
		return slackTimestampSeconds(messages[left].TS) < slackTimestampSeconds(messages[right].TS)
	})
	messages = client.expandThreads(ctx, channel, messages, start, end, pageSize)
	users := client.resolveMessageUsers(ctx, channel, messages)
	applyDisplayNames(messages, users)
	return todayMessages{
		Channel:  channel,
		Date:     date,
		Start:    start.Format(time.RFC3339),
		End:      end.Format(time.RFC3339),
		Users:    users,
		Messages: messages,
	}, nil
}

func (client *slackClient) expandThreads(ctx context.Context, channel string, messages []slackMessage, start, end time.Time, pageSize int) []slackMessage {
	expanded := make([]slackMessage, 0, len(messages))
	for _, message := range messages {
		expanded = append(expanded, message)
		if message.ReplyCount <= 0 || (message.ThreadTS != "" && message.ThreadTS != message.TS) {
			continue
		}
		replies := client.loadThreadReplies(ctx, channel, message.TS, pageSize)
		for _, reply := range replies {
			if reply.TS == message.TS {
				continue
			}
			replySeconds := slackTimestampSeconds(reply.TS)
			if replySeconds < start.Unix() || replySeconds >= end.Unix() {
				continue
			}
			reply.IsThreadReply = true
			reply.ParentTS = message.TS
			expanded = append(expanded, reply)
		}
	}
	return expanded
}

func (client *slackClient) loadThreadReplies(ctx context.Context, channel, threadTS string, pageSize int) []slackMessage {
	params := map[string]any{
		"channel": channel,
		"ts":      threadTS,
		"limit":   clampHistoryPageSize(pageSize),
	}
	replies := []slackMessage{}
	for {
		payload, err := client.callAPI(ctx, "conversations.replies", params)
		if err != nil || !payload.OK {
			return replies
		}
		var response conversationHistoryResponse
		if err := json.NewDecoder(bytes.NewReader(payload.Raw)).Decode(&response); err != nil {
			return replies
		}
		replies = append(replies, response.Messages...)
		cursor := strings.TrimSpace(response.ResponseMetadata.NextCursor)
		if cursor == "" {
			break
		}
		params["cursor"] = cursor
	}
	sort.Slice(replies, func(left, right int) bool {
		return slackTimestampSeconds(replies[left].TS) < slackTimestampSeconds(replies[right].TS)
	})
	return replies
}

type userInfoResponse struct {
	OK   bool `json:"ok"`
	User struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Profile struct {
			DisplayName           string `json:"display_name"`
			DisplayNameNormalized string `json:"display_name_normalized"`
			RealName              string `json:"real_name"`
			RealNameNormalized    string `json:"real_name_normalized"`
		} `json:"profile"`
	} `json:"user"`
}

type edgeUsersListResponse struct {
	OK         bool       `json:"ok"`
	Error      string     `json:"error,omitempty"`
	NextMarker string     `json:"next_marker"`
	Results    []edgeUser `json:"results"`
}

type edgeUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RealName string `json:"real_name"`
	Profile  struct {
		DisplayName           string `json:"display_name"`
		DisplayNameNormalized string `json:"display_name_normalized"`
		RealName              string `json:"real_name"`
		RealNameNormalized    string `json:"real_name_normalized"`
	} `json:"profile"`
}

func (client *slackClient) resolveMessageUsers(ctx context.Context, channel string, messages []slackMessage) map[string]string {
	userIDs := map[string]bool{}
	for _, message := range messages {
		if message.User != "" {
			userIDs[message.User] = true
		}
		for _, userID := range message.ReplyUsers {
			if userID != "" {
				userIDs[userID] = true
			}
		}
		for _, match := range slackMentionPattern.FindAllStringSubmatch(message.Text, -1) {
			if len(match) == 2 {
				userIDs[match[1]] = true
			}
		}
	}
	names := map[string]string{}
	if len(userIDs) > 0 {
		for id, name := range client.resolveChannelUsers(ctx, channel) {
			if userIDs[id] && name != "" {
				names[id] = name
			}
		}
	}
	for userID := range userIDs {
		if names[userID] != "" {
			continue
		}
		if name := client.resolveUserName(ctx, userID); name != "" {
			names[userID] = name
		}
	}
	return names
}

func (client *slackClient) resolveChannelUsers(ctx context.Context, channel string) map[string]string {
	if client.teamID == "" || client.token == "" || channel == "" {
		return nil
	}
	names := map[string]string{}
	marker := ""
	for {
		params := map[string]any{
			"token":         client.token,
			"channels":      []string{channel},
			"present_first": true,
			"filter":        "everyone AND NOT bots AND NOT apps",
			"count":         30,
		}
		if marker != "" {
			params["marker"] = marker
		}
		body, err := client.callEdgeAPI(ctx, url.PathEscape(client.teamID)+"/users/list", params)
		if err != nil {
			return names
		}
		var response edgeUsersListResponse
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&response); err != nil || !response.OK {
			return names
		}
		for _, user := range response.Results {
			if user.ID == "" {
				continue
			}
			if name := edgeUserDisplayName(user); name != "" {
				names[user.ID] = name
			}
		}
		marker = strings.TrimSpace(response.NextMarker)
		if marker == "" {
			return names
		}
	}
}

func edgeUserDisplayName(user edgeUser) string {
	return firstNonEmpty(
		user.Profile.DisplayNameNormalized,
		user.Profile.DisplayName,
		user.Profile.RealNameNormalized,
		user.Profile.RealName,
		user.RealName,
		user.Name,
		user.ID,
	)
}

func applyDisplayNames(messages []slackMessage, users map[string]string) {
	for index := range messages {
		if messages[index].User != "" {
			messages[index].DisplayName = users[messages[index].User]
		}
	}
}

func (client *slackClient) resolveUserName(ctx context.Context, userID string) string {
	payload, err := client.callAPI(ctx, "users.info", map[string]any{"user": userID})
	if err != nil || !payload.OK {
		return ""
	}
	var response userInfoResponse
	if err := json.NewDecoder(bytes.NewReader(payload.Raw)).Decode(&response); err != nil {
		return ""
	}
	return firstNonEmpty(
		response.User.Profile.DisplayNameNormalized,
		response.User.Profile.DisplayName,
		response.User.Profile.RealNameNormalized,
		response.User.Profile.RealName,
		response.User.Name,
		response.User.ID,
	)
}

func dayBounds(date string) (string, time.Time, time.Time, error) {
	location, err := time.LoadLocation(stockholmTimeZone)
	if err != nil {
		return "", time.Time{}, time.Time{}, fmt.Errorf("load %s timezone: %w", stockholmTimeZone, err)
	}
	if date == "" {
		now := time.Now().In(location)
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
		return start.Format("2006-01-02"), start, start.AddDate(0, 0, 1), nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", date, location)
	if err != nil || parsed.Format("2006-01-02") != date {
		return "", time.Time{}, time.Time{}, errors.New("--date must use YYYY-MM-DD format")
	}
	start := time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, location)
	return start.Format("2006-01-02"), start, start.AddDate(0, 0, 1), nil
}

func clampHistoryPageSize(value int) int {
	if value < 1 {
		return 200
	}
	if value > 200 {
		return 200
	}
	return value
}

func formatSlackUnixTime(value time.Time) string {
	return strconv.FormatInt(value.Unix(), 10)
}

func messageSender(message slackMessage) string {
	return firstNonEmpty(message.DisplayName, message.Username, message.User, message.BotID, "-")
}

func messageText(message slackMessage) string {
	text := strings.ReplaceAll(message.Text, "\r\n", "\n")
	return strings.TrimSpace(html.UnescapeString(text))
}
