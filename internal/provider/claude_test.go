package provider_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/corygyarmathy/slipbox/internal/provider"
)

// fakeEnv names the directory the fake `claude` records its call in and
// reads its reply from. When it is set, the test binary is the fake.
const fakeEnv = "S2N_FAKE_CLAUDE_DIR"

func TestMain(m *testing.M) {
	if dir := os.Getenv(fakeEnv); dir != "" {
		os.Exit(fakeClaude(dir))
	}
	os.Exit(m.Run())
}

// fakeClaude is the fake `claude`. It records its arguments, working
// directory, stdin and system prompt file in dir, then prints dir/stdout and
// dir/stderr and exits with the status in dir/exit. A line of dir/stdout
// that is pause isn't printed: the fake waits pauseFor there.
func fakeClaude(dir string) int {
	args := os.Args[1:]
	b, _ := json.Marshal(args)
	os.WriteFile(filepath.Join(dir, "args.json"), b, 0o644)
	cwd, _ := os.Getwd()
	os.WriteFile(filepath.Join(dir, "cwd"), []byte(cwd), 0o644)
	stdin, _ := io.ReadAll(os.Stdin)
	os.WriteFile(filepath.Join(dir, "stdin"), stdin, 0o644)
	for i, a := range args {
		if a == "--system-prompt-file" && i+1 < len(args) {
			if sys, err := os.ReadFile(args[i+1]); err == nil {
				os.WriteFile(filepath.Join(dir, "system"), sys, 0o644)
			}
		}
	}
	out, _ := os.ReadFile(filepath.Join(dir, "stdout"))
	for line := range bytes.Lines(out) {
		if string(line) == pause+"\n" {
			time.Sleep(pauseFor)
			continue
		}
		os.Stdout.Write(line)
	}
	errOut, _ := os.ReadFile(filepath.Join(dir, "stderr"))
	os.Stderr.Write(errOut)
	code, _ := os.ReadFile(filepath.Join(dir, "exit"))
	n, _ := strconv.Atoi(strings.TrimSpace(string(code)))
	return n
}

// pause, as a line of the fake's stdout, makes it wait pauseFor.
const (
	pause    = "PAUSE"
	pauseFor = 300 * time.Millisecond
)

// fake puts a fake `claude` first on PATH that prints stdout and stderr and
// exits with code, and returns the directory it records its call in.
func fake(t *testing.T, stdout, stderr string, code int) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin, rec := t.TempDir(), t.TempDir()
	if err := os.Symlink(exe, filepath.Join(bin, "claude")); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]string{"stdout": stdout, "stderr": stderr, "exit": strconv.Itoa(code)} {
		if err := os.WriteFile(filepath.Join(rec, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(fakeEnv, rec)
	return rec
}

func recorded(t *testing.T, rec, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(rec, name))
	if err != nil {
		t.Fatalf("the fake recorded no %s: %v", name, err)
	}
	return string(b)
}

// probe is the rate_limit_event a probe of `claude -p --output-format
// stream-json --verbose` printed on the maintainer's Pro account.
const probe = `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","resetsAt":1792559400,"rateLimitType":"five_hour","overageStatus":"rejected","overageDisabledReason":"out_of_credits","isUsingOverage":false,"unifiedWindows":{"five_hour":{"utilization":0.27,"resetsAt":1792559400},"seven_day":{"utilization":0.26,"resetsAt":1792832400}}},"uuid":"f4f7f461-f9f9-4ab6-ac0e-abb99bf23ef1","session_id":"f4fad8f6-938b-4e3f-9d1b-60d2be377f06"}`

// rejected is a rate_limit_event refusing a call, built from probe's shape:
// the 5-hour limit is used up.
const rejected = `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":1792559400,"rateLimitType":"five_hour","overageStatus":"rejected","overageDisabledReason":"out_of_credits","isUsingOverage":false,"unifiedWindows":{"five_hour":{"utilization":1,"resetsAt":1792559400},"seven_day":{"utilization":0.41,"resetsAt":1792832400}}},"uuid":"00000000-0000-0000-0000-000000000000","session_id":"00000000-0000-0000-0000-000000000000"}`

// stream is `claude -p --output-format stream-json --verbose` output in the
// shape claude prints it: an init message, then events, then result, one
// message to a line.
func stream(result string, events ...string) string {
	lines := []string{`{"type":"system","subtype":"init","session_id":"00000000-0000-0000-0000-000000000000","model":"claude-opus-5-5"}`}
	lines = append(lines, events...)
	if result != "" {
		lines = append(lines, result)
	}
	return strings.Join(lines, "\n") + "\n"
}

// reply is the `result` message claude prints last, in the shape it prints
// it, after probe.
func reply(isError bool, text string) string {
	return stream(result(isError, text), probe)
}

// result is a `result` message.
func result(isError bool, text string) string {
	b, _ := json.Marshal(map[string]any{
		"type":           "result",
		"subtype":        "success",
		"is_error":       isError,
		"duration_ms":    61234,
		"num_turns":      1,
		"result":         text,
		"session_id":     "00000000-0000-0000-0000-000000000000",
		"total_cost_usd": 0.4321,
		"usage":          map[string]int{"input_tokens": 9, "output_tokens": 4000},
		"modelUsage": map[string]map[string]any{
			"claude-opus-5-5": {
				"inputTokens":              9,
				"outputTokens":             4000,
				"cacheReadInputTokens":     0,
				"cacheCreationInputTokens": 30000,
				"webSearchRequests":        0,
				"costUSD":                  0.43,
			},
			"claude-haiku-5-5": {"inputTokens": 100, "outputTokens": 20},
		},
	})
	return string(b)
}

// repoRoot is the repository root: the nearest directory above the test's
// working directory that holds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}
		dir = parent
	}
}

