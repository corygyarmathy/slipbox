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
	"strconv"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/extract"
	"github.com/corygyarmathy/slipbox/internal/fallback"
	"github.com/corygyarmathy/slipbox/internal/format"
	"github.com/corygyarmathy/slipbox/internal/keptloss"
	"github.com/corygyarmathy/slipbox/internal/lossproposer"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/quoteonce"
	"github.com/corygyarmathy/slipbox/internal/repair"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/runreport"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/stageout"
	"github.com/corygyarmathy/slipbox/internal/supportcheck"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
	"github.com/corygyarmathy/slipbox/internal/usagelimit"
)

// pragmatismAfterProposer gives the Pragmatism source and a run folder of
// the Pragmatism run stopped after step 1.5: every step's directory to there,
// copied whole. Step 1.5's union is given an invented second loss, 4.1
// (addLoss41).
func pragmatismAfterProposer(t *testing.T) (*source.Source, string) {
	t.Helper()
	folder := testfixture.PragmatismFolder(t, append(stepDirsBefore(run.LossProposerDir), run.LossProposerDir)...)
	addLoss41(t, folder.Dir)
	return testfixture.PragmatismSource(t), folder.Dir
}

// addLoss41 adds a loss to segment 4 in step 1.5's union in runDir, which
// the Pragmatism run's proposers didn't propose, so that the kept-loss repair
// calls two segments, and one adds nothing. Its wording is invented.
func addLoss41(t *testing.T, runDir string) {
	t.Helper()
	p := filepath.Join(runDir, run.LossProposerDir, run.ProposedLossesFile)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var segs []lossproposer.Segment
	if err := json.Unmarshal(b, &segs); err != nil {
		t.Fatal(err)
	}
	if segs[4].Segment != 4 || len(segs[4].Losses) != 0 {
		t.Fatalf("the recorded union has a loss in segment 4")
	}
	segs[4].Losses = []lossproposer.Loss{{Wordings: []lossproposer.Item{{
		Run:    1,
		Idea:   "An invented loss the notes already state",
		Quotes: []string{"An invented quote."},
		Why:    "It stands in for a loss the kept-loss call finds already stated.",
		Text:   "1. **An invented loss the notes already state**\n   - Quote: \"An invented quote.\"\n   - Why: It stands in for a loss the kept-loss call finds already stated.",
	}}}}
	if b, err = json.Marshal(segs); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// resume runs `s2n resume` on runDir with p, and gives its exit code and
// stderr. It stops after the final stage 1 notes, as these tests are of
// stage 1, and then writes the run report, which the run writes after stage
// 3, from what stage 1 left.
func resume(t *testing.T, runDir string, p provider.Provider) (int, string) {
	t.Helper()
	code, stderr := resumeTo(t, runDir, run.FinalDir, p)
	if code == 0 {
		folder, err := run.Open(runDir)
		if err != nil {
			t.Fatal(err)
		}
		path, err := folder.SourcePath()
		if err != nil {
			t.Fatal(err)
		}
		src, err := source.Read(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := runreport.Run(src, folder); err != nil {
			t.Fatal(err)
		}
	}
	return code, stderr
}

// resumeTo is resume, stopping after the step stopAfter.
func resumeTo(t *testing.T, runDir, stopAfter string, p provider.Provider) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runCommand(context.Background(), []string{"resume", "-stop-after", stopAfter, runDir}, &stdout, &stderr, p)
	return code, stderr.String()
}

// keptLossReplies are the prompts and replies for resuming
// pragmatismAfterProposer's run, which keeps every proposed loss: segment
// 0's call, served the Pragmatism run's reply, and segment 4's, served a
// reply that adds nothing, then step 1.3a's re-check of segment 0, served
// the run's, and of segment 4, which passes.
func keptLossReplies(t *testing.T, src *source.Source, runDir string) (prompts, replies []string) {
	t.Helper()
	kept, err := keptloss.Losses(&run.Folder{Dir: runDir})
	if err != nil {
		t.Fatal(err)
	}
	if ids := keptIDs(kept); !reflect.DeepEqual(ids, []string{"0.1", "4.1"}) {
		t.Fatalf("kept %q, want 0.1 and 4.1", ids)
	}
	before, err := note.ReadDir(filepath.Join(runDir, filepath.FromSlash(run.RepairNotes)))
	if err != nil {
		t.Fatal(err)
	}
	recorded := filepath.Join(testfixture.PragmatismRun(t), run.KeptLossDir)
	after, err := note.ReadDir(filepath.Join(recorded, run.NotesDir))
	if err != nil {
		t.Fatal(err)
	}
	keptRaw := testfixture.Files(t, filepath.Join(recorded, run.RawDir))
	recheckRaw := testfixture.Files(t, filepath.Join(recorded, run.KeptLossSupportDir, run.RawDir))
	return []string{
			keptloss.Prompt(before, src.Segments[0], kept[:1]), keptloss.Prompt(before, src.Segments[4], kept[1:]),
			supportcheck.Prompt(after, src.Segments[0]), supportcheck.Prompt(after, src.Segments[4]),
		},
		[]string{
			keptRaw["0.txt"], "=== REPORT ===\n- Loss 4.1: already stated.\n",
			recheckRaw["0.txt"], "# 4 The Formation of New Beliefs\n\npass 9\n",
		}
}

// runReport reads the run report in runDir, split into its sections.
func runReport(t *testing.T, runDir string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(runDir, run.RunReportDir, run.RunReportFile))
	if err != nil {
		t.Fatal(err)
	}
	sections := map[string]string{}
	for _, part := range strings.Split(string(b), "\n## ")[1:] {
		title, body, _ := strings.Cut(part, "\n")
		sections[title] = body
	}
	return sections
}

