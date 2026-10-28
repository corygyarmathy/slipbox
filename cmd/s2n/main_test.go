package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/extract"
	"github.com/corygyarmathy/slipbox/internal/fallback"
	"github.com/corygyarmathy/slipbox/internal/format"
	"github.com/corygyarmathy/slipbox/internal/keptloss"
	"github.com/corygyarmathy/slipbox/internal/lossproposer"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/readsalone"
	"github.com/corygyarmathy/slipbox/internal/repair"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/stageout"
	"github.com/corygyarmathy/slipbox/internal/striptraces"
	"github.com/corygyarmathy/slipbox/internal/supportcheck"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// wantCall is a call a step is to make: its model, at medium effort, and its
// system and user prompts.
type wantCall struct{ model, system, user string }

// TestRun_Lamp runs `s2n run` on Lamp at the defaults, through every model
// step (lampEveryStep), stopped after the run report, then resumed to stage
// out, given an empty vault. Each step's calls are briefed and tiered as the
// defaults say, and each user prompt is built from what the steps before it
// left in the run folder, so the steps hand their work on through it. What
// each step writes is for its own tests to say.
func TestRun_Lamp(t *testing.T) {
	runDir := filepath.Join(t.TempDir(), "run")
	p := lampModel(t, runDir, lampEveryStep())
	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"run", "-run-dir", runDir, "-stop-after", run.RunReportDir, lampSourcePath(t)}, &stdout, &stderr, p); code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != runDir {
		t.Errorf("stdout = %q, want the run folder %q", got, runDir)
	}
	if want := "s2n run: stopped after " + run.RunReportDir + ", as -stop-after asked. Run s2n resume " + runDir + " to go on.\n"; stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
	folder := &run.Folder{Dir: runDir}
	if p, err := folder.SourcePath(); err != nil {
		t.Errorf("run folder has no copy of the source: %v", err)
	} else if b, err := os.ReadFile(p); err != nil || string(b) != testfixture.LampChapter {
		t.Errorf("run folder's source is not the chapter read (err %v)", err)
	} else if filepath.Base(p) != testfixture.Lamp+".md" {
		t.Errorf("run folder's source is named %q", filepath.Base(p))
	}

	// What each step was to be given, read from the run folder as the steps
	// before it left it.
	src := testfixture.LampSource(t)
	at := func(rel string) string { return filepath.Join(runDir, filepath.FromSlash(rel)) }
	notes := func(rel string) []*note.Note {
		n, err := note.ReadDir(at(rel))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	extracted, repaired, kept, final := notes(run.ExtractNotes), notes(run.RepairNotes), notes(run.KeptLossNotes), notes(run.FinalNotes)
	mech := testfixture.Files(t, at(run.MechanicalCheckDir))[run.ReportFile]
	uncovered := testfixture.Files(t, at(run.MechanicalRecheckDir+"/"+run.UncoveredDir))
	keptLosses, err := keptloss.Losses(folder)
	if err != nil {
		t.Fatal(err)
	}
	stripped, err := split.ReadFolders(at(run.StrippedNotes))
	if err != nil {
		t.Fatal(err)
	}
	// The run did what lampEveryStep sets it up to: the kept-loss repair
	// added its loss after the statement step 1.4 repaired, and no segment
	// fell back.
	if n := testfixture.Files(t, at(run.FinalNotes))["1 The Keeper's Watch.md"]; !strings.Contains(n, "also logged the weather in a book.[^4] Then the inspectors read every page.[^5]\n") {
		t.Errorf("the final notes do not hold the repair and the kept loss:\n%s", n)
	}
	for _, dir := range []string{run.SplitGatesDir, run.FormatGateDir} {
		if items, err := fallback.Read(folder, dir); err != nil || len(items) != 0 {
			t.Errorf("%s: fallbacks %+v (err %v), want none", dir, items, err)
		}
	}

	// A step's system prompt is its briefs, the frame and then what its
	// calls share, if anything.
	system := func(shared string, briefFiles ...string) string {
		var text []string
		for _, b := range briefFiles {
			text = append(text, testfixture.RepoFile(t, "internal/briefs/"+b))
		}
		s := strings.Join(text, "\n\n") + "\n\n" + lampFrame
		if shared != "" {
			s += "\n" + shared
		}
		return s
	}
	every := []int{0, 1, 2, 3}
	calls := func(model, system string, segs []int, user func(seg source.Segment) string) []wantCall {
		var out []wantCall
		for _, n := range segs {
			out = append(out, wantCall{model, system, user(src.Segments[n])})
		}
		return out
	}
	proposer := calls("sonnet", system(lossproposer.Shared(repaired), "proposer.md"), []int{1, 2, 3}, func(seg source.Segment) string {
		return lossproposer.Prompt(seg, uncovered[strconv.Itoa(seg.Number)+".txt"])
	})
	steps := []struct {
		name  string
		calls []wantCall
	}{
		{"1.1", calls("opus", system("", "stage-1-quote-once.md"), every, func(seg source.Segment) string {
			return extract.Prompt(src, seg)
		})},
		{"1.3a", calls("sonnet", system(supportcheck.Shared(extracted), "checker-support.md"), every, func(seg source.Segment) string {
			return supportcheck.Prompt(extracted, seg)
		})},
		{"1.3b", calls("sonnet", system("", "checker-reads-alone.md"), every, func(seg source.Segment) string {
			return readsalone.Prompt(extracted, seg)
		})},
		// Step 1.4 calls the one segment with a flag that counts, given step
		// 1.2's report and the segment's step 1.3 replies.
		{"1.4", calls("opus", system("", "repair-edits-quote-once.md", "stage-1-quote-once.md"), []int{1}, func(seg source.Segment) string {
			return repair.Prompt(extracted, seg, mech, "pass\n", watchDangling)
		})},
		{"1.4 re-check 1.3a", calls("sonnet", system(supportcheck.Shared(repaired), "checker-support.md"), []int{1}, func(seg source.Segment) string {
			return supportcheck.Prompt(repaired, seg)
		})},
		{"1.4 re-check 1.3b", calls("sonnet", system("", "checker-reads-alone.md"), []int{1}, func(seg source.Segment) string {
			return readsalone.Prompt(repaired, seg)
		})},
		// Step 1.5 calls each segment with an uncovered list, twice.
		{"1.5 run 1", proposer},
		{"1.5 run 2", proposer},
		// The kept-loss repair calls the one segment with a loss, given the
		// notes step 1.4 left, and re-checks it.
		{"kept-loss repair", calls("opus", system("", "kept-loss-edits-quote-once.md", "stage-1-quote-once.md"), []int{1}, func(seg source.Segment) string {
			return keptloss.Prompt(repaired, seg, keptLosses)
		})},
		{"kept-loss re-check 1.3a", calls("sonnet", system(supportcheck.Shared(kept), "checker-support.md"), []int{1}, func(seg source.Segment) string {
			return supportcheck.Prompt(kept, seg)
		})},
		{"2.1", calls("sonnet", system("", "stage-2.md"), every, func(seg source.Segment) string {
			prompt, _ := split.Prompt(src, final, seg)
			return prompt
		})},
		{"3.1", calls("haiku", system("", "stage-3.md"), every, func(seg source.Segment) string {
			for _, f := range stripped {
				if f.Name == split.Folder(seg) {
					return format.Prompt(f)
				}
			}
			t.Fatalf("no stripped notes for %s", seg)
			return ""
		})},
	}

	// The steps run in order, and each makes its calls before the next
	// begins: its first segment's first, then the rest in any order.
	got := p.Calls()
	total := 0
	for _, s := range steps {
		total += len(s.calls)
	}
	if len(got) != total {
		t.Fatalf("%d calls, want %d", len(got), total)
	}
	next := 0
	for _, s := range steps {
		made := got[next : next+len(s.calls)]
		next += len(s.calls)
		if made[0].User != s.calls[0].user {
			t.Errorf("step %s's first call was %.80q, want its first segment's", s.name, made[0].User)
		}
		for _, w := range s.calls {
			i := slices.IndexFunc(made, func(c provider.Request) bool { return c.User == w.user })
			if i < 0 {
				t.Errorf("step %s made no call for %.80q", s.name, w.user)
				continue
			}
			if c := made[i]; c.System != w.system {
				t.Errorf("step %s call for %.80q: system prompt is not its briefs, the frame and what its calls share", s.name, c.User)
			} else if c.Model != w.model || c.Effort != "medium" {
				t.Errorf("step %s call for %.80q: model %q at %q, want %s at medium", s.name, c.User, c.Model, c.Effort, w.model)
			}
		}
	}
	lines := 0
	for name, body := range testfixture.Files(t, runDir) {
		if filepath.Base(name) == run.UsageFile {
			lines += strings.Count(body, "\n")
		}
	}
	if lines != len(got) {
		t.Errorf("the usage logs hold %d calls, want the %d made", lines, len(got))
	}

	// Resumed with a vault, the run stages out what step 3.1's gate passed
	// on, with the run report written for staging, and deletes the run
	// folder.
	checked, err := split.ReadFolders(at(run.FormattedOrFallbackNotes))
	if err != nil {
		t.Fatal(err)
	}
	placed, _ := stageout.Place(src.Name, checked, nil)
	want := map[string]string{stageout.RunReportFile: testfixture.Files(t, at(run.RunReportDir))[run.StagedReportFile]}
	for _, pl := range placed {
		want[pl.Path()] = pl.Text
	}
	vault := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(filepath.Join(vault, stageout.SlipBoxDir), 0o755); err != nil {
		t.Fatal(err)
	}
	none := testfixture.Serve(t)
	stderr.Reset()
	if code := runCommand(context.Background(), []string{"resume", "-vault", vault, runDir}, &stdout, &stderr, none); code != 0 || len(none.Calls()) != 0 {
		t.Fatalf("resume: exit %d with %d calls, want 0 with none: %s", code, len(none.Calls()), stderr.String())
	}
	if w := fmt.Sprintf("The run's %d model calls cost $", len(got)); !strings.Contains(stderr.String(), w) {
		t.Errorf("stderr %q does not say %q", stderr.String(), w)
	}
	if _, err := os.Stat(runDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the run folder is not deleted (stat: %v)", err)
	}
	staged := testfixture.Files(t, stageout.DefaultStaging(vault, src.Name))
	if _, ok := staged[stageout.ClashListFile]; !ok {
		t.Errorf("the staging folder has no %s", stageout.ClashListFile)
	}
	delete(staged, stageout.ClashListFile)
	if !reflect.DeepEqual(keys(staged), keys(want)) {
		t.Fatalf("staged %q, want %q", keys(staged), keys(want))
	}
	for name, w := range want {
		if staged[name] != w {
			t.Errorf("staged %s:\n%s\nwant:\n%s", name, staged[name], w)
		}
	}
}

