package splitcheck_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/splitcheck"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// lampReport is the split check's report on the invented chapter's passing
// split, with `[[L#` for `[[<source>#`. Segment 0 splits a stage 1 line of
// two statements into two lines and segment 2 merges two into one, so their
// text is edited, with no trace lost or added. Segment 1 counts its aside's
// statements, and every segment but 0 adds name spans: one quote twice, one
// emphasized, and one with a curly apostrophe.
const lampReport = lampKeepers + `
## 1 The Keeper's Watch: 2 atomic notes, statements 5->5, traces 5->8, hub footnotes 0
  extra_traces: [[L#The Keeper’s Watch]] "the inspectors"
  extra_traces: [[L#The Keeper’s Watch]] "The keeper"
  extra_traces: [[L#The Keeper’s Watch]] "The keeper"
  edited_text: "Then the inspectors read every page." in The Log Was Read Only After a Wreck.md
  edited_text: "The keeper trimmed the wick every four hours." in The Watch Began at Dusk.md
  edited_text: "The keeper logged the weather in a book." in The Watch Began at Dusk.md
  name_span: [[L#The Keeper’s Watch]] "the inspectors" on "Then the inspectors read every page." in The Log Was Read Only After a Wreck.md
  name_span: [[L#The Keeper’s Watch]] "The keeper" on "The keeper trimmed the wick every four hours." in The Watch Began at Dusk.md
  name_span: [[L#The Keeper’s Watch]] "The keeper" on "The keeper logged the weather in a book." in The Watch Began at Dusk.md

## 2 The Lens: 3 atomic notes, statements 5->4, traces 5->6, hub footnotes 0
  extra_traces: [[L#The Lens]] "*The Fresnel lens*"
  edited_text: "*The Fresnel lens* uses rings of prisms instead of one thick glass." in Rings of Prisms.md
  edited_text: "The lens bends light into a beam. A beam carries farther than a glow." in The Lens Makes a Beam.md
  name_span: [[L#The Lens]] "*The Fresnel lens*" on "*The Fresnel lens* uses rings of prisms instead of one thick glass." in Rings of Prisms.md
` + lampStorms

// lampKeepers is lampReport's segment 0.
const lampKeepers = `
## 0 Keepers of the Lamp: 2 atomic notes, statements 2->3, traces 3->3, hub footnotes 0
  edited_text: "A lighthouse is a promise made to ships." in A Lighthouse Is a Promise.md
  edited_text: "It burns whether or not a ship is near." in A Lighthouse Is a Promise.md
`

// lampKeepersDropped is segment 0's report once the split drops a statement
// and its trace: the trace is lost, and the segment fails.
const lampKeepersDropped = `
## 0 Keepers of the Lamp: 2 atomic notes, statements 2->2, traces 3->2, hub footnotes 0
  lost_traces: [[L#2. Keepers of the Lamp]] "It burns whether or not a ship is near."
  edited_text: "A lighthouse is a promise made to ships." in A Lighthouse Is a Promise.md
`

// lampStorms is lampReport's segment 3.
const lampStorms = `
## 3 Storms: 2 atomic notes, statements 3->3, traces 3->5, hub footnotes 0
  extra_traces: [[L#Storms]] "the keeper’s"
  extra_traces: [[L#Storms]] "The keeper"
  edited_text: "A wreck was the keeper’s failure, whatever the weather." in In a Storm the Light Matters Most.md
  edited_text: "The keeper stayed at the lamp (in 1839, for nine days) through every gale." in The Keeper Stayed Through Every Gale.md
  name_span: [[L#Storms]] "the keeper’s" on "A wreck was the keeper’s failure, whatever the weather." in In a Storm the Light Matters Most.md
  name_span: [[L#Storms]] "The keeper" on "The keeper stayed at the lamp (in 1839, for nine days) through every gale." in The Keeper Stayed Through Every Gale.md
`