// Resuming after step 1.5 sends every proposed loss through the kept-loss
// call with its brief, the quote-once kept-loss brief by default, re-checks
// segments 0 and 4, and writes the final notes, which the run report reads,
// and stops at nothing. Segment 0's calls are the Pragmatism run's, so its
// final notes are the run's, byte for byte; segment 4's call adds nothing,
// so its loss is dropped. No step before it runs again, and a second resume
// has nothing to do.
func TestResume_AddsEveryLoss(t *testing.T) {
	src, runDir := pragmatismAfterProposer(t)
	prompts, replies := keptLossReplies(t, src, runDir)
	p := testfixture.Replay(t, prompts, replies)
	code, stderr := resume(t, runDir, p)
	if code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, stderr)
	}
	final := filepath.Join(runDir, filepath.FromSlash(run.FinalNotes))
	if !strings.Contains(stderr, "stopped after "+run.FinalDir) {
		t.Errorf("stderr %q does not say the run stopped after the final notes", stderr)
	}

	calls := p.Calls()
	if len(calls) != 4 {
		t.Fatalf("%d calls, want the 2 kept-loss calls and their 2 re-checks", len(calls))
	}
	frame := testfixture.PragmatismFrame(t)
	wantSystem := testfixture.RepoFile(t, "internal/briefs/kept-loss-edits-quote-once.md") + "\n\n" + testfixture.RepoFile(t, "internal/briefs/stage-1-quote-once.md") +
		"\n\n=== FRAME: " + testfixture.Pragmatism + ".md ===\n" + frame + "\n=== END FRAME ===\n"
	for _, c := range calls[:2] {
		if c.System != wantSystem || c.Model != "opus" {
			t.Errorf("kept-loss call: model %q, and its system prompt is not kept-loss-edits-quote-once.md, stage-1-quote-once.md and the frame", c.Model)
		}
	}
	// The re-check shares the notes the kept-loss call wrote, which the run
	// writes as the final notes once nothing in them is broken.
	after, err := note.ReadDir(final)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range calls[2:] {
		if c.System != supportcheck.Check().Brief+"\n\n=== FRAME: "+testfixture.Pragmatism+".md ===\n"+frame+"\n=== END FRAME ===\n\n"+supportcheck.Shared(after) || c.Model != supportcheck.Model {
			t.Errorf("re-check call is not step 1.3a's, with the notes it was given in its system prompt")
		}
	}
	// Segment 0's calls are the requests the run made.
	recorded := testfixture.PragmatismRun(t)
	for _, dir := range []string{run.KeptLossDir, filepath.Join(run.KeptLossDir, run.KeptLossSupportDir)} {
		got := testfixture.Files(t, filepath.Join(runDir, dir, run.RequestsDir))
		if want := testfixture.Files(t, filepath.Join(recorded, dir, run.RequestsDir)); got["0.sha256"] != want["0.sha256"] {
			t.Errorf("%s: segment 0's request is not the run's", dir)
		}
	}

	got := testfixture.Files(t, final)
	want := testfixture.Files(t, filepath.Join(recorded, filepath.FromSlash(run.FinalNotes)))
	if !reflect.DeepEqual(keys(got), keys(want)) {
		t.Fatalf("final notes %q, want %q", keys(got), keys(want))
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("final %s is not the run's", name)
		}
	}
	if repaired := testfixture.Files(t, filepath.Join(runDir, filepath.FromSlash(run.RepairNotes))); got["0 What Pragmatism Means.md"] == repaired["0 What Pragmatism Means.md"] {
		t.Errorf("final 0 What Pragmatism Means has no kept loss")
	}

	report := runReport(t, runDir)
	if added := report["Losses added"]; !strings.HasPrefix(added, "\n- Loss 0.1: A dispute can be settled by distinguishing") || strings.Contains(added, "Loss 4.") {
		t.Errorf("Losses added is not loss 0.1 alone:\n%s", added)
	}
	if w := "- 1.4-kept-loss-repair, segment 4: loss 4.1: the kept-loss call added no statement"; !strings.Contains(report["Dropped"], w) {
		t.Errorf("Dropped has no %q:\n%s", w, report["Dropped"])
	}
	if !strings.Contains(report["Statements that still fail"], "\n- ") {
		t.Errorf("Statements that still fail lists none:\n%s", report["Statements that still fail"])
	}
	for _, w := range []string{"- Asides: 0\n", "- Name spans that point outside their segment, author name spans included: 7, in ["} {
		if !strings.Contains(report["Asides and name spans"], w) {
			t.Errorf("Asides and name spans has no %q:\n%s", w, report["Asides and name spans"])
		}
	}

	again := testfixture.Replay(t, nil, nil)
	if code, stderr := resumeTo(t, runDir, run.FinalDir, again); code != 0 || len(again.Calls()) != 0 {
		t.Errorf("second resume: exit %d with %d calls, want 0 with none: %s", code, len(again.Calls()), stderr)
	}
}

