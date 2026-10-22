package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/briefs"
	"github.com/corygyarmathy/slipbox/internal/extract"
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

// recordedStep is a model step of the Pragmatism run, as a replay serves it:
// the user prompts the run sends, in its order, each with the reply the run
// recorded to it.
type recordedStep struct {
	// name is the step as the design numbers it, and dirs are where its
	// calls' requests are recorded, from the run folder.
	name string
	dirs []string
	// model and briefs are what the run's calls asked for at the defaults,
	// and shared what its calls share in the system prompt after the frame,
	// or "".
	model   string
	briefs  []string
	shared  string
	prompts []string
	replies []string
}

// The Pragmatism run's model steps, by their index in pragmatismSteps.
const (
	extractStep = iota
	supportStep
	readsAloneStep
	repairStep
	supportRecheckStep
	readsAloneRecheckStep
	proposerStep
	keptLossStep
	keptLossRecheckStep
	splitStep
	formatStep
)

// pragmatismSteps gives the Pragmatism run's model steps, each with the
// user prompts the run sends for src and the run's recorded reply to each.
// The prompts are built from the notes and lists the recorded run folder
// holds at each step, so a step that sends something else finds no reply.
// Step 1.4 calls the six segments whose flags count, and re-checks the four
// its replies changed. Step 1.5 calls every segment, which each have an
// uncovered list, twice. The kept-loss repair calls segment 0, the one
// segment with a loss, and re-checks it.
func pragmatismSteps(t *testing.T, src *source.Source) []recordedStep {
	t.Helper()
	recorded := testfixture.PragmatismRun(t)
	at := func(rel ...string) string { return filepath.Join(append([]string{recorded}, rel...)...) }
	notes := func(dir string) []*note.Note {
		n, err := note.ReadDir(at(filepath.FromSlash(dir)))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	extracted, repaired, kept, final := notes(run.ExtractNotes), notes(run.RepairNotes), notes(run.KeptLossNotes), notes(run.FinalNotes)
	// raw is a step's recorded replies, by segment number.
	raw := func(dir ...string) map[int]string {
		out := map[int]string{}
		for name, text := range testfixture.Files(t, at(append(dir, run.RawDir)...)) {
			n, err := strconv.Atoi(strings.TrimSuffix(name, ".txt"))
			if err != nil {
				t.Fatal(err)
			}
			out[n] = text
		}
		return out
	}
	// add adds a call for each segment with a recorded reply in dir, in
	// segment order, its prompt given by prompt.
	add := func(s *recordedStep, dir []string, prompt func(seg source.Segment) string) {
		replies := raw(dir...)
		for _, seg := range src.Segments {
			if r, ok := replies[seg.Number]; ok {
				s.prompts = append(s.prompts, prompt(seg))
				s.replies = append(s.replies, r)
			}
		}
	}
	step := func(name, model string, briefs []string, shared string, dir string, prompt func(seg source.Segment) string) recordedStep {
		s := recordedStep{name: name, dirs: []string{dir}, model: model, briefs: briefs, shared: shared}
		add(&s, strings.Split(dir, "/"), prompt)
		return s
	}

	support := raw(run.SupportCheckDir)
	readsAlone := raw(run.ReadsAloneDir)
	mech := testfixture.Files(t, at(run.MechanicalCheckDir))[run.ReportFile]
	uncovered := testfixture.Files(t, at(run.MechanicalRecheckDir, run.UncoveredDir))
	var keptLosses []keptloss.Kept
	if err := json.Unmarshal([]byte(testfixture.Files(t, at(run.KeptLossDir))[run.KeptLossesFile]), &keptLosses); err != nil {
		t.Fatal(err)
	}
	stripped, err := split.ReadFolders(at(filepath.FromSlash(run.StrippedNotes)))
	if err != nil {
		t.Fatal(err)
	}

	proposer := recordedStep{name: "1.5", model: "sonnet", briefs: []string{"proposer.md"}, shared: lossproposer.Shared(repaired)}
	for k := 1; k <= lossproposer.Runs; k++ {
		dir := run.LossProposerDir + "/" + run.RunPrefix + strconv.Itoa(k)
		proposer.dirs = append(proposer.dirs, dir)
		add(&proposer, strings.Split(dir, "/"), func(seg source.Segment) string {
			return lossproposer.Prompt(seg, uncovered[strconv.Itoa(seg.Number)+".txt"])
		})
	}
	return []recordedStep{
		extractStep: step("1.1", "opus", []string{"stage-1-quote-once.md"}, "", run.ExtractDir, func(seg source.Segment) string {
			return extract.Prompt(src, seg)
		}),
		supportStep: step("1.3a", "sonnet", []string{"checker-support.md"}, supportcheck.Shared(extracted), run.SupportCheckDir, func(seg source.Segment) string {
			return supportcheck.Prompt(extracted, seg)
		}),
		readsAloneStep: step("1.3b", "sonnet", []string{"checker-reads-alone.md"}, "", run.ReadsAloneDir, func(seg source.Segment) string {
			return readsalone.Prompt(extracted, seg)
		}),
		repairStep: step("1.4", "opus", []string{"repair-edits-quote-once.md", "stage-1-quote-once.md"}, "", run.RepairDir, func(seg source.Segment) string {
			return repair.Prompt(extracted, seg, mech, support[seg.Number], readsAlone[seg.Number])
		}),
		supportRecheckStep: step("1.4 re-check 1.3a", "sonnet", []string{"checker-support.md"}, supportcheck.Shared(repaired), run.SupportRecheckDir, func(seg source.Segment) string {
			return supportcheck.Prompt(repaired, seg)
		}),
		readsAloneRecheckStep: step("1.4 re-check 1.3b", "sonnet", []string{"checker-reads-alone.md"}, "", run.ReadsAloneRecheckDir, func(seg source.Segment) string {
			return readsalone.Prompt(repaired, seg)
		}),
		proposerStep: proposer,
		keptLossStep: step("kept-loss repair", "opus", []string{"kept-loss-edits-quote-once.md", "stage-1-quote-once.md"}, "", run.KeptLossDir, func(seg source.Segment) string {
			var mine []keptloss.Kept
			for _, k := range keptLosses {
				if k.Segment == seg.Number {
					mine = append(mine, k)
				}
			}
			return keptloss.Prompt(repaired, seg, mine)
		}),
		keptLossRecheckStep: step("kept-loss re-check 1.3a", "sonnet", []string{"checker-support.md"}, supportcheck.Shared(kept), run.KeptLossDir+"/"+run.KeptLossSupportDir, func(seg source.Segment) string {
			return supportcheck.Prompt(kept, seg)
		}),
		splitStep: step("2.1", "sonnet", []string{"stage-2.md"}, "", run.SplitDir, func(seg source.Segment) string {
			prompt, _ := split.Prompt(src, final, seg)
			return prompt
		}),
		formatStep: step("3.1", "haiku", []string{"stage-3.md"}, "", run.FormatDir, func(seg source.Segment) string {
			for _, f := range stripped {
				if f.Name == split.Folder(seg) {
					return format.Prompt(f)
				}
			}
			t.Fatalf("no stripped notes for segment %d", seg.Number)
			return ""
		}),
	}
}

// replies gives steps' prompts and replies, in order, for a replay.
func replies(steps ...recordedStep) (prompts, replies []string) {
	for _, s := range steps {
		prompts = append(prompts, s.prompts...)
		replies = append(replies, s.replies...)
	}
	return prompts, replies
}

// pragmatismReplay is a replay of the Pragmatism run's steps up to and
// including the step at index last.
func pragmatismReplay(t *testing.T, src *source.Source, last int) (*provider.Replay, []recordedStep) {
	t.Helper()
	steps := pragmatismSteps(t, src)[:last+1]
	prompts, replies := replies(steps...)
	return testfixture.Replay(t, prompts, replies), steps
}

// TestRun_ReplaysThePragmatismRun runs `s2n run` on the Pragmatism source
// with the run's recorded replies and the defaults, as the run was made:
// stopped after the run report, then resumed to stage out, given an empty
// vault. Each step makes the requests the run recorded, so it writes what
// the run wrote, and the staging folder is the run's, byte for byte.
func TestRun_ReplaysThePragmatismRun(t *testing.T) {
	sourcePath := testfixture.SourcePath(t, testfixture.Pragmatism)
	src := testfixture.PragmatismSource(t)
	recorded := testfixture.PragmatismRun(t)
	p, steps := pragmatismReplay(t, src, formatStep)

	runDir := filepath.Join(t.TempDir(), "run")
	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"run", "-run-dir", runDir, "-stop-after", run.RunReportDir, sourcePath}, &stdout, &stderr, p); code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != runDir {
		t.Errorf("stdout = %q, want the run folder %q", got, runDir)
	}
	folder := &run.Folder{Dir: runDir}
	if p, err := folder.SourcePath(); err != nil {
		t.Errorf("run folder has no copy of the source: %v", err)
	} else if b, err := os.ReadFile(p); err != nil || string(b) != src.Frame {
		t.Errorf("run folder's source is not the frame read (err %v)", err)
	} else if filepath.Base(p) != testfixture.Pragmatism+".md" {
		t.Errorf("run folder's source is named %q", filepath.Base(p))
	}

	// A step's system prompt is its briefs, the frame and then the notes its
	// calls share, if it has any.
	frame := testfixture.PragmatismFrame(t)
	system := func(s recordedStep) string {
		var text []string
		for _, b := range s.briefs {
			text = append(text, testfixture.RepoFile(t, "internal/briefs/"+b))
		}
		system := strings.Join(text, "\n\n") + "\n\n=== FRAME: " + testfixture.Pragmatism + ".md ===\n" + frame + "\n=== END FRAME ===\n"
		if s.shared != "" {
			system += "\n" + s.shared
		}
		return system
	}
	calls := p.Calls()
	prompts, _ := replies(steps...)
	if len(calls) != len(prompts) || len(calls) != 72 {
		t.Fatalf("%d calls, want the run's %d, one per recorded reply (72)", len(calls), len(prompts))
	}
	at := 0
	for _, s := range steps {
		for _, c := range calls[at : at+len(s.prompts)] {
			if c.System != system(s) {
				t.Errorf("step %s call for %.80q: system prompt is not %s, the frame and the step's shared notes", s.name, c.User, s.briefs)
			}
			if c.Model != s.model || c.Effort != "medium" {
				t.Errorf("step %s call for %.80q: model %q at %q, want %s at medium", s.name, c.User, c.Model, c.Effort, s.model)
			}
		}
		if calls[at].User != s.prompts[0] {
			t.Errorf("first step %s call was %.80q, want its first segment's", s.name, calls[at].User)
		}
		at += len(s.prompts)
		for _, dir := range s.dirs {
			testfixture.SameRequests(t, filepath.Join(runDir, filepath.FromSlash(dir)), filepath.Join(recorded, filepath.FromSlash(dir)), "")
		}
	}

	// Every file the run wrote is the recorded run's, byte for byte, but the
	// usage logs, which time and cost each call, and the run report, which
	// links into the run folder.
	got, want := testfixture.Files(t, runDir), testfixture.Files(t, recorded)
	if !reflect.DeepEqual(keys(got), keys(want)) {
		t.Fatalf("the run wrote %d files, want the recorded run's %d", len(got), len(want))
	}
	for name, w := range want {
		switch {
		case filepath.Base(name) == run.UsageFile:
			if g, w := strings.Count(got[name], "\n"), strings.Count(w, "\n"); g != w {
				t.Errorf("%s logs %d calls, want the run's %d", name, g, w)
			}
		case name == run.RunReportDir+"/"+run.RunReportFile:
			_, rest, _ := strings.Cut(w, "Links are to the run folder, `")
			recordedDir, _, _ := strings.Cut(rest, "`")
			if g := strings.ReplaceAll(got[name], runDir, recordedDir); recordedDir == "" || g != w {
				t.Errorf("%s is not the run's, its run folder aside", name)
			}
		case got[name] != w:
			t.Errorf("%s is not the recorded run's", name)
		}
	}

	vault := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(filepath.Join(vault, stageout.SlipBoxDir), 0o755); err != nil {
		t.Fatal(err)
	}
	none := testfixture.Replay(t, nil, nil)
	stderr.Reset()
	if code := runCommand(context.Background(), []string{"resume", "-vault", vault, runDir}, &stdout, &stderr, none); code != 0 || len(none.Calls()) != 0 {
		t.Fatalf("resume: exit %d with %d calls, want 0 with none: %s", code, len(none.Calls()), stderr.String())
	}
	if _, err := os.Stat(runDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the run folder is not deleted (stat: %v)", err)
	}
	if got, want := testfixture.Files(t, stageout.DefaultStaging(vault, src.Name)), testfixture.Files(t, testfixture.PragmatismStaged(t)); !reflect.DeepEqual(got, want) {
		t.Errorf("staged %d files, not the run's %d byte for byte", len(got), len(want))
	}
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

