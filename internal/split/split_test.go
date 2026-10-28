package split_test

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/corygyarmathy/slipbox/internal/provider"
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

// segment is the key a fake matches a segment's call on: the label its user
// prompt gives the segment's notes.
func segment(n int) string { return fmt.Sprintf("=== SEGMENT %d STAGE 1 NOTES ===", n) }

// noCalls is a provider that fails t if it is called.
func noCalls(t *testing.T) *testfixture.Fake {
	return testfixture.NewFake(func(req provider.Request) (provider.Reply, error) {
		t.Errorf("unexpected call: %.80q", req.User)
		return provider.Reply{}, errors.New("unexpected call")
	})
}

// TestPrompt pins each segment's user prompt on the invented chapter: its
// own final stage 1 notes, the aside among them, then the hub title and the
// source's name.
func TestPrompt(t *testing.T) {
	src, notes := testfixture.LampSource(t), testfixture.LampStage1Notes(t)
	for _, seg := range src.Segments {
		got, ok := split.Prompt(src, notes, seg)
		if !ok {
			t.Fatalf("%s: no prompt, but it has notes", seg)
		}
		testfixture.Golden(t, "prompt/"+strconv.Itoa(seg.Number)+".txt", got)
		// The golden files can't say why a note is in the prompt: it is one
		// of the segment's own.
		for _, n := range notes {
			in := strings.Contains(got, "--- NOTE: "+n.Name+" ---\n")
			own := strings.HasPrefix(n.Name, strconv.Itoa(seg.Number)+" ")
			if in != own {
				t.Errorf("%s: %q in the prompt: %v, want %v", seg, n.Name, in, own)
			}
		}
	}
	// A segment with no notes has no prompt, so no call.
	var others []*note.Note
	for _, n := range notes {
		if !strings.HasPrefix(n.Name, "3 ") {
			others = append(others, n)
		}
	}
	if got, ok := split.Prompt(src, others, src.Segments[3]); ok {
		t.Errorf("%s has no notes, but a prompt:\n%s", src.Segments[3], got)
	}
}

// file is a reply's `=== FILE: <name> ===` block holding body.
func file(name, body string) string { return "=== FILE: " + name + " ===\n" + body }

// lampNote is the body of the note at path in LampSplit, a split the
// step's tests give as a reply.
func lampNote(path string) string { return testfixture.LampSplit()[path] }

// replyCase is a segment's reply in TestRun, and what the step should write
// for it.
type replyCase struct {
	name    string
	segment int
	reply   string
	want    written
}

// written is what step 2.1 writes for a segment: its notes, by file name in
// its folder, and its manifest; or, for a reply that falls back, neither,
// and why it falls back.
type written struct {
	notes    map[string]string
	manifest string
	fallback string
}

