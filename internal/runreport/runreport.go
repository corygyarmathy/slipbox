// Package runreport writes the run report, which replaced the checkpoint's
// review packet (ADR 0009). It is written
// once step 3.1's gate is complete, so it covers the whole run. It is kept
// minimal, since no one is expected to read it closely, and nothing in it
// needs action. It opens with a summary of the run (Summary):
//
//   - Covered sentences: the frame's covered sentences out of its total,
//     with the percentage, before kept losses, as step 1.2 counts them on
//     the repaired notes, and after, as it counts them on the kept-loss
//     repair's notes. A frame with no sentences to count has no percentage.
//   - Counts: losses added, statements that still fail, items dropped, and
//     segments that fell back in stages 2 and 3, each the number of lines
//     in its section below.
//   - A table with a row for each step whose directory holds a usage log,
//     in run order: the models and effort levels its calls asked for, each
//     with the full model ids their usage names, its calls, their cost in
//     dollars at API rates and their seconds added up, then a total row,
//     the run's whole total that the final terminal line prints, worked
//     out from the steps' unrounded figures. A step's
//     row counts every call its log holds, so the kept-loss repair's
//     includes its re-check's step 1.3a calls, and the loss proposer's both
//     its runs. The seconds are the calls' time added up, more than the run
//     took when calls ran in parallel.
//
// The summary is a value, so other records of a run can take it as it is:
// the run log appends it, read again from the run folder (ReadSummary).
// Five sections follow it, with one line per item:
//
//  1. Losses added: each kept loss the kept-loss call added a statement for.
//  2. Statements that still fail: step 1.4's still-fails list and the
//     kept-loss repair's, each statement with its grades and reasons. Step
//     1.2's broken and untraced flags are left out, since the final notes
//     dropped those statements, and section 3 lists them.
//  3. Dropped: each item a stage 1 step dropped instead of stopping the run,
//     and why (package dropped), in step order, step 1.5's remarks included.
//     A run's free lines, such as "No losses proposed.", are left out.
//  4. Fallbacks: each segment that fell back in stage 2 or 3 to the notes
//     the stage before gave it (package fallback), and why, with a link to
//     the report of the gate it failed or to its notes, which the staged
//     copy names instead, stage 2's first.
//  5. Asides and name spans: the asides, and the name spans that point
//     outside their segment, author name spans included, each counted, with
//     links to the final notes that have them.
//
// The links are relative to the report's place in the run folder. The copy
// step 3.2 stages, run.StagedReportFile, names the notes instead, since a
// successful run deletes the run folder.
package runreport

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/corygyarmathy/slipbox/internal/dropped"
	"github.com/corygyarmathy/slipbox/internal/fallback"
	"github.com/corygyarmathy/slipbox/internal/keptloss"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/modelstep"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/repair"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
)

// droppingSteps are the steps that drop items, in run order.
var droppingSteps = []string{run.ExtractDir, run.RepairDir, run.LossProposerDir, run.KeptLossDir, run.FinalDir}

// modelSteps are the steps that make model calls, in run order, the order
// of the summary's table.
var modelSteps = []string{
	run.ExtractDir, run.SupportCheckDir, run.ReadsAloneDir, run.RepairDir, run.SupportRecheckDir, run.ReadsAloneRecheckDir,
	run.LossProposerDir, run.KeptLossDir, run.SplitDir, run.FormatDir,
}

// fallbackSteps are the gate steps of stages 2 and 3, in run order, each with
// what its fallbacks go on with and where it writes them. Each lists its
// model step's fallbacks with its own.
var fallbackSteps = []fallbackStep{
	{run.SplitGatesDir, "fell back to its stage 1 notes", run.SplitOrFallbackNotes},
	{run.FormatGateDir, "kept its notes unformatted", run.FormattedOrFallbackNotes},
}

type fallbackStep struct {
	dir, what, notes string
}

// Input is what the report is made from.
type Input struct {
	SourceName string
	// RunDir is the run folder's path.
	RunDir string
	// Kept are the kept losses, every loss step 1.5 proposed.
	Kept []keptloss.Kept
	// StillFails are step 1.4's still-fails list, then the kept-loss
	// repair's.
	StillFails []repair.Entry
	// Dropped are each dropping step's items, by its directory name.
	Dropped map[string][]dropped.Item
	// Fallbacks are the segments that fell back in stage 2 or 3, by the
	// directory name of the stage's gate step, and Segments the source's
	// segments.
	Fallbacks map[string][]fallback.Item
	Segments  []source.Segment
	// Final are the final stage 1 notes, and Mech step 1.2's findings on
	// them.
	Final []*note.Note
	Mech  *mechcheck.Report
	// Steps are each model step's usage, in run order.
	Steps []modelstep.StepTotal
	// CoverageBefore is step 1.2's coverage on the repaired notes, before
	// kept losses, and CoverageAfter its coverage on the kept-loss repair's.
	CoverageBefore, CoverageAfter mechcheck.CoverageTotals
	// Staged names the notes without links, for the copy step 3.2 stages.
	Staged bool
}

