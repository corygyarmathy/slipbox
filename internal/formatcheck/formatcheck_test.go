package formatcheck_test

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/fallback"
	"github.com/corygyarmathy/slipbox/internal/formatcheck"
	"github.com/corygyarmathy/slipbox/internal/gate"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// readFolders reads the segment folders in dir.
func readFolders(t *testing.T, dir string) []split.SegmentFolder {
	t.Helper()
	folders, err := split.ReadFolders(dir)
	if err != nil {
		t.Fatal(err)
	}
	return folders
}

// folders writes files, by slash-separated path, to a directory and reads
// its segment folders, as Run reads them.
func folders(t *testing.T, files map[string]string) []split.SegmentFolder {
	t.Helper()
	dir := t.TempDir()
	testfixture.WriteFiles(t, dir, files)
	return readFolders(t, dir)
}

// edited is files with old, which must be in the file rel, replaced by new.
func edited(t *testing.T, files map[string]string, rel, old, new string) map[string]string {
	t.Helper()
	out := maps.Clone(files)
	if !strings.Contains(out[rel], old) {
		t.Fatalf("%s does not hold %q", rel, old)
	}
	out[rel] = strings.Replace(out[rel], old, new, 1)
	return out
}

// sourceFootnote is the source footnote of the invented chapter.
const sourceFootnote = "[^1]: " + testfixture.LampCitekeyLink + "\n"

// handWrittenStage2 are stage 2 notes and handWrittenStage3 their formatted
// copies, written to reach each problem the check finds. Each is in
// `3 Storms/` but the hub of `2 The Lens/`, unchanged:
//
//   - Forbidden Elements: each forbidden element once, the reserved
//     callouts in two cases, each of which the report gives as the text it
//     matched.
//   - Titles: added headings and callout titles, in the source and not, a
//     folded callout, a blockquote, a title with an apostrophe, which the
//     report quotes, a title in the source in another case, and a
//     seven-`#` line, which is no heading.
//   - Fixed Parts: the frontmatter, the H1 and the source footnote changed.
//   - Emphasis: underscores and stars that do and don't count as emphasis,
//     such as `_lumen_` beside a snake_case word, and each kind of list
//     marker.
//   - Rewritten: more than twenty changed words, of which the report gives
//     the first twenty.
//   - Line Endings: the formatted copy in CRLF, which the check reads as no
//     change, since it reads every line ending as `\n`.
//   - Line Separators: a heading and words run together with U+2028, `\v`,
//     `\f`, U+0085 and `\x1c` between them, none of which ends a line in the
//     note model. The heading is one line, so its title takes the words
//     after it, isn't in the source, and the words are changed.
//   - No H1: no H1, and a callout first, so the callout's counts come before
//     the emphasis's.
//   - Storms, the hub, unchanged; Dropped, in stage 2 alone, which Check
//     doesn't report, since only Run reports a missing note; New Note, in
//     stage 3 alone, which it does; and in stage 3, `.Hidden.md`, which is
//     hidden, and `Loose.md`, which is in no segment folder, so neither is
//     read.
var handWrittenStage2 = map[string]string{
	"2 The Lens/The Lens.md":         "# The Lens\n",
	"3 Storms/Storms.md":             "# Storms\n\n1. [[Titles]]\n",
	"3 Storms/Dropped.md":            "# Dropped[^1]\n\nDropped text.\n\n" + sourceFootnote,
	"3 Storms/Forbidden Elements.md": "---\ntags: [storms]\n---\n# Forbidden Elements[^1]\n\nThe light is steady and bright.\n\nThe keeper aims at the best.\n\n" + sourceFootnote,
	"3 Storms/Titles.md":             "# Titles[^1]\n\nIn a storm the light matters most.\n\nThe keeper spells this out.\n\nA quoted line.\n\n" + sourceFootnote,
	"3 Storms/Fixed Parts.md":        "---\ntags: [storms]\n---\n# Fixed Parts[^1]\n\nEvery keeper tends the lamp.\n\n" + sourceFootnote,
	"3 Storms/Emphasis.md":           "# Emphasis[^1]\n\nThe word lumen and the snake_case_word and __init__ stay.\n\nA b c and d e.\n\nOne item.\nTwo item.\nThree item.\n\n" + sourceFootnote,
	"3 Storms/Rewritten.md":          "# Rewritten[^1]\n\nthe keeper always trims the wick when the flame smokes and logs the weather before the light is lit at dusk each day.\n\n" + sourceFootnote,
	"3 Storms/Line Endings.md":       "# Line Endings[^1]\n\nThe lamp is lit at dusk.\n\n" + sourceFootnote,
	"3 Storms/Line Separators.md":    "# Line Separators\nalpha beta gamma delta\nepsilon\n",
	"3 Storms/No H1.md":              "A wreck is a failure.\n",
}

