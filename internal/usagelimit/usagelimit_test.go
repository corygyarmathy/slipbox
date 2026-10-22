package usagelimit_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/usagelimit"
)

// start is the fake clock's time when a test begins.
var start = time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)

// clock is a fake usagelimit.Clock. Wait doesn't sleep: it moves the time on
// to until and records it.
type clock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Time
}

func newClock() *clock { return &clock{now: start} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Wait(ctx context.Context, until time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, until)
	if until.After(c.now) {
		c.now = until
	}
	return nil
}

func (c *clock) Waits() []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.waits)
}

// fake is a provider whose calls each return the next of its replies, or
// the last once they run out. A call it is given a gate for blocks until the
// gate is closed.
type fake struct {
	mu      sync.Mutex
	clock   *clock
	replies []answer
	// started has each call's time, user prompt and predicted cost.
	started             []call
	running, maxRunning int
	gates               map[string]chan struct{}
}

type answer struct {
	reply provider.Reply
	err   error
}

type call struct {
	at   time.Time
	user string
	cost float64
}

func (f *fake) Call(ctx context.Context, req provider.Request) (provider.Reply, error) {
	f.mu.Lock()
	f.started = append(f.started, call{f.clock.Now(), req.User, usagelimit.Cost(ctx)})
	f.running++
	f.maxRunning = max(f.maxRunning, f.running)
	a := answer{reply: provider.Reply{Text: "ok"}}
	if len(f.replies) > 0 {
		a = f.replies[0]
		if len(f.replies) > 1 {
			f.replies = f.replies[1:]
		}
	}
	gate := f.gates[req.User]
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	f.running--
	f.mu.Unlock()
	return a.reply, a.err
}

func (f *fake) Started() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.started)
}

// reading is a reading taken at start with the 5-hour limit at fiveHour and
// the weekly one at weekly, resetting in two hours and three days.
func reading(fiveHour, weekly float64) *provider.Reading {
	return &provider.Reading{
		At: start, Status: "allowed", Limit: provider.FiveHour, ResetsAt: start.Add(2 * time.Hour),
		Limits: map[string]provider.Limit{
			provider.FiveHour: {Utilization: fiveHour, ResetsAt: start.Add(2 * time.Hour)},
			provider.SevenDay: {Utilization: weekly, ResetsAt: start.Add(72 * time.Hour)},
		},
	}
}

// said collects what a gate says.
type said struct {
	mu    sync.Mutex
	lines []string
}

func (s *said) say(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, line)
}

func (s *said) Lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.lines)
}

func newGate(p provider.Provider, c *clock, s *said) *usagelimit.Gate {
	return usagelimit.NewGate(p, 10, c, s.say, nil)
}

func callAt(t *testing.T, g *usagelimit.Gate, user string, cost float64) error {
	t.Helper()
	_, err := g.Call(usagelimit.WithCost(context.Background(), cost), provider.Request{User: user})
	return err
}

// A call the 5-hour limit has room for starts at once, with its predicted
// cost.
func TestGate_Fits(t *testing.T) {
	c, s := newClock(), &said{}
	f := &fake{clock: c}
	g := newGate(f, c, s)
	// 0.27 + $0.50 of $10 + 0.01 = 0.33.
	g.Observe(reading(0.27, 0.26))
	if err := callAt(t, g, "u", 0.5); err != nil {
		t.Fatal(err)
	}
	if got := f.Started(); len(got) != 1 || !got[0].at.Equal(start) || got[0].cost != 0.5 {
		t.Errorf("calls = %+v, want one at once costing 0.5", got)
	}
	if len(c.Waits()) != 0 || len(s.Lines()) != 0 {
		t.Errorf("waited %v, said %q", c.Waits(), s.Lines())
	}
}