// Run writes the run report into its step directory in folder, from the
// stage 1 steps' output, the stage 2 and 3 steps' fallbacks and the model
// steps' usage logs.
func Run(src *source.Source, folder *run.Folder) error {
	in, err := read(src, folder)
	if err != nil {
		return fmt.Errorf("run report: %w", err)
	}
	step, err := folder.Begin(run.RunReportDir)
	if err != nil {
		return err
	}
	if err := step.WriteFile(run.RunReportFile, []byte(Report(in))); err != nil {
		return err
	}
	in.Staged = true
	if err := step.WriteFile(run.StagedReportFile, []byte(Report(in))); err != nil {
		return err
	}
	return step.Commit()
}

// ReadSummary gives the summary of folder's run, as Run works it out, from
// the run folder rather than from the report, so that the run log takes the
// value itself.
func ReadSummary(src *source.Source, folder *run.Folder) (Summary, error) {
	in, err := read(src, folder)
	if err != nil {
		return Summary{}, fmt.Errorf("run report: %w", err)
	}
	return in.Summary(), nil
}

func read(src *source.Source, folder *run.Folder) (Input, error) {
	in := Input{SourceName: src.Name, RunDir: folder.Dir, Dropped: map[string][]dropped.Item{}, Fallbacks: map[string][]fallback.Item{}, Segments: src.Segments}
	var err error
	if in.Kept, err = keptloss.Losses(folder); err != nil {
		return in, err
	}
	for _, dir := range []string{run.StillFailsDir, run.KeptLossDir} {
		es, err := repair.ReadStillFails(folder, dir)
		if err != nil {
			return in, err
		}
		in.StillFails = append(in.StillFails, es...)
	}
	for _, dir := range droppingSteps {
		if in.Dropped[dir], err = dropped.Read(folder, dir); err != nil {
			return in, err
		}
	}
	for _, st := range fallbackSteps {
		if in.Fallbacks[st.dir], err = fallback.Read(folder, st.dir); err != nil {
			return in, err
		}
	}
	if in.Final, err = note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes))); err != nil {
		return in, err
	}
	in.Mech = mechcheck.Check(src, in.Final)
	if in.Steps, err = modelstep.StepTotals(folder.Dir); err != nil {
		return in, err
	}
	// A step not in modelSteps goes last, so the table still adds up.
	order := func(st modelstep.StepTotal) int {
		if i := slices.Index(modelSteps, st.Step); i >= 0 {
			return i
		}
		return len(modelSteps)
	}
	slices.SortStableFunc(in.Steps, func(a, b modelstep.StepTotal) int { return order(a) - order(b) })
	if in.CoverageBefore, err = coverage(src, folder, run.RepairNotes); err != nil {
		return in, err
	}
	if in.CoverageAfter, err = coverage(src, folder, run.KeptLossNotes); err != nil {
		return in, err
	}
	return in, nil
}

// coverage gives step 1.2's coverage totals on the notes in the run folder's
// directory dir, slash-separated. The check is cheap and gives the same
// totals each time, so the report works them out from the notes the two
// re-checks read rather than from a file of theirs, which a run folder made
// before the summary would lack.
func coverage(src *source.Source, folder *run.Folder, dir string) (mechcheck.CoverageTotals, error) {
	notes, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(dir)))
	if err != nil {
		return mechcheck.CoverageTotals{}, err
	}
	return mechcheck.Check(src, notes).Coverage.Totals(), nil
}