// -stage-1-model replaces stage 1's tier for steps 1.1 and 1.4's calls.
func TestRunCommand_Stage1Model(t *testing.T) {
	sourcePath := testfixture.SourcePath(t, testfixture.Pragmatism)
	src := testfixture.PragmatismSource(t)
	p, _ := pragmatismReplay(t, src, proposerStep)

	runDir := filepath.Join(t.TempDir(), "run")
	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"run", "-run-dir", runDir, "-stop-after", run.LossProposerDir, "-stage-1-model", "sonnet", sourcePath}, &stdout, &stderr, p); code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr.String())
	}
	for _, c := range p.Calls() {
		if c.Model != "sonnet" {
			t.Errorf("call for %q: model %q, want sonnet", c.User, c.Model)
		}
	}
	usage := testfixture.Files(t, filepath.Join(runDir, run.ExtractDir))[run.UsageFile]
	if n := strings.Count(usage, "\n"); n != len(src.Segments) {
		t.Errorf("usage log has %d lines, want one per call (%d)", n, len(src.Segments))
	}
	if !strings.Contains(usage, `"step":"`+run.ExtractDir+`"`) || !strings.Contains(usage, `"model":"sonnet"`) {
		t.Errorf("usage log does not name the step and model: %q", usage)
	}
}

