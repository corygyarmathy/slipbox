// Package keptloss is the kept-loss repair, the step after step 1.5
// (ADR 0008). Every loss step 1.5 proposed is
// kept: the losses go through a repair call of their own, once per segment
// with any, as new traced statements. Steps 1.2 and 1.3a then re-check the
// result, and what they find goes to the run report. Nothing in the step
// stops the run (ADR 0009): the final stage 1 notes drop a broken or
// untraced statement (WriteFinal).
//
// The call has step 1.4's shape (package repair): its brief is
// briefs.KeptLoss, a blank line, then briefs.Stage1, and the frame follows.
// The user prompt gives the segment's repaired notes, then its kept losses,
// each with every run's wording. The reply may rewrite the segment's existing
// notes, and nothing else. Its shape is repair.Reply's, as step 1.4's is:
// whole notes, or edits briefed by briefs.KeptLossEdits, which may only add
// statements. Edits can be written quote-once, as step 1.4's can, briefed by
// briefs.KeptLossEditsQuoteOnce.
package keptloss

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/corygyarmathy/slipbox/internal/briefs"
	"github.com/corygyarmathy/slipbox/internal/checker"
	"github.com/corygyarmathy/slipbox/internal/dropped"
	"github.com/corygyarmathy/slipbox/internal/lossproposer"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/modelstep"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/quoteonce"
	"github.com/corygyarmathy/slipbox/internal/repair"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/supportcheck"
)

// Effort is the kept-loss call's effort level: medium, what every recorded run
// used. Not trialled; pinned with step 1.1's (ADR 0004).
const Effort = "medium"

// FirstCallRate is the step's predicted cost in dollars per prompt character
// before it has a finished call (modelstep.Spec.FirstCallRate): the kept-loss
// repair's first call in Ch 4's quote-once run (ADR 0006), at Opus, cost
// $0.385 for a 120,874-character prompt.
const FirstCallRate = 3.18e-6

// Kept is a kept loss: every loss step 1.5 proposed.
type Kept struct {
	// Segment is the loss's segment, and Number its k, counting from 1 in
	// the segment's union order.
	Segment int `json:"segment"`
	Number  int `json:"number"`
	lossproposer.Loss
}

// ID is the loss's `<segment>.<k>`.
func (k Kept) ID() string {
	return strconv.Itoa(k.Segment) + "." + strconv.Itoa(k.Number)
}

// Losses gives every loss in step 1.5's union in folder, in its order.
func Losses(folder *run.Folder) ([]Kept, error) {
	p := filepath.Join(folder.Dir, run.LossProposerDir, run.ProposedLossesFile)
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var segs []lossproposer.Segment
	if err := json.Unmarshal(b, &segs); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	var kept []Kept
	for _, seg := range segs {
		for k, l := range seg.Losses {
			kept = append(kept, Kept{Segment: seg.Segment, Number: k + 1, Loss: l})
		}
	}
	return kept, nil
}

// Wordings writes each run's wording of l: a blank line, then
// `Run <k>: <idea>`, its quotes and its why.
func Wordings(b *strings.Builder, l lossproposer.Loss) {
	for _, w := range l.Wordings {
		fmt.Fprintf(b, "\nRun %d: %s\n", w.Run, w.Idea)
		for _, q := range w.Quotes {
			fmt.Fprintf(b, "- Quote: \"%s\"\n", q)
		}
		if w.Why != "" {
			fmt.Fprintf(b, "- Why: %s\n", w.Why)
		}
	}
}

// Brief is the kept-loss call's brief for reply: the kept-loss brief, a blank
// line, then the stage 1 rules, and file is the kept-loss brief's file. The
// runner puts the frame after it. With quoteOnce, Edits replies are briefed
// to write their statements quote-once, as step 1.4's are (repair.Brief).
func Brief(reply repair.Reply, quoteOnce bool) (file, text string) {
	switch {
	case reply == repair.Edits && quoteOnce:
		return briefs.KeptLossEditsQuoteOnceFile, briefs.KeptLossEditsQuoteOnce + "\n\n" + briefs.Stage1QuoteOnce
	case reply == repair.Edits:
		return briefs.KeptLossEditsFile, briefs.KeptLossEdits + "\n\n" + briefs.Stage1
	}
	return briefs.KeptLossFile, briefs.KeptLoss + "\n\n" + briefs.Stage1
}

