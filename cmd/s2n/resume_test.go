package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

// Step 1.5's replies on Lamp's segments 2 and 3 in lampAfterProposer, each
// proposing a loss, and the kept-loss repair's and its re-check's replies on
// them in keptLossCases.
const (
	// stormsLoss gives the author's sentence, which no note quotes, as a
	// loss.
	stormsLoss = "# Storms\n\n" +
		"1. **The author stood in the lamp room**\n" +
		"   - Quote: \"I have stood in that lamp room in a gale, and the glass shook.\"\n" +
		"   - Why: The author saw a gale from the lamp.\n"
	lensLoss = "# The Lens\n\n" +
		"1. **The lens is named: the Fresnel lens**\n" +
		"   - Quote: \"*The Fresnel lens*\"\n" +
		"   - Why: The section names it.\n"
	// lensKept adds loss 2.1, inserted quote-once after the note's last
	// statement.
	lensKept = "=== EDIT: 2 The Lens.md ===\n" +
		"--- AFTER ---\nIt uses rings of prisms instead of one thick glass.[^5]\n" +
		"--- INSERT ---\n*The Fresnel lens* uses rings of prisms instead of one thick glass.\n" +
		"=== REPORT ===\n- Loss 2.1: named the lens.\n"
	// lensRecheck grades the statement lensKept adds as cut lost content.
	lensRecheck = "# 2 The Lens\n\n" +
		"- [cut lost content] \"*The Fresnel lens* uses rings…\": the cut drops that the rings replace one thick glass.\n" +
		"pass 5 / cut lost content 1\n"
	// stormsAlready adds nothing for loss 3.1, which it finds already stated.
	stormsAlready = "=== REPORT ===\n- Loss 3.1: already stated.\n"
)

// lampAfterProposer gives a run folder of Lamp stopped after step 1.5, which
// proposed a loss in segment 2, 2.1, and one in segment 3, 3.1, so that the
// kept-loss repair calls two segments.
func lampAfterProposer(t *testing.T) string {
	t.Helper()
	runDir, _, _ := lampRun(t, lampReplies{run.LossProposerDir: {
		2: {lensLoss, lensLoss},
		3: {stormsLoss, stormsLoss},
	}}, "-stop-after", run.LossProposerDir)
	kept, err := keptloss.Losses(&run.Folder{Dir: runDir})
	if err != nil {
		t.Fatal(err)
	}
	if ids := keptIDs(kept); !reflect.DeepEqual(ids, []string{"2.1", "3.1"}) {
		t.Fatalf("kept %q, want 2.1 and 3.1", ids)
	}
	return runDir
}

// keptLoss and keptLossRecheck are the keys Serve matches segment n's
// kept-loss call and its re-check on: the labels their user prompts give the
// segment's notes.
func keptLoss(n int) string { return fmt.Sprintf("=== YOUR SEGMENT'S NOTES (segment %d: ", n) }
func keptLossRecheck(n int) string {
	return fmt.Sprintf("=== YOUR SEGMENT'S NOTES TO CHECK (segment %d: ", n)
}

// keptLossCases are the replies for resuming lampAfterProposer's run, which
// keeps every proposed loss: segment 2's kept-loss call adds loss 2.1, which
// its re-check fails, and segment 3's adds nothing, so its re-check passes.
func keptLossCases() []testfixture.Case {
	return []testfixture.Case{
		{Contains: keptLoss(2), Reply: lensKept},
		{Contains: keptLoss(3), Reply: stormsAlready},
		{Contains: keptLossRecheck(2), Reply: lensRecheck},
		{Contains: keptLossRecheck(3), Reply: "pass\n"},
	}
}