func TestClaude_CallShape(t *testing.T) {
	rec := fake(t, reply(false, "=== FILE: a.md ===\nnote\n"), "", 0)
	got, err := provider.Claude{}.Call(context.Background(), provider.Request{Model: "opus", Effort: "medium", System: "SYSTEM\n", User: "USER"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "=== FILE: a.md ===\nnote\n" {
		t.Errorf("Text = %q, want the reply's result", got.Text)
	}

	var args []string
	if err := json.Unmarshal([]byte(recorded(t, rec, "args.json")), &args); err != nil {
		t.Fatal(err)
	}
	cwd := recorded(t, rec, "cwd")
	sysFile := ""
	if len(args) == 14 {
		sysFile = args[10]
	}
	want := []string{"-p", "--model", "opus", "--effort", "medium", "--tools", "", "--disable-slash-commands",
		"--strict-mcp-config", "--system-prompt-file", sysFile, "--output-format", "stream-json", "--verbose"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %q, want %q", args, want)
	}
	// No MCP server may load, so every call of a step has the same prefix.
	strict := false
	for _, a := range args {
		switch {
		case a == "--strict-mcp-config":
			strict = true
		case a == "--mcp-config" || strings.HasPrefix(a, "--mcp-config="):
			t.Errorf("args %q pass --mcp-config", args)
		}
	}
	if !strict {
		t.Errorf("args %q lack --strict-mcp-config", args)
	}
	if filepath.Dir(sysFile) != cwd {
		t.Errorf("system prompt file %q is not in the call's directory %q", sysFile, cwd)
	}
	if s := recorded(t, rec, "system"); s != "SYSTEM\n" {
		t.Errorf("system prompt = %q, want %q", s, "SYSTEM\n")
	}
	if s := recorded(t, rec, "stdin"); s != "USER" {
		t.Errorf("stdin = %q, want the user prompt", s)
	}

	root := repoRoot(t)
	if rel, err := filepath.Rel(root, cwd); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Errorf("call ran in %q, inside the repository %q", cwd, root)
	}
	if _, err := os.Stat(cwd); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("call's directory %q left behind (stat err %v)", cwd, err)
	}

	wantModels := map[string]provider.Tokens{
		"claude-opus-5-5":  {InputTokens: 9, CacheCreationInputTokens: 30000, OutputTokens: 4000},
		"claude-haiku-5-5": {InputTokens: 100, OutputTokens: 20},
	}
	if got.Usage.CostUSD != 0.4321 || got.Usage.Turns != 1 || got.Usage.IsError || !reflect.DeepEqual(got.Usage.Models, wantModels) {
		t.Errorf("Usage = %+v", got.Usage)
	}
	if got.Usage.Seconds < 0 {
		t.Errorf("Usage.Seconds = %v", got.Usage.Seconds)
	}
}

// A system prompt over Linux's 128 KiB argument limit reaches claude intact.
func TestClaude_LargeSystemPrompt(t *testing.T) {
	rec := fake(t, reply(false, "ok"), "", 0)
	system := strings.Repeat("Brena’s Dorcanes — a frame line.\n", 200*1024/32)
	if len(system) <= 128*1024 {
		t.Fatalf("system prompt is only %d bytes", len(system))
	}
	if _, err := (provider.Claude{}).Call(context.Background(), provider.Request{Model: "opus", System: system, User: "u"}); err != nil {
		t.Fatal(err)
	}
	if recorded(t, rec, "system") != system {
		t.Error("the system prompt claude read differs from the one sent")
	}
}

// is_error, a non-zero exit and malformed JSON each fail the call, and each
// error says which.
func TestClaude_Failures(t *testing.T) {
	for _, tc := range []struct {
		name, stdout, stderr string
		code                 int
		want                 string
		usage                bool
	}{
		{"is_error", reply(true, "Prompt is too long"), "", 0, "is_error true: Prompt is too long", true},
		{"is_error and exit", reply(true, "API Error: 529"), "", 1, "is_error true: API Error: 529", true},
		{"exit", "", "Invalid API key", 3, "exited with status 3: Invalid API key", false},
		{"exit with a reply", reply(false, "ok"), "killed", 137, "exited with status 137: killed", true},
		{"malformed JSON", "not json", "", 0, "no result message", false},
		{"not a result", stream("", probe), "", 0, "no result message", false},
		{"empty output", "", "", 0, "no result message", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake(t, tc.stdout, tc.stderr, tc.code)
			got, err := provider.Claude{}.Call(context.Background(), provider.Request{Model: "opus", System: "s", User: "u"})
			if err == nil {
				t.Fatal("call succeeded")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not say %q", err, tc.want)
			}
			if hasUsage := got.Usage.Models != nil; hasUsage != tc.usage {
				t.Errorf("failed call's usage = %+v, want usage reported: %v", got.Usage, tc.usage)
			}
			var limited *provider.UsageLimited
			if errors.As(err, &limited) {
				t.Errorf("error %q is a usage-limit refusal", err)
			}
		})
	}
}