// Prompt is seg's user prompt: seg's notes, as step 1.4's prompt gives them,
// then its kept losses, each under its id with every run's wording
// (Wordings). notes are every stage 1 note, in name order.
func Prompt(notes []*note.Note, seg source.Segment, kept []Kept) string {
	var b strings.Builder
	repair.SegmentNotesBlock(&b, notes, seg)
	b.WriteString("=== KEPT LOSSES ===\n")
	for i, k := range kept {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "Loss %s\n", k.ID())
		Wordings(&b, k.Loss)
	}
	b.WriteString("=== END KEPT LOSSES ===\n")
	return b.String()
}

// Run runs the kept-loss repair with model, briefed for reply and quoteOnce
// (s2n then traces what the reply wrote quote-once): one call for each
// segment of src with a proposed loss, on step 1.4's repaired notes. It
// writes the step's directory: the kept losses, every note under `stage-1/`,
// a changed one as the reply gives it, and each call's report and raw reply.
// A block or edit of a reply that can't be applied is dropped, as step 1.4
// drops one, and so is one that deletes a note, adds one or, for an edit,
// changes an existing statement.
//
// Then it re-checks, into the step's directory: step 1.2 on every note, and
// step 1.3a on the segments with a kept loss. What they find stops nothing.
// `still-fails.json` lists each statement new to its note with a step 1.3a
// grade that counts (repair.Counts), and each step 1.3a flag that matches no
// statement, since it may be on a new one. `dropped.json` lists what the step
// dropped, and each kept loss of a segment whose notes gained no statement.
func Run(ctx context.Context, p provider.Provider, src *source.Source, model string, reply repair.Reply, quoteOnce bool, folder *run.Folder) error {
	kept, err := Losses(folder)
	if err != nil {
		return fmt.Errorf("kept-loss repair: %w", err)
	}
	before, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(run.RepairNotes)))
	if err != nil {
		return fmt.Errorf("kept-loss repair: %w", err)
	}
	bySegment := map[int][]Kept{}
	var segments []int
	for _, k := range kept {
		if _, ok := bySegment[k.Segment]; !ok {
			segments = append(segments, k.Segment)
		}
		bySegment[k.Segment] = append(bySegment[k.Segment], k)
	}

	step, err := folder.Begin(run.KeptLossDir)
	if err != nil {
		return err
	}
	if kept == nil {
		kept = []Kept{}
	}
	if err := writeJSON(step, run.KeptLossesFile, kept); err != nil {
		return err
	}
	prompt := func(_ *source.Source, seg source.Segment) (string, bool) {
		k, ok := bySegment[seg.Number]
		if !ok {
			return "", false
		}
		return Prompt(before, seg, k), true
	}
	file, brief := Brief(reply, quoteOnce)
	results, err := modelstep.Run(ctx, p, src, modelstep.Spec{Brief: brief, BriefFile: file, Model: model, Effort: Effort, FirstCallRate: FirstCallRate, Prompt: prompt}, step)
	if err != nil {
		return fmt.Errorf("kept-loss repair: %w", err)
	}
	tracer := repair.Tracer(src, quoteOnce)

	notes := map[string]string{}
	for _, n := range before {
		notes[n.Name] = n.Render()
	}
	var drops []dropped.Item
	for _, r := range results {
		drops = append(drops, apply(notes, r.Segment, r.Files, tracer)...)
		if err := step.WriteFile(path.Join(run.ReportsDir, strconv.Itoa(r.Segment.Number)+".md"), []byte(r.Tail)); err != nil {
			return err
		}
	}
	for name, text := range notes {
		if err := step.WriteFile(path.Join(run.NotesDir, name), []byte(text)); err != nil {
			return err
		}
	}
	fails, unadded, err := recheck(ctx, p, src, step, before, segments, bySegment)
	if err != nil {
		return err
	}
	if fails == nil {
		fails = []repair.Entry{}
	}
	if err := writeJSON(step, run.StillFailsFile, fails); err != nil {
		return err
	}
	if err := dropped.Write(step, append(drops, unadded...)); err != nil {
		return err
	}
	return step.Commit()
}

