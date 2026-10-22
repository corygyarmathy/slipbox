// Package splitgates runs step 2.1's three gates, the split check (package
// splitcheck), the trace diff (package tracediff) and the strip check
// (package striptraces), as one step of the run. They are one step so that `s2n resume` always runs all
// three: a hand fix to the split notes, or a new split, is split-checked
// again before it is diffed, and diffed again before it is strip-checked.
//
// A segment that fails a gate falls back: it goes on with its final stage 1
// notes under a hub of its own (split.Fallback), and the run goes on. So
// does a segment step 2.1 listed as fallen back, for a reply it couldn't
// write. The step writes every segment's notes as they go on, for step 2.2.
package splitgates

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/corygyarmathy/slipbox/internal/fallback"
	"github.com/corygyarmathy/slipbox/internal/gate"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/splitcheck"
	"github.com/corygyarmathy/slipbox/internal/striptraces"
	"github.com/corygyarmathy/slipbox/internal/tracediff"
)

// Run runs step 2.1's gates on the final stage 1 notes in folder and step
// 2.1's split of them, writing each gate's report to the step's directory.
// Each segment is split-checked, then diffed if it passed, then
// strip-checked if it passed both, and a segment that fails one falls back,
// as does each segment step 2.1 listed. A segment with stage 1 notes but no
// folder, which step 2.1 didn't list, is checked as an empty folder, so it
// loses every trace and falls back.
//
// The step writes, under its `stage-2/`, each segment folder that passed,
// as step 2.1 wrote it, and each fallback, and lists the fallbacks in its
// run.FallbacksFile. The fallbacks are strip-checked too, into their own
// report, and as there is nothing left for a fallback to fall back to, one
// that fails stops the run, with a *gate.Stopped and the directory left
// partial.
//
// Before any gate, a folder no segment of src claims stops the run, with no
// report: its notes would pass the gates and then reach no later step,
// which finds folders by segment. It has no stage 1 notes to fall
// back to.
func Run(src *source.Source, folder *run.Folder) error {
	stage1Dir := filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes))
	stage1, err := note.ReadDir(stage1Dir)
	if err != nil {
		return fmt.Errorf("step 2.1's gates: %w", err)
	}
	splitNotes := filepath.Join(folder.Dir, filepath.FromSlash(run.SplitNotes))
	folders, err := split.ReadFolders(splitNotes)
	if err != nil {
		return fmt.Errorf("step 2.1's gates: %w", err)
	}
	replyFaults, err := fallback.Read(folder, run.SplitDir)
	if err != nil {
		return fmt.Errorf("step 2.1's gates: %w", err)
	}
	step, err := folder.Begin(run.SplitGatesDir)
	if err != nil {
		return err
	}
	if unclaimed := split.Unclaimed(src, folders); len(unclaimed) > 0 {
		return split.UnclaimedStop(FolderGateName, src, splitNotes, unclaimed)
	}

	fb := split.NewFallbacks(src, replyFaults)
	checking := fb.Without(withMissing(src, stage1, folders))
	gates := []struct {
		name, file string
		check      func([]split.SegmentFolder) folderReport
	}{
		{splitcheck.GateName, run.SplitCheckFile, func(fs []split.SegmentFolder) folderReport { return splitcheck.Check(src, stage1, fs) }},
		{tracediff.GateName, run.TraceDiffFile, func(fs []split.SegmentFolder) folderReport { return tracediff.Check(src, stage1, fs) }},
		{striptraces.GateName, run.StripCheckFile, func(fs []split.SegmentFolder) folderReport { return striptraces.Check(fs) }},
	}
	for _, g := range gates {
		r := g.check(checking)
		if err := step.WriteFile(g.file, []byte(r.String())); err != nil {
			return err
		}
		fb.Fail(r.FailingFolders(), g.name, g.file)
		checking = fb.Without(checking)
	}

	var fallbacks []split.SegmentFolder
	for _, seg := range src.Segments {
		if fb.Has(split.Folder(seg)) {
			fallbacks = append(fallbacks, split.Fallback(seg, stage1))
		}
	}
	r := striptraces.Check(fallbacks)
	if err := step.WriteFile(run.FallbackStripCheckFile, []byte(r.String())); err != nil {
		return err
	}
	if r.Failed() {
		return &gate.Stopped{
			Gate:     FallbackGateName,
			Segments: split.FailingSegments(src, r.FailingFolders()),
			Report:   filepath.Join(step.Dir, run.FallbackStripCheckFile),
			Items:    r.Items(func(p striptraces.Problem) string { return filepath.Join(stage1Dir, p.Note) }),
			Fixes:    fallbackFixes(filepath.Join(folder.Dir, run.SplitDir)),
		}
	}

	for _, f := range append(checking, fallbacks...) {
		for _, n := range f.Notes {
			if err := step.WriteFile(path.Join(run.Stage2NotesDir, f.Name, n.Name), []byte(n.Render())); err != nil {
				return err
			}
		}
	}
	if err := fallback.Write(step, fb.Items()); err != nil {
		return err
	}
	return step.Commit()
}

// FallbackGateName names the strip check of the stage 2 fallbacks when it
// stops the run.
const FallbackGateName = "the strip check of the fallback notes"

// fallbackFixes say what to do about each kind of item of a fallback that
// can't be stripped: its segment failed step 2.1, so the note is a final
// stage 1 note. The last choice is to delete resplit, step 2.1's directory,
// to split again.
func fallbackFixes(resplit string) map[string]string {
	why := "the segment failed step 2.1 and falls back to this final stage 1 note, so "
	again := "; or delete " + resplit + " to split every segment again"
	return map[string]string{
		striptraces.NoH1:          why + "give it an H1, `# <title>`, above its statements" + again,
		striptraces.OtherFootnote: why + "make the footnote a trace, `[[<file>#<section>]] \"<quote>\"`, of a statement that refers to it, or delete it and its references" + again,
	}
}

// folderReport is a gate's report on segment folders, which names the
// folders that fail it.
type folderReport interface {
	gate.Report
	FailingFolders() []string
}

// withMissing is folders with an empty folder added, in name order, for each
// segment of src that has stage 1 notes to split but no folder.
func withMissing(src *source.Source, stage1 []*note.Note, folders []split.SegmentFolder) []split.SegmentFolder {
	have := map[string]bool{}
	for _, f := range folders {
		have[f.Name] = true
	}
	out := slices.Clone(folders)
	for _, seg := range src.Segments {
		if _, ok := split.Prompt(src, stage1, seg); ok && !have[split.Folder(seg)] {
			out = append(out, split.SegmentFolder{Name: split.Folder(seg)})
		}
	}
	slices.SortFunc(out, func(a, b split.SegmentFolder) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// FolderGateName names the check that every segment folder is a segment's.
const FolderGateName = "step 2.1's folder check"