// stepCalls gives the calls of p briefed as step, as stepOf names it.
func stepCalls(p *testfixture.Fake, step string) []provider.Request {
	var out []provider.Request
	for _, c := range p.Calls() {
		if s, _ := stepOf(c); s == step {
			out = append(out, c)
		}
	}
	return out
}

// -stage-1-model replaces stage 1's tier for the steps run at it: step 1.1,
// step 1.4's repair call and the kept-loss repair, which -repair-model
// would otherwise set.
func TestRunCommand_Stage1Model(t *testing.T) {
	runDir, p, _ := lampRun(t, lampEveryStep(), "-stop-after", run.FinalDir, "-stage-1-model", "sonnet")
	for _, step := range []string{run.ExtractDir, run.RepairDir, run.KeptLossDir} {
		calls := stepCalls(p, step)
		if len(calls) == 0 {
			t.Errorf("step %s made no call", step)
		}
		for _, c := range calls {
			if c.Model != "sonnet" {
				t.Errorf("step %s call for %.60q: model %q, want sonnet", step, c.User, c.Model)
			}
		}
	}
	usage := testfixture.Files(t, filepath.Join(runDir, run.ExtractDir))[run.UsageFile]
	if n := strings.Count(usage, "\n"); n != 4 {
		t.Errorf("usage log has %d lines, want one per call (4)", n)
	}
	if !strings.Contains(usage, `"step":"`+run.ExtractDir+`"`) || !strings.Contains(usage, `"model":"sonnet"`) {
		t.Errorf("usage log does not name the step and model: %q", usage)
	}
}