// Summary is the run report's opening section as a value: the models,
// coverage, counts, and cost and time per step. Each count is the number of
// lines in its section of the report.
type Summary struct {
	// Steps are each model step's usage, in run order, and Total all of
	// theirs added up.
	Steps []modelstep.StepTotal `json:"steps"`
	Total modelstep.Total       `json:"total"`
	// CoverageBefore is the frame's covered sentences before kept losses,
	// and CoverageAfter after them.
	CoverageBefore mechcheck.CoverageTotals `json:"coverage_before"`
	CoverageAfter  mechcheck.CoverageTotals `json:"coverage_after"`
	LossesAdded    int                      `json:"losses_added"`
	StillFail      int                      `json:"still_fail"`
	Dropped        int                      `json:"dropped"`
	// Stage2Fallbacks and Stage3Fallbacks are the segments that fell back
	// in each stage.
	Stage2Fallbacks int `json:"stage_2_fallbacks"`
	Stage3Fallbacks int `json:"stage_3_fallbacks"`
}

// Summary gives the run's summary.
func (in Input) Summary() Summary {
	s := Summary{
		Steps:          in.Steps,
		CoverageBefore: in.CoverageBefore,
		CoverageAfter:  in.CoverageAfter,
		LossesAdded:    len(in.lossesAdded()),
		StillFail:      len(in.stillFails()),
		Dropped:        len(in.dropped()),
	}
	for _, st := range in.Steps {
		s.Total.Add(st.Total)
	}
	for _, st := range fallbackSteps {
		switch n := len(in.fallbacks(st)); st.dir {
		case run.SplitGatesDir:
			s.Stage2Fallbacks = n
		case run.FormatGateDir:
			s.Stage3Fallbacks = n
		}
	}
	return s
}

// Report renders the run report.
func Report(in Input) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Run report: %s\n\nWhat the run's model calls used, then what s2n added, dropped and still flags in stage 1, and the segments that fell back in stages 2 and 3. ", in.SourceName)
	if in.Staged {
		b.WriteString("Notes are named as stage 1 left them.\n")
	} else {
		fmt.Fprintf(&b, "Links are to the run folder, %s.\n", code(in.RunDir))
	}

	summary(&b, in.Summary())
	lines(&b, "Losses added", in.lossesAdded())
	lines(&b, "Statements that still fail", in.stillFails())
	lines(&b, "Dropped", in.dropped())
	var fallbacks []string
	for _, st := range fallbackSteps {
		fallbacks = append(fallbacks, in.fallbacks(st)...)
	}
	lines(&b, "Fallbacks", fallbacks)

	section(&b, "Asides and name spans")
	var asides []string
	for _, nt := range in.Final {
		if note.IsAside(nt.Name) {
			asides = append(asides, in.link(nt.Name))
		}
	}
	fmt.Fprintf(&b, "- Asides: %d%s\n", len(asides), list(asides))
	spans, total := []string{}, 0
	for _, nr := range in.Mech.Notes {
		k := 0
		for _, f := range nr.Findings {
			if f.Kind == mechcheck.CrossSegment {
				k++
			}
		}
		if k > 0 {
			spans = append(spans, in.link(nr.Name)+" ("+strconv.Itoa(k)+")")
			total += k
		}
	}
	fmt.Fprintf(&b, "- Name spans that point outside their segment, author name spans included: %d%s\n", total, list(spans))
	return b.String()
}

// summary renders the summary section: coverage and counts, then a row per
// model step with its models, and a total row.
func summary(b *strings.Builder, s Summary) {
	section(b, "Summary")
	fmt.Fprintf(b, "- Covered sentences: %s before kept losses, %s after.\n", s.CoverageBefore, s.CoverageAfter)
	fmt.Fprintf(b, "- Losses added: %d. Statements that still fail: %d. Dropped: %d. Fallbacks: %d in stage 2, %d in stage 3.\n",
		s.LossesAdded, s.StillFail, s.Dropped, s.Stage2Fallbacks, s.Stage3Fallbacks)
	b.WriteString("\n| Step | Models | Calls | Cost | Seconds |\n|---|---|---|---|---|\n")
	row := func(step, models string, t modelstep.Total) {
		fmt.Fprintf(b, "| %s | %s | %d | $%.2f | %.0f |\n", step, models, t.Calls, t.CostUSD, t.Seconds)
	}
	for _, st := range s.Steps {
		var models []string
		for _, m := range st.Models {
			model := fmt.Sprintf("%s (%s)", m.Model, m.Effort)
			if len(m.Used) > 0 {
				model += ": " + strings.Join(m.Used, ", ")
			}
			models = append(models, model)
		}
		row(st.Step, strings.Join(models, "; "), st.Total)
	}
	row("Total", "", s.Total)
	b.WriteString("\nCost is in dollars at API rates. Seconds are the calls' time added up, more than the run took when calls ran in parallel. The total row adds up the steps' unrounded figures, so it can differ from the rows above by a cent or a second.\n")
}

