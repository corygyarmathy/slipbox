// Package readsalone is step 1.3b, the reads-alone check, in a call of its
// own: per segment, an independent checker reads the frame and that
// segment's notes only, and flags each statement whose referring words don't
// resolve from earlier in the same note (ADR 0007). The run keeps each report and parses it into flags of the same
// shape as step 1.3a's.
package readsalone

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
// before it has a finished call (modelstep.Spec.FirstCallRate): step 1.3b's
// first call in Ch 4's quote-once run (ADR 0006), at Sonnet, cost $0.116 for
// a 76,950-character prompt. Step 1.4's re-check of it found step 1.3b's
// cache, and cost a quarter of that.
const FirstCallRate = 1.51e-6

// Grades are checker-reads-alone.md's non-pass grades.
var Grades = []checker.Grade{checker.Dangling, checker.OutsideFrame}

// Prompt is seg's user prompt, the trialled checker prompt after the frame
// without the other segments' block: seg's own notes only. notes are every
// stage 1 note, in name order.
func Prompt(notes []*note.Note, seg source.Segment) string {
	mine, _ := checker.SegmentNotes(notes, seg)
	var b strings.Builder
	checker.NotesBlock(&b, mine, fmt.Sprintf("YOUR SEGMENT'S NOTES (segment %d: %s)", seg.Number, seg.Heading))
	return b.String()
}

// Check is step 1.3b as checker.Run runs it on the stage 1 notes step 1.1
// wrote. Step 1.4's re-check changes its directory, notes and segments.
func Check() checker.Check {
	return checker.Check{
		Dir: run.ReadsAloneDir, Brief: briefs.CheckerReadsAlone, BriefFile: briefs.CheckerReadsAloneFile, Model: Model, Effort: Effort, FirstCallRate: FirstCallRate, Grades: Grades, Prompt: Prompt, Notes: run.ExtractNotes,
	}
}

// Run runs step 1.3b on every segment of src, reading the stage 1 notes step
// 1.1 wrote in folder, and writes its reports and flags as checker.Run does.
func Run(ctx context.Context, p provider.Provider, src *source.Source, folder *run.Folder) error {
	return checker.Run(ctx, p, src, folder, Check())
}