// -extract-effort sets step 1.1's effort level, and only step 1.1's: every
// other step's calls go at its own level, and every usage line records the
// level its call went at.
func TestRunCommand_ExtractEffort(t *testing.T) {
	sourcePath := testfixture.SourcePath(t, testfixture.Pragmatism)
	src := testfixture.PragmatismSource(t)
	p, _ := pragmatismReplay(t, src, proposerStep)

	runDir := filepath.Join(t.TempDir(), "run")
	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"run", "-run-dir", runDir, "-stop-after", run.LossProposerDir, "-extract-effort", "low", sourcePath}, &stdout, &stderr, p); code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr.String())
	}
	extractPrompts := map[string]bool{}
	for _, seg := range src.Segments {
		extractPrompts[extract.Prompt(src, seg)] = true
	}
	for _, c := range p.Calls() {
		want := "medium"
		if extractPrompts[c.User] {
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

// With no flags, step 1.1 writes quote-once (ADR 0006), and step 1.4's
// repair call asks for edits written quote-once, at stage 1's tier (ADR
// 0007), as the Pragmatism run's calls did.
func TestRunCommand_DefaultsToEditsQuoteOnce(t *testing.T) {
	sourcePath := testfixture.SourcePath(t, testfixture.Pragmatism)
	src := testfixture.PragmatismSource(t)
	p, steps := pragmatismReplay(t, src, repairStep)

	runDir := filepath.Join(t.TempDir(), "run")
	var stdout, stderr bytes.Buffer
	args := []string{"run", "-run-dir", runDir, "-stop-after", run.RepairDir, sourcePath}
	if code := runCommand(context.Background(), args, &stdout, &stderr, p); code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr.String())
	}
	brief := map[string]string{}
	for _, u := range steps[extractStep].prompts {
		brief[u] = briefs.Stage1QuoteOnce
	}
	for _, u := range steps[repairStep].prompts {
		brief[u] = briefs.RepairEditsQuoteOnce + "\n\n" + briefs.Stage1QuoteOnce
	}
	if len(p.Calls()) != 30 {
		t.Errorf("%d calls, want the run's 30 to the end of step 1.4", len(p.Calls()))
	}
	for _, c := range p.Calls() {
		want, ok := brief[c.User]
		if !ok {
			continue
		}
		if c.Model != extract.DefaultModel || !strings.HasPrefix(c.System, want) {
			t.Errorf("call for %.60q: model %q, or not briefed by the quote-once brief", c.User, c.Model)
		}
	}
	if want := "Run s2n resume " + runDir + " to go on."; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr %q does not say %q", stderr.String(), want)
	}
}

