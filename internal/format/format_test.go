package format_test

import (
	"context"
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
	"github.com/corygyarmathy/slipbox/internal/format"
	"github.com/corygyarmathy/slipbox/internal/formatcheck"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// atStripped makes a run folder for src whose step 2.2 is complete, with
// stripped, keyed by `<n> <title>/<note>`, as its stripped notes.
func atStripped(t *testing.T, src *source.Source, stripped map[string]string) *run.Folder {
	t.Helper()
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	if err := folder.SaveSource(src.Name, []byte(src.Frame)); err != nil {
		t.Fatal(err)
	}
	testfixture.WriteFiles(t, filepath.Join(folder.Dir, filepath.FromSlash(run.StrippedNotes)), stripped)
	return folder
}

// segment is the key a fake matches a segment's call on: the label its user
// prompt gives the segment's notes.
func segment(n int) string { return fmt.Sprintf("=== SEGMENT %d STAGE 2 NOTES ===", n) }

// TestPrompt pins each segment's user prompt on the invented split, its
// traces stripped: every note of the segment's folder, the hub among them.
func TestPrompt(t *testing.T) {
	stripped := testfixture.LampStripped(t)
	for _, f := range testfixture.LampFolders(t, stripped) {
		number, _, _ := strings.Cut(f.Name, " ")
		got := format.Prompt(f)
		testfixture.Golden(t, "prompt/"+number+".txt", got)
		// The golden files can't say why a note is in the prompt: it is in
		// the segment's folder.
		for rel := range stripped {
			dir, name, _ := strings.Cut(rel, "/")
			if in, own := strings.Contains(got, "--- NOTE: "+name+" ---\n"), dir == f.Name; in != own {
				t.Errorf("%s: %q in the prompt: %v, want %v", f.Name, rel, in, own)
			}
		}
	}
}

// file is a reply's `=== FILE: <name> ===` block holding body.
func file(name, body string) string { return "=== FILE: " + name + " ===\n" + body }

// lampNote is the body of the note at path in LampStripped, the notes the
// step is given, which a reply that changes nothing gives back.
func lampNote(t *testing.T, path string) string {
	t.Helper()
	body, ok := testfixture.LampStripped(t)[path]
	if !ok {
		t.Fatalf("LampStripped has no %q", path)
	}
	return body
}

// replyCase is a segment's reply in TestRun, and what the step should write
// for it.
type replyCase struct {
	name    string
	segment int
	reply   string
	want    written
}

// written is what step 3.1 writes for a segment: its notes, by file name in
// its folder, and its manifest; or, for a reply that falls back, neither,
// and why it falls back.
type written struct {
	notes    map[string]string
	manifest string
	fallback string
}

// fresnel is `2 The Lens/Rings of Prisms.md` formatted: the source's
// emphasis given as `_x_`, and its frontmatter kept.
const fresnel = "---\ntags: [lighthouses]\n---\n# Rings of Prisms[^1]\n\n" +
	"_The Fresnel lens_ uses rings of prisms instead of one thick glass.\n\n" +
	"[^1]: " + testfixture.LampCitekeyLink + "\n"

// fresnelManifest is the manifest of the change fresnel makes.
const fresnelManifest = `{"changes": [{"note": "Rings of Prisms", "element": "_x_", "what": "*x* to _x_", "judgement": "mechanical", "why": "the source italicises it"}]}`

// mustGiveBack ends the fault of a reply whose notes aren't its segment's.
const mustGiveBack = ", but it must give back every note of its segment, as named"

// replyCases are the replies of the invented chapter's four segments, one
// call each.
func replyCases(t *testing.T) []replyCase {
	return []replyCase{
		// A note given back under another name falls back, though the reply
		// gives as many notes as the segment has: the notes are matched by
		// name, not counted.
		{"a renamed note falls back, though the count matches", 0,
			file("A Lighthouse Is a Promise.md", lampNote(t, "0 Keepers of the Lamp/A Lighthouse Is a Promise.md")) +
				file("Keepers of the Lamp.md", lampNote(t, "0 Keepers of the Lamp/Keepers of the Lamp.md")) +
				file("The Keeper Kept His Promise.md", strings.Replace(lampNote(t, "0 Keepers of the Lamp/The Keeper Kept the Promise.md"), "Kept the", "Kept His", 1)) +
				"=== MANIFEST ===\n{\"changes\": []}\n",
			written{fallback: `step 3.1's reply left out ["The Keeper Kept the Promise.md"] and wrote ["The Keeper Kept His Promise.md"], not notes of the segment` + mustGiveBack}},
		// "No change" is a correct result: every note is written as it was.
		// A preamble before the first file is no note; a note named without
		// `.md` is written with it; the hub may come first, out of the
		// prompt's order; a note given twice under one name is written once,
		// with the later body; and a manifest with no trailing newline is
		// kept as the runner parses its block: trimmed, with one trailing
		// newline.
		{"no change, a preamble, a name without .md, the hub first, a note given twice, and a manifest with no trailing newline", 1,
			"No markup to change.\n\n" +
				file("The Keeper's Watch.md", lampNote(t, "1 The Keeper's Watch/The Keeper's Watch.md")) +
				file("The Log Was Read Only After a Wreck.md", "# The Log Was Read Only After a Wreck[^1]\n\nA first draft.\n") +
				file("The Watch Began at Dusk", lampNote(t, "1 The Keeper's Watch/The Watch Began at Dusk.md")) +
				file("The Log Was Read Only After a Wreck.md", lampNote(t, "1 The Keeper's Watch/The Log Was Read Only After a Wreck.md")) +
				"=== MANIFEST ===\n\n{\"changes\": []}",
			written{
				notes: map[string]string{
					"The Keeper's Watch.md":                  lampNote(t, "1 The Keeper's Watch/The Keeper's Watch.md"),
					"The Log Was Read Only After a Wreck.md": lampNote(t, "1 The Keeper's Watch/The Log Was Read Only After a Wreck.md"),
					"The Watch Began at Dusk.md":             lampNote(t, "1 The Keeper's Watch/The Watch Began at Dusk.md"),
				},
				manifest: "{\"changes\": []}\n",
			}},
		// The well-formed shape: every note, one with its markup changed,
		// and a manifest, each written as given.
		{"well-formed", 2,
			file("The Lens.md", lampNote(t, "2 The Lens/The Lens.md")) +
				file("The Lens Makes a Beam.md", lampNote(t, "2 The Lens/The Lens Makes a Beam.md")) +
				file("A Keeper Need Not Watch the Lens.md", lampNote(t, "2 The Lens/A Keeper Need Not Watch the Lens.md")) +
				file("Rings of Prisms.md", fresnel) +
				"=== MANIFEST ===\n" + fresnelManifest + "\n",
			written{
				notes: map[string]string{
					"The Lens.md":                         lampNote(t, "2 The Lens/The Lens.md"),
					"The Lens Makes a Beam.md":            lampNote(t, "2 The Lens/The Lens Makes a Beam.md"),
					"A Keeper Need Not Watch the Lens.md": lampNote(t, "2 The Lens/A Keeper Need Not Watch the Lens.md"),
					"Rings of Prisms.md":                  fresnel,
				},
				manifest: fresnelManifest + "\n",
			}},
		// A note of another segment falls back, though the run has a note
		// of that name: a reply may give back only its own segment's notes.
		{"another segment's note falls back", 3,
			file("Storms.md", lampNote(t, "3 Storms/Storms.md")) +
				file("In a Storm the Light Matters Most.md", lampNote(t, "3 Storms/In a Storm the Light Matters Most.md")) +
				file("The Keeper Stayed Through Every Gale.md", lampNote(t, "3 Storms/The Keeper Stayed Through Every Gale.md")) +
				file("The Lens.md", lampNote(t, "2 The Lens/The Lens.md")) +
				"=== MANIFEST ===\n{\"changes\": []}\n",
			written{fallback: `step 3.1's reply wrote ["The Lens.md"], not notes of the segment` + mustGiveBack}},
	}
}

// TestRun runs step 3.1 on the invented split, its traces stripped, with
// replyCases, and checks the calls it made and what it wrote.
func TestRun(t *testing.T) {
	src := testfixture.LampSource(t)
	stripped := testfixture.LampStripped(t)
	folder := atStripped(t, src, stripped)
	cases := replyCases(t)
	var serve []testfixture.Case
	for _, c := range cases {
		serve = append(serve, testfixture.Case{Contains: segment(c.segment), Reply: c.reply})
	}
	p := testfixture.Serve(t, serve...)
	if err := format.Run(context.Background(), p, src, format.DefaultModel, folder); err != nil {
		t.Fatal(err)
	}

	files := testfixture.Files(t, filepath.Join(folder.Dir, run.FormatDir))
	items, err := fallback.Read(folder, run.FormatDir)
	if err != nil {
		t.Fatal(err)
	}
	var wantItems []fallback.Item
	wantFolders := map[string]bool{}
	for _, c := range cases {
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
			if got := under(files, run.Stage3NotesDir+"/"+split.Folder(seg)); !maps.Equal(got, c.want.notes) {
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
	// Only the segments whose replies don't fall back have folders, and only
	// segments 0 and 3 are listed as fallbacks, in whatever order.
	got := map[string]bool{}
	for path := range under(files, run.Stage3NotesDir) {
		dir, _, _ := strings.Cut(path, "/")
		got[dir] = true
	}
	if !reflect.DeepEqual(got, wantFolders) {
		t.Errorf("%s/ holds folders %v, want %v", run.Stage3NotesDir, slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(wantFolders)))
	}
	bySegment := func(a, b fallback.Item) int { return a.Segment - b.Segment }
	if slices.SortFunc(items, bySegment); !reflect.DeepEqual(items, wantItems) {
		t.Errorf("fallbacks %+v, want %+v", items, wantItems)
	}

	// One call per segment, segment 0's first, each with its prompt,
	// briefed by the stage 3 brief and the frame, at haiku and medium. The
	// system prompt is built here rather than with modelstep.SystemPrompt,
	// so a fault there still shows.
	wantSystem := testfixture.RepoFile(t, "internal/briefs/stage-3.md") +
		"\n\n" + testfixture.LampFrame
	calls := p.Calls()
	if len(calls) != len(src.Segments) {
		t.Fatalf("%d calls, want one per segment (%d)", len(calls), len(src.Segments))
	}
	users := map[string]bool{}
	for _, c := range calls {
		if c.System != wantSystem {
			t.Errorf("call for %.80q: system prompt is not stage-3.md and the frame", c.User)
		}
		if c.Model != "haiku" || c.Effort != "medium" {
			t.Errorf("call for %.80q: model %q at effort %q, want haiku at medium", c.User, c.Model, c.Effort)
		}
		users[c.User] = true
	}
	for _, f := range testfixture.LampFolders(t, stripped) {
		if want := format.Prompt(f); !users[want] {
			t.Errorf("%s: no call has its prompt %q", f.Name, want)
		}
	}
	if !strings.Contains(calls[0].User, segment(0)) {
		t.Errorf("first call was not segment 0's: %.80q", calls[0].User)
	}
	if n := strings.Count(files[run.UsageFile], `"brief":"stage-3.md"`); n != len(src.Segments) {
		t.Errorf("usage log names stage-3.md %d times, want once per segment:\n%s", n, files[run.UsageFile])
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

// tinyStripped is segment 0's stripped notes; segment 1 has none.
var tinyStripped = map[string]string{
	"0 Tiny/Alpha Is First.md": "# Alpha Is First[^1]\n\nAlpha is first.\n\n[^1]: [[@Tiny - A. Author]]\n",
	"0 Tiny/Tiny.md":           "# Tiny\n\n1. [[Alpha Is First]]\n",
}

// "No change" is a correct result, and a name the reply gives without .md
// gets it. A segment with no folder of
// stripped notes gets no call.
func TestRun_NoChange(t *testing.T) {
	src, err := source.Parse("Tiny", tinySource)
	if err != nil {
		t.Fatal(err)
	}
	folder := atStripped(t, src, tinyStripped)
	reply := "=== FILE: Alpha Is First ===\n" + tinyStripped["0 Tiny/Alpha Is First.md"] +
		"=== FILE: Tiny.md ===\n" + tinyStripped["0 Tiny/Tiny.md"] +
		"=== MANIFEST ===\n{\"changes\": []}"
	p := testfixture.Serve(t, testfixture.Case{Contains: segment(0), Reply: reply})
	if err := format.Run(context.Background(), p, src, "sonnet", folder); err != nil {
		t.Fatal(err)
	}
	if c := p.Calls(); len(c) != 1 || c[0].Model != "sonnet" {
		t.Fatalf("calls %+v, want one, to the model asked for", c)
	}
	got := testfixture.Files(t, filepath.Join(folder.Dir, run.FormatDir))
	want := []string{"fallbacks.json", "manifests/0.json", "raw/0.txt", "requests/0.sha256", "stage-3/0 Tiny/Alpha Is First.md", "stage-3/0 Tiny/Tiny.md", "usage.jsonl"}
	if keys := slices.Sorted(maps.Keys(got)); !reflect.DeepEqual(keys, want) {
		t.Fatalf("step wrote %q, want %q", keys, want)
	}
	// The reply's manifest, which ends with no newline, is kept as the runner
	// parses its blocks: trimmed, with one trailing newline.
	if m := got["manifests/0.json"]; m != "{\"changes\": []}\n" {
		t.Errorf("manifest %q, want the reply's trimmed, with one trailing newline", m)
	}
	for rel, body := range tinyStripped {
		if got["stage-3/"+rel] != body {
			t.Errorf("%s changed: %q", rel, got["stage-3/"+rel])
		}
	}
}

// A reply that isn't whole notes with plain names, or whose notes aren't
// the segment's, each once, falls back: the step completes with no folder
// or manifest for its segment, and lists the segment and the reply's fault.
func TestRun_ABadReplyFallsBack(t *testing.T) {
	src, err := source.Parse("Tiny", tinySource)
	if err != nil {
		t.Fatal(err)
	}
	const tail = "=== MANIFEST ===\n{}\n"
	for name, c := range map[string]struct{ reply, why string }{
		"escape":    {"=== FILE: ../Alpha Is First.md ===\nx\n=== FILE: Tiny.md ===\nhub\n" + tail, `named a note "../Alpha Is First.md", not a plain file name`},
		"delete":    {"=== DELETE: Alpha Is First.md ===\n=== FILE: Tiny.md ===\nhub\n" + tail, `gave "Alpha Is First.md" as a delete or edits, not a whole note`},
		"edit":      {"=== EDIT: Alpha Is First.md ===\nx\n=== FILE: Tiny.md ===\nhub\n" + tail, `gave "Alpha Is First.md" as a delete or edits, not a whole note`},
		"twice":     {"=== FILE: Alpha Is First ===\nx\n=== FILE: Alpha Is First.md ===\ny\n=== FILE: Tiny.md ===\nhub\n" + tail, `wrote "Alpha Is First.md" twice`},
		"missing":   {"=== FILE: Alpha Is First.md ===\nalpha\n" + tail, `left out ["Tiny.md"]` + mustGiveBack},
		"extra":     {"=== FILE: Alpha Is First.md ===\nalpha\n=== FILE: Tiny.md ===\nhub\n=== FILE: Beta.md ===\nbeta\n" + tail, `wrote ["Beta.md"], not notes of the segment` + mustGiveBack},
		"renamed":   {"=== FILE: Alpha Comes First.md ===\nalpha\n=== FILE: Tiny.md ===\nhub\n" + tail, `left out ["Alpha Is First.md"] and wrote ["Alpha Comes First.md"], not notes of the segment` + mustGiveBack},
		"no notes":  {tail, `left out ["Alpha Is First.md" "Tiny.md"]` + mustGiveBack},
		"no blocks": {"No change.\n", `left out ["Alpha Is First.md" "Tiny.md"]` + mustGiveBack},
	} {
		t.Run(name, func(t *testing.T) {
			folder := atStripped(t, src, tinyStripped)
			p := testfixture.Serve(t, testfixture.Case{Contains: segment(0), Reply: c.reply})
			if err := format.Run(context.Background(), p, src, format.DefaultModel, folder); err != nil {
				t.Fatal(err)
			}
			got := testfixture.Files(t, filepath.Join(folder.Dir, run.FormatDir))
			if keys, want := slices.Sorted(maps.Keys(got)), []string{"fallbacks.json", "raw/0.txt", "requests/0.sha256", "usage.jsonl"}; !reflect.DeepEqual(keys, want) {
				t.Errorf("step wrote %q, want %q", keys, want)
			}
			items, err := fallback.Read(folder, run.FormatDir)
			if err != nil {
				t.Fatal(err)
			}
			if want := []fallback.Item{{Segment: 0, Why: "step 3.1's reply " + c.why}}; !reflect.DeepEqual(items, want) {
				t.Errorf("fallbacks %+v, want %+v", items, want)
			}
		})
	}
}

// The step doesn't run until step 2.2 is complete: it makes no call and
// writes no directory.
func TestRun_NotStripped(t *testing.T) {
	src, err := source.Parse("Tiny", tinySource)
	if err != nil {
		t.Fatal(err)
	}
	folder := atStripped(t, src, tinyStripped)
	if err := os.Rename(filepath.Join(folder.Dir, run.StripDir), filepath.Join(folder.Dir, run.StripDir+run.PartialSuffix)); err != nil {
		t.Fatal(err)
	}
	p := testfixture.Serve(t)
	if err := format.Run(context.Background(), p, src, format.DefaultModel, folder); err == nil {
		t.Errorf("no error")
	}
	if c := p.Calls(); len(c) != 0 {
		t.Errorf("%d calls, want none", len(c))
	}
	for _, name := range []string{run.FormatDir, run.FormatDir + run.PartialSuffix} {
		if _, err := os.Stat(filepath.Join(folder.Dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s exists (stat: %v)", name, err)
		}
	}
}

// When every segment's reply falls back, step 3.1 still writes its
// `stage-3/`, empty, so its gate keeps every segment's stripped notes rather
// than failing to read it.
func TestRun_EveryReplyFallsBackThenGate(t *testing.T) {
	src, err := source.Parse("Tiny", tinySource)
	if err != nil {
		t.Fatal(err)
	}
	reply := "=== FILE: Alpha Is First ===\nx\n=== FILE: Alpha Is First.md ===\ny\n=== FILE: Tiny.md ===\nhub\n=== MANIFEST ===\n{}\n"
	folder := atStripped(t, src, tinyStripped)
	p := testfixture.Serve(t, testfixture.Case{Contains: segment(0), Reply: reply})
	if err := format.Run(context.Background(), p, src, format.DefaultModel, folder); err != nil {
		t.Fatal(err)
	}
	if err := formatcheck.Run(src, folder); err != nil {
		t.Fatalf("formatcheck.Run: %v", err)
	}
	got := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FormattedOrFallbackNotes)))
	want := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.StrippedNotes)))
	if !reflect.DeepEqual(got, want) {
		t.Errorf("notes that go on %q, want the stripped notes %q", got, want)
	}
}