// A call that won't fit isn't started until the 5-hour limit resets, plus a
// minute, and the gate says so once.
func TestGate_WontFitWaitsForTheReset(t *testing.T) {
	c, s := newClock(), &said{}
	f := &fake{clock: c}
	g := newGate(f, c, s)
	// 0.95 + $0.50 of $10 + 0.01 = 1.01.
	g.Observe(reading(0.95, 0.26))
	if err := callAt(t, g, "u", 0.5); err != nil {
		t.Fatal(err)
	}
	until := start.Add(2*time.Hour + time.Minute)
	if got := f.Started(); len(got) != 1 || !got[0].at.Equal(until) {
		t.Errorf("calls = %+v, want one at %v", got, until)
	}
	if got := c.Waits(); !slices.Equal(got, []time.Time{until}) {
		t.Errorf("waits = %v, want one until %v", got, until)
	}
	if lines := s.Lines(); len(lines) != 1 || !strings.Contains(lines[0], "5-hour usage limit has no room") {
		t.Errorf("said %q, want one line naming the 5-hour limit", lines)
	}
}

// A call that would just fit, at a predicted 1.0, starts.
func TestGate_JustFits(t *testing.T) {
	c, s := newClock(), &said{}
	f := &fake{clock: c}
	g := newGate(f, c, s)
	// 0.94 + $0.50 of $10 + 0.01 = 1.0.
	g.Observe(reading(0.94, 0.26))
	if err := callAt(t, g, "u", 0.5); err != nil {
		t.Fatal(err)
	}
	if len(c.Waits()) != 0 {
		t.Errorf("waited %v", c.Waits())
	}
}

// The check counts every call still running at the same predicted cost.
func TestGate_CountsCallsInFlight(t *testing.T) {
	c, s := newClock(), &said{}
	release := make(chan struct{})
	f := &fake{clock: c, gates: map[string]chan struct{}{"a": release, "b": release, "c": release}}
	g := newGate(f, c, s)
	// With two running: 0.85 + 3 × $0.40 of $10 + 0.01 = 0.98, and with
	// three, 1.02.
	g.Observe(reading(0.85, 0.26))
	var wg sync.WaitGroup
	for _, u := range []string{"a", "b"} {
		wg.Go(func() { callAt(t, g, u, 0.4) })
	}
	for len(f.Started()) < 2 {
		time.Sleep(time.Millisecond)
	}
	wg.Go(func() { callAt(t, g, "c", 0.4) })
	for len(f.Started()) < 3 {
		time.Sleep(time.Millisecond)
	}
	if got := f.Started()[2]; !got.at.Equal(start) {
		t.Errorf("the third call started at %v, want at once", got.at)
	}
	wg.Go(func() { callAt(t, g, "d", 0.4) })
	for len(c.Waits()) < 1 {
		time.Sleep(time.Millisecond)
	}
	if n := len(f.Started()); n != 3 {
		t.Errorf("%d calls started, want the fourth to wait", n)
	}
	close(release)
	wg.Wait()
	until := start.Add(2*time.Hour + time.Minute)
	if got := f.Started()[3]; got.user != "d" || !got.at.Equal(until) {
		t.Errorf("the fourth call = %+v, want it at %v", got, until)
	}
}

// The weekly limit isn't predicted: it has no room only at 0.99.
func TestGate_WeeklyLimit(t *testing.T) {
	for _, tc := range []struct {
		weekly float64
		wait   bool
	}{{0.98, false}, {0.99, true}, {1, true}} {
		c, s := newClock(), &said{}
		f := &fake{clock: c}
		g := newGate(f, c, s)
		g.Observe(reading(0.1, tc.weekly))
		if err := callAt(t, g, "u", 5); err != nil {
			t.Fatal(err)
		}
		until := start.Add(72*time.Hour + time.Minute)
		if got := c.Waits(); tc.wait != slices.Equal(got, []time.Time{until}) || !tc.wait && len(got) > 0 {
			t.Errorf("weekly at %v: waits = %v, want a wait until %v: %v", tc.weekly, got, until, tc.wait)
		}
		if tc.wait && !strings.Contains(s.Lines()[0], "weekly usage limit") {
			t.Errorf("weekly at %v: said %q", tc.weekly, s.Lines())
		}
	}
}

