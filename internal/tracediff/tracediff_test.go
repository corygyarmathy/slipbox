package tracediff_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/counter"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/splitcheck"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
	"github.com/corygyarmathy/slipbox/internal/tracediff"
)

// lampReport is the trace diff's report on the invented chapter's passing
// split, with `[[L#` for `[[<source>#`. Segment 0 splits a stage 1 line and
// segment 2 merges two, which changes no sentence. Every segment but 0 adds
// name spans' quotes, which change the sentences they repair: one quote
// twice, one emphasized, and one with a curly apostrophe.
const lampReport = `## 0 Keepers of the Lamp: 2 atomic notes; quotes 3->3
## 1 The Keeper's Watch: 2 atomic notes; quotes 5->8
  QUOTE ADDED x1: the inspectors
  QUOTE ADDED x2: The keeper
  SENTENCE before: Then they read every page.
  SENTENCE before: He trimmed the wick every four hours.
  SENTENCE before: He logged the weather in a book.
  SENTENCE after:  Then the inspectors read every page.
  SENTENCE after:  The keeper trimmed the wick every four hours.
  SENTENCE after:  The keeper logged the weather in a book.
  NAME SPAN x1: the inspectors
  NAME SPAN x2: The keeper
## 2 The Lens: 3 atomic notes; quotes 5->6
  QUOTE ADDED x1: *The Fresnel lens*
  SENTENCE before: It uses rings of prisms instead of one thick glass.
  SENTENCE after:  *The Fresnel lens* uses rings of prisms instead of one thick glass.
  NAME SPAN x1: *The Fresnel lens*
` + lampStorms

// lampStorms is lampReport's segment 3.
const lampStorms = `## 3 Storms: 2 atomic notes; quotes 3->5
  QUOTE ADDED x1: the keeper’s
  QUOTE ADDED x1: The keeper
  SENTENCE before: A wreck was his failure, whatever the weather.
  SENTENCE before: He stayed at the lamp (in 1839, for nine days) through every gale.
  SENTENCE after:  A wreck was the keeper’s failure, whatever the weather.
  SENTENCE after:  The keeper stayed at the lamp (in 1839, for nine days) through every gale.
  NAME SPAN x1: the keeper’s
  NAME SPAN x1: The keeper
`

// lampStormsReworded is segment 3's report once the split rewords a quote
// so that it is no longer exact in the source: the quote is lost, and its
// reworded copy added is no name span's, though the segment's other added
// quotes still are.
const lampStormsReworded = `## 3 Storms: 2 atomic notes; quotes 3->5
  QUOTE LOST x1: stayed at the lamp (in 1839, for nine days) through every gale
  QUOTE ADDED x1: the keeper’s
  QUOTE ADDED x1: The keeper
  QUOTE ADDED x1: stayed at the lamp through every gale
  SENTENCE before: A wreck was his failure, whatever the weather.
  SENTENCE before: He stayed at the lamp (in 1839, for nine days) through every gale.
  SENTENCE after:  A wreck was the keeper’s failure, whatever the weather.
  SENTENCE after:  The keeper stayed at the lamp (in 1839, for nine days) through every gale.
  NAME SPAN x1: the keeper’s
  NAME SPAN x1: The keeper
`

// The invented chapter's split passes with lampReport. Where it rewords one
// of segment 3's quotes, that segment alone fails, with lampStormsReworded.
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := testfixture.LampSplit()
			tt.split(files)
			r := tracediff.Check(testfixture.LampSource(t), testfixture.LampStage1Notes(t), testfixture.LampFolders(t, files))
			if got := r.String(); got != tt.want {
				t.Errorf("report:\n%s\nwant:\n%s", got, tt.want)
			}
			if got := r.FailingFolders(); !slices.Equal(got, tt.failing) || r.Failed() != (len(tt.failing) > 0) {
				t.Errorf("Failed() = %v, FailingFolders() = %q, want %q", r.Failed(), got, tt.failing)
			}
		})
	}
}

// handSource is the source the hand-written notes quote.
const handSource = "---\nsource: \"[[@S - Ann Author]]\"\n---\n\n# Title\n\nThe first and the second, as Dorcanes said.\n\n## Other\n\nx and Brena.\n"

const stage1 = `---
tags: [x]
---
# 1 Title

- First statement, he said.[^1] Its second sentence.[^2]
Aside.

[^1]: [[S#Title]] "first"
[^2]: [[S#Title]] "second"
`

