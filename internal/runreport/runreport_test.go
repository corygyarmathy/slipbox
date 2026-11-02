package runreport_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/dropped"
	"github.com/corygyarmathy/slipbox/internal/fallback"
	"github.com/corygyarmathy/slipbox/internal/keptloss"
	"github.com/corygyarmathy/slipbox/internal/lossproposer"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/modelstep"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/repair"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/runreport"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
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
		{run.RepairDir, []dropped.Item{{Segment: 0, What: "an edit", Why: "repair"}}, map[string]string{filepath.Join(run.NotesDir, "0 One.md"): "# 0 One\n"}},
		{run.LossProposerDir, []dropped.Item{{Segment: 0, What: lossproposer.Remark(remark), Why: lossproposer.RemarkWhy}}, map[string]string{run.ProposedLossesFile: string(union)}},
		{run.KeptLossDir, []dropped.Item{{Segment: 0, What: "loss 0.1", Why: "kept-loss"}}, map[string]string{filepath.Join(run.NotesDir, "0 One.md"): "# 0 One\n"}},
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

// summaryInput is an invented run's input: two segments, two kept losses of
// which the kept-loss repair added one, a statement that still fails, two
// dropped items, one fallback in each of stages 2 and 3, and model steps
// at three tiers, the kept-loss repair's calls at two, which raised
// coverage.
func summaryInput(t *testing.T) runreport.Input {
	t.Helper()
	src, err := source.Parse("Tiny", "---\nsource: \"[[@Tiny - A. Author]]\"\n---\n# 1. Tiny\n\nAlpha is first. Beta is second.\n\n## Two\n\nGamma is third. Delta is fourth.\n")
	if err != nil {
		t.Fatal(err)
	}
	loss := func(idea string) lossproposer.Loss {
		return lossproposer.Loss{Wordings: []lossproposer.Item{{Run: 1, Idea: idea}}}
	}
	used := func(model, effort string, ids ...string) modelstep.Model {
		return modelstep.Model{Model: model, Effort: effort, Used: ids}
	}
	return runreport.Input{
		SourceName: "Tiny",
		RunDir:     "/runs/Tiny/x",
		Segments:   src.Segments,
		Kept: []keptloss.Kept{
			{Segment: 0, Number: 1, Loss: loss("Beta is second.")},
			{Segment: 1, Number: 1, Loss: loss("Delta is fourth.")},
		},
		StillFails: []repair.Entry{
			{File: "0 Tiny.md", Statement: "Alpha is first.[^1]", Flags: []repair.Flag{{Step: repair.StepSupport, Grades: []string{"weak"}, Reason: "thin"}}},
		},
		Dropped: map[string][]dropped.Item{
			run.RepairDir:   {{Segment: 0, Note: "0 Tiny.md", What: `the edit at "Zeta."`, Why: "no statement reads it"}},
			run.KeptLossDir: {{Segment: 1, What: "loss 1.1", Why: "the kept-loss call added no statement", Loss: "1.1"}},
		},
		Fallbacks: map[string][]fallback.Item{
			run.SplitGatesDir: {{Segment: 1, Why: "the split check", Report: run.SplitCheckFile}},
			run.FormatGateDir: {{Segment: 0, Why: `step 3.1's reply wrote "A.md" twice`}},
		},
		Final: []*note.Note{note.Parse("0 Tiny.md", "# 0 Tiny\n"), note.Parse("1 Two.md", "# 1 Two\n")},
		Mech:  &mechcheck.Report{},
		Steps: []modelstep.StepTotal{
			{Step: run.ExtractDir, Total: modelstep.Total{Calls: 2, CostUSD: 1.25, Seconds: 95.5},
				Models: []modelstep.Model{used("opus", "medium", "claude-haiku-5-5", "claude-opus-5-5")}},
			// The repair's one call failed, so its usage names no model.
			{Step: run.RepairDir, Total: modelstep.Total{Calls: 1, Seconds: 2},
				Models: []modelstep.Model{used("opus", "medium")}},
			{Step: run.LossProposerDir, Total: modelstep.Total{Calls: 4, CostUSD: 0.5, Seconds: 60},
				Models: []modelstep.Model{used("sonnet", "medium", "claude-sonnet-5-5")}},
			{Step: run.KeptLossDir, Total: modelstep.Total{Calls: 3, CostUSD: 0.75, Seconds: 70},
				Models: []modelstep.Model{used("opus", "medium", "claude-opus-5-5"), used("sonnet", "medium", "claude-sonnet-5-5")}},
			{Step: run.SplitDir, Total: modelstep.Total{Calls: 2, CostUSD: 0.375, Seconds: 40},
				Models: []modelstep.Model{used("sonnet", "medium", "claude-sonnet-5-5")}},
			{Step: run.FormatDir, Total: modelstep.Total{Calls: 3, CostUSD: 0.125, Seconds: 20.25},
				Models: []modelstep.Model{used("haiku", "medium", "claude-haiku-5-5")}},
		},
		CoverageBefore: mechcheck.CoverageTotals{Covered: 2, Total: 4},
		CoverageAfter:  mechcheck.CoverageTotals{Covered: 3, Total: 4},
	}
}