// A broken trace in the notes the kept-loss repair reads stops nothing: the
// final notes drop its statement, with its trace, and the run report lists
// it.
func TestResume_ABrokenTraceIsDroppedAndReported(t *testing.T) {
	src, runDir := pragmatismAfterProposer(t)
	const name = "2 The Method's Character and Affinities.md"
	notePath := filepath.Join(runDir, filepath.FromSlash(run.RepairNotes), name)
	orig, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatal(err)
	}
	n := note.Parse(name, string(orig))
	stmt := n.Statements()[0]
	def := n.Defs()[0]
	tr, ok := def.Trace()
	if !ok || !slices.Contains(stmt.Refs, def.Label) {
		t.Fatalf("%s's first statement does not cite its first trace", name)
	}
	broken := strings.Replace(string(orig), "\""+tr.Quote+"\"", "\"The lamp is a kind of fish.\"", 1)
	if err := os.WriteFile(notePath, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	prompts, replies := keptLossReplies(t, src, runDir)
	if code, stderr := resume(t, runDir, testfixture.Replay(t, prompts, replies)); code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, stderr)
	}
	final := testfixture.Files(t, filepath.Join(runDir, filepath.FromSlash(run.FinalNotes)))[name]
	if strings.Contains(final, "The lamp is a kind of fish.") || strings.Contains(final, note.DropRefs(stmt.Text)) {
		t.Errorf("the final note keeps the broken statement or its trace:\n%s", final)
	}
	if w := "- stage-1-final, segment 2, `" + name + "`: " + stmt.Text + ": "; !strings.Contains(runReport(t, runDir)["Dropped"], w) {
		t.Errorf("the run report does not list the dropped statement %q", w)
	}
}

// A kept loss whose new statement still fails step 1.3a stops nothing: the
// statement stays, and the run report lists it with its grade. The
// Pragmatism run's re-check grades a statement its kept-loss call added as
// cut lost content and reworded.
func TestResume_KeptLossFailingSupportIsReported(t *testing.T) {
	src, runDir := pragmatismAfterProposer(t)
	prompts, replies := keptLossReplies(t, src, runDir)
	code, stderr := resume(t, runDir, testfixture.Replay(t, prompts, replies))
	if code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, stderr)
	}
	const added = "The corpus of[^7] a ferocious metaphysical dispute[^8] was a squirrel"
	if fails := runReport(t, runDir)["Statements that still fail"]; !strings.Contains(fails, "- 0 What Pragmatism Means: "+added) || !strings.Contains(fails, "(1.3a cut lost content: ") {
		t.Errorf("the run report does not list the new statement with its grade:\n%s", fails)
	}
	if !strings.Contains(testfixture.Files(t, filepath.Join(runDir, filepath.FromSlash(run.FinalNotes)))["0 What Pragmatism Means.md"], added) {
		t.Errorf("the final notes do not keep the new statement")
	}
}

// A run stopped in stage 1, here by a failed call in step 1.3b, resumes from
// that step: the steps before it aren't called again, and the stopped
// attempt's spent call stays in the step's usage log.
func TestResume_StoppedInStage1(t *testing.T) {
	sourcePath := testfixture.SourcePath(t, testfixture.Pragmatism)
	src := testfixture.PragmatismSource(t)
	steps := pragmatismSteps(t, src)
	done, doneReplies := replies(steps[extractStep], steps[supportStep])
	rest, restReplies := replies(steps[readsAloneStep : proposerStep+1]...)

	runDir := filepath.Join(t.TempDir(), "run")
	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"run", "-run-dir", runDir, "-stop-after", run.LossProposerDir, sourcePath}, &stdout, &stderr, testfixture.Replay(t, done, doneReplies)); code != 1 {
		t.Fatalf("run: exit %d, want 1: %s", code, stderr.String())
	}
	folder := &run.Folder{Dir: runDir}
	if folder.Complete(run.ReadsAloneDir) || !folder.Complete(run.SupportCheckDir) {
		t.Fatalf("run did not stop in step 1.3b")
	}

	p := testfixture.Replay(t, rest, restReplies)
	code, errText := resumeTo(t, runDir, run.LossProposerDir, p)
	if code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, errText)
	}
	if n := len(p.Calls()); n != len(rest) {
		t.Errorf("%d calls, want %d: the steps from 1.3b on", n, len(rest))
	}
	usage := testfixture.Files(t, filepath.Join(runDir, run.ReadsAloneDir))[run.UsageFile]
	if n := strings.Count(usage, "\n"); n != len(src.Segments)+1 {
		t.Errorf("step 1.3b's usage log has %d lines, want the stopped attempt's call and one per segment", n)
	}
	if !strings.Contains(usage, `"is_error":true`) {
		t.Errorf("step 1.3b's usage log does not keep the failed call:\n%s", usage)
	}
}

