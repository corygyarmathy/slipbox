package splitgates_test

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/fallback"
	"github.com/corygyarmathy/slipbox/internal/gate"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/splitcheck"
	"github.com/corygyarmathy/slipbox/internal/splitgates"
	"github.com/corygyarmathy/slipbox/internal/striptraces"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
	"github.com/corygyarmathy/slipbox/internal/tracediff"
)

// passingSplitCheck is the split check's report on folder's final stage 1
// notes and split, which it fails the test unless they pass.
func passingSplitCheck(t *testing.T, src *source.Source, folder *run.Folder) string {
	t.Helper()
	notes, folders := finalAndSplit(t, folder)
	r := splitcheck.Check(src, notes, folders)
	if r.Failed() {
		t.Fatalf("the split check fails:\n%s", r)
	}
	return r.String()
}

// passingTraceDiff is the trace diff's report on folder's final stage 1
// notes and split, which it fails the test unless they pass. tracediff's
// tests say what it finds on them.
func passingTraceDiff(t *testing.T, src *source.Source, folder *run.Folder) string {
	t.Helper()
	notes, folders := finalAndSplit(t, folder)
	r := tracediff.Check(src, notes, folders)
	if r.Failed() {
		t.Fatalf("the trace diff fails:\n%s", r)
	}
	return r.String()
}

// finalAndSplit reads folder's final stage 1 notes and its split.
func finalAndSplit(t *testing.T, folder *run.Folder) ([]*note.Note, []split.SegmentFolder) {
	t.Helper()
	notes, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes)))
	if err != nil {
		t.Fatal(err)
	}
	folders, err := split.ReadFolders(filepath.Join(folder.Dir, filepath.FromSlash(run.SplitNotes)))
	if err != nil {
		t.Fatal(err)
	}
	return notes, folders
}

// The invented split passes all three gates together: the step's directory
// is complete, with the reports, the split's segment folders copied whole as
// the notes that go on, and no fallback.
func TestRun_Passes(t *testing.T) {
	src, folder := testfixture.LampRun(t)
	splitCheck, traceDiff := passingSplitCheck(t, src, folder), passingTraceDiff(t, src, folder)
	if err := splitgates.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	if !folder.Complete(run.SplitGatesDir) {
		t.Fatal("the step is not complete")
	}
	for file, want := range map[string]string{
		run.SplitCheckFile:         splitCheck,
		run.TraceDiffFile:          traceDiff,
		run.StripCheckFile:         "Every note can be stripped.\n",
		run.FallbackStripCheckFile: "Every note can be stripped.\n",
		run.FallbacksFile:          "[]\n",
	} {
		got, err := os.ReadFile(filepath.Join(folder.Dir, run.SplitGatesDir, file))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s kept differs from the one wanted:\n%s", file, got)
		}
	}
	split := splitNotes(t, folder)
	if got := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.SplitOrFallbackNotes))); !reflect.DeepEqual(got, split) {
		t.Errorf("the notes that go on are not the split's, byte for byte")
	}
}

// wantFallback checks that the gates completed with segment number's folder,
// name, fallen back alone, for why, with the gate's report file: its notes
// that go on are its final stage 1 notes under a generated hub, and every
// other folder's are the split's, byte for byte.
func wantFallback(t *testing.T, src *source.Source, folder *run.Folder, number int, name, why, report string) {
	t.Helper()
	if !folder.Complete(run.SplitGatesDir) {
		t.Fatal("the step is not complete")
	}
	items, err := fallback.Read(folder, run.SplitGatesDir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []fallback.Item{{Segment: number, Why: why, Report: report}}; !reflect.DeepEqual(items, want) {
		t.Errorf("fallbacks %+v, want %+v", items, want)
	}
	stage1, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes)))
	if err != nil {
		t.Fatal(err)
	}
	want := splitNotes(t, folder)
	for rel := range want {
		if strings.HasPrefix(rel, name+"/") {
			delete(want, rel)
		}
	}
	fb := split.Fallback(src.Segments[number], stage1)
	if fb.Name != name {
		t.Fatalf("segment %d's folder is %q, want %q", number, fb.Name, name)
	}
	for _, n := range fb.Notes {
		want[name+"/"+n.Name] = n.Render()
	}
	got := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.SplitOrFallbackNotes)))
	if !reflect.DeepEqual(keys(got), keys(want)) {
		t.Fatalf("the notes that go on are %q, want %q", keys(got), keys(want))
	}
	for rel, w := range want {
		if got[rel] != w {
			t.Errorf("%s differs:\n%s\nwant:\n%s", rel, got[rel], w)
		}
	}
	hub := got[name+"/"+split.Hub(src.Segments[number])]
	if !strings.Contains(hub, "1. [[") || strings.Contains(hub, "[^") {
		t.Errorf("the fallback's hub lists no note, or has a footnote:\n%s", hub)
	}
}