// -extract-effort sets step 1.1's effort level, and only step 1.1's: every
// other step's calls go at its own level, and every usage line records the
// level its call went at.
func TestRunCommand_ExtractEffort(t *testing.T) {
	runDir, p, _ := lampRun(t, lampEveryStep(), "-stop-after", run.FinalDir, "-extract-effort", "low")
	if len(stepCalls(p, run.ExtractDir)) == 0 {
		t.Fatal("step 1.1 made no call")
	}
	for _, c := range p.Calls() {
		want := "medium"
		if s, _ := stepOf(c); s == run.ExtractDir {
			want = "low"
		}
		if c.Effort != want {
			t.Errorf("call for %.60q: effort %q, want %q", c.User, c.Effort, want)
		}
	}

	lines := 0
	for name, body := range testfixture.Files(t, runDir) {
		if filepath.Base(name) != run.UsageFile {
			continue
		}
		for _, line := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
			var rec struct{ Step, Effort string }
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			want := "medium"
			if rec.Step == run.ExtractDir {
				want = "low"
			}
			if rec.Effort != want {
				t.Errorf("%s: usage line %s records effort %q, want %q", name, line, rec.Effort, want)
			}
			lines++
		}
	}
	if lines != len(p.Calls()) {
		t.Errorf("%d usage lines for %d calls", lines, len(p.Calls()))
	}
}

// -repair-model and -repair-reply set step 1.4's repair call apart from stage
// 1's tier, and -stop-after stops the run after step 1.4's re-check.
// Resuming without the flags goes on through step 1.5.
func TestRunCommand_RepairConfiguration(t *testing.T) {
	runDir, p, stderr := lampRun(t, lampEveryStep(), "-repair-model", "sonnet", "-repair-reply", "edits", "-stop-after", run.ReadsAloneRecheckDir)
	if !strings.Contains(stderr, "stopped after "+run.ReadsAloneRecheckDir) {
		t.Errorf("stderr %q does not say where the run stopped", stderr)
	}
	folder := &run.Folder{Dir: runDir}
	if !folder.Complete(run.ReadsAloneRecheckDir) || folder.Complete(run.StillFailsDir) {
		t.Fatalf("the run did not stop right after step 1.4's re-check")
	}
	// Step 1.1 keeps stage 1's tier.
	for _, c := range stepCalls(p, run.ExtractDir) {
		if c.Model != extract.DefaultModel {
			t.Errorf("step 1.1 call for %.60q: model %q, want %q", c.User, c.Model, extract.DefaultModel)
		}
	}
	repairCalls := stepCalls(p, run.RepairDir)
	if len(repairCalls) != 1 {
		t.Fatalf("%d repair calls, want 1, segment 1's", len(repairCalls))
	}
	if c := repairCalls[0]; c.Model != "sonnet" || !strings.HasPrefix(c.System, testfixture.RepoFile(t, "internal/briefs/repair-edits-quote-once.md")+"\n\n") {
		t.Errorf("repair call: model %q, or not briefed by the edit-shaped brief", c.Model)
	}
	if usage := testfixture.Files(t, filepath.Join(runDir, run.RepairDir))[run.UsageFile]; !strings.Contains(usage, `"model":"sonnet"`) || strings.Contains(usage, `"model":"opus"`) {
		t.Errorf("step 1.4's usage log does not record sonnet alone:\n%s", usage)
	}

	resumed := lampModel(t, runDir, lampEveryStep())
	if code, errText := resumeTo(t, runDir, run.LossProposerDir, resumed); code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, errText)
	}
	if n, want := len(stepCalls(resumed, run.LossProposerDir)), len(resumed.Calls()); n != want || n != 6 {
		t.Errorf("resume made %d calls, %d of them step 1.5's, want step 1.5's alone, two for each of 3 segments", want, n)
	}
}

// -proposer-model sets the model of both of step 1.5's runs. Each 1.5 line
// in the usage log records it, and the way on names it. With no flag, step
// 1.5 asks for its tier, Sonnet.
func TestRunCommand_ProposerModel(t *testing.T) {
	for _, tc := range []struct {
		name, model string
		flags       []string
		way         string
	}{
		{"default", lossproposer.Model, nil, "s2n resume "},
		{"opus", "opus", []string{"-proposer-model", "opus"}, "s2n resume -proposer-model opus "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runDir, p, stderr := lampRun(t, nil, append([]string{"-stop-after", run.LossProposerDir}, tc.flags...)...)
			calls := stepCalls(p, run.LossProposerDir)
			if len(calls) != 6 {
				t.Fatalf("%d step 1.5 calls, want two for each of 3 segments", len(calls))
			}
			for _, c := range calls {
				if c.Model != tc.model {
					t.Errorf("step 1.5 call for %.60q: model %q, want %q", c.User, c.Model, tc.model)
				}
			}
			usage := testfixture.Files(t, filepath.Join(runDir, run.LossProposerDir))[run.UsageFile]
			lines := strings.Split(strings.TrimSuffix(usage, "\n"), "\n")
			if len(lines) != len(calls) {
				t.Fatalf("step 1.5's usage log has %d lines, want one per call (%d)", len(lines), len(calls))
			}
			for _, l := range lines {
				if !strings.Contains(l, `"model":"`+tc.model+`"`) {
					t.Errorf("usage line does not record %s: %s", tc.model, l)
				}
			}
			if want := "Run " + tc.way + runDir + " to go on."; !strings.Contains(stderr, want) {
				t.Errorf("stderr %q does not say %q", stderr, want)
			}
		})
	}
}