var handWrittenStage3 = map[string]string{
	"Loose.md":                       "# Loose\n\n## Not In The Source\n",
	"3 Storms/.Hidden.md":            "# Hidden\n\n## Not In The Source\n",
	"2 The Lens/The Lens.md":         "# The Lens\n",
	"3 Storms/Storms.md":             "# Storms\n\n1. [[Titles]]\n",
	"3 Storms/New Note.md":           "# New Note[^1]\n\nNew text.\n\n" + sourceFootnote,
	"3 Storms/Forbidden Elements.md": "---\ntags: [storms]\n---\n# Forbidden Elements[^1]\n\n> [!note]\n> The light is ==steady== and bright.\n\n---\n\n%%The%% keeper ![[aims]] at\n- [ ] the\n- [X] best.\n\n> [!Quote]\n\n" + sourceFootnote,
	"3 Storms/Titles.md":             "# Titles[^1]\n\n## Storms\n\n### A Heading Not In The Source\n\n#### a BEAM carries farther\n\n> [!Example] *The Fresnel* _lens_\n> In a storm the light matters most.\n\n> [!Info]- Keeper's Own Title\n> The keeper spells this out.\n\n>> A quoted line.\n\n####### Seven\n\n" + sourceFootnote,
	"3 Storms/Fixed Parts.md":        "---\ntags: [storms, lamps]\n---\n# Fixed Parts, Renamed[^1]\n\nEvery keeper tends the lamp.\n\n[^1]: [[@Lighthouses]]\n",
	"3 Storms/Emphasis.md":           "# Emphasis[^1]\n\nThe word _lumen_ and the snake_case_word and __init__ stay.\n\n*_A_* _b_c_ **c** and _d e_.\n\n1. One item.\n* Two item.\n+ Three item.\n\n" + sourceFootnote,
	"3 Storms/Rewritten.md":          "# Rewritten[^1]\n\nTHE KEEPER ALWAYS TRIMS THE WICK WHEN THE FLAME SMOKES AND LOGS THE WEATHER BEFORE THE LIGHT IS LIT AT DUSK EACH DAY.\n\n" + sourceFootnote,
	"3 Storms/Line Endings.md":       "# Line Endings[^1]\r\n\r\nThe lamp is lit at dusk.\r\n\r\n[^1]: " + testfixture.LampCitekeyLink + "\r\n",
	"3 Storms/Line Separators.md":    "# Line Separators\n## Keeper\u2028alpha\vbeta\fgamma\u0085delta\x1c\nepsilon\n",
	"3 Storms/No H1.md":              "> [!Info] Storms\n> _A wreck_ is a failure.\n",
}

// handWrittenReport is the report on the hand-written notes. A problem
// quotes the text it names, as Go quotes a string.
const handWrittenReport = `
3 Storms/Emphasis.md
  gained: list item 3, **x** 1, _x_ 2
  PROBLEM: TEXT CHANGED: - b + b_c

3 Storms/Fixed Parts.md
  PROBLEM: frontmatter changed
  PROBLEM: H1 changed: "# Fixed Parts[^1]" -> "# Fixed Parts, Renamed[^1]"
  PROBLEM: footnote definitions changed

3 Storms/Forbidden Elements.md
  gained: callout:Quote 1, callout:note 1, callout/quote line 1, list item 2
  PROBLEM: TEXT CHANGED: - steady + ==steady== - The + --- + %%The%% - aims + ![[aims]] + [ + ] + [X]
  PROBLEM: forbidden element: "[!note]"
  PROBLEM: forbidden element: "==steady=="
  PROBLEM: forbidden element: "---"
  PROBLEM: forbidden element: "%%"
  PROBLEM: forbidden element: "%%"
  PROBLEM: forbidden element: "![["
  PROBLEM: forbidden element: "- [ ]"
  PROBLEM: forbidden element: "- [X]"
  PROBLEM: forbidden element: "[!Quote]"

3 Storms/Line Separators.md
  gained: h2 1
  PROBLEM: TEXT CHANGED: - alpha - beta - gamma - delta
  PROBLEM: title not in source: "Keeper\u2028alpha\vbeta\fgamma\u0085delta\x1c"
NEW NOTE (not in stage 2): 3 Storms/New Note.md

3 Storms/No H1.md
  gained: callout:Info 1, callout/quote line 1, _x_ 1

3 Storms/Rewritten.md
  PROBLEM: TEXT CHANGED: - the - keeper - always - trims - the - wick - when - the - flame - smokes - and - logs - the - weather - before - the - light - is - lit - at

3 Storms/Titles.md
  gained: callout:Example 1, callout:Info 1, callout/quote line 3, h2 1, h3 1, h4 1
  PROBLEM: TEXT CHANGED: + ####### + Seven
  PROBLEM: title not in source: "A Heading Not In The Source"
  PROBLEM: title not in source: "Keeper's Own Title"

== 7 notes changed; 7 with problems; markup gained: callout:Example 1, callout:Info 2, callout:Quote 1, callout:note 1, callout/quote line 5, h2 2, h3 1, h4 1, list item 5, **x** 1, _x_ 3
`