// The report opens with the summary: coverage before and after kept losses,
// each section's count, and a row per model step with its models, then the
// total row. The staged copy has the same summary.
func TestReport_Summary(t *testing.T) {
	in := summaryInput(t)
	testfixture.Golden(t, "report.md", runreport.Report(in))
	in.Staged = true
	testfixture.Golden(t, "staged-report.md", runreport.Report(in))
}

// Each of the summary's counts is the number of lines in its section, and
// the total adds up every step's.
func TestSummary_CountsTheSectionsLines(t *testing.T) {
	in := summaryInput(t)
	got := in.Summary()
	want := runreport.Summary{
		Steps:           in.Steps,
		Total:           modelstep.Total{Calls: 15, CostUSD: 3, Seconds: 287.75},
		CoverageBefore:  in.CoverageBefore,
		CoverageAfter:   in.CoverageAfter,
		LossesAdded:     1,
		StillFail:       1,
		Dropped:         2,
		Stage2Fallbacks: 1,
		Stage3Fallbacks: 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Summary =\n %+v\nwant\n %+v", got, want)
	}
	report := runreport.Report(in)
	for title, n := range map[string]int{
		"Losses added":               got.LossesAdded,
		"Statements that still fail": got.StillFail,
		"Dropped":                    got.Dropped,
		"Fallbacks":                  got.Stage2Fallbacks + got.Stage3Fallbacks,
	} {
		_, body, _ := strings.Cut(report, "\n## "+title+"\n\n")
		body, _, _ = strings.Cut(body, "\n## ")
		lines := 0
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "- ") {
				lines++
			}
		}
		if lines != n {
			t.Errorf("section %q has %d lines, its count is %d:\n%s", title, lines, n, body)
		}
	}
}

// A frame with no sentences to count gives coverage without a percentage.
func TestReport_SummaryWithNoSentencesToCount(t *testing.T) {
	got := runreport.Report(runreport.Input{SourceName: "Tiny", RunDir: "/runs/Tiny/x", Mech: &mechcheck.Report{}})
	if want := "- Covered sentences: 0/0 before kept losses, 0/0 after.\n"; !strings.Contains(got, want) {
		t.Errorf("report has no %q:\n%s", want, got)
	}
}

// commit writes files into the step directory dir in folder and commits it.
func commit(t *testing.T, folder *run.Folder, dir string, files map[string]string) {
	t.Helper()
	step, err := folder.Begin(dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range files {
		if err := step.WriteFile(name, []byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	if err := step.Commit(); err != nil {
		t.Fatal(err)
	}
}

// A run with no losses, still-fails, drops or fallbacks gives zero counts,
// and, with no kept loss to add, step 1.2 counts the same coverage before
// kept losses and after. Run works out the coverage from the repaired notes
// and the kept-loss repair's, and reads each model step's usage from its
// log, the steps in run order, the loss proposer's before the kept-loss
// repair's, in both the run folder's report and the staged copy.
func TestRun_SummaryOfAQuietRun(t *testing.T) {
	src, err := source.Parse("Tiny", "---\nsource: \"[[@Tiny]]\"\n---\n## One\n\nAlpha is first. Beta is second.\n")
	if err != nil {
		t.Fatal(err)
	}
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	notes := map[string]string{"0 One.md": "# 0 One\n\nAlpha is first.[^1]\n\n[^1]: [[Tiny#One]] \"Alpha is first.\"\n"}
	staged := func(files map[string]string) map[string]string {
		out := map[string]string{}
		for name, text := range files {
			out[filepath.Join(run.NotesDir, name)] = text
		}
		return out
	}
	commit(t, folder, run.RepairDir, staged(notes))
	commit(t, folder, run.LossProposerDir, map[string]string{
		run.ProposedLossesFile: "[]",
		run.UsageFile:          `{"step":"1.5-loss-proposer","run":1,"model":"sonnet","effort":"medium","cost_usd":0.25,"seconds":10,"models":{"claude-sonnet-5-5":{}}}` + "\n",
	})
	keptLoss := staged(notes)
	keptLoss[run.UsageFile] = `{"step":"1.4-kept-loss-repair","model":"opus","effort":"medium","cost_usd":1,"seconds":30,"models":{"claude-opus-5-5":{}}}` + "\n"
	commit(t, folder, run.KeptLossDir, keptLoss)
	commit(t, folder, run.FinalDir, staged(notes))
	if err := runreport.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	want := "## Summary\n\n" +
		"- Covered sentences: 1/2 (50%) before kept losses, 1/2 (50%) after.\n" +
		"- Losses added: 0. Statements that still fail: 0. Dropped: 0. Fallbacks: 0 in stage 2, 0 in stage 3.\n\n" +
		"| Step | Models | Calls | Cost | Seconds |\n|---|---|---|---|---|\n" +
		"| 1.5-loss-proposer | sonnet (medium): claude-sonnet-5-5 | 1 | $0.25 | 10 |\n" +
		"| 1.4-kept-loss-repair | opus (medium): claude-opus-5-5 | 1 | $1.00 | 30 |\n" +
		"| Total |  | 2 | $1.25 | 40 |\n"
	for _, name := range []string{run.RunReportFile, run.StagedReportFile} {
		b, err := os.ReadFile(filepath.Join(folder.Dir, run.RunReportDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if got := string(b); !strings.Contains(got, want) {
			t.Errorf("%s has no %q:\n%s", name, want, got)
		}
	}
}
