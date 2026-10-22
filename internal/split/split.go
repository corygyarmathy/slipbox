// Package split is step 2.1, split and hub: per segment, one call splits the
// segment's final stage 1 notes into atomic notes, each moving its statements
// with their traces and writing none, and writes the segment's hub note
// (ADR 0003). The run writes each segment's notes
// into a `<n> <title>/` folder and keeps its manifest.
package split

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/corygyarmathy/slipbox/internal/briefs"
	"github.com/corygyarmathy/slipbox/internal/checker"
	"github.com/corygyarmathy/slipbox/internal/fallback"
	"github.com/corygyarmathy/slipbox/internal/gate"
	"github.com/corygyarmathy/slipbox/internal/modelstep"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
)

// DefaultModel is the step's tier: Sonnet, which split Ch 4 with the fewest
// faulty name-span edits of the three tiers, at about half Opus's cost (ADR
// 0005).
const DefaultModel = "sonnet"

// Effort is the step's effort level: medium. Not trialled; pinned with step
// 1.1's (ADR 0004).
const Effort = "medium"

// FirstCallRate is the step's predicted cost in dollars per prompt character
// before it has a finished call (modelstep.Spec.FirstCallRate): step 2.1's
// first call in Ch 4's stage 2 tier trial (ADR 0005) cost $0.147 for a
// 78,360-character prompt. The run was at Sonnet, the default; at Haiku and
// Opus, in the trial's other two runs, it cost 1.28e-7 and 4.10e-6 a
// character.
const FirstCallRate = 1.88e-6

// Folder is the name of seg's folder of stage 2 notes: `<n> <title>`.
func Folder(seg source.Segment) string {
	return checker.SegmentPrefix(seg) + seg.Title
}

// Hub is the file name of seg's hub note in its folder, `<title>.md`.
func Hub(seg source.Segment) string {
	return seg.Title + ".md"
}

// SegmentFolder is one segment's folder of stage 2 notes, as step 2.1 wrote
// it.
type SegmentFolder struct {
	// Name is the folder's name, `<n> <title>`.
	Name string
	// Notes are its notes, the hub among them, in byte order of their names.
	Notes []*note.Note
}

// Stage1 gives the stage 1 notes the folder's segment split, those of stage1
// named `<n> *.md` for the folder's `<n> <title>`, in stage1's order.
func (f SegmentFolder) Stage1(stage1 []*note.Note) []*note.Note {
	number, _, _ := strings.Cut(f.Name, " ")
	var out []*note.Note
	for _, n := range stage1 {
		if strings.HasPrefix(n.Name, number+" ") {
			out = append(out, n)
		}
	}
	return out
}

// IsHub says whether n is the folder's hub, the note named after the
// folder's title.
func (f SegmentFolder) IsHub(n *note.Note) bool {
	_, title, _ := strings.Cut(f.Name, " ")
	return n.Name == title+".md"
}

// FailingSegments describes the segment folders that fail a gate, named as
// its report names them, in segment order, then each that names no segment
// of src, in the order given.
func FailingSegments(src *source.Source, folders []string) []string {
	failed := map[string]bool{}
	for _, f := range folders {
		failed[f] = true
	}
	var out []string
	for _, seg := range src.Segments {
		if failed[Folder(seg)] {
			out = append(out, seg.String())
			delete(failed, Folder(seg))
		}
	}
	for _, f := range folders {
		if failed[f] {
			out = append(out, fmt.Sprintf("folder %q", f))
		}
	}
	return out
}

// ReadFolders reads every segment folder in dir, such as step 2.1's
// `stage-2/`, in byte order of their names (os.ReadDir's), so a report lists
// segments in the same order on every system. A file in dir, or a folder
// whose name starts with a dot, which is hidden, is not a segment folder.
func ReadFolders(dir string) ([]SegmentFolder, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []SegmentFolder
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		notes, err := note.ReadDir(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, SegmentFolder{Name: e.Name(), Notes: notes})
	}
	return out, nil
}

// Prompt is seg's user prompt, as trialled after the frame: seg's
// own notes as a `SEGMENT <n> STAGE 1 NOTES` block, then the hub title and
// the source's name. notes are every final stage 1 note, in name order. It
// is false if seg has no notes, so it gets no call.
func Prompt(src *source.Source, notes []*note.Note, seg source.Segment) (string, bool) {
	mine, _ := checker.SegmentNotes(notes, seg)
	if len(mine) == 0 {
		return "", false
	}
	var b strings.Builder
	checker.NotesBlock(&b, mine, fmt.Sprintf("SEGMENT %d STAGE 1 NOTES", seg.Number))
	fmt.Fprintf(&b, "Hub title: `%s`. Source file name for traces: `%s`.", seg.Title, src.Name)
	return b.String(), true
}