// lampFinal are the final stage 1 notes keptLossCases leave: LampStage1,
// with loss 2.1 added to segment 2's note.
func lampFinal() map[string]string {
	final := testfixture.LampStage1()
	lens := func(q string) string { return testfixture.LampTrace("The Lens", q) + "\n" }
	final["2 The Lens.md"] = "# 2 The Lens\n\n" +
		"The lens bends light into a beam.[^1]\n" +
		"A beam carries farther than a glow.[^2]\n" +
		"Obviously, a keeper can take no notice of the lens on a clear night.[^3]\n" +
		"On a foggy night it is no help at all.[^4]\n" +
		"It uses rings of prisms instead of one thick glass.[^5] *The Fresnel lens* uses rings of prisms instead of one thick glass.[^6]\n\n" +
		"[^1]: " + lens("The lens bends light into a beam.") +
		"[^2]: " + lens("A beam carries farther than a glow.") +
		"[^3]: " + lens("Obviously, a keeper can take no notice of the lens on a clear night.") +
		"[^4]: " + lens("On a foggy night it is no help at all.") +
		"[^5]: " + lens("uses rings of prisms instead of one thick glass") +
		"[^6]: " + lens("*The Fresnel lens* uses rings of prisms instead of one thick glass.")
	return final
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
// segments 2 and 3, and writes the final notes, which the run report reads,
// and stops at nothing. Segment 2's call adds its loss, and segment 3's
// adds nothing, so its loss is dropped. No step before it runs again, and a
// second resume has nothing to do.
func TestResume_AddsEveryLoss(t *testing.T) {
	runDir := lampAfterProposer(t)
	src := testfixture.LampSource(t)
	before, err := note.ReadDir(filepath.Join(runDir, filepath.FromSlash(run.RepairNotes)))
	if err != nil {
		t.Fatal(err)
	}
	kept, err := keptloss.Losses(&run.Folder{Dir: runDir})
	if err != nil {
		t.Fatal(err)
	}
	p := testfixture.Serve(t, keptLossCases()...)
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
	// Each kept-loss call is given its segment's losses and the notes step
	// 1.4 left, segment 2's first.
	wantSystem := testfixture.RepoFile(t, "internal/briefs/kept-loss-edits-quote-once.md") + "\n\n" +
		testfixture.RepoFile(t, "internal/briefs/stage-1-quote-once.md") + "\n\n" + lampFrame
	for i, n := range []int{2, 3} {
		c := calls[i]
		if c.System != wantSystem || c.Model != "opus" || c.Effort != "medium" {
			t.Errorf("kept-loss call %d: model %q at %q, and its system prompt is not kept-loss-edits-quote-once.md, stage-1-quote-once.md and the frame", i, c.Model, c.Effort)
		}
		if want := keptloss.Prompt(before, src.Segments[n], kept[i:i+1]); c.User != want {
			t.Errorf("kept-loss call %d is not segment %d's, with its loss and step 1.4's notes:\n%s", i, n, c.User)
		}
	}
	// The re-check shares the notes the kept-loss call wrote, which the run
	// writes as the final notes once nothing in them is broken.
	after, err := note.ReadDir(final)
	if err != nil {
		t.Fatal(err)
	}
	wantSystem = testfixture.RepoFile(t, "internal/briefs/checker-support.md") + "\n\n" + lampFrame + "\n" + supportcheck.Shared(after)
	for i, n := range []int{2, 3} {
		c := calls[2+i]
		if c.System != wantSystem || c.Model != "sonnet" || c.Effort != "medium" {
			t.Errorf("re-check call %d is not step 1.3a's, with the notes it was given in its system prompt", i)
		}
		if c.User != supportcheck.Prompt(after, src.Segments[n]) {
			t.Errorf("re-check call %d is not segment %d's, on the kept-loss notes:\n%s", i, n, c.User)
		}
	}

	if got, want := testfixture.Files(t, final), lampFinal(); !reflect.DeepEqual(got, want) {
		t.Errorf("final notes:\n%v\nwant:\n%v", got, want)
	}
	report := runReport(t, runDir)
	if added := report["Losses added"]; !strings.HasPrefix(added, "\n- Loss 2.1: The lens is named") || strings.Contains(added, "Loss 3.") {
		t.Errorf("Losses added is not loss 2.1 alone:\n%s", added)
	}
	if w := "- 1.4-kept-loss-repair, segment 3: loss 3.1: the kept-loss call added no statement"; !strings.Contains(report["Dropped"], w) {
		t.Errorf("Dropped has no %q:\n%s", w, report["Dropped"])
	}
	if !strings.Contains(report["Statements that still fail"], "\n- ") {
		t.Errorf("Statements that still fail lists none:\n%s", report["Statements that still fail"])
	}

	again := testfixture.Serve(t)
	if code, stderr := resumeTo(t, runDir, run.FinalDir, again); code != 0 || len(again.Calls()) != 0 {
		t.Errorf("second resume: exit %d with %d calls, want 0 with none: %s", code, len(again.Calls()), stderr)
	}
}

// A broken trace in the notes the kept-loss repair reads stops nothing: the
// final notes drop its statement, with its trace, and the run report lists
// it.
func TestResume_ABrokenTraceIsDroppedAndReported(t *testing.T) {
	runDir := lampAfterProposer(t)
	const name = "2 The Lens.md"
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
	if code, stderr := resume(t, runDir, testfixture.Serve(t, keptLossCases()...)); code != 0 {
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
// statement stays, and the run report lists it with its grade.
func TestResume_KeptLossFailingSupportIsReported(t *testing.T) {
	runDir := lampAfterProposer(t)
	code, stderr := resume(t, runDir, testfixture.Serve(t, keptLossCases()...))
	if code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, stderr)
	}
	const added = "*The Fresnel lens* uses rings of prisms instead of one thick glass.[^6]"
	if fails := runReport(t, runDir)["Statements that still fail"]; !strings.Contains(fails, "- 2 The Lens: ") || !strings.Contains(fails, added) || !strings.Contains(fails, "(1.3a cut lost content: ") {
		t.Errorf("the run report does not list the new statement with its grade:\n%s", fails)
	}
	if !strings.Contains(testfixture.Files(t, filepath.Join(runDir, filepath.FromSlash(run.FinalNotes)))["2 The Lens.md"], added) {
		t.Errorf("the final notes do not keep the new statement")
	}
}

// A run stopped in stage 1, here by a failed call in step 1.3b, resumes from
// that step: the steps before it aren't called again, and the stopped
// attempt's spent call stays in the step's usage log.
func TestResume_StoppedInStage1(t *testing.T) {
	runDir := filepath.Join(t.TempDir(), "run")
	lamp := lampModel(t, runDir, nil)
	first := testfixture.NewFake(func(req provider.Request) (provider.Reply, error) {
		if step, _ := stepOf(req); step == run.ReadsAloneDir {
			return provider.Reply{}, errors.New("the call failed")
		}
		return lamp.Call(context.Background(), req)
	})
	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"run", "-run-dir", runDir, "-stop-after", run.LossProposerDir, lampSourcePath(t)}, &stdout, &stderr, first); code != 1 {
		t.Fatalf("run: exit %d, want 1: %s", code, stderr.String())
	}
	folder := &run.Folder{Dir: runDir}
	if folder.Complete(run.ReadsAloneDir) || !folder.Complete(run.SupportCheckDir) {
		t.Fatalf("run did not stop in step 1.3b")
	}

	p := lampModel(t, runDir, nil)
	code, errText := resumeTo(t, runDir, run.LossProposerDir, p)
	if code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, errText)
	}
	// Step 1.3b calls each of the 4 segments, and step 1.5 the 3 with an
	// uncovered list twice.
	if n, rest := len(p.Calls()), len(stepCalls(p, run.ReadsAloneDir))+len(stepCalls(p, run.LossProposerDir)); n != rest || n != 4+6 {
		t.Errorf("%d calls, %d of them step 1.3b's and 1.5's, want those steps' alone (10)", n, rest)
	}
	usage := testfixture.Files(t, filepath.Join(runDir, run.ReadsAloneDir))[run.UsageFile]
	if n := strings.Count(usage, "\n"); n != 4+1 {
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
// 3's re-check, so the step stops in its re-check. The second makes no
// kept-loss call, and re-checks only the segment the first didn't finish:
// segment 2's re-check, called first, always finishes. The step's usage log
// holds both attempts' calls, and the final notes are those of a resume
// that didn't stop.
func TestResume_CallsOnlyTheUnfinishedSegments(t *testing.T) {
	runDir := lampAfterProposer(t)
	served := testfixture.Serve(t, keptLossCases()...)
	first := testfixture.NewFake(func(req provider.Request) (provider.Reply, error) {
		if strings.Contains(req.User, keptLossRecheck(3)) {
			return provider.Reply{}, errors.New("the call failed")
		}
		return served.Call(context.Background(), req)
	})
	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"resume", "-stop-after", run.FinalDir, runDir}, &stdout, &stderr, first); code == 0 {
		t.Fatal("resume succeeded with no reply for segment 3's re-check")
	}
	if (&run.Folder{Dir: runDir}).Complete(run.KeptLossDir) {
		t.Fatal("the kept-loss repair is complete after its re-check failed")
	}
	recheck := filepath.Join(runDir, run.KeptLossDir+run.PartialSuffix, run.KeptLossSupportDir+run.PartialSuffix)
	if _, err := os.Stat(filepath.Join(recheck, run.RequestsDir, "2.sha256")); err != nil {
		t.Fatalf("segment 2's re-check, called first, is not finished: %v", err)
	}
	if _, err := os.Stat(filepath.Join(recheck, run.RequestsDir, "3.sha256")); err == nil {
		t.Fatal("segment 3's re-check is finished, though its call failed")
	}

	second := testfixture.Serve(t, keptLossCases()...)
	stderr.Reset()
	if code := runCommand(context.Background(), []string{"resume", "-stop-after", run.FinalDir, runDir}, &stdout, &stderr, second); code != 0 {
		t.Fatalf("second resume: exit %d, want 0: %s", code, stderr.String())
	}
	if calls := second.Calls(); len(calls) != 1 || !strings.Contains(calls[0].User, keptLossRecheck(3)) {
		t.Errorf("second resume made %d calls, want only segment 3's unfinished re-check", len(calls))
	}

	b, err := os.ReadFile(filepath.Join(runDir, run.KeptLossDir, run.UsageFile))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Count(string(b), "\n"), len(first.Calls())+len(second.Calls()); got != want {
		t.Errorf("usage log holds %d calls, want both attempts' %d", got, want)
	}
	if got, want := testfixture.Files(t, filepath.Join(runDir, filepath.FromSlash(run.FinalNotes))), lampFinal(); !reflect.DeepEqual(got, want) {
		t.Errorf("final notes:\n%v\nwant:\n%v", got, want)
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

// Resuming Lamp's run with its stage 1 done runs step 2.1 on the final stage
// 1 notes, with the model -stage-2-model names, then its gates. Segment 3's
// reply loses a statement and its trace. A folder no segment claims, added
// by hand, stops the gates, and once it is gone they run again, with no
// call. The split check fails segment 3, which falls back to its final stage
// 1 notes under a hub of its own. Step 2.2 strips every segment's notes, and
// step 3.1 formats them. One reply changed a word of segment 3's, so the
// format check fails it, and it falls back again, to its stripped notes.
// Step 3.2 stages every segment's notes.
func TestResume_SplitsTheFinalNotes(t *testing.T) {
	// A resume that can reach step 3.2 checks the vault before it runs a
	// step, so the run has one from the start.
	vault := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(filepath.Join(vault, stageout.SlipBoxDir), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(VaultEnv, vault)
	runDir := lampAt(t, run.FinalDir)
	src := testfixture.LampSource(t)
	final := filepath.Join(runDir, filepath.FromSlash(run.FinalNotes))
	notes, err := note.ReadDir(final)
	if err != nil {
		t.Fatal(err)
	}
	splitFiles := testfixture.LampSplit()
	const lost = "3 Storms/In a Storm the Light Matters Most.md"
	splitFiles[lost] = strings.NewReplacer(
		"In a storm the light matters most.[^1]\n", "",
		"[^1]: "+testfixture.LampTrace("Storms", "In a storm the light matters most.")+"\n", "",
	).Replace(splitFiles[lost])
	if splitFiles[lost] == testfixture.LampSplit()[lost] {
		t.Fatalf("%s has no statement to lose", lost)
	}
	p := lampModel(t, runDir, lampReplies{run.SplitDir: {3: {splitReply(splitFiles, 3)}}})

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
	var wantSplit []string
	for rel := range testfixture.LampSplit() {
		if strings.Contains(rel, "/") && strings.HasSuffix(rel, ".md") {
			wantSplit = append(wantSplit, rel)
		}
	}
	slices.Sort(wantSplit)
	if got := keys(testfixture.Files(t, splitNotes)); !slices.Equal(got, wantSplit) {
		t.Errorf("step 2.1 wrote %q, want each segment's notes as its reply gave them, %q", got, wantSplit)
	}

	// A folder no segment claims stops the gates, with exit status 4, and
	// says what to do.
	extra := filepath.Join(splitNotes, "99 Extra")
	if err := os.Mkdir(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	again := testfixture.Serve(t)
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
	const changed = "3 Storms/3 Storms.md"
	var formatted []*note.Note
	for _, f := range folders {
		if f.Name != split.Folder(src.Segments[3]) {
			continue
		}
		for _, n := range f.Notes {
			text := n.Render()
			if f.Name+"/"+n.Name == changed {
				if !strings.Contains(text, " every gale.") {
					t.Fatalf("%s has no word to change", changed)
				}
				text = strings.Replace(text, " every gale.", " every storm.", 1)
			}
			formatted = append(formatted, note.Parse(n.Name, text))
		}
	}
	p = lampModel(t, runDir, lampReplies{run.FormatDir: {3: {fileBlocks(formatted) + "=== MANIFEST ===\n{}\n"}}})
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
		if n, _ := segmentOf(c); c.User != format.Prompt(folders[n]) {
			t.Errorf("step 3.1's call for segment %d is not given its stripped notes:\n%s", n, c.User)
		}
	}
	if !folder.Complete(run.FormatGateDir) {
		t.Fatalf("step 3.1's gate is not complete")
	}
	if items, err := fallback.Read(folder, run.FormatGateDir); err != nil || !reflect.DeepEqual(items, []fallback.Item{{Segment: 3, Why: "the format check", Report: run.FormatCheckFile}}) {
		t.Errorf("fallbacks %+v (err %v), want segment 3's, for the format check", items, err)
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
		"- 2.1-gates, segment 3 (Storms): fell back to its stage 1 notes: the split check failed it; its report is split-check.txt\n",
		"- 3.1-gate, segment 3 (Storms): kept its notes unformatted: the format check failed it; its report is format-check.txt\n",
	} {
		if !strings.Contains(report, w) {
			t.Errorf("the staging folder's %s has no %q:\n%s", stageout.RunReportFile, w, report)
		}
	}
	delete(got, stageout.RunReportFile)
	if !reflect.DeepEqual(got, wantStaged) {
		t.Errorf("staged %d notes, want the %d the gate passed on", len(got), len(wantStaged))
	}
	// The vault's notes reach step 3.2, which lists the hub as like one.
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
	runDir := lampAfterProposer(t)
	if code, stderr := resumeTo(t, runDir, run.FinalDir, testfixture.Serve(t, keptLossCases()...)); code != 0 {
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
	if code, stderr := resume(t, runDir, testfixture.Serve(t)); code != 0 {
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
	runDir := lampAfterProposer(t)
	cp := filepath.Join(runDir, oldCheckpointDir)
	if err := os.Mkdir(cp, 0o755); err != nil {
		t.Fatal(err)
	}
	code, stderr := resume(t, runDir, testfixture.Serve(t))
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

// A run folder stopped in step 2.1's gates, with every file the gates write
// in their partial directory, resumes through all three of the gates with no
// model call, and they write what they write in a run that didn't stop.
func TestResume_PassesTheSplitGates(t *testing.T) {
	straight := filepath.Join(lampAt(t, run.SplitGatesDir), run.SplitGatesDir)
	runDir := lampAt(t, run.SplitDir)
	if err := os.CopyFS(filepath.Join(runDir, run.SplitGatesDir+run.PartialSuffix), os.DirFS(straight)); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	p := testfixture.Serve(t)
	if code := runCommand(context.Background(), []string{"resume", "-stop-after", run.SplitGatesDir, runDir}, &stdout, &stderr, p); code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, stderr.String())
	}
	if calls := p.Calls(); len(calls) != 0 {
		t.Errorf("%d model calls, want none", len(calls))
	}
	if !(&run.Folder{Dir: runDir}).Complete(run.SplitGatesDir) {
		t.Fatal("step 2.1's gates are not complete")
	}
	if got, want := testfixture.Files(t, filepath.Join(runDir, run.SplitGatesDir)), testfixture.Files(t, straight); !reflect.DeepEqual(got, want) {
		t.Errorf("step 2.1's gates wrote %q, not what they write in a run that didn't stop, %q", keys(got), keys(want))
	}
}