// On formatted copies of the invented split, the hand-written notes and a
// note whose full stop moved past a footnote reference, the check gives
// each note's markup and problems.
func TestCheck(t *testing.T) {
	split := testfixture.LampStripped(t)
	markup := edited(t, split, "2 The Lens/Rings of Prisms.md", "uses rings of prisms", "uses _rings_ of **prisms**")
	markup = edited(t, markup, "2 The Lens/A Keeper Need Not Watch the Lens.md", "Obviously,", "- Obviously,")
	markup = edited(t, markup, "0 Keepers of the Lamp/A Lighthouse Is a Promise.md", "A lighthouse is", "> [!Abstract] Keepers of the Lamp\n> A lighthouse is")
	traced := map[string]string{"3 Storms/In a Storm the Light Matters Most.md": testfixture.LampSplit()["3 Storms/In a Storm the Light Matters Most.md"]}
	for _, tt := range []struct {
		name           string
		stage2, stage3 map[string]string
		want           string
		failing        []string
	}{
		{name: "markup only", stage2: split, stage3: markup, want: `
0 Keepers of the Lamp/A Lighthouse Is a Promise.md
  gained: callout:Abstract 1, callout/quote line 1

2 The Lens/A Keeper Need Not Watch the Lens.md
  gained: list item 1

2 The Lens/Rings of Prisms.md
  gained: **x** 1, _x_ 1

== 3 notes changed; 0 with problems; markup gained: callout:Abstract 1, callout/quote line 1, list item 1, **x** 1, _x_ 1
`},
		{name: "hand-written", stage2: handWrittenStage2, stage3: handWrittenStage3, want: handWrittenReport, failing: []string{"3 Storms"}},
		{name: "full stop moved past a reference", stage2: traced,
			stage3: edited(t, traced, "3 Storms/In a Storm the Light Matters Most.md", "the weather.[^3]", "the weather[^3]."), want: `
3 Storms/In a Storm the Light Matters Most.md
  PROBLEM: TEXT CHANGED: - weather.[^3] + weather[^3].

== 1 notes changed; 1 with problems; markup gained: none
`, failing: []string{"3 Storms"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := formatcheck.Check(folders(t, tt.stage2), folders(t, tt.stage3), testfixture.LampSource(t))
			if got := r.String(); got != tt.want {
				t.Errorf("report:\n%s\nwant:\n%s", got, tt.want)
			}
			if got := r.FailingFolders(); !reflect.DeepEqual(got, tt.failing) || r.Failed() != (len(tt.failing) > 0) {
				t.Errorf("Failed() = %v, FailingFolders() = %q, want %q", r.Failed(), got, tt.failing)
			}
		})
	}
}

// check runs the check on one note, its stage 2 text plain and its stage 3
// text formatted, in segment folder `1 A` of a one-segment source.
func check(t *testing.T, plain, formatted string) *formatcheck.Report {
	t.Helper()
	src, err := source.Parse("S", "---\nsource: \"[[@S]]\"\n---\n## A\n\nAlpha beta gamma delta.\n")
	if err != nil {
		t.Fatal(err)
	}
	folder := func(text string) []split.SegmentFolder {
		return []split.SegmentFolder{{Name: "1 A", Notes: []*note.Note{note.Parse("N.md", text)}}}
	}
	return formatcheck.Check(folder(plain), folder(formatted), src)
}

