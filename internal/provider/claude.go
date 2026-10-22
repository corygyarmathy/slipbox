package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Claude is the real adapter: each call runs `claude -p` in the shape the
// trials called it, with MCP servers kept out (below). It is safe for
// concurrent use.
//
// The call runs in a fresh temporary directory under $TMPDIR, which is taken
// to be outside any repository, so no CLAUDE.md or project memory loads and
// the model reads only the brief, the frame and the prompt, as in the
// trials. It passes --strict-mcp-config and no --mcp-config,
// so no MCP server loads either: the account's claude.ai connectors would
// otherwise put about 3.6k tokens before the system prompt on some calls and
// not others, and a step's later calls couldn't read its first call's cache
// (ADR 0004).
//
// The system prompt goes in a file there and is passed as
// --system-prompt-file, because a frame can outgrow Linux's 128 KiB limit on
// one argument. The user prompt goes on stdin.
type Claude struct{}

// systemPromptFile is the system prompt's name in the call's directory.
const systemPromptFile = "system-prompt.md"

// Call runs one `claude -p` call. A reply with is_error true, a non-zero exit
// or output with no result message each fail the call with an error saying
// which. A failed call's Reply still holds its Usage: the seconds it took,
// and whatever else claude reported, so the spend is still logged.
//
// claude prints its output as stream-json: one JSON message per line. The
// reply and its usage are the final `result` message's, and the Reading is
// the last `rate_limit_event`'s, taken when claude printed it. The account
// sends that event as the response begins, so a reading taken when the call
// ended would date it after the calls that ended while this one ran, and
// spend they made would seem to be in it. A failed call is a *UsageLimited when that
// event's status is "rejected" or, as a backstop for a refusal that comes
// with no event, when its result says it hit a limit.
func (Claude) Call(ctx context.Context, req Request) (Reply, error) {
	dir, err := os.MkdirTemp("", "s2n-call-")
	if err != nil {
		return Reply{}, fmt.Errorf("claude: %w", err)
	}
	defer os.RemoveAll(dir)
	system := filepath.Join(dir, systemPromptFile)
	if err := os.WriteFile(system, []byte(req.System), 0o600); err != nil {
		return Reply{}, fmt.Errorf("claude: %w", err)
	}

	cmd := exec.CommandContext(ctx, "claude", "-p",
		"--model", req.Model,
		"--effort", req.Effort,
		"--tools", "",
		"--disable-slash-commands",
		"--strict-mcp-config",
		"--system-prompt-file", system,
		"--output-format", "stream-json",
		"--verbose")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(req.User)
	var (
		stdout printed
		stderr bytes.Buffer
	)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	runErr := cmd.Run()
	seconds := math.Round(time.Since(start).Seconds()*10) / 10

	if err := ctx.Err(); err != nil {
		return Reply{Usage: Usage{Seconds: seconds}}, err
	}
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return Reply{Usage: Usage{Seconds: seconds}}, fmt.Errorf("claude: %w", runErr)
	}
	out, reading := readStream(stdout.lines())
	failed := Reply{Usage: Usage{Seconds: seconds, Reading: reading}}
	if out == nil {
		if limited := refusal(reading, ""); limited != nil {
			return failed, limited
		}
		if exitErr != nil {
			return failed, fmt.Errorf("claude exited with status %d: %s", exitErr.ExitCode(), tail(stderr.Bytes()))
		}
		return failed, fmt.Errorf("claude printed no result message: %s", tail(stdout.buf.Bytes()))
	}

	reply := Reply{Text: out.Result, Usage: out.usage(seconds)}
	reply.Usage.Reading = reading
	if out.IsError || exitErr != nil {
		if limited := refusal(reading, out.Result); limited != nil {
			return reply, limited
		}
	}
	switch {
	case out.IsError:
		return reply, fmt.Errorf("claude replied with is_error true: %s", tail([]byte(out.Result)))
	case exitErr != nil:
		return reply, fmt.Errorf("claude exited with status %d: %s", exitErr.ExitCode(), tail(stderr.Bytes()))
	}
	return reply, nil
}

// limitText is how a refusal for a usage limit reads, such as "You've hit
// your session limit".
var limitText = regexp.MustCompile(`(?i)\bhit your\b.*\blimit\b`)

// refusal is the *UsageLimited a failed call is, given its reading and its
// result text, or nil if it was refused for something else.
func refusal(reading *Reading, text string) *UsageLimited {
	switch {
	case reading != nil && reading.Status == "rejected":
		return &UsageLimited{Limit: reading.Limit, ResetsAt: reading.ResetsAt, Text: text}
	case limitText.MatchString(text):
		limited := &UsageLimited{Text: text}
		if reading != nil {
			limited.Limit, limited.ResetsAt = reading.Limit, reading.ResetsAt
		}
		return limited
	}
	return nil
}

