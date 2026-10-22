// Package format is step 3.1, format: per segment, one call adds or removes
// markup only in the segment's stripped notes, its hub among them, under the
// formatting convention in the brief, and replies with every note, changed or
// not, and a manifest. "No change" is a
// correct result. The run writes each segment's notes into a `<n> <title>/`
// folder and keeps its manifest.
package format

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/corygyarmathy/slipbox/internal/briefs"
	"github.com/corygyarmathy/slipbox/internal/checker"
	"github.com/corygyarmathy/slipbox/internal/fallback"
	"github.com/corygyarmathy/slipbox/internal/modelstep"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
)

// DefaultModel is the step's tier: Haiku, which formatted Ch 4 (ADR 0005).
const DefaultModel = "haiku"

// Effort is the step's effort level: medium. Not trialled; pinned with step
// 1.1's (ADR 0004).
const Effort = "medium"

// FirstCallRate is the step's predicted cost in dollars per prompt character
// before it has a finished call (modelstep.Spec.FirstCallRate). No recorded
// run reaches step 3.1, so it is step 2.1's at Haiku, the step's tier: a call
// that also reads a segment's notes with the frame and writes them back. Step
// 2.1's first call in Ch 4's stage 2 tier trial (ADR 0005), at Haiku, cost
// $0.0100 for a 78,360-character prompt.
const FirstCallRate = 1.28e-7

// Prompt is the user prompt for the segment folder f, as trialled after the
// frame: every note in f, the hub among them, in byte order of
// their names, as a `SEGMENT <n> STAGE 2 NOTES` block for f's `<n> <title>`.
func Prompt(f split.SegmentFolder) string {
	number, _, _ := strings.Cut(f.Name, " ")
	var b strings.Builder
	checker.NotesBlock(&b, f.Notes, "SEGMENT "+number+" STAGE 2 NOTES")
	return b.String()
}

// Run runs step 3.1 with model on every segment of src that has a folder of
// stripped notes in folder, writing the step's directory: each segment's
// notes under `stage-3/<n> <title>/`, its manifest as `manifests/<n>.json`
// and its raw reply as `raw/<n>.txt`. It runs only once step 2.2 is
// complete. A reply must give each of its segment's notes once, as a whole
// note, and no other. A segment whose reply doesn't gets no folder or
// manifest: it falls back, and the step lists it in its run.FallbacksFile,
// for step 3.1's gate to give it its stripped notes.
func Run(ctx context.Context, p provider.Provider, src *source.Source, model string, folder *run.Folder) error {
	if !folder.Complete(run.StripDir) {
		return errors.New("format: step 2.2 has not stripped the traces, so there are no notes to format yet")
	}
	folders, err := split.ReadFolders(filepath.Join(folder.Dir, filepath.FromSlash(run.StrippedNotes)))
	if err != nil {
		return fmt.Errorf("format: %w", err)
	}
	byName := map[string]split.SegmentFolder{}
	for _, f := range folders {
		byName[f.Name] = f
	}
	step, err := folder.Begin(run.FormatDir)
	if err != nil {
		return err
	}
	prompt := func(_ *source.Source, seg source.Segment) (string, bool) {
		f, ok := byName[split.Folder(seg)]
		if !ok {
			return "", false
		}
		return Prompt(f), true
	}
	results, err := modelstep.Run(ctx, p, src, modelstep.Spec{Brief: briefs.Stage3, BriefFile: briefs.Stage3File, Model: model, Effort: Effort, FirstCallRate: FirstCallRate, Prompt: prompt}, step)
	if err != nil {
		return fmt.Errorf("format: %w", err)
	}
	// The gate reads the folder even if every segment falls back.
	if err := step.Mkdir(run.Stage3NotesDir); err != nil {
		return err
	}
	var fallbacks []fallback.Item
	for _, r := range results {
		f := byName[split.Folder(r.Segment)]
		files, err := replyNotes(f, r)
		var fault *modelstep.Fault
		if errors.As(err, &fault) {
			fallbacks = append(fallbacks, fallback.Item{Segment: r.Segment.Number, Why: "step 3.1's reply " + fault.Why})
			continue
		}
		if err != nil {
			return err
		}
		if err := modelstep.WriteNotes(step, path.Join(run.Stage3NotesDir, f.Name), files); err != nil {
			return err
		}
		if err := step.WriteFile(path.Join(run.ManifestsDir, strconv.Itoa(r.Segment.Number)+".json"), []byte(r.Tail)); err != nil {
			return err
		}
	}
	if err := fallback.Write(step, fallbacks); err != nil {
		return err
	}
	return step.Commit()
}

// replyNotes gives r's notes, as modelstep.Notes does. A reply that leaves
// out a note of f, or gives one that isn't f's, is a *modelstep.Fault too:
// the format check reads only the notes written, so a note left out would be
// lost unreported.
func replyNotes(f split.SegmentFolder, r modelstep.Result) ([]modelstep.File, error) {
	files, err := modelstep.Notes(r)
	if err != nil {
		return nil, err
	}
	written := map[string]bool{}
	for _, file := range files {
		written[file.Name] = true
	}
	if missing, extra := compare(f, written); len(missing) > 0 || len(extra) > 0 {
		var faults []string
		if len(missing) > 0 {
			faults = append(faults, fmt.Sprintf("left out %q", missing))
		}
		if len(extra) > 0 {
			faults = append(faults, fmt.Sprintf("wrote %q, not notes of the segment", extra))
		}
		return nil, &modelstep.Fault{Segment: r.Segment, Why: strings.Join(faults, " and ") + ", but it must give back every note of its segment, as named"}
	}
	return files, nil
}

// compare gives the notes of f that written lacks, and the names in written
// that aren't f's, each in byte order.
func compare(f split.SegmentFolder, written map[string]bool) (missing, extra []string) {
	mine := map[string]bool{}
	for _, n := range f.Notes {
		mine[n.Name] = true
		if !written[n.Name] {
			missing = append(missing, n.Name)
		}
	}
	for name := range written {
		if !mine[name] {
			extra = append(extra, name)
		}
	}
	slices.Sort(extra)
	return missing, extra
}