// -repair-model sets the kept-loss repair's model as well as step 1.4's
// repair call's, and step 1.1 keeps stage 1's tier.
func TestRunCommand_RepairModelSetsTheKeptLossRepair(t *testing.T) {
	_, p, _ := lampRun(t, lampEveryStep(), "-repair-model", "sonnet", "-stop-after", run.FinalDir)
	calls := stepCalls(p, run.KeptLossDir)
	if len(calls) != 1 {
		t.Fatalf("%d kept-loss calls, want 1, segment 1's", len(calls))
	}
	if calls[0].Model != "sonnet" {
		t.Errorf("kept-loss call: model %q, want sonnet", calls[0].Model)
	}
	for _, c := range stepCalls(p, run.ExtractDir) {
		if c.Model != extract.DefaultModel {
			t.Errorf("step 1.1 call for %.60q: model %q, want %q", c.User, c.Model, extract.DefaultModel)
		}
	}
}

// A run that can reach step 3.2 with no vault stops before it runs a step,
// so it spends no model call on notes it can't stage, and says what to do.
func TestRun_NoVaultStopsBeforeAnyCall(t *testing.T) {
	t.Setenv(VaultEnv, "")
	runDir := filepath.Join(t.TempDir(), "run")
	p := testfixture.Serve(t)
	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"run", "-run-dir", runDir, lampSourcePath(t)}, &stdout, &stderr, p); code != 1 || len(p.Calls()) != 0 {
		t.Fatalf("exit %d with %d calls, want 1 with none: %s", code, len(p.Calls()), stderr.String())
	}
	if want := "Give -vault DIR or set $" + VaultEnv + ", then run s2n resume " + runDir + ".\n"; !strings.HasSuffix(stderr.String(), want) {
		t.Errorf("stderr %q does not end %q", stderr.String(), want)
	}
	if (&run.Folder{Dir: runDir}).Complete(run.ExtractDir) {
		t.Error("step 1.1 ran")
	}
}

// A statement step 1.4 repaired that its re-check still flags stops
// nothing: the run report lists it, with the re-check's grade.
func TestRun_RepairFailingRecheckIsReported(t *testing.T) {
	const stillDangling = "# 1 The Keeper's Watch\n\n" +
		"- [dangling] \"The keeper also logged the weather in a book.\": \"also\" follows nothing.\n" +
		"pass 2 / dangling 1 / outside-frame 0\n"
	over := lampEveryStep()
	over[run.ReadsAloneDir][1] = append(over[run.ReadsAloneDir][1], stillDangling)
	runDir, _, _ := lampRun(t, over, "-stop-after", run.RunReportDir)
	const want = "- 1 The Keeper's Watch: The keeper[^3] also logged the weather in a book.[^4] (1.3b dangling: \"also\" follows nothing.)"
	if fails := runReport(t, runDir)["Statements that still fail"]; !strings.Contains(fails, want) {
		t.Errorf("Statements that still fail has no %q:\n%s", want, fails)
	}
}

// servePrompts returns a Fake that answers a call whose user prompt is
// prompts[i] with replies[i], so a test on an invented source checks that
// each step was given what the steps before it wrote, by giving no reply
// to any other prompt: that fails the test, and the call. Each reply is
// given once, in order for a prompt listed more than once, so a call made
// more often than listed fails too.
func servePrompts(t *testing.T, prompts, replies []string) *testfixture.Fake {
	t.Helper()
	if len(prompts) != len(replies) {
		t.Fatalf("%d prompts for %d replies", len(prompts), len(replies))
	}
	var mu sync.Mutex
	m := map[string][]string{}
	for i, p := range prompts {
		m[p] = append(m[p], replies[i])
	}
	return testfixture.NewFake(func(req provider.Request) (provider.Reply, error) {
		mu.Lock()
		defer mu.Unlock()
		if rs := m[req.User]; len(rs) > 0 {
			m[req.User] = rs[1:]
			return provider.Reply{Text: rs[0]}, nil
		}
		t.Errorf("no reply for the user prompt %.80q", req.User)
		return provider.Reply{}, fmt.Errorf("no reply for the user prompt %.80q", req.User)
	})
}