// replyCases are the replies of the invented chapter's four segments, one
// call each.
var replyCases = []replyCase{
	// The well-formed shape: the atomic notes, the hub and a manifest, each
	// written as given.
	{"well-formed", 0,
		file("A Lighthouse Is a Promise.md", lampNote("0 Keepers of the Lamp/A Lighthouse Is a Promise.md")) +
			file("The Keeper Kept the Promise.md", lampNote("0 Keepers of the Lamp/The Keeper Kept the Promise.md")) +
			file("Keepers of the Lamp.md", lampNote("0 Keepers of the Lamp/Keepers of the Lamp.md")) +
			"=== MANIFEST ===\n{\"segment\": \"2. Keepers of the Lamp\", \"edited_statements\": []}\n",
		written{
			notes: map[string]string{
				"A Lighthouse Is a Promise.md":   lampNote("0 Keepers of the Lamp/A Lighthouse Is a Promise.md"),
				"The Keeper Kept the Promise.md": lampNote("0 Keepers of the Lamp/The Keeper Kept the Promise.md"),
				"Keepers of the Lamp.md":         lampNote("0 Keepers of the Lamp/Keepers of the Lamp.md"),
			},
			manifest: "{\"segment\": \"2. Keepers of the Lamp\", \"edited_statements\": []}\n",
		}},
	// A preamble before the first file, which is no note; a note named
	// without `.md`, which is written with it; the aside's note beside the
	// main note's; and a manifest after a blank line with no trailing
	// newline, kept as the runner parses its block: trimmed, with one
	// trailing newline.
	{"a preamble, a name without .md, the aside, and a manifest with no trailing newline", 1,
		"Here is the split.\n\n" +
			file("The Watch Began at Dusk", lampNote("1 The Keeper's Watch/The Watch Began at Dusk.md")) +
			file("The Log Was Read Only After a Wreck.md", lampNote("1 The Keeper's Watch/The Log Was Read Only After a Wreck.md")) +
			file("The Keeper's Watch.md", lampNote("1 The Keeper's Watch/The Keeper's Watch.md")) +
			"=== MANIFEST ===\n\n{\"segment\": \"The Keeper’s Watch\"}",
		written{
			notes: map[string]string{
				"The Watch Began at Dusk.md":             lampNote("1 The Keeper's Watch/The Watch Began at Dusk.md"),
				"The Log Was Read Only After a Wreck.md": lampNote("1 The Keeper's Watch/The Log Was Read Only After a Wreck.md"),
				"The Keeper's Watch.md":                  lampNote("1 The Keeper's Watch/The Keeper's Watch.md"),
			},
			manifest: "{\"segment\": \"The Keeper’s Watch\"}\n",
		}},
	// A hub under a name that isn't the segment's title falls back, though
	// the reply has a note besides its atomic ones: the hub is the note named
	// `<title>.md`. The other segments are written.
	{"a hub under another name falls back", 2,
		file("The Lens Makes a Beam.md", lampNote("2 The Lens/The Lens Makes a Beam.md")) +
			file("Rings of Prisms.md", lampNote("2 The Lens/Rings of Prisms.md")) +
			file("Lens.md", lampNote("2 The Lens/The Lens.md")) +
			"=== MANIFEST ===\n{}\n",
		written{fallback: `step 2.1's reply wrote no hub note "The Lens.md"`}},
	// A note given twice under one name is written once, with the later
	// body: a model that writes a note again means the later version.
	{"a note given twice under one name", 3,
		file("In a Storm the Light Matters Most.md", "# In a Storm the Light Matters Most\n\nA first draft.\n") +
			file("The Keeper Stayed Through Every Gale.md", lampNote("3 Storms/The Keeper Stayed Through Every Gale.md")) +
			file("In a Storm the Light Matters Most.md", lampNote("3 Storms/In a Storm the Light Matters Most.md")) +
			file("Storms.md", lampNote("3 Storms/Storms.md")) +
			"=== MANIFEST ===\n{}\n",
		written{
			notes: map[string]string{
				"In a Storm the Light Matters Most.md":    lampNote("3 Storms/In a Storm the Light Matters Most.md"),
				"The Keeper Stayed Through Every Gale.md": lampNote("3 Storms/The Keeper Stayed Through Every Gale.md"),
				"Storms.md": lampNote("3 Storms/Storms.md"),
			},
			manifest: "{}\n",
		}},
}