// A run waits for the latest reset among the limits without room.
func TestGate_WaitsForTheLatestReset(t *testing.T) {
	c, s := newClock(), &said{}
	f := &fake{clock: c}
	g := newGate(f, c, s)
	g.Observe(reading(0.995, 0.995))
	if err := callAt(t, g, "u", 0.1); err != nil {
		t.Fatal(err)
	}
	if got, want := c.Waits(), []time.Time{start.Add(72*time.Hour + time.Minute)}; !slices.Equal(got, want) {
		t.Errorf("waits = %v, want %v", got, want)
	}
	if got := s.Lines(); len(got) != 1 || !strings.Contains(got[0], "5-hour and weekly usage limits have no room") {
		t.Errorf("said %q", got)
	}
}

// A rejected status, or overage, is no room in the limit it is about.
func TestGate_RejectedAndOverage(t *testing.T) {
	for name, r := range map[string]func(*provider.Reading){
		"rejected": func(r *provider.Reading) { r.Status = "rejected" },
		"overage":  func(r *provider.Reading) { r.UsingOverage = true },
	} {
		c, s := newClock(), &said{}
		f := &fake{clock: c}
		g := newGate(f, c, s)
		latest := reading(0.2, 0.2)
		r(latest)
		g.Observe(latest)
		if err := callAt(t, g, "u", 0.1); err != nil {
			t.Fatal(err)
		}
		if got, want := c.Waits(), []time.Time{start.Add(2*time.Hour + time.Minute)}; !slices.Equal(got, want) {
			t.Errorf("%s: waits = %v, want %v", name, got, want)
		}
	}
}

// A limit whose reset has passed since its reading has room.
func TestGate_ResetSinceTheReading(t *testing.T) {
	c, s := newClock(), &said{}
	c.now = start.Add(3 * time.Hour)
	f := &fake{clock: c}
	g := newGate(f, c, s)
	latest := reading(1, 0.2)
	latest.Status = "rejected"
	g.Observe(latest)
	if err := callAt(t, g, "u", 0.1); err != nil {
		t.Fatal(err)
	}
	if len(c.Waits()) != 0 {
		t.Errorf("waited %v", c.Waits())
	}
}

// The gate keeps the latest reading it is given, by when it was taken.
func TestGate_KeepsTheLatestReading(t *testing.T) {
	c, s := newClock(), &said{}
	f := &fake{clock: c}
	g := newGate(f, c, s)
	newer := reading(0.2, 0.2)
	older := reading(0.995, 0.2)
	older.At = start.Add(-time.Hour)
	g.Observe(newer)
	g.Observe(older)
	g.Observe(nil)
	if err := callAt(t, g, "u", 0.1); err != nil {
		t.Fatal(err)
	}
	if len(c.Waits()) != 0 {
		t.Errorf("waited %v on the older reading", c.Waits())
	}
}