// Markup is listed in a fixed order of elements, not the order the note
// first used them: callouts by type, callout and quote lines, headings by
// level, list items, then bold and underscored text. The note's lost markup
// and the report's totals keep the same order.
func TestCheck_MarkupInElementOrder(t *testing.T) {
	const plain = "# N\n\nAlpha beta gamma delta.\n"
	const formatted = "# N\n\n_Alpha_ **beta**\n- gamma\n### Sub\n## Head\n> [!Example] One\n> [!Abstract] Two\n> delta.\n"
	want := []formatcheck.Markup{
		{Element: "callout:Abstract", Count: 1}, {Element: "callout:Example", Count: 1}, {Element: "callout/quote line", Count: 1},
		{Element: "h2", Count: 1}, {Element: "h3", Count: 1}, {Element: "list item", Count: 1},
		{Element: "**x**", Count: 1}, {Element: "_x_", Count: 1},
	}
	r := check(t, plain, formatted)
	if got := r.Notes[0].Gained; !reflect.DeepEqual(got, want) {
		t.Errorf("note's Gained = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(r.Gained, want) {
		t.Errorf("report's Gained = %v, want %v", r.Gained, want)
	}
	if got := check(t, formatted, plain).Notes[0].Lost; !reflect.DeepEqual(got, want) {
		t.Errorf("Lost = %v, want %v", got, want)
	}
	if line := "markup gained: callout:Abstract 1, callout:Example 1, callout/quote line 1, h2 1, h3 1, list item 1, **x** 1, _x_ 1\n"; !strings.HasSuffix(r.String(), line) {
		t.Errorf("report doesn't end %q:\n%s", line, r)
	}
}

// A note whose line endings alone changed, `\n` to `\r\n` or a lone `\r`,
// hasn't changed, and has no problems.
func TestCheck_LineEndingsAloneUnchanged(t *testing.T) {
	const plain = "---\nx: y\n---\n# N\n\nAlpha beta.[^1]\n\n[^1]: [[@S]]\n"
	for _, eol := range []string{"\r\n", "\r"} {
		r := check(t, plain, strings.ReplaceAll(plain, "\n", eol))
		if n := r.Notes[0]; n.Changed || len(n.Problems) > 0 || r.Changed != 0 {
			t.Errorf("with %q line endings: Changed = %v, Problems = %q", eol, n.Changed, n.Problems)
		}
	}
}

// The body leaves out the H1's line, not the first place the H1's text
// occurs, so a `## <title>` heading over a `# <title>` H1 is markup the note
// gained, and its words are unchanged.
func TestCheck_H1LineLeftOutOfTheBody(t *testing.T) {
	r := check(t, "# Alpha\n\nBeta gamma.\n", "## Alpha\n# Alpha\n\nBeta gamma.\n")
	n := r.Notes[0]
	if len(n.Problems) > 0 {
		t.Errorf("Problems = %q, want none", n.Problems)
	}
	if want := []formatcheck.Markup{{Element: "h2", Count: 1}}; !reflect.DeepEqual(n.Gained, want) {
		t.Errorf("Gained = %v, want %v", n.Gained, want)
	}
}

// lamp makes a run folder for the invented chapter whose step 2.2 is
// complete, with the invented split stripped as its stripped notes, and whose
// step 3.1 is complete, with a copy of them as its formatted notes: "no
// change".
func lamp(t *testing.T) (*source.Source, *run.Folder) {
	t.Helper()
	src := testfixture.LampSource(t)
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	if err := folder.SaveSource(src.Name, []byte(src.Frame)); err != nil {
		t.Fatal(err)
	}
	notes := testfixture.LampStripped(t)
	for _, dir := range []string{run.StrippedNotes, run.FormattedNotes} {
		testfixture.WriteFiles(t, filepath.Join(folder.Dir, filepath.FromSlash(dir)), notes)
	}
	return src, folder
}

// edit replaces old, which must be there, with new in the formatted note
// rel, `<n> <title>/<note>`.
func edit(t *testing.T, folder *run.Folder, rel, old, new string) {
	t.Helper()
	p := filepath.Join(folder.Dir, filepath.FromSlash(run.FormattedNotes), filepath.FromSlash(rel))
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), old) {
		t.Fatalf("%s does not hold %q", rel, old)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

const lampNote = "2 The Lens/A Keeper Need Not Watch the Lens.md"

// Formatting that adds markup only passes: the step's directory is
// complete, with a report giving the markup gained.
func TestRun_MarkupOnlyPasses(t *testing.T) {
	src, folder := lamp(t)
	edit(t, folder, lampNote, "no help at all.", "no **help** at all.")
	if err := formatcheck.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	if !folder.Complete(run.FormatGateDir) {
		t.Fatal("the step is not complete")
	}
	b, err := os.ReadFile(filepath.Join(folder.Dir, run.FormatGateDir, run.FormatCheckFile))
	if err != nil {
		t.Fatal(err)
	}
	want := "\n" + lampNote + "\n  gained: **x** 1\n\n== 1 notes changed; 0 with problems; markup gained: **x** 1\n"
	if string(b) != want {
		t.Errorf("report kept:\n%s\nwant:\n%s", b, want)
	}
	formatted := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FormattedNotes)))
	if got := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FormattedOrFallbackNotes))); !reflect.DeepEqual(got, formatted) {
		t.Errorf("the notes that go on are not the formatted ones, byte for byte")
	}
	if items, err := fallback.Read(folder, run.FormatGateDir); err != nil || items == nil || len(items) != 0 {
		t.Errorf("fallbacks %+v (err %v), want an empty list", items, err)
	}
}

