// Package repair is step 1.4, the repair pass, then its re-check (ADR
// 0007). A segment with any flag that counts
// gets one repair call at stage 1's tier, and the call's reply replaces the
// notes it changed in a new copy of the notes. Steps 1.2 and 1.3 then run
// again on the segments it changed. There is no second call, whatever they
// find: what still fails is listed for the run report (stillfails.go). A
// block or edit of a reply that can't be applied is dropped, and listed for
// the run report too.
//
// The call is made as the repair trial made it (ADR 0007):
// its brief is briefs.Repair, a blank line, then briefs.Stage1, and its user
// prompt carries the checks' reports verbatim, withdrawn reports and repeated
// flags included. Parsed flags decide only which segments are called and
// what still fails.
//
// The reply's shape can be chosen (Reply): whole notes, as trialled, or edits
// to them, briefed by briefs.RepairEdits, the default (ADR 0007).
// Edits can also be written quote-once, briefed by briefs.RepairEditsQuoteOnce,
// the default (ADR 0006): the call writes its new statements with
// the joining text marked and no footnotes, and s2n writes their traces
// (package quoteonce).
package repair

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
	"sort"
	"strconv"
	"strings"

	"github.com/corygyarmathy/slipbox/internal/briefs"
	"github.com/corygyarmathy/slipbox/internal/checker"
	"github.com/corygyarmathy/slipbox/internal/dropped"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/modelstep"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/quoteonce"
	"github.com/corygyarmathy/slipbox/internal/readsalone"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/supportcheck"
)

// Effort is the repair call's effort level: medium, what every recorded run
// used. Not trialled; pinned with step 1.1's (ADR 0004).
const Effort = "medium"

// FirstCallRate is the step's predicted cost in dollars per prompt character
// before it has a finished call (modelstep.Spec.FirstCallRate): step 1.4's
// repair call's first call in Ch 4's quote-once run (ADR 0006), at Opus,
// cost $0.302 for a 91,049-character prompt.
const FirstCallRate = 3.31e-6

// Reply is the shape of a repair call's reply: the changed notes in full, or
// edits to them. It is shared by the kept-loss repair, whose call has the same
// shape.
type Reply string

const (
	// Whole replies give each changed note in full, `=== FILE: … ===`, as
	// the repair trial ran it.
	Whole Reply = "whole"
	// Edits replies give `=== EDIT: … ===` blocks of edits, each addressed by
	// a statement (note.ParseEdits), and s2n makes them through the note
	// model. They're the default (ADR 0007).
	Edits Reply = "edits"
)

// DefaultReply is the reply shape a run uses unless it is given another.
const DefaultReply = Edits

// Replies are the reply shapes, the default first.
var Replies = []Reply{Edits, Whole}

// Brief is the repair call's brief for reply: the repair brief, a blank line,
// then the stage 1 rules, and file is the repair brief's file. The runner
// puts the frame after it. For Whole replies it is the brief as trialled.
// With quoteOnce, Edits replies are briefed to write their statements
// quote-once, with the quote-once stage 1 rules. Whole replies have no
// quote-once brief, so they are briefed as without it.
func Brief(reply Reply, quoteOnce bool) (file, text string) {
	switch {
	case reply == Edits && quoteOnce:
		return briefs.RepairEditsQuoteOnceFile, briefs.RepairEditsQuoteOnce + "\n\n" + briefs.Stage1QuoteOnce
	case reply == Edits:
		return briefs.RepairEditsFile, briefs.RepairEdits + "\n\n" + briefs.Stage1
	}
	return briefs.RepairFile, briefs.Repair + "\n\n" + briefs.Stage1
}

// Tracer gives what traces a repair call's reply under quoteOnce: a
// quote-once tracer for src, or nil without quoteOnce, when replies are
// taken as written. It is shared by the kept-loss repair.
func Tracer(src *source.Source, quoteOnce bool) *quoteonce.Tracer {
	if !quoteOnce {
		return nil
	}
	return quoteonce.New(src)
}

