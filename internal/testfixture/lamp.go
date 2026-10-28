package testfixture

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
)

// Lamp is the invented chapter's name, the source file name without .md.
// The chapter, its stage 1 notes and their split are invented, so the tests
// that read them, of the step 1.3 checks, step 2.1's gates, strip traces and
// the format check among them, read no trial text.
const Lamp = "Lighthouses - Ch 2 - Keepers of the Lamp"

// LampChapter is the invented chapter: four segments, the first the text
// before its first `##` heading, one heading with a curly apostrophe, an
// emphasized phrase, and a sentence in the author's first person, for a
// statement to set the author's name into (ADR 0006). LampStage1 traces
// into it and LampSplit splits that.
const LampChapter = `---
source: "[[@A History of Lighthouses - Rhea Vale]]"
---

# 2. Keepers of the Lamp

A lighthouse is a promise made to ships. It burns whether or not a ship is near.

The keeper kept that promise for the harbour.

## The Keeper’s Watch

The watch began at dusk. The keeper trimmed the wick every four hours, and logged the weather in a book.

The log was read by nobody until a ship was lost. Then the inspectors read every page.

## The Lens

The lens bends light into a beam. A beam carries farther than a glow.

Obviously, a keeper can take no notice of the lens on a clear night. On a foggy night it is no help at all.

*The Fresnel lens* uses rings of prisms instead of one thick glass.

## Storms

In a storm the light matters most. A wreck on the rocks was the keeper’s failure, whatever the weather.

The keeper stayed at the lamp (in 1839, for nine days) through every gale.

I have stood in that lamp room in a gale, and the glass shook.
`

// LampCitekeyLink is LampChapter's `source:` value without its YAML quotes,
// the source footnote strip traces gives each atomic note.
const LampCitekeyLink = "[[@A History of Lighthouses - Rhea Vale]]"

// LampTrace is a trace of quote into LampChapter's section, a footnote
// definition's value.
func LampTrace(section, quote string) string {
	return "[[" + Lamp + "#" + section + "]] \"" + quote + "\""
}

// The sections of LampChapter, as a trace names them.
const (
	lampIntro = "2. Keepers of the Lamp"
	lampWatch = "The Keeper’s Watch"
	lampLens  = "The Lens"
	lampStorm = "Storms"
)

// lampDefs are footnote definition lines, `[^1]` onward, each value a trace
// into section of the quote given.
func lampDefs(section string, quotes ...string) string {
	out := ""
	for i, q := range quotes {
		out += "[^" + strconv.Itoa(i+1) + "]: " + LampTrace(section, q) + "\n"
	}
	return out
}

// LampStage1 gives the final stage 1 notes of LampChapter, by file name:
// one note per segment, and an aside in segment 1. Segment 1's and 3's
// statements say "he" and "they" where the source names who, for the split
// to repair with name spans.
func LampStage1() map[string]string {
	return map[string]string{
		"0 Keepers of the Lamp.md": "# 0 Keepers of the Lamp\n\n" +
			"A lighthouse is a promise made to ships. It burns whether or not a ship is near.[^1][^2]\n" +
			"The keeper kept that promise for the harbour.[^3]\n\n" +
			lampDefs(lampIntro, "A lighthouse is a promise made to ships.", "It burns whether or not a ship is near.", "The keeper kept that promise for the harbour."),
		"1 The Keeper's Watch.md": "# 1 The Keeper's Watch\n\n" +
			"The watch began at dusk.[^1]\n" +
			"He trimmed the wick every four hours.[^2]\n" +
			"He logged the weather in a book.[^3]\n\n" +
			lampDefs(lampWatch, "The watch began at dusk.", "trimmed the wick every four hours", "logged the weather in a book"),
		"1 The Keeper's Watch - Aside - The log.md": "# 1 The Keeper's Watch - Aside - The log\n\n" +
			"The log was read by nobody until a ship was lost.[^1]\n" +
			"Then they read every page.[^2]\n\n" +
			lampDefs(lampWatch, "The log was read by nobody until a ship was lost.", "read every page"),
		"2 The Lens.md": "# 2 The Lens\n\n" +
			"The lens bends light into a beam.[^1]\n" +
			"A beam carries farther than a glow.[^2]\n" +
			"Obviously, a keeper can take no notice of the lens on a clear night.[^3]\n" +
			"On a foggy night it is no help at all.[^4]\n" +
			"It uses rings of prisms instead of one thick glass.[^5]\n\n" +
			lampDefs(lampLens, "The lens bends light into a beam.", "A beam carries farther than a glow.",
				"Obviously, a keeper can take no notice of the lens on a clear night.", "On a foggy night it is no help at all.",
				"uses rings of prisms instead of one thick glass"),
		"3 Storms.md": "# 3 Storms\n\n" +
			"In a storm the light matters most.[^1]\n" +
			"A wreck was his failure, whatever the weather.[^2]\n" +
			"He stayed at the lamp (in 1839, for nine days) through every gale.[^3]\n\n" +
			lampDefs(lampStorm, "In a storm the light matters most.", "failure, whatever the weather",
				"stayed at the lamp (in 1839, for nine days) through every gale"),
	}
}

