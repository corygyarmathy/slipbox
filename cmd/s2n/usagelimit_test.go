package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
	"github.com/corygyarmathy/slipbox/internal/usagelimit"
)

// TestMain keeps every test off the network and off the wall clock: the
// account's usage is a reading with room, taken before any a test's run
// folder holds, and the gate's clock doesn't sleep.
func TestMain(m *testing.M) {
	accountUsage = func(context.Context) (*provider.Reading, error) {
		return &provider.Reading{Limits: map[string]provider.Limit{provider.FiveHour: {}}}, nil
	}
	clock = &fakeClock{now: time.Now()}
	os.Exit(m.Run())
}

// fakeClock is a usagelimit.Clock whose Wait moves the time on at once and
// records it.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Wait(ctx context.Context, until time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waits = append(c.waits, until)
	c.now = until
	return ctx.Err()
}

// useClock makes c the gate's clock for the rest of t.
func useClock(t *testing.T, c *fakeClock) {
	old := clock
	clock = c
	t.Cleanup(func() { clock = old })
}

// refuse fails every call as claude does for a usage limit, with resetsAt
// as the limit's reset time.
type refuse struct{ resetsAt time.Time }

func (r refuse) Call(ctx context.Context, req provider.Request) (provider.Reply, error) {
	return provider.Reply{Text: "You've hit your limit"}, &provider.UsageLimited{ResetsAt: r.resetsAt, Text: "You've hit your limit"}
}

// A resume takes its latest reading from the run folder's usage logs: a
// 5-hour limit the stopped attempt found full holds step 1.1's next call
// until it resets.
func TestResume_TakesTheLatestReadingFromTheUsageLogs(t *testing.T) {
	sourcePath := testfixture.SourcePath(t, testfixture.Pragmatism)
	src := testfixture.PragmatismSource(t)
	runDir := filepath.Join(t.TempDir(), "run")
	flags := []string{"-stop-after", run.ExtractDir}
	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), append(append([]string{"run", "-run-dir", runDir}, flags...), sourcePath), &stdout, &stderr, refuse{}); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stderr.String())
	}

	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	reset := now.Add(3 * time.Hour)
	full := `{"step":"1.1-extract","segment":0,"is_error":true,"reading":{"at":"` + now.Add(-time.Minute).Format(time.RFC3339) +
		`","status":"allowed","limit":"five_hour","limits":{"five_hour":{"utilization":0.995,"resets_at":"` + reset.Format(time.RFC3339) + `"}}}}` + "\n"
	log := filepath.Join(runDir, run.ExtractDir+run.PartialSuffix, run.UsageFile)
	if err := os.WriteFile(log, []byte(full), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &fakeClock{now: now}
	useClock(t, c)
	p, _ := pragmatismReplay(t, src, extractStep)
	stderr.Reset()
	if code := runCommand(context.Background(), append(append([]string{"resume"}, flags...), runDir), &stdout, &stderr, p); code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, stderr.String())
	}
	if want := []time.Time{reset.Add(usagelimit.AfterReset)}; !slices.Equal(c.waits, want) {
		t.Errorf("waits = %v, want %v", c.waits, want)
	}
	if !strings.Contains(stderr.String(), "5-hour usage limit has no room, so the run waits until") {
		t.Errorf("stderr does not say what the run waits for:\n%s", stderr.String())
	}
}

// Ctrl-C stops the run with the command that resumes it.
func TestRun_InterruptedSaysHowToResume(t *testing.T) {
	sourcePath := testfixture.SourcePath(t, testfixture.Pragmatism)
	runDir := filepath.Join(t.TempDir(), "run")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	if code := runCommand(ctx, []string{"run", "-run-dir", runDir, "-five-hour-usd", "35", "-stop-after", run.ExtractDir, sourcePath}, &stdout, &stderr, refuse{}); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stderr.String())
	}
	if want := "s2n run: interrupted. Run s2n resume -five-hour-usd 35 " + runDir + " to go on.\n"; !strings.HasSuffix(stderr.String(), want) {
		t.Errorf("stderr = %q, want it to end %q", stderr.String(), want)
	}
}

// interrupting is a usagelimit.Clock that interrupts the run, as Ctrl-C
// does, when the gate waits.
type interrupting struct {
	now    time.Time
	cancel context.CancelFunc
}

func (c interrupting) Now() time.Time { return c.now }

func (c interrupting) Wait(ctx context.Context, until time.Time) error {
	c.cancel()
	return ctx.Err()
}

// Ctrl-C during a wait stops the run with the command that resumes it, and
// the call that waited isn't logged, as it was never made.
func TestRun_InterruptedWhileWaiting(t *testing.T) {
	sourcePath := testfixture.SourcePath(t, testfixture.Pragmatism)
	runDir := filepath.Join(t.TempDir(), "run")
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	oldUsage := accountUsage
	accountUsage = func(context.Context) (*provider.Reading, error) {
		return &provider.Reading{At: now, Limits: map[string]provider.Limit{provider.FiveHour: {Utilization: 0.995, ResetsAt: now.Add(time.Hour)}}}, nil
	}
	t.Cleanup(func() { accountUsage = oldUsage })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	oldClock := clock
	clock = interrupting{now: now, cancel: cancel}
	t.Cleanup(func() { clock = oldClock })
	var stdout, stderr bytes.Buffer
	if code := runCommand(ctx, []string{"run", "-run-dir", runDir, "-stop-after", run.ExtractDir, sourcePath}, &stdout, &stderr, refuse{}); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stderr.String())
	}
	got := stderr.String()
	for _, want := range []string{"5-hour usage limit has no room, so the run waits until", "s2n run: interrupted. Run s2n resume " + runDir + " to go on.\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("stderr does not say %q:\n%s", want, got)
		}
	}
	if b, err := os.ReadFile(filepath.Join(runDir, run.ExtractDir+run.PartialSuffix, run.UsageFile)); err == nil && len(b) > 0 {
		t.Errorf("a call never made was logged: %s", b)
	}
}

// A usage limit whose reset time can't be read stops the run, saying what
// to do.
func TestRun_UnreadableResetStopsTheRun(t *testing.T) {
	sourcePath := testfixture.SourcePath(t, testfixture.Pragmatism)
	runDir := filepath.Join(t.TempDir(), "run")
	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"run", "-run-dir", runDir, "-stop-after", run.ExtractDir, sourcePath}, &stdout, &stderr, refuse{}); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stderr.String())
	}
	got := stderr.String()
	for _, want := range []string{"can't read when it resets", "You've hit your limit", "Once the limit has reset", "run s2n resume " + runDir + "."} {
		if !strings.Contains(got, want) {
			t.Errorf("stderr does not say %q:\n%s", want, got)
		}
	}
}

// -five-hour-usd must be a positive number of dollars.
func TestRunCommand_FiveHourUSD(t *testing.T) {
	for _, v := range []string{"0", "-3", "NaN"} {
		var stdout, stderr bytes.Buffer
		if code := runCommand(context.Background(), []string{"run", "-five-hour-usd", v, "a.md"}, &stdout, &stderr, nil); code != 2 {
			t.Errorf("-five-hour-usd %s: exit %d, want 2", v, code)
		}
	}
}