// Counts says whether a step 1.3a grade counts for repair and the still-fails
// list. Step 1.3b governs reads alone, so 1.3a's own dangling and
// outside-frame grades don't (ADR 0007). Every step 1.3b grade counts.
func Counts(g checker.Grade) bool {
	return g != checker.Dangling && g != checker.OutsideFrame
}

// countedSupport gives flags, step 1.3a's, with only the grades that count,
// leaving out a flag with none.
func countedSupport(flags []checker.Flag) []checker.Flag {
	var out []checker.Flag
	for _, f := range flags {
		var gs []checker.Grade
		for _, g := range f.Grades {
			if Counts(g) {
				gs = append(gs, g)
			}
		}
		if len(gs) > 0 {
			f.Grades = gs
			out = append(out, f)
		}
	}
	return out
}

// Prompt is seg's user prompt, as trialled: seg's notes, then its
// sections of the mechanical check's report, then its step 1.3a and step 1.3b
// reports as received. notes are every stage 1 note, in name order, and mech
// is the mechanical check's report.
func Prompt(notes []*note.Note, seg source.Segment, mech, support, readsAlone string) string {
	var b strings.Builder
	SegmentNotesBlock(&b, notes, seg)
	fmt.Fprintf(&b, "=== MECHANICAL CHECK ===\n%s\n=== END MECHANICAL CHECK ===\n\n", mechBlock(mech, seg))
	fmt.Fprintf(&b, "=== SUPPORT AND CUTS CHECK ===\n%s\n=== END SUPPORT AND CUTS CHECK ===\n\n", support)
	fmt.Fprintf(&b, "=== READS-ALONE CHECK ===\n%s\n=== END READS-ALONE CHECK ===\n", readsAlone)
	return b.String()
}

// SegmentNotesBlock writes seg's own notes as the repair call's user prompt
// starts, as trialled: each under its title, between labelled
// lines, then a blank line. notes are every stage 1 note, in name order.
func SegmentNotesBlock(b *strings.Builder, notes []*note.Note, seg source.Segment) {
	mine, _ := checker.SegmentNotes(notes, seg)
	label := fmt.Sprintf("YOUR SEGMENT'S NOTES (segment %d: %s)", seg.Number, seg.Heading)
	fmt.Fprintf(b, "=== %s ===\n", label)
	for _, n := range mine {
		fmt.Fprintf(b, "--- NOTE: %s ---\n%s\n", strings.TrimSuffix(n.Name, ".md"), n.Render())
	}
	fmt.Fprintf(b, "=== END %s ===\n\n", label)
}

// mechBlock gives the mechanical check report's sections
// for seg's notes, before any coverage section, joined and stripped, or
// "No lines." if there are none. A section starts at each line that starts
// with `### `.
func mechBlock(report string, seg source.Segment) string {
	text, _, _ := strings.Cut(report, "## Coverage")
	prefix := "### " + checker.SegmentPrefix(seg)
	var b strings.Builder
	for len(text) > 0 {
		// The part runs to the next line that starts with `### `.
		end := len(text)
		for i := 1; i < len(text); i++ {
			if text[i-1] == '\n' && strings.HasPrefix(text[i:], "### ") {
				end = i
				break
			}
		}
		if strings.HasPrefix(text[:end], prefix) {
			b.WriteString(text[:end])
		}
		text = text[end:]
	}
	if s := strings.TrimSpace(b.String()); s != "" {
		return s
	}
	return "No lines."
}

// checks is the first run's step 1.2 and step 1.3 results, as the repair
// call reads them.
type checks struct {
	notes []*note.Note
	// mech is the mechanical check's report as written, and mechReport its
	// findings as it wrote them.
	mech       string
	mechReport *mechcheck.Report
	// support and readsAlone are each segment's report as received, and
	// supportFlags and readsAloneFlags its parsed flags, by segment number.
	support, readsAlone           map[int]string
	supportFlags, readsAloneFlags map[int][]checker.Flag
}

