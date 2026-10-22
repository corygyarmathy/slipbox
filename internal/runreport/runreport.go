// Package runreport writes the run report, which replaced the checkpoint's
// review packet (ADR 0009). It is written
// once step 3.1's gate is complete, so it covers the whole run. It is kept
// minimal, since no one is expected to read it closely, and nothing in it
// needs action. It has one line per item, in five sections:
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
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/repair"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
)

// droppingSteps are the steps that drop items, in run order.
var droppingSteps = []string{run.ExtractDir, run.RepairDir, run.LossProposerDir, run.KeptLossDir, run.FinalDir}

// fallbackSteps are the gate steps of stages 2 and 3, in run order, each with
// what its fallbacks go on with and where it writes them. Each lists its
// model step's fallbacks with its own.
var fallbackSteps = []struct {
	dir, what, notes string
}{
	{run.SplitGatesDir, "fell back to its stage 1 notes", run.SplitOrFallbackNotes},
	{run.FormatGateDir, "kept its notes unformatted", run.FormattedOrFallbackNotes},
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
	// Staged names the notes without links, for the copy step 3.2 stages.
	Staged bool
}

// Run writes the run report into its step directory in folder, from the
// stage 1 steps' output and the stage 2 and 3 steps' fallbacks.
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
	return in, nil
}

// Report renders the run report.
func Report(in Input) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Run report: %s\n\nWhat s2n added, dropped and still flags in stage 1, and the segments that fell back in stages 2 and 3. ", in.SourceName)
	if in.Staged {
		b.WriteString("Notes are named as stage 1 left them.\n")
	} else {
		fmt.Fprintf(&b, "Links are to the run folder, %s.\n", code(in.RunDir))
	}

	unadded := map[string]bool{}
	for _, d := range in.Dropped[run.KeptLossDir] {
		if d.Loss != "" {
			unadded[d.Loss] = true
		}
	}
	section(&b, "Losses added")
	n := 0
	for _, k := range in.Kept {
		if unadded[k.ID()] || len(k.Wordings) == 0 {
			continue
		}
		fmt.Fprintf(&b, "- Loss %s: %s\n", k.ID(), k.Wordings[0].Idea)
		n++
	}
	none(&b, n)

	final := map[string]bool{}
	for _, nt := range in.Final {
		final[nt.Name] = true
	}
	section(&b, "Statements that still fail")
	n = 0
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
		fmt.Fprintf(&b, "- %s: %s (%s)\n", title(e.File), what, strings.Join(flags, "; "))
		n++
	}
	none(&b, n)

	section(&b, "Dropped")
	n = 0
	for _, dir := range droppingSteps {
		for _, d := range in.Dropped[dir] {
			fmt.Fprintf(&b, "- %s, segment %d", dir, d.Segment)
			if d.Note != "" {
				fmt.Fprintf(&b, ", %s", code(d.Note))
			}
			fmt.Fprintf(&b, ": %s: %s\n", d.What, d.Why)
			n++
		}
	}
	none(&b, n)

	section(&b, "Fallbacks")
	n = 0
	for _, st := range fallbackSteps {
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
			fmt.Fprintf(&b, "- %s, %s: %s: %s", st.dir, seg, st.what, why)
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
				fmt.Fprintf(&b, "; %s %s", where, name)
			default:
				fmt.Fprintf(&b, "; %s [%s](<%s>)", where, name, target)
			}
			b.WriteString("\n")
			n++
		}
	}
	none(&b, n)

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

func section(b *strings.Builder, title string) {
	fmt.Fprintf(b, "\n## %s\n\n", title)
}

func none(b *strings.Builder, n int) {
	if n == 0 {
		b.WriteString("None.\n")
	}
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
