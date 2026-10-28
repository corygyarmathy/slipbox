package runreport_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/dropped"
	"github.com/corygyarmathy/slipbox/internal/fallback"
	"github.com/corygyarmathy/slipbox/internal/keptloss"
	"github.com/corygyarmathy/slipbox/internal/lossproposer"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/repair"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/runreport"
	"github.com/corygyarmathy/slipbox/internal/source"
)

// The report lists a loss as added unless the kept-loss repair dropped it,
// leaves out a still-failing statement's broken and untraced flags, and a
// statement whose note the final notes dropped, and gives each dropped item
// a line with its step.
func TestReport(t *testing.T) {
	loss := func(idea string) lossproposer.Loss {
		return lossproposer.Loss{Wordings: []lossproposer.Item{{Run: 1, Idea: idea}}}
	}
	in := runreport.Input{
		SourceName: "Tiny",
		RunDir:     "/runs/Tiny/x",
		Kept: []keptloss.Kept{
			{Segment: 0, Number: 1, Loss: loss("Beta is second.")},
			{Segment: 1, Number: 1, Loss: loss("Delta is fourth.")},
		},
		StillFails: []repair.Entry{
			{File: "0 A.md", Statement: "Alpha.[^1]", Flags: []repair.Flag{
				{Step: repair.StepMechanical, Grades: []string{repair.GradeBroken}, Reason: "BROKEN"},
				{Step: repair.StepSupport, Grades: []string{"weak"}, Reason: "thin"},
			}},
			{File: "0 A.md", Statement: "Gamma.", Flags: []repair.Flag{{Step: repair.StepMechanical, Grades: []string{repair.GradeUntraced}, Reason: "UNTRACED"}}},
			{File: "0 A - Aside - Gone.md", Statement: "Epsilon.[^1]", Flags: []repair.Flag{{Step: repair.StepReadsAlone, Grades: []string{"dangling"}, Reason: "which"}}},
		},
		Dropped: map[string][]dropped.Item{
			run.RepairDir:   {{Segment: 0, Note: "0 A.md", What: `the edit at "Zeta."`, Why: "no statement reads it"}},
			run.KeptLossDir: {{Segment: 1, What: "loss 1.1", Why: "the kept-loss call added no statement", Loss: "1.1"}},
			run.FinalDir:    {{Segment: 0, Note: "0 A.md", What: "Gamma.", Why: "UNTRACED"}},
		},
		Final: []*note.Note{note.Parse("0 A.md", "# 0 A\n"), note.Parse("0 A - Aside - Kept.md", "# 0 A - Aside - Kept\n")},
		Mech:  &mechcheck.Report{},
	}
	got := runreport.Report(in)
	for _, w := range []string{
		"## Losses added\n\n- Loss 0.1: Beta is second.\n\n## ",
		"## Statements that still fail\n\n- 0 A: Alpha.[^1] (1.3a weak: thin)\n\n## ",
		"## Dropped\n\n" +
			"- 1.4-repair, segment 0, `0 A.md`: the edit at \"Zeta.\": no statement reads it\n" +
			"- 1.4-kept-loss-repair, segment 1: loss 1.1: the kept-loss call added no statement\n" +
			"- stage-1-final, segment 0, `0 A.md`: Gamma.: UNTRACED\n",
		"- Asides: 1, in [0 A - Aside - Kept](<../stage-1-final/stage-1/0 A - Aside - Kept.md>)\n",
		"- Name spans that point outside their segment, author name spans included: 0\n",
	} {
		if !strings.Contains(got, w) {
			t.Errorf("report has no %q:\n%s", w, got)
		}
	}
}