func readChecks(src *source.Source, folder *run.Folder) (*checks, error) {
	c := &checks{}
	var err error
	if c.notes, err = note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(run.ExtractNotes))); err != nil {
		return nil, err
	}
	mech, err := os.ReadFile(filepath.Join(folder.Dir, run.MechanicalCheckDir, run.ReportFile))
	if err != nil {
		return nil, err
	}
	if c.mechReport, err = mechcheck.ReadFindings(folder, run.MechanicalCheckDir); err != nil {
		return nil, err
	}
	c.mech = string(mech)
	if c.support, c.supportFlags, err = readCheck(src, folder, run.SupportCheckDir, nil); err != nil {
		return nil, err
	}
	if c.readsAlone, c.readsAloneFlags, err = readCheck(src, folder, run.ReadsAloneDir, nil); err != nil {
		return nil, err
	}
	return c, nil
}

// readCheck reads a checker step's report and flags for each of src's
// segments in segs, or every segment if segs is nil.
func readCheck(src *source.Source, folder *run.Folder, dir string, segs []int) (map[int]string, map[int][]checker.Flag, error) {
	reports, flags := map[int]string{}, map[int][]checker.Flag{}
	for _, seg := range src.Segments {
		if segs != nil && !slices.Contains(segs, seg.Number) {
			continue
		}
		n := strconv.Itoa(seg.Number)
		report, err := os.ReadFile(filepath.Join(folder.Dir, dir, run.ReportsDir, n+".md"))
		if err != nil {
			return nil, nil, err
		}
		b, err := os.ReadFile(filepath.Join(folder.Dir, dir, run.FlagsDir, n+".json"))
		if err != nil {
			return nil, nil, err
		}
		var r checker.Report
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", filepath.Join(dir, run.FlagsDir, n+".json"), err)
		}
		reports[seg.Number], flags[seg.Number] = string(report), r.Flags
	}
	return reports, flags, nil
}

// flagged says whether seg has a flag that counts: a failure in step 1.2's
// report on one of its notes, a step 1.3a flag with a grade that counts, or
// any step 1.3b flag.
func (c *checks) flagged(seg source.Segment) bool {
	if len(c.readsAloneFlags[seg.Number]) > 0 || len(countedSupport(c.supportFlags[seg.Number])) > 0 {
		return true
	}
	prefix := checker.SegmentPrefix(seg)
	for _, nr := range c.mechReport.Notes {
		if !strings.HasPrefix(nr.Name, prefix) {
			continue
		}
		for _, f := range nr.Findings {
			if f.Failed() {
				return true
			}
		}
	}
	return false
}