// One atomic note and the hub, from a hand-written stage 1 note: each case
// changes the atomic note.
func TestCheck_Findings(t *testing.T) {
	tests := []struct {
		name, atomic string
		want         tracediff.SegmentReport
		failed       bool
	}{
		{
			name:   "moved",
			atomic: "# A\n\n1. First statement, he said.[^1]\nIts second sentence.[^2] Aside.\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n",
			want: tracediff.SegmentReport{QuotesAfter: 2, LostQuotes: []counter.Entry{}, AddedQuotes: []counter.Entry{},
				SentencesBefore: []string{}, SentencesAfter: []string{}},
		},
		{
			name:   "lost quote",
			atomic: "# A\n\nFirst statement, he said.[^1] Its second sentence.\nAside.\n\n[^1]: [[S#Title]] \"first\"\n",
			want: tracediff.SegmentReport{QuotesAfter: 1, LostQuotes: []counter.Entry{{Key: "second", Count: 1}}, AddedQuotes: []counter.Entry{},
				SentencesBefore: []string{}, SentencesAfter: []string{}},
			failed: true,
		},
		{
			name:   "name span",
			atomic: "# A\n\nFirst statement, Dorcanes[^3] said.[^1] Its second sentence.[^2] Aside.\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n[^3]: [[S#Title]] \"Dorcanes\"\n",
			want: tracediff.SegmentReport{QuotesAfter: 3, LostQuotes: []counter.Entry{}, AddedQuotes: []counter.Entry{{Key: "Dorcanes", Count: 1}},
				NameSpans:       []counter.Entry{{Key: "Dorcanes", Count: 1}},
				SentencesBefore: []string{"First statement, he said."}, SentencesAfter: []string{"First statement, Dorcanes said."}},
		},
		{
			name:   "added twice, exact once",
			atomic: "# A\n\nFirst statement, Dorcanes[^3] said.[^1] Its second sentence.[^2] Aside.\nDorcanes[^4] again.\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n[^3]: [[S#Title]] \"Dorcanes\"\n[^4]: [[S#Other]] \"Dorcanes\"\n",
			want: tracediff.SegmentReport{QuotesAfter: 4, LostQuotes: []counter.Entry{}, AddedQuotes: []counter.Entry{{Key: "Dorcanes", Count: 2}},
				SentencesBefore: []string{"First statement, he said."}, SentencesAfter: []string{"First statement, Dorcanes said.", "Dorcanes again."}},
			failed: true,
		},
		{
			name:   "added quote stage 1 has",
			atomic: "# A\n\nFirst statement, he said.[^1] Its second sentence.[^2] Aside.\nFirst statement, he said.[^3]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n[^3]: [[S#Title]] \"first\"\n",
			want: tracediff.SegmentReport{QuotesAfter: 3, LostQuotes: []counter.Entry{}, AddedQuotes: []counter.Entry{{Key: "first", Count: 1}},
				NameSpans:       []counter.Entry{{Key: "first", Count: 1}},
				SentencesBefore: []string{}, SentencesAfter: []string{"First statement, he said."}},
		},
		{
			name:   "added quote not in the source",
			atomic: "# A\n\nFirst statement, Xenophon[^3] said.[^1] Its second sentence.[^2] Aside.\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n[^3]: [[S#Title]] \"Xenophon\"\n",
			want: tracediff.SegmentReport{QuotesAfter: 3, LostQuotes: []counter.Entry{}, AddedQuotes: []counter.Entry{{Key: "Xenophon", Count: 1}},
				SentencesBefore: []string{"First statement, he said."}, SentencesAfter: []string{"First statement, Xenophon said."}},
			failed: true,
		},
		{
			name:   "added quote in another section than it names",
			atomic: "# A\n\nFirst statement, Brena[^3] said.[^1] Its second sentence.[^2] Aside.\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n[^3]: [[S#Title]] \"Brena\"\n",
			want: tracediff.SegmentReport{QuotesAfter: 3, LostQuotes: []counter.Entry{}, AddedQuotes: []counter.Entry{{Key: "Brena", Count: 1}},
				SentencesBefore: []string{"First statement, he said."}, SentencesAfter: []string{"First statement, Brena said."}},
			failed: true,
		},
		{
			name:   "edited only",
			atomic: "# A\n\nFirst statement, Dorcanes said.[^1] Its second sentence.[^2]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"second\"\n",
			want: tracediff.SegmentReport{QuotesAfter: 2, LostQuotes: []counter.Entry{}, AddedQuotes: []counter.Entry{},
				SentencesBefore: []string{"First statement, he said.", "Aside."}, SentencesAfter: []string{"First statement, Dorcanes said."}},
		},
	}
	stage1Notes := []*note.Note{note.Parse("1 Title.md", stage1), note.Parse("2 Other.md", "# 2 Other\n\nElsewhere.[^1]\n\n[^1]: [[S#Other]] \"x\"\n")}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			folder := split.SegmentFolder{Name: "1 Title", Notes: []*note.Note{note.Parse("A.md", tt.atomic), note.Parse("Title.md", "# Title\n\n1. [[A]]\n")}}
			src, err := source.Parse("S", handSource)
			if err != nil {
				t.Fatal(err)
			}
			r := tracediff.Check(src, stage1Notes, []split.SegmentFolder{folder})
			want := tt.want
			want.Folder, want.AtomicNotes, want.QuotesBefore = "1 Title", 1, 2
			if want.NameSpans == nil {
				want.NameSpans = []counter.Entry{}
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

// A name span whose quote stage 1 also has, traced to another section, is
// taken by both gates: "and" is in both of the source's sections.
func TestCheck_NameSpanOfAQuoteStage1TracesElsewhere(t *testing.T) {
	src, err := source.Parse("S", handSource)
	if err != nil {
		t.Fatal(err)
	}
	stage1Notes := []*note.Note{note.Parse("1 Title.md", "# 1 Title\n\nFirst statement, he said.[^1]\nAnd x.[^2]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Other]] \"and\"\n")}
	folder := split.SegmentFolder{Name: "1 Title", Notes: []*note.Note{
		note.Parse("A.md", "# A\n\nFirst statement, and[^3] he said.[^1]\nAnd x.[^2]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Other]] \"and\"\n[^3]: [[S#Title]] \"and\"\n"),
		note.Parse("Title.md", "# Title\n\n1. [[A]]\n"),
	}}
	folders := []split.SegmentFolder{folder}
	if sc := splitcheck.Check(src, stage1Notes, folders); sc.Failed() {
		t.Fatalf("split check failed:\n%s", sc)
	}
	r := tracediff.Check(src, stage1Notes, folders)
	if r.Failed() {
		t.Errorf("trace diff failed:\n%s", r)
	}
	if want := []counter.Entry{{Key: "and", Count: 1}}; len(r.Segments) != 1 || !reflect.DeepEqual(r.Segments[0].NameSpans, want) {
		t.Errorf("name spans = %+v, want %+v", r.Segments, want)
	}
}

// An added copy of a quote under a trace stage 1 has, as in the stage 1
// note that first showed it, is a name span's when it is exact, wherever it sits: where it
// sits is the split check's to test, and it runs first, so each case runs
// both gates in that order.
func TestCheck_NameSpanStage1Has(t *testing.T) {
	const (
		stage1   = "# 1 Title\n\nHe said the first.[^1]\nDorcanes on the second.[^2][^3]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] %s\n[^3]: [[S#Title]] \"second\"\n"
		hub      = "# Title\n\n1. [[A]]\n2. [[B]]\n"
		first    = "He said the first.[^1]\n\n[^1]: [[S#Title]] \"first\"\n"
		second   = "Dorcanes on the second.[^1][^2]\n\n[^1]: [[S#Title]] \"Dorcanes\"\n[^2]: [[S#Title]] \"second\"\n"
		reworded = "Dorcanes spoke on the second.[^1][^2]\n\n[^1]: [[S#Title]] \"Dorcanes\"\n[^2]: [[S#Title]] \"second\"\n"
		named    = "Dorcanes said the first.[^1][^2]\n\n[^1]: [[S#Title]] \"Dorcanes\"\n[^2]: [[S#Title]] \"first\"\n"
		moved    = "He said the first.[^1][^2]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"Dorcanes\"\n"
	)
	dorcanes := []counter.Entry{{Key: "Dorcanes", Count: 1}}
	tests := []struct {
		name, quote string
		a, b, c     string
		splitFailed bool
		want        []counter.Entry
		failed      bool
	}{
		{name: "on an edited sentence", a: named, b: second, want: dorcanes},
		{name: "fuzzy", quote: `"Dorcanes sad"`, a: strings.ReplaceAll(named, `"Dorcanes"`, `"Dorcanes sad"`), b: strings.ReplaceAll(second, `"Dorcanes"`, `"Dorcanes sad"`), splitFailed: true, failed: true},
		{name: "broken", quote: `"Xenophon"`, a: strings.ReplaceAll(named, `"Dorcanes"`, `"Xenophon"`), b: strings.ReplaceAll(second, `"Dorcanes"`, `"Xenophon"`), splitFailed: true, failed: true},
		{name: "moved onto an unedited sentence", a: moved, b: second, splitFailed: true, want: dorcanes},
		{name: "on a duplicated statement", a: first, b: second, c: second, splitFailed: true, want: []counter.Entry{{Key: "Dorcanes", Count: 1}, {Key: "second", Count: 1}}},
		{name: "on a reworded duplicated statement", a: first, b: second, c: reworded, splitFailed: true, want: []counter.Entry{{Key: "Dorcanes", Count: 1}, {Key: "second", Count: 1}}},
		{name: "moved, with stage 1's sentence edited", a: moved, b: reworded, splitFailed: true, want: dorcanes},
		{name: "more edited copies than are added", a: named, b: reworded, want: dorcanes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quote := tt.quote
			if quote == "" {
				quote = `"Dorcanes"`
			}
			src, err := source.Parse("S", handSource)
			if err != nil {
				t.Fatal(err)
			}
			stage1Notes := []*note.Note{note.Parse("1 Title.md", fmt.Sprintf(stage1, quote))}
			notes := []*note.Note{note.Parse("A.md", "# A\n\n"+tt.a), note.Parse("B.md", "# B\n\n"+tt.b)}
			if tt.c != "" {
				notes = append(notes, note.Parse("C.md", "# C\n\n"+tt.c))
			}
			folders := []split.SegmentFolder{{Name: "1 Title", Notes: append(notes, note.Parse("Title.md", hub))}}
			if sc := splitcheck.Check(src, stage1Notes, folders); sc.Failed() != tt.splitFailed {
				t.Errorf("split check Failed() = %v, want %v\n%s", sc.Failed(), tt.splitFailed, sc)
			}
			r := tracediff.Check(src, stage1Notes, folders)
			if r.Failed() != tt.failed {
				t.Errorf("Failed() = %v, want %v\n%s", r.Failed(), tt.failed, r)
			}
			want := tt.want
			if want == nil {
				want = []counter.Entry{}
			}
			if got := r.Segments[0].NameSpans; !reflect.DeepEqual(got, want) {
				t.Errorf("NameSpans = %+v, want %+v", got, want)
			}
		})
	}
}

// A name repaired into the first sentence of a stage 1 statement of two,
// with the statement's footnote references left at its end, passes both
// gates.
func TestCheck_NameSpanInAStatementOfTwoSentences(t *testing.T) {
	src, err := source.Parse("S", handSource)
	if err != nil {
		t.Fatal(err)
	}
	stage1Notes := []*note.Note{note.Parse("1 Title.md", "# 1 Title\n\nHe said it. It was the first.[^1]\nDorcanes on the second.[^2][^3]\n\n[^1]: [[S#Title]] \"first\"\n[^2]: [[S#Title]] \"Dorcanes\"\n[^3]: [[S#Title]] \"second\"\n")}
	folders := []split.SegmentFolder{{Name: "1 Title", Notes: []*note.Note{
		note.Parse("A.md", "# A\n\nDorcanes said it. It was the first.[^1][^2]\n\n[^1]: [[S#Title]] \"Dorcanes\"\n[^2]: [[S#Title]] \"first\"\n"),
		note.Parse("B.md", "# B\n\nDorcanes on the second.[^1][^2]\n\n[^1]: [[S#Title]] \"Dorcanes\"\n[^2]: [[S#Title]] \"second\"\n"),
		note.Parse("Title.md", "# Title\n\n1. [[A]]\n2. [[B]]\n"),
	}}}
	if sc := splitcheck.Check(src, stage1Notes, folders); sc.Failed() {
		t.Fatalf("split check failed:\n%s", sc)
	}
	r := tracediff.Check(src, stage1Notes, folders)
	if r.Failed() {
		t.Errorf("trace diff failed:\n%s", r)
	}
	if want := []counter.Entry{{Key: "Dorcanes", Count: 1}}; !reflect.DeepEqual(r.Segments[0].NameSpans, want) {
		t.Errorf("name spans = %+v, want %+v", r.Segments[0].NameSpans, want)
	}
}

// A quote and a sentence are cut to their first 120 and 160 code points, not
// bytes, a lost quote prints with its count, and a name span follows the
// segment's other lines, cut as a quote is.
func TestReport_String(t *testing.T) {
	long := strings.Repeat("é", 200)
	r := &tracediff.Report{Segments: []tracediff.SegmentReport{{
		Folder: "3 Arches", AtomicNotes: 2, QuotesBefore: 6, QuotesAfter: 4,
		LostQuotes:      []counter.Entry{{Key: long, Count: 2}},
		SentencesBefore: []string{long},
		SentencesAfter:  []string{"Short."},
		NameSpans:       []counter.Entry{{Key: long, Count: 1}},
	}}}
	want := "## 3 Arches: 2 atomic notes; quotes 6->4\n" +
		"  QUOTE LOST x2: " + strings.Repeat("é", 120) + "\n" +
		"  SENTENCE before: " + strings.Repeat("é", 160) + "\n" +
		"  SENTENCE after:  Short.\n" +
		"  NAME SPAN x1: " + strings.Repeat("é", 120) + "\n"
	if got := r.String(); got != want {
		t.Errorf("String() =\n%s\nwant:\n%s", got, want)
	}
}
