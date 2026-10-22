package modelstep_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/corygyarmathy/slipbox/internal/modelstep"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
	"github.com/corygyarmathy/slipbox/internal/usagelimit"
)

// events is a provider that logs when each segment's call starts and ends,
// and tracks how many calls are in flight. Segment 0's call is slow, so a
// runner that didn't wait for it would log another start first.
type events struct {
	mu       sync.Mutex
	log      []string
	inFlight int
	maxIn    int
}

func (e *events) Call(ctx context.Context, req provider.Request) (provider.Reply, error) {
	e.mu.Lock()
	e.log = append(e.log, "start "+req.User)
	e.inFlight++
	e.maxIn = max(e.maxIn, e.inFlight)
	e.mu.Unlock()

	if req.User == "0" {
		time.Sleep(50 * time.Millisecond)
	} else {
		time.Sleep(10 * time.Millisecond)
	}

	e.mu.Lock()
	e.log = append(e.log, "end "+req.User)
	e.inFlight--
	e.mu.Unlock()
	return provider.Reply{Text: "=== FILE: " + req.User + ".md ===\nnote\n=== REPORT ===\nreport " + req.User + "\n"}, nil
}

func numberPrompt(_ *source.Source, seg source.Segment) (string, bool) {
	return fmt.Sprint(seg.Number), true
}

// invented is the text of an invented source of segments segments: the text
// before its first `##` heading, then one section per segment after it. The
// runner reads nothing of a source but its name, frame and segments.
func invented(segments int) string {
	text := "---\nsource: \"[[@Invented - A. Author]]\"\n---\n\n# Invented\n\nThe text before the first heading.\n"
	for n := 1; n < segments; n++ {
		text += fmt.Sprintf("\n## Section %d\n\nThe text of section %d.\n", n, n)
	}
	return text
}

// inventedSource parses invented(segments), named "Invented".
func inventedSource(t *testing.T, segments int) *source.Source {
	t.Helper()
	src, err := source.Parse("Invented", invented(segments))
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Segments) != segments {
		t.Fatalf("invented source has %d segments, want %d", len(src.Segments), segments)
	}
	return src
}

func newStep(t *testing.T) *run.Step {
	t.Helper()
	f, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	step, err := f.Begin("step")
	if err != nil {
		t.Fatal(err)
	}
	return step
}

func TestRun_Segment0First_ThenAtMostParallel(t *testing.T) {
	src := inventedSource(t, 8)
	p := &events{}
	step := newStep(t)
	results, err := modelstep.Run(context.Background(), p, src, modelstep.Spec{Brief: "B", Model: "m", Effort: "medium", Prompt: numberPrompt}, step)
	if err != nil {
		t.Fatal(err)
	}
	if p.log[0] != "start 0" || p.log[1] != "end 0" {
		t.Errorf("segment 0's call did not complete before the others started: %q", p.log)
	}
	if p.maxIn > modelstep.Parallel {
		t.Errorf("%d calls in flight, want at most %d", p.maxIn, modelstep.Parallel)
	}
	if p.maxIn < 2 {
		t.Errorf("the segments after 0 did not run in parallel: at most %d in flight", p.maxIn)
	}
	for n, r := range results {
		if r.Segment.Number != n {
			t.Errorf("results[%d] is segment %d", n, r.Segment.Number)
		}
		if want := fmt.Sprintf("report %d\n", n); r.Tail != want {
			t.Errorf("results[%d].Tail = %q, want %q", n, r.Tail, want)
		}
		raw, err := os.ReadFile(filepath.Join(step.Dir, run.RawDir, fmt.Sprintf("%d.txt", n)))
		if err != nil {
			t.Errorf("raw reply for segment %d not kept: %v", n, err)
		} else if want := fmt.Sprintf("=== FILE: %d.md ===\nnote\n=== REPORT ===\nreport %d\n", n, n); string(raw) != want {
			t.Errorf("raw/%d.txt = %q, want %q", n, raw, want)
		}
	}
}