// Run runs step 1.4's call on every segment of src with a flag that counts,
// with model and briefed for reply and quoteOnce, reading steps 1.1 to 1.3's directories in
// folder. It writes the repair directory: every note, a changed one as the
// reply gives it, under `stage-1/`, and each call's report as `reports/<n>.md`
// and raw reply as `raw/<n>.txt`. A reply that names another segment's note,
// deletes the segment's main note or deletes a note that doesn't exist fails
// the step, as does an edit that is malformed or can't be made. A reply in
// the other shape is applied all the same: the shape only picks the brief.
// With quoteOnce, s2n writes the traces of a reply's notes and edits that
// have no footnotes (package quoteonce).
func Run(ctx context.Context, p provider.Provider, src *source.Source, model string, reply Reply, quoteOnce bool, folder *run.Folder) error {
	c, err := readChecks(src, folder)
	if err != nil {
		return fmt.Errorf("repair: %w", err)
	}
	step, err := folder.Begin(run.RepairDir)
	if err != nil {
		return err
	}
	prompt := func(_ *source.Source, seg source.Segment) (string, bool) {
		if !c.flagged(seg) {
			return "", false
		}
		return Prompt(c.notes, seg, c.mech, c.support[seg.Number], c.readsAlone[seg.Number]), true
	}
	file, brief := Brief(reply, quoteOnce)
	results, err := modelstep.Run(ctx, p, src, modelstep.Spec{Brief: brief, BriefFile: file, Model: model, Effort: Effort, FirstCallRate: FirstCallRate, Prompt: prompt}, step)
	if err != nil {
		return fmt.Errorf("repair: %w", err)
	}
	tracer := Tracer(src, quoteOnce)

	notes := map[string]string{}
	for _, n := range c.notes {
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
	if err := dropped.Write(step, drops); err != nil {
		return err
	}
	return step.Commit()
}

// apply applies a reply's files for seg to notes, the notes' text by file
// name. A FILE block replaces its note, an EDIT block makes its edits to it
// (EditNote), and a DELETE removes one. A block that names a note the segment
// doesn't have is dropped: the brief returns only the notes the call changed,
// so a new name is a misnamed note. So is a DELETE of the main note, and a
// block of a note the reply both edits and rewrites or deletes (Conflicted).
// It gives what it dropped. A non-nil tracer traces what the reply wrote
// quote-once (EditTracer, TraceNote).
func apply(notes map[string]string, seg source.Segment, files []modelstep.File, tracer *quoteonce.Tracer) []dropped.Item {
	prefix := checker.SegmentPrefix(seg)
	main := checker.MainNote(seg)
	var drops []dropped.Item
	drop := func(f modelstep.File, why string) {
		drops = append(drops, dropped.Item{Segment: seg.Number, Note: f.Name, What: Block(f), Why: why})
	}
	conflicted := Conflicted(files)
	for _, f := range files {
		name, err := note.FileName(f.Name)
		if err != nil {
			drop(f, err.Error())
			continue
		}
		if conflicted[name] {
			drop(f, "the reply both edits the note and gives it in full or deletes it")
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			drop(f, "it is not one of the segment's notes")
			continue
		}
		if _, ok := notes[name]; !ok {
			drop(f, "it is not a note")
			continue
		}
		if f.Edit {
			text, edits, err := EditNote(name, notes[name], f.Body, EditTracer(tracer, seg), note.Replace, note.Insert)
			if err != nil {
				drop(f, err.Error())
				continue
			}
			notes[name] = text
			drops = append(drops, edits.Items(seg, name)...)
			continue
		}
		if !f.Delete {
			notes[name] = TraceNote(tracer, seg, name, f.Body)
			continue
		}
		if name == main {
			drop(f, "it is the main note; only an aside may be deleted")
			continue
		}
		delete(notes, name)
	}
	return drops
}

// Block names a reply's block, for a dropped item: its DELETE line, or its
// FILE or EDIT block.
func Block(f modelstep.File) string {
	switch {
	case f.Delete:
		return "the reply's DELETE"
	case f.Edit:
		return "the reply's EDIT block"
	}
	return "the reply's FILE block"
}

// Conflicted gives the names of the notes a reply both edits and rewrites or
// deletes: which comes first is not the reply's to say, so every block of
// such a note is dropped.
func Conflicted(files []modelstep.File) map[string]bool {
	edited, whole := map[string]bool{}, map[string]bool{}
	for _, f := range files {
		name, err := note.FileName(f.Name)
		if err != nil {
			continue
		}
		if f.Edit {
			edited[name] = true
		} else {
			whole[name] = true
		}
	}
	out := map[string]bool{}
	for name := range edited {
		if whole[name] {
			out[name] = true
		}
	}
	return out
}

// DroppedEdit is an edit EditNote left out: the statement it quotes, and why.
type DroppedEdit struct {
	Statement, Why string
}

// DroppedEdits are the edits EditNote left out of one note.
type DroppedEdits []DroppedEdit

// Items gives the dropped edits as dropped items of seg's note name.
func (ds DroppedEdits) Items(seg source.Segment, name string) []dropped.Item {
	var items []dropped.Item
	for _, d := range ds {
		items = append(items, dropped.Item{Segment: seg.Number, Note: name, What: fmt.Sprintf("the edit at %q", d.Statement), Why: d.Why})
	}
	return items
}

// EditNote makes an EDIT block's edits, body, to the note named name whose
// text is text, and gives the edited note's text and the edits it left out.
// Each edit is first passed through trace, if it isn't nil.
//
// An edit is left out if its operation is not one of ops, if the note model
// can't make it on its own (note.Note.Apply): it quotes no statement or more
// than one, or if it is made at a statement that overlaps an earlier edit's.
// The rest are made together. A body that doesn't parse as edits is an error, and the
// caller drops the block.
func EditNote(name, text, body string, trace func(note.Edit) note.Edit, ops ...note.Op) (string, DroppedEdits, error) {
	edits, err := note.ParseEdits(body)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", name, err)
	}
	var drops DroppedEdits
	var allowed []note.Edit
	for _, e := range edits {
		if !slices.Contains(ops, e.Op) {
			drops = append(drops, DroppedEdit{e.Statement, fmt.Sprintf("it %s, which this call may not do", opVerb[e.Op])})
			continue
		}
		if trace != nil {
			e = trace(e)
		}
		allowed = append(allowed, e)
	}
	n := note.Parse(name, text)
	var placed []note.Edit
	out := n
	for _, e := range allowed {
		if _, err := n.Apply([]note.Edit{e}); err != nil {
			drops = append(drops, DroppedEdit{e.Statement, err.Error()})
			continue
		}
		with, err := n.Apply(append(slices.Clone(placed), e))
		if err != nil {
			drops = append(drops, DroppedEdit{e.Statement, err.Error()})
			continue
		}
		placed, out = append(placed, e), with
	}
	if len(placed) == 0 {
		return text, drops, nil
	}
	return out.Render(), drops, nil
}