// lampStormsReworded is segment 3's report once the split rewords a quote
// so that it is no longer exact in the source: the trace is lost, and its
// reworded copy added is no name span. Neither is the name span beside it,
// on a statement left with no stage 1 trace.
const lampStormsReworded = `
## 3 Storms: 2 atomic notes, statements 3->3, traces 3->5, hub footnotes 0
  lost_traces: [[L#Storms]] "stayed at the lamp (in 1839, for nine days) through every gale"
  extra_traces: [[L#Storms]] "the keeper’s"
  extra_traces: [[L#Storms]] "The keeper"
  extra_traces: [[L#Storms]] "stayed at the lamp through every gale"
  edited_text: "A wreck was the keeper’s failure, whatever the weather." in In a Storm the Light Matters Most.md
  edited_text: "The keeper stayed at the lamp (in 1839, for nine days) through every gale." in The Keeper Stayed Through Every Gale.md
  name_span: [[L#Storms]] "the keeper’s" on "A wreck was the keeper’s failure, whatever the weather." in In a Storm the Light Matters Most.md
`

// The invented chapter's split passes with lampReport. Where it rewords one
// of segment 3's quotes, that segment alone fails, with
// lampStormsReworded, beside the name span it still adds. Where it drops one
// of segment 0's statements, that segment alone fails, with
// lampKeepersDropped.
func TestCheck_Lamp(t *testing.T) {
	const quote = `"stayed at the lamp (in 1839, for nine days) through every gale"`
	tests := []struct {
		name    string
		split   func(map[string]string)
		failing []string
		want    string
	}{
		{name: "passes", split: func(map[string]string) {}, want: lampReport},
		{
			name: "reworded quote",
			split: func(files map[string]string) {
				const n = "3 Storms/The Keeper Stayed Through Every Gale.md"
				files[n] = strings.Replace(files[n], quote, `"stayed at the lamp through every gale"`, 1)
			},
			failing: []string{"3 Storms"},
			want:    strings.TrimSuffix(lampReport, lampStorms) + lampStormsReworded,
		}, {
			name: "dropped statement",
			split: func(files map[string]string) {
				const n = "0 Keepers of the Lamp/A Lighthouse Is a Promise.md"
				for _, line := range []string{"It burns whether or not a ship is near.[^2]\n", "[^2]: " + testfixture.LampTrace("2. Keepers of the Lamp", "It burns whether or not a ship is near.") + "\n"} {
					files[n] = strings.Replace(files[n], line, "", 1)
				}
			},
			failing: []string{"0 Keepers of the Lamp"},
			want:    lampKeepersDropped + strings.TrimPrefix(lampReport, lampKeepers),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := testfixture.LampSplit()
			tt.split(files)
			r := splitcheck.Check(testfixture.LampSource(t), testfixture.LampStage1Notes(t), testfixture.LampFolders(t, files))
			want := strings.ReplaceAll(tt.want, "[[L#", "[["+testfixture.Lamp+"#")
			if got := r.String(); got != want {
				t.Errorf("report:\n%s\nwant:\n%s", got, want)
			}
			if got := r.FailingFolders(); !slices.Equal(got, tt.failing) || r.Failed() != (len(tt.failing) > 0) {
				t.Errorf("Failed() = %v, FailingFolders() = %q, want %q", r.Failed(), got, tt.failing)
			}
		})
	}
}