// wantFallback checks that the gate completed with the segment folder name
// fallen back alone, as want: its notes that go on are its stripped notes,
// and every other folder's the formatted ones, byte for byte.
func wantFallback(t *testing.T, folder *run.Folder, name string, want fallback.Item) {
	t.Helper()
	if !folder.Complete(run.FormatGateDir) {
		t.Fatal("the step is not complete")
	}
	items, err := fallback.Read(folder, run.FormatGateDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(items, []fallback.Item{want}) {
		t.Errorf("fallbacks %+v, want %+v", items, []fallback.Item{want})
	}
	notes := map[string]string{}
	for rel, text := range testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FormattedNotes))) {
		if !strings.HasPrefix(rel, name+"/") {
			notes[rel] = text
		}
	}
	for rel, text := range testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.StrippedNotes))) {
		if strings.HasPrefix(rel, name+"/") {
			notes[rel] = text
		}
	}
	if got := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FormattedOrFallbackNotes))); !reflect.DeepEqual(got, notes) {
		t.Errorf("the notes that go on are not %s's stripped notes and the rest formatted", name)
	}
}

// A formatted note with one word changed fails the gate: its segment falls
// back to its stripped notes, unformatted, and the report gives the change.
// The other segments go on formatted.
func TestRun_ChangedWordFallsBack(t *testing.T) {
	src, folder := lamp(t)
	edit(t, folder, lampNote, "no help at all.", "no use at all.")
	if err := formatcheck.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	wantFallback(t, folder, "2 The Lens", fallback.Item{Segment: 2, Why: formatcheck.GateName, Report: run.FormatCheckFile})
	b, err := os.ReadFile(filepath.Join(folder.Dir, run.FormatGateDir, run.FormatCheckFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\n"+lampNote+"\n  PROBLEM: TEXT CHANGED: - help + use\n") || !strings.HasSuffix(string(b), "; 1 with problems; markup gained: none\n") {
		t.Errorf("report kept does not give the change:\n%s", b)
	}
}

// TEXT CHANGED gives the words removed and added, each changed run in the
// note's order, and a replaced run's removed words before its added ones.
func TestRun_TextChangedLayout(t *testing.T) {
	src, folder := lamp(t)
	edit(t, folder, lampNote, "can take no notice of", "may ignore")
	edit(t, folder, lampNote, "On a foggy night", "On a misty night")
	if err := formatcheck.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(folder.Dir, run.FormatGateDir, run.FormatCheckFile))
	if err != nil {
		t.Fatal(err)
	}
	want := "\n  PROBLEM: TEXT CHANGED: - can - take - no - notice - of + may + ignore - foggy + misty\n"
	if !strings.Contains(string(b), want) {
		t.Errorf("report does not hold %q:\n%s", want, b)
	}
}