// apply applies a reply's files for seg to notes, the notes' text by file
// name. A FILE block replaces its note, and an EDIT block makes its edits to
// it, which may only add statements. The call adds no note and deletes none,
// so a DELETE, or a name the segment's notes don't have, is dropped. It gives
// what it dropped. A non-nil tracer traces what the reply wrote quote-once.
func apply(notes map[string]string, seg source.Segment, files []modelstep.File, tracer *quoteonce.Tracer) []dropped.Item {
	prefix := checker.SegmentPrefix(seg)
	var drops []dropped.Item
	drop := func(f modelstep.File, why string) {
		drops = append(drops, dropped.Item{Segment: seg.Number, Note: f.Name, What: repair.Block(f), Why: why})
	}
	conflicted := repair.Conflicted(files)
	for _, f := range files {
		name, err := note.FileName(f.Name)
		if err != nil {
			drop(f, err.Error())
			continue
		}
		switch {
		case conflicted[name]:
			drop(f, "the reply both edits the note and gives it in full or deletes it")
			continue
		case f.Delete:
			drop(f, "the kept-loss call deletes no note")
			continue
		case !strings.HasPrefix(name, prefix):
			drop(f, "it is not one of the segment's notes")
			continue
		}
		if _, ok := notes[name]; !ok {
			drop(f, "it is not a note; the kept-loss call adds no note")
			continue
		}
		if f.Edit {
			text, edits, err := repair.EditNote(name, notes[name], f.Body, repair.EditTracer(tracer, seg), note.Insert, note.Paragraph)
			if err != nil {
				drop(f, err.Error())
				continue
			}
			notes[name] = text
			drops = append(drops, edits.Items(seg, name)...)
			continue
		}
		notes[name] = repair.TraceNote(tracer, seg, name, f.Body)
	}
	return drops
}

// recheck re-checks the notes step has written, and gives the statements new
// to their notes that step 1.3a still flags, and a dropped item for each kept
// loss of a segment whose notes gained no statement. before are the notes the
// call was given.
func recheck(ctx context.Context, p provider.Provider, src *source.Source, step *run.Step, before []*note.Note, segments []int, kept map[int][]Kept) ([]repair.Entry, []dropped.Item, error) {
	inner := &run.Folder{Dir: step.Dir}
	if err := mechcheck.RunOn(src, inner, run.NotesDir, run.KeptLossMechanicalDir); err != nil {
		return nil, nil, err
	}
	if len(segments) > 0 {
		support := supportcheck.Check()
		support.Dir, support.Notes, support.Segments = run.KeptLossSupportDir, run.NotesDir, segments
		err := checker.Run(ctx, p, src, inner, support)
		// The step's usage log holds every call it made, so a stopped attempt's
		// re-check calls are carried over with its repair calls.
		if uerr := moveUsage(step, run.KeptLossSupportDir); uerr != nil && err == nil {
			err = uerr
		}
		if err != nil {
			return nil, nil, err
		}
	}

	after, err := note.ReadDir(filepath.Join(step.Dir, run.NotesDir))
	if err != nil {
		return nil, nil, err
	}
	var fails []repair.Entry
	var unadded []dropped.Item
	for _, seg := range src.Segments {
		if !slices.Contains(segments, seg.Number) {
			continue
		}
		flags, err := readFlags(filepath.Join(step.Dir, run.KeptLossSupportDir, run.FlagsDir, strconv.Itoa(seg.Number)+".json"))
		if err != nil {
			return nil, nil, err
		}
		f, added := review(seg, before, after, flags)
		fails = append(fails, f...)
		if !added {
			for _, k := range kept[seg.Number] {
				unadded = append(unadded, dropped.Item{Segment: seg.Number, What: "loss " + k.ID(), Loss: k.ID(),
					Why: "the kept-loss call added no statement to the segment's notes; its report is " + path.Join(run.KeptLossDir, run.ReportsDir, strconv.Itoa(seg.Number)+".md")})
			}
		}
	}
	return fails, unadded, nil
}

// review gives seg's statements new to their notes that step 1.3a flags with
// a grade that counts, each with those flags alone, and each flag that
// matches no statement; and says whether seg's notes gained any statement.
// flags are seg's step 1.3a flags on the notes after the call.
func review(seg source.Segment, before, after []*note.Note, flags []checker.Flag) ([]repair.Entry, bool) {
	old, _ := checker.SegmentNotes(before, seg)
	mine, _ := checker.SegmentNotes(after, seg)
	existing := map[string]map[string]bool{}
	for _, n := range old {
		existing[n.Name] = map[string]bool{}
		for _, s := range n.Statements() {
			existing[n.Name][note.MatchForm(s.Text)] = true
		}
	}
	isNew := func(file, statement string) bool {
		return !existing[file][note.MatchForm(statement)]
	}
	added := false
	for _, n := range mine {
		for _, s := range n.Statements() {
			if isNew(n.Name, s.Text) {
				added = true
			}
		}
	}

	var out []repair.Entry
	for _, e := range repair.StillFails(mine, &mechcheck.Report{}, flags, nil) {
		if e.Statement != "" && !isNew(e.File, e.Statement) {
			continue
		}
		out = append(out, e)
	}
	return out, added
}