func TestResumeCommand_Errors(t *testing.T) {
	for _, c := range []struct {
		args []string
		code int
	}{
		{[]string{"resume", filepath.Join(t.TempDir(), "nope")}, 1},
		{[]string{"resume", t.TempDir()}, 1},
		{[]string{"resume", "-run-dir", "x", "y"}, 2},
		{[]string{"resume", "a", "b"}, 2},
	} {
		var stdout, stderr bytes.Buffer
		if code := runCommand(context.Background(), c.args, &stdout, &stderr, nil); code != c.code {
			t.Errorf("runCommand(%q): exit %d, want %d: %s", c.args, code, c.code, stderr.String())
		}
	}
}

// A resume after the kept-loss repair stopped calls only what hadn't
// finished (ADR 0002 §6). The first resume serves every reply but segment
// 4's re-check, so the step stops in its re-check. The second makes no
// kept-loss call, and re-checks only the segment the first didn't finish:
// segment 0's re-check, called first, always finishes. The step's usage log
// holds both attempts' calls, and the final notes are the Pragmatism run's.
func TestResume_CallsOnlyTheUnfinishedSegments(t *testing.T) {
	src, runDir := pragmatismAfterProposer(t)
	// prompts are segment 0's and 4's kept-loss calls, then their re-checks.
	prompts, replies := keptLossReplies(t, src, runDir)
	first := testfixture.Replay(t, prompts[:3], replies[:3])
	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"resume", "-stop-after", run.FinalDir, runDir}, &stdout, &stderr, first); code == 0 {
		t.Fatal("resume succeeded with no reply for segment 4's re-check")
	}
	if (&run.Folder{Dir: runDir}).Complete(run.KeptLossDir) {
		t.Fatal("the kept-loss repair is complete after its re-check failed")
	}
	recheck := filepath.Join(runDir, run.KeptLossDir+run.PartialSuffix, run.KeptLossSupportDir+run.PartialSuffix)
	if _, err := os.Stat(filepath.Join(recheck, run.RequestsDir, "0.sha256")); err != nil {
		t.Fatalf("segment 0's re-check, called first, is not finished: %v", err)
	}
	if _, err := os.Stat(filepath.Join(recheck, run.RequestsDir, "4.sha256")); err == nil {
		t.Fatal("segment 4's re-check is finished, though its call failed")
	}

	second := testfixture.Replay(t, prompts, replies)
	stderr.Reset()
	if code := runCommand(context.Background(), []string{"resume", "-stop-after", run.FinalDir, runDir}, &stdout, &stderr, second); code != 0 {
		t.Fatalf("second resume: exit %d, want 0: %s", code, stderr.String())
	}
	if calls := second.Calls(); len(calls) != 1 || calls[0].User != prompts[3] {
		t.Errorf("second resume made %d calls, want only segment 4's unfinished re-check", len(calls))
	}

	b, err := os.ReadFile(filepath.Join(runDir, run.KeptLossDir, run.UsageFile))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Count(string(b), "\n"), len(first.Calls())+len(second.Calls()); got != want {
		t.Errorf("usage log holds %d calls, want both attempts' %d", got, want)
	}
	got := testfixture.Files(t, filepath.Join(runDir, filepath.FromSlash(run.FinalNotes)))
	want := testfixture.Files(t, filepath.Join(testfixture.PragmatismRun(t), filepath.FromSlash(run.FinalNotes)))
	if !reflect.DeepEqual(got, want) {
		t.Error("final notes are not the Pragmatism run's")
	}
}

// keptIDs gives each kept loss's id, in order.
func keptIDs(kept []keptloss.Kept) []string {
	var ids []string
	for _, k := range kept {
		ids = append(ids, k.ID())
	}
	return ids
}