// splitNotes are step 2.1's notes in folder's segment folders, by path in
// its `stage-2/`. The split keeps each manifest beside the notes, which is no
// note, and a loose note in no segment folder, which no gate reads and
// doesn't go on.
func splitNotes(t *testing.T, folder *run.Folder) map[string]string {
	t.Helper()
	files := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.SplitNotes)))
	maps.DeleteFunc(files, func(rel, _ string) bool { return !strings.HasSuffix(rel, ".md") || !strings.Contains(rel, "/") })
	return files
}

func keys(m map[string]string) []string {
	return slices.Sorted(maps.Keys(m))
}

// A split that drops a statement's line loses its traces: the split check
// fails its segment, which falls back, and the rest go on. Its folder isn't
// diffed or strip-checked.
func TestRun_LostTraceFallsBack(t *testing.T) {
	src, folder := testfixture.LampRun(t)
	p := filepath.Join(folder.Dir, filepath.FromSlash(run.SplitNotes), "3 Storms")
	notes, err := note.ReadDir(p)
	if err != nil {
		t.Fatal(err)
	}
	var target *note.Note
	for _, n := range notes {
		if n.Name != "Storms.md" {
			target = n
			break
		}
	}
	dropped := false
	for i, l := range target.Lines {
		if l.Kind == note.Body && len(l.Refs) > 0 {
			target.Lines = append(target.Lines[:i], target.Lines[i+1:]...)
			dropped = true
			break
		}
	}
	if !dropped {
		t.Fatalf("%s has no traced line to drop", target.Name)
	}
	if err := os.WriteFile(filepath.Join(p, target.Name), []byte(target.Render()), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := splitgates.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	wantFallback(t, src, folder, 3, "3 Storms", splitcheck.GateName, run.SplitCheckFile)
	dir := filepath.Join(folder.Dir, run.SplitGatesDir)
	b, err := os.ReadFile(filepath.Join(dir, run.SplitCheckFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\n## 3 Storms: 2 atomic notes, statements 3->2, ") || !strings.Contains(string(b), "  lost_traces: ") {
		t.Errorf("report kept does not give the lost traces:\n%s", b)
	}
	for _, file := range []string{run.TraceDiffFile, run.StripCheckFile} {
		if b, err := os.ReadFile(filepath.Join(dir, file)); err != nil || strings.Contains(string(b), "3 Storms") {
			t.Errorf("%s checked the segment that failed the split check (err %v):\n%s", file, err, b)
		}
	}
}

// A stage 1 definition no statement refers to is no trace to the split
// check, but its quote is one to the trace diff. A split that leaves it out
// passes the split check and loses the quote: the trace diff fails its
// segment, which falls back to the stage 1 notes that have the quote.
func TestRun_LostQuoteFallsBack(t *testing.T) {
	src, folder := testfixture.LampRun(t)
	splitCheck := passingSplitCheck(t, src, folder)
	final := filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes))
	notes, err := note.ReadDir(final)
	if err != nil {
		t.Fatal(err)
	}
	var target *note.Note
	for _, n := range notes {
		if strings.HasPrefix(n.Name, "2 ") {
			target = n
			break
		}
	}
	text := target.Render() + "[^99]: " + testfixture.LampTrace("The Lens", "an unreferenced quote") + "\n"
	if err := os.WriteFile(filepath.Join(final, target.Name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := splitgates.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	wantFallback(t, src, folder, 2, "2 The Lens", tracediff.GateName, run.TraceDiffFile)
	dir := filepath.Join(folder.Dir, run.SplitGatesDir)
	if b, err := os.ReadFile(filepath.Join(dir, run.SplitCheckFile)); err != nil || string(b) != splitCheck {
		t.Errorf("split check report kept is not the passing one (err %v):\n%s", err, b)
	}
	b, err := os.ReadFile(filepath.Join(dir, run.TraceDiffFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "## 2 The Lens: 3 atomic notes; quotes 6->6\n  QUOTE LOST x1: an unreferenced quote\n") {
		t.Errorf("report kept does not give the lost quote:\n%s", b)
	}
}

// A segment folder the human deleted is checked as an empty folder, so the
// split check fails it on its lost traces rather than passing it unchecked,
// and it falls back.
func TestRun_MissingFolderFallsBack(t *testing.T) {
	src, folder := testfixture.LampRun(t)
	if err := os.RemoveAll(filepath.Join(folder.Dir, filepath.FromSlash(run.SplitNotes), "3 Storms")); err != nil {
		t.Fatal(err)
	}
	if err := splitgates.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	wantFallback(t, src, folder, 3, "3 Storms", splitcheck.GateName, run.SplitCheckFile)
	b, err := os.ReadFile(filepath.Join(folder.Dir, run.SplitGatesDir, run.SplitCheckFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\n## 3 Storms: 0 atomic notes, statements 3->0, ") {
		t.Errorf("report kept does not check the missing folder:\n%s", b)
	}
}

// A segment step 2.1 listed as fallen back, for a reply it couldn't write,
// has no folder. The gates don't check it, and it falls back with step
// 2.1's reason.
func TestRun_StepTwoOneFallbackFallsBack(t *testing.T) {
	src, folder := testfixture.LampRun(t)
	if err := os.RemoveAll(filepath.Join(folder.Dir, filepath.FromSlash(run.SplitNotes), "3 Storms")); err != nil {
		t.Fatal(err)
	}
	why := `step 2.1's reply wrote "X.md" twice`
	if err := os.WriteFile(filepath.Join(folder.Dir, run.SplitDir, run.FallbacksFile), []byte(`[{"segment": 3, "why": "step 2.1's reply wrote \"X.md\" twice"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := splitgates.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	wantFallback(t, src, folder, 3, "3 Storms", why, "")
	for _, file := range []string{run.SplitCheckFile, run.TraceDiffFile, run.StripCheckFile} {
		if b, err := os.ReadFile(filepath.Join(folder.Dir, run.SplitGatesDir, file)); err != nil || strings.Contains(string(b), "3 Storms") {
			t.Errorf("%s checked the segment step 2.1 fell back (err %v):\n%s", file, err, b)
		}
	}
}

// A fallback whose stage 1 note step 2.2 couldn't strip has nothing left to
// fall back to: its strip check stops the run, names the segment, and
// leaves the step partial with its report.
func TestRun_FallbackThatCantBeStrippedStops(t *testing.T) {
	src, folder := testfixture.LampRun(t)
	if err := os.RemoveAll(filepath.Join(folder.Dir, filepath.FromSlash(run.SplitNotes), "3 Storms")); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes), "3 Storms.md")
	b, err := os.ReadFile(final)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(final, []byte(strings.Replace(string(b), "# 3 Storms\n", "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	err = splitgates.Run(src, folder)
	var stopped *gate.Stopped
	if !errors.As(err, &stopped) {
		t.Fatalf("Run = %v, want a *gate.Stopped", err)
	}
	report := filepath.Join(folder.Dir, run.SplitGatesDir+run.PartialSuffix, run.FallbackStripCheckFile)
	if stopped.Gate != splitgates.FallbackGateName || !reflect.DeepEqual(stopped.Segments, []string{"segment 3 (Storms)"}) || stopped.Report != report {
		t.Errorf("stopped = %+v", stopped)
	}
	want := []gate.Item{{
		Kind:    striptraces.NoH1,
		Problem: "This atomic note has no H1 for step 2.2 to put the source footnote after.",
		Paths:   []string{final},
	}}
	if !reflect.DeepEqual(stopped.Items, want) {
		t.Errorf("Items = %+v, want %+v", stopped.Items, want)
	}
	if fix := stopped.Fixes[striptraces.NoH1]; !strings.Contains(fix, "falls back to this final stage 1 note") || !strings.HasSuffix(fix, "; or delete "+filepath.Join(folder.Dir, run.SplitDir)+" to split every segment again") {
		t.Errorf("fix %q does not say the note is a fallback's, or give the choice to split again", fix)
	}
	if folder.Complete(run.SplitGatesDir) {
		t.Error("the step is complete after the fallback's strip check failed")
	}
	if b, err := os.ReadFile(report); err != nil || !strings.Contains(string(b), "3 Storms: 3 Storms.md has no H1") {
		t.Errorf("report kept does not give the note with no H1 (err %v):\n%s", err, b)
	}
}

// A folder no segment claims would pass the gates, and its notes would reach
// no later step. The folder check stops the run before any gate, with no
// report, and its message alone names the folder, says why it fails, lists
// the segments' folders and says what to do.
func TestRun_UnclaimedFolderStops(t *testing.T) {
	src, folder := testfixture.LampRun(t)
	splitNotes := filepath.Join(folder.Dir, filepath.FromSlash(run.SplitNotes))
	hub := "# Extra\n\n- [[Some note]]\n"
	if err := os.Mkdir(filepath.Join(splitNotes, "99 Extra"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(splitNotes, "99 Extra", "Extra.md"), []byte(hub), 0o644); err != nil {
		t.Fatal(err)
	}
	stopped := wantUnclaimed(t, src, folder, "99 Extra")
	if !strings.Contains(stopped.Fix, "No segment of the source has the folder name of "+filepath.Join(splitNotes, "99 Extra")+".") {
		t.Errorf("fix does not name the folder's full path or say why it fails: %s", stopped.Fix)
	}
}

// A folder whose name has no space names no segment either, so it stops the
// run as a folder no segment claims.
func TestRun_FolderWithoutTitleStops(t *testing.T) {
	src, folder := testfixture.LampRun(t)
	if err := os.Mkdir(filepath.Join(folder.Dir, filepath.FromSlash(run.SplitNotes), "stray"), 0o755); err != nil {
		t.Fatal(err)
	}
	wantUnclaimed(t, src, folder, "stray")
}

// Two folders no segment claims stop the run once, naming both, each with
// its full path.
func TestRun_TwoUnclaimedFoldersStop(t *testing.T) {
	src, folder := testfixture.LampRun(t)
	splitNotes := filepath.Join(folder.Dir, filepath.FromSlash(run.SplitNotes))
	for _, name := range []string{"stray", "99 Extra"} {
		if err := os.Mkdir(filepath.Join(splitNotes, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	err := splitgates.Run(src, folder)
	var stopped *gate.Stopped
	if !errors.As(err, &stopped) {
		t.Fatalf("Run = %v, want a *gate.Stopped", err)
	}
	if want := []string{`folder "99 Extra"`, `folder "stray"`}; !reflect.DeepEqual(stopped.Segments, want) {
		t.Errorf("Segments = %q, want %q", stopped.Segments, want)
	}
	paths := filepath.Join(splitNotes, "99 Extra") + ", " + filepath.Join(splitNotes, "stray")
	if !strings.Contains(stopped.Fix, "No segment of the source has the folder name of "+paths+".") {
		t.Errorf("fix does not name both folders' full paths: %s", stopped.Fix)
	}
	if !strings.Contains(stopped.Fix, "Rename each to the folder of the segment its notes belong to") {
		t.Errorf("fix does not speak of each folder: %s", stopped.Fix)
	}
}

// wantUnclaimed runs the gates and checks that the folder check stops them
// on the folder name, with the step partial and no report.
func wantUnclaimed(t *testing.T, src *source.Source, folder *run.Folder, name string) *gate.Stopped {
	t.Helper()
	err := splitgates.Run(src, folder)
	var stopped *gate.Stopped
	if !errors.As(err, &stopped) {
		t.Fatalf("Run = %v, want a *gate.Stopped", err)
	}
	if stopped.Gate != splitgates.FolderGateName || !reflect.DeepEqual(stopped.Segments, []string{fmt.Sprintf("folder %q", name)}) || stopped.Report != "" {
		t.Errorf("stopped = %+v", stopped)
	}
	if want := fmt.Sprintf("%s failed on folder %q", splitgates.FolderGateName, name); err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	for _, seg := range src.Segments {
		if !strings.Contains(stopped.Fix, fmt.Sprintf("%q", split.Folder(seg))) {
			t.Errorf("fix does not list segment folder %q: %s", split.Folder(seg), stopped.Fix)
		}
	}
	if !strings.Contains(stopped.Fix, "Rename it to the folder of the segment its notes belong to, move its notes into that folder, or delete it") {
		t.Errorf("fix does not give the choices: %s", stopped.Fix)
	}
	if folder.Complete(run.SplitGatesDir) {
		t.Error("the step is complete after the folder check failed")
	}
	partial := filepath.Join(folder.Dir, run.SplitGatesDir+run.PartialSuffix)
	entries, err := os.ReadDir(partial)
	if err != nil {
		t.Fatalf("the step is not left partial: %v", err)
	}
	if len(entries) > 0 {
		t.Errorf("the partial step has %d entries, want no report", len(entries))
	}
	return stopped
}