func TestRunCommand_Usage(t *testing.T) {
	for _, args := range [][]string{
		nil, {"resume"}, {"run"}, {"run", "a.md", "b.md"}, {"run", "-extract-effort", "auto", "a.md"},
		{"run", "-repair-reply", "diff", "a.md"}, {"resume", "-stop-after", "1.4-repair-recheck", "dir"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runCommand(context.Background(), args, &stdout, &stderr, nil); code != 2 {
			t.Errorf("runCommand(%q): exit %d, want 2", args, code)
		}
	}
}

// -quote-once=false briefs step 1.1 by the stage 1 rules as trialled before
// quote-once, and writes its notes as the reply gives them, footnotes and
// all. Step 1.4 is briefed by the traced edit-shaped brief, or with
// -repair-reply whole by the whole-notes brief, at the model -repair-model
// names, and the way on names every flag, since resume reads none from the
// run folder.
func TestRunCommand_QuoteOnceOff(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "Tiny.md")
	if err := os.WriteFile(sourcePath, []byte("---\nsource: \"[[@Tiny - An Author]]\"\n---\n\n## Alpha\n\nAlpha is first. Beta is second.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := source.Read(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	seg := src.Segments[0]
	name := "0 " + seg.Title + ".md"
	traces := "[^1]: [[Tiny#Alpha]] \"Alpha is first.\"\n[^2]: [[Tiny#Alpha]] \"Beta is second.\"\n"
	extracted := note.Parse(name, "# 0 "+seg.Title+"\n\nAlpha is first.[^1] Beta is the second.[^2]\n\n"+traces)
	repaired := note.Parse(name, "# 0 "+seg.Title+"\n\nAlpha is first.[^1] Beta is second.[^2]\n\n"+traces)
	// Step 1.3a fails Beta's statement, which step 1.2 finds untraced too,
	// so step 1.4 calls the segment.
	support := "# 0 " + seg.Title + "\n\n- [fail] \"Beta is the second.\": the source says \"second\", not \"the second\".\n\npass 1 / fail 1\n"
	readsAlone := "# 0 " + seg.Title + "\n\npass 2 / dangling 0 / outside-frame 0\n"
	mech := mechcheck.Check(src, []*note.Note{extracted}).String()
	for _, tc := range []struct {
		name, reply string
		flags       []string
		way, brief  string
	}{
		{
			"edits",
			"=== EDIT: " + name + " ===\n--- REPLACE ---\nBeta is the second.[^2]\n--- WITH ---\nBeta is second.[^1]\n[^1]: [[Tiny#Alpha]] \"Beta is second.\"\n=== REPORT ===\nBeta's statement now reads as the source does.\n",
			nil,
			"-repair-model sonnet -quote-once=false",
			"repair-edits.md",
		},
		{
			"whole",
			"=== FILE: " + name + " ===\n" + repaired.Render() + "=== REPORT ===\nBeta's statement now reads as the source does.\n",
			[]string{"-repair-reply", "whole"},
			"-repair-model sonnet -repair-reply whole -quote-once=false",
			"repair.md",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := servePrompts(t,
				[]string{
					extract.Prompt(src, seg),
					supportcheck.Prompt([]*note.Note{extracted}, seg),
					readsalone.Prompt([]*note.Note{extracted}, seg),
					repair.Prompt([]*note.Note{extracted}, seg, mech, support, readsAlone),
				},
				[]string{
					"=== FILE: " + name + " ===\n" + extracted.Render() + "=== REPORT ===\nNone.\n",
					support,
					readsAlone,
					tc.reply,
				})

			runDir := filepath.Join(t.TempDir(), "run")
			var stdout, stderr bytes.Buffer
			args := append([]string{"run", "-run-dir", runDir, "-quote-once=false", "-repair-model", "sonnet", "-stop-after", run.RepairDir}, tc.flags...)
			if code := runCommand(context.Background(), append(args, sourcePath), &stdout, &stderr, p); code != 0 {
				t.Fatalf("exit %d, want 0: %s", code, stderr.String())
			}
			if want := "Run s2n resume " + tc.way + " " + runDir + " to go on."; !strings.Contains(stderr.String(), want) {
				t.Errorf("stderr %q does not say %q", stderr.String(), want)
			}
			calls := p.Calls()
			if len(calls) != 4 {
				t.Fatalf("%d calls, want one per step to step 1.4 (4)", len(calls))
			}
			stage1 := testfixture.RepoFile(t, "internal/briefs/stage-1.md")
			if !strings.HasPrefix(calls[0].System, stage1+"\n\n=== FRAME: ") || calls[0].Model != extract.DefaultModel {
				t.Errorf("step 1.1's call: model %q, or not briefed by the traced stage 1 rules", calls[0].Model)
			}
			if !strings.HasPrefix(calls[3].System, testfixture.RepoFile(t, "internal/briefs/"+tc.brief)+"\n\n"+stage1+"\n\n=== FRAME: ") || calls[3].Model != "sonnet" {
				t.Errorf("step 1.4's call: model %q, or not briefed by the %s brief and the traced stage 1 rules", calls[3].Model, tc.name)
			}
			if got := testfixture.Files(t, filepath.Join(runDir, filepath.FromSlash(run.ExtractNotes)))[name]; got != extracted.Render() {
				t.Errorf("step 1.1 wrote %q, want the note as the reply gives it, footnotes and all", got)
			}
			if got := testfixture.Files(t, filepath.Join(runDir, filepath.FromSlash(run.RepairNotes)))[name]; got != repaired.Render() {
				t.Errorf("step 1.4 wrote %q, want %q", got, repaired.Render())
			}
		})
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestRun_GoesUnattendedToStageOut runs `s2n run` once on a one-segment
// source, with invented replies for every step, from step 1.1 to stage out
// (ADR 0009). Nothing stops it: step 1.5's one proposed loss is added with
// no review, no segment falls back, and the run report goes into the
// staging folder with the notes.
func TestRun_GoesUnattendedToStageOut(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "Tiny.md")
	if err := os.WriteFile(sourcePath, []byte("---\nsource: \"[[@Tiny - An Author]]\"\n---\n\n## Alpha\n\nAlpha is first. Beta is second.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := source.Read(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(src.Segments) != 1 {
		t.Fatalf("%d segments, want 1", len(src.Segments))
	}
	seg := src.Segments[0]
	name := "0 " + seg.Title + ".md"
	alpha := "[^1]: [[Tiny#Alpha]] \"Alpha is first.\"\n"
	extracted := note.Parse(name, "# 0 "+seg.Title+"\n\nAlpha is first.[^1]\n\n"+alpha)
	final := note.Parse(name, "# 0 "+seg.Title+"\n\nAlpha is first.[^1] Beta is second.[^2]\n\n"+alpha+"[^2]: [[Tiny#Alpha]] \"Beta is second.\"\n")

	// Step 1.5 gets the uncovered list the extracted note leaves, and both
	// runs propose Beta.
	cov := mechcheck.Check(src, []*note.Note{extracted}).Coverage
	if len(cov.Sections) != 1 {
		t.Fatalf("%d uncovered sections, want 1", len(cov.Sections))
	}
	proposal := "1. **Beta is second.**\n   - Quote: \"Beta is second.\"\n   - Why: it's the argument\n"
	union := lossproposer.Union(cov.Sections[0], []lossproposer.Reply{lossproposer.Parse(proposal), lossproposer.Parse(proposal)})
	kept := []keptloss.Kept{{Segment: 0, Number: 1, Loss: union.Losses[0]}}

	atomic := note.Parse("Alpha and Beta.md", "# Alpha and Beta\n\nAlpha is first.[^1] Beta is second.[^2]\n\n"+alpha+"[^2]: [[Tiny#Alpha]] \"Beta is second.\"\n")
	hub := note.Parse(split.Hub(seg), "# "+seg.Title+"\n\n1. [[Alpha and Beta]]\n")
	var stripped []*note.Note
	for _, n := range []*note.Note{atomic, hub} {
		s, err := striptraces.Strip(n, src.CitekeyLink, n == hub)
		if err != nil {
			t.Fatal(err)
		}
		stripped = append(stripped, s)
	}
	splitPrompt, _ := split.Prompt(src, []*note.Note{final}, seg)
	files := func(notes []*note.Note) string {
		var b strings.Builder
		for _, n := range notes {
			b.WriteString("=== FILE: " + n.Name + " ===\n" + n.Render())
		}
		return b.String() + "=== MANIFEST ===\n{}\n"
	}

	p := servePrompts(t,
		[]string{
			extract.Prompt(src, seg),
			supportcheck.Prompt([]*note.Note{extracted}, seg),
			readsalone.Prompt([]*note.Note{extracted}, seg),
			lossproposer.Prompt(seg, cov.Sections[0].List()), lossproposer.Prompt(seg, cov.Sections[0].List()),
			keptloss.Prompt([]*note.Note{extracted}, seg, kept),
			supportcheck.Prompt([]*note.Note{final}, seg),
			splitPrompt,
			format.Prompt(split.SegmentFolder{Name: split.Folder(seg), Notes: stripped}),
		},
		[]string{
			"=== FILE: " + name + " ===\n" + extracted.Render() + "=== REPORT ===\nNone.\n",
			"# 0 " + seg.Title + "\n\npass 1\n",
			"# 0 " + seg.Title + "\n\npass 1 / dangling 0 / outside-frame 0\n",
			proposal, proposal,
			"=== EDIT: " + name + " ===\n--- AFTER ---\nAlpha is first.[^1]\n--- INSERT ---\nBeta is second.[^1]\n[^1]: [[Tiny#Alpha]] \"Beta is second.\"\n=== REPORT ===\n- Loss 0.1: added.\n",
			"# 0 " + seg.Title + "\n\npass 2\n",
			files([]*note.Note{atomic, hub}),
			files(stripped),
		})

	vault := filepath.Join(dir, "vault")
	if err := os.MkdirAll(filepath.Join(vault, stageout.SlipBoxDir), 0o755); err != nil {
		t.Fatal(err)
	}
	runDir := filepath.Join(dir, "run")
	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"run", "-run-dir", runDir, "-quote-once=false", "-vault", vault, sourcePath}, &stdout, &stderr, p); code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr.String())
	}
	if n := len(p.Calls()); n != 9 {
		t.Errorf("%d calls, want one per prompt (9)", n)
	}
	if _, err := os.Stat(runDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the run folder is not deleted (stat: %v)", err)
	}
	staging := stageout.DefaultStaging(vault, src.Name)
	got := testfixture.Files(t, staging)
	if !reflect.DeepEqual(keys(got), []string{"Alpha and Beta.md", "Alpha.md", stageout.ClashListFile, stageout.RunReportFile}) {
		t.Fatalf("staged %q", keys(got))
	}
	if got["Alpha and Beta.md"] != stripped[0].Render() {
		t.Errorf("staged note is not the stripped one:\n%s", got["Alpha and Beta.md"])
	}
	report := got[stageout.RunReportFile]
	for _, w := range []string{"## Losses added\n\n- Loss 0.1: Beta is second.\n", "## Statements that still fail\n\nNone.\n", "## Dropped\n\nNone.\n", "## Fallbacks\n\nNone.\n", "- Asides: 0\n"} {
		if !strings.Contains(report, w) {
			t.Errorf("the run report has no %q:\n%s", w, report)
		}
	}
	if want := "and the run report " + filepath.Join(staging, stageout.RunReportFile); !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr %q does not say %q", stderr.String(), want)
	}
}

