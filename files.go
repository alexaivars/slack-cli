package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type slackFile struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Mimetype           string `json:"mimetype"`
	URLPrivateDownload string `json:"url_private_download"`
}

type fileInfoResponse struct {
	File slackFile `json:"file"`
}

func (client *slackClient) FileInfo(ctx context.Context, fileID string) (slackFile, error) {
	payload, err := client.callAPI(ctx, "files.info", map[string]any{"file": fileID})
	if err != nil {
		return slackFile{}, err
	}
	if !payload.OK {
		return slackFile{}, slackAPIError(payload)
	}
	var response fileInfoResponse
	if err := json.NewDecoder(bytes.NewReader(payload.Raw)).Decode(&response); err != nil {
		return slackFile{}, fmt.Errorf("decode files.info response: %w", err)
	}
	if response.File.ID == "" || response.File.URLPrivateDownload == "" {
		return slackFile{}, errors.New("files.info response did not include a downloadable file")
	}
	return response.File, nil
}

func (client *slackClient) DownloadFile(ctx context.Context, fileID, destination string, overwrite bool) (slackFile, error) {
	if destination == "" {
		return slackFile{}, errors.New("download destination is required")
	}
	if info, err := os.Stat(destination); err == nil {
		if info.IsDir() {
			return slackFile{}, fmt.Errorf("download destination %q is a directory", destination)
		}
		if !overwrite {
			return slackFile{}, fmt.Errorf("download destination %q already exists; pass --overwrite to replace it", destination)
		}
	} else if !os.IsNotExist(err) {
		return slackFile{}, fmt.Errorf("inspect download destination %q: %w", destination, err)
	}
	file, err := client.FileInfo(ctx, fileID)
	if err != nil {
		return slackFile{}, err
	}

	if err := validateSlackDownloadURL(file.URLPrivateDownload); err != nil {
		return slackFile{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URLPrivateDownload, nil)
	if err != nil {
		return slackFile{}, fmt.Errorf("build file download request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "slack-cli/0.1")
	if len(client.cookies) > 0 {
		request.Header.Set("Cookie", formatCookieHeader(client.cookies))
	}

	downloadClient := *client.httpClient
	downloadClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many download redirects")
		}
		return validateSlackDownloadURL(request.URL.String())
	}
	response, err := downloadClient.Do(request)
	if err != nil {
		return slackFile{}, fmt.Errorf("download Slack file: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return slackFile{}, fmt.Errorf("download Slack file failed (%s): %s", response.Status, strings.TrimSpace(string(body)))
	}

	temporary, err := os.CreateTemp(filepath.Dir(destination), ".slack-cli-download-*")
	if err != nil {
		return slackFile{}, fmt.Errorf("create temporary download file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, response.Body); err != nil {
		temporary.Close()
		return slackFile{}, fmt.Errorf("write download: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return slackFile{}, fmt.Errorf("finish download: %w", err)
	}
	// Linking publishes the completed download without replacing a concurrent file.
	publish := os.Link
	if overwrite {
		publish = os.Rename
	}
	if err := publish(temporaryPath, destination); err != nil {
		return slackFile{}, fmt.Errorf("save download to %q: %w", destination, err)
	}
	return file, nil
}

func validateSlackDownloadURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "files.slack.com") || parsed.User != nil {
		return errors.New("download URL must use HTTPS on files.slack.com without user information")
	}
	return nil
}