// TestRun runs step 2.1 on the invented chapter's final stage 1 notes with
// replyCases, and checks the calls it made and what it wrote.
func TestRun(t *testing.T) {
	src := testfixture.LampSource(t)
	folder := atFinal(t, src, testfixture.LampStage1())
	var cases []testfixture.Case
	for _, c := range replyCases {
		cases = append(cases, testfixture.Case{Contains: segment(c.segment), Reply: c.reply})
	}
	p := testfixture.Serve(t, cases...)
	if err := split.Run(context.Background(), p, src, split.DefaultModel, folder); err != nil {
		t.Fatal(err)
	}

	files := testfixture.Files(t, filepath.Join(folder.Dir, run.SplitDir))
	items, err := fallback.Read(folder, run.SplitDir)
	if err != nil {
		t.Fatal(err)
	}
	var wantItems []fallback.Item
	wantFolders := map[string]bool{}
	for _, c := range replyCases {
		seg := src.Segments[c.segment]
		n := strconv.Itoa(c.segment)
		if c.want.fallback != "" {
			wantItems = append(wantItems, fallback.Item{Segment: c.segment, Why: c.want.fallback})
		} else {
			wantFolders[split.Folder(seg)] = true
		}
		t.Run(c.name, func(t *testing.T) {
			if got := files[run.RawDir+"/"+n+".txt"]; got != c.reply {
				t.Errorf("raw reply is not the reply:\n got %q\nwant %q", got, c.reply)
			}
			if got := under(files, run.Stage2NotesDir+"/"+split.Folder(seg)); !maps.Equal(got, c.want.notes) {
				t.Errorf("notes in %s/:\n got %q\nwant %q", split.Folder(seg), got, c.want.notes)
			}
			manifest, ok := files[run.ManifestsDir+"/"+n+".json"]
			if c.want.fallback != "" && ok {
				t.Errorf("a reply that falls back has a manifest %q", manifest)
			}
			if c.want.fallback == "" && manifest != c.want.manifest {
				t.Errorf("manifest %q, want %q", manifest, c.want.manifest)
			}
			var why string
			for _, it := range items {
				if it.Segment == c.segment {
					why = it.Why
				}
			}
			if why != c.want.fallback {
				t.Errorf("falls back for %q, want %q", why, c.want.fallback)
			}
		})
	}
	// Only the segments whose replies don't fall back have folders, and the
	// fallbacks are listed in segment order.
	got := map[string]bool{}
	for path := range under(files, run.Stage2NotesDir) {
		dir, _, _ := strings.Cut(path, "/")
		got[dir] = true
	}
	if !reflect.DeepEqual(got, wantFolders) {
		t.Errorf("%s/ holds folders %v, want %v", run.Stage2NotesDir, slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(wantFolders)))
	}
	if !reflect.DeepEqual(items, wantItems) {
		t.Errorf("fallbacks %+v, want %+v", items, wantItems)
	}

	// One call per segment, segment 0's first, each with its prompt,
	// briefed by the stage 2 brief and the frame, at sonnet and medium. The
	// system prompt is built here rather than with modelstep.SystemPrompt,
	// so a fault there still shows.
	wantSystem := testfixture.RepoFile(t, "internal/briefs/stage-2.md") +
		"\n\n=== FRAME: " + testfixture.Lamp + ".md ===\n" + testfixture.LampChapter + "\n=== END FRAME ===\n"
	calls := p.Calls()
	if len(calls) != len(src.Segments) {
		t.Fatalf("%d calls, want one per segment (%d)", len(calls), len(src.Segments))
	}
	users := map[string]bool{}
	for _, c := range calls {
		if c.System != wantSystem {
			t.Errorf("call for %.80q: system prompt is not stage-2.md and the frame", c.User)
		}
		if c.Model != "sonnet" || c.Effort != "medium" {
			t.Errorf("call for %.80q: model %q at effort %q, want sonnet at medium", c.User, c.Model, c.Effort)
		}
		users[c.User] = true
	}
	notes := testfixture.LampStage1Notes(t)
	for _, seg := range src.Segments {
		if want, _ := split.Prompt(src, notes, seg); !users[want] {
			t.Errorf("%s: no call has its prompt %q", seg, want)
		}
	}
	if !strings.Contains(calls[0].User, segment(0)) {
		t.Errorf("first call was not segment 0's: %.80q", calls[0].User)
	}
	if n := strings.Count(files[run.UsageFile], `"brief":"stage-2.md"`); n != len(src.Segments) {
		t.Errorf("usage log names stage-2.md %d times, want once per segment:\n%s", n, files[run.UsageFile])
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
	p := testfixture.Serve(t, testfixture.Case{Contains: segment(0), Reply: reply})
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
		p := testfixture.Serve(t, testfixture.Case{Contains: segment(0), Reply: c.reply})
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
			if err := split.Run(context.Background(), noCalls(t), src, split.DefaultModel, folder); err != nil {
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
	p := noCalls(t)
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
	p := testfixture.Serve(t, testfixture.Case{Contains: segment(0), Reply: reply})
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
