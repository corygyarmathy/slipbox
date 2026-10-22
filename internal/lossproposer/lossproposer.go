// Package lossproposer is step 1.5, the loss proposer (ADR 0008). Each segment with an uncovered list gets a call with
// the proposer brief, on Sonnet unless a run sets another model, and the step
// runs twice: each Sonnet run found 4 of 5 known losses, and each missed a
// different one. The second run's prompts are the first's, byte for byte, so
// it is a cache read. The union of the two runs goes to the kept-loss repair,
// every loss of it (union.go), and each item that isn't a loss, a remark,
// goes to the run report as dropped.
//
// The step reads the repaired notes and step 1.4's re-check of step 1.2, so
// its uncovered lists are the ones the notes it reads leave.
package lossproposer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/corygyarmathy/slipbox/internal/briefs"
	"github.com/corygyarmathy/slipbox/internal/checker"
	"github.com/corygyarmathy/slipbox/internal/dropped"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/modelstep"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
)

// Model is the proposer's tier, and the model Run is given unless a run sets
// another: Sonnet. Opus has been trialled once, beside a Sonnet step 1.1,
// and every item it proposed read as a real loss (ADR 0008).
const Model = "sonnet"

// Effort is the proposer's effort level: medium, what every recorded run
// used. Not trialled; pinned with step 1.1's (ADR 0004).
const Effort = "medium"

// FirstCallRate is the step's predicted cost in dollars per prompt character
// before it has a finished call (modelstep.Spec.FirstCallRate): step 1.5's
// first run's first call in Ch 4's quote-once run (ADR 0006), at Sonnet, cost
// $0.326 for a 227,169-character prompt.
const FirstCallRate = 1.43e-6

// Runs is how many times the step runs: twice, keeping the union.
const Runs = 2

// Shared is what every call of the step shares, for the system prompt after
// the frame: every stage 1 note, in name order, as the trial gave them. The
// trial put them in the user prompt, where they were not cached.
func Shared(notes []*note.Note) string {
	var b strings.Builder
	checker.NotesBlock(&b, notes, "ALL STAGE 1 NOTES FOR THE FRAME")
	return b.String()
}

// Prompt is seg's user prompt, the rest of the trialled prompt: seg's
// uncovered list as written.
func Prompt(seg source.Segment, uncovered string) string {
	return fmt.Sprintf("=== YOUR SEGMENT'S UNCOVERED LIST (segment %d: %s) ===\n%s=== END ===\n", seg.Number, seg.Heading, uncovered)
}

// RemarkWhy is why step 1.5 drops a remark, for the run report.
const RemarkWhy = "its quotes cite no line of the uncovered list, so it wasn't added as a loss"

// Run runs step 1.5 on every segment of src with an uncovered list, reading
// the repaired notes and the re-check's uncovered lists in folder. It runs
// the proposer on model over every such segment, then again with the same
// prompts, and writes the union of the two runs as the proposed losses. A
// segment with no uncovered list gets no call and no entry. Each remark, an
// item the union didn't make a loss, is written as a dropped item, so the
// run report lists it.
func Run(ctx context.Context, p provider.Provider, src *source.Source, model string, folder *run.Folder) error {
	notes, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(run.RepairNotes)))
	if err != nil {
		return fmt.Errorf("loss proposer: %w", err)
	}
	lists := map[int]string{}
	for _, seg := range src.Segments {
		name := mechcheck.SectionCoverage{Segment: seg.Number}.File()
		b, err := os.ReadFile(filepath.Join(folder.Dir, run.MechanicalRecheckDir, run.UncoveredDir, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("loss proposer: %w", err)
		}
		lists[seg.Number] = string(b)
	}

	step, err := folder.Begin(run.LossProposerDir)
	if err != nil {
		return err
	}
	prompt := func(_ *source.Source, seg source.Segment) (string, bool) {
		list, ok := lists[seg.Number]
		if !ok {
			return "", false
		}
		return Prompt(seg, list), true
	}
	// The runs go one after the other, so every second call finds the
	// first's prompt cached.
	var runs [Runs][]modelstep.Result
	shared := Shared(notes)
	for k := range runs {
		spec := modelstep.Spec{Brief: briefs.Proposer, BriefFile: briefs.ProposerFile, Model: model, Effort: Effort, FirstCallRate: FirstCallRate, Shared: shared, Prompt: prompt, Run: k + 1}
		if runs[k], err = modelstep.Run(ctx, p, src, spec, step); err != nil {
			return fmt.Errorf("loss proposer: %w", err)
		}
	}

	segments := []Segment{}
	var drops []dropped.Item
	for i, r := range runs[0] {
		list, err := mechcheck.ParseList(r.Segment.Number, lists[r.Segment.Number])
		if err != nil {
			return fmt.Errorf("loss proposer: %w", err)
		}
		var replies []Reply
		for k := range runs {
			replies = append(replies, Parse(runs[k][i].Text))
		}
		seg := Union(list, replies)
		for _, it := range seg.Remarks {
			drops = append(drops, dropped.Item{Segment: seg.Segment, What: Remark(it), Why: RemarkWhy})
		}
		segments = append(segments, seg)
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(segments); err != nil {
		return err
	}
	if err := step.WriteFile(run.ProposedLossesFile, b.Bytes()); err != nil {
		return err
	}
	if err := dropped.Write(step, drops); err != nil {
		return err
	}
	return step.Commit()
}