// Run runs step 2.1 with model on every segment of src that has final stage
// 1 notes in folder, writing the step's directory: each segment's notes
// under `stage-2/<n> <title>/`, its manifest as `manifests/<n>.json` and its
// raw reply as `raw/<n>.txt`. A reply must give its segment's hub and at
// least one atomic note, as whole notes. A segment whose reply doesn't gets
// no folder or manifest: it falls back, and the step lists it in its
// run.FallbacksFile, for step 2.1's gates to give it its fallback.
func Run(ctx context.Context, p provider.Provider, src *source.Source, model string, folder *run.Folder) error {
	notes, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes)))
	if err != nil {
		return fmt.Errorf("split: %w", err)
	}
	// A title that can't be a folder name fails here, before any call is
	// paid for, as its hub could never be written.
	for _, seg := range src.Segments {
		if _, ok := Prompt(src, notes, seg); !ok {
			continue
		}
		if _, err := note.FileName(Folder(seg)); err != nil {
			return fmt.Errorf("split: %s: its title can't name a folder: %w", seg, err)
		}
	}
	step, err := folder.Begin(run.SplitDir)
	if err != nil {
		return err
	}
	prompt := func(src *source.Source, seg source.Segment) (string, bool) { return Prompt(src, notes, seg) }
	results, err := modelstep.Run(ctx, p, src, modelstep.Spec{Brief: briefs.Stage2, BriefFile: briefs.Stage2File, Model: model, Effort: Effort, FirstCallRate: FirstCallRate, Prompt: prompt}, step)
	if err != nil {
		return fmt.Errorf("split: %w", err)
	}
	// The gates read the folder even if every segment falls back.
	if err := step.Mkdir(run.Stage2NotesDir); err != nil {
		return err
	}
	var fallbacks []fallback.Item
	for _, r := range results {
		files, err := replyNotes(r)
		var fault *modelstep.Fault
		if errors.As(err, &fault) {
			fallbacks = append(fallbacks, fallback.Item{Segment: r.Segment.Number, Why: "step 2.1's reply " + fault.Why})
			continue
		}
		if err != nil {
			return fmt.Errorf("split: %w", err)
		}
		if err := modelstep.WriteNotes(step, path.Join(run.Stage2NotesDir, Folder(r.Segment)), files); err != nil {
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

// replyNotes gives r's notes, as modelstep.Notes does. A reply without its
// segment's hub or any atomic note is a *modelstep.Fault too: the split check
// and the trace diff read only the folders written, so its segment would lose
// its statements unreported.
func replyNotes(r modelstep.Result) ([]modelstep.File, error) {
	files, err := modelstep.Notes(r)
	if err != nil {
		return nil, err
	}
	hub := false
	for _, f := range files {
		hub = hub || f.Name == Hub(r.Segment)
	}
	if !hub {
		return nil, &modelstep.Fault{Segment: r.Segment, Why: fmt.Sprintf("wrote no hub note %q", Hub(r.Segment))}
	}
	if len(files) < 2 {
		return nil, &modelstep.Fault{Segment: r.Segment, Why: "wrote no atomic note"}
	}
	return files, nil
}

// Fallback is seg's folder when it falls back in stage 2: its final stage 1
// notes, of stage1, in place of a split, and a hub with no statements or
// traces. The hub is titled as step 2.1's is, and lists each note in name
// order as a numbered item, `1. [[4 The Second Law]]`, with each aside nested
// under the note it is an aside of, as the brief nests them: a tab, then
// `- Aside: [[4 The Second Law - Aside - Maxwell's Demon]]`. An aside with no
// such note in stage1 is an item of its own.
func Fallback(seg source.Segment, stage1 []*note.Note) SegmentFolder {
	mine, _ := checker.SegmentNotes(stage1, seg)
	var mains, asides []*note.Note
	for _, n := range mine {
		if note.IsAside(n.Name) {
			asides = append(asides, n)
		} else {
			mains = append(mains, n)
		}
	}
	link := func(n *note.Note) string { return "[[" + strings.TrimSuffix(n.Name, ".md") + "]]" }
	var b strings.Builder
	fmt.Fprintf(&b, "---\naliases: []\ntags: []\n---\n\n# %s\n\n", seg.Title)
	item := 0
	listed := map[*note.Note]bool{}
	for _, m := range mains {
		item++
		fmt.Fprintf(&b, "%d. %s\n", item, link(m))
		prefix := strings.TrimSuffix(m.Name, ".md") + " - Aside - "
		for _, a := range asides {
			if !listed[a] && strings.HasPrefix(a.Name, prefix) {
				fmt.Fprintf(&b, "\t- Aside: %s\n", link(a))
				listed[a] = true
			}
		}
	}
	for _, a := range asides {
		if !listed[a] {
			item++
			fmt.Fprintf(&b, "%d. Aside: %s\n", item, link(a))
		}
	}
	notes := append(slices.Clone(mine), note.Parse(Hub(seg), b.String()))
	slices.SortFunc(notes, func(a, b *note.Note) int { return strings.Compare(a.Name, b.Name) })
	return SegmentFolder{Name: Folder(seg), Notes: notes}
}

// Unclaimed names the folders, in the order given, that are no segment of
// src's Folder.
func Unclaimed(src *source.Source, folders []SegmentFolder) []string {
	claimed := map[string]bool{}
	for _, seg := range src.Segments {
		claimed[Folder(seg)] = true
	}
	var out []string
	for _, f := range folders {
		if !claimed[f.Name] {
			out = append(out, f.Name)
		}
	}
	return out
}

// UnclaimedStop is the stop, by the gate named gateName, for the folders in
// dir that no segment of src claims. It has no report, so its Fix says on
// its own which folders, why, and what to do.
func UnclaimedStop(gateName string, src *source.Source, dir string, unclaimed []string) *gate.Stopped {
	paths := make([]string, len(unclaimed))
	for i, f := range unclaimed {
		paths[i] = filepath.Join(dir, f)
	}
	names := make([]string, len(src.Segments))
	for i, seg := range src.Segments {
		names[i] = fmt.Sprintf("%q", Folder(seg))
	}
	it := "it"
	if len(unclaimed) > 1 {
		it = "each"
	}
	return &gate.Stopped{
		Gate:     gateName,
		Segments: FailingSegments(src, unclaimed),
		Fix: fmt.Sprintf("No segment of the source has the folder name of %s. The source's segments' folders are %s. Rename %s to the folder of the segment its notes belong to, move its notes into that folder, or delete it",
			strings.Join(paths, ", "), strings.Join(names, ", "), it),
	}
}
