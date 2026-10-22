package split_test

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/fallback"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/splitgates"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// atFinal makes a run folder for src whose final stage 1 notes are notes,
// keyed by file name.
func atFinal(t *testing.T, src *source.Source, notes map[string]string) *run.Folder {
	t.Helper()
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	if err := folder.SaveSource(src.Name, []byte(src.Frame)); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range notes {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return folder
}

// stage2Prompt is segment n's user prompt as the run built it, from the
// notes' file text: the notes block of the notes named `<n> *.md` in
// sorted order, then the hub title and the source's name.
func stage2Prompt(notes map[string]string, src *source.Source, n int) string {
	var names []string
	for name := range notes {
		if strings.HasPrefix(name, strconv.Itoa(n)+" ") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	label := "SEGMENT " + strconv.Itoa(n) + " STAGE 1 NOTES"
	p := "=== " + label + " ===\n"
	for _, name := range names {
		p += "--- NOTE: " + name + " ---\n" + notes[name] + "\n"
	}
	p += "=== END " + label + " ===\n\n"
	return p + "Hub title: `" + src.Segments[n].Title + "`. Source file name for traces: `" + src.Name + "`."
}

// TestRun_ReplaysThePragmatismSplit runs step 2.1 on the Pragmatism run's
// final stage 1 notes with the run's recorded replies. Each call is the
// request the run made, so each segment's folder holds the run's notes byte
// for byte, its manifest is kept, and no segment falls back. Every call is
// sent stage2Prompt's prompt, and the stage 2 brief and the frame as its
// system prompt, at the step's tier and effort.
func TestRun_ReplaysThePragmatismSplit(t *testing.T) {
	src, recorded := testfixture.PragmatismSource(t), testfixture.PragmatismRun(t)
	folder := testfixture.PragmatismFolder(t, run.FinalDir)
	notes := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes)))
	var prompts []string
	for n := range src.Segments {
		prompts = append(prompts, stage2Prompt(notes, src, n))
	}
	step := filepath.Join(recorded, run.SplitDir)
	p := testfixture.Replay(t, prompts, testfixture.RawReplies(t, step, len(src.Segments)))
	if err := split.Run(context.Background(), p, src, split.DefaultModel, folder); err != nil {
		t.Fatal(err)
	}
	testfixture.SameRequests(t, filepath.Join(folder.Dir, run.SplitDir), step, "")

	calls := p.Calls()
	if len(calls) != len(src.Segments) {
		t.Errorf("%d calls, want one per segment", len(calls))
	}
	frame := testfixture.PragmatismFrame(t)
	wantSystem := testfixture.RepoFile(t, "internal/briefs/stage-2.md") + "\n\n=== FRAME: " + testfixture.Pragmatism + ".md ===\n" + frame + "\n=== END FRAME ===\n"
	for _, c := range calls {
		if c.Model != "sonnet" || c.Effort != "medium" || c.System != wantSystem {
			t.Errorf("call for %.40q: model %q at %q, or its system prompt is not stage-2.md and the frame", c.User, c.Model, c.Effort)
		}
	}

	got := testfixture.Files(t, filepath.Join(folder.Dir, run.SplitDir))
	want := testfixture.Files(t, step)
	for _, dir := range []string{run.Stage2NotesDir, run.ManifestsDir, run.RawDir} {
		g, w := under(got, dir), under(want, dir)
		if !reflect.DeepEqual(slices.Sorted(maps.Keys(g)), slices.Sorted(maps.Keys(w))) {
			t.Errorf("%s/ holds %q, want the run's %q", dir, slices.Sorted(maps.Keys(g)), slices.Sorted(maps.Keys(w)))
			continue
		}
		for name, text := range w {
			if g[name] != text {
				t.Errorf("%s/%s is not the run's", dir, name)
			}
		}
	}
	if n := len(under(got, run.Stage2NotesDir)); n != 64 {
		t.Errorf("%d notes, want the run's 56 atomic notes and 8 hubs", n)
	}
	if items, err := fallback.Read(folder, run.SplitDir); err != nil || len(items) != 0 {
		t.Errorf("fallbacks %+v (err %v), want none", items, err)
	}
	if n := strings.Count(got[run.UsageFile], `"brief":"stage-2.md"`); n != len(src.Segments) {
		t.Errorf("usage log names stage-2.md %d times, want once per segment:\n%s", n, got[run.UsageFile])
	}
}

// under returns the entries of files under dir/, keyed by the rest of the
// path.
func under(files map[string]string, dir string) map[string]string {
	out := map[string]string{}
	for k, v := range files {
		if rest, ok := strings.CutPrefix(k, dir+"/"); ok {
			out[rest] = v
		}
	}
	return out
}

const tinySource = `---
source: "[[@Tiny - A. Author]]"
---
# 1. Tiny

Alpha is first.

## Two

Gamma is third.
`

const tinyNote0 = "# 0 Tiny\n\nAlpha is first.[^1]\n\n[^1]: [[Tiny#1. Tiny]] \"Alpha is first.\"\n"