// LampSplit gives a split of LampStage1 that passes step 2.1's gates, by
// slash-separated path under `stage-2/`. It reaches what the gates allow:
//
//   - 0: a stage 1 line of two statements split into two notes, so the
//     text of each is edited.
//   - 1: the aside's statements in a note of their own, and name spans
//     repairing "he" (one quote, twice) and "they".
//   - 2: two stage 1 lines merged into one, and a name span whose quote is
//     emphasized. One note has frontmatter.
//   - 3: name spans, one with a curly apostrophe.
//
// Each folder keeps a manifest.json beside its notes, which is no note, and
// Loose.md sits in no segment folder, so no gate reads it.
//
// The split check's and the trace diff's tests pin their whole report on
// this split, so a test that needs another shape edits its own copy rather
// than this one.
func LampSplit() map[string]string {
	return map[string]string{
		"Loose.md": "# Loose\n\nIn no segment folder.[^1]\n\n[^1]: not a trace\n",

		"0 Keepers of the Lamp/manifest.json":          "{}\n",
		"0 Keepers of the Lamp/Keepers of the Lamp.md": "# Keepers of the Lamp\n\n1. [[A Lighthouse Is a Promise]]\n2. [[The Keeper Kept the Promise]]\n",
		"0 Keepers of the Lamp/A Lighthouse Is a Promise.md": "# A Lighthouse Is a Promise\n\n" +
			"A lighthouse is a promise made to ships.[^1]\n" +
			"It burns whether or not a ship is near.[^2]\n\n" +
			lampDefs(lampIntro, "A lighthouse is a promise made to ships.", "It burns whether or not a ship is near."),
		"0 Keepers of the Lamp/The Keeper Kept the Promise.md": "# The Keeper Kept the Promise\n\n" +
			"The keeper kept that promise for the harbour.[^1]\n\n" +
			lampDefs(lampIntro, "The keeper kept that promise for the harbour."),

		"1 The Keeper's Watch/manifest.json":         "{}\n",
		"1 The Keeper's Watch/The Keeper's Watch.md": "# The Keeper's Watch\n\n1. [[The Watch Began at Dusk]]\n    - Aside: [[The Log Was Read Only After a Wreck]]\n",
		"1 The Keeper's Watch/The Watch Began at Dusk.md": "# The Watch Began at Dusk\n\n" +
			"The watch began at dusk.[^1]\n" +
			"The keeper[^2] trimmed the wick every four hours.[^3]\n" +
			"The keeper[^4] logged the weather in a book.[^5]\n\n" +
			lampDefs(lampWatch, "The watch began at dusk.", "The keeper", "trimmed the wick every four hours", "The keeper", "logged the weather in a book"),
		"1 The Keeper's Watch/The Log Was Read Only After a Wreck.md": "# The Log Was Read Only After a Wreck\n\n" +
			"The log was read by nobody until a ship was lost.[^1]\n" +
			"Then the inspectors[^2] read every page.[^3]\n\n" +
			lampDefs(lampWatch, "The log was read by nobody until a ship was lost.", "the inspectors", "read every page"),

		"2 The Lens/manifest.json": "{}\n",
		"2 The Lens/The Lens.md":   "# The Lens\n\n1. [[The Lens Makes a Beam]]\n2. [[A Keeper Need Not Watch the Lens]]\n3. [[Rings of Prisms]]\n",
		"2 The Lens/The Lens Makes a Beam.md": "# The Lens Makes a Beam\n\n" +
			"The lens bends light into a beam. A beam carries farther than a glow.[^1][^2]\n\n" +
			lampDefs(lampLens, "The lens bends light into a beam.", "A beam carries farther than a glow."),
		"2 The Lens/A Keeper Need Not Watch the Lens.md": "# A Keeper Need Not Watch the Lens\n\n" +
			"Obviously, a keeper can take no notice of the lens on a clear night.[^1]\n" +
			"On a foggy night it is no help at all.[^2]\n\n" +
			lampDefs(lampLens, "Obviously, a keeper can take no notice of the lens on a clear night.", "On a foggy night it is no help at all."),
		"2 The Lens/Rings of Prisms.md": "---\ntags: [lighthouses]\n---\n# Rings of Prisms\n\n" +
			"*The Fresnel lens*[^1] uses rings of prisms instead of one thick glass.[^2]\n\n" +
			lampDefs(lampLens, "*The Fresnel lens*", "uses rings of prisms instead of one thick glass"),

		"3 Storms/manifest.json": "{}\n",
		"3 Storms/Storms.md":     "# Storms\n\n1. [[In a Storm the Light Matters Most]]\n2. [[The Keeper Stayed Through Every Gale]]\n",
		"3 Storms/In a Storm the Light Matters Most.md": "# In a Storm the Light Matters Most\n\n" +
			"In a storm the light matters most.[^1]\n" +
			"A wreck was the keeper’s[^2] failure, whatever the weather.[^3]\n\n" +
			lampDefs(lampStorm, "In a storm the light matters most.", "the keeper’s", "failure, whatever the weather"),
		"3 Storms/The Keeper Stayed Through Every Gale.md": "# The Keeper Stayed Through Every Gale\n\n" +
			"The keeper[^1] stayed at the lamp (in 1839, for nine days) through every gale.[^2]\n\n" +
			lampDefs(lampStorm, "The keeper", "stayed at the lamp (in 1839, for nine days) through every gale"),
	}
}