// A segment the step has no prompt for gets no call and no result, and the
// first segment called goes first.
func TestRun_SkipsSegmentsWithNoPrompt(t *testing.T) {
	src := inventedSource(t, 8)
	odd := func(_ *source.Source, seg source.Segment) (string, bool) {
		return fmt.Sprint(seg.Number), seg.Number%2 == 1
	}
	p := &events{}
	step := newStep(t)
	results, err := modelstep.Run(context.Background(), p, src, modelstep.Spec{Effort: "medium", Prompt: odd}, step)
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, r := range results {
		got = append(got, r.Segment.Number)
	}
	if want := []int{1, 3, 5, 7}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("results are segments %v, want %v", got, want)
	}
	if p.log[0] != "start 1" || p.log[1] != "end 1" {
		t.Errorf("segment 1's call did not complete before the others started: %q", p.log)
	}
	if len(p.log) != 8 {
		t.Errorf("%d call events, want 8 (4 calls): %q", len(p.log), p.log)
	}
	if _, err := os.Stat(filepath.Join(step.Dir, run.RawDir, "0.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("raw reply written for skipped segment 0 (stat err %v)", err)
	}

	none := func(*source.Source, source.Segment) (string, bool) { return "", false }
	if results, err := modelstep.Run(context.Background(), p, src, modelstep.Spec{Effort: "medium", Prompt: none}, newStep(t)); err != nil || len(results) != 0 {
		t.Errorf("Run with no prompts = %d results, %v; want none", len(results), err)
	}
}

func TestRun_SystemPrompt(t *testing.T) {
	src := inventedSource(t, 5)
	p := provider.NewReplay(nil)
	_, err := modelstep.Run(context.Background(), p, src, modelstep.Spec{Brief: "BRIEF", Model: "m", Effort: "medium", Prompt: numberPrompt}, newStep(t))
	if err == nil {
		t.Fatal("Run succeeded with no recorded replies")
	}
	calls := p.Calls()
	if len(calls) != 1 {
		t.Fatalf("%d calls after segment 0 failed, want 1", len(calls))
	}
	// The trialled shape: brief, blank line, the frame block.
	want := "BRIEF\n\n=== FRAME: Invented.md ===\n" + invented(5) + "\n=== END FRAME ===\n"
	if calls[0].System != want {
		t.Error("system prompt is not the brief and frame in the trialled shape")
	}
	if calls[0].Model != "m" || calls[0].Effort != "medium" {
		t.Errorf("model and effort = %q, %q, want %q, %q", calls[0].Model, calls[0].Effort, "m", "medium")
	}
}

// A step that states no effort level, or one claude doesn't take, makes no
// call.
func TestRun_EffortLevelRequired(t *testing.T) {
	src := inventedSource(t, 5)
	for _, effort := range []string{"", "auto", "Medium"} {
		p := provider.NewReplay(nil)
		_, err := modelstep.Run(context.Background(), p, src, modelstep.Spec{Brief: "BRIEF", Model: "m", Effort: effort, Prompt: numberPrompt}, newStep(t))
		if err == nil || !strings.Contains(err.Error(), "effort level") {
			t.Errorf("effort %q: err = %v, want one naming the effort level", effort, err)
		}
		if n := len(p.Calls()); n != 0 {
			t.Errorf("effort %q: %d calls, want none", effort, n)
		}
	}
}

// A step's shared material follows the frame after a blank line, in every
// call's system prompt alike, and the user prompts stay the segments' own.
func TestRun_SharedFollowsTheFrame(t *testing.T) {
	src := inventedSource(t, 5)
	replies := map[string][]provider.Reply{}
	for _, seg := range src.Segments {
		replies[fmt.Sprint(seg.Number)] = []provider.Reply{{}}
	}
	p := provider.NewReplay(replies)
	spec := modelstep.Spec{Brief: "BRIEF", Model: "m", Effort: "medium", Shared: "=== SHARED ===\nS\n=== END SHARED ===\n\n", Prompt: numberPrompt}
	if _, err := modelstep.Run(context.Background(), p, src, spec, newStep(t)); err != nil {
		t.Fatal(err)
	}
	want := "BRIEF\n\n=== FRAME: Invented.md ===\n" + invented(5) + "\n=== END FRAME ===\n\n" +
		"=== SHARED ===\nS\n=== END SHARED ===\n\n"
	calls := p.Calls()
	if len(calls) != len(src.Segments) {
		t.Fatalf("%d calls, want %d", len(calls), len(src.Segments))
	}
	var users []string
	for _, c := range calls {
		if c.System != want {
			t.Errorf("call %q: system prompt is not the brief, the frame and the shared material", c.User)
		}
		users = append(users, c.User)
	}
	slices.Sort(users)
	if got := strings.Join(users, " "); got != "0 1 2 3 4" {
		t.Errorf("user prompts %q, want each segment's number once", got)
	}
}

