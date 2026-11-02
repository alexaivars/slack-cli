package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadFileFetchesPrivateURLWithAuthorization(t *testing.T) {
	contents := "private image bytes"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/files.info":
			if request.Header.Get("Authorization") != "Bearer xoxc-test" {
				t.Errorf("files.info Authorization = %q", request.Header.Get("Authorization"))
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatalf("read files.info request: %v", err)
			}
			values, err := url.ParseQuery(string(body))
			if err != nil || values.Get("file") != "F123" {
				t.Errorf("files.info form = %q", body)
			}
			response.Header().Set("Content-Type", "application/json")
			io.WriteString(response, `{"ok":true,"file":{"id":"F123","name":"screenshot.png","mimetype":"image/png","url_private_download":"`+server.URL+`/private/F123"}}`)
		case "/private/F123":
			if request.Header.Get("Authorization") != "Bearer xoxc-test" {
				t.Errorf("download Authorization = %q", request.Header.Get("Authorization"))
			}
			response.Header().Set("Content-Type", "image/png")
			io.WriteString(response, contents)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "screenshot.png")
	client := newSlackClient(server.URL+"/api", "xoxc-test")
	file, err := client.DownloadFile(context.Background(), "F123", destination, false)
	if err != nil {
		t.Fatalf("DownloadFile returned an error: %v", err)
	}
	if file.ID != "F123" || file.Name != "screenshot.png" {
		t.Fatalf("DownloadFile returned %#v", file)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read download: %v", err)
	}
	if string(got) != contents {
		t.Errorf("download contents = %q, want %q", got, contents)
	}
}

func TestDownloadFileRefusesToOverwrite(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "existing.png")
	if err := os.WriteFile(destination, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write destination: %v", err)
	}
	client := newSlackClient("https://slack.test/api", "xoxc-test")
	_, err := client.DownloadFile(context.Background(), "F123", destination, false)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("DownloadFile error = %v, want existing-file error", err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read existing file: %v", err)
	}
	if string(contents) != "keep" {
		t.Fatalf("existing file contents = %q, want keep", contents)
	}
}