// s2n run given a source that isn't valid UTF-8 stops before it makes a run
// folder, and says which file to re-save.
func TestRunCommand_SourceNotUTF8(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "Bad.md")
	if err := os.WriteFile(sourcePath, []byte("---\nsource: \"[[@X]]\"\n---\n## A\ncaf\xe9 bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	runDir := filepath.Join(t.TempDir(), "run")
	for _, args := range [][]string{{"run", "-run-dir", runDir, sourcePath}, {"run", sourcePath}} {
		var stdout, stderr bytes.Buffer
		if code := runCommand(context.Background(), args, &stdout, &stderr, nil); code != 1 {
			t.Errorf("runCommand(%q): exit %d, want 1: %s", args, code, stderr.String())
		}
		want := "s2n run: the source " + sourcePath + ": not valid UTF-8, first invalid byte on line 5 (byte offset 33). Re-save it as UTF-8, then run s2n run again.\n"
		if got := stderr.String(); got != want {
			t.Errorf("runCommand(%q): stderr = %q, want %q", args, got, want)
		}
		if stdout.Len() != 0 {
			t.Errorf("runCommand(%q): stdout = %q, want nothing", args, stdout.String())
		}
	}
	if _, err := os.Stat(runDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("run folder %s: stat err = %v, want it not made", runDir, err)
	}
	if entries, err := os.ReadDir(state); err != nil || len(entries) != 0 {
		t.Errorf("$XDG_STATE_HOME holds %v (err %v), want nothing", entries, err)
	}
}

// s2n resume reads the run folder's own copy of the source, which passed the
// check when s2n run saved it, so a copy edited since then gets the plain
// error, not s2n run's message to run it again.
func TestResumeCommand_SourceNotUTF8(t *testing.T) {
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	if err := folder.SaveSource("Bad", []byte("---\nsource: \"[[@X]]\"\n---\n## A\ncaf\xe9 bar\n")); err != nil {
		t.Fatal(err)
	}
	sourcePath, err := folder.SourcePath()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"resume", folder.Dir}, &stdout, &stderr, nil); code != 1 {
		t.Errorf("exit %d, want 1: %s", code, stderr.String())
	}
	want := "s2n resume: " + sourcePath + ": not valid UTF-8, first invalid byte on line 5 (byte offset 33)\n"
	if got := stderr.String(); got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