// -repair-model and -repair-reply set step 1.4's repair call apart from stage
// 1's tier, and -stop-after stops the run after step 1.4's re-check.
// Resuming without the flags goes on through step 1.5.
func TestRunCommand_RepairConfiguration(t *testing.T) {
	sourcePath := testfixture.SourcePath(t, testfixture.Pragmatism)
	src := testfixture.PragmatismSource(t)
	steps := pragmatismSteps(t, src)
	prompts, replies := replies(steps[:readsAloneRecheckStep+1]...)
	p := testfixture.Replay(t, prompts, replies)

	runDir := filepath.Join(t.TempDir(), "run")
	var stdout, stderr bytes.Buffer
	args := []string{"run", "-run-dir", runDir, "-repair-model", "sonnet", "-repair-reply", "edits", "-stop-after", run.ReadsAloneRecheckDir, sourcePath}
	if code := runCommand(context.Background(), args, &stdout, &stderr, p); code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "stopped after "+run.ReadsAloneRecheckDir) {
		t.Errorf("stderr %q does not say where the run stopped", stderr.String())
	}
	folder := &run.Folder{Dir: runDir}
	if !folder.Complete(run.ReadsAloneRecheckDir) || folder.Complete(run.StillFailsDir) {
		t.Fatalf("the run did not stop right after step 1.4's re-check")
	}
	// Step 1.1 keeps stage 1's tier, and the checkers their own.
	model := map[string]string{}
	for _, u := range steps[extractStep].prompts {
		model[u] = extract.DefaultModel
	}
	for _, u := range steps[repairStep].prompts {
		model[u] = "sonnet"
	}
	for _, c := range p.Calls() {
		want, ok := model[c.User]
		if ok && c.Model != want {
			t.Errorf("call for %.60q: model %q, want %q", c.User, c.Model, want)
		}
		if ok && want == "sonnet" && !strings.HasPrefix(c.System, briefs.RepairEditsQuoteOnce) {
			t.Errorf("repair call for %.60q is not briefed by the edit-shaped brief", c.User)
		}
	}
	if usage := testfixture.Files(t, filepath.Join(runDir, run.RepairDir))[run.UsageFile]; !strings.Contains(usage, `"model":"sonnet"`) || strings.Contains(usage, `"model":"opus"`) {
		t.Errorf("step 1.4's usage log does not record sonnet alone:\n%s", usage)
	}

	resumed := testfixture.Replay(t, steps[proposerStep].prompts, steps[proposerStep].replies)
	if code, errText := resumeTo(t, runDir, run.LossProposerDir, resumed); code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, errText)
	}
	if n, want := len(resumed.Calls()), len(steps[proposerStep].prompts); n != want {
		t.Errorf("resume made %d calls, want %d: step 1.5's", n, want)
	}
}