// A segment with no final stage 1 notes gets no call, and no folder. The
// step calls the model it is given.
func TestRun_SkipsASegmentWithNoNotes(t *testing.T) {
	src, err := source.Parse("Tiny", tinySource)
	if err != nil {
		t.Fatal(err)
	}
	notes := map[string]string{"0 Tiny.md": tinyNote0}
	folder := atFinal(t, src, notes)
	// The manifest ends with no newline, as some of the model's do.
	reply := "=== FILE: Alpha Is First.md ===\nalpha\n=== FILE: Tiny.md ===\nhub\n=== MANIFEST ===\n\n{}"
	p := testfixture.Replay(t, []string{stage2Prompt(notes, src, 0)}, []string{reply})
	if err := split.Run(context.Background(), p, src, "opus", folder); err != nil {
		t.Fatal(err)
	}
	if c := p.Calls(); len(c) != 1 || c[0].Model != "opus" {
		t.Fatalf("calls %+v, want one, to the model asked for", c)
	}
	got := testfixture.Files(t, filepath.Join(folder.Dir, run.SplitDir))
	// The reply's manifest is kept as the runner parses its blocks: trimmed,
	// with one trailing newline.
	if m := got["manifests/0.json"]; m != "{}\n" {
		t.Errorf("manifest %q, want the reply's trimmed, with one trailing newline", m)
	}
	want := []string{"fallbacks.json", "manifests/0.json", "raw/0.txt", "requests/0.sha256", "stage-2/0 Tiny/Alpha Is First.md", "stage-2/0 Tiny/Tiny.md", "usage.jsonl"}
	if keys := slices.Sorted(maps.Keys(got)); !reflect.DeepEqual(keys, want) {
		t.Errorf("step wrote %q, want %q", keys, want)
	}
}

// A reply that isn't whole notes with plain names, or that leaves out its
// hub or every atomic note, falls back: the step completes with no folder or
// manifest for its segment, and lists the segment and the reply's fault. The
// reply is finished, so a resume of a stopped step falls back again with no
// second call: a fallback has no retry.
func TestRun_ABadReplyFallsBack(t *testing.T) {
	src, err := source.Parse("Tiny", tinySource)
	if err != nil {
		t.Fatal(err)
	}
	notes := map[string]string{"0 Tiny.md": tinyNote0}
	for _, c := range []struct{ reply, why string }{
		{"=== FILE: ../Escape.md ===\nx\n=== MANIFEST ===\n{}\n", `step 2.1's reply named a note "../Escape.md", not a plain file name`},
		{"=== DELETE: 0 Tiny.md ===\n=== MANIFEST ===\n{}\n", `step 2.1's reply gave "0 Tiny.md" as a delete or edits, not a whole note`},
		{"=== EDIT: 0 Tiny.md ===\nx\n=== MANIFEST ===\n{}\n", `step 2.1's reply gave "0 Tiny.md" as a delete or edits, not a whole note`},
		{"=== FILE: Alpha ===\nx\n=== FILE: Alpha.md ===\ny\n=== FILE: Tiny.md ===\nhub\n=== MANIFEST ===\n{}\n", `step 2.1's reply wrote "Alpha.md" twice`},
		{"=== FILE: Alpha Is First.md ===\nalpha\n=== MANIFEST ===\n{}\n", `step 2.1's reply wrote no hub note "Tiny.md"`},
		{"=== FILE: Alpha Is First.md ===\nalpha\n=== FILE: Hub.md ===\nhub\n=== MANIFEST ===\n{}\n", `step 2.1's reply wrote no hub note "Tiny.md"`},
		{"=== FILE: Tiny ===\nhub\n=== MANIFEST ===\n{}\n", "step 2.1's reply wrote no atomic note"},
	} {
		folder := atFinal(t, src, notes)
		p := testfixture.Replay(t, []string{stage2Prompt(notes, src, 0)}, []string{c.reply})
		if err := split.Run(context.Background(), p, src, split.DefaultModel, folder); err != nil {
			t.Errorf("reply %q: %v", c.reply, err)
			continue
		}
		got := testfixture.Files(t, filepath.Join(folder.Dir, run.SplitDir))
		want := []string{"fallbacks.json", "raw/0.txt", "requests/0.sha256", "usage.jsonl"}
		if keys := slices.Sorted(maps.Keys(got)); !reflect.DeepEqual(keys, want) {
			t.Errorf("reply %q: step wrote %q, want %q", c.reply, keys, want)
		}
		for attempt := range 2 {
			items, err := fallback.Read(folder, run.SplitDir)
			if err != nil {
				t.Fatal(err)
			}
			if want := []fallback.Item{{Segment: 0, Why: c.why}}; !reflect.DeepEqual(items, want) {
				t.Errorf("reply %q, attempt %d: fallbacks %+v, want %+v", c.reply, attempt+1, items, want)
			}
			if attempt == 1 {
				break
			}
			step := filepath.Join(folder.Dir, run.SplitDir)
			if err := os.Rename(step, step+run.PartialSuffix); err != nil {
				t.Fatal(err)
			}
			if err := split.Run(context.Background(), testfixture.Replay(t, nil, nil), src, split.DefaultModel, folder); err != nil {
				t.Fatalf("reply %q: resume: %v", c.reply, err)
			}
		}
	}
}

