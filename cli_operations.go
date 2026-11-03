package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type listConversationsOptions struct {
	Types           []string
	ExcludeArchived bool
}

type readMessagesOptions struct {
	Limit          int
	IncludeThreads bool
}

type threadMessages struct {
	Channel  string            `json:"channel"`
	Users    map[string]string `json:"users,omitempty"`
	Messages []slackMessage    `json:"messages"`
}

type searchOptions struct {
	Channel string
	Limit   int
}

type conversation struct {
	ID             string `json:"id"`
	Name           string `json:"name,omitempty"`
	NameNormalized string `json:"name_normalized,omitempty"`
	IsChannel      bool   `json:"is_channel,omitempty"`
	IsGroup        bool   `json:"is_group,omitempty"`
	IsIM           bool   `json:"is_im,omitempty"`
	IsPrivate      bool   `json:"is_private,omitempty"`
	IsMember       bool   `json:"is_member,omitempty"`
	IsArchived     bool   `json:"is_archived,omitempty"`
}

type postedMessage struct {
	Channel string       `json:"channel"`
	TS      string       `json:"ts"`
	Message slackMessage `json:"message"`
}

type searchResult struct {
	ChannelID   string `json:"channel_id,omitempty"`
	ChannelName string `json:"channel_name,omitempty"`
	User        string `json:"user,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Text        string `json:"text,omitempty"`
	TS          string `json:"ts,omitempty"`
	Permalink   string `json:"permalink,omitempty"`
}

type conversationsListResponse struct {
	OK               bool           `json:"ok"`
	Error            string         `json:"error,omitempty"`
	Channels         []conversation `json:"channels"`
	ResponseMetadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

type conversationInfoResponse struct {
	OK      bool         `json:"ok"`
	Error   string       `json:"error,omitempty"`
	Channel conversation `json:"channel"`
}

type conversationsOpenResponse struct {
	OK      bool         `json:"ok"`
	Error   string       `json:"error,omitempty"`
	Channel conversation `json:"channel"`
}

type usersListResponse struct {
	OK               bool        `json:"ok"`
	Error            string      `json:"error,omitempty"`
	Members          []slackUser `json:"members"`
	ResponseMetadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

type slackUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RealName string `json:"real_name"`
	Deleted  bool   `json:"deleted"`
	Profile  struct {
		DisplayName           string `json:"display_name"`
		DisplayNameNormalized string `json:"display_name_normalized"`
		RealName              string `json:"real_name"`
		RealNameNormalized    string `json:"real_name_normalized"`
	} `json:"profile"`
}

type postMessageResponse struct {
	OK      bool         `json:"ok"`
	Error   string       `json:"error,omitempty"`
	Channel string       `json:"channel"`
	TS      string       `json:"ts"`
	Message slackMessage `json:"message"`
}

type searchMessagesResponse struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	Messages struct {
		Matches []struct {
			Type      string `json:"type"`
			User      string `json:"user"`
			Username  string `json:"username"`
			Text      string `json:"text"`
			TS        string `json:"ts"`
			Permalink string `json:"permalink"`
			Channel   struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"channel"`
		} `json:"matches"`
	} `json:"messages"`
}

func (client *slackClient) ListConversations(ctx context.Context, opts listConversationsOptions) ([]conversation, error) {
	types := opts.Types
	if len(types) == 0 {
		types = []string{"public_channel", "private_channel", "mpim", "im"}
	}
	params := map[string]any{
		"types":            strings.Join(types, ","),
		"exclude_archived": opts.ExcludeArchived,
		"limit":            200,
	}
	var conversations []conversation
	for {
		payload, err := client.callAPI(ctx, "conversations.list", params)
		if err != nil {
			return nil, err
		}
		if !payload.OK {
			return nil, slackAPIError(payload)
		}
		var response conversationsListResponse
		if err := json.NewDecoder(bytes.NewReader(payload.Raw)).Decode(&response); err != nil {
			return nil, fmt.Errorf("decode conversations.list response: %w", err)
		}
		conversations = append(conversations, response.Channels...)
		cursor := strings.TrimSpace(response.ResponseMetadata.NextCursor)
		if cursor == "" {
			break
		}
		params["cursor"] = cursor
	}
	return conversations, nil
}