// -proposer-model sets the model of both of step 1.5's runs. Each 1.5 line
// in the usage log records it, and the way on names it. With no flag, step 1.5 asks for its tier, Sonnet.
func TestRunCommand_ProposerModel(t *testing.T) {
	sourcePath := testfixture.SourcePath(t, testfixture.Pragmatism)
	src := testfixture.PragmatismSource(t)
	for _, tc := range []struct {
		name, model string
		flags       []string
		way         string
	}{
		{"default", lossproposer.Model, nil, "s2n resume "},
		{"opus", "opus", []string{"-proposer-model", "opus"}, "s2n resume -proposer-model opus "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, steps := pragmatismReplay(t, src, proposerStep)
			runDir := filepath.Join(t.TempDir(), "run")
			var stdout, stderr bytes.Buffer
			args := append([]string{"run", "-run-dir", runDir, "-stop-after", run.LossProposerDir}, tc.flags...)
			if code := runCommand(context.Background(), append(args, sourcePath), &stdout, &stderr, p); code != 0 {
				t.Fatalf("exit %d, want 0: %s", code, stderr.String())
			}
			proposer := map[string]bool{}
			for _, u := range steps[proposerStep].prompts {
				proposer[u] = true
			}
			calls := p.Calls()
			if prompts, _ := replies(steps...); len(calls) != len(prompts) {
				t.Fatalf("%d calls, want %d", len(calls), len(prompts))
			}
			for _, c := range calls {
				if proposer[c.User] && c.Model != tc.model {
					t.Errorf("step 1.5 call for %.60q: model %q, want %q", c.User, c.Model, tc.model)
				}
			}
			usage := testfixture.Files(t, filepath.Join(runDir, run.LossProposerDir))[run.UsageFile]
			lines := strings.Split(strings.TrimSuffix(usage, "\n"), "\n")
			if want := len(steps[proposerStep].prompts); len(lines) != want {
				t.Fatalf("step 1.5's usage log has %d lines, want one per call (%d)", len(lines), want)
			}
			for _, l := range lines {
				if !strings.Contains(l, `"model":"`+tc.model+`"`) {
					t.Errorf("usage line does not record %s: %s", tc.model, l)
				}
			}
			if want := "Run " + tc.way + runDir + " to go on."; !strings.Contains(stderr.String(), want) {
				t.Errorf("stderr %q does not say %q", stderr.String(), want)
			}
		})
	}
}

