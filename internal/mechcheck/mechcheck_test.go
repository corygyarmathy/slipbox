package mechcheck_test

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
)

// chapter is an invented source chapter of five segments, the first the
// text before its first `##` heading, and one heading with a curly
// apostrophe. Its last segment ends in an endnote definition, which the
// trace match doesn't search and coverage doesn't count.
const chapter = `---
source: "[[@A Short History of Bridges - J. Doe]]"
---

# 3. The Stone Arch

An arch stands by its shape. It needs no glue.

Every stone in it is pressed by its neighbours.

## The Builder’s Rule

Each stone leans on the next. The rule is old, and nobody wrote it down.

Masons taught it to apprentices by example. Abandoning the rule would bring the arch down.

## Arches

The search for a span, then, is the search for a shape. Later in the chapter the author returns to this. A shape is a kind of strength, and later in the chapter that claim is tested.

Similarly, a stone arch and a brick arch are not two sciences.

## Load

A load becomes thrust. Thrust runs down to the abutments.

## Keystone

The keystone goes in last. Then the centring comes out.

[^1]: The centring is the timber frame. It holds the stones until the keystone is in.
`

// trace is the start of a footnote definition tracing into chapter's
// section, or into its `source:` line if section is "".
func trace(label, section string) string {
	if section != "" {
		section = "#" + section
	}
	return "[^" + label + "]: [[Bridges - Ch 3 - The Stone Arch" + section + "]] "
}