// EditTracer gives what traces an edit to one of seg's notes, for EditNote:
// tracer's Edit, or nil for a nil tracer.
func EditTracer(tracer *quoteonce.Tracer, seg source.Segment) func(note.Edit) note.Edit {
	if tracer == nil {
		return nil
	}
	return func(e note.Edit) note.Edit { return tracer.Edit(seg.Number, e) }
}

// TraceNote gives the text of a FILE block's note, named name, of segment
// seg: as written for a nil tracer, and otherwise traced if it was written
// quote-once (quoteonce.Tracer.Note).
func TraceNote(tracer *quoteonce.Tracer, seg source.Segment, name, text string) string {
	if tracer == nil {
		return text
	}
	return tracer.Note(seg.Number, note.Parse(name, text)).Render()
}

// opVerb names what an edit does, for errors.
var opVerb = map[note.Op]string{
	note.Replace:   "replaces a statement",
	note.Insert:    "inserts statements",
	note.Paragraph: "adds a paragraph",
}

// Changed gives the numbers of src's segments whose notes the repair call
// changed, in segment order: those whose notes in the repair directory are not
// step 1.1's, by name and byte for byte.
func Changed(src *source.Source, folder *run.Folder) ([]int, error) {
	before, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(run.ExtractNotes)))
	if err != nil {
		return nil, err
	}
	after, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(run.RepairNotes)))
	if err != nil {
		return nil, err
	}
	changed := []int{}
	for _, seg := range src.Segments {
		a, _ := checker.SegmentNotes(before, seg)
		b, _ := checker.SegmentNotes(after, seg)
		if !sameNotes(a, b) {
			changed = append(changed, seg.Number)
		}
	}
	return changed, nil
}

func sameNotes(a, b []*note.Note) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Render() != b[i].Render() {
			return false
		}
	}
	return true
}

// Recheck runs steps 1.2 and 1.3 again on the repaired notes, each into a
// re-check directory of its own: RecheckMechanical, RecheckSupport, then
// RecheckReadsAlone.
func Recheck(ctx context.Context, p provider.Provider, src *source.Source, folder *run.Folder) error {
	if err := RecheckMechanical(src, folder); err != nil {
		return err
	}
	if err := RecheckSupport(ctx, p, src, folder); err != nil {
		return err
	}
	return RecheckReadsAlone(ctx, p, src, folder)
}

