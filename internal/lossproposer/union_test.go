package lossproposer_test

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/lossproposer"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
)

func TestParse(t *testing.T) {
	reply := "# Arches\n\n" +
		"1. **First idea**\n" +
		"   - Quote: \"One sentence.\"\n" +
		"   - Quote: “Two *sentence*.” (and more)\n" +
		"   - Why: It matters.\n\n" +
		"2. Second idea\n" +
		"* Quote: no marks at all\n" +
		"* Why: Also.\n\n" +
		"No other losses proposed.\n"
	got := lossproposer.Parse(reply)
	want := lossproposer.Reply{
		Items: []lossproposer.Item{
			{Idea: "First idea", Quotes: []string{"One sentence.", "Two *sentence*."}, Why: "It matters.",
				Text: "1. **First idea**\n   - Quote: \"One sentence.\"\n   - Quote: “Two *sentence*.” (and more)\n   - Why: It matters."},
			{Idea: "Second idea", Quotes: []string{"no marks at all"}, Why: "Also.",
				Text: "2. Second idea\n* Quote: no marks at all\n* Why: Also."},
		},
		Other: []string{"No other losses proposed."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse =\n%+v\nwant\n%+v", got, want)
	}

	if got := lossproposer.Parse("# Arches\n\nNo losses proposed."); len(got.Items) != 0 || !reflect.DeepEqual(got.Other, []string{"No losses proposed."}) {
		t.Errorf("Parse of no losses = %+v", got)
	}
}

// item is a one-item reply whose quotes are quotes.
func item(idea string, quotes ...string) string {
	var b strings.Builder
	b.WriteString("1. **" + idea + "**\n")
	for _, q := range quotes {
		b.WriteString("   - Quote: \"" + q + "\"\n")
	}
	b.WriteString("   - Why: w\n")
	return b.String()
}

// Items from different runs merge when they cite any of the same uncovered
// text.
func TestUnion_MergesItemsCitingTheSameText(t *testing.T) {
	list := mechcheck.SectionCoverage{Segment: 1, Heading: "H", Sentences: []mechcheck.UncoveredSentence{
		{Paragraph: 1, Text: "The first ‘long’ sentence, which runs on."},
		{Paragraph: 1, Partial: true, Text: "Some covered words AND SOME UNCOVERED ONES."},
		{Paragraph: 2, Text: "A third."},
		{Paragraph: 3, Text: "Not a third."},
	}}
	tests := []struct {
		name   string
		r1, r2 string
		// want gives each loss's wordings by run, as "1+2".
		want []string
	}{
		{"same quote", item("a", "A third."), item("b", "A third."), []string{"1+2"}},
		{"part of a line, against the whole line", item("a", "which runs on"), item("b", "The first 'long' sentence, which runs on."), []string{"1+2"}},
		{"case restored against capitals", item("a", "and some uncovered ones."), item("b", "some covered words and some uncovered ones"), []string{"1+2"}},
		{"a quote spanning two lines", item("a", "A third."), item("b", "and some uncovered ones. A third."), []string{"1+2"}},
		{"different lines", item("a", "A third."), item("b", "which runs on"), []string{"1", "2"}},
		{"any shared line", item("a", "A third.", "which runs on"), item("b", "which runs on"), []string{"1+2"}},
		{"unmatched quotes, the same after normalising", item("a", "A third.", "Not in the *list*."), item("b", "which runs on", "not in the list."), []string{"1+2"}},
		{"unmatched quotes, different", item("a", "A third.", "Not in the list."), item("b", "which runs on", "Elsewhere."), []string{"1", "2"}},
		{"a line inside another, quoted whole", item("a", "A third."), item("b", "Not a third."), []string{"1", "2"}},
		{"one run's items, one line", item("a", "A third.") + "\n2. **c**\n   - Quote: \"A third.\"\n", "No losses proposed.", []string{"1", "1"}},
		{"into the first loss only", item("a", "A third.") + "\n2. **c**\n   - Quote: \"which runs on\"\n", item("b", "A third.", "which runs on"), []string{"1+2", "1"}},
		{"at most one item per run", item("a", "A third."), item("b", "A third.") + "\n2. **d**\n   - Quote: \"A third.\"\n", []string{"1+2", "2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seg := lossproposer.Union(list, []lossproposer.Reply{lossproposer.Parse(tt.r1), lossproposer.Parse(tt.r2)})
			var got []string
			for _, l := range seg.Losses {
				var runs []string
				for _, w := range l.Wordings {
					runs = append(runs, strconv.Itoa(w.Run))
				}
				got = append(got, strings.Join(runs, "+"))
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("losses by run = %q, want %q", got, tt.want)
			}
			if seg.Segment != 1 || seg.Heading != "H" {
				t.Errorf("segment %d %q, want 1 \"H\"", seg.Segment, seg.Heading)
			}
		})
	}
}

// An item whose quotes cite no uncovered line isn't a loss: it goes to
// Remarks, in run order, apart from the free lines in Other. One that cites
// a line stays a loss, whatever its why says.
func TestUnion_AnItemCitingNoLineIsARemark(t *testing.T) {
	list := mechcheck.SectionCoverage{Segment: 1, Heading: "H", Sentences: []mechcheck.UncoveredSentence{
		{Paragraph: 1, Text: "A third."},
	}}
	unquoted := "1. **Covered already**\n   - Quote: none uncovered (note 28 covers this).\n   - Why: Already covered, so no item.\n"
	withdrawn := "2. **Weak**\n   - Quote: \"A third.\"\n   - Why: It is a weak item and I would drop it.\n"
	tests := []struct {
		name   string
		r1, r2 string
		// losses gives each loss's wordings by run, as "1+2".
		losses []string
		// remarks are the remarks as Remark gives them.
		remarks []string
		other   []lossproposer.Line
	}{
		{"an unquoted Quote line", unquoted + "\nNo other losses proposed.\n", "No losses proposed.", nil, []string{
			"run 1's item: 1. **Covered already** - Quote: none uncovered (note 28 covers this). - Why: Already covered, so no item.",
		}, []lossproposer.Line{
			{Run: 1, Text: "No other losses proposed."},
			{Run: 2, Text: "No losses proposed."},
		}},
		{"a quote that matches no line", item("a", "Not in the list."), item("b", "A third."), []string{"2"}, []string{
			"run 1's item: 1. **a** - Quote: \"Not in the list.\" - Why: w",
		}, []lossproposer.Line{}},
		{"no Quote line", item("a"), "No losses proposed.", nil, []string{
			"run 1's item: 1. **a** - Why: w",
		}, []lossproposer.Line{
			{Run: 2, Text: "No losses proposed."},
		}},
		{"a withdrawn item that cites a line", unquoted + withdrawn, item("b", "A third."), []string{"1+2"}, []string{
			"run 1's item: 1. **Covered already** - Quote: none uncovered (note 28 covers this). - Why: Already covered, so no item.",
		}, []lossproposer.Line{}},
		{"the same remark from both runs", unquoted, unquoted, nil, []string{
			"run 1's item: 1. **Covered already** - Quote: none uncovered (note 28 covers this). - Why: Already covered, so no item.",
			"run 2's item: 1. **Covered already** - Quote: none uncovered (note 28 covers this). - Why: Already covered, so no item.",
		}, []lossproposer.Line{}},
		{"no remark", item("a", "A third."), "No losses proposed.", []string{"1"}, nil, []lossproposer.Line{
			{Run: 2, Text: "No losses proposed."},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seg := lossproposer.Union(list, []lossproposer.Reply{lossproposer.Parse(tt.r1), lossproposer.Parse(tt.r2)})
			var got []string
			for _, l := range seg.Losses {
				var runs []string
				for _, w := range l.Wordings {
					runs = append(runs, strconv.Itoa(w.Run))
				}
				got = append(got, strings.Join(runs, "+"))
			}
			if !reflect.DeepEqual(got, tt.losses) {
				t.Errorf("losses by run = %q, want %q", got, tt.losses)
			}
			var remarks []string
			for _, it := range seg.Remarks {
				remarks = append(remarks, lossproposer.Remark(it))
			}
			if !reflect.DeepEqual(remarks, tt.remarks) {
				t.Errorf("remarks =\n%q\nwant\n%q", remarks, tt.remarks)
			}
			if !reflect.DeepEqual(seg.Other, tt.other) {
				t.Errorf("other =\n%+v\nwant\n%+v", seg.Other, tt.other)
			}
		})
	}
}

// A reply with one item quoting "none uncovered" and the free line "No
// losses proposed." gives one remark and one free line, apart.
func TestUnion_ARemarkAndAFreeLineAreApart(t *testing.T) {
	list := mechcheck.SectionCoverage{Segment: 1, Heading: "H", Sentences: []mechcheck.UncoveredSentence{
		{Paragraph: 1, Text: "A third."},
	}}
	reply := "1. **Covered already**\n   - Quote: none uncovered\n   - Why: Already covered.\n\nNo losses proposed.\n"
	seg := lossproposer.Union(list, []lossproposer.Reply{lossproposer.Parse(reply)})
	if len(seg.Losses) != 0 {
		t.Errorf("losses = %+v, want none", seg.Losses)
	}
	if len(seg.Remarks) != 1 || seg.Remarks[0].Run != 1 || seg.Remarks[0].Idea != "Covered already" {
		t.Errorf("remarks = %+v, want run 1's \"Covered already\"", seg.Remarks)
	}
	if want := []lossproposer.Line{{Run: 1, Text: "No losses proposed."}}; !reflect.DeepEqual(seg.Other, want) {
		t.Errorf("other = %+v, want %+v", seg.Other, want)
	}
}

// A quote that shortens a line with an ellipsis, or runs from one line into
// the next, cites the lines its pieces fall in, so its item is a loss.
func TestUnion_ShortenedAndStraddlingQuotes(t *testing.T) {
	list := mechcheck.SectionCoverage{Segment: 1, Heading: "H", Sentences: []mechcheck.UncoveredSentence{
		{Paragraph: 1, Text: "The first long sentence, which runs on and on."},
		{Paragraph: 1, Text: "A second sentence follows it."},
	}}
	tests := []struct {
		name   string
		r1, r2 string
		// losses gives each loss's wordings by run, as "1+2".
		losses []string
	}{
		{"shortened", item("a", "The first long sentence … runs on and on."), "No losses proposed.", []string{"1"}},
		{"shortened with three dots in brackets", item("a", "The first long sentence [...] runs on and on."), "No losses proposed.", []string{"1"}},
		{"straddling", item("a", "which runs on and on. A second sentence"), "No losses proposed.", []string{"1"}},
		{"straddling, against the second line alone", item("a", "A second sentence follows it."), item("b", "which runs on and on. A second sentence"), []string{"1+2"}},
		{"straddling, against the first line alone", item("a", "The first long sentence"), item("b", "which runs on and on. A second sentence"), []string{"1+2"}},
		{"shortened, against its line", item("a", "The first long sentence, which runs on and on."), item("b", "The first long sentence … runs on and on."), []string{"1+2"}},
		{"shortened, against the other line", item("a", "A second sentence follows it."), item("b", "The first long sentence … runs on and on."), []string{"1", "2"}},
		{"shortened with a spaced ellipsis", item("a", "The first long sentence . . . runs on and on."), "No losses proposed.", []string{"1"}},
		{"cut at its end, then a full stop", item("a", "A second sentence follows it."), item("b", "The first long sentence, which runs on and on...."), []string{"1", "2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seg := lossproposer.Union(list, []lossproposer.Reply{lossproposer.Parse(tt.r1), lossproposer.Parse(tt.r2)})
			var got []string
			for _, l := range seg.Losses {
				var runs []string
				for _, w := range l.Wordings {
					runs = append(runs, strconv.Itoa(w.Run))
				}
				got = append(got, strings.Join(runs, "+"))
			}
			if !reflect.DeepEqual(got, tt.losses) {
				t.Errorf("losses by run = %q, want %q", got, tt.losses)
			}
			if len(seg.Remarks) != 0 {
				t.Errorf("remarks %+v, want none", seg.Remarks)
			}
		})
	}
}

// A shortened quote cites the lines that hold all its pieces in order, not
// each line that holds one of them, so it doesn't merge with another run's
// item quoting a line that shares only a short piece.
func TestUnion_ShortenedQuoteCitesTheLineHoldingAllItsPieces(t *testing.T) {
	tests := []struct {
		name         string
		line1, line2 string
		r1, r2       string
	}{
		{"short piece", "The rule, once it has been stated plainly, is clear.", "The rule of thumb fails here.",
			item("a", "The rule of thumb fails here."), item("b", "The rule … is clear.")},
		{"a line with an ellipsis, quoted whole", "It is a question of facts … and of meaning.", "They spoke of facts and of meaning.",
			item("a", "They spoke of facts and of meaning."), item("b", "It is a question of facts … and of meaning.")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			list := mechcheck.SectionCoverage{Segment: 1, Heading: "H", Sentences: []mechcheck.UncoveredSentence{
				{Paragraph: 1, Text: tt.line1},
				{Paragraph: 1, Text: tt.line2},
			}}
			seg := lossproposer.Union(list, []lossproposer.Reply{lossproposer.Parse(tt.r1), lossproposer.Parse(tt.r2)})
			if len(seg.Losses) != 2 {
				t.Errorf("got %d losses, want 2, one per run: %+v", len(seg.Losses), seg.Losses)
			}
		})
	}
}