func TestClaude_NoCommand(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := provider.Claude{}.Call(context.Background(), provider.Request{})
	if err == nil {
		t.Fatal("call succeeded with no claude to run")
	}
}

// The reading is the last rate_limit_event's: each usage limit's utilization
// and reset time, and the status and overage flag.
func TestClaude_Reading(t *testing.T) {
	older := strings.Replace(probe, `"utilization":0.27`, `"utilization":0.11`, 1)
	fake(t, stream(result(false, "ok"), older, probe), "", 0)
	before := time.Now()
	got, err := provider.Claude{}.Call(context.Background(), provider.Request{Model: "opus", System: "s", User: "u"})
	if err != nil {
		t.Fatal(err)
	}
	r := got.Usage.Reading
	if r == nil {
		t.Fatal("no reading")
	}
	if r.At.Before(before) || r.At.After(time.Now()) {
		t.Errorf("At = %v, want the time of the call", r.At)
	}
	r.At = time.Time{}
	want := &provider.Reading{
		Status: "allowed", Limit: provider.FiveHour, ResetsAt: time.Unix(1792559400, 0).UTC(),
		Limits: map[string]provider.Limit{
			provider.FiveHour: {Utilization: 0.27, ResetsAt: time.Unix(1792559400, 0).UTC()},
			provider.SevenDay: {Utilization: 0.26, ResetsAt: time.Unix(1792832400, 0).UTC()},
		},
	}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("Reading = %+v, want %+v", r, want)
	}
}

// The reading is taken when claude printed its event, as the response
// began, not when the call ended.
func TestClaude_ReadingIsTakenWhenPrinted(t *testing.T) {
	fake(t, stream(result(false, "ok"), probe, pause), "", 0)
	got, err := provider.Claude{}.Call(context.Background(), provider.Request{Model: "opus", System: "s", User: "u"})
	ended := time.Now()
	if err != nil {
		t.Fatal(err)
	}
	if r := got.Usage.Reading; r == nil || !r.At.Before(ended.Add(-pauseFor/2)) {
		t.Errorf("Reading = %+v, want it taken before the fake paused, not at the call's end %v", r, ended)
	}
}

// A call with no rate_limit_event has no reading.
func TestClaude_NoReading(t *testing.T) {
	fake(t, stream(result(false, "ok")), "", 0)
	got, err := provider.Claude{}.Call(context.Background(), provider.Request{Model: "opus", System: "s", User: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Usage.Reading != nil {
		t.Errorf("Reading = %+v, want none", got.Usage.Reading)
	}
}

// A call refused for a usage limit is a *UsageLimited holding the limit's
// reset time: from a rejected status, or, with no event to say so, from the
// result's text.
func TestClaude_UsageLimited(t *testing.T) {
	reset := time.Unix(1792559400, 0).UTC()
	for _, tc := range []struct {
		name, stdout string
		code         int
		limit        string
		resetsAt     time.Time
	}{
		{"rejected", stream(result(true, "You've hit your session limit · resets 7pm"), rejected), 1, provider.FiveHour, reset},
		{"rejected, exit 0", stream(result(true, "Claude AI usage limit reached"), rejected), 0, provider.FiveHour, reset},
		{"rejected, no result", stream("", rejected), 1, provider.FiveHour, reset},
		{"text only", stream(result(true, "You've hit your weekly limit · resets Oct 23")), 1, "", time.Time{}},
		{"text, allowed event", stream(result(true, "You've hit your limit"), probe), 1, provider.FiveHour, reset},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake(t, tc.stdout, "", tc.code)
			got, err := provider.Claude{}.Call(context.Background(), provider.Request{Model: "opus", System: "s", User: "u"})
			var limited *provider.UsageLimited
			if !errors.As(err, &limited) {
				t.Fatalf("error %v is not a *UsageLimited", err)
			}
			if limited.Limit != tc.limit || !limited.ResetsAt.Equal(tc.resetsAt) {
				t.Errorf("UsageLimited = %+v, want limit %q resetting at %v", limited, tc.limit, tc.resetsAt)
			}
			if got.Usage.Seconds < 0 {
				t.Errorf("Usage = %+v", got.Usage)
			}
		})
	}
}

// A successful reply that mentions a limit is not a refusal.
func TestClaude_LimitTextInAReply(t *testing.T) {
	fake(t, stream(result(false, "You've hit your limit, Dorcanes said."), probe), "", 0)
	if _, err := (provider.Claude{}).Call(context.Background(), provider.Request{Model: "opus", System: "s", User: "u"}); err != nil {
		t.Fatal(err)
	}
}