// RecheckMechanical runs step 1.2 again on every segment's repaired notes, so
// its coverage and uncovered lists are the whole frame's, for the loss
// proposer.
func RecheckMechanical(src *source.Source, folder *run.Folder) error {
	return mechcheck.RunOn(src, folder, run.RepairNotes, run.MechanicalRecheckDir)
}

// RecheckSupport runs step 1.3a again on the segments the repair call
// changed: an unchanged segment's flags carry over.
func RecheckSupport(ctx context.Context, p provider.Provider, src *source.Source, folder *run.Folder) error {
	return recheck(ctx, p, src, folder, supportcheck.Check(), run.SupportRecheckDir)
}

// RecheckReadsAlone runs step 1.3b again on the segments the repair call
// changed, as RecheckSupport does step 1.3a.
func RecheckReadsAlone(ctx context.Context, p provider.Provider, src *source.Source, folder *run.Folder) error {
	return recheck(ctx, p, src, folder, readsalone.Check(), run.ReadsAloneRecheckDir)
}

func recheck(ctx context.Context, p provider.Provider, src *source.Source, folder *run.Folder, check checker.Check, dir string) error {
	changed, err := Changed(src, folder)
	if err != nil {
		return fmt.Errorf("re-check: %w", err)
	}
	check.Dir, check.Notes, check.Segments = dir, run.RepairNotes, changed
	return checker.Run(ctx, p, src, folder, check)
}

// ReadStillFails reads the still-fails list in the step directory dir, in
// folder. A step with no list, from a run folder made before the run report
// existed, has none.
func ReadStillFails(folder *run.Folder, dir string) ([]Entry, error) {
	p := filepath.Join(folder.Dir, filepath.FromSlash(dir), run.StillFailsFile)
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var es []Entry
	if err := json.Unmarshal(b, &es); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return es, nil
}

// WriteStillFails writes the still-fails list for the run report, from the
// repaired notes, step 1.2's findings on them in its re-check, and each
// segment's step 1.3 flags: the re-check's for a segment the repair call
// changed, the first run's for any other.
func WriteStillFails(src *source.Source, folder *run.Folder) error {
	notes, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(run.RepairNotes)))
	if err != nil {
		return fmt.Errorf("still fails: %w", err)
	}
	changed, err := Changed(src, folder)
	if err != nil {
		return fmt.Errorf("still fails: %w", err)
	}
	var unchanged []int
	for _, seg := range src.Segments {
		if !slices.Contains(changed, seg.Number) {
			unchanged = append(unchanged, seg.Number)
		}
	}
	var support, readsAlone []checker.Flag
	for _, d := range []struct {
		dir  string
		segs []int
		into *[]checker.Flag
	}{
		{run.SupportCheckDir, unchanged, &support},
		{run.SupportRecheckDir, changed, &support},
		{run.ReadsAloneDir, unchanged, &readsAlone},
		{run.ReadsAloneRecheckDir, changed, &readsAlone},
	} {
		if len(d.segs) == 0 {
			continue
		}
		_, flags, err := readCheck(src, folder, d.dir, d.segs)
		if err != nil {
			return fmt.Errorf("still fails: %w", err)
		}
		for _, n := range sortedKeys(flags) {
			*d.into = append(*d.into, flags[n]...)
		}
	}

	mech, err := mechcheck.ReadFindings(folder, run.MechanicalRecheckDir)
	if err != nil {
		return fmt.Errorf("still fails: %w", err)
	}
	entries := StillFails(notes, mech, support, readsAlone)
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(entries); err != nil {
		return err
	}
	step, err := folder.Begin(run.StillFailsDir)
	if err != nil {
		return err
	}
	if err := step.WriteFile(run.StillFailsFile, b.Bytes()); err != nil {
		return err
	}
	return step.Commit()
}

func sortedKeys[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