// failOn fails the call for one segment and serves an empty reply otherwise.
type failOn string

func (f failOn) Call(ctx context.Context, req provider.Request) (provider.Reply, error) {
	if req.User == string(f) {
		return provider.Reply{}, errors.New("boom")
	}
	return provider.Reply{}, ctx.Err()
}

func TestRun_AFailureFailsTheStep(t *testing.T) {
	src := inventedSource(t, 5)
	_, err := modelstep.Run(context.Background(), failOn("3"), src, modelstep.Spec{Effort: "medium", Prompt: numberPrompt}, newStep(t))
	if err == nil {
		t.Fatal("Run succeeded although segment 3 failed")
	}
}

// spent replies with fixed usage, and fails segment 1's call after
// reporting usage, as `claude -p` does for a reply with is_error true.
type spent struct{}

func (spent) Call(ctx context.Context, req provider.Request) (provider.Reply, error) {
	u := provider.Usage{
		Seconds: 12.5,
		CostUSD: 0.25,
		Turns:   1,
		Models:  map[string]provider.Tokens{"claude-opus-5-5": {InputTokens: 3, CacheCreationInputTokens: 2000, CacheReadInputTokens: 50, OutputTokens: 700}},
	}
	if req.User == "1" {
		u.IsError = true
		return provider.Reply{Text: "Prompt is too long", Usage: u}, errors.New("is_error")
	}
	return provider.Reply{Text: "=== REPORT ===\n", Usage: u}, nil
}

// Each call appends one usage record, a failed call that reported usage
// included, with what it replied.
func TestRun_LogsEachCallsUsage(t *testing.T) {
	src := inventedSource(t, 5)
	step := newStep(t)
	if _, err := modelstep.Run(context.Background(), spent{}, src, modelstep.Spec{BriefFile: "test.md", Model: "opus", Effort: "high", Prompt: numberPrompt}, step); err == nil {
		t.Fatal("Run succeeded although segment 1 failed")
	}
	b, err := os.ReadFile(filepath.Join(step.Dir, run.UsageFile))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	seen := map[int]bool{}
	for _, line := range lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("usage line %q: %v", line, err)
		}
		n := int(rec["segment"].(float64))
		seen[n] = true
		want := fmt.Sprintf(`{"step":"step","segment":%d,"model":"opus","effort":"high","brief":"test.md","seconds":12.5,"cost_usd":0.25,"is_error":%v,"turns":1,`+
			`"models":{"claude-opus-5-5":{"input_tokens":3,"cache_creation_input_tokens":2000,"cache_read_input_tokens":50,"output_tokens":700}}}`, n, n == 1)
		if n == 1 {
			want = strings.TrimSuffix(want, "}") + `,"result":"Prompt is too long"}`
		}
		if line != want {
			t.Errorf("usage line\n%s\nwant\n%s", line, want)
		}
	}
	if !seen[0] || !seen[1] {
		t.Errorf("usage logged for segments %v, want 0 and the failed 1 among them", seen)
	}
	if len(lines) != len(seen) {
		t.Errorf("%d usage lines for %d segments", len(lines), len(seen))
	}
}

// broken fails every call without reporting usage, as `claude -p` does when
// it exits non-zero or prints no result.
type broken struct{}