// parseSource parses text as the source `S`.
func parseSource(t *testing.T, text string) *source.Source {
	t.Helper()
	src, err := source.Parse("S", text)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// handSource is the source the hand-written notes quote.
const handSource = "---\nsource: \"[[@S - Ann Author]]\"\n---\n\n# Title\n\nThe first and the second, as Dorcanes said.\n\n## Other\n\nx and Brena.\n"

const stage1 = `---
tags: [x]
---
# 1 Title

- First statement.[^1] Its second sentence.[^2]
Aside.
## Heading
Untraced in stage 1.

[^1]: [[S#Title]] "first"
[^2]: [[S#Title]] "second"
`

// One atomic note and the hub, from a hand-written stage 1 note: each case
// changes the atomic note or the hub.
func TestCheck_Findings(t *testing.T) {
	const hub = "# Title\n\n1. [[A]]\n"
	tests := []struct {
		name, atomic, hub string
		want              splitcheck.SegmentReport
		failed            bool
	}{
		{
			name:   "moved",
			atomic: "# A\n\n1. First statement.[^1] Its second sentence.[^2]\nUntraced in stage 1.[^3]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n[^3]: [[S#Title]] \"second\"\n",
			hub:    hub,
			want: splitcheck.SegmentReport{StatementsAfter: 2, TracesAfter: 3,
				LostTraces: []string{}, ExtraTraces: []string{`[[S#Title]] "second"`}},
			failed: true,
		},
		{
			name:   "lost trace",
			atomic: "# A\n\nFirst statement.[^1] Its second sentence.\n\n[^1]: [[S#Title]] \"first\"\n",
			hub:    hub,
			want: splitcheck.SegmentReport{StatementsAfter: 1, TracesAfter: 1,
				LostTraces: []string{`[[S#Title]] "second"`}, ExtraTraces: []string{}},
			failed: true,
		},
		{
			name:   "edited and untraced",
			atomic: "# A\n\nFirst statement, by Dorcanes.[^1][^9] Its second sentence.[^2]\nNew line.\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n",
			hub:    hub,
			want: splitcheck.SegmentReport{StatementsAfter: 2, TracesAfter: 3,
				LostTraces: []string{}, ExtraTraces: []string{"?"},
				Untraced: []splitcheck.Statement{{Text: "New line.", Note: "A.md"}},
				Edited:   []splitcheck.Statement{{Text: "First statement, by Dorcanes. Its second sentence.", Note: "A.md"}}},
			failed: true,
		},
		{
			name:   "edited only",
			atomic: "# A\n\nFirst statement, by Dorcanes.[^1] Its second sentence.[^2]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n",
			hub:    hub,
			want: splitcheck.SegmentReport{StatementsAfter: 1, TracesAfter: 2,
				LostTraces: []string{}, ExtraTraces: []string{},
				Edited: []splitcheck.Statement{{Text: "First statement, by Dorcanes. Its second sentence.", Note: "A.md"}}},
			failed: false,
		},
		{
			name:   "name span",
			atomic: "# A\n\nFirst statement, by Dorcanes.[^1][^3] Its second sentence.[^2]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n[^3]: [[S#Title]] \"Dorcanes\"\n",
			hub:    hub,
			want: splitcheck.SegmentReport{StatementsAfter: 1, TracesAfter: 3,
				LostTraces: []string{}, ExtraTraces: []string{`[[S#Title]] "Dorcanes"`},
				Edited:    []splitcheck.Statement{{Text: "First statement, by Dorcanes. Its second sentence.", Note: "A.md"}},
				NameSpans: []splitcheck.NameSpan{{Trace: `[[S#Title]] "Dorcanes"`, Statement: splitcheck.Statement{Text: "First statement, by Dorcanes. Its second sentence.", Note: "A.md"}}}},
			failed: false,
		},
		{
			name:   "name span from the source line",
			atomic: "# A\n\nFirst statement, by Ann Author.[^1][^3] Its second sentence.[^2]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n[^3]: [[S]] \"Ann Author\"\n",
			hub:    hub,
			want: splitcheck.SegmentReport{StatementsAfter: 1, TracesAfter: 3,
				LostTraces: []string{}, ExtraTraces: []string{`[[S]] "Ann Author"`},
				Edited:    []splitcheck.Statement{{Text: "First statement, by Ann Author. Its second sentence.", Note: "A.md"}},
				NameSpans: []splitcheck.NameSpan{{Trace: `[[S]] "Ann Author"`, Statement: splitcheck.Statement{Text: "First statement, by Ann Author. Its second sentence.", Note: "A.md"}}}},
			failed: false,
		},
		{
			name:   "added trace not in the source",
			atomic: "# A\n\nFirst statement, by Xenophon.[^1][^3] Its second sentence.[^2]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n[^3]: [[S#Title]] \"Xenophon\"\n",
			hub:    hub,
			want: splitcheck.SegmentReport{StatementsAfter: 1, TracesAfter: 3,
				LostTraces: []string{}, ExtraTraces: []string{`[[S#Title]] "Xenophon"`},
				Edited: []splitcheck.Statement{{Text: "First statement, by Xenophon. Its second sentence.", Note: "A.md"}}},
			failed: true,
		},
		{
			name:   "added trace in another section than it names",
			atomic: "# A\n\nFirst statement, by Brena.[^1][^3] Its second sentence.[^2]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n[^3]: [[S#Title]] \"Brena\"\n",
			hub:    hub,
			want: splitcheck.SegmentReport{StatementsAfter: 1, TracesAfter: 3,
				LostTraces: []string{}, ExtraTraces: []string{`[[S#Title]] "Brena"`},
				Edited: []splitcheck.Statement{{Text: "First statement, by Brena. Its second sentence.", Note: "A.md"}}},
			failed: true,
		},
		{
			name:   "added trace fuzzy in the source",
			atomic: "# A\n\nFirst statement, by Dorcanes.[^1][^3] Its second sentence.[^2]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n[^3]: [[S#Title]] \"the first and the secnd, as Dorcanes said\"\n",
			hub:    hub,
			want: splitcheck.SegmentReport{StatementsAfter: 1, TracesAfter: 3,
				LostTraces: []string{}, ExtraTraces: []string{`[[S#Title]] "the first and the secnd, as Dorcanes said"`},
				Edited: []splitcheck.Statement{{Text: "First statement, by Dorcanes. Its second sentence.", Note: "A.md"}}},
			failed: true,
		},
		{
			name:   "duplicated statement",
			atomic: "# A\n\nFirst statement.[^1] Its second sentence.[^2]\nFirst statement.[^1] Its second sentence.[^2]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n",
			hub:    hub,
			want: splitcheck.SegmentReport{StatementsAfter: 2, TracesAfter: 4,
				LostTraces: []string{}, ExtraTraces: []string{`[[S#Title]] "first"`, `[[S#Title]] "second"`}},
			failed: true,
		},
		{
			name:   "statement of added traces only",
			atomic: "# A\n\nFirst statement.[^1] Its second sentence.[^2]\nDorcanes said.[^3]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n[^3]: [[S#Title]] \"Dorcanes\"\n",
			hub:    hub,
			want: splitcheck.SegmentReport{StatementsAfter: 2, TracesAfter: 3,
				LostTraces: []string{}, ExtraTraces: []string{`[[S#Title]] "Dorcanes"`},
				Edited: []splitcheck.Statement{{Text: "Dorcanes said.", Note: "A.md"}}},
			failed: true,
		},
		{
			name:   "hub footnote",
			atomic: "# A\n\nFirst statement.[^1] Its second sentence.[^2]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n",
			hub:    hub + "\n[^1]: x\n",
			want: splitcheck.SegmentReport{StatementsAfter: 1, TracesAfter: 2,
				LostTraces: []string{}, ExtraTraces: []string{}, HubFootnotes: 1},
			failed: true,
		},
	}
	stage1Notes := []*note.Note{note.Parse("1 Title.md", stage1), note.Parse("2 Other.md", "# 2 Other\n\nElsewhere.[^1]\n\n[^1]: [[S#Other]] \"x\"\n")}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			folder := split.SegmentFolder{Name: "1 Title", Notes: []*note.Note{note.Parse("A.md", tt.atomic), note.Parse("Title.md", tt.hub)}}
			r := splitcheck.Check(parseSource(t, handSource), stage1Notes, []split.SegmentFolder{folder})
			want := tt.want
			want.Folder, want.AtomicNotes, want.StatementsBefore, want.TracesBefore = "1 Title", 1, 2, 2
			if want.NameSpans == nil {
				want.NameSpans = []splitcheck.NameSpan{}
			}
			if len(r.Segments) != 1 || !reflect.DeepEqual(r.Segments[0], want) {
				t.Fatalf("segments = %+v, want [%+v]", r.Segments, want)
			}
			if r.Failed() != tt.failed {
				t.Errorf("Failed() = %v, want %v", r.Failed(), tt.failed)
			}
		})
	}
}

