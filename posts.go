package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type searchResponse struct {
	OK       bool `json:"ok"`
	Messages struct {
		Total      int           `json:"total"`
		Matches    []postMessage `json:"matches"`
		Pagination struct {
			TotalCount int `json:"total_count"`
			PageCount  int `json:"page_count"`
		} `json:"pagination"`
		Paging struct {
			Pages int `json:"pages"`
		} `json:"paging"`
	} `json:"messages"`
}

type postMessage struct {
	User      string `json:"user,omitempty"`
	TS        string `json:"ts"`
	Text      string `json:"text"`
	Permalink string `json:"permalink"`
	Channel   struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"channel"`
	Files []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Title string `json:"title"`
	} `json:"files"`
}

func exportMonthPosts(ctx context.Context, client *slackClient, userID string, opts commandOptions) (int, int, string, error) {
	if userID == "" {
		return 0, 0, "", errors.New("Slack user id is missing; run `slack-cli auth login` or `slack-cli auth status`")
	}
	month := opts.month
	if month == "" {
		month = currentMonth()
	}
	after, before, err := monthBounds(month)
	if err != nil {
		return 0, 0, "", err
	}

	query := fmt.Sprintf("from:<@%s> after:%s before:%s", userID, after, before)
	countPerPage := clampPageSize(opts.countPerPage)
	matches := []postMessage{}
	total := 0
	pageCount := 1
	for page := 1; page <= pageCount; page++ {
		payload, err := client.callAPI(ctx, "search.messages", map[string]any{
			"query":    query,
			"count":    countPerPage,
			"page":     page,
			"sort":     "timestamp",
			"sort_dir": "desc",
		})
		if err != nil {
			return 0, 0, "", err
		}
		if !payload.OK {
			return 0, 0, "", slackAPIError(payload)
		}

		var response searchResponse
		if err := json.NewDecoder(bytes.NewReader(payload.Raw)).Decode(&response); err != nil {
			return 0, 0, "", fmt.Errorf("decode search.messages response: %w", err)
		}
		matches = append(matches, response.Messages.Matches...)
		if response.Messages.Total > 0 {
			total = response.Messages.Total
		}
		if response.Messages.Pagination.TotalCount > 0 {
			total = response.Messages.Pagination.TotalCount
		}
		pageCount = firstPositive(response.Messages.Pagination.PageCount, response.Messages.Paging.Pages, pageCount)
	}
	if total == 0 {
		total = len(matches)
	}

	// Search can return nearby context messages. Retain only messages authored by
	// the authenticated user and de-duplicate across pages.
	unique := make(map[string]postMessage, len(matches))
	for _, match := range matches {
		if match.User != userID || match.TS == "" {
			continue
		}
		unique[match.Channel.ID+"/"+match.TS] = match
	}
	matches = matches[:0]
	for _, match := range unique {
		matches = append(matches, match)
	}
	sort.Slice(matches, func(left, right int) bool {
		return slackTimestampSeconds(matches[left].TS) < slackTimestampSeconds(matches[right].TS)
	})

	output := opts.output
	if output == "" {
		ext := ".log"
		if opts.json {
			ext = ".json"
		}
		output = "slack-posts-" + month + ext
	}
	var contents []byte
	if opts.json {
		contents, err = json.MarshalIndent(struct {
			Month    string        `json:"month"`
			Start    string        `json:"start"`
			End      string        `json:"end"`
			UserID   string        `json:"user_id"`
			Messages []postMessage `json:"messages"`
		}{month, after, before, userID, matches}, "", "  ")
		contents = append(contents, '\n')
	} else {
		contents = []byte(renderPostsLog(month, after, before, query, len(matches), matches))
	}
	if err != nil {
		return 0, 0, "", fmt.Errorf("encode posts export: %w", err)
	}
	if err := os.WriteFile(output, contents, 0o600); err != nil {
		return 0, 0, "", fmt.Errorf("write posts export: %w", err)
	}
	if err := os.Chmod(output, 0o600); err != nil {
		return 0, 0, "", fmt.Errorf("secure posts export: %w", err)
	}
	return len(matches), total, output, nil
}

func renderPostsLog(month, after, before, query string, total int, matches []postMessage) string {
	lines := []string{
		"Slack posts export",
		"Month: " + month,
		"Range: " + after + " to " + before,
		"Query: " + query,
		"Exported at: " + time.Now().UTC().Format(time.RFC3339),
		fmt.Sprintf("Total: %d", total),
		"",
	}

	sort.Slice(matches, func(left, right int) bool {
		return slackTimestampSeconds(matches[left].TS) < slackTimestampSeconds(matches[right].TS)
	})
	for index, match := range matches {
		channelName := "unknown"
		if match.Channel.Name != "" {
			channelName = "#" + match.Channel.Name
		} else if match.Channel.ID != "" {
			channelName = match.Channel.ID
		}
		lines = append(lines, fmt.Sprintf("%d. [%s] %s", index+1, formatSlackTimestamp(match.TS), channelName))
		if match.Permalink != "" {
			lines = append(lines, match.Permalink)
		}
		text := normalizeMessageText(match.Text)
		if text == "" {
			text = "(no text)"
		}
		lines = append(lines, text)
		if len(match.Files) > 0 {
			names := []string{}
			for _, file := range match.Files {
				names = append(names, firstNonEmpty(file.Title, file.Name, file.ID))
			}
			lines = append(lines, "Files: "+strings.Join(names, ", "))
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n") + "\n"
}

func currentMonth() string {
	now := time.Now()
	return fmt.Sprintf("%04d-%02d", now.Year(), int(now.Month()))
}

func monthBounds(month string) (string, string, error) {
	start, err := time.Parse("2006-01", month)
	if err != nil || start.Format("2006-01") != month {
		return "", "", errors.New("--month must use YYYY-MM format")
	}
	before := start.AddDate(0, 1, 0)
	return start.Format("2006-01-02"), before.Format("2006-01-02"), nil
}

func clampPageSize(value int) int {
	if value < 1 {
		return 100
	}
	if value > 100 {
		return 100
	}
	return value
}

func formatSlackTimestamp(ts string) string {
	seconds := slackTimestampSeconds(ts)
	if seconds == 0 {
		return ts
	}
	location, err := time.LoadLocation("Europe/Stockholm")
	if err != nil {
		location = time.Local
	}
	return time.Unix(seconds, 0).In(location).Format("2006-01-02 15:04")
}

func slackTimestampSeconds(ts string) int64 {
	prefix, _, _ := strings.Cut(ts, ".")
	seconds, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil {
		return 0
	}
	return seconds
}

func normalizeMessageText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.TrimSpace(html.UnescapeString(text))
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
