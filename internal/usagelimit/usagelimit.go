// Package usagelimit keeps a run inside the account's usage limits (ADR
// 0011). Its Gate wraps the provider: before each call it predicts whether
// the 5-hour limit has room for the call and for every call still running,
// and holds the call until the limit resets if it hasn't. A call the account
// refuses for a usage limit closes the gate in the same way.
//
// The gate decides from the latest Reading it has seen: the one the run
// folder's usage logs hold when the run starts or resumes, the account's
// usage read before the first call (ReadAccount), and each call's own. Every
// call's reading is logged with its usage, so a resume starts from the
// latest of those; the account's reading isn't logged, and a resume reads
// the account again. A reading that holds only a status, with no limit's
// utilization, leaves the gate checking the limits against the latest one
// that held them.
//
// A reading is taken as a call's response begins, so it doesn't hold what
// that call, or any call that ended after it, spent. The gate adds the cost
// of every call that ended after the reading to its 5-hour utilization. A
// call that was running when the reading was taken may have had some of its
// spend in it already, and counts in full, so the gate errs on waiting early
// rather than late.
//
// The check, before a call whose predicted cost is c:
//
//   - The 5-hour limit has room if its utilization, plus (the cost of the
//     calls that ended since its reading + (calls in flight + 1) × c) ÷
//     FiveHourUSD, plus 0.01, is at most 1. The 0.01 is a step of the
//     utilization the account reports, so a call doesn't start on a limit
//     the next step would fill.
//   - Any other limit, the weekly one or a per-model one, isn't predicted: it
//     has no room only when its utilization is at least 0.99.
//   - The limit a reading's status is about has no room when the status is
//     "rejected" or calls are being spent past it (overage), since a run
//     never spends past a usage limit.
//   - A limit whose reset time has passed since its reading has room.
//
// A call that won't fit, or a refused one, closes the gate until the latest
// reset among the limits without room, plus a minute. While it is closed no
// call starts, and the calls already running finish and keep their replies.
// The gate says once which limits it waits for and until when. When it
// opens, the next call runs alone, as a step's first call does, so that it
// writes the cache again before the rest read it (ADR 0004): a wait that
// long outlasts the cache. A limit with no room whose reset time can't be
// read stops the run with an *Unreadable, since the gate can't know how long
// to wait. So do MaxUnexplained refusals in a row that no reset still to come
// explains: each is waited out for a minute and the call made again, but a
// run that goes on being refused for a limit that should have reset would
// otherwise spin unattended for as long as no one looks.
package usagelimit

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/corygyarmathy/slipbox/internal/provider"
)

// DefaultFiveHourUSD is the dollars at API rates a Pro plan's 5-hour limit
// holds: about $10, a figure measured on trial calls (ADR 0011). Another
// plan's figure is given with s2n's -five-hour-usd.
const DefaultFiveHourUSD = 10.0

// Margin is added to a predicted 5-hour utilization: one step of the
// utilization the account reports.
const Margin = 0.01

// Full is the utilization at which a limit that isn't predicted has no room.
const Full = 0.99

// AfterReset is how long past a limit's reset the gate waits, so that the
// account has reset it.
const AfterReset = time.Minute

// MaxUnexplained is how many refusals in a row, with no reset still to come
// to wait for, stop the run.
const MaxUnexplained = 3

// Gate is a provider.Provider that holds each call until the usage limits
// have room for it, as the package doc says. A call's predicted cost comes
// from its context (WithCost). It is safe for concurrent use.
type Gate struct {
	provider    provider.Provider
	fiveHourUSD float64
	clock       Clock
	say         func(string)

	mu sync.Mutex
	// latest is the latest reading, for its status, and limits the latest
	// that held the limits' utilization.
	latest, limits *provider.Reading
	// ended holds the calls that ended after limits was taken, and what each
	// cost.
	ended   []spend
	running int
	// unexplained counts the refusals in a row with no reset still to come.
	unexplained int
	// until is when the gate opens, and waitFor the limits it waits for.
	// announced is the until already said.
	until, announced time.Time
	waitFor          []string
	// alone is set once the gate has closed, so the first call after it
	// opens runs alone; aloneRunning while that call runs.
	alone, aloneRunning bool
	// stopped is the error every call gets once the gate can't wait.
	stopped error
	// changed is closed, and replaced, whenever a call ends.
	changed chan struct{}
	// first reads the account's usage before the first call, or is nil once
	// it has.
	first func(context.Context) (*provider.Reading, error)
}

