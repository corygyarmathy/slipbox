package usagelimit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/corygyarmathy/slipbox/internal/provider"
)

// AccountURL is where the account's usage is read: what Claude Code's
// /usage reads. It is undocumented.
const AccountURL = "https://api.anthropic.com/api/oauth/usage"

// accountHost is the one host the OAuth token is sent to.
const accountHost = "api.anthropic.com"

// ReadAccount reads the account's usage limits from AccountURL with Claude
// Code's own OAuth token, from .credentials.json in $CLAUDE_CONFIG_DIR or
// ~/.claude. It sends the token only to AccountURL, follows no redirect, and
// never logs, writes or refreshes it: an expired token fails the read, and
// claude refreshes it on its next call.
func ReadAccount(ctx context.Context) (*provider.Reading, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}
	return readAccount(ctx, http.DefaultTransport, path, time.Now())
}

// readAccount is ReadAccount with the transport, the credentials file and
// the time the reading is taken given.
func readAccount(ctx context.Context, transport http.RoundTripper, credentials string, now time.Time) (*provider.Reading, error) {
	token, err := readToken(credentials)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, AccountURL, nil)
	if err != nil {
		return nil, err
	}
	if req.URL.Scheme != "https" || req.URL.Host != accountHost {
		return nil, fmt.Errorf("%s is not on %s, so the token isn't sent there", AccountURL, accountHost)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", AccountURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("reading %s: %s", AccountURL, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", AccountURL, err)
	}
	return parseAccount(body, now)
}

// credentialsPath is Claude Code's credentials file.
func credentialsPath() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".credentials.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", ".credentials.json"), nil
}

// readToken gives the OAuth access token in the credentials file at path.
// Its errors never hold the token.
func readToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("Claude Code's credentials: %w", err)
	}
	var creds struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(b, &creds); err != nil {
		return "", errors.New("Claude Code's credentials " + path + " are not the JSON s2n reads")
	}
	if creds.ClaudeAiOauth.AccessToken == "" {
		return "", errors.New("Claude Code's credentials " + path + " hold no OAuth access token")
	}
	return creds.ClaudeAiOauth.AccessToken, nil
}

// accountLimits are the usage limits parseAccount reads from AccountURL's
// reply, by their names there, which are the names a rate_limit_event gives
// them: the 5-hour and weekly limits, and the per-model weekly ones. The
// reply holds other objects with a utilization, such as extra_usage, the
// credits spent past the plan, and codenamed ones; none is a limit a call is
// refused for, and extra_usage has no reset time.
var accountLimits = []string{provider.FiveHour, provider.SevenDay, "seven_day_opus", "seven_day_sonnet"}

// parseAccount gives the reading in AccountURL's reply body, taken at now.
// Each usage limit is an object with a utilization, in percent, and an
// RFC 3339 resets_at, or null for a limit the plan doesn't have
// (testdata/oauth-usage/).
func parseAccount(body []byte, now time.Time) (*provider.Reading, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, fmt.Errorf("the reply from %s is not a JSON object: %w", AccountURL, err)
	}
	r := &provider.Reading{At: now, Limits: map[string]provider.Limit{}}
	for _, name := range accountLimits {
		var l *struct {
			Utilization *float64   `json:"utilization"`
			ResetsAt    *time.Time `json:"resets_at"`
		}
		if raw, ok := fields[name]; !ok || json.Unmarshal(raw, &l) != nil || l == nil || l.Utilization == nil {
			continue
		}
		limit := provider.Limit{Utilization: *l.Utilization / 100}
		if l.ResetsAt != nil {
			limit.ResetsAt = l.ResetsAt.UTC()
		}
		r.Limits[name] = limit
	}
	if _, ok := r.Limits[provider.FiveHour]; !ok {
		return nil, fmt.Errorf("the reply from %s gives no %s limit", AccountURL, provider.FiveHour)
	}
	return r, nil
}