func (broken) Call(ctx context.Context, req provider.Request) (provider.Reply, error) {
	return provider.Reply{Usage: provider.Usage{Seconds: 1.5}}, errors.New("exited with status 1")
}

// A failed call that reported no tokens is still logged, with is_error true.
func TestRun_LogsAFailedCallWithoutTokens(t *testing.T) {
	src := inventedSource(t, 5)
	step := newStep(t)
	if _, err := modelstep.Run(context.Background(), broken{}, src, modelstep.Spec{BriefFile: "test.md", Model: "opus", Effort: "high", Prompt: numberPrompt}, step); err == nil {
		t.Fatal("Run succeeded although every call failed")
	}
	b, err := os.ReadFile(filepath.Join(step.Dir, run.UsageFile))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"step":"step","segment":0,"model":"opus","effort":"high","brief":"test.md","seconds":1.5,"cost_usd":0,"is_error":true,"turns":0,"models":null}` + "\n"
	if string(b) != want {
		t.Errorf("usage log = %q, want %q", b, want)
	}
}

// Two numbered runs of one step keep their raw replies apart and name their
// run in each usage record.
func TestRun_NumberedRuns(t *testing.T) {
	src := inventedSource(t, 5)
	step := newStep(t)
	for k := 1; k <= 2; k++ {
		if _, err := modelstep.Run(context.Background(), &events{}, src, modelstep.Spec{Model: "m", Effort: "medium", Prompt: numberPrompt, Run: k}, step); err != nil {
			t.Fatal(err)
		}
	}
	files := testfixture.Files(t, step.Dir)
	for k := 1; k <= 2; k++ {
		for n := range len(src.Segments) {
			if _, ok := files[fmt.Sprintf("run-%d/%s/%d.txt", k, run.RawDir, n)]; !ok {
				t.Errorf("run %d's raw reply for segment %d not kept", k, n)
			}
		}
	}
	if _, ok := files[run.RawDir+"/0.txt"]; ok {
		t.Error("a numbered run wrote raw/ outside its run directory")
	}
	runs := map[float64]int{}
	for _, line := range strings.Split(strings.TrimSuffix(files[run.UsageFile], "\n"), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("usage line %q: %v", line, err)
		}
		r, _ := rec["run"].(float64)
		runs[r]++
	}
	if runs[1] != len(src.Segments) || runs[2] != len(src.Segments) || len(runs) != 2 {
		t.Errorf("usage records by run = %v, want %d each for runs 1 and 2", runs, len(src.Segments))
	}
}

func TestTotals_AddsUpEveryUsageLog(t *testing.T) {
	dir := t.TempDir()
	logs := map[string]string{
		"1.1-extract/" + run.UsageFile: `{"step":"1.1-extract","segment":0,"cost_usd":1.25,"seconds":60}
{"step":"1.1-extract","segment":1,"cost_usd":0.5,"seconds":30}
`,
		"1.6-kept-loss/1.6-recheck-1.2/" + run.UsageFile: `{"step":"x","cost_usd":0.25,"seconds":30,"is_error":true}` + "\n",
		"1.1-extract/raw/0.txt":                          "not a usage log\n",
	}
	for rel, text := range logs {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := modelstep.Totals(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := (modelstep.Total{Calls: 3, CostUSD: 2, Seconds: 120}); got != want {
		t.Errorf("Totals = %+v, want %+v", got, want)
	}

	if err := os.WriteFile(filepath.Join(dir, run.UsageFile), []byte("{not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := modelstep.Totals(dir); err == nil {
		t.Errorf("Totals of a broken log = nil error, want one")
	}
}

// counting serves each segment a reply naming its prompt, fails the call
// whose user prompt is fail, and counts the calls by user prompt.
type counting struct {
	mu    sync.Mutex
	fail  string
	calls map[string]int
}

func (c *counting) Call(ctx context.Context, req provider.Request) (provider.Reply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.calls == nil {
		c.calls = map[string]int{}
	}
	c.calls[req.User]++
	if req.User == c.fail {
		return provider.Reply{}, errors.New("boom")
	}
	return provider.Reply{Text: "=== REPORT ===\nreply to " + req.User + "\n"}, nil
}

// restart begins step's step again in its run folder, as `s2n resume` does
// after the attempt stopped.
func restart(t *testing.T, step *run.Step) *run.Step {
	t.Helper()
	next, err := (&run.Folder{Dir: filepath.Dir(step.Dir)}).Begin(step.Name)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

// A resume after a stopped attempt calls only the segments it didn't finish,
// gives every segment's result, and appends its calls to the usage log, in a
// step run once and in a numbered run, the loss proposer's.
func TestRun_ResumeCallsOnlyTheUnfinished(t *testing.T) {
	for _, k := range []int{0, 2} {
		t.Run(fmt.Sprintf("run %d", k), func(t *testing.T) { resumeCallsOnlyTheUnfinished(t, k) })
	}
}

func resumeCallsOnlyTheUnfinished(t *testing.T, k int) {
	src := inventedSource(t, 5)
	spec := modelstep.Spec{Model: "m", Effort: "medium", Prompt: numberPrompt, Run: k}
	step := newStep(t)
	first := &counting{fail: "3"}
	if _, err := modelstep.Run(context.Background(), first, src, spec, step); err == nil {
		t.Fatal("Run succeeded although segment 3 failed")
	}
	finished := map[string]bool{}
	for n := range len(src.Segments) {
		if _, err := os.Stat(filepath.Join(step.Dir, filepath.FromSlash(modelstep.RunDir(k)), run.RequestsDir, fmt.Sprintf("%d.sha256", n))); err == nil {
			finished[fmt.Sprint(n)] = true
		}
	}
	// Segment 0 goes first, so it always finishes; the others may have been
	// cancelled when 3 failed.
	if !finished["0"] || finished["3"] {
		t.Fatalf("finished %v: want segment 0, called first, and not the failed 3", finished)
	}

	step = restart(t, step)
	second := &counting{}
	results, err := modelstep.Run(context.Background(), second, src, spec, step)
	if err != nil {
		t.Fatal(err)
	}
	for n := range len(src.Segments) {
		user := fmt.Sprint(n)
		want := 1
		if finished[user] {
			want = 0
		}
		if second.calls[user] != want {
			t.Errorf("segment %d: %d calls on resume, want %d", n, second.calls[user], want)
		}
		if r := results[n]; r.Segment.Number != n || r.Tail != "reply to "+user+"\n" {
			t.Errorf("results[%d] = segment %d, tail %q", n, r.Segment.Number, r.Tail)
		}
	}
	b, err := os.ReadFile(filepath.Join(step.Dir, run.UsageFile))
	if err != nil {
		t.Fatal(err)
	}
	made := 0
	for _, p := range []*counting{first, second} {
		for _, n := range p.calls {
			made += n
		}
	}
	if lines := strings.Count(string(b), "\n"); lines != made {
		t.Errorf("usage log holds %d calls, want both attempts' %d", lines, made)
	}
}

// A finished segment whose request changed since its reply, such as its
// notes edited by hand, is called again; the others are reused.
func TestRun_AChangedInputIsCalledAgain(t *testing.T) {
	src := inventedSource(t, 5)
	edited := false
	prompt := func(_ *source.Source, seg source.Segment) (string, bool) {
		if edited && seg.Number == 2 {
			return "2 edited", true
		}
		return fmt.Sprint(seg.Number), true
	}
	spec := modelstep.Spec{Model: "m", Effort: "medium", Prompt: prompt}
	step := newStep(t)
	if _, err := modelstep.Run(context.Background(), &counting{}, src, spec, step); err != nil {
		t.Fatal(err)
	}
	edited = true
	p := &counting{}
	if _, err := modelstep.Run(context.Background(), p, src, spec, restart(t, step)); err != nil {
		t.Fatal(err)
	}
	if want := map[string]int{"2 edited": 1}; !reflect.DeepEqual(p.calls, want) {
		t.Errorf("calls %v, want only segment 2's, whose prompt changed", p.calls)
	}

	// The brief is in every request, so changing it calls every segment.
	spec.Brief = "new brief"
	p = &counting{}
	if _, err := modelstep.Run(context.Background(), p, src, spec, restart(t, step)); err != nil {
		t.Fatal(err)
	}
	if len(p.calls) != len(src.Segments) {
		t.Errorf("%d segments called after the brief changed, want all %d", len(p.calls), len(src.Segments))
	}
}

// A reply the step's Check rejects fails the step once the other calls have
// finished, without cancelling them, and isn't finished: a resume calls its
// segment again and reuses the rest.
func TestRun_ARejectedReplyIsCalledAgain(t *testing.T) {
	src := inventedSource(t, 5)
	reject := true
	spec := modelstep.Spec{Model: "m", Effort: "medium", Prompt: numberPrompt, Check: func(r modelstep.Result) error {
		if reject && r.Segment.Number == 2 {
			return errors.New("no hub")
		}
		return nil
	}}
	step := newStep(t)
	p := &counting{}
	if _, err := modelstep.Run(context.Background(), p, src, spec, step); err == nil || err.Error() != "no hub" {
		t.Fatalf("err = %v, want the rejection", err)
	}
	if len(p.calls) != len(src.Segments) {
		t.Errorf("%d segments called, want all %d: a rejection cancels no call", len(p.calls), len(src.Segments))
	}
	if _, err := os.Stat(filepath.Join(step.Dir, run.RawDir, "2.txt")); err != nil {
		t.Errorf("the rejected reply isn't kept: %v", err)
	}

	reject = false
	step = restart(t, step)
	p = &counting{}
	if _, err := modelstep.Run(context.Background(), p, src, spec, step); err != nil {
		t.Fatal(err)
	}
	if want := map[string]int{"2": 1}; !reflect.DeepEqual(p.calls, want) {
		t.Errorf("calls on resume %v, want only the rejected segment 2's", p.calls)
	}
}

// A segment whose newer call was rejected keeps no request record, so going
// back to its older request calls it again rather than reusing the rejected
// reply under the older record.
func TestRun_AnOldRecordIsNotReusedForANewerReply(t *testing.T) {
	src := inventedSource(t, 5)
	user := "A"
	spec := modelstep.Spec{
		Model:  "m",
		Effort: "medium",
		Prompt: func(_ *source.Source, seg source.Segment) (string, bool) {
			return user, seg.Number == 1
		},
		Check: func(r modelstep.Result) error {
			if strings.Contains(r.Text, "reply to B") {
				return errors.New("rejected")
			}
			return nil
		},
	}
	step := newStep(t)
	if _, err := modelstep.Run(context.Background(), &counting{}, src, spec, step); err != nil {
		t.Fatal(err)
	}
	user = "B"
	step = restart(t, step)
	if _, err := modelstep.Run(context.Background(), &counting{}, src, spec, step); err == nil {
		t.Fatal("Run succeeded although the reply to B was rejected")
	}
	user = "A"
	step = restart(t, step)
	p := &counting{}
	results, err := modelstep.Run(context.Background(), p, src, spec, step)
	if err != nil {
		t.Fatal(err)
	}
	if p.calls["A"] != 1 || !strings.Contains(results[0].Text, "reply to A") {
		t.Errorf("request A: %d calls, reply %q; want it called again", p.calls["A"], results[0].Text)
	}
}

// fakeClock is a usagelimit.Clock whose Wait moves the time on at once, and
// records it, so no test sleeps for a usage limit.
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

// limited refuses the first call for segment refuse for a usage limit, as
// claude does, and answers every other call after a pause, so that the calls
// beside the refused one are still running when it is refused. A call whose
// context ends fails with its error.
type limited struct {
	refuse    string
	resetsAt  time.Time
	mu        sync.Mutex
	calls     map[string]int
	cancelled int
}

const refusedText = "You've hit your session limit · resets 7pm"

func (l *limited) Call(ctx context.Context, req provider.Request) (provider.Reply, error) {
	l.mu.Lock()
	l.calls[req.User]++
	first := l.calls[req.User] == 1
	l.mu.Unlock()
	if req.User == l.refuse && first {
		r := &provider.Reading{At: time.Now(), Status: "rejected", Limit: provider.FiveHour, ResetsAt: l.resetsAt,
			Limits: map[string]provider.Limit{provider.FiveHour: {Utilization: 1, ResetsAt: l.resetsAt}}}
		return provider.Reply{Text: refusedText, Usage: provider.Usage{Seconds: 3.4, Reading: r}},
			&provider.UsageLimited{Limit: provider.FiveHour, ResetsAt: l.resetsAt, Text: refusedText}
	}
	select {
	case <-time.After(30 * time.Millisecond):
	case <-ctx.Done():
		l.mu.Lock()
		l.cancelled++
		l.mu.Unlock()
		return provider.Reply{}, ctx.Err()
	}
	return provider.Reply{Text: "=== REPORT ===\nreport " + req.User + "\n", Usage: provider.Usage{CostUSD: 0.1}}, nil
}

// A call refused for a usage limit doesn't cancel the calls running beside
// it, which keep their replies: it is logged, waited out and made again.
func TestRun_ARefusalIsWaitedOut(t *testing.T) {
	src := inventedSource(t, 5)
	start := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	reset := start.Add(time.Hour)
	clock := &fakeClock{now: start}
	p := &limited{refuse: "2", resetsAt: reset, calls: map[string]int{}}
	gate := usagelimit.NewGate(p, 10, clock, func(string) {}, nil)
	step := newStep(t)
	results, err := modelstep.Run(context.Background(), gate, src, modelstep.Spec{Model: "opus", Effort: "medium", Prompt: numberPrompt}, step)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 5 {
		t.Fatalf("%d results, want 5", len(results))
	}
	for n, r := range results {
		if want := fmt.Sprintf("report %d\n", n); r.Tail != want {
			t.Errorf("segment %d's reply = %q, want %q", n, r.Tail, want)
		}
		if _, err := os.Stat(filepath.Join(step.Dir, run.RawDir, fmt.Sprintf("%d.txt", n))); err != nil {
			t.Errorf("segment %d's raw reply not kept: %v", n, err)
		}
	}
	for user, n := range p.calls {
		if want := map[bool]int{true: 2, false: 1}[user == "2"]; n != want {
			t.Errorf("segment %s was called %d times, want %d", user, n, want)
		}
	}
	if want := []time.Time{reset.Add(usagelimit.AfterReset)}; !slices.Equal(clock.waits, want) {
		t.Errorf("waits = %v, want %v", clock.waits, want)
	}
	if p.cancelled > 0 {
		t.Errorf("%d calls were cancelled", p.cancelled)
	}

	b, err := os.ReadFile(filepath.Join(step.Dir, run.UsageFile))
	if err != nil {
		t.Fatal(err)
	}
	var refused []map[string]any
	for line := range strings.Lines(string(b)) {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatal(err)
		}
		if rec["is_error"] == true {
			refused = append(refused, rec)
		}
	}
	if len(refused) != 1 || refused[0]["segment"] != 2.0 || refused[0]["result"] != refusedText {
		t.Fatalf("failed calls logged = %v, want segment 2's refusal with its result", refused)
	}
	reading, _ := refused[0]["reading"].(map[string]any)
	if reading["status"] != "rejected" || reading["limit"] != provider.FiveHour {
		t.Errorf("the refusal's reading = %v", refused[0]["reading"])
	}
}

// A refusal whose reset time can't be read is the step's error, once the
// calls beside it have finished and kept their replies. A call not yet
// started when it came isn't started.
func TestRun_AnUnreadableResetCancelsNothing(t *testing.T) {
	src := inventedSource(t, 5)
	clock := &fakeClock{now: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)}
	p := &limited{refuse: "2", calls: map[string]int{}}
	gate := usagelimit.NewGate(p, 10, clock, func(string) {}, nil)
	step := newStep(t)
	_, err := modelstep.Run(context.Background(), gate, src, modelstep.Spec{Model: "opus", Effort: "medium", Prompt: numberPrompt}, step)
	var unreadable *usagelimit.Unreadable
	if !errors.As(err, &unreadable) {
		t.Fatalf("error %v is not an *Unreadable", err)
	}
	if p.cancelled > 0 {
		t.Errorf("%d calls were cancelled", p.cancelled)
	}
	for user, n := range p.calls {
		if _, err := os.Stat(filepath.Join(step.Dir, run.RawDir, user+".txt")); user != "2" && n > 0 && err != nil {
			t.Errorf("segment %s's raw reply not kept: %v", user, err)
		}
	}
	if p.calls["2"] != 1 {
		t.Errorf("segment 2 was called %d times, want once", p.calls["2"])
	}
	// A call never started isn't logged: the log has one line a call made.
	b, err := os.ReadFile(filepath.Join(step.Dir, run.UsageFile))
	if err != nil {
		t.Fatal(err)
	}
	made := 0
	for _, n := range p.calls {
		made += n
	}
	if logged := strings.Count(string(b), "\n"); logged != made {
		t.Errorf("%d calls logged, %d made, want one line a call made", logged, made)
	}
}

// costs records each call's predicted cost, by user prompt, and replies
// with a cost of $0.30.
type costs struct {
	mu   sync.Mutex
	cost map[string]float64
}

func (c *costs) Call(ctx context.Context, req provider.Request) (provider.Reply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cost[req.User] = usagelimit.Cost(ctx)
	return provider.Reply{Text: "=== REPORT ===\n", Usage: provider.Usage{CostUSD: 0.3}}, nil
}

// A step's first call is predicted at its first-call rate, and each later
// one at the step's own rate so far, by its prompt's size in characters.
func TestRun_PredictsEachCallsCost(t *testing.T) {
	src := inventedSource(t, 5)
	p := &costs{cost: map[string]float64{}}
	spec := modelstep.Spec{Brief: "B", Model: "opus", Effort: "medium", FirstCallRate: 2e-6, Prompt: numberPrompt}
	if _, err := modelstep.Run(context.Background(), p, src, spec, newStep(t)); err != nil {
		t.Fatal(err)
	}
	chars := float64(len([]rune(modelstep.SystemPrompt("B", src, ""))) + 1)
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-12 }
	if got, want := p.cost["0"], 2e-6*chars; !near(got, want) {
		t.Errorf("the first call's predicted cost = %v, want %v", got, want)
	}
	// Each later call's prompt is the same size as the first's, so the
	// step's rate predicts the $0.30 each finished call cost.
	for _, user := range []string{"1", "2", "3", "4"} {
		if got := p.cost[user]; !near(got, 0.3) {
			t.Errorf("segment %s's predicted cost = %v, want 0.3", user, got)
		}
	}
}

// LatestReading gives the latest reading, by when it was taken, in every
// usage log in the run folder.
func TestLatestReading(t *testing.T) {
	dir := t.TempDir()
	logs := map[string]string{
		"1.1-extract/" + run.UsageFile: `{"step":"1.1-extract","segment":0,"reading":{"at":"2026-10-21T09:00:00Z","limits":{"five_hour":{"utilization":0.1}}}}
{"step":"1.1-extract","segment":1}
`,
		"1.4-kept-loss-repair/recheck-1.3a-support-check.partial/" + run.UsageFile: `{"step":"x","reading":{"at":"2026-10-21T11:00:00Z","status":"rejected","limit":"five_hour","limits":{"five_hour":{"utilization":1}}}}` + "\n",
		"1.3a-support-check/" + run.UsageFile:                                      `{"step":"1.3a-support-check","reading":{"at":"2026-10-21T10:00:00Z","limits":{"five_hour":{"utilization":0.5}}}}` + "\n",
	}
	for rel, text := range logs {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := modelstep.LatestReading(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Status != "rejected" || !got.At.Equal(time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)) {
		t.Errorf("LatestReading = %+v, want the 11:00 rejected one", got)
	}
	if got, err := modelstep.LatestReading(t.TempDir()); got != nil || err != nil {
		t.Errorf("LatestReading of a folder with no logs = %+v, %v", got, err)
	}
}