// NewGate returns a gate over p. fiveHourUSD is the dollars at API rates the
// 5-hour limit holds, clock tells the time and waits, and say prints a line
// saying what the gate waits for. first, if not nil, reads the account's
// usage, and the gate calls it once, before the first call: if it fails, the
// gate says so in a line and checks the first call against what it has.
func NewGate(p provider.Provider, fiveHourUSD float64, clock Clock, say func(string), first func(context.Context) (*provider.Reading, error)) *Gate {
	return &Gate{provider: p, fiveHourUSD: fiveHourUSD, clock: clock, say: say, changed: make(chan struct{}), first: first}
}

// Observe gives the gate a reading, which it keeps if it is later than the
// latest it has. A nil reading is ignored.
func (g *Gate) Observe(r *provider.Reading) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.observe(r)
}

func (g *Gate) observe(r *provider.Reading) {
	if r == nil {
		return
	}
	if g.latest == nil || r.At.After(g.latest.At) {
		g.latest = r
	}
	if len(r.Limits) > 0 && (g.limits == nil || r.At.After(g.limits.At)) {
		g.limits = r
		g.ended = slices.DeleteFunc(g.ended, func(s spend) bool { return !s.at.After(r.At) })
	}
}

// spend is a call that ended, and what it cost.
type spend struct {
	at  time.Time
	usd float64
}

// Call makes req's call once the usage limits have room for it. A call the
// account refuses for a usage limit fails with a *Refused, and the gate holds
// the next call until the limit resets, so the caller can make the call
// again. Once a limit's reset time can't be read, every call fails with an
// *Unreadable. A call the gate never started fails with a *NotStarted.
func (g *Gate) Call(ctx context.Context, req provider.Request) (provider.Reply, error) {
	alone, err := g.admit(ctx, Cost(ctx))
	if err != nil {
		return provider.Reply{}, &NotStarted{Err: err}
	}
	reply, err := g.provider.Call(ctx, req)
	return reply, g.end(alone, reply.Usage, err)
}

