package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadFileFetchesPrivateURLWithAuthorization(t *testing.T) {
	contents := "private image bytes"
	client := newSlackClient("https://slack.test/api", "xoxc-test")
	client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Bearer xoxc-test" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/api/files.info":
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			values, err := url.ParseQuery(string(body))
			if err != nil || values.Get("file") != "F123" {
				t.Fatalf("files.info form = %q", body)
			}
			return jsonResponse(`{"ok":true,"file":{"id":"F123","name":"screenshot.png","url_private_download":"https://files.slack.com/private/F123"}}`), nil
		case "/private/F123":
			response := jsonResponse("")
			response.StatusCode = http.StatusFound
			response.Header.Set("Location", "https://files.slack.com/download/F123")
			return response, nil
		case "/download/F123":
			return jsonResponse(contents), nil
		default:
			t.Fatalf("unexpected request: %s", request.URL)
			return nil, nil
		}
	})
	destination := filepath.Join(t.TempDir(), "screenshot.png")
	file, err := client.DownloadFile(t.Context(), "F123", destination, false)
	if err != nil {
		t.Fatalf("DownloadFile returned an error: %v", err)
	}
	if file.ID != "F123" || file.Name != "screenshot.png" {
		t.Fatalf("DownloadFile returned %#v", file)
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
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

func TestDownloadFileRejectsUntrustedURLsBeforeSendingCredentials(t *testing.T) {
	for _, target := range []string{
		"http://files.slack.com/file",
		"https://untrusted.example/file",
		"https://files.slack.com.untrusted.example/file",
		"https://sub.files.slack.com/file",
		"https://files.slack.com:444/file",
		"https://user@files.slack.com/file",
	} {
		for _, redirect := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/redirect=%t", target, redirect), func(t *testing.T) {
				client := newSlackClient("https://slack.test/api", "xoxc-test")
				client.cookies = map[string]string{"d": "test-cookie"}
				attempted := false
				client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
					if request.URL.Path == "/api/files.info" {
						fileURL := target
						if redirect {
							fileURL = "https://files.slack.com/start"
						}
						return jsonResponse(fmt.Sprintf(`{"ok":true,"file":{"id":"F123","url_private_download":%q}}`, fileURL)), nil
					}
					if redirect && request.URL.String() == "https://files.slack.com/start" {
						response := jsonResponse("")
						response.StatusCode = http.StatusFound
						response.Header.Set("Location", target)
						return response, nil
					}
					attempted = true
					return jsonResponse("private file"), nil
				})
				_, err := client.DownloadFile(t.Context(), "F123", filepath.Join(t.TempDir(), "file"), false)
				if err == nil || attempted {
					t.Fatalf("untrusted download: error=%v, request sent=%t; want rejection before request", err, attempted)
				}
			})
		}
	}
}

func TestDownloadFilePreservesDestinationCreatedDuringDownload(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		t.Run(fmt.Sprintf("overwrite=%t", overwrite), func(t *testing.T) {
			destination := filepath.Join(t.TempDir(), "file")
			client := newSlackClient("https://slack.test/api", "xoxc-test")
			client.httpClient.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path == "/api/files.info" {
					if err := os.WriteFile(destination, []byte("concurrent file"), 0o600); err != nil {
						t.Fatal(err)
					}
					return jsonResponse(`{"ok":true,"file":{"id":"F123","url_private_download":"https://files.slack.com/file"}}`), nil
				}
				return jsonResponse("downloaded bytes"), nil
			})
			_, err := client.DownloadFile(t.Context(), "F123", destination, overwrite)
			want := "concurrent file"
			if overwrite {
				want = "downloaded bytes"
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, os.ErrExist) {
				t.Fatalf("download error = %v, want existing-file error", err)
			}
			contents, err := os.ReadFile(destination)
			if err != nil || string(contents) != want {
				t.Fatalf("destination = %q, %v; want %q", contents, err, want)
			}
		})
	}
}