// TestRun_FallsBack runs `s2n run` from step 1.1 to stage out on a source of
// two segments, with invented replies, in which a segment fails a stage 2 or
// 3 step (ADR 0009). Nothing stops the run. A segment that fails step 2.1
// or its gates is staged as its final stage 1 notes, stripped and with a
// source footnote, under a generated hub. One that fails step 3.1 or its gate
// is staged as its stripped notes, unformatted. Every other segment is
// staged as the run would stage it if nothing failed, byte for byte, and the
// staged run report, written after step 3.1's gate, names each fallback.
func TestRun_FallsBack(t *testing.T) {
	for _, c := range []struct {
		name string
		// split gives each segment's step 2.1 reply from its good one, and
		// fails are the segments that fail stage 2.
		split func(seg int, good []*note.Note) string
		fails []int
		// format gives each segment's step 3.1 reply from its stripped
		// notes and the formatting of them that passes, and formatFails
		// are the segments that fail stage 3.
		format      func(seg int, stripped, formatted []*note.Note) string
		formatFails []int
		// report are the staged report's fallback lines.
		report string
	}{
		{
			name: "split check",
			split: func(seg int, good []*note.Note) string {
				if seg == 0 {
					good[0] = note.Parse(good[0].Name, "# Alpha Note\n\nAlpha is first.\n")
				}
				return fileBlocks(good)
			},
			fails:  []int{0},
			report: "- 2.1-gates, segment 0 (Alpha): fell back to its stage 1 notes: the split check failed it; its report is split-check.txt\n",
		},
		{
			name: "name given twice",
			split: func(seg int, good []*note.Note) string {
				if seg == 0 {
					return "=== FILE: Alpha Note ===\n" + good[0].Render() + fileBlocks(good)
				}
				return fileBlocks(good)
			},
			fails:  []int{0},
			report: "- 2.1-gates, segment 0 (Alpha): fell back to its stage 1 notes: step 2.1's reply wrote \"Alpha Note.md\" twice; its notes are in 2.1-gates/stage-2/0 Alpha\n",
		},
		{
			name: "no hub",
			split: func(seg int, good []*note.Note) string {
				if seg == 0 {
					return fileBlocks(good[:1])
				}
				return fileBlocks(good)
			},
			fails:  []int{0},
			report: "- 2.1-gates, segment 0 (Alpha): fell back to its stage 1 notes: step 2.1's reply wrote no hub note \"Alpha.md\"; its notes are in 2.1-gates/stage-2/0 Alpha\n",
		},
		{
			name: "no atomic note",
			split: func(seg int, good []*note.Note) string {
				if seg == 0 {
					return fileBlocks(good[1:])
				}
				return fileBlocks(good)
			},
			fails:  []int{0},
			report: "- 2.1-gates, segment 0 (Alpha): fell back to its stage 1 notes: step 2.1's reply wrote no atomic note; its notes are in 2.1-gates/stage-2/0 Alpha\n",
		},
		{
			name: "format check",
			format: func(seg int, stripped, formatted []*note.Note) string {
				if seg == 1 {
					formatted[0] = note.Parse(formatted[0].Name, strings.Replace(formatted[0].Render(), "second", "third", 1))
				}
				return fileBlocks(formatted)
			},
			formatFails: []int{1},
			report:      "- 3.1-gate, segment 1 (Beta): kept its notes unformatted: the format check failed it; its report is format-check.txt\n",
		},
		{
			name: "note left out",
			format: func(seg int, stripped, formatted []*note.Note) string {
				if seg == 1 {
					return fileBlocks(formatted[:1])
				}
				return fileBlocks(formatted)
			},
			formatFails: []int{1},
			report:      "- 3.1-gate, segment 1 (Beta): kept its notes unformatted: step 3.1's reply left out [\"Beta.md\"], but it must give back every note of its segment, as named; its notes are in 3.1-gate/stage-3/1 Beta\n",
		},
		{
			name: "both stages",
			split: func(seg int, good []*note.Note) string {
				if seg == 0 {
					good[0] = note.Parse(good[0].Name, "# Alpha Note\n\nAlpha is first.\n")
				}
				return fileBlocks(good)
			},
			fails: []int{0},
			format: func(seg int, stripped, formatted []*note.Note) string {
				if seg == 0 {
					formatted[0] = note.Parse(formatted[0].Name, strings.Replace(formatted[0].Render(), "first", "last", 1))
				}
				return fileBlocks(formatted)
			},
			formatFails: []int{0},
			report: "- 2.1-gates, segment 0 (Alpha): fell back to its stage 1 notes: the split check failed it; its report is split-check.txt\n" +
				"- 3.1-gate, segment 0 (Alpha): kept its notes unformatted: the format check failed it; its report is format-check.txt\n",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.split == nil {
				c.split = func(_ int, good []*note.Note) string { return fileBlocks(good) }
			}
			if c.format == nil {
				c.format = func(_ int, _, formatted []*note.Note) string { return fileBlocks(formatted) }
			}
			dir := t.TempDir()
			sourcePath := filepath.Join(dir, "Tiny.md")
			if err := os.WriteFile(sourcePath, []byte("---\nsource: \"[[@Tiny - An Author]]\"\n---\n\n## Alpha\n\nAlpha is first.\n\n## Beta\n\nBeta is second.\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			src, err := source.Read(sourcePath)
			if err != nil {
				t.Fatal(err)
			}
			if len(src.Segments) != 2 {
				t.Fatalf("%d segments, want 2", len(src.Segments))
			}
			sentences := []string{"Alpha is first.", "Beta is second."}
			trace := func(seg int) string {
				return "[^1]: [[Tiny#" + src.Segments[seg].Title + "]] \"" + sentences[seg] + "\"\n"
			}
			var final []*note.Note
			for i, seg := range src.Segments {
				name := strconv.Itoa(i) + " " + seg.Title
				final = append(final, note.Parse(name+".md", "# "+name+"\n\n"+sentences[i]+"[^1]\n\n"+trace(i)))
			}

			var prompts, replies []string
			add := func(prompt, reply string) {
				prompts = append(prompts, prompt)
				replies = append(replies, reply)
			}
			// Stage 1 covers every sentence and flags nothing, so steps 1.4
			// and 1.5 make no call, and the final notes are step 1.1's.
			for i, seg := range src.Segments {
				add(extract.Prompt(src, seg), "=== FILE: "+final[i].Name+" ===\n"+final[i].Render()+"=== REPORT ===\nNone.\n")
				h1 := "# " + strings.TrimSuffix(final[i].Name, ".md") + "\n\n"
				add(supportcheck.Prompt(final, seg), h1+"pass 1\n")
				add(readsalone.Prompt(final, seg), h1+"pass 1 / dangling 0 / outside-frame 0\n")
			}

			// The staged notes: each segment's split or its fallback,
			// stripped, then formatted unless it fails stage 3. Formatting
			// that passes bolds the sentence's last word.
			want := map[string]string{}
			for i, seg := range src.Segments {
				good := []*note.Note{
					note.Parse(seg.Title+" Note.md", "# "+seg.Title+" Note\n\n"+sentences[i]+"[^1]\n\n"+trace(i)),
					note.Parse(split.Hub(seg), "# "+seg.Title+"\n\n1. [["+seg.Title+" Note]]\n"),
				}
				splitPrompt, _ := split.Prompt(src, final, seg)
				add(splitPrompt, c.split(i, slices.Clone(good))+"=== MANIFEST ===\n{}\n")
				onward := good
				if slices.Contains(c.fails, i) {
					onward = split.Fallback(seg, final).Notes
				}
				var stripped, formatted []*note.Note
				for _, n := range onward {
					s, err := striptraces.Strip(n, src.CitekeyLink, n.Name == split.Hub(seg))
					if err != nil {
						t.Fatal(err)
					}
					stripped = append(stripped, s)
					last := strings.Fields(sentences[i])[2]
					formatted = append(formatted, note.Parse(s.Name, strings.Replace(s.Render(), " "+last, " **"+strings.TrimSuffix(last, ".")+"**.", 1)))
				}
				add(format.Prompt(split.SegmentFolder{Name: split.Folder(seg), Notes: stripped}), c.format(i, stripped, slices.Clone(formatted))+"=== MANIFEST ===\n{}\n")
				staged := formatted
				if slices.Contains(c.formatFails, i) {
					staged = stripped
				}
				for _, n := range staged {
					want[n.Name] = n.Render()
				}
			}
			p := servePrompts(t, prompts, replies)

			vault := filepath.Join(dir, "vault")
			if err := os.MkdirAll(filepath.Join(vault, stageout.SlipBoxDir), 0o755); err != nil {
				t.Fatal(err)
			}
			runDir := filepath.Join(dir, "run")
			var stdout, stderr bytes.Buffer
			if code := runCommand(context.Background(), []string{"run", "-run-dir", runDir, "-quote-once=false", "-vault", vault, sourcePath}, &stdout, &stderr, p); code != 0 {
				t.Fatalf("exit %d, want 0: %s", code, stderr.String())
			}
			if n := len(p.Calls()); n != len(prompts) {
				t.Errorf("%d calls, want one per prompt (%d), and no second call for a segment that fell back", n, len(prompts))
			}
			got := testfixture.Files(t, stageout.DefaultStaging(vault, src.Name))
			report := got[stageout.RunReportFile]
			delete(got, stageout.RunReportFile)
			delete(got, stageout.ClashListFile)
			if !reflect.DeepEqual(keys(got), keys(want)) {
				t.Fatalf("staged %q, want %q", keys(got), keys(want))
			}
			for name, w := range want {
				if got[name] != w {
					t.Errorf("staged %s:\n%s\nwant:\n%s", name, got[name], w)
				}
			}
			for _, i := range c.fails {
				hub := got[split.Hub(src.Segments[i])]
				if want := "1. [[" + strings.TrimSuffix(final[i].Name, ".md") + "]]\n"; !strings.Contains(hub, want) || strings.Contains(hub, "[^") {
					t.Errorf("the fallback's hub does not list its stage 1 note, or has a footnote:\n%s", hub)
				}
				if n := got[final[i].Name]; !strings.HasSuffix(n, "[^1]: "+src.CitekeyLink+"\n") || strings.Count(n, "[^") != 2 {
					t.Errorf("the fallback's stage 1 note is not stripped with a source footnote:\n%s", n)
				}
			}
			if want := "## Fallbacks\n\n" + c.report + "\n## "; !strings.Contains(report, want) {
				t.Errorf("the staged run report has no %q:\n%s", want, report)
			}
		})
	}
}

// fileBlocks is notes as a reply's `=== FILE: … ===` blocks.
func fileBlocks(notes []*note.Note) string {
	var b strings.Builder
	for _, n := range notes {
		b.WriteString("=== FILE: " + n.Name + " ===\n" + n.Render())
	}
	return b.String()
}

// The run report is written after step 3.1's gate, so it covers stages 2
// and 3, and before step 3.2, which stages it.
func TestSteps_RunReportAfterTheFormatGate(t *testing.T) {
	dirs := stepDirs()
	at := func(dir string) int { return slices.Index(dirs, dir) }
	if !(at(run.FormatGateDir) >= 0 && at(run.RunReportDir) == at(run.FormatGateDir)+1 && at(run.StageOutDir) == at(run.RunReportDir)+1) {
		t.Errorf("steps %q, want %s straight after %s, then %s", dirs, run.RunReportDir, run.FormatGateDir, run.StageOutDir)
	}
}