// admit waits until a call that costs cost can start, counts it as running
// and says whether it runs alone.
func (g *Gate) admit(ctx context.Context, cost float64) (alone bool, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if first := g.first; first != nil {
		g.first = nil
		g.mu.Unlock()
		r, err := first(ctx)
		g.mu.Lock()
		switch {
		case err != nil && g.latest == nil:
			g.say(fmt.Sprintf("the account's usage could not be read, so the first call starts unchecked: %v", err))
		case err != nil:
			g.say(fmt.Sprintf("the account's usage could not be read, so the first call is checked against the run folder's last reading: %v", err))
		default:
			g.observe(r)
		}
	}
	for {
		if g.stopped != nil {
			return false, g.stopped
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		now := g.clock.Now()
		if now.Before(g.until) {
			if !g.announced.Equal(g.until) {
				g.announced = g.until
				g.say(fmt.Sprintf("the %s %s no room, so the run waits until %s.", describe(g.waitFor), have(g.waitFor), g.until.Local().Format("Mon 2 Jan 15:04 MST")))
			}
			until := g.until
			g.mu.Unlock()
			err := g.clock.Wait(ctx, until)
			g.mu.Lock()
			if err != nil {
				return false, err
			}
			continue
		}
		if g.aloneRunning || (g.alone && g.running > 0) {
			changed := g.changed
			g.mu.Unlock()
			select {
			case <-changed:
			case <-ctx.Done():
			}
			g.mu.Lock()
			continue
		}
		full, err := g.full(now, cost)
		if err != nil {
			g.stopped = err
			return false, err
		}
		if len(full) > 0 {
			g.close(now, full)
			continue
		}
		g.running++
		if g.alone {
			g.alone, g.aloneRunning = false, true
			return true, nil
		}
		return false, nil
	}
}

// end counts a call as ended, keeps its reading, and gives the error the
// call fails with: callErr, or a *Refused or *Unreadable for a refusal.
func (g *Gate) end(alone bool, u provider.Usage, callErr error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	defer func() {
		close(g.changed)
		g.changed = make(chan struct{})
	}()
	g.running--
	if alone {
		g.aloneRunning = false
	}
	now := g.clock.Now()
	if u.CostUSD > 0 {
		g.ended = append(g.ended, spend{now, u.CostUSD})
	}
	g.observe(u.Reading)
	var limited *provider.UsageLimited
	if !errors.As(callErr, &limited) {
		g.unexplained = 0
		return callErr
	}
	// The refusal's own limit and reset time, and those of any limit the
	// latest reading shows full: a refusal that names neither leaves the gate
	// nothing to wait for.
	full, err := g.full(now, 0)
	if err != nil || full == nil {
		full = map[string]time.Time{}
	}
	if limited.Limit != "" || !limited.ResetsAt.IsZero() {
		if reset, ok := full[limited.Limit]; !ok || reset.IsZero() || limited.ResetsAt.After(reset) {
			full[limited.Limit] = limited.ResetsAt
		}
	}
	if _, ok := unreadable(full); ok || len(full) == 0 {
		if g.stopped == nil {
			g.stopped = &Unreadable{Limited: limited}
		}
		return g.stopped
	}
	if !slices.ContainsFunc(slices.Collect(maps.Values(full)), now.Before) {
		if g.unexplained++; g.unexplained >= MaxUnexplained {
			if g.stopped == nil {
				g.stopped = &Unreadable{Limited: limited, Refusals: g.unexplained}
			}
			return g.stopped
		}
	} else {
		g.unexplained = 0
	}
	g.close(now, full)
	return &Refused{Limited: limited, Until: g.until}
}

// close closes the gate until the latest reset in full, the limits with no
// room and their reset times, plus AfterReset, and for at least AfterReset
// from now: a refusal whose reset has passed is waited out for a minute, not
// made again at once. It never opens the gate sooner.
func (g *Gate) close(now time.Time, full map[string]time.Time) {
	latest := now
	for _, reset := range full {
		if reset.After(latest) {
			latest = reset
		}
	}
	if until := latest.Add(AfterReset); until.After(g.until) {
		g.until, g.waitFor = until, slices.Sorted(maps.Keys(full))
	}
	g.alone = true
}

// full gives the limits the latest reading shows no room in for a call that
// costs cost, with their reset times, as the package doc says. A limit
// without room whose reset time can't be read is an *Unreadable.
func (g *Gate) full(now time.Time, cost float64) (map[string]time.Time, error) {
	full := map[string]time.Time{}
	var limits map[string]provider.Limit
	if g.limits != nil {
		limits = g.limits.Limits
	}
	var since float64
	for _, s := range g.ended {
		since += s.usd
	}
	for name, l := range limits {
		if !l.ResetsAt.IsZero() && !now.Before(l.ResetsAt) {
			continue
		}
		u := l.Utilization
		if name == provider.FiveHour {
			if u+(since+float64(g.running+1)*cost)/g.fiveHourUSD+Margin > 1 {
				full[name] = l.ResetsAt
			}
		} else if u >= Full {
			full[name] = l.ResetsAt
		}
	}
	if r := g.latest; r != nil && (r.Status == "rejected" || r.UsingOverage) && (r.ResetsAt.IsZero() || now.Before(r.ResetsAt)) {
		if reset, ok := full[r.Limit]; !ok || reset.IsZero() {
			full[r.Limit] = r.ResetsAt
		}
	}
	if name, ok := unreadable(full); ok {
		return nil, &Unreadable{Limited: &provider.UsageLimited{Limit: name}}
	}
	return full, nil
}

// unreadable gives a limit in full whose reset time is zero, and true, or
// false if every reset time was read.
func unreadable(full map[string]time.Time) (string, bool) {
	for _, name := range slices.Sorted(maps.Keys(full)) {
		if full[name].IsZero() {
			return name, true
		}
	}
	return "", false
}

// describe names limits for a line of output: "5-hour usage limit", "weekly
// and 5-hour usage limits".
func describe(limits []string) string {
	var names []string
	for _, l := range limits {
		switch l {
		case provider.FiveHour:
			names = append(names, "5-hour")
		case provider.SevenDay:
			names = append(names, "weekly")
		case "":
			names = append(names, "account's")
		default:
			names = append(names, l)
		}
	}
	if len(names) == 1 {
		return names[0] + " usage limit"
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1] + " usage limits"
}

func have(limits []string) string {
	if len(limits) == 1 {
		return "has"
	}
	return "have"
}

// Refused is a call refused for a usage limit, which the gate waits out: the
// next call it lets start, at Until or later, can be this call made again.
type Refused struct {
	Limited *provider.UsageLimited
	Until   time.Time
}

func (e *Refused) Error() string {
	return fmt.Sprintf("%v; the run waits until %s", e.Limited, e.Until.Format(time.RFC3339))
}

func (e *Refused) Unwrap() error { return e.Limited }

// Unreadable is a usage limit with no room whose reset time can't be read,
// so the run can't wait for it. Refusals, when not 0, is instead how many
// refusals in a row came after every reset time the account gave had passed.
type Unreadable struct {
	Limited  *provider.UsageLimited
	Refusals int
}

func (e *Unreadable) Error() string {
	limits := []string{e.Limited.Limit}
	if e.Refusals > 0 {
		return fmt.Sprintf("claude refused %d calls in a row for the %s, after every reset time the account gave had passed, so the run can't tell how long to wait. claude replied: %s", e.Refusals, describe(limits), e.Limited.Text)
	}
	if e.Limited.Text != "" {
		return fmt.Sprintf("the %s has no room, and s2n can't read when it resets, so the run can't wait for it. claude replied: %s", describe(limits), e.Limited.Text)
	}
	return fmt.Sprintf("the %s has no room, and s2n can't read when it resets, so the run can't wait for it", describe(limits))
}

func (e *Unreadable) Unwrap() error { return e.Limited }

// NotStarted is a call the gate never started, as the run was interrupted or
// stopped while the call waited: it was neither made nor spent.
type NotStarted struct {
	Err error
}

func (e *NotStarted) Error() string { return e.Err.Error() }

func (e *NotStarted) Unwrap() error { return e.Err }

type costKey struct{}

// WithCost gives ctx a call's predicted cost in dollars at API rates, for
// the gate's check.
func WithCost(ctx context.Context, usd float64) context.Context {
	return context.WithValue(ctx, costKey{}, usd)
}

// Cost is the predicted cost ctx holds, or 0.
func Cost(ctx context.Context) float64 {
	usd, _ := ctx.Value(costKey{}).(float64)
	return usd
}

// Clock tells the time and waits.
type Clock interface {
	Now() time.Time
	// Wait returns once the time is until, or with ctx's error if ctx ends
	// first.
	Wait(ctx context.Context, until time.Time) error
}

// RealClock is the wall clock.
var RealClock Clock = realClock{}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Wait sleeps in steps of at most a minute, reading the wall clock after
// each, since a timer counts no time the computer spends asleep and a wait
// can last days.
func (realClock) Wait(ctx context.Context, until time.Time) error {
	for {
		d := time.Until(until)
		if d <= 0 {
			return nil
		}
		t := time.NewTimer(min(d, time.Minute))
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}