// -quote-once=false briefs step 1.1 by the stage 1 rules as trialled before
// quote-once, and writes its notes as the reply gives them, footnotes and
// all. Step 1.4 is briefed by the traced edit-shaped brief, or with
// -repair-reply whole by the whole-notes brief, at the model -repair-model
// names, and the way on names every flag, since resume reads none from the
// run folder. The source and replies are invented: the Pragmatism run was
// made quote-once, with edits.
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
			briefs.RepairEdits,
		},
		{
			"whole",
			"=== FILE: " + name + " ===\n" + repaired.Render() + "=== REPORT ===\nBeta's statement now reads as the source does.\n",
			[]string{"-repair-reply", "whole"},
			"-repair-model sonnet -repair-reply whole -quote-once=false",
			briefs.Repair,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testfixture.Replay(t,
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
			if !strings.HasPrefix(calls[0].System, briefs.Stage1+"\n\n=== FRAME: ") || calls[0].Model != extract.DefaultModel {
				t.Errorf("step 1.1's call: model %q, or not briefed by the traced stage 1 rules", calls[0].Model)
			}
			if !strings.HasPrefix(calls[3].System, tc.brief+"\n\n"+briefs.Stage1+"\n\n=== FRAME: ") || calls[3].Model != "sonnet" {
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

// under returns the entries of files under dir/, keyed by the rest of the path.
func under(files map[string]string, dir string) map[string]string {
	out := map[string]string{}
	for k, v := range files {
		if rest, ok := strings.CutPrefix(k, dir+"/"); ok {
			out[rest] = v
		}
	}
	return out
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
// source, with replayed replies for every step, from step 1.1 to stage out
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

	p := testfixture.Replay(t,
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
// two segments, with replayed replies, in which a segment fails a stage 2 or
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
			p := testfixture.Replay(t, prompts, replies)

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

// stepDirsBefore returns the directories of the steps before the step whose
// directory is dir, in run order.
func stepDirsBefore(dir string) []string {
	dirs := stepDirs()
	return dirs[:slices.Index(dirs, dir)]
}