// WriteFiles writes files, by slash-separated path, under dir, making the
// directories they need.
func WriteFiles(t testing.TB, dir string, files map[string]string) {
	t.Helper()
	for rel, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("testfixture: %v", err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatalf("testfixture: %v", err)
		}
	}
}

// LampSource parses LampChapter.
func LampSource(t testing.TB) *source.Source {
	t.Helper()
	src, err := source.Parse(Lamp, LampChapter)
	if err != nil {
		t.Fatalf("testfixture: %v", err)
	}
	return src
}

// LampStage1Notes reads LampStage1's notes, in byte order of their names, as
// note.ReadDir gives them.
func LampStage1Notes(t testing.TB) []*note.Note {
	t.Helper()
	dir := t.TempDir()
	WriteFiles(t, dir, LampStage1())
	notes, err := note.ReadDir(dir)
	if err != nil {
		t.Fatalf("testfixture: %v", err)
	}
	return notes
}

// LampFolders reads the segment folders of a split, files by slash-separated
// path under `stage-2/`, such as LampSplit or a test's copy of it.
func LampFolders(t testing.TB, files map[string]string) []split.SegmentFolder {
	t.Helper()
	dir := t.TempDir()
	WriteFiles(t, dir, files)
	folders, err := split.ReadFolders(dir)
	if err != nil {
		t.Fatalf("testfixture: %v", err)
	}
	return folders
}

// LampExtractRun makes a run folder for LampChapter whose step 1.1 wrote
// LampStage1 as its stage 1 notes, the folder the step 1.3 checks read.
func LampExtractRun(t testing.TB) (*source.Source, *run.Folder) {
	t.Helper()
	src, folder := lampFolder(t)
	WriteFiles(t, filepath.Join(folder.Dir, filepath.FromSlash(run.ExtractNotes)), LampStage1())
	return src, folder
}

// LampRun makes a run folder for LampChapter whose final stage 1 notes are
// LampStage1 and whose step 2.1 split is LampSplit.
func LampRun(t testing.TB) (*source.Source, *run.Folder) {
	t.Helper()
	src, folder := lampFolder(t)
	WriteFiles(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes)), LampStage1())
	WriteFiles(t, filepath.Join(folder.Dir, filepath.FromSlash(run.SplitNotes)), LampSplit())
	return src, folder
}

// lampFolder makes a new run folder holding LampChapter as its source.
func lampFolder(t testing.TB) (*source.Source, *run.Folder) {
	t.Helper()
	src := LampSource(t)
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatalf("testfixture: %v", err)
	}
	if err := folder.SaveSource(src.Name, []byte(src.Frame)); err != nil {
		t.Fatalf("testfixture: %v", err)
	}
	return src, folder
}