// The report counts each final note's name spans that point outside their
// segment, step 1.2's cross-segment findings, and links each note that has
// any with its count. Another kind of finding isn't counted, and a note with
// none isn't listed.
func TestReport_NameSpansOutsideTheirSegment(t *testing.T) {
	in := runreport.Input{
		SourceName: "Tiny",
		RunDir:     "/runs/Tiny/x",
		Mech: &mechcheck.Report{Notes: []mechcheck.NoteReport{
			{Name: "0 A.md", Findings: []mechcheck.Finding{{Kind: mechcheck.CrossSegment}, {Kind: mechcheck.Fuzzy}, {Kind: mechcheck.CrossSegment}}},
			{Name: "1 B.md", Findings: []mechcheck.Finding{{Kind: mechcheck.Fuzzy}}},
			{Name: "2 C.md", Findings: []mechcheck.Finding{{Kind: mechcheck.CrossSegment}}},
		}},
	}
	got := runreport.Report(in)
	want := "- Name spans that point outside their segment, author name spans included: 3, in " +
		"[0 A](<../stage-1-final/stage-1/0 A.md>) (2), [2 C](<../stage-1-final/stage-1/2 C.md>) (1)\n"
	if !strings.Contains(got, want) {
		t.Errorf("report has no %q:\n%s", want, got)
	}
}

// The staged report names the notes, since the run folder its links point
// into is deleted once a run succeeds.
func TestReport_Staged(t *testing.T) {
	in := runreport.Input{
		SourceName: "Tiny",
		RunDir:     "/runs/Tiny/x",
		Final:      []*note.Note{note.Parse("0 A - Aside - Kept.md", "# 0 A - Aside - Kept\n")},
		Mech:       &mechcheck.Report{},
		Staged:     true,
	}
	got := runreport.Report(in)
	if want := "- Asides: 1, in 0 A - Aside - Kept\n"; !strings.Contains(got, want) {
		t.Errorf("staged report has no %q:\n%s", want, got)
	}
	if strings.Contains(got, "](") || strings.Contains(got, in.RunDir) {
		t.Errorf("staged report links into the run folder:\n%s", got)
	}
}

// Each segment that fell back has a line, stage 2's first, with the gate it
// failed and a link to its report, or the reply's fault and a link to the
// notes it goes on with. The staged copy names the report or notes, with no
// link.
func TestReport_Fallbacks(t *testing.T) {
	src, err := source.Parse("Tiny", "---\nsource: \"[[@Tiny - A. Author]]\"\n---\n# 1. Tiny\n\nAlpha.\n\n## Two\n\nGamma.\n")
	if err != nil {
		t.Fatal(err)
	}
	in := runreport.Input{
		SourceName: "Tiny",
		RunDir:     "/runs/Tiny/x",
		Mech:       &mechcheck.Report{},
		Segments:   src.Segments,
		Fallbacks: map[string][]fallback.Item{
			run.SplitGatesDir: {
				{Segment: 0, Why: `step 2.1's reply wrote "A.md" twice`},
				{Segment: 1, Why: "the split check", Report: run.SplitCheckFile},
			},
			run.FormatGateDir: {{Segment: 1, Why: "the format check", Report: run.FormatCheckFile}},
		},
	}
	got := runreport.Report(in)
	want := "## Fallbacks\n\n" +
		"- 2.1-gates, segment 0 (1. Tiny): fell back to its stage 1 notes: step 2.1's reply wrote \"A.md\" twice; its notes are in [2.1-gates/stage-2/0 Tiny](<../2.1-gates/stage-2/0 Tiny>)\n" +
		"- 2.1-gates, segment 1 (Two): fell back to its stage 1 notes: the split check failed it; its report is [split-check.txt](<../2.1-gates/split-check.txt>)\n" +
		"- 3.1-gate, segment 1 (Two): kept its notes unformatted: the format check failed it; its report is [format-check.txt](<../3.1-gate/format-check.txt>)\n\n## "
	if !strings.Contains(got, want) {
		t.Errorf("report has no %q:\n%s", want, got)
	}
	in.Staged = true
	got = runreport.Report(in)
	want = "## Fallbacks\n\n" +
		"- 2.1-gates, segment 0 (1. Tiny): fell back to its stage 1 notes: step 2.1's reply wrote \"A.md\" twice; its notes are in 2.1-gates/stage-2/0 Tiny\n" +
		"- 2.1-gates, segment 1 (Two): fell back to its stage 1 notes: the split check failed it; its report is split-check.txt\n" +
		"- 3.1-gate, segment 1 (Two): kept its notes unformatted: the format check failed it; its report is format-check.txt\n\n## "
	if !strings.Contains(got, want) {
		t.Errorf("staged report has no %q:\n%s", want, got)
	}
	if strings.Contains(got, "](") || strings.Contains(got, in.RunDir) {
		t.Errorf("staged report links into the run folder:\n%s", got)
	}
}

