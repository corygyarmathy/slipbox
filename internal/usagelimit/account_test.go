package usagelimit

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/corygyarmathy/slipbox/internal/provider"
)

// token stands in for Claude Code's OAuth token. It isn't a real one.
const token = "sk-ant-oat01-test-token-not-real"

// transport is a fake http.RoundTripper: it records each request and
// answers with status and body. No request leaves the test.
type transport struct {
	status   int
	body     string
	location string
	requests []*http.Request
}

func (f *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.requests = append(f.requests, req)
	h := http.Header{}
	if f.location != "" {
		h.Set("Location", f.location)
	}
	return &http.Response{StatusCode: f.status, Status: http.StatusText(f.status), Header: h, Body: io.NopCloser(strings.NewReader(f.body)), Request: req}, nil
}

// credentials writes a credentials file holding token, and gives its path.
func credentials(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".credentials.json")
	body := `{"claudeAiOauth":{"accessToken":"` + token + `","refreshToken":"sk-ant-ort01-not-real","expiresAt":1792559400000,"scopes":["user:inference"]}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The token goes to AccountURL only, as a bearer token.
func TestReadAccount_Request(t *testing.T) {
	f := &transport{status: http.StatusOK, body: `{"five_hour":{"utilization":27.0,"resets_at":"2026-10-21T05:10:00+00:00"}}`}
	if _, err := readAccount(context.Background(), f, credentials(t), time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(f.requests) != 1 {
		t.Fatalf("%d requests, want 1", len(f.requests))
	}
	req := f.requests[0]
	if req.Method != http.MethodGet || req.URL.String() != AccountURL {
		t.Errorf("request = %s %s, want GET %s", req.Method, req.URL, AccountURL)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer "+token {
		t.Errorf("Authorization = %q", got)
	}
}

// A redirect isn't followed, so the token goes nowhere else.
func TestReadAccount_NoRedirect(t *testing.T) {
	f := &transport{status: http.StatusFound, location: "https://example.com/usage"}
	_, err := readAccount(context.Background(), f, credentials(t), time.Now())
	if err == nil {
		t.Fatal("read succeeded on a redirect")
	}
	if len(f.requests) != 1 {
		t.Errorf("%d requests, want only the first", len(f.requests))
	}
}

// A failed read says why, and never with the token.
func TestReadAccount_Failures(t *testing.T) {
	missing := filepath.Join(t.TempDir(), ".credentials.json")
	noToken := filepath.Join(t.TempDir(), ".credentials.json")
	if err := os.WriteFile(noToken, []byte(`{"claudeAiOauth":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, credentials string
		f                 *transport
		want              string
	}{
		{"no credentials", missing, &transport{}, "credentials"},
		{"no token", noToken, &transport{}, "no OAuth access token"},
		{"unauthorized", credentials(t), &transport{status: http.StatusUnauthorized, body: `{"error":"` + token + `"}`}, "Unauthorized"},
		{"not JSON", credentials(t), &transport{status: http.StatusOK, body: "<html>"}, "not a JSON object"},
		{"no 5-hour limit", credentials(t), &transport{status: http.StatusOK, body: `{"five_hour":null}`}, "no five_hour limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readAccount(context.Background(), tc.f, tc.credentials, time.Now())
			if err == nil {
				t.Fatal("read succeeded")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not say %q", err, tc.want)
			}
			if strings.Contains(err.Error(), token) {
				t.Errorf("error %q holds the token", err)
			}
		})
	}
}

// The recorded reply gives the 5-hour and weekly limits, as fractions with
// their reset times, and nothing for the objects that aren't usage limits.
func TestReadAccount_RecordedReply(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "oauth-usage", "reply.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 21, 8, 38, 49, 0, time.UTC)
	f := &transport{status: http.StatusOK, body: string(body)}
	got, err := readAccount(context.Background(), f, credentials(t), now)
	if err != nil {
		t.Fatal(err)
	}
	want := &provider.Reading{At: now, Limits: map[string]provider.Limit{
		provider.FiveHour: {Utilization: 0.03, ResetsAt: time.Date(2026, 10, 21, 13, 10, 0, 500118000, time.UTC)},
		provider.SevenDay: {Utilization: 0.27, ResetsAt: time.Date(2026, 10, 24, 9, 0, 0, 500138000, time.UTC)},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("reading = %+v, want %+v", got, want)
	}
}

// A per-model weekly limit is read when the plan has one.
func TestReadAccount_PerModelLimit(t *testing.T) {
	body := `{"five_hour":{"utilization":10.0,"resets_at":"2026-10-21T13:10:00+00:00"},"seven_day_opus":{"utilization":99.0,"resets_at":"2026-10-24T09:00:00+00:00"},"extra_usage":{"utilization":100.0}}`
	got, err := readAccount(context.Background(), &transport{status: http.StatusOK, body: body}, credentials(t), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := got.Limits["seven_day_opus"]; !ok || l.Utilization != 0.99 {
		t.Errorf("limits = %+v, want seven_day_opus at 0.99", got.Limits)
	}
	if _, ok := got.Limits["extra_usage"]; ok {
		t.Errorf("limits = %+v, want no extra_usage", got.Limits)
	}
}