func stoneArch(t *testing.T) *source.Source {
	t.Helper()
	src, err := source.Parse("Bridges - Ch 3 - The Stone Arch", chapter)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// cleanNotes are stage 1 notes whose only findings are the two that are no
// flag for the repair pass: a fuzzy match, and traces into other segments,
// one of them into the `source:` line. Their traces cover some sentences
// whole, some in part and some not at all.
var cleanNotes = map[string]string{
	"0 The Stone Arch.md": "# 0 The Stone Arch\n\n" +
		"An arch stands by its shape.[^1]\n" +
		"Its stones are pressed by their neighbours.[^2]\n" +
		"J. Doe[^3] says the keystone goes in last.[^4]\n\n" +
		trace("1", "3. The Stone Arch") + "\"An arch stands by its shape.\"\n" +
		trace("2", "3. The Stone Arch") + "\"pressed by its neighbours\"\n" +
		trace("3", "") + "\"J. Doe\"\n" +
		trace("4", "Keystone") + "\"The keystone goes in last.\"\n",
	"1 The Builder's Rule.md": "# 1 The Builder's Rule\n\n" +
		"Each stone leans on the next.[^1] The rule is old.[^2]\n\n" +
		trace("1", "The Builder’s Rule") + "\"Each stone leans on the next.\"\n" +
		trace("2", "The Builder's Rule") + "\"The rule is old\"\n",
	"2 Arches.md": "# 2 Arches\n\n" +
		"The search for a span is the search for a shape.[^1]\n\n" +
		trace("1", "Arches") + "\"The search for a span, than, is the search for a shape.\"\n",
}

// flaggedNotes are stage 1 notes written to reach every finding:
//
//   - 1 The Builder's Rule: exact traces, one naming its section with a
//     curly apostrophe and one with a straight one, a fuzzy trace, a broken
//     one, a section the source doesn't have (once with an apostrophe,
//     which the report quotes), a reference to an undefined footnote, a
//     statement whose every trace is broken, a statement with no footnote,
//     a trace into the `source:` line, a cross-segment trace, a label
//     defined twice (listed where it is first defined, taking its last
//     definition), trailing whitespace, and the three punctuation flags.
//   - 2 Arches: CRLF line endings, and frontmatter.
//   - 3 Load: a trace defined between two `---` lines at the top of the
//     note, which the note model reads as frontmatter and still reads as a
//     trace, so the check checks it.
//   - Loose note: a name that doesn't start with a segment number, so no
//     cross-segment listing, and a footnote that isn't a trace.
var flaggedNotes = map[string]string{
	"1 The Builder's Rule.md": "# 1 The Builder's Rule\n\n" +
		"Each stone leans on the next.[^1]\n" +
		"The rule is old, and nobody wrote it down.[^2]\n" +
		"This statement has no trace at all.\n" +
		"The rule is set down by *J. Doe*[^3] and is the search for a span[^4]   \n" +
		"The quote of this one was invented, so nothing in its section matches it.[^5]\n" +
		"This one cites sections the source doesn't have.[^6] [^7]\n" +
		"This one cites a footnote that was never defined.[^99]\n" +
		"The rule has parts\n" +
		"- abandoning the rule would bring the arch down.[^8]\n" +
		"- ‘Example’ is how masons taught it[^9]\n" +
		"1. The rule is old.[^10]\n" +
		"A line ending in a lead-in:\n" +
		"- *Masons* taught it.[^11]\n\n" +
		trace("1", "The Builder’s Rule") + "\"Each stone leans on the next.\"\n" +
		trace("2", "The Builder's Rule") + "\"The rule is old, and nobody wrote it down\"\n" +
		trace("3", "") + "\"J. Doe\"\n" +
		trace("4", "Load") + "\"the search for a span\"\n" +
		trace("5", "The Builder’s Rule") + "\"Masons wrote the rule down in a great book that every apprentice had to copy out\"\n" +
		trace("6", "Builder's Nowhere") + "\"anything\"\n" +
		trace("7", "Nowhere") + "\"anything\"\n" +
		trace("8", "The Builder’s Rule") + "\"abandoning the rule would bring the arch down\"\n" +
		trace("9", "The Builder’s Rule") + "\"Masons taught it to aprentices by example\"\n" +
		trace("10", "The Builder’s Rule") + "\"the rule is old\"\n" +
		trace("11", "The Builder’s Rule") + "\"*Masons* taught it\"\n" +
		trace("4", "Arches") + "\"the search for a span\"\n",
	"2 Arches.md": "---\r\ntags: [trial]\r\n---\r\n# 2 Arches\r\n\r\n" +
		"A stone arch and a brick arch are not two sciences.[^1]\r\n\r\n" +
		trace("1", "Arches") + "\"a stone arch and a brick arch are not two sciences\"\r\n",
	"3 Load.md": "---\n" +
		"A load becomes thrust.[^1]\n\n" +
		trace("1", "Load") + "\"A load becomes thrust.\"\n" +
		"---\n",
	"Loose note.md": "# Loose note\n\n" +
		"A statement whose footnote is not a trace.[^1]\n" +
		"A statement traced into another segment, which a note not named for a segment doesn't list.[^2]\n\n" +
		"[^1]: a note to self, not a trace\n" +
		trace("2", "Arches") + "\"Similarly, a stone arch and a brick arch are not two sciences\"\n",
}

// parseNotes parses notes, by name, in order of their names, as
// note.ReadDir reads them.
func parseNotes(notes map[string]string) []*note.Note {
	var out []*note.Note
	for _, name := range slices.Sorted(maps.Keys(notes)) {
		out = append(out, note.Parse(name, notes[name]))
	}
	return out
}

// writeNotes writes notes, by name, into dir.
func writeNotes(t *testing.T, dir string, notes map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range notes {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// On each set of stage 1 notes, the check grades the traces, counts the
// coverage and each kind of finding, says whether any is a flag for the
// repair pass, and renders the report and each segment's uncovered list.
func TestCheck(t *testing.T) {
	tests := []struct {
		name      string
		notes     map[string]string
		failed    bool
		stats     mechcheck.Stats
		covered   int
		findings  map[mechcheck.Kind]int
		report    string
		uncovered string
	}{
		{"clean", cleanNotes, false, mechcheck.Stats{Exact: 6, Fuzzy: 1, Statements: 5}, 4,
			map[mechcheck.Kind]int{mechcheck.CrossSegment: 2, mechcheck.Fuzzy: 1},
			"\n### 0 The Stone Arch.md\n" +
				"  CROSS-SEGMENT [^3] -> @record: J. Doe\n" +
				"  CROSS-SEGMENT [^4] -> Keystone: The keystone goes in last.\n" +
				"\n### 1 The Builder's Rule.md\n" +
				"\n### 2 Arches.md\n" +
				"  fuzzy  [^1] quote : The search for a span, than, is the search for a shape.\n" +
				"                source: The search for a span, then, is the search for a shape.\n",
			"0.txt # 3. The Stone Arch\n" +
				"  ¶1 UNCOVERED: It needs no glue.\n" +
				"  ¶2 PARTIAL (uncovered in CAPS): EVERY STONE IN IT IS pressed by its neighbours.\n" +
				"1.txt # The Builder's Rule\n" +
				"  ¶1 PARTIAL (uncovered in CAPS): The rule is old, AND NOBODY WROTE IT DOWN.\n" +
				"  ¶2 UNCOVERED: Masons taught it to apprentices by example.\n" +
				"  ¶2 UNCOVERED: Abandoning the rule would bring the arch down.\n" +
				"2.txt # Arches\n" +
				"  ¶1 UNCOVERED: Later in the chapter the author returns to this.\n" +
				"  ¶1 UNCOVERED: A shape is a kind of strength, and later in the chapter that claim is tested.\n" +
				"  ¶2 UNCOVERED: Similarly, a stone arch and a brick arch are not two sciences.\n" +
				"3.txt # Load\n" +
				"  ¶1 UNCOVERED: A load becomes thrust.\n" +
				"  ¶1 UNCOVERED: Thrust runs down to the abutments.\n" +
				"4.txt # Keystone\n" +
				"  ¶1 UNCOVERED: Then the centring comes out.\n"},
		// A lead-in with no footnote is an untraced statement. So is each
		// frontmatter line, which the check, skipping only headings and
		// footnote definitions, lints and counts as a statement.
		{"flagged", flaggedNotes, true, mechcheck.Stats{Exact: 10, Fuzzy: 1, Broken: 5, UntracedStatements: 12, Statements: 22}, 5,
			map[mechcheck.Kind]int{
				mechcheck.CrossSegment: 4, mechcheck.LeadIn: 1, mechcheck.NoTerminal: 7, mechcheck.LowercaseItem: 1,
				mechcheck.Untraced: 8, mechcheck.SectionNotFound: 4, mechcheck.Broken: 1, mechcheck.Fuzzy: 1,
			},
			"\n### 1 The Builder's Rule.md\n" +
				"  CROSS-SEGMENT [^3] -> @record: J. Doe\n" +
				"  CROSS-SEGMENT [^4] -> Arches: the search for a span\n" +
				"  CROSS-SEGMENT [^6] -> Builder's Nowhere: anything\n" +
				"  CROSS-SEGMENT [^7] -> Nowhere: anything\n" +
				"  PUNCT no terminal punctuation: The rule is set down by *J. Doe* and is the search for a span\n" +
				"  PUNCT lead-in without ':': The rule has parts\n" +
				"  PUNCT list item lowercase: - abandoning the rule would bring the arch down.\n" +
				"  PUNCT no terminal punctuation: - ‘Example’ is how masons taught it\n" +
				"  UNTRACED statement: This statement has no trace at all.\n" +
				"  BROKEN [^5] Masons wrote the rule down in a great book that every apprentice had to copy out\n" +
				"  BROKEN [^6] (section \"Builder's Nowhere\" not found)\n" +
				"  BROKEN [^7] (section \"Nowhere\" not found)\n" +
				"  BROKEN [^99] (not a trace)\n" +
				"  UNTRACED statement: The rule has parts\n" +
				"  fuzzy  [^9] quote : Masons taught it to aprentices by example\n" +
				"                source: Masons taught it to apprentices by exampl\n" +
				"  UNTRACED statement: A line ending in a lead-in:\n" +
				"\n### 2 Arches.md\n" +
				"  PUNCT no terminal punctuation: ---\n" +
				"  PUNCT no terminal punctuation: tags: [trial]\n" +
				"  PUNCT no terminal punctuation: ---\n" +
				"  UNTRACED statement: ---\n" +
				"  UNTRACED statement: tags: [trial]\n" +
				"  UNTRACED statement: ---\n" +
				"\n### 3 Load.md\n" +
				"  PUNCT no terminal punctuation: ---\n" +
				"  PUNCT no terminal punctuation: ---\n" +
				"  UNTRACED statement: ---\n" +
				"  UNTRACED statement: ---\n" +
				"\n### Loose note.md\n" +
				"  BROKEN [^1] (not a trace)\n",
			"0.txt # 3. The Stone Arch\n" +
				"  ¶1 UNCOVERED: An arch stands by its shape.\n" +
				"  ¶1 UNCOVERED: It needs no glue.\n" +
				"  ¶2 UNCOVERED: Every stone in it is pressed by its neighbours.\n" +
				"1.txt # The Builder's Rule\n" +
				"  ¶2 PARTIAL (uncovered in CAPS): Masons taught it to apprentices by examplE.\n" +
				"2.txt # Arches\n" +
				"  ¶1 PARTIAL (uncovered in CAPS): The search for a span, THEN, IS THE SEARCH FOR A SHAPE.\n" +
				"  ¶1 UNCOVERED: Later in the chapter the author returns to this.\n" +
				"  ¶1 UNCOVERED: A shape is a kind of strength, and later in the chapter that claim is tested.\n" +
				"3.txt # Load\n" +
				"  ¶1 UNCOVERED: Thrust runs down to the abutments.\n" +
				"4.txt # Keystone\n" +
				"  ¶1 UNCOVERED: The keystone goes in last.\n" +
				"  ¶1 UNCOVERED: Then the centring comes out.\n"},
	}
	src := stoneArch(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := mechcheck.Check(src, parseNotes(tt.notes))
			if r.Stats != tt.stats {
				t.Errorf("Stats = %+v, want %+v", r.Stats, tt.stats)
			}
			if r.Coverage.Covered != tt.covered || r.Coverage.Total != 15 {
				t.Errorf("covered %d/%d, want %d/15", r.Coverage.Covered, r.Coverage.Total, tt.covered)
			}
			findings := map[mechcheck.Kind]int{}
			for _, n := range r.Notes {
				for _, f := range n.Findings {
					findings[f.Kind]++
				}
			}
			if !reflect.DeepEqual(findings, tt.findings) {
				t.Errorf("findings by kind = %v, want %v", findings, tt.findings)
			}
			if r.Failed() != tt.failed {
				t.Errorf("Failed() = %v, want %v", r.Failed(), tt.failed)
			}
			if got := r.String(); got != tt.report {
				t.Errorf("report:\n%s\nwant:\n%s", got, tt.report)
			}
			var uncovered strings.Builder
			for _, s := range r.Coverage.Sections {
				uncovered.WriteString(s.File() + " " + s.List())
			}
			if got := uncovered.String(); got != tt.uncovered {
				t.Errorf("uncovered lists:\n%s\nwant:\n%s", got, tt.uncovered)
			}
		})
	}
}

func TestCheck_FindingsLocateTheirLines(t *testing.T) {
	src := stoneArch(t)
	text := "# 1 X\n\nUntraced line.\nTraced.[^1]\n\n[^1]: [[S#Arches]] \"Similarly, a stone arch and a brick arch are not two sciences\"\n"
	r := mechcheck.Check(src, []*note.Note{note.Parse("1 X.md", text)})
	got := r.Notes[0].Findings
	if len(got) != 2 {
		t.Fatalf("findings = %+v", got)
	}
	if got[0].Kind != mechcheck.CrossSegment || got[0].Line != 5 {
		t.Errorf("first finding = %+v, want the cross-segment footnote on line 5", got[0])
	}
	if got[1].Kind != mechcheck.Untraced || got[1].Line != 2 {
		t.Errorf("second finding = %+v, want the untraced statement on line 2", got[1])
	}
	if r.Spans["Arches"] == nil {
		t.Error("the verified trace has no span in Arches")
	}
}

// The cross-segment footnotes are listed in the order the note first defines
// their labels, and a label defined twice takes its last definition.
func TestCheck_FootnotesInFirstDefinitionOrderLastWins(t *testing.T) {
	text := "# 1 X\n\nA.[^1]\nB.[^2]\n\n[^2]: [[S#Arches]] \"two\"\n[^1]: [[S#Arches]] \"one\"\n[^1]: [[S#Load]] \"one again\"\n"
	r := mechcheck.Check(stoneArch(t), []*note.Note{note.Parse("1 X.md", text)})
	var got []string
	for _, f := range r.Notes[0].Findings {
		if f.Kind == mechcheck.CrossSegment {
			got = append(got, fmt.Sprintf("[^%s] line %d %s", f.Ref, f.Line, f.Section))
		}
	}
	if want := []string{"[^2] line 5 Arches", "[^1] line 7 Load"}; !reflect.DeepEqual(got, want) {
		t.Errorf("cross-segment footnotes = %q, want %q", got, want)
	}
}

// A footnote that isn't a trace has no section, so it isn't listed as a
// cross-segment footnote, and the statement that cites it is reported broken.
func TestCheck_FootnoteNotATraceIsNotCrossSegment(t *testing.T) {
	text := "# 1 X\n\nA.[^1]\n\n[^1]: a note to self\n"
	r := mechcheck.Check(stoneArch(t), []*note.Note{note.Parse("1 X.md", text)})
	var broken int
	for _, f := range r.Notes[0].Findings {
		if f.Kind == mechcheck.CrossSegment {
			t.Errorf("listed as cross-segment: %+v", f)
		}
		if f.Kind == mechcheck.SectionNotFound && f.NoTrace && f.Ref == "1" {
			broken++
		}
	}
	if broken != 1 {
		t.Errorf("findings = %+v, want [^1] once as not a trace", r.Notes[0].Findings)
	}
}

// A note numbered for no segment is flagged, and the rest of it is checked,
// since stage 1 flags never stop the run.
func TestCheck_NoteNumberedForNoSegment(t *testing.T) {
	src := stoneArch(t)
	tests := []struct{ name, want string }{
		{"5 Past the end.md", "\n### 5 Past the end.md\n  NO SEGMENT \"5\": the source's segments are 0 to 4\n  UNTRACED statement: A line.\n"},
		{"2a Not a number.md", "\n### 2a Not a number.md\n  NO SEGMENT \"2a\": the source's segments are 0 to 4\n  UNTRACED statement: A line.\n"},
	}
	for _, tt := range tests {
		r := mechcheck.Check(src, []*note.Note{note.Parse(tt.name, "# x\n\nA line.\n")})
		if got := r.String(); got != tt.want {
			t.Errorf("report:\n got %q\nwant %q", got, tt.want)
		}
		if f := r.Notes[0].Findings[0]; f.Kind != mechcheck.NoSegment || !f.Failed() {
			t.Errorf("first finding = %+v, want a failed NoSegment", f)
		}
	}
}

// The trace match ignores case on the quote's first character: a quote
// taken from mid-sentence and capitalised, or from a sentence start and
// lowercased, still matches.
func TestCheck_IgnoresCaseOnTheFirstCharacter(t *testing.T) {
	src := stoneArch(t)
	tests := []struct {
		quote                string
		exact, fuzzy, broken int
	}{
		{"The search for a span, then, is the search for a shape", 1, 0, 0},
		{"the search for a span, then, is the search for a shape", 1, 0, 0},
		{"Is the search for a shape", 1, 0, 0},
		// One letter wrong as well: 13 of 14 characters match, but only with
		// the first one's case ignored.
		{"the search fxr", 0, 1, 0},
		{"Xhe search fxr", 0, 0, 1},
	}
	for _, tt := range tests {
		text := "# 2 Arches\n\nA statement.[^1]\n\n[^1]: [[S#Arches]] \"" + tt.quote + "\"\n"
		s := mechcheck.Check(src, []*note.Note{note.Parse("2 Arches.md", text)}).Stats
		if s.Exact != tt.exact || s.Fuzzy != tt.fuzzy || s.Broken != tt.broken {
			t.Errorf("%q: exact, fuzzy, broken = %d, %d, %d, want %d, %d, %d",
				tt.quote, s.Exact, s.Fuzzy, s.Broken, tt.exact, tt.fuzzy, tt.broken)
		}
	}
}

// A quote in the source as given spans that place, even where its first
// character in another case is earlier: the coverage check is derived from
// the spans.
func TestCheck_PrefersTheQuoteAsGiven(t *testing.T) {
	src := stoneArch(t)
	start := func(quote string) int {
		text := "# 2 Arches\n\nA statement.[^1]\n\n[^1]: [[S#Arches]] \"" + quote + "\"\n"
		r := mechcheck.Check(src, []*note.Note{note.Parse("2 Arches.md", text)})
		if r.Stats.Exact != 1 {
			t.Fatalf("%q: stats %+v, want one exact match", quote, r.Stats)
		}
		return r.Spans["Arches"][0].Start
	}
	if upper, lower := start("Later in the chapter"), start("later in the chapter"); upper >= lower {
		t.Errorf("\"later in the chapter\" spans %d, want its own place after the capitalised one at %d", lower, upper)
	}
}

// Two segments with the same heading have a section each, and a trace marks
// only the one it resolved to as covered.
func TestCheck_RepeatedHeadingCoversOneSegment(t *testing.T) {
	src, err := source.Parse("S", "---\nsource: \"[[@S]]\"\n---\n## Summary\n\nAlpha beta gamma delta. Epsilon zeta eta theta.\n\n## Summary\n\nIota kappa lambda mu. Nu xi omicron pi.\n")
	if err != nil {
		t.Fatal(err)
	}
	n := note.Parse("1 Summary.md", "# 1 Summary\n\nA statement.[^1]\n\n[^1]: [[S#Summary]] \"Iota kappa lambda mu.\"\n")
	c := mechcheck.Check(src, []*note.Note{n}).Coverage
	for _, s := range c.Sections {
		for _, u := range s.Sentences {
			if s.Segment != 1 && u.Partial {
				t.Errorf("segment %d, which nothing traces into, lists %q", s.Segment, u.String())
			}
		}
	}
	if c.Covered != 1 || c.Total != 4 {
		t.Errorf("covered %d/%d, want 1/4", c.Covered, c.Total)
	}
}

// A finding quotes the section it names, as Go quotes a string.
func TestFinding_String(t *testing.T) {
	for f, want := range map[mechcheck.Finding]string{
		{Kind: mechcheck.SectionNotFound, Ref: "7", Section: "Builder's Nowhere"}: `  BROKEN [^7] (section "Builder's Nowhere" not found)`,
		{Kind: mechcheck.SectionNotFound, Ref: "99", NoTrace: true}:               `  BROKEN [^99] (not a trace)`,
	} {
		if got := f.String(); got != want {
			t.Errorf("%+v renders %q, want %q", f, got, want)
		}
	}
}

// Run reads step 1.1's notes and writes the report, the coverage audit file
// and the uncovered lists, each as the check renders it, into its own step
// directory.
func TestRun_WritesTheReport(t *testing.T) {
	src := stoneArch(t)
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	notesDir := filepath.Join(folder.Dir, run.ExtractDir, run.NotesDir)
	writeNotes(t, notesDir, flaggedNotes)
	if err := mechcheck.Run(src, folder); err != nil {
		t.Fatalf("Run: %v (a report with flags is not an error)", err)
	}
	notes, err := note.ReadDir(notesDir)
	if err != nil {
		t.Fatal(err)
	}
	r := mechcheck.Check(src, notes)
	step := filepath.Join(folder.Dir, run.MechanicalCheckDir)
	for file, want := range map[string]string{run.ReportFile: r.String(), run.CoverageFile: r.Coverage.String()} {
		if got, err := os.ReadFile(filepath.Join(step, file)); err != nil {
			t.Fatal(err)
		} else if string(got) != want {
			t.Errorf("%s is not the check's rendered report:\n%s", file, got)
		}
	}
	lists := map[string]string{}
	for _, s := range r.Coverage.Sections {
		lists[s.File()] = s.List()
	}
	if got := files(t, filepath.Join(step, run.UncoveredDir)); !reflect.DeepEqual(got, lists) {
		t.Errorf("uncovered lists are not the check's rendered lists\n got: %q\nwant: %q", got, lists)
	}
}

// A fully covered segment has no uncovered list, and the lists after it keep
// their segments' numbers: segment 2's list is 2.txt, not 1.txt.
func TestCheck_FullyCoveredSegmentHasNoList(t *testing.T) {
	src, err := source.Parse("S", "---\nsource: \"[[@S]]\"\n---\n# Book\n\nZero one two. Zero three four.\n\n## One\n\nOne covered here.\n\n5.\n\n## Two\n\nTwo is partly covered. Two is not.\n")
	if err != nil {
		t.Fatal(err)
	}
	text := "# 1 One\n\nA statement.[^1]\nAnother.[^2]\n\n[^1]: [[S#One]] \"One covered here.\"\n[^2]: [[S#Two]] \"Two is partly\"\n"
	c := mechcheck.Check(src, []*note.Note{note.Parse("1 One.md", text)}).Coverage
	var segs []int
	for _, s := range c.Sections {
		segs = append(segs, s.Segment)
	}
	if !reflect.DeepEqual(segs, []int{0, 2}) {
		t.Fatalf("sections with lists = %v, want [0 2]", segs)
	}
	if got := c.Sections[1].File(); got != "2.txt" {
		t.Errorf("segment 2's list is %s, want 2.txt", got)
	}
	if got, want := c.Sections[1].List(), "# Two\n  ¶1 PARTIAL (uncovered in CAPS): Two is partly COVERED.\n  ¶1 UNCOVERED: Two is not.\n"; got != want {
		t.Errorf("segment 2's list:\n got %q\nwant %q", got, want)
	}
	// "5." has too few letters or digits to count, so the frame has five
	// sentences.
	if c.Covered != 1 || c.Total != 5 {
		t.Errorf("covered %d/%d, want 1/5", c.Covered, c.Total)
	}
	if !strings.HasSuffix(c.String(), "  covered sentences: 1/5 (20%)\n") {
		t.Errorf("totals:\n%s", c.String())
	}
}

// A frame with no sentences to count gives the counts without a
// percentage.
func TestCheck_NoSentencesToCount(t *testing.T) {
	src, err := source.Parse("S", "---\nsource: \"[[@S]]\"\n---\n## One\n\n5. i.\n")
	if err != nil {
		t.Fatal(err)
	}
	c := mechcheck.Check(src, nil).Coverage
	want := "\n## Coverage: source sentences not fully spanned by verified traces\n\n## Totals\n  exact 0, fuzzy 0, broken 0, untraced statements 0, statements 0\n  covered sentences: 0/0\n"
	if got := c.String(); got != want {
		t.Errorf("coverage:\n got %q\nwant %q", got, want)
	}
}

// ParseList reads back every uncovered list the check renders, on each set
// of notes.
func TestParseList_ReadsWhatListRenders(t *testing.T) {
	src := stoneArch(t)
	for r, notes := range map[string]map[string]string{"clean": cleanNotes, "flagged": flaggedNotes} {
		for _, s := range mechcheck.Check(src, parseNotes(notes)).Coverage.Sections {
			got, err := mechcheck.ParseList(s.Segment, s.List())
			if err != nil {
				t.Errorf("%s list %d: %v", r, s.Segment, err)
			} else if !reflect.DeepEqual(got, s) {
				t.Errorf("%s list %d read back as\n%+v\nwant\n%+v", r, s.Segment, got, s)
			}
		}
	}
	if _, err := mechcheck.ParseList(0, "# H\nnot a sentence line\n"); err == nil {
		t.Error("ParseList accepted a line in neither shape")
	}
}

// Locate finds a quote, with punctuation at its ends the source doesn't have
// trimmed as little as it needs to be, and gives the source's own text.
func TestLocator_Locate(t *testing.T) {
	src, err := source.Parse("Src", "---\nsource: \"[[@B - Ann Author]]\"\n---\n\n# T\n\nIt is *so*, he said (“so!”).\n\n## Two\n\nIt is so.\n")
	if err != nil {
		t.Fatal(err)
	}
	l := mechcheck.NewLocator(src)
	tests := []struct {
		seg   int
		quote string
		want  mechcheck.Located
	}{
		{0, "It is _so_,", mechcheck.Located{Section: "T", Quote: "It is *so*,", Exact: true}},
		{0, "it is so.", mechcheck.Located{Section: "Two", Quote: "It is so.", Exact: true}},
		{1, "It is so", mechcheck.Located{Section: "Two", Quote: "It is so", Exact: true}},
		{0, "“so!”.", mechcheck.Located{Section: "T", Quote: "“so!”", Exact: true}},
		{0, "(“so!”);", mechcheck.Located{Section: "T", Quote: "(“so!”)", Exact: true}},
		{1, "Ann Author", mechcheck.Located{Section: "", Quote: "Ann Author", Exact: true}},
		{0, " nowhere ", mechcheck.Located{Section: "T", Quote: "nowhere"}},
		{1, "...", mechcheck.Located{Section: "Two", Quote: "..."}},
		{9, "nowhere", mechcheck.Located{Section: "T", Quote: "nowhere"}},
	}
	for _, tt := range tests {
		got := l.Locate(tt.seg, tt.quote, mechcheck.Place{})
		got.End = mechcheck.Place{}
		if got != tt.want {
			t.Errorf("Locate(%d, %q) = %+v, want %+v", tt.seg, tt.quote, got, tt.want)
		}
	}
}

// Exact takes a quote exact in the section its trace names, as the trace
// match grades it, and nothing it would grade fuzzy or broken, nor a quote
// that is only in another section, or exact only with its ends trimmed.
func TestLocator_Exact(t *testing.T) {
	src, err := source.Parse("Src", "---\nsource: \"[[@B - Ann Author]]\"\n---\n\n# T\n\nIt is *so*, he said (“so!”).\n\n## Two\n\nIt is so.\n")
	if err != nil {
		t.Fatal(err)
	}
	l := mechcheck.NewLocator(src)
	tests := []struct {
		section, quote string
		want           bool
	}{
		{"T", "It is _so_", true},
		{"T", "he said", true},
		{"Two", "it is so.", true},
		{"", "Ann Author", true},
		{"T", "It is so.", false},
		{"Two", "he said", false},
		{"T", "he sad", false},
		{"T", "(“so!”);", false},
		{"Three", "It is so.", false},
		{"", "he said", false},
		{"T", "", false},
	}
	for _, tt := range tests {
		if got := l.Exact(note.Trace{File: "Src", Section: tt.section, Quote: tt.quote}); got != tt.want {
			t.Errorf("Exact(%q, %q) = %v, want %v", tt.section, tt.quote, got, tt.want)
		}
	}
}

// Two segments with one heading: step 1.2 matches a trace naming it in the
// last of them only, and so does Exact.
func TestLocator_ExactTakesTheLastSectionOfAHeading(t *testing.T) {
	src, err := source.Parse("S", "---\nsource: \"[[@S - A]]\"\n---\n\n# Book\n\n## Notes\n\nAlpha is here.\n\n## Middle\n\nBeta.\n\n## Notes\n\nGamma.\n")
	if err != nil {
		t.Fatal(err)
	}
	l := mechcheck.NewLocator(src)
	for quote, want := range map[string]bool{"Alpha": false, "Gamma": true} {
		tr := note.Trace{File: "S", Section: "Notes", Quote: quote}
		r := mechcheck.Check(src, []*note.Note{note.Parse("1 Notes.md", "# 1 Notes\n\nIt.[^1]\n\n[^1]: [[S#Notes]] \""+quote+"\"\n")})
		if step12 := r.Stats.Exact == 1; step12 != want {
			t.Fatalf("step 1.2 grades %q exact = %v, want %v", quote, step12, want)
		}
		if got := l.Exact(tr); got != want {
			t.Errorf("Exact(Notes, %q) = %v, want %v", quote, got, want)
		}
	}
}

// Step 1.2 writes its findings into its directory, and ReadFindings reads
// them back to the notes' reports the check gave, every kind of finding
// among them.
func TestReadFindings_ReadsWhatRunWrote(t *testing.T) {
	src := stoneArch(t)
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	notesDir := filepath.Join(folder.Dir, run.ExtractDir, run.NotesDir)
	writeNotes(t, notesDir, map[string]string{
		"1 Every kind.md": "# 1 Every kind\n\n" +
			"A lead-in[^1]\n" +
			"- lowercase item.[^2]\n" +
			"No terminal punctuation[^3]\n" +
			"An untraced statement.\n" +
			"Broken.[^4]\n" +
			"Fuzzy.[^5]\n" +
			"Missing.[^6]\n\n" +
			"[^1]: [[S#Arches]] \"Similarly, a stone arch and a brick arch are not two sciences\"\n" +
			"[^2]: [[S#Nowhere]] \"A quote.\"\n" +
			"[^3]: [[S#Arches]] \"Similarly, a stone arch and a brick arch are not two sciences\"\n" +
			"[^4]: [[S#Arches]] \"Nothing like this is in the source at all.\"\n" +
			"[^5]: [[S#Arches]] \"the search fxr\"\n",
		"5 Past the end.md": "# x\n\nA line.\n",
		"0 Clean.md":        "# 0 Clean\n",
	})
	if err := mechcheck.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	notes, err := note.ReadDir(notesDir)
	if err != nil {
		t.Fatal(err)
	}
	want := mechcheck.Check(src, notes).Notes
	got, err := mechcheck.ReadFindings(folder, run.MechanicalCheckDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Notes) != len(want) {
		t.Fatalf("read %d notes' findings, want %d", len(got.Notes), len(want))
	}
	kinds := map[mechcheck.Kind]bool{}
	for i, w := range want {
		g := got.Notes[i]
		if g.Name != w.Name || !slices.Equal(g.Findings, w.Findings) {
			t.Errorf("note %d read back as\n %+v\nwant\n %+v", i, g, w)
		}
		for _, f := range w.Findings {
			kinds[f.Kind] = true
		}
	}
	for k := range len(mechcheck.KindNames) {
		if !kinds[mechcheck.Kind(k)] {
			t.Errorf("no finding of kind %d to read back", k)
		}
	}

	b, err := os.ReadFile(filepath.Join(folder.Dir, run.MechanicalCheckDir, run.FindingsFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"kind": "cross-segment"`) {
		t.Errorf("findings.json does not name its kinds:\n%s", b)
	}
}

// A missing findings file is an error naming it.
func TestReadFindings_MissingFile(t *testing.T) {
	folder := &run.Folder{Dir: t.TempDir()}
	_, err := mechcheck.ReadFindings(folder, run.MechanicalCheckDir)
	if err == nil || !strings.Contains(err.Error(), filepath.Join(run.MechanicalCheckDir, run.FindingsFile)) {
		t.Errorf("err = %v, want one naming %s", err, run.FindingsFile)
	}
}

// Every kind has a name for findings.json.
func TestKindNames_EveryKind(t *testing.T) {
	for k, name := range mechcheck.KindNames {
		if name == "" {
			t.Errorf("kind %d has no name", k)
		}
	}
}

// files reads every file in dir, by name.
func files(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = string(b)
	}
	return out
}