func (client *slackClient) ConversationInfo(ctx context.Context, channel string) (conversation, error) {
	payload, err := client.callAPI(ctx, "conversations.info", map[string]any{"channel": channel})
	if err != nil {
		return conversation{}, err
	}
	if !payload.OK {
		return conversation{}, slackAPIError(payload)
	}
	var response conversationInfoResponse
	if err := json.NewDecoder(bytes.NewReader(payload.Raw)).Decode(&response); err != nil {
		return conversation{}, fmt.Errorf("decode conversations.info response: %w", err)
	}
	if response.Channel.ID == "" {
		response.Channel.ID = channel
	}
	return response.Channel, nil
}

func (client *slackClient) ResolveChannel(ctx context.Context, value string) (conversation, error) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "#"))
	if value == "" {
		return conversation{}, fmt.Errorf("channel is required")
	}
	if isChannelID(value) {
		info, err := client.ConversationInfo(ctx, value)
		if err == nil {
			return info, nil
		}
		return conversation{ID: value}, nil
	}
	conversations, err := client.ListConversations(ctx, listConversationsOptions{ExcludeArchived: true})
	if err != nil {
		return conversation{}, err
	}
	normalized := normalizeChannelLookup(value)
	for _, item := range conversations {
		if normalizeChannelLookup(item.Name) == normalized || normalizeChannelLookup(item.NameNormalized) == normalized {
			return item, nil
		}
	}
	return conversation{}, fmt.Errorf("channel not found: %s", value)
}

func (client *slackClient) ResolveWriteTarget(ctx context.Context, value string) (conversation, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "@") {
		user, err := client.ResolveUser(ctx, strings.TrimPrefix(value, "@"))
		if err != nil {
			return conversation{}, err
		}
		channel, err := client.OpenIM(ctx, user.ID)
		if err != nil {
			return conversation{}, err
		}
		channel.IsIM = true
		if channel.Name == "" {
			channel.Name = slackUserDisplayName(user)
		}
		return channel, nil
	}
	return client.ResolveChannel(ctx, value)
}

func (client *slackClient) ResolveUser(ctx context.Context, value string) (slackUser, error) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "@"))
	if value == "" {
		return slackUser{}, fmt.Errorf("user is required")
	}
	if isUserID(value) {
		return slackUser{ID: value, Name: client.resolveUserName(ctx, value)}, nil
	}
	params := map[string]any{"limit": 200}
	normalized := normalizeUserLookup(value)
	matches := map[string]slackUser{}
	for {
		payload, err := client.callAPI(ctx, "users.list", params)
		if err != nil {
			return slackUser{}, err
		}
		if !payload.OK {
			return slackUser{}, slackAPIError(payload)
		}
		var response usersListResponse
		if err := json.NewDecoder(bytes.NewReader(payload.Raw)).Decode(&response); err != nil {
			return slackUser{}, fmt.Errorf("decode users.list response: %w", err)
		}
		for _, user := range response.Members {
			if user.Deleted || user.ID == "" {
				continue
			}
			if normalizeUserLookup(user.Name) == normalized {
				return user, nil
			}
			if slackUserMatches(user, normalized) {
				matches[user.ID] = user
			}
		}
		cursor := strings.TrimSpace(response.ResponseMetadata.NextCursor)
		if cursor == "" {
			break
		}
		params["cursor"] = cursor
	}
	if len(matches) > 1 {
		return slackUser{}, fmt.Errorf("ambiguous user: @%s; use a username or user ID", value)
	}
	for _, user := range matches {
		return user, nil
	}
	return slackUser{}, fmt.Errorf("user not found: @%s", value)
}

func (client *slackClient) OpenIM(ctx context.Context, userID string) (conversation, error) {
	payload, err := client.callAPI(ctx, "conversations.open", map[string]any{
		"users":     userID,
		"return_im": true,
	})
	if err != nil {
		return conversation{}, err
	}
	if !payload.OK {
		return conversation{}, slackAPIError(payload)
	}
	var response conversationsOpenResponse
	if err := json.NewDecoder(bytes.NewReader(payload.Raw)).Decode(&response); err != nil {
		return conversation{}, fmt.Errorf("decode conversations.open response: %w", err)
	}
	if response.Channel.ID == "" {
		return conversation{}, fmt.Errorf("conversations.open returned no channel")
	}
	return response.Channel, nil
}

