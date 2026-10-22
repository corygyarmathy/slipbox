// Package provider is the seam between the run and the model (ADR 0002 §4).
// A call takes a model, a system prompt and a user prompt, and returns the
// reply text and its usage. Real runs use Claude, the `claude -p` adapter;
// tests use Replay, which serves recorded replies and never reaches a model.
//
// A call's usage holds its Reading: each usage limit's utilization and reset
// time as the call's response began, as `claude -p` reports them. A call the account
// refused for a usage limit fails with a *UsageLimited, which says when the
// limit resets. The provider only reports and classifies: what the run does
// about a usage limit is package usagelimit's (ADR 0011).
package provider

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Request is one model call.
type Request struct {
	Model string
	// Effort is the call's effort level, one of Efforts. Every call states
	// one, so a run's thinking spend doesn't depend on the settings of
	// whoever runs it.
	Effort string
	System string
	User   string
}

// Efforts are the effort levels `claude -p --effort` takes, lowest first.
var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

// Usage is what one call consumed, as `claude -p` reports it.
type Usage struct {
	Seconds float64 `json:"seconds"`
	CostUSD float64 `json:"cost_usd"`
	// IsError is true when the call failed: claude replied with is_error
	// true, exited non-zero or printed no result.
	IsError bool `json:"is_error"`
	Turns   int  `json:"turns"`
	// Models holds each model's tokens, keyed by its full model id. A call
	// can use more than the model it asked for.
	Models map[string]Tokens `json:"models"`
	// Reading is the usage limits as the call's response began, so it
	// doesn't hold what the call spent, or nil if claude reported none.
	Reading *Reading `json:"reading,omitempty"`
}

// Reading is the account's usage limits at one moment.
type Reading struct {
	// At is when the reading was taken, so the latest of several can be told.
	At time.Time `json:"at"`
	// Status is the account's status for Limit: "allowed",
	// "allowed_warning" or "rejected". A reading taken before any call has
	// none.
	Status string `json:"status,omitempty"`
	// Limit names the usage limit Status, ResetsAt and UsingOverage are
	// about, such as FiveHour.
	Limit string `json:"limit,omitempty"`
	// ResetsAt is when Limit resets, or zero if it wasn't given.
	ResetsAt time.Time `json:"resets_at,omitzero"`
	// UsingOverage is true when calls are being spent past Limit.
	UsingOverage bool `json:"using_overage,omitempty"`
	// Limits holds each usage limit's reading, keyed by its name. It is empty
	// when claude reported only Status.
	Limits map[string]Limit `json:"limits,omitempty"`
}

// The usage limits a Reading names that the run knows. Others, such as a
// per-model weekly limit, keep the name the account gives them.
const (
	FiveHour = "five_hour"
	SevenDay = "seven_day"
)

// Limit is one usage limit's reading.
type Limit struct {
	// Utilization is the fraction of the limit used, from 0 to 1, in steps
	// of 0.01.
	Utilization float64 `json:"utilization"`
	// ResetsAt is when the limit resets, or zero if it wasn't given.
	ResetsAt time.Time `json:"resets_at,omitzero"`
}

// UsageLimited is a call the account refused because a usage limit has no
// room left. The call was never answered, so it can be made again once the
// limit resets.
type UsageLimited struct {
	// Limit names the usage limit, or is "" if claude didn't say which.
	Limit string
	// ResetsAt is when Limit resets, or zero if it couldn't be read.
	ResetsAt time.Time
	// Text is what claude replied.
	Text string
}

func (e *UsageLimited) Error() string {
	limit := "a usage limit"
	if e.Limit != "" {
		limit = "the " + e.Limit + " usage limit"
	}
	if e.ResetsAt.IsZero() {
		return fmt.Sprintf("claude refused the call for %s, and gave no reset time: %s", limit, e.Text)
	}
	return fmt.Sprintf("claude refused the call for %s, which resets at %s", limit, e.ResetsAt.Format(time.RFC3339))
}

// Tokens is one model's token counts in a call.
type Tokens struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	OutputTokens             int `json:"output_tokens"`
}

// Reply is a call's reply text and its usage.
type Reply struct {
	Text  string
	Usage Usage
}

// Provider makes model calls. Call is safe for concurrent use.
type Provider interface {
	Call(ctx context.Context, req Request) (Reply, error)
}

// Replay is the test adapter. It serves recorded replies keyed by user
// prompt, and records every request so a test can check the system prompt,
// model and effort it was sent. A prompt's replies are served in order, one
// per call, so a step that sends the same prompt twice (the loss proposer's
// two runs) gets both. A call with no recorded reply left is an error, so a test
// also fails on a wrong prompt.
type Replay struct {
	mu      sync.Mutex
	replies map[string][]Reply
	calls   []Request
}

// NewReplay returns a Replay serving replies, keyed by user prompt.
func NewReplay(replies map[string][]Reply) *Replay {
	r := &Replay{replies: make(map[string][]Reply, len(replies))}
	for user, rs := range replies {
		r.replies[user] = append([]Reply(nil), rs...)
	}
	return r
}

// Call returns the next reply recorded for req.User.
func (r *Replay) Call(ctx context.Context, req Request) (Reply, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, req)
	if err := ctx.Err(); err != nil {
		return Reply{}, err
	}
	rs := r.replies[req.User]
	if len(rs) == 0 {
		return Reply{}, fmt.Errorf("replay: no recorded reply left for user prompt %q", req.User)
	}
	r.replies[req.User] = rs[1:]
	return rs[0], nil
}

// Calls returns the requests made so far, in the order they arrived.
func (r *Replay) Calls() []Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Request(nil), r.calls...)
}