// printed is an io.Writer that keeps what claude prints and when it printed
// each line.
type printed struct {
	buf bytes.Buffer
	// ends holds the offset just past each line's newline, and when it was
	// written.
	ends []lineEnd
}

type lineEnd struct {
	off int
	at  time.Time
}

func (l *printed) Write(p []byte) (int, error) {
	now := time.Now()
	for i, b := range p {
		if b == '\n' {
			l.ends = append(l.ends, lineEnd{l.buf.Len() + i + 1, now})
		}
	}
	return l.buf.Write(p)
}

// line is one line claude printed, and when.
type line struct {
	b  []byte
	at time.Time
}

// lines gives each line written, and a last one with no newline, dated now.
func (l *printed) lines() []line {
	var out []line
	b, off := l.buf.Bytes(), 0
	for _, e := range l.ends {
		out = append(out, line{b[off:e.off], e.at})
		off = e.off
	}
	if off < len(b) {
		out = append(out, line{b[off:], time.Now()})
	}
	return out
}

// readStream reads claude's stream-json output: its last `result` message,
// or nil if it printed none, and the reading its last `rate_limit_event`
// gives, taken when that line was printed, or nil if it printed none. A line
// that isn't a JSON message is skipped.
func readStream(lines []line) (*claudeOutput, *Reading) {
	var (
		out     *claudeOutput
		reading *Reading
	)
	for _, l := range lines {
		var msg struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(l.b, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "result":
			var o claudeOutput
			if json.Unmarshal(l.b, &o) == nil {
				out = &o
			}
		case "rate_limit_event":
			var e rateLimitEvent
			if json.Unmarshal(l.b, &e) == nil {
				reading = e.Info.reading(l.at)
			}
		}
	}
	return out, reading
}

// rateLimitEvent is the part of a `rate_limit_event` message a call reads.
// Its resetsAt times are Unix seconds. unifiedWindows is marked internal in
// Claude Code's schema, so a reading may come without it.
type rateLimitEvent struct {
	Info rateLimitInfo `json:"rate_limit_info"`
}

type rateLimitInfo struct {
	Status         string `json:"status"`
	ResetsAt       int64  `json:"resetsAt"`
	RateLimitType  string `json:"rateLimitType"`
	IsUsingOverage bool   `json:"isUsingOverage"`
	UnifiedWindows map[string]struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    int64   `json:"resetsAt"`
	} `json:"unifiedWindows"`
}

func (i rateLimitInfo) reading(at time.Time) *Reading {
	r := &Reading{At: at, Status: i.Status, Limit: i.RateLimitType, ResetsAt: unixTime(i.ResetsAt), UsingOverage: i.IsUsingOverage}
	if len(i.UnifiedWindows) > 0 {
		r.Limits = make(map[string]Limit, len(i.UnifiedWindows))
		for name, l := range i.UnifiedWindows {
			r.Limits[name] = Limit{Utilization: l.Utilization, ResetsAt: unixTime(l.ResetsAt)}
		}
	}
	return r
}

// unixTime is the time sec Unix seconds give, or zero for 0.
func unixTime(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

// claudeOutput is the part of the `result` message a call reads.
type claudeOutput struct {
	Type       string  `json:"type"`
	Result     string  `json:"result"`
	IsError    bool    `json:"is_error"`
	NumTurns   int     `json:"num_turns"`
	TotalCost  float64 `json:"total_cost_usd"`
	ModelUsage map[string]struct {
		InputTokens              int `json:"inputTokens"`
		CacheCreationInputTokens int `json:"cacheCreationInputTokens"`
		CacheReadInputTokens     int `json:"cacheReadInputTokens"`
		OutputTokens             int `json:"outputTokens"`
	} `json:"modelUsage"`
}

func (r claudeOutput) usage(seconds float64) Usage {
	u := Usage{Seconds: seconds, CostUSD: r.TotalCost, IsError: r.IsError, Turns: r.NumTurns}
	if len(r.ModelUsage) > 0 {
		u.Models = make(map[string]Tokens, len(r.ModelUsage))
		for m, t := range r.ModelUsage {
			u.Models[m] = Tokens{
				InputTokens:              t.InputTokens,
				CacheCreationInputTokens: t.CacheCreationInputTokens,
				CacheReadInputTokens:     t.CacheReadInputTokens,
				OutputTokens:             t.OutputTokens,
			}
		}
	}
	return u
}

// tail is the last part of b, trimmed, for an error message.
func tail(b []byte) string {
	const max = 500
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "(no output)"
	}
	if len(s) > max {
		s = "…" + s[len(s)-max:]
	}
	return s
}