// A run with no fallback says so.
func TestReport_NoFallbacks(t *testing.T) {
	got := runreport.Report(runreport.Input{SourceName: "Tiny", RunDir: "/runs/Tiny/x", Mech: &mechcheck.Report{}})
	if want := "## Fallbacks\n\nNone.\n"; !strings.Contains(got, want) {
		t.Errorf("report has no %q:\n%s", want, got)
	}
}

// Step 1.5's remarks are listed under Dropped in step order, after step
// 1.4's and before the kept-loss repair's, in both the run folder's report
// and the staged copy. The proposer's free lines are in neither.
func TestRun_ListsRemarksAndNoFreeLines(t *testing.T) {
	src, err := source.Parse("Tiny", "---\nsource: \"[[@Tiny]]\"\n---\n## One\n\nAlpha is first.\n")
	if err != nil {
		t.Fatal(err)
	}
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	remark := lossproposer.Item{Run: 2, Idea: "Covered", Quotes: []string{"none uncovered"}, Text: "1. **Covered**\n   - Quote: none uncovered"}
	union, err := json.Marshal([]lossproposer.Segment{{
		Segment: 0, Heading: "One", Losses: []lossproposer.Loss{}, Remarks: []lossproposer.Item{remark},
		Other: []lossproposer.Line{{Run: 1, Text: "No losses proposed."}, {Run: 2, Text: "I left out the rest."}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		dir   string
		drops []dropped.Item
		files map[string]string
	}{
		{run.ExtractDir, []dropped.Item{{Segment: 0, What: "a DELETE", Why: "extract"}}, nil},
		{run.RepairDir, []dropped.Item{{Segment: 0, What: "an edit", Why: "repair"}}, nil},
		{run.LossProposerDir, []dropped.Item{{Segment: 0, What: lossproposer.Remark(remark), Why: lossproposer.RemarkWhy}}, map[string]string{run.ProposedLossesFile: string(union)}},
		{run.KeptLossDir, []dropped.Item{{Segment: 0, What: "loss 0.1", Why: "kept-loss"}}, nil},
		{run.FinalDir, nil, map[string]string{filepath.Join(run.NotesDir, "0 One.md"): "# 0 One\n"}},
	}
	for _, st := range steps {
		step, err := folder.Begin(st.dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := dropped.Write(step, st.drops); err != nil {
			t.Fatal(err)
		}
		for name, text := range st.files {
			if err := step.WriteFile(name, []byte(text)); err != nil {
				t.Fatal(err)
			}
		}
		if err := step.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	if err := runreport.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	want := "## Dropped\n\n" +
		"- 1.1-extract, segment 0: a DELETE: extract\n" +
		"- 1.4-repair, segment 0: an edit: repair\n" +
		"- 1.5-loss-proposer, segment 0: run 2's item: 1. **Covered** - Quote: none uncovered: " + lossproposer.RemarkWhy + "\n" +
		"- 1.4-kept-loss-repair, segment 0: loss 0.1: kept-loss\n\n## "
	for _, name := range []string{run.RunReportFile, run.StagedReportFile} {
		b, err := os.ReadFile(filepath.Join(folder.Dir, run.RunReportDir, name))
		if err != nil {
			t.Fatal(err)
		}
		got := string(b)
		if !strings.Contains(got, want) {
			t.Errorf("%s has no %q:\n%s", name, want, got)
		}
		for _, free := range []string{"No losses proposed.", "I left out the rest."} {
			if strings.Contains(got, free) {
				t.Errorf("%s lists the free line %q:\n%s", name, free, got)
			}
		}
	}
}