// A call's reading is the gate's next latest.
func TestGate_ObservesEachCall(t *testing.T) {
	c, s := newClock(), &said{}
	full := reading(0.995, 0.2)
	full.At = start.Add(time.Second)
	f := &fake{clock: c, replies: []answer{{reply: provider.Reply{Usage: provider.Usage{Reading: full}}}, {}}}
	g := newGate(f, c, s)
	g.Observe(reading(0.2, 0.2))
	for range 2 {
		if err := callAt(t, g, "u", 0.1); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := c.Waits(), []time.Time{start.Add(2*time.Hour + time.Minute)}; !slices.Equal(got, want) {
		t.Errorf("waits = %v, want %v", got, want)
	}
}

// A refused call fails with a *Refused, and the next call waits for the
// reset and runs alone: the others wait for it to end.
func TestGate_Refusal(t *testing.T) {
	c, s := newClock(), &said{}
	reset := start.Add(90 * time.Minute)
	refusedReading := reading(1, 0.3)
	refusedReading.Status, refusedReading.ResetsAt = "rejected", reset
	refusedReading.Limits[provider.FiveHour] = provider.Limit{Utilization: 1, ResetsAt: reset}
	release := make(chan struct{})
	f := &fake{clock: c, gates: map[string]chan struct{}{"again": release}, replies: []answer{
		{reply: provider.Reply{Text: "You've hit your session limit", Usage: provider.Usage{Reading: refusedReading}},
			err: &provider.UsageLimited{Limit: provider.FiveHour, ResetsAt: reset, Text: "You've hit your session limit"}},
		{},
	}}
	g := newGate(f, c, s)
	err := callAt(t, g, "refused", 0.1)
	var refused *usagelimit.Refused
	if !errors.As(err, &refused) {
		t.Fatalf("error %v is not a *Refused", err)
	}
	until := reset.Add(time.Minute)
	if !refused.Until.Equal(until) {
		t.Errorf("Until = %v, want %v", refused.Until, until)
	}
	var limited *provider.UsageLimited
	if !errors.As(err, &limited) {
		t.Errorf("error %v does not wrap the *UsageLimited", err)
	}

	var wg sync.WaitGroup
	wg.Go(func() { callAt(t, g, "again", 0.1) })
	for len(f.Started()) < 2 {
		time.Sleep(time.Millisecond)
	}
	wg.Go(func() { callAt(t, g, "next", 0.1) })
	time.Sleep(20 * time.Millisecond)
	if n := len(f.Started()); n != 2 {
		t.Errorf("%d calls started while the first after the wait ran, want it alone", n)
	}
	close(release)
	wg.Wait()
	got := f.Started()
	if len(got) != 3 || got[1].user != "again" || !got[1].at.Equal(until) {
		t.Errorf("calls = %+v, want the call made again at %v, then the next", got, until)
	}
	if f.maxRunning != 1 {
		t.Errorf("%d calls ran at once, want 1", f.maxRunning)
	}
	if lines := s.Lines(); len(lines) != 1 {
		t.Errorf("said %q, want one line", lines)
	}
}

// A refusal doesn't stop the calls running beside it, which end with their
// replies.
func TestGate_RefusalLeavesRunningCalls(t *testing.T) {
	c, s := newClock(), &said{}
	release := make(chan struct{})
	reset := start.Add(time.Hour)
	f := &fake{clock: c, gates: map[string]chan struct{}{"running": release}, replies: []answer{
		{reply: provider.Reply{Text: "kept"}},
		{err: &provider.UsageLimited{Limit: provider.FiveHour, ResetsAt: reset}},
	}}
	g := newGate(f, c, s)
	done := make(chan provider.Reply)
	go func() {
		r, _ := g.Call(context.Background(), provider.Request{User: "running"})
		done <- r
	}()
	for len(f.Started()) < 1 {
		time.Sleep(time.Millisecond)
	}
	var refused *usagelimit.Refused
	if err := callAt(t, g, "refused", 0.1); !errors.As(err, &refused) {
		t.Fatalf("error %v is not a *Refused", err)
	}
	close(release)
	if r := <-done; r.Text != "kept" {
		t.Errorf("the running call's reply = %q, want it kept", r.Text)
	}
}

// A refusal whose reset time can't be read stops every call after it with
// an *Unreadable.
func TestGate_Unreadable(t *testing.T) {
	c, s := newClock(), &said{}
	f := &fake{clock: c, replies: []answer{{err: &provider.UsageLimited{Text: "You've hit your limit"}}}}
	g := newGate(f, c, s)
	var unreadable *usagelimit.Unreadable
	if err := callAt(t, g, "u", 0.1); !errors.As(err, &unreadable) {
		t.Fatalf("error %v is not an *Unreadable", err)
	}
	if err := callAt(t, g, "v", 0.1); !errors.As(err, &unreadable) {
		t.Fatalf("the next call's error %v is not an *Unreadable", err)
	}
	if n := len(f.Started()); n != 1 {
		t.Errorf("%d calls started, want only the refused one", n)
	}
}

// A limit with no room and no reset time stops the run before any call.
func TestGate_UnreadableReading(t *testing.T) {
	c, s := newClock(), &said{}
	f := &fake{clock: c}
	g := newGate(f, c, s)
	r := reading(0.2, 1)
	r.Limits[provider.SevenDay] = provider.Limit{Utilization: 1}
	g.Observe(r)
	var unreadable *usagelimit.Unreadable
	if err := callAt(t, g, "u", 0.1); !errors.As(err, &unreadable) {
		t.Fatalf("error %v is not an *Unreadable", err)
	}
	if len(f.Started()) != 0 {
		t.Error("a call started")
	}
}

// Ctrl-C during a wait ends the call with the context's error.
func TestGate_CancelledWait(t *testing.T) {
	c, s := newClock(), &said{}
	f := &fake{clock: c}
	g := newGate(f, c, s)
	g.Observe(reading(0.995, 0.2))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Call(ctx, provider.Request{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v, want context.Canceled", err)
	}
	if len(f.Started()) != 0 {
		t.Error("a call started")
	}
}

// The account's usage is read once, before the first call, and checked.
func TestGate_FirstReading(t *testing.T) {
	c, s := newClock(), &said{}
	f := &fake{clock: c}
	reads := 0
	g := usagelimit.NewGate(f, 10, c, s.say, func(context.Context) (*provider.Reading, error) {
		reads++
		return reading(0.995, 0.2), nil
	})
	for range 2 {
		if err := callAt(t, g, "u", 0.1); err != nil {
			t.Fatal(err)
		}
	}
	if reads != 1 {
		t.Errorf("read the account %d times, want once", reads)
	}
	if got := f.Started(); len(got) != 2 || !got[0].at.Equal(start.Add(2*time.Hour+time.Minute)) {
		t.Errorf("calls = %+v, want the first after the reset", got)
	}
}

// A first reading that fails says so in one line, and the first call starts
// unchecked.
func TestGate_FirstReadingFails(t *testing.T) {
	c, s := newClock(), &said{}
	f := &fake{clock: c}
	g := usagelimit.NewGate(f, 10, c, s.say, func(context.Context) (*provider.Reading, error) {
		return nil, errors.New("no credentials")
	})
	if err := callAt(t, g, "u", 0.1); err != nil {
		t.Fatal(err)
	}
	if lines := s.Lines(); len(lines) != 1 || !strings.Contains(lines[0], "starts unchecked: no credentials") {
		t.Errorf("said %q", lines)
	}
	if len(f.Started()) != 1 {
		t.Error("the call didn't start")
	}
}

// A refusal whose reset has already passed is made again after a minute,
// not at once.
func TestGate_RefusalWithAPastReset(t *testing.T) {
	c, s := newClock(), &said{}
	f := &fake{clock: c, replies: []answer{{err: &provider.UsageLimited{Limit: provider.FiveHour, ResetsAt: start.Add(-time.Hour)}}, {}}}
	g := newGate(f, c, s)
	var refused *usagelimit.Refused
	if err := callAt(t, g, "u", 0.1); !errors.As(err, &refused) {
		t.Fatalf("error %v is not a *Refused", err)
	}
	if err := callAt(t, g, "u", 0.1); err != nil {
		t.Fatal(err)
	}
	if got, want := c.Waits(), []time.Time{start.Add(time.Minute)}; !slices.Equal(got, want) {
		t.Errorf("waits = %v, want %v", got, want)
	}
}

// A reading that holds only a status leaves the limits as the last reading
// that held them showed.
func TestGate_StatusOnlyReadingKeepsTheLimits(t *testing.T) {
	c, s := newClock(), &said{}
	f := &fake{clock: c}
	g := newGate(f, c, s)
	g.Observe(reading(0.95, 0.2))
	g.Observe(&provider.Reading{At: start.Add(time.Second), Status: "allowed", Limit: provider.FiveHour, ResetsAt: start.Add(2 * time.Hour)})
	if err := callAt(t, g, "u", 1); err != nil {
		t.Fatal(err)
	}
	if got, want := c.Waits(), []time.Time{start.Add(2*time.Hour + time.Minute)}; !slices.Equal(got, want) {
		t.Errorf("waits = %v, want %v", got, want)
	}
}

// The cost of the calls that ended after the latest reading counts against
// the 5-hour limit, as the reading doesn't hold it.
func TestGate_CountsCallsEndedSinceTheReading(t *testing.T) {
	c, s := newClock(), &said{}
	f := &fake{clock: c, replies: []answer{{reply: provider.Reply{Usage: provider.Usage{CostUSD: 0.4}}}}}
	g := newGate(f, c, s)
	// Each call costs $0.40, so the first three fit: the third, after two
	// ended, at 0.85 + ($0.80 + $0.40) of $10 + 0.01 = 0.98. The fourth, at
	// 1.02, waits.
	r := reading(0.85, 0.2)
	r.At = start.Add(-time.Second)
	g.Observe(r)
	for range 4 {
		if err := callAt(t, g, "u", 0.4); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := c.Waits(), []time.Time{start.Add(2*time.Hour + time.Minute)}; !slices.Equal(got, want) {
		t.Errorf("waits = %v, want the fourth call to wait: %v", got, want)
	}
	// A later reading holds what those calls spent.
	later := reading(0.85, 0.2)
	later.At = c.Now()
	later.Limits[provider.FiveHour] = provider.Limit{Utilization: 0.85, ResetsAt: c.Now().Add(5 * time.Hour)}
	g.Observe(later)
	if err := callAt(t, g, "u", 0.4); err != nil {
		t.Fatal(err)
	}
	if n := len(c.Waits()); n != 1 {
		t.Errorf("waited %d times, want the call after the later reading at once", n)
	}
}

// Refusals in a row that no reset still to come explains stop the run after
// MaxUnexplained, each having been waited out for a minute.
func TestGate_UnexplainedRefusalsStopTheRun(t *testing.T) {
	c, s := newClock(), &said{}
	passed := &provider.UsageLimited{Limit: provider.FiveHour, ResetsAt: start.Add(-time.Hour), Text: "You've hit your session limit"}
	f := &fake{clock: c, replies: []answer{{err: passed}}}
	g := newGate(f, c, s)
	var refused *usagelimit.Refused
	for i := range usagelimit.MaxUnexplained - 1 {
		if err := callAt(t, g, "u", 0.1); !errors.As(err, &refused) {
			t.Fatalf("refusal %d: error %v is not a *Refused", i+1, err)
		}
	}
	var unreadable *usagelimit.Unreadable
	err := callAt(t, g, "u", 0.1)
	if !errors.As(err, &unreadable) || unreadable.Refusals != usagelimit.MaxUnexplained {
		t.Fatalf("error %v is not an *Unreadable after %d refusals", err, usagelimit.MaxUnexplained)
	}
	if !strings.Contains(err.Error(), "3 calls in a row") {
		t.Errorf("error %q doesn't say how many refusals", err)
	}
	if n := len(c.Waits()); n != usagelimit.MaxUnexplained-1 {
		t.Errorf("waited %d times, want %d", n, usagelimit.MaxUnexplained-1)
	}
}

// A call the gate never started fails with a *NotStarted, which wraps why.
func TestGate_NotStarted(t *testing.T) {
	c, s := newClock(), &said{}
	g := newGate(&fake{clock: c}, c, s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := g.Call(ctx, provider.Request{})
	var notStarted *usagelimit.NotStarted
	if !errors.As(err, &notStarted) || !errors.Is(err, context.Canceled) {
		t.Errorf("error %v, want a *NotStarted wrapping context.Canceled", err)
	}
}