func (client *slackClient) ReadMessages(ctx context.Context, channel string, opts readMessagesOptions) (todayMessages, error) {
	limit := clampReadLimit(opts.Limit)
	params := map[string]any{
		"channel": channel,
		"limit":   limit,
	}
	messages := []slackMessage{}
	for len(messages) < limit {
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
		if cursor == "" || len(response.Messages) == 0 {
			break
		}
		params["cursor"] = cursor
		params["limit"] = min(200, limit-len(messages))
	}
	if len(messages) > limit {
		messages = messages[:limit]
	}
	sort.Slice(messages, func(left, right int) bool {
		return slackTimestampSeconds(messages[left].TS) < slackTimestampSeconds(messages[right].TS)
	})
	if opts.IncludeThreads {
		messages = client.expandThreadsWithoutDate(ctx, channel, messages, limit)
	}
	users := client.resolveMessageUsers(ctx, channel, messages)
	applyDisplayNames(messages, users)
	return todayMessages{
		Channel:  channel,
		Users:    users,
		Messages: messages,
	}, nil
}

func (client *slackClient) ReadThread(ctx context.Context, channel, timestamp string) (threadMessages, error) {
	params := map[string]any{
		"channel": channel,
		"ts":      timestamp,
		"limit":   200,
	}
	messages := []slackMessage{}
	for {
		payload, err := client.callAPI(ctx, "conversations.replies", params)
		if err != nil {
			return threadMessages{}, err
		}
		if !payload.OK {
			return threadMessages{}, slackAPIError(payload)
		}
		var response conversationHistoryResponse
		if err := json.NewDecoder(bytes.NewReader(payload.Raw)).Decode(&response); err != nil {
			return threadMessages{}, fmt.Errorf("decode conversations.replies response: %w", err)
		}
		messages = append(messages, response.Messages...)
		cursor := strings.TrimSpace(response.ResponseMetadata.NextCursor)
		if cursor == "" || len(response.Messages) == 0 {
			break
		}
		params["cursor"] = cursor
	}
	if len(messages) == 0 {
		return threadMessages{}, fmt.Errorf("conversations.replies returned no messages for %s", timestamp)
	}
	sort.Slice(messages, func(left, right int) bool {
		return slackTimestampSeconds(messages[left].TS) < slackTimestampSeconds(messages[right].TS)
	})
	parentTS := messages[0].TS
	for index := range messages {
		if messages[index].TS == parentTS {
			continue
		}
		messages[index].IsThreadReply = true
		messages[index].ParentTS = parentTS
	}
	users := client.resolveMessageUsers(ctx, channel, messages)
	applyDisplayNames(messages, users)
	return threadMessages{Channel: channel, Users: users, Messages: messages}, nil
}

func (client *slackClient) expandThreadsWithoutDate(ctx context.Context, channel string, messages []slackMessage, pageSize int) []slackMessage {
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
			reply.IsThreadReply = true
			reply.ParentTS = message.TS
			expanded = append(expanded, reply)
		}
	}
	return expanded
}

func (client *slackClient) PostMessage(ctx context.Context, channel, text string, blocks json.RawMessage) (postedMessage, error) {
	return client.postMessage(ctx, channel, text, "", blocks)
}

func (client *slackClient) ReplyMessage(ctx context.Context, channel, threadTS, text string) (postedMessage, error) {
	return client.postMessage(ctx, channel, text, threadTS, nil)
}

func (client *slackClient) postMessage(ctx context.Context, channel, text, threadTS string, blocks json.RawMessage) (postedMessage, error) {
	params := map[string]any{
		"channel": channel,
		"text":    text,
	}
	if len(blocks) > 0 {
		params["blocks"] = blocks
	}
	if threadTS != "" {
		params["thread_ts"] = threadTS
	}
	payload, err := client.callAPI(ctx, "chat.postMessage", params)
	if err != nil {
		return postedMessage{}, err
	}
	if !payload.OK {
		return postedMessage{}, slackAPIError(payload)
	}
	var response postMessageResponse
	if err := json.NewDecoder(bytes.NewReader(payload.Raw)).Decode(&response); err != nil {
		return postedMessage{}, fmt.Errorf("decode chat.postMessage response: %w", err)
	}
	return postedMessage{Channel: response.Channel, TS: response.TS, Message: response.Message}, nil
}