// A fallback is the segment's final stage 1 notes as they are, and a hub
// titled as step 2.1's, with each aside nested under its note, as the brief
// nests them.
func TestFallback(t *testing.T) {
	src, err := source.Parse("Tiny", tinySource)
	if err != nil {
		t.Fatal(err)
	}
	main := note.Parse("0 Tiny.md", tinyNote0)
	aside := note.Parse("0 Tiny - Aside - Beta.md", "# 0 Tiny - Aside - Beta\n\nAside.\n")
	other := note.Parse("1 Two.md", "# 1 Two\n")
	f := split.Fallback(src.Segments[0], []*note.Note{aside, main, other})
	if f.Name != "0 Tiny" {
		t.Errorf("folder %q, want %q", f.Name, "0 Tiny")
	}
	var names []string
	for _, n := range f.Notes {
		names = append(names, n.Name)
	}
	if want := []string{"0 Tiny - Aside - Beta.md", "0 Tiny.md", "Tiny.md"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("notes %q, want %q", names, want)
	}
	if f.Notes[1] != main || f.Notes[0] != aside {
		t.Errorf("the stage 1 notes are not as they were")
	}
	want := "---\naliases: []\ntags: []\n---\n\n# Tiny\n\n1. [[0 Tiny]]\n\t- Aside: [[0 Tiny - Aside - Beta]]\n"
	if got := f.Notes[2].Render(); got != want {
		t.Errorf("hub:\n%s\nwant:\n%s", got, want)
	}
	if !f.IsHub(f.Notes[2]) {
		t.Errorf("the hub is not the folder's hub")
	}
}

// A segment whose title can't name a folder fails the step before any call.
func TestRun_RefusesATitleThatCantNameAFolder(t *testing.T) {
	src, err := source.Parse("Tiny", strings.Replace(tinySource, "# 1. Tiny", "# 1. Input/Output", 1))
	if err != nil {
		t.Fatal(err)
	}
	notes := map[string]string{"0 Tiny.md": tinyNote0}
	folder := atFinal(t, src, notes)
	p := testfixture.Replay(t, nil, nil)
	if err := split.Run(context.Background(), p, src, split.DefaultModel, folder); err == nil {
		t.Errorf("no error")
	}
	if c := p.Calls(); len(c) != 0 {
		t.Errorf("%d calls, want none", len(c))
	}
	if folder.Complete(run.SplitDir) {
		t.Errorf("step complete")
	}
}

// A failing folder that names no segment of the source is named after its
// folder, after the segments. A segment is named by its heading as the
// source gives it, curly apostrophe and all.
func TestFailingSegments(t *testing.T) {
	src, err := source.Parse("Tiny", strings.Replace(tinySource, "## Two", "## The Lamp’s Wick", 1))
	if err != nil {
		t.Fatal(err)
	}
	folders := []string{"9 Stray", "1 The Lamp's Wick", "0 Tiny"}
	want := []string{"segment 0 (1. Tiny)", "segment 1 (The Lamp’s Wick)", `folder "9 Stray"`}
	if got := split.FailingSegments(src, folders); !reflect.DeepEqual(got, want) {
		t.Errorf("FailingSegments = %q, want %q", got, want)
	}
}

// When every segment's reply falls back, step 2.1 still writes its
// `stage-2/`, empty, so its gates give every segment its fallback rather
// than failing to read it.
func TestRun_EveryReplyFallsBackThenGates(t *testing.T) {
	src, err := source.Parse("Tiny", tinySource)
	if err != nil {
		t.Fatal(err)
	}
	notes := map[string]string{"0 Tiny.md": tinyNote0}
	folder := atFinal(t, src, notes)
	reply := "=== FILE: Alpha Is First.md ===\nalpha\n=== MANIFEST ===\n{}\n"
	p := testfixture.Replay(t, []string{stage2Prompt(notes, src, 0)}, []string{reply})
	if err := split.Run(context.Background(), p, src, split.DefaultModel, folder); err != nil {
		t.Fatal(err)
	}
	if err := splitgates.Run(src, folder); err != nil {
		t.Fatalf("splitgates.Run: %v", err)
	}
	got := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.SplitOrFallbackNotes)))
	if want := []string{"0 Tiny/0 Tiny.md", "0 Tiny/Tiny.md"}; !reflect.DeepEqual(slices.Sorted(maps.Keys(got)), want) {
		t.Errorf("notes that go on %q, want %q", slices.Sorted(maps.Keys(got)), want)
	}
}

// ReadFolders gives the segment folders in byte order of their names. A
// file, or a hidden folder such as `.trash`, is not a segment folder.
func TestReadFolders_SortsSkippingFilesAndHiddenFolders(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"2 B", "10 Z", ".trash", "1 A"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "0 Note.md"), []byte("# x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	folders, err := split.ReadFolders(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range folders {
		names = append(names, f.Name)
	}
	if want := []string{"1 A", "10 Z", "2 B"}; !reflect.DeepEqual(names, want) {
		t.Errorf("folders = %q, want %q", names, want)
	}
}
