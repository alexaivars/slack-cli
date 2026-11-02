package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

type browserLoginOptions struct {
	browserProfileDir string
	workspace         string
	headless          bool
	timeout           time.Duration
	verbose           bool
}

type capturedSlackSession struct {
	token      string
	cookies    map[string]string
	teamDomain string
	expiresAt  int64
}

func captureSlackSession(opts browserLoginOptions) (capturedSlackSession, error) {
	if opts.browserProfileDir == "" {
		return capturedSlackSession{}, errors.New("browser profile directory is required")
	}
	if opts.timeout == 0 {
		opts.timeout = 5 * time.Minute
	}
	if err := os.MkdirAll(opts.browserProfileDir, 0o700); err != nil {
		return capturedSlackSession{}, fmt.Errorf("create browser profile directory: %w", err)
	}
	state := &browserCaptureState{
		cookies:    map[string]string{},
		teamDomain: opts.workspace,
		verbose:    opts.verbose,
	}

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserDataDir(opts.browserProfileDir),
		chromedp.WindowSize(1440, 1000),
		chromedp.Flag("headless", opts.headless),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	)
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	defer cancelAllocator()
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer func() {
		closeCtx, cancelClose := context.WithTimeout(browserCtx, 5*time.Second)
		defer cancelClose()
		_ = browser.Close().Do(closeCtx)
		_ = chromedp.Cancel(browserCtx)
		cancelBrowser()
	}()

	targetURL := "https://app.slack.com/client"
	if opts.workspace != "" {
		targetURL = "https://" + opts.workspace + ".slack.com/"
	}
	if err := chromedp.Run(browserCtx,
		network.Enable(),
		chromedp.Navigate(targetURL),
	); err != nil {
		return capturedSlackSession{}, fmt.Errorf("open Slack login browser: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Log in to Slack in the opened browser window.")
	fmt.Fprintln(os.Stderr, "After Slack loads, this command will capture the active web API token.")

	deadlineCtx, cancelDeadline := context.WithTimeout(browserCtx, opts.timeout)
	defer cancelDeadline()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := updateStateFromChromedp(deadlineCtx, state); err != nil && opts.verbose {
			fmt.Fprintf(os.Stderr, "storage extraction not ready: %v\n", err)
		}
		if state.tokenValue() != "" && state.hasCookie("d") {
			break
		}
		select {
		case <-deadlineCtx.Done():
			return capturedSlackSession{}, errors.New("timed out waiting for Slack localConfig_v2 token; make sure Slack is logged in and a workspace is open")
		case <-ticker.C:
		}
	}
	if err := updateStateFromChromedp(browserCtx, state); err != nil && opts.verbose {
		fmt.Fprintf(os.Stderr, "warning: final browser state capture failed: %v\n", err)
	}
	if state.tokenValue() == "" {
		return capturedSlackSession{}, errors.New("captured no xoxc token from localConfig_v2")
	}
	if !state.hasCookie("d") {
		return capturedSlackSession{}, errors.New("captured xoxc token but no Slack d cookie; keep Slack open until cookies load")
	}
	return capturedSlackSession{
		token:      state.tokenValue(),
		cookies:    state.cookieValues(),
		teamDomain: state.teamDomainValue(),
		expiresAt:  state.expiresAtValue(),
	}, nil
}

type browserCaptureState struct {
	mu         sync.Mutex
	token      string
	cookies    map[string]string
	expiries   []int64
	teamDomain string
	verbose    bool
}

func (state *browserCaptureState) captureToken(value string, source string) {
	if value == "" || !strings.HasPrefix(value, "xoxc-") {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.token == "" {
		state.token = value
		if state.verbose {
			fmt.Fprintf(os.Stderr, "captured xoxc token from %s\n", source)
		}
	}
}

func (state *browserCaptureState) tokenValue() string {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.token
}

func (state *browserCaptureState) cookieValues() map[string]string {
	state.mu.Lock()
	defer state.mu.Unlock()
	cookies := map[string]string{}
	for key, value := range state.cookies {
		cookies[key] = value
	}
	return cookies
}

func (state *browserCaptureState) hasCookie(name string) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.cookies[name] != ""
}

func (state *browserCaptureState) teamDomainValue() string {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.teamDomain
}