func (client *slackClient) SearchMessages(ctx context.Context, query string, opts searchOptions) ([]searchResult, error) {
	limit := clampSearchLimit(opts.Limit)
	params := map[string]any{
		"query":    query,
		"count":    limit,
		"sort":     "timestamp",
		"sort_dir": "desc",
	}
	payload, err := client.callAPI(ctx, "search.messages", params)
	if err != nil {
		return nil, err
	}
	if !payload.OK {
		return nil, slackAPIError(payload)
	}
	var response searchMessagesResponse
	if err := json.NewDecoder(bytes.NewReader(payload.Raw)).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode search.messages response: %w", err)
	}
	messages := make([]slackMessage, 0, len(response.Messages.Matches))
	results := make([]searchResult, 0, len(response.Messages.Matches))
	for _, match := range response.Messages.Matches {
		messages = append(messages, slackMessage{User: match.User, Username: match.Username, Text: match.Text, TS: match.TS})
		results = append(results, searchResult{
			ChannelID:   match.Channel.ID,
			ChannelName: match.Channel.Name,
			User:        match.User,
			Text:        match.Text,
			TS:          match.TS,
			Permalink:   match.Permalink,
		})
	}
	users := client.resolveMessageUsers(ctx, opts.Channel, messages)
	for index := range results {
		results[index].DisplayName = users[results[index].User]
	}
	return results, nil
}

func (client *slackClient) JoinChannel(ctx context.Context, channel string) (conversation, error) {
	payload, err := client.callAPI(ctx, "conversations.join", map[string]any{"channel": channel})
	if err != nil {
		return conversation{}, err
	}
	if !payload.OK {
		return conversation{}, slackAPIError(payload)
	}
	var response conversationInfoResponse
	if err := json.NewDecoder(bytes.NewReader(payload.Raw)).Decode(&response); err != nil {
		return conversation{}, fmt.Errorf("decode conversations.join response: %w", err)
	}
	return response.Channel, nil
}

func (client *slackClient) LeaveChannel(ctx context.Context, channel string) error {
	payload, err := client.callAPI(ctx, "conversations.leave", map[string]any{"channel": channel})
	if err != nil {
		return err
	}
	if !payload.OK {
		return slackAPIError(payload)
	}
	return nil
}

func (client *slackClient) CreateChannel(ctx context.Context, name string, private bool) (conversation, error) {
	payload, err := client.callAPI(ctx, "conversations.create", map[string]any{
		"name":       strings.TrimPrefix(strings.TrimSpace(name), "#"),
		"is_private": private,
	})
	if err != nil {
		return conversation{}, err
	}
	if !payload.OK {
		return conversation{}, slackAPIError(payload)
	}
	var response conversationInfoResponse
	if err := json.NewDecoder(bytes.NewReader(payload.Raw)).Decode(&response); err != nil {
		return conversation{}, fmt.Errorf("decode conversations.create response: %w", err)
	}
	return response.Channel, nil
}

func (client *slackClient) DeleteMessage(ctx context.Context, channel, ts string) error {
	payload, err := client.callAPI(ctx, "chat.delete", map[string]any{"channel": channel, "ts": ts})
	if err != nil {
		return err
	}
	if !payload.OK {
		return slackAPIError(payload)
	}
	return nil
}

func (client *slackClient) ArchiveChannel(ctx context.Context, channel string) error {
	payload, err := client.callAPI(ctx, "conversations.archive", map[string]any{"channel": channel})
	if err != nil {
		return err
	}
	if !payload.OK {
		return slackAPIError(payload)
	}
	return nil
}

func isChannelID(value string) bool {
	if value == "" {
		return false
	}
	switch value[0] {
	case 'C', 'G', 'D':
		return true
	default:
		return false
	}
}

func isUserID(value string) bool {
	if value == "" {
		return false
	}
	switch value[0] {
	case 'U', 'W':
		return true
	default:
		return false
	}
}

func normalizeChannelLookup(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "#"))
	value = strings.ReplaceAll(value, "_", "-")
	return strings.ToLower(value)
}

func normalizeUserLookup(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "@"))
	return strings.ToLower(value)
}

func slackUserMatches(user slackUser, normalized string) bool {
	values := []string{
		user.Name,
		user.RealName,
		user.Profile.DisplayName,
		user.Profile.DisplayNameNormalized,
		user.Profile.RealName,
		user.Profile.RealNameNormalized,
	}
	for _, value := range values {
		if normalizeUserLookup(value) == normalized {
			return true
		}
	}
	return false
}

func slackUserDisplayName(user slackUser) string {
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

func clampReadLimit(value int) int {
	if value < 1 {
		return 20
	}
	if value > 200 {
		return 200
	}
	return value
}

func clampSearchLimit(value int) int {
	if value < 1 {
		return 20
	}
	if value > 100 {
		return 100
	}
	return value
}