// lossesAdded gives a line for each kept loss the kept-loss call added a
// statement for.
func (in Input) lossesAdded() []string {
	unadded := map[string]bool{}
	for _, d := range in.Dropped[run.KeptLossDir] {
		if d.Loss != "" {
			unadded[d.Loss] = true
		}
	}
	var out []string
	for _, k := range in.Kept {
		if unadded[k.ID()] || len(k.Wordings) == 0 {
			continue
		}
		out = append(out, fmt.Sprintf("Loss %s: %s", k.ID(), k.Wordings[0].Idea))
	}
	return out
}

// stillFails gives a line for each statement that still fails, in a final
// note, with a flag other than step 1.2's broken and untraced.
func (in Input) stillFails() []string {
	final := map[string]bool{}
	for _, nt := range in.Final {
		final[nt.Name] = true
	}
	var out []string
	for _, e := range in.StillFails {
		if e.File != "" && !final[e.File] {
			continue
		}
		var flags []string
		for _, f := range e.Flags {
			if f.Step == repair.StepMechanical && (slices.Contains(f.Grades, repair.GradeBroken) || slices.Contains(f.Grades, repair.GradeUntraced)) {
				continue
			}
			flags = append(flags, fmt.Sprintf("%s %s: %s", f.Step, strings.Join(f.Grades, ", "), f.Reason))
		}
		if len(flags) == 0 {
			continue
		}
		what := e.Statement
		if what == "" {
			what = "no statement matched the checker's quote " + code(e.Quote)
		}
		out = append(out, fmt.Sprintf("%s: %s (%s)", title(e.File), what, strings.Join(flags, "; ")))
	}
	return out
}

// dropped gives a line for each dropped item, in step order.
func (in Input) dropped() []string {
	var out []string
	for _, dir := range droppingSteps {
		for _, d := range in.Dropped[dir] {
			line := fmt.Sprintf("%s, segment %d", dir, d.Segment)
			if d.Note != "" {
				line += ", " + code(d.Note)
			}
			out = append(out, line+fmt.Sprintf(": %s: %s", d.What, d.Why))
		}
	}
	return out
}

// fallbacks gives a line for each segment that fell back at the gate step
// st.
func (in Input) fallbacks(st fallbackStep) []string {
	var out []string
	for _, f := range in.Fallbacks[st.dir] {
		seg := "segment " + strconv.Itoa(f.Segment)
		folder := ""
		for _, sg := range in.Segments {
			if sg.Number == f.Segment {
				seg, folder = sg.String(), split.Folder(sg)
			}
		}
		why := f.Why
		if f.Report != "" {
			why += " failed it"
		}
		line := fmt.Sprintf("%s, %s: %s: %s", st.dir, seg, st.what, why)
		var where, name, target string
		switch {
		case f.Report != "":
			where, name, target = "its report is", f.Report, path.Join("..", st.dir, f.Report)
		case folder != "":
			where, name, target = "its notes are in", path.Join(st.notes, folder), path.Join("..", st.notes, folder)
		}
		switch {
		case name == "":
		case in.Staged:
			line += fmt.Sprintf("; %s %s", where, name)
		default:
			line += fmt.Sprintf("; %s [%s](<%s>)", where, name, target)
		}
		out = append(out, line)
	}
	return out
}

// lines renders a section of one line per item, or "None.".
func lines(b *strings.Builder, title string, items []string) {
	section(b, title)
	for _, l := range items {
		fmt.Fprintf(b, "- %s\n", l)
	}
	if len(items) == 0 {
		b.WriteString("None.\n")
	}
}

func section(b *strings.Builder, title string) {
	fmt.Fprintf(b, "\n## %s\n\n", title)
}

// list is items after a count: ", in " and the items, or "" for none.
func list(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return ", in " + strings.Join(items, ", ")
}

// link is a Markdown link to the final stage 1 note named name, from the
// report's directory, or for a staged report the note's name.
func (in Input) link(name string) string {
	if in.Staged {
		return title(name)
	}
	return "[" + title(name) + "](<" + path.Join("..", run.FinalNotes, name) + ">)"
}

// title is a note's file name without .md, or a phrase for "", a checker
// flag's H1 that names no note.
func title(name string) string {
	if name == "" {
		return "A note the checker named by a title the segment's notes don't have"
	}
	return strings.TrimSuffix(name, ".md")
}

// code is s as a Markdown code span.
func code(s string) string {
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}
