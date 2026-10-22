// Package supportcheck is step 1.3a, the support and cuts check: per segment,
// an independent checker grades each statement's support, its cuts and its
// name spans, and replies with a report (ADR 0007). The run keeps each report and parses it into flags, which the
// repair pass and the run report read.
package supportcheck

import (
	"context"
	"fmt"
	"strings"

	"github.com/corygyarmathy/slipbox/internal/briefs"
	"github.com/corygyarmathy/slipbox/internal/checker"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
)

// Model is the checker's tier: Sonnet.
const Model = "sonnet"

// Effort is the checker's effort level: medium, what every recorded run
// used. Not trialled; pinned with step 1.1's (ADR 0004).
const Effort = "medium"

// FirstCallRate is the step's predicted cost in dollars per prompt character
// before it has a finished call (modelstep.Spec.FirstCallRate): step 1.3a's
// first call in Ch 4's quote-once run (ADR 0006), at Sonnet, cost $0.342 for
// a 225,537-character prompt. Step 1.4's re-check of it cost 1.50e-6 a
// character.
const FirstCallRate = 1.52e-6

// Grades are checker-support.md's non-pass grades.
var Grades = []checker.Grade{
	checker.Weak, checker.Fail, checker.Dangling, checker.OutsideFrame, checker.CutLostContent,
	checker.Reworded, checker.WrongReferent, checker.NotANameSpan, checker.CutWouldDo, checker.ReadsWrong,
}

// Shared is what every call of the step shares, for the system prompt after
// the frame: every stage 1 note, in name order. The trial put the other
// segments' notes in the user prompt instead, which left nothing after the
// frame to cache.
func Shared(notes []*note.Note) string {
	var b strings.Builder
	checker.NotesBlock(&b, notes, "ALL SEGMENTS' NOTES (check only the notes your segment's list names; the rest are reference only)")
	return b.String()
}

// Prompt is seg's user prompt: the names of seg's own notes, the ones to
// check, one per line. The notes themselves are in the shared block, so the
// prompt names them rather than giving them again. notes are every stage 1
// note, in name order.
func Prompt(notes []*note.Note, seg source.Segment) string {
	mine, _ := checker.SegmentNotes(notes, seg)
	label := fmt.Sprintf("YOUR SEGMENT'S NOTES TO CHECK (segment %d: %s)", seg.Number, seg.Heading)
	var b strings.Builder
	fmt.Fprintf(&b, "=== %s ===\n", label)
	for _, n := range mine {
		fmt.Fprintf(&b, "%s\n", n.Name)
	}
	fmt.Fprintf(&b, "=== END %s ===\n", label)
	return b.String()
}

// Check is step 1.3a as checker.Run runs it on the stage 1 notes step 1.1
// wrote. Step 1.4's re-check changes its directory, notes and segments.
func Check() checker.Check {
	return checker.Check{
		Dir: run.SupportCheckDir, Brief: briefs.CheckerSupport, BriefFile: briefs.CheckerSupportFile, Model: Model, Effort: Effort, FirstCallRate: FirstCallRate, Grades: Grades, Prompt: Prompt, Shared: Shared,
		Notes: run.ExtractNotes,
	}
}

// Run runs step 1.3a on every segment of src, reading the stage 1 notes step
// 1.1 wrote in folder, and writes its reports and flags as checker.Run does.
func Run(ctx context.Context, p provider.Provider, src *source.Source, folder *run.Folder) error {
	return checker.Run(ctx, p, src, folder, Check())
}
