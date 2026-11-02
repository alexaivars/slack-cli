package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const defaultAPI = "https://slack.com/api"
const defaultEdgeAPI = "https://edgeapi.slack.com/cache"

type slackClient struct {
	baseURL     string
	edgeBaseURL string
	teamID      string
	token       string
	cookies     map[string]string
	httpClient  *http.Client
}

type apiPayload struct {
	OK       bool            `json:"ok"`
	Error    string          `json:"error,omitempty"`
	Needed   string          `json:"needed,omitempty"`
	Provided string          `json:"provided,omitempty"`
	Raw      json.RawMessage `json:"-"`
}

type authInfo struct {
	OK     bool   `json:"ok"`
	URL    string `json:"url,omitempty"`
	Team   string `json:"team,omitempty"`
	User   string `json:"user,omitempty"`
	TeamID string `json:"team_id,omitempty"`
	UserID string `json:"user_id,omitempty"`
}

func newSlackClient(baseURL, token string) *slackClient {
	return &slackClient{
		baseURL:     strings.TrimRight(baseURL, "/"),
		edgeBaseURL: defaultEdgeAPI,
		token:       token,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (client *slackClient) callAPI(ctx context.Context, method string, params map[string]any) (apiPayload, error) {
	form := url.Values{}
	for key, value := range params {
		if value == nil {
			continue
		}
		encoded, err := encodeSlackParam(value)
		if err != nil {
			return apiPayload{}, fmt.Errorf("encode parameter %s: %w", key, err)
		}
		form.Set(key, encoded)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		client.baseURL+"/"+strings.TrimLeft(method, "/"),
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return apiPayload{}, fmt.Errorf("build Slack request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "slack-cli/0.1")
	if len(client.cookies) > 0 {
		request.Header.Set("Cookie", formatCookieHeader(client.cookies))
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return apiPayload{}, fmt.Errorf("Slack request failed: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
	if err != nil {
		return apiPayload{}, fmt.Errorf("read Slack response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return apiPayload{}, fmt.Errorf("Slack request failed (%s): %s", response.Status, strings.TrimSpace(string(body)))
	}

	var payload apiPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return apiPayload{}, fmt.Errorf("decode Slack response: %w", err)
	}
	payload.Raw = append(payload.Raw[:0], body...)
	return payload, nil
}

func (client *slackClient) callEdgeAPI(ctx context.Context, path string, params map[string]any) ([]byte, error) {
	encoded, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("encode Slack edge request: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		client.edgeBaseURL+"/"+strings.TrimLeft(path, "/"),
		bytes.NewReader(encoded),
	)
	if err != nil {
		return nil, fmt.Errorf("build Slack edge request: %w", err)
	}
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	request.Header.Set("User-Agent", "slack-cli/0.1")
	if len(client.cookies) > 0 {
		request.Header.Set("Cookie", formatCookieHeader(client.cookies))
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("Slack edge request failed: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read Slack edge response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Slack edge request failed (%s)", response.Status)
	}
	return body, nil
}

func formatCookieHeader(cookies map[string]string) string {
	names := make([]string, 0, len(cookies))
	for name := range cookies {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+cookies[name])
	}
	return strings.Join(parts, "; ")
}

func (client *slackClient) authTest(ctx context.Context) (authInfo, error) {
	payload, err := client.callAPI(ctx, "auth.test", nil)
	if err != nil {
		return authInfo{}, err
	}
	if !payload.OK {
		return authInfo{}, slackAPIError(payload)
	}
	var info authInfo
	if err := json.NewDecoder(bytes.NewReader(payload.Raw)).Decode(&info); err != nil {
		return authInfo{}, fmt.Errorf("decode auth.test response: %w", err)
	}
	return info, nil
}

func encodeSlackParam(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case bool:
		encoded, err := json.Marshal(typed)
		return string(encoded), err
	case json.RawMessage:
		return string(typed), nil
	case map[string]any, []any:
		encoded, err := json.Marshal(typed)
		return string(encoded), err
	default:
		return fmt.Sprint(typed), nil
	}
}

func slackAPIError(payload apiPayload) error {
	detail := payload.Error
	if detail == "" {
		detail = "unknown_error"
	}
	if payload.Needed != "" {
		detail += " (needed: " + payload.Needed + ")"
	}
	if payload.Provided != "" {
		detail += " (provided: " + payload.Provided + ")"
	}
	return fmt.Errorf("Slack API error: %s", detail)
}