func readFlags(p string) ([]checker.Flag, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var r checker.Report
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return r.Flags, nil
}

// moveUsage appends the usage log of the step dir inside step, complete or
// partial, to step's own, and removes it.
func moveUsage(step *run.Step, dir string) error {
	for _, d := range []string{dir, dir + run.PartialSuffix} {
		p := filepath.Join(step.Dir, d, run.UsageFile)
		b, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if err := step.AppendFile(run.UsageFile, b); err != nil {
			return err
		}
		if err := os.Remove(p); err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(step *run.Step, rel string, v any) error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	return step.WriteFile(rel, b.Bytes())
}

// WriteFinal writes the final stage 1 notes, for stage 2, into their own step
// directory: the kept-loss repair's, with each statement its re-check of
// step 1.2 finds broken or untraced dropped with its traces (ADR 0009). A
// stage 2 split of such a statement would fail the split check. An aside
// left with no statement is deleted. `dropped.json` lists each statement and
// note dropped, with step 1.2's reason. A statement its note has more than
// once is dropped each time.
func WriteFinal(src *source.Source, folder *run.Folder) error {
	notes, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(run.KeptLossNotes)))
	if err != nil {
		return fmt.Errorf("final notes: %w", err)
	}
	mech, err := mechcheck.ReadFindings(folder, path.Join(run.KeptLossDir, run.KeptLossMechanicalDir))
	if err != nil {
		return fmt.Errorf("final notes: %w", err)
	}
	// bad are each note's statements to drop, one entry for all the copies
	// of a statement the note has more than once.
	bad := map[string][]repair.Entry{}
	seen := map[[2]string]int{}
	for _, e := range repair.StillFails(notes, mech, nil, nil) {
		if e.Statement == "" || !slices.ContainsFunc(e.Flags, brokenOrUntraced) {
			continue
		}
		k := [2]string{e.File, note.StatementKey(e.Statement)}
		if i, ok := seen[k]; ok {
			bad[e.File][i].Flags = append(bad[e.File][i].Flags, e.Flags...)
			continue
		}
		seen[k] = len(bad[e.File])
		bad[e.File] = append(bad[e.File], e)
	}
	step, err := folder.Begin(run.FinalDir)
	if err != nil {
		return err
	}
	var drops []dropped.Item
	for _, n := range notes {
		seg := segmentOf(src, n.Name)
		for _, e := range bad[n.Name] {
			out, err := n.Apply([]note.Edit{{Op: note.Replace, Statement: e.Statement, Every: true}})
			if err != nil {
				return fmt.Errorf("final notes: dropping a broken or untraced statement: %w", err)
			}
			n = out
			var why []string
			for _, f := range e.Flags {
				if brokenOrUntraced(f) && !slices.Contains(why, f.Reason) {
					why = append(why, f.Reason)
				}
			}
			drops = append(drops, dropped.Item{Segment: seg, Note: n.Name, What: e.Statement, Why: strings.Join(why, "; ")})
		}
		if len(bad[n.Name]) > 0 && note.IsAside(n.Name) && len(n.Statements()) == 0 {
			drops = append(drops, dropped.Item{Segment: seg, Note: n.Name, What: "the note", Why: "an aside left with no statement"})
			continue
		}
		if err := step.WriteFile(path.Join(run.NotesDir, n.Name), []byte(n.Render())); err != nil {
			return err
		}
	}
	if err := dropped.Write(step, drops); err != nil {
		return err
	}
	return step.Commit()
}

// brokenOrUntraced says whether f is step 1.2's flag of a broken trace or an
// untraced statement.
func brokenOrUntraced(f repair.Flag) bool {
	return f.Step == repair.StepMechanical && (slices.Contains(f.Grades, repair.GradeBroken) || slices.Contains(f.Grades, repair.GradeUntraced))
}

// segmentOf is the number of the segment of src whose note is named name, or
// -1 if none is.
func segmentOf(src *source.Source, name string) int {
	for _, seg := range src.Segments {
		if strings.HasPrefix(name, checker.SegmentPrefix(seg)) {
			return seg.Number
		}
	}
	return -1
}