func (state *browserCaptureState) expiresAtValue() int64 {
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.expiries) == 0 {
		return 0
	}
	expiresAt := state.expiries[0]
	for _, expiry := range state.expiries[1:] {
		if expiry < expiresAt {
			expiresAt = expiry
		}
	}
	return expiresAt
}

func (state *browserCaptureState) trackExpiryLocked(expiry int64) {
	for _, tracked := range state.expiries {
		if tracked == expiry {
			return
		}
	}
	state.expiries = append(state.expiries, expiry)
}

type slackStorageTokenResult struct {
	Token  string `json:"token"`
	TeamID string `json:"teamID"`
}

// This originally used Playwright request/response listeners because
// Playwright's request.allHeaders(), request.postData(), and response.text()
// made Slack's browser token easy to observe. The token that worked in
// practice still came from browser storage, so this chromedp version avoids
// timing-sensitive network scraping and reads Slack's known localStorage shape
// directly: localConfig_v2 -> teams[activeTeamID].token. The required d cookie
// is then collected through CDP Network.getCookies.
func updateStateFromChromedp(ctx context.Context, state *browserCaptureState) error {
	var currentURL string
	var result slackStorageTokenResult
	if err := chromedp.Run(ctx,
		chromedp.Location(&currentURL),
		chromedp.Evaluate(slackLocalConfigTokenExpression, &result),
	); err != nil {
		return err
	}
	if currentURL != "" {
		if parsed, err := url.Parse(currentURL); err == nil {
			if strings.HasSuffix(parsed.Hostname(), ".slack.com") && parsed.Hostname() != "app.slack.com" {
				state.mu.Lock()
				state.teamDomain = strings.TrimSuffix(parsed.Hostname(), ".slack.com")
				state.mu.Unlock()
			}
		}
	}
	if result.Token != "" {
		firstCapture := state.tokenValue() == ""
		state.captureToken(result.Token, "localConfig_v2")
		if firstCapture && result.TeamID != "" && state.verbose {
			fmt.Fprintf(os.Stderr, "captured Slack token for team %s\n", result.TeamID)
		}
	}

	cookieURLs := []string{"https://slack.com", "https://app.slack.com"}
	if domain := state.teamDomainValue(); domain != "" {
		cookieURLs = append(cookieURLs, "https://"+domain+".slack.com")
	}
	var cookies []*network.Cookie
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		cookies, err = network.GetCookies().WithURLs(cookieURLs).Do(ctx)
		return err
	})); err != nil {
		return err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	for _, cookie := range cookies {
		if cookie.Name == "" || cookie.Value == "" || !strings.Contains(cookie.Domain, "slack.com") {
			continue
		}
		state.cookies[cookie.Name] = cookie.Value
		if state.verbose && cookie.Name == "d" {
			fmt.Fprintln(os.Stderr, "captured Slack d cookie")
		}
		if cookie.Expires > 0 {
			state.trackExpiryLocked(int64(cookie.Expires))
		}
	}
	return nil
}

const slackLocalConfigTokenExpression = `(() => {
  const empty = { token: "", teamID: "" };
  const raw = window.localStorage && window.localStorage.getItem("localConfig_v2");
  if (!raw) return empty;

  let config;
  try {
    config = JSON.parse(raw);
  } catch {
    return empty;
  }

  const teams = config && config.teams && typeof config.teams === "object" ? config.teams : {};
  const pathMatch = String(document.location.pathname || "").match(/^\/client\/([A-Z0-9]+)/);
  const activeTeamID = pathMatch ? pathMatch[1] : "";

  const findToken = (team) => {
    if (!team || typeof team !== "object") return "";
    if (typeof team.token === "string" && /^xoxc-/.test(team.token)) return team.token;
    if (typeof team.api_token === "string" && /^xoxc-/.test(team.api_token)) return team.api_token;
    if (typeof team.enterprise_api_token === "string" && /^xoxc-/.test(team.enterprise_api_token)) return team.enterprise_api_token;
    for (const value of Object.values(team)) {
      if (typeof value === "string" && /^xoxc-/.test(value)) return value;
    }
    return "";
  };

  if (activeTeamID && teams[activeTeamID]) {
    const token = findToken(teams[activeTeamID]);
    if (token) return { token, teamID: activeTeamID };
  }

  for (const [teamID, team] of Object.entries(teams)) {
    const token = findToken(team);
    if (token) return { token, teamID };
  }

  return empty;
})()`