// An extra copy of a trace stage 1 carries, as in the stage 1 note that
// first showed it, is a name span only on an anchored statement with edited text, and only
// when its copies on unedited statements are stage 1's own.
func TestCheck_NameSpanStage1Carries(t *testing.T) {
	const (
		stage1   = "# 1 Title\n\nHe said the first.[^1]\nDorcanes on the second.[^2][^3]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] %s\n[^3]: [[S#Title]] \"second\"\n"
		dorcanes = `[[S#Title]] "Dorcanes"`
		hub      = "# Title\n\n1. [[A]]\n2. [[B]]\n"
		first    = "He said the first.[^1]\n\n[^1]: [[S#Title]] \"first\"\n"
		second   = "Dorcanes on the second.[^1][^2]\n\n[^1]: [[S#Title]] \"Dorcanes\"\n[^2]: [[S#Title]] \"second\"\n"
		named    = "Dorcanes said the first.[^1][^2]\n\n[^1]: [[S#Title]] \"Dorcanes\"\n[^2]: [[S#Title]] \"first\"\n"
	)
	tests := []struct {
		name, quote string
		a, b, c     string
		want        []splitcheck.NameSpan
		failed      bool
	}{
		{name: "on an edited, anchored statement", a: named, b: second,
			want: []splitcheck.NameSpan{{Trace: dorcanes, Statement: splitcheck.Statement{Text: "Dorcanes said the first.", Note: "A.md"}}}},
		{name: "fuzzy", quote: `"Dorcanes sad"`, a: strings.ReplaceAll(named, `"Dorcanes"`, `"Dorcanes sad"`), b: strings.ReplaceAll(second, `"Dorcanes"`, `"Dorcanes sad"`), failed: true},
		{name: "broken", quote: `"Xenophon"`, a: strings.ReplaceAll(named, `"Dorcanes"`, `"Xenophon"`), b: strings.ReplaceAll(second, `"Dorcanes"`, `"Xenophon"`), failed: true},
		{name: "moved onto an unedited statement", a: "He said the first.[^1][^2]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"Dorcanes\"\n", b: second, failed: true},
		{name: "on a duplicated statement", a: first, b: second, c: second, failed: true},
		{name: "on a reworded duplicated statement", a: first, b: second, c: "Dorcanes spoke on the second.[^1][^2]\n\n[^1]: [[S#Title]] \"Dorcanes\"\n[^2]: [[S#Title]] \"second\"\n", failed: true},
		{name: "on a statement of no stage 1 trace", a: first, b: second, c: "Dorcanes spoke.[^1]\n\n[^1]: [[S#Title]] \"Dorcanes\"\n", failed: true},
		{name: "moved, with stage 1's statement edited", a: "He said the first.[^1][^2]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"Dorcanes\"\n", b: "Dorcanes spoke on the second.[^1][^2]\n\n[^1]: [[S#Title]] \"Dorcanes\"\n[^2]: [[S#Title]] \"second\"\n", failed: true},
		{name: "more edited copies than are extra", a: named, b: "Dorcanes spoke on the second.[^1][^2]\n\n[^1]: [[S#Title]] \"Dorcanes\"\n[^2]: [[S#Title]] \"second\"\n",
			want: []splitcheck.NameSpan{{Trace: dorcanes, Statement: splitcheck.Statement{Text: "Dorcanes said the first.", Note: "A.md"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quote := tt.quote
			if quote == "" {
				quote = `"Dorcanes"`
			}
			stage1Notes := []*note.Note{note.Parse("1 Title.md", fmt.Sprintf(stage1, quote))}
			notes := []*note.Note{note.Parse("A.md", "# A\n\n"+tt.a), note.Parse("B.md", "# B\n\n"+tt.b)}
			if tt.c != "" {
				notes = append(notes, note.Parse("C.md", "# C\n\n"+tt.c))
			}
			folder := split.SegmentFolder{Name: "1 Title", Notes: append(notes, note.Parse("Title.md", hub))}
			r := splitcheck.Check(parseSource(t, handSource), stage1Notes, []split.SegmentFolder{folder})
			if r.Failed() != tt.failed {
				t.Errorf("Failed() = %v, want %v\n%s", r.Failed(), tt.failed, r)
			}
			want := tt.want
			if want == nil {
				want = []splitcheck.NameSpan{}
			}
			if got := r.Segments[0].NameSpans; !reflect.DeepEqual(got, want) {
				t.Errorf("NameSpans = %+v, want %+v", got, want)
			}
		})
	}
}

// A statement prints as its quoted text and the note it is in.
func TestReport_String(t *testing.T) {
	r := &splitcheck.Report{Segments: []splitcheck.SegmentReport{{
		Folder: "3 Arches", AtomicNotes: 2, StatementsBefore: 4, StatementsAfter: 5, TracesBefore: 6, TracesAfter: 5,
		LostTraces: []string{`[[S#Arches]] "it's"`},
		Untraced:   []splitcheck.Statement{{Text: "Dorcanes' view.", Note: "Dorcanes' View.md"}},
		Edited:     []splitcheck.Statement{{Text: `He said "it's" so.`, Note: "N.md"}},
		NameSpans:  []splitcheck.NameSpan{{Trace: `[[S#Arches]] "Dorcanes"`, Statement: splitcheck.Statement{Text: "Dorcanes' view.", Note: "N.md"}}},
	}}}
	want := "\n## 3 Arches: 2 atomic notes, statements 4->5, traces 6->5, hub footnotes 0\n" +
		"  lost_traces: [[S#Arches]] \"it's\"\n" +
		"  untraced_after: \"Dorcanes' view.\" in Dorcanes' View.md\n" +
		"  edited_text: \"He said \\\"it's\\\" so.\" in N.md\n" +
		"  name_span: [[S#Arches]] \"Dorcanes\" on \"Dorcanes' view.\" in N.md\n"
	if got := r.String(); got != want {
		t.Errorf("String() =\n%s\nwant:\n%s", got, want)
	}
}

// Lost traces are listed in the order the stage 1 notes first give them, not
// sorted, so the report reads down the notes.
func TestCheck_LostTracesInFirstCountedOrder(t *testing.T) {
	stage1Notes := []*note.Note{note.Parse("1 Title.md", "# 1 Title\n\nB.[^1] A.[^2]\n\n[^1]: [[S#Title]] \"second\"\n[^2]: [[S#Title]] \"first\"\n")}
	folder := split.SegmentFolder{Name: "1 Title", Notes: []*note.Note{
		note.Parse("A.md", "# A\n\nB. A.\n"),
		note.Parse("Title.md", "# Title\n\n1. [[A]]\n"),
	}}
	r := splitcheck.Check(parseSource(t, handSource), stage1Notes, []split.SegmentFolder{folder})
	if want := []string{`[[S#Title]] "second"`, `[[S#Title]] "first"`}; !reflect.DeepEqual(r.Segments[0].LostTraces, want) {
		t.Errorf("LostTraces = %q, want %q", r.Segments[0].LostTraces, want)
	}
}

// An empty frontmatter, `---` then `---`, closes at its second line, as the
// note model reads it.
func TestCheck_EmptyFrontmatter(t *testing.T) {
	stage1Notes := []*note.Note{note.Parse("1 T.md", "# 1 T\n\n- A.[^1]\n\n[^1]: [[S#X]] \"a\"\n")}
	folder := split.SegmentFolder{Name: "1 T", Notes: []*note.Note{
		note.Parse("A.md", "---\n---\n# A\n\nA.[^1]\n\n[^1]: [[S#X]] \"a\"\n"),
		note.Parse("T.md", "# T\n\n1. [[A]]\n"),
	}}
	r := splitcheck.Check(parseSource(t, handSource), stage1Notes, []split.SegmentFolder{folder})
	if r.Failed() || r.Segments[0].StatementsAfter != 1 {
		t.Errorf("report = %+v, want 1 statement and a pass", r.Segments)
	}
}