// A stripped note with no formatted copy, its note or its whole segment
// folder deleted, fails the gate: the report names the note, and its segment
// falls back.
func TestRun_MissingNoteFallsBack(t *testing.T) {
	folder2 := "2 The Lens"
	for name, remove := range map[string]string{
		"note":   lampNote,
		"folder": folder2,
	} {
		t.Run(name, func(t *testing.T) {
			src, folder := lamp(t)
			if err := os.RemoveAll(filepath.Join(folder.Dir, filepath.FromSlash(run.FormattedNotes), filepath.FromSlash(remove))); err != nil {
				t.Fatal(err)
			}
			if err := formatcheck.Run(src, folder); err != nil {
				t.Fatal(err)
			}
			wantFallback(t, folder, folder2, fallback.Item{Segment: 2, Why: formatcheck.GateName, Report: run.FormatCheckFile})
			b, err := os.ReadFile(filepath.Join(folder.Dir, run.FormatGateDir, run.FormatCheckFile))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(b), "MISSING NOTE (not in stage 3): "+lampNote+"\n") || strings.HasSuffix(string(b), "; 0 with problems; markup gained: none\n") {
				t.Errorf("report kept does not give the missing note:\n%s", b)
			}
		})
	}
}

// A segment step 3.1 listed as fallen back, for a reply it couldn't write,
// has no formatted folder. The gate doesn't check it, and it falls back with
// step 3.1's reason.
func TestRun_StepThreeOneFallbackFallsBack(t *testing.T) {
	src, folder := lamp(t)
	folder2 := "2 The Lens"
	if err := os.RemoveAll(filepath.Join(folder.Dir, filepath.FromSlash(run.FormattedNotes), folder2)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder.Dir, run.FormatDir, run.FallbacksFile), []byte(`[{"segment": 2, "why": "step 3.1's reply wrote \"X.md\" twice"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := formatcheck.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	wantFallback(t, folder, folder2, fallback.Item{Segment: 2, Why: `step 3.1's reply wrote "X.md" twice`})
	if b, err := os.ReadFile(filepath.Join(folder.Dir, run.FormatGateDir, run.FormatCheckFile)); err != nil || strings.Contains(string(b), folder2) {
		t.Errorf("the report checks the segment step 3.1 fell back (err %v):\n%s", err, b)
	}
}

// The gate checks step 3.1's output, so it doesn't run before step 3.1 is
// complete.
func TestRun_NeedsTheFormattedNotes(t *testing.T) {
	src, folder := lamp(t)
	if err := os.Rename(filepath.Join(folder.Dir, run.FormatDir), filepath.Join(folder.Dir, run.FormatDir+run.PartialSuffix)); err != nil {
		t.Fatal(err)
	}
	err := formatcheck.Run(src, folder)
	var stopped *gate.Stopped
	if err == nil || errors.As(err, &stopped) || !strings.Contains(err.Error(), "step 3.1") {
		t.Fatalf("Run = %v, want an error saying step 3.1 is not done", err)
	}
	if _, err := os.Stat(filepath.Join(folder.Dir, run.FormatGateDir+run.PartialSuffix)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the step began (stat: %v)", err)
	}
}

// A formatted folder no segment claims, which only a hand edit makes, has no
// stripped notes to fall back to, so it stops the run before the gate, with
// no report, naming the folder and saying what to do.
func TestRun_UnclaimedFolderStops(t *testing.T) {
	src, folder := lamp(t)
	formatted := filepath.Join(folder.Dir, filepath.FromSlash(run.FormattedNotes))
	if err := os.Mkdir(filepath.Join(formatted, "99 Extra"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(formatted, "99 Extra", "Extra.md"), []byte("# Extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := formatcheck.Run(src, folder)
	var stopped *gate.Stopped
	if !errors.As(err, &stopped) {
		t.Fatalf("Run = %v, want a *gate.Stopped", err)
	}
	if stopped.Gate != formatcheck.FolderGateName || !reflect.DeepEqual(stopped.Segments, []string{`folder "99 Extra"`}) || stopped.Report != "" {
		t.Errorf("stopped = %+v", stopped)
	}
	if want := "No segment of the source has the folder name of " + filepath.Join(formatted, "99 Extra") + "."; !strings.HasPrefix(stopped.Fix, want) {
		t.Errorf("Fix = %q, want it to start %q", stopped.Fix, want)
	}
	if folder.Complete(run.FormatGateDir) {
		t.Error("the step is complete after an unclaimed folder")
	}
}