// Resuming the Pragmatism run with its stage 1 done runs step 2.1 on the
// final stage 1 notes, with the model -stage-2-model names, then its gates.
// The run's split replies serve it, but segment 3's is made to lose a
// statement and its trace. A folder no segment claims, added by hand, stops
// the gates, and once it is gone they run again, with no call. The split
// check fails segment 3, though it takes the name-span traces the split
// added, and segment 3 falls back to its final stage 1 notes under a hub of
// its own. Step 2.2 strips every segment's notes, and step 3.1 formats them.
// One reply changed a word of segment 3's, so the format check fails it,
// and it falls back again, to its stripped notes. Step 3.2 stages every
// segment's notes.
func TestResume_SplitsTheFinalNotes(t *testing.T) {
	// A resume that can reach step 3.2 checks the vault before it runs a
	// step, so the run has one from the start.
	vault := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(filepath.Join(vault, stageout.SlipBoxDir), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(VaultEnv, vault)
	runDir := testfixture.PragmatismFolder(t, stepDirsBefore(run.SplitDir)...).Dir
	src := testfixture.PragmatismSource(t)
	final := filepath.Join(runDir, filepath.FromSlash(run.FinalNotes))
	notes, err := note.ReadDir(final)
	if err != nil {
		t.Fatal(err)
	}
	var prompts []string
	for _, seg := range src.Segments {
		prompt, _ := split.Prompt(src, notes, seg)
		prompts = append(prompts, prompt)
	}
	replies := testfixture.RawReplies(t, filepath.Join(testfixture.PragmatismRun(t), run.SplitDir), len(src.Segments))
	const lost = " Writers on this subject have begun to show a singular unanimity as to what the laws of nature and elements of fact mean, when formulated by mathematicians, physicists and chemists.[^2]"
	lostTrace := "[^2]: [[" + src.Name + "#Pragmatism as a Theory of Truth]] \"Writers on this subject have begun to show a singular unanimity as to what the laws of nature and elements of fact mean, when formulated by mathematicians, physicists and chemists.\"\n"
	if !strings.Contains(replies[3], lost) || !strings.Contains(replies[3], lostTrace) {
		t.Fatalf("segment 3's split reply has no statement to lose")
	}
	replies[3] = strings.Replace(strings.Replace(replies[3], lost, "", 1), lostTrace, "", 1)
	p := testfixture.Replay(t, prompts, replies)

	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"resume", "-stage-2-model", "haiku", "-stop-after", run.SplitDir, runDir}, &stdout, &stderr, p); code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, stderr.String())
	}
	if n := len(p.Calls()); n != len(src.Segments) {
		t.Errorf("%d calls, want step 2.1's, one per segment", n)
	}
	for _, c := range p.Calls() {
		if c.Model != "haiku" {
			t.Errorf("call for %.40q: model %q, want haiku", c.User, c.Model)
		}
	}
	splitNotes := filepath.Join(runDir, filepath.FromSlash(run.SplitNotes))
	if n := len(testfixture.Files(t, splitNotes)); n != 64 {
		t.Errorf("%d notes, want the run's 56 atomic notes and 8 hubs", n)
	}

	// A folder no segment claims stops the gates, with exit status 4, and
	// says what to do.
	extra := filepath.Join(splitNotes, "99 Extra")
	if err := os.Mkdir(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	again := testfixture.Replay(t, nil, nil)
	stderr.Reset()
	if code := runCommand(context.Background(), []string{"resume", "-stage-2-model", "haiku", runDir}, &stdout, &stderr, again); code != ExitGate || len(again.Calls()) != 0 {
		t.Fatalf("resume with a stray folder: exit %d with %d calls, want %d with none: %s", code, len(again.Calls()), ExitGate, stderr.String())
	}
	for _, want := range []string{
		`step 2.1's folder check failed on folder "99 Extra". No segment of the source has the folder name of ` + extra + ".",
		"then run s2n resume -stage-2-model haiku " + runDir,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr %q does not say %q", stderr.String(), want)
		}
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}

	stderr.Reset()
	if code := runCommand(context.Background(), []string{"resume", "-stop-after", run.StripDir, runDir}, &stdout, &stderr, again); code != 0 || len(again.Calls()) != 0 {
		t.Fatalf("resume to step 2.2: exit %d with %d calls, want 0 with none: %s", code, len(again.Calls()), stderr.String())
	}
	folder := &run.Folder{Dir: runDir}
	gates := filepath.Join(runDir, run.SplitGatesDir)
	if !folder.Complete(run.SplitGatesDir) {
		t.Fatalf("step 2.1's gates are not complete")
	}
	items, err := fallback.Read(folder, run.SplitGatesDir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []fallback.Item{{Segment: 3, Why: "the split check", Report: run.SplitCheckFile}}; !reflect.DeepEqual(items, want) {
		t.Errorf("fallbacks %+v, want %+v", items, want)
	}
	kept, err := os.ReadFile(filepath.Join(gates, run.SplitCheckFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, truth, _ := strings.Cut(string(kept), "\n## 3 Pragmatism as a Theory of Truth: "); !strings.Contains(truth, "\n  lost_traces: ") {
		t.Errorf("report kept does not give segment 3's lost trace:\n%s", kept)
	}
	if !strings.Contains(string(kept), "  name_span: ") {
		t.Errorf("report kept lists no name span:\n%s", kept)
	}
	if b, err := os.ReadFile(filepath.Join(gates, run.TraceDiffFile)); err != nil || strings.Contains(string(b), "3 Pragmatism as a Theory of Truth") || !strings.Contains(string(b), "## 2 The Method's Character and Affinities: ") {
		t.Errorf("the trace diff did not check every segment but the one the split check failed (err %v):\n%s", err, b)
	}
	gated := testfixture.Files(t, filepath.Join(runDir, filepath.FromSlash(run.SplitOrFallbackNotes)))
	fb := split.Fallback(src.Segments[3], notes)
	want := map[string]string{}
	for rel, text := range testfixture.Files(t, splitNotes) {
		if !strings.HasPrefix(rel, fb.Name+"/") {
			want[rel] = text
		}
	}
	for _, n := range fb.Notes {
		want[fb.Name+"/"+n.Name] = n.Render()
	}
	if !reflect.DeepEqual(gated, want) {
		t.Errorf("the notes the gates passed on are not segment 3's fallback and the rest of the split")
	}

	stripped := filepath.Join(runDir, filepath.FromSlash(run.StrippedNotes))
	if !folder.Complete(run.StripDir) {
		t.Fatalf("step 2.2 is not complete")
	}
	if got, want := len(testfixture.Files(t, stripped)), len(gated); got != want {
		t.Errorf("step 2.2 wrote %d notes, want the %d the gates passed on", got, want)
	}
	hubs := map[string]bool{}
	for _, seg := range src.Segments {
		hubs[split.Folder(seg)+"/"+split.Hub(seg)] = true
	}
	for rel, text := range testfixture.Files(t, stripped) {
		want := 2
		if hubs[rel] {
			want = 0
		}
		if got := strings.Count(text, "[^"); got != want {
			t.Errorf("%s: %d footnote marks, want %d, a source footnote's or a hub's none", rel, got, want)
		}
	}

	// Step 3.1 formats the stripped notes, with the model -stage-3-model
	// names. Each reply is "no change", every note as it came, but for one
	// word of segment 3's main note, which its fallback kept.
	folders, err := split.ReadFolders(stripped)
	if err != nil {
		t.Fatal(err)
	}
	const changed = "3 Pragmatism as a Theory of Truth/3 Pragmatism as a Theory of Truth.md"
	prompts = nil
	replies = nil
	for _, f := range folders {
		prompts = append(prompts, format.Prompt(f))
		var reply string
		for _, n := range f.Notes {
			text := n.Render()
			if f.Name+"/"+n.Name == changed {
				if !strings.Contains(text, " approximations.") {
					t.Fatalf("%s has no word to change", changed)
				}
				text = strings.Replace(text, " approximations.", " estimates.", 1)
			}
			reply += "=== FILE: " + n.Name + " ===\n" + text
		}
		replies = append(replies, reply+"=== MANIFEST ===\n{}\n")
	}
	p = testfixture.Replay(t, prompts, replies)
	stderr.Reset()
	if code := runCommand(context.Background(), []string{"resume", "-stage-3-model", "sonnet", "-stop-after", run.FormatGateDir, runDir}, &stdout, &stderr, p); code != 0 {
		t.Fatalf("resume to step 3.1's gate: exit %d, want 0: %s", code, stderr.String())
	}
	if n := len(p.Calls()); n != len(src.Segments) {
		t.Errorf("%d calls, want step 3.1's, one per segment", n)
	}
	for _, c := range p.Calls() {
		if c.Model != "sonnet" {
			t.Errorf("call for %.40q: model %q, want sonnet", c.User, c.Model)
		}
	}
	if !folder.Complete(run.FormatGateDir) {
		t.Fatalf("step 3.1's gate is not complete")
	}
	if items, err := fallback.Read(folder, run.FormatGateDir); err != nil || !reflect.DeepEqual(items, []fallback.Item{{Segment: 3, Why: "the format check", Report: run.FormatCheckFile}}) {
		t.Errorf("fallbacks %+v (err %v), want segment 3's, for the format check", items, err)
	}
	if b, err := os.ReadFile(filepath.Join(runDir, run.FormatGateDir, run.FormatCheckFile)); err != nil || !strings.Contains(string(b), "\n"+changed+"\n  PROBLEM: TEXT CHANGED: - approximations. + estimates.\n") {
		t.Errorf("report kept does not give the changed word (err %v):\n%s", err, b)
	}
	checked := filepath.Join(runDir, filepath.FromSlash(run.FormattedOrFallbackNotes))
	if got, want := testfixture.Files(t, checked), testfixture.Files(t, stripped); !reflect.DeepEqual(got, want) {
		t.Errorf("step 3.1's gate passed on %d notes, not the %d stripped ones: each reply was no change, but for segment 3's, which fell back", len(got), len(want))
	}

	// With no vault the run stops before step 3.2.
	t.Setenv(VaultEnv, "")
	stderr.Reset()
	if code := runCommand(context.Background(), []string{"resume", runDir}, &stdout, &stderr, again); code != 1 || len(again.Calls()) != 0 {
		t.Fatalf("resume with no vault: exit %d with %d calls, want 1 with none: %s", code, len(again.Calls()), stderr.String())
	}
	if want := "Give -vault DIR or set $" + VaultEnv + ", then run s2n resume " + runDir; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr %q does not say %q", stderr.String(), want)
	}

	// Given a vault, step 3.2 stages the notes out into the default staging
	// folder, and the run folder is deleted. The vault's _Slip_Box holds a
	// note named like segment 2's hub, in another case.
	hub := split.Hub(src.Segments[2])
	upper := strings.ToUpper(strings.TrimSuffix(hub, ".md"))
	slipBoxNote := filepath.Join(vault, stageout.SlipBoxDir, upper+".md")
	if err := os.MkdirAll(filepath.Dir(slipBoxNote), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(slipBoxNote, []byte("# "+strings.ToUpper(src.Segments[2].Title)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checkedFolders, err := split.ReadFolders(checked)
	if err != nil {
		t.Fatal(err)
	}
	placed, _ := stageout.Place(src.Name, checkedFolders, nil)
	wantStaged := map[string]string{}
	for _, pl := range placed {
		wantStaged[pl.Path()] = pl.Text
	}
	if _, ok := wantStaged[hub]; !ok {
		t.Fatalf("segment 2's hub is not staged flat, as %s", hub)
	}

	// A staging folder that already holds a file stops the run, which
	// keeps its run folder.
	staging := filepath.Join(vault, stageout.StagingDir, src.Name)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(staging, "old.md")
	if err := os.WriteFile(old, []byte("an earlier run's\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := runCommand(context.Background(), []string{"resume", "-vault", vault, runDir}, &stdout, &stderr, again); code != 1 || len(again.Calls()) != 0 {
		t.Fatalf("resume into an occupied staging folder: exit %d with %d calls, want 1 with none: %s", code, len(again.Calls()), stderr.String())
	}
	if want := "or give -staging another folder, then run s2n resume -vault " + vault + " " + runDir; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr %q does not say %q", stderr.String(), want)
	}
	if _, err := os.Stat(runDir); err != nil {
		t.Errorf("the run folder is not kept: %v", err)
	}
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}

	stderr.Reset()
	if code := runCommand(context.Background(), []string{"resume", "-vault", vault, runDir}, &stdout, &stderr, again); code != 0 || len(again.Calls()) != 0 {
		t.Fatalf("resume with a vault: exit %d with %d calls, want 0 with none: %s", code, len(again.Calls()), stderr.String())
	}
	for _, want := range []string{"staged the notes in " + staging, "model calls cost $"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr %q does not say %q", stderr.String(), want)
		}
	}
	if _, err := os.Stat(runDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the run folder is not deleted (stat: %v)", err)
	}
	got := testfixture.Files(t, staging)
	clashes, ok := got[stageout.ClashListFile]
	if !ok {
		t.Fatalf("the staging folder has no %s", stageout.ClashListFile)
	}
	delete(got, stageout.ClashListFile)
	// The run report, written after step 3.1's gate, names both fallbacks.
	report := got[stageout.RunReportFile]
	for _, w := range []string{
		"# Run report: " + src.Name + "\n",
		"- 2.1-gates, segment 3 (Pragmatism as a Theory of Truth): fell back to its stage 1 notes: the split check failed it; its report is split-check.txt\n",
		"- 3.1-gate, segment 3 (Pragmatism as a Theory of Truth): kept its notes unformatted: the format check failed it; its report is format-check.txt\n",
	} {
		if !strings.Contains(report, w) {
			t.Errorf("the staging folder's %s has no %q:\n%s", stageout.RunReportFile, w, report)
		}
	}
	delete(got, stageout.RunReportFile)
	if !reflect.DeepEqual(got, wantStaged) {
		t.Errorf("staged %d notes, want the %d the gate passed on", len(got), len(wantStaged))
	}
	if want := "- [[" + stageout.StagingDir + "/" + src.Name + "/" + strings.TrimSuffix(hub, ".md") + "|" + hub + "]] and [[" + stageout.SlipBoxDir + "/" + upper + "|" + stageout.SlipBoxDir + "/" + upper + ".md]]\n"; !strings.Contains(clashes, want) {
		t.Errorf("the clash list does not name %s as like %s:\n%s", hub, slipBoxNote, clashes)
	}
	vaultFiles := testfixture.Files(t, vault)
	for rel := range vaultFiles {
		if !strings.HasPrefix(rel, stageout.StagingDir+"/"+src.Name+"/") && rel != stageout.SlipBoxDir+"/"+upper+".md" {
			t.Errorf("%s was written in the vault, outside the staging folder", rel)
		}
	}
}

// A run given -stage-2-model or -stage-3-model is told to resume with it.
func TestResumeCommand_StageModels(t *testing.T) {
	opts := options{stage1Model: extract.DefaultModel, extractEffort: extract.DefaultEffort, repairModel: extract.DefaultModel,
		repairReply: repair.DefaultReply, quoteOnce: quoteonce.Default, proposerModel: lossproposer.Model, stage2Model: "haiku", stage3Model: "sonnet", fiveHourUSD: usagelimit.DefaultFiveHourUSD}
	if got, want := resumeCommand(opts, "dir"), "s2n resume -stage-2-model haiku -stage-3-model sonnet dir"; got != want {
		t.Errorf("resumeCommand = %q, want %q", got, want)
	}
	opts.stage2Model, opts.stage3Model = split.DefaultModel, format.DefaultModel
	if got, want := resumeCommand(opts, "dir"), "s2n resume dir"; got != want {
		t.Errorf("resumeCommand = %q, want %q", got, want)
	}
}

// A run given -vault other than $S2N_VAULT, or -staging other than the
// vault's default, is told to resume with it.
func TestResumeCommand_VaultAndStaging(t *testing.T) {
	opts := options{stage1Model: extract.DefaultModel, extractEffort: extract.DefaultEffort, repairModel: extract.DefaultModel,
		repairReply: repair.DefaultReply, quoteOnce: quoteonce.Default, proposerModel: lossproposer.Model, stage2Model: split.DefaultModel, stage3Model: format.DefaultModel, fiveHourUSD: usagelimit.DefaultFiveHourUSD,
		vault: "v", vaultDefault: "v", sourceName: "src", staging: stageout.DefaultStaging("v", "src")}
	if got, want := resumeCommand(opts, "dir"), "s2n resume dir"; got != want {
		t.Errorf("resumeCommand = %q, want %q", got, want)
	}
	opts.vault, opts.staging = "w", "elsewhere"
	if got, want := resumeCommand(opts, "dir"), "s2n resume -vault w -staging elsewhere dir"; got != want {
		t.Errorf("resumeCommand = %q, want %q", got, want)
	}
}

// A run given -proposer-model is told to resume with it, and one left at the
// proposer's tier is not.
func TestResumeCommand_ProposerModel(t *testing.T) {
	opts := options{stage1Model: extract.DefaultModel, extractEffort: extract.DefaultEffort, repairModel: extract.DefaultModel,
		repairReply: repair.DefaultReply, quoteOnce: quoteonce.Default, proposerModel: "opus", stage2Model: split.DefaultModel, stage3Model: format.DefaultModel, fiveHourUSD: usagelimit.DefaultFiveHourUSD}
	if got, want := resumeCommand(opts, "dir"), "s2n resume -proposer-model opus dir"; got != want {
		t.Errorf("resumeCommand = %q, want %q", got, want)
	}
	opts.proposerModel = lossproposer.Model
	if got, want := resumeCommand(opts, "dir"), "s2n resume dir"; got != want {
		t.Errorf("resumeCommand = %q, want %q", got, want)
	}
}

// A run folder from before the run became unattended, that got past the
// kept-loss repair, resumes:
// its checkpoint was reviewed, its kept-loss repair wrote no still-fails
// list or dropped list, and step 1.5 wrote no dropped list, which the
// run report reads as empty.
func TestResume_AnOldFolderPastTheKeptLossRepair(t *testing.T) {
	src, runDir := pragmatismAfterProposer(t)
	prompts, replies := keptLossReplies(t, src, runDir)
	if code, stderr := resumeTo(t, runDir, run.FinalDir, testfixture.Replay(t, prompts, replies)); code != 0 {
		t.Fatalf("resume to the final notes: exit %d: %s", code, stderr)
	}
	for _, f := range []string{
		filepath.Join(run.KeptLossDir, run.StillFailsFile),
		filepath.Join(run.KeptLossDir, run.DroppedFile),
		filepath.Join(run.LossProposerDir, run.DroppedFile),
	} {
		if err := os.Remove(filepath.Join(runDir, f)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(runDir, oldCheckpointDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if code, stderr := resume(t, runDir, testfixture.Replay(t, nil, nil)); code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, stderr)
	}
	if got := runReport(t, runDir)["Dropped"]; strings.TrimSpace(got) != "None." {
		t.Errorf("the run report's dropped section is %q, want none, since the lists are gone", got)
	}
}

// A run folder from before the run became unattended, that stopped at the
// checkpoint, isn't
// resumed, since its review would be ignored, and the stop says what to do.
func TestResume_RefusesAnOldCheckpoint(t *testing.T) {
	_, runDir := pragmatismAfterProposer(t)
	cp := filepath.Join(runDir, oldCheckpointDir)
	if err := os.Mkdir(cp, 0o755); err != nil {
		t.Fatal(err)
	}
	code, stderr := resume(t, runDir, testfixture.Replay(t, nil, nil))
	if code != 1 {
		t.Fatalf("resume: exit %d, want 1: %s", code, stderr)
	}
	for _, w := range []string{"stopped at the checkpoint", "ignore the edits and keep boxes in " + cp, "Start a new run with s2n run, or delete " + cp} {
		if !strings.Contains(stderr, w) {
			t.Errorf("stderr %q does not say %q", stderr, w)
		}
	}
	if _, err := os.Stat(filepath.Join(runDir, run.KeptLossDir)); err == nil {
		t.Error("the kept-loss repair ran")
	}
}

// A run folder stopped in step 2.1's gates, as the Pragmatism run's would be
// with its gates' directory partial, resumes through all three of the gates
// with no model call: each report is the run's, listing its name spans
// (ADR 0010) and nothing that fails.
func TestResume_PassesTheSplitGates(t *testing.T) {
	runDir := testfixture.PragmatismFolder(t, stepDirsBefore(run.SplitGatesDir)...).Dir
	recorded := filepath.Join(testfixture.PragmatismRun(t), run.SplitGatesDir)
	if err := os.CopyFS(filepath.Join(runDir, run.SplitGatesDir+run.PartialSuffix), os.DirFS(recorded)); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	p := testfixture.Replay(t, nil, nil)
	if code := runCommand(context.Background(), []string{"resume", "-stop-after", run.SplitGatesDir, runDir}, &stdout, &stderr, p); code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, stderr.String())
	}
	if calls := p.Calls(); len(calls) != 0 {
		t.Errorf("%d model calls, want none", len(calls))
	}
	if !(&run.Folder{Dir: runDir}).Complete(run.SplitGatesDir) {
		t.Fatal("step 2.1's gates are not complete")
	}
	if got, want := testfixture.Files(t, filepath.Join(runDir, run.SplitGatesDir)), testfixture.Files(t, recorded); !reflect.DeepEqual(got, want) {
		t.Errorf("step 2.1's gates wrote %d files, not the run's %d byte for byte", len(got), len(want))
	}
	const spans = 3
	if got := countNameSpans(t, filepath.Join(runDir, run.SplitGatesDir, run.SplitCheckFile), func(string) int { return 1 }, "  name_span: "); got != spans {
		t.Errorf("the split check lists %d name spans, want %d", got, spans)
	}
	times := func(rest string) int {
		n, _ := strconv.Atoi(rest[:strings.Index(rest, ":")])
		return n
	}
	if got := countNameSpans(t, filepath.Join(runDir, run.SplitGatesDir, run.TraceDiffFile), times, "  NAME SPAN x"); got != spans {
		t.Errorf("the trace diff lists %d name spans, want %d", got, spans)
	}
}

// countNameSpans counts the name spans in the report at path: on each line
// starting with prefix, as many as count gives for the rest of the line.
func countNameSpans(t *testing.T, path string, count func(rest string) int, prefix string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, l := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(l, prefix); ok {
			n += count(rest)
		}
	}
	return n
}
