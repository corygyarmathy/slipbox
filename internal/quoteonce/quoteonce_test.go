package quoteonce_test

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/quoteonce"
	"github.com/corygyarmathy/slipbox/internal/source"
)

// chapter is an invented source chapter, with what quote-once must find a
// quote's place among: emphasis, curly quotes, list items, an author who
// writes in the first person, and words repeated in another section.
const chapter = `---
source: "[[@A Short History of Bridges - Jane Doe]]"
---

# 3. The Stone Arch

An arch stands by its *shape*. It needs no glue, and no “mortar” either.

Two parts are common to all arches.

1. *Keystone*. The keystone goes in last.
2. *Voussoirs*. Each wedge leans on the next.

## Arches

Builders learned this slowly, and at some cost. Telford himself had built such arches before he built roads.

Of the parts which I have named, two matter most:

1. The keystone, which locks the ring.
2. The abutment, which takes the thrust.

In my view, the keystone and the abutment matter most. Why does an arch stand at all?

## Load

A load becomes thrust. Thrust runs down to the abutments, which must not move.
`

func stoneArch(t *testing.T) *source.Source {
	t.Helper()
	src, err := source.Parse("Bridges - Ch 3", chapter)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// tracedNotes are stage 1 notes as a generator writes them with footnotes:
// whole sentences, quotes cut mid-sentence, a name traced on its own, an
// author name span splitting a quote, `_x_` emphasis where the source has
// `*x*`, list items, and a trace into another segment's section.
var tracedNotes = map[string]string{
	"0 The Stone Arch.md": "# 0 The Stone Arch\n\n" +
		"An arch stands by its _shape_.[^1] It needs no glue,[^2] and no \"mortar\".[^3]\n" +
		"Two parts are common to all arches:[^4]\n\n" +
		"1. _Keystone_.[^5] The keystone goes in last.[^6]\n" +
		"2. _Voussoirs_.[^7] Each wedge leans on the next.[^8]\n\n" +
		"[^1]: [[Bridges - Ch 3#3. The Stone Arch]] \"An arch stands by its *shape*.\"\n" +
		"[^2]: [[Bridges - Ch 3#3. The Stone Arch]] \"It needs no glue\"\n" +
		"[^3]: [[Bridges - Ch 3#3. The Stone Arch]] \"“mortar”\"\n" +
		"[^4]: [[Bridges - Ch 3#3. The Stone Arch]] \"Two parts are common to all arches\"\n" +
		"[^5]: [[Bridges - Ch 3#3. The Stone Arch]] \"*Keystone*.\"\n" +
		"[^6]: [[Bridges - Ch 3#3. The Stone Arch]] \"The keystone goes in last.\"\n" +
		"[^7]: [[Bridges - Ch 3#3. The Stone Arch]] \"*Voussoirs*.\"\n" +
		"[^8]: [[Bridges - Ch 3#3. The Stone Arch]] \"Each wedge leans on the next.\"\n",
	"1 Arches.md": "# 1 Arches\n\n" +
		"Builders learned this[^1] at some cost.[^2] Telford[^3] had built such arches before he built roads.[^4]\n" +
		"Of the parts which I,[^5] Jane Doe,[^6] have named, two matter most:[^7] the keystone and the abutment.[^8]\n\n" +
		"[^1]: [[Bridges - Ch 3#Arches]] \"Builders learned this\"\n" +
		"[^2]: [[Bridges - Ch 3#Arches]] \"at some cost.\"\n" +
		"[^3]: [[Bridges - Ch 3#Arches]] \"Telford\"\n" +
		"[^4]: [[Bridges - Ch 3#Arches]] \"had built such arches before he built roads.\"\n" +
		"[^5]: [[Bridges - Ch 3#Arches]] \"Of the parts which I\"\n" +
		"[^6]: [[Bridges - Ch 3]] \"Jane Doe\"\n" +
		"[^7]: [[Bridges - Ch 3#Arches]] \"have named, two matter most:\"\n" +
		"[^8]: [[Bridges - Ch 3#Arches]] \"the keystone and the abutment\"\n",
	"2 Load.md": "# 2 Load\n\n" +
		"A load becomes thrust.[^1] Thrust runs down to the abutments.[^2]\n" +
		"Builders learned this slowly.[^3]\n\n" +
		"[^1]: [[Bridges - Ch 3#Load]] \"A load becomes thrust.\"\n" +
		"[^2]: [[Bridges - Ch 3#Load]] \"Thrust runs down to the abutments\"\n" +
		"[^3]: [[Bridges - Ch 3#Arches]] \"Builders learned this slowly\"\n",
}

// Traced notes, with their joining text marked and their footnotes taken
// out, trace back to their own traces: each statement reads as it did, with
// the same sections and quotes in the same order, and step 1.2 finds them as
// it found the traced ones. The one difference is a quote an author name
// span splits, which is one trace (joinNameSpans).
func TestNote_TracedNotesTraceBack(t *testing.T) {
	src := stoneArch(t)
	tr := quoteonce.New(src)
	var traced, derived []*note.Note
	joins := 0
	for name, text := range tracedNotes {
		n := note.Parse(name, text)
		traced = append(traced, n)
		marked, ok := mark(t, n)
		if !ok {
			t.Errorf("%s: a quote isn't in its statement", n.Name)
			continue
		}
		got := tr.Note(segmentOf(t, n.Name), note.Parse(n.Name, marked))
		derived = append(derived, got)
		want := statementTraces(t, n)
		joins += joinNameSpans(want)
		if got := statementTraces(t, got); !reflect.DeepEqual(got, want) {
			for i := range min(len(got), len(want)) {
				if !reflect.DeepEqual(got[i], want[i]) {
					t.Errorf("%s:\n got  %+v\n want %+v", n.Name, got[i], want[i])
					break
				}
			}
			if len(got) != len(want) {
				t.Errorf("%s: %d statement lines, want %d", n.Name, len(got), len(want))
			}
		}
	}
	if joins != 1 {
		t.Errorf("joined %d quotes split by a name span, want 1", joins)
	}
	want, got := mechcheck.Check(src, traced), mechcheck.Check(src, derived)
	want.Stats.Exact -= joins
	if got.Stats != want.Stats {
		t.Errorf("step 1.2's counts are %+v, want the traced notes' %+v", got.Stats, want.Stats)
	}
	if got.Coverage.Covered != want.Coverage.Covered || got.Coverage.Total != want.Coverage.Total {
		t.Errorf("coverage %d/%d, want %d/%d", got.Coverage.Covered, got.Coverage.Total, want.Coverage.Covered, want.Coverage.Total)
	}
}

// joinNameSpans joins, in place, each traced quote that an author name
// span splits, "I,[^1] <name>,[^2] …[^3]", into the one trace the tracer
// writes for it, and gives how many it joined.
func joinNameSpans(lines []lineTraces) int {
	joins := 0
	for i, l := range lines {
		tr := l.Traces
		for k := 0; k+2 < len(tr); k++ {
			if tr[k+1].Section == "" && tr[k].Section != "" && tr[k].Section == tr[k+2].Section {
				whole := tr[k]
				whole.Quote += " " + tr[k+2].Quote
				tr = append(tr[:k:k], append([]note.Trace{tr[k+1], whole}, tr[k+3:]...)...)
				joins++
			}
		}
		lines[i].Traces = tr
	}
	return joins
}

// lineTraces is one statement line: its text without footnote references,
// and its traces in reading order.
type lineTraces struct {
	Text   string
	Traces []note.Trace
}

func statementTraces(t *testing.T, n *note.Note) []lineTraces {
	t.Helper()
	defs := map[string]note.Trace{}
	for _, d := range n.Defs() {
		tr, ok := d.Trace()
		if !ok {
			t.Fatalf("%s: [^%s] is not a trace", n.Name, d.Label)
		}
		defs[d.Label] = tr
	}
	var out []lineTraces
	for _, l := range n.Lines {
		if l.Kind != note.Body || strings.HasPrefix(l.Text, "#") || strings.TrimSpace(l.Text) == "" {
			continue
		}
		lt := lineTraces{Text: note.StripRefs(l.Text)}
		for _, r := range l.Refs {
			lt.Traces = append(lt.Traces, defs[r])
		}
		out = append(out, lt)
	}
	return out
}

func segmentOf(t *testing.T, name string) int {
	t.Helper()
	num, _, _ := strings.Cut(name, " ")
	k, err := strconv.Atoi(num)
	if err != nil {
		t.Fatalf("%s: no segment number", name)
	}
	return k
}

var (
	refPattern  = regexp.MustCompile(`\[\^(\d+)\]`)
	listPattern = regexp.MustCompile(`^\s*(?:[-*]|\d+\.)\s+`)
)

// mark writes n quote-once, as a generator following the quote-once brief
// would have: its footnotes gone, and in each statement line the text outside
// the traced quotes in braces, with `{}` where two quotes meet in one
// sentence. It is false if a quote isn't in the statement it traces.
func mark(t *testing.T, n *note.Note) (string, bool) {
	t.Helper()
	defs := map[string]string{}
	for _, d := range n.Defs() {
		if tr, ok := d.Trace(); ok {
			defs[d.Label] = tr.Quote
		}
	}
	var b strings.Builder
	for _, l := range n.Lines {
		if l.Kind == note.Definition {
			continue
		}
		if l.Kind != note.Body || len(l.Refs) == 0 {
			b.WriteString(l.Text + "\n")
			continue
		}
		text := l.Text
		if m := listPattern.FindString(text); m != "" {
			b.WriteString(m)
			text = text[len(m):]
		}
		from := 0
		lastQuote := ""
		for _, m := range refPattern.FindAllStringSubmatchIndex(text, -1) {
			chunk := text[from:m[0]]
			from = m[1]
			a, z, ok := find(chunk, defs[text[m[2]:m[3]]])
			if !ok {
				return "", false
			}
			pre, q, post := chunk[:a], chunk[a:z], chunk[z:]
			switch {
			case strings.TrimSpace(pre) != "":
				lead := pre[:len(pre)-len(strings.TrimLeft(pre, " "))]
				b.WriteString(lead + "{" + pre[len(lead):] + "}")
			case lastQuote != "" && oneSentence(lastQuote, q):
				b.WriteString("{}" + pre)
			default:
				b.WriteString(pre)
			}
			b.WriteString(q)
			lastQuote = q
			if post != "" {
				b.WriteString("{" + post + "}")
				lastQuote = ""
			}
		}
		if rest := text[from:]; strings.TrimSpace(rest) != "" {
			b.WriteString("{" + rest + "}")
		} else {
			b.WriteString(rest)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n", true
}

// find gives the byte range in chunk of quote, matched as step 1.2 matches
// it, with the emphasis markers at its ends.
func find(chunk, quote string) (int, int, bool) {
	norm, at := note.MatchFormOffsets(chunk)
	q := note.MatchForm(quote)
	i := strings.Index(norm, q)
	if i < 0 && q != "" {
		first, size := utf8.DecodeRuneInString(q)
		for r := unicode.SimpleFold(first); r != first && i < 0; r = unicode.SimpleFold(r) {
			i = strings.Index(norm, string(r)+q[size:])
		}
	}
	if i < 0 || q == "" {
		return 0, 0, false
	}
	a, z := at[i][0], at[i+len(q)-1][1]
	for a > 0 && strings.ContainsRune("_*", rune(chunk[a-1])) {
		a--
	}
	for z < len(chunk) && strings.ContainsRune("_*", rune(chunk[z])) {
		z++
	}
	return a, z, true
}

// oneSentence says whether two quotes, one after the other with a space
// between, read as one sentence to the coverage check's splitter, so that
// quote-once needs `{}` between them.
func oneSentence(a, b string) bool {
	n := func(s string) int { return len(mechcheck.SentenceRanges(note.MatchForm(s))) }
	return n(a+" "+b) < n(a)+n(b)
}

// Notes marked by hand as the quote-once brief asks, with braces only where
// a quote needs them, trace to what a generator writing footnotes traces
// them to: each statement reads as it does, and is traced to the same
// sections, each quote is the same but perhaps for punctuation at its ends,
// and step 1.2 finds every quote exactly.
func TestNote_HandMarkedNotes(t *testing.T) {
	tests := []struct {
		name, marked, traced string
	}{{
		name: "0 The Stone Arch.md",
		// The whole note, which needs no braces: `_x_` emphasis where the
		// source has `*x*`, curly quotes, a `:` the source doesn't have
		// before a list, and list items.
		marked: "# 0 The Stone Arch\n\n" +
			"An arch stands by its _shape_. It needs no glue, and no “mortar” either.\n\n" +
			"Two parts are common to all arches:\n\n" +
			"1. _Keystone_. The keystone goes in last.\n" +
			"2. _Voussoirs_. Each wedge leans on the next.\n",
		traced: "# 0 The Stone Arch\n\n" +
			"An arch stands by its _shape_.[^1] It needs no glue, and no “mortar” either.[^2]\n\n" +
			"Two parts are common to all arches:[^3]\n\n" +
			"1. _Keystone_.[^4] The keystone goes in last.[^5]\n" +
			"2. _Voussoirs_.[^6] Each wedge leans on the next.[^7]\n\n" +
			"[^1]: [[Bridges - Ch 3#3. The Stone Arch]] \"An arch stands by its *shape*.\"\n" +
			"[^2]: [[Bridges - Ch 3#3. The Stone Arch]] \"It needs no glue, and no “mortar” either.\"\n" +
			"[^3]: [[Bridges - Ch 3#3. The Stone Arch]] \"Two parts are common to all arches.\"\n" +
			"[^4]: [[Bridges - Ch 3#3. The Stone Arch]] \"*Keystone*.\"\n" +
			"[^5]: [[Bridges - Ch 3#3. The Stone Arch]] \"The keystone goes in last.\"\n" +
			"[^6]: [[Bridges - Ch 3#3. The Stone Arch]] \"*Voussoirs*.\"\n" +
			"[^7]: [[Bridges - Ch 3#3. The Stone Arch]] \"Each wedge leans on the next.\"\n",
	}, {
		name: "1 Arches - Aside - Cost.md",
		// A mid-sentence cut, marked `{}`.
		marked: "# 1 Arches - Aside - Cost\n\n" +
			"Builders learned this{} at some cost.\n",
		traced: "# 1 Arches - Aside - Cost\n\n" +
			"Builders learned this[^1] at some cost.[^2]\n\n" +
			"[^1]: [[Bridges - Ch 3#Arches]] \"Builders learned this\"\n" +
			"[^2]: [[Bridges - Ch 3#Arches]] \"at some cost.\"\n",
	}, {
		name: "1 Arches.md",
		// A name traced on its own, marked `{}`, the author name span, and a
		// first-person cut that changes a quote's first letter's case.
		marked: "# 1 Arches\n\n" +
			"Telford{} had built such arches before he built roads.\n\n" +
			"Of the parts which I{, }Jane Doe{, }have named, two matter most:\n\n" +
			"The keystone and the abutment matter most.\n",
		traced: "# 1 Arches\n\n" +
			"Telford[^1] had built such arches before he built roads.[^2]\n\n" +
			"Of the parts which I,[^3] Jane Doe,[^4] have named, two matter most:[^5]\n\n" +
			"The keystone and the abutment matter most.[^6]\n\n" +
			"[^1]: [[Bridges - Ch 3#Arches]] \"Telford\"\n" +
			"[^2]: [[Bridges - Ch 3#Arches]] \"had built such arches before he built roads.\"\n" +
			"[^3]: [[Bridges - Ch 3#Arches]] \"Of the parts which I\"\n" +
			"[^4]: [[Bridges - Ch 3]] \"Jane Doe\"\n" +
			"[^5]: [[Bridges - Ch 3#Arches]] \"have named, two matter most:\"\n" +
			"[^6]: [[Bridges - Ch 3#Arches]] \"the keystone and the abutment matter most.\"\n",
	}}
	src := stoneArch(t)
	tr := quoteonce.New(src)
	var derived []*note.Note
	for _, tt := range tests {
		got := tr.Note(segmentOf(t, tt.name), note.Parse(tt.name, tt.marked))
		derived = append(derived, got)
		want, have := statementTraces(t, note.Parse(tt.name, tt.traced)), statementTraces(t, got)
		joinNameSpans(want)
		if len(have) != len(want) {
			t.Fatalf("%s: %d statement lines, want %d", tt.name, len(have), len(want))
		}
		for i, l := range have {
			w := want[i]
			if l.Text != w.Text {
				t.Errorf("%s: line %d reads\n %q\nwant\n %q", tt.name, i, l.Text, w.Text)
			}
			if len(l.Traces) != len(w.Traces) {
				t.Errorf("%s: line %d has %d traces, want %d", tt.name, i, len(l.Traces), len(w.Traces))
				continue
			}
			for k := range l.Traces {
				g, w := l.Traces[k], w.Traces[k]
				if g.File != w.File || g.Section != w.Section || edgeless(g.Quote) != edgeless(w.Quote) {
					t.Errorf("%s: line %d trace %d is\n %+v\nwant\n %+v", tt.name, i, k+1, g, w)
				}
			}
		}
	}
	r := mechcheck.Check(src, derived)
	if r.Stats.Fuzzy != 0 || r.Stats.Broken != 0 || r.Stats.UntracedStatements != 0 {
		t.Errorf("step 1.2's counts are %+v, want every quote exact", r.Stats)
	}
}

// edgeless is a quote normalised, without what isn't a letter or digit at its
// ends.
func edgeless(q string) string {
	return strings.TrimFunc(note.MatchForm(q), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}

// The source for the unit tests: one segment before the first heading, two
// sections, and a `source:` line naming the author.
const unitSource = `---
source: "[[@A Book - Ann Author]]"
---

# 1. The Chapter

As I said before, the *first* point is plain. The second point is “quoted” here.

## Next

I think the first point is plain, and it matters. The repeated words come here. The repeated words come here too.
`

func unitTracer(t *testing.T) (*quoteonce.Tracer, *source.Source) {
	t.Helper()
	src, err := source.Parse("Src", unitSource)
	if err != nil {
		t.Fatal(err)
	}
	return quoteonce.New(src), src
}

func TestNote(t *testing.T) {
	tr, _ := unitTracer(t)
	tests := []struct {
		name, seg0, want string
	}{{
		name: "a sentence a cut starts, with the source's emphasis and curly quotes, and two contiguous sentences",
		seg0: "# 0 The Chapter\n\nThe _first_ point is plain. The second point is \"quoted\" here.\n",
		want: "# 0 The Chapter\n\nThe _first_ point is plain.[^1] The second point is \"quoted\" here.[^2]\n\n" +
			"[^1]: [[Src#1. The Chapter]] \"the *first* point is plain.\"\n" +
			"[^2]: [[Src#1. The Chapter]] \"The second point is “quoted” here.\"\n",
	}, {
		name: "the author name span, from the source: line, set into a quote it leaves whole, and joining text",
		seg0: "# 0 The Chapter\n\nI{, }Ann Author{, }said before{:} the first point is plain.\n",
		want: "# 0 The Chapter\n\nI, Ann Author,[^1] said before:[^2] the first point is plain.[^3]\n\n" +
			"[^1]: [[Src]] \"Ann Author\"\n" +
			"[^2]: [[Src#1. The Chapter]] \"I said before\"\n" +
			"[^3]: [[Src#1. The Chapter]] \"the *first* point is plain.\"\n",
	}, {
		name: "the author name span between quotes that aren't contiguous in the source",
		seg0: "# 0 The Chapter\n\nI{, }Ann Author{, }matters.\n",
		want: "# 0 The Chapter\n\nI,[^1] Ann Author,[^2] matters.[^3]\n\n" +
			"[^1]: [[Src#1. The Chapter]] \"I\"\n" +
			"[^2]: [[Src]] \"Ann Author\"\n" +
			"[^3]: [[Src#Next]] \"matters.\"\n",
	}, {
		name: "a name span that isn't the author's, found in a section, isn't joined into the quotes either side",
		seg0: "# 0 The Chapter\n\nI{, }plain{, }think the first point is plain.\n",
		want: "# 0 The Chapter\n\nI,[^1] plain,[^2] think the first point is plain.[^3]\n\n" +
			"[^1]: [[Src#1. The Chapter]] \"I\"\n" +
			"[^2]: [[Src#1. The Chapter]] \"plain\"\n" +
			"[^3]: [[Src#Next]] \"think the first point is plain\"\n",
	}, {
		name: "joining text that runs on from a quote into a word",
		seg0: "# 0 The Chapter\n\nAnn Author{'s} first point is plain.\n",
		want: "# 0 The Chapter\n\nAnn Author[^1]'s first point is plain.[^2]\n\n" +
			"[^1]: [[Src]] \"Ann Author\"\n" +
			"[^2]: [[Src#1. The Chapter]] \"*first* point is plain.\"\n",
	}, {
		name: "{} between two quotes of one sentence, and a name span from another section",
		seg0: "# 0 The Chapter\n\nThe second point{} matters.\n",
		want: "# 0 The Chapter\n\nThe second point[^1] matters.[^2]\n\n" +
			"[^1]: [[Src#1. The Chapter]] \"The second point\"\n" +
			"[^2]: [[Src#Next]] \"matters.\"\n",
	}, {
		name: "punctuation at a quote's end that the source doesn't have, and a list item",
		seg0: "# 0 The Chapter\n\nThe first point is plain:\n\n1. The second point is “quoted”!\n",
		want: "# 0 The Chapter\n\nThe first point is plain:[^1]\n\n1. The second point is “quoted”![^2]\n\n" +
			"[^1]: [[Src#1. The Chapter]] \"the *first* point is plain\"\n" +
			"[^2]: [[Src#1. The Chapter]] \"The second point is “quoted”\"\n",
	}, {
		name: "a quote not in the frame is written as given, in the segment's own section",
		seg0: "# 0 The Chapter\n\nThe third point is plain.\n",
		want: "# 0 The Chapter\n\nThe third point is plain.[^1]\n\n" +
			"[^1]: [[Src#1. The Chapter]] \"The third point is plain.\"\n",
	}, {
		name: "a line whose braces don't pair up is left as it is",
		seg0: "# 0 The Chapter\n\nThe second point is {quoted here.\n\nThe first point is plain.\n",
		want: "# 0 The Chapter\n\nThe second point is {quoted here.\n\nThe first point is plain.[^1]\n\n" +
			"[^1]: [[Src#1. The Chapter]] \"the *first* point is plain.\"\n",
	}, {
		name: "a note with footnotes and no braces is left as it is",
		seg0: "# 0 The Chapter\n\nThe first point.[^1]\n\nThe second point.\n\n[^1]: [[Src#Next]] \"point\"\n",
		want: "# 0 The Chapter\n\nThe first point.[^1]\n\nThe second point.\n\n[^1]: [[Src#Next]] \"point\"\n",
	}, {
		name: "in a note with footnotes and braces, the lines without a footnote are traced",
		seg0: "# 0 The Chapter\n\nThe {first} point.[^1]\n\nThe second point{} matters.\n\n[^1]: [[Src#Next]] \"point\"\n",
		want: "# 0 The Chapter\n\nThe {first} point.[^1]\n\nThe second point[^2] matters.[^3]\n\n" +
			"[^1]: [[Src#Next]] \"point\"\n" +
			"[^2]: [[Src#1. The Chapter]] \"The second point\"\n" +
			"[^3]: [[Src#Next]] \"matters.\"\n",
	}}
	for _, tt := range tests {
		got := tr.Note(0, note.Parse("0 The Chapter.md", tt.seg0)).Render()
		if got != tt.want {
			t.Errorf("%s:\n got\n%s\nwant\n%s", tt.name, got, tt.want)
		}
	}
}

// A quoted run is split into statements where stage 1 would put a footnote
// reference, and not after an abbreviation's period, nor at a mark inside a
// quotation. A statement may start behind an emphasis marker.
func TestNote_StatementEnds(t *testing.T) {
	const text = "Some stones, e.g. Granite, last. He asked ‘Why? Because it stands’ and left. It stands. *Then* it falls."
	src, err := source.Parse("Src", "---\nsource: \"[[@A Book - Ann Author]]\"\n---\n\n# 1. The Chapter\n\n"+text+"\n")
	if err != nil {
		t.Fatal(err)
	}
	got := quoteonce.New(src).Note(0, note.Parse("0 The Chapter.md", "# 0 The Chapter\n\n"+text+"\n")).Render()
	want := "# 0 The Chapter\n\n" +
		"Some stones, e.g. Granite, last.[^1] He asked ‘Why? Because it stands’ and left.[^2] It stands.[^3] *Then* it falls.[^4]\n\n" +
		"[^1]: [[Src#1. The Chapter]] \"Some stones, e.g. Granite, last.\"\n" +
		"[^2]: [[Src#1. The Chapter]] \"He asked ‘Why? Because it stands’ and left.\"\n" +
		"[^3]: [[Src#1. The Chapter]] \"It stands.\"\n" +
		"[^4]: [[Src#1. The Chapter]] \"*Then* it falls.\"\n"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A repeated quote is traced to its place after the statement's quote before
// it, and a segment's own section comes before the others.
func TestNote_PrefersThePlaceAfterTheQuoteBefore(t *testing.T) {
	tr, src := unitTracer(t)
	n := tr.Note(1, note.Parse("1 Next.md", "# 1 Next\n\nThe first point is plain{.} The repeated words come here too.\n"))
	want := "[^1]: [[Src#Next]] \"the first point is plain\"\n[^2]: [[Src#Next]] \"The repeated words come here too.\"\n"
	if got := n.Render(); !strings.HasSuffix(got, want) {
		t.Errorf("got\n%s\nwant it to end\n%s", got, want)
	}
	if r := mechcheck.Check(src, []*note.Note{n}); r.Stats.Exact != 2 {
		t.Errorf("step 1.2's counts are %+v, want 2 exact", r.Stats)
	}
}

// A quote the source has only fuzzily is written as given, in the section
// step 1.2's fuzzy match finds it in.
func TestNote_FuzzyQuote(t *testing.T) {
	tr, src := unitTracer(t)
	n := tr.Note(0, note.Parse("0 The Chapter.md", "# 0 The Chapter\n\nThe repeated words came here too.\n"))
	want := "[^1]: [[Src#Next]] \"The repeated words came here too.\"\n"
	if got := n.Render(); !strings.HasSuffix(got, want) {
		t.Errorf("got\n%s\nwant it to end\n%s", got, want)
	}
	if r := mechcheck.Check(src, []*note.Note{n}); r.Stats.Fuzzy != 1 {
		t.Errorf("step 1.2's counts are %+v, want 1 fuzzy", r.Stats)
	}
}

func TestEdit(t *testing.T) {
	tr, _ := unitTracer(t)
	got := tr.Edit(0, note.Edit{Op: note.Insert, Statement: "x", Text: "The second point{} matters.\n\nThe first point is plain."})
	want := note.Edit{Op: note.Insert, Statement: "x", Text: "The second point[^1] matters.[^2]\n\nThe first point is plain.[^3]", Defs: []note.Def{
		{Label: "1", Value: `[[Src#1. The Chapter]] "The second point"`},
		{Label: "2", Value: `[[Src#Next]] "matters."`},
		{Label: "3", Value: `[[Src#1. The Chapter]] "the *first* point is plain."`},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got\n %+v\nwant\n %+v", got, want)
	}
	mixed := tr.Edit(0, note.Edit{Op: note.Insert, Statement: "x", Text: "The {first} point.[^1]\n\nThe second point{} matters.", Defs: []note.Def{{Label: "1", Value: `[[Src#Next]] "point"`}}})
	want = note.Edit{Op: note.Insert, Statement: "x", Text: "The {first} point.[^1]\n\nThe second point[^2] matters.[^3]", Defs: []note.Def{
		{Label: "1", Value: `[[Src#Next]] "point"`},
		{Label: "2", Value: `[[Src#1. The Chapter]] "The second point"`},
		{Label: "3", Value: `[[Src#Next]] "matters."`},
	}}
	if !reflect.DeepEqual(mixed, want) {
		t.Errorf("an edit with footnotes and braces: got\n %+v\nwant\n %+v", mixed, want)
	}
	list := tr.Edit(0, note.Edit{Op: note.Insert, Statement: "x", Text: "The first point is plain:\n\n1. The second point is “quoted”!\n2. The second point{} matters."})
	want = note.Edit{Op: note.Insert, Statement: "x", Text: "The first point is plain:[^1]\n\n1. The second point is “quoted”![^2]\n2. The second point[^3] matters.[^4]", Defs: []note.Def{
		{Label: "1", Value: `[[Src#1. The Chapter]] "the *first* point is plain"`},
		{Label: "2", Value: `[[Src#1. The Chapter]] "The second point is “quoted”"`},
		{Label: "3", Value: `[[Src#1. The Chapter]] "The second point"`},
		{Label: "4", Value: `[[Src#Next]] "matters."`},
	}}
	if !reflect.DeepEqual(list, want) {
		t.Errorf("an edit with a list: got\n %+v\nwant\n %+v", list, want)
	}
	for _, e := range []note.Edit{
		{Op: note.Replace, Statement: "x"},
		{Op: note.Insert, Statement: "x", Text: "The first point.[^1]", Defs: []note.Def{{Label: "1", Value: `[[Src#Next]] "point"`}}},
	} {
		if got := tr.Edit(0, e); !reflect.DeepEqual(got, e) {
			t.Errorf("Edit(%+v) = %+v, want it unchanged", e, got)
		}
	}
}

// A kept-loss edit written quote-once, a lead-in holding the author name
// span and a list, traces and applies after a statement mid-way through a
// paragraph: the paragraph breaks at the list, as the source's does, and
// step 1.2 finds every new quote.
func TestEdit_ListMidParagraph(t *testing.T) {
	src := stoneArch(t)
	n := note.Parse("1 Arches.md", "# 1 Arches\n\n"+
		"Builders learned this slowly, and at some cost.[^1] Why does an arch stand at all?[^2]\n\n"+
		"[^1]: [[Bridges - Ch 3#Arches]] \"Builders learned this slowly, and at some cost.\"\n"+
		"[^2]: [[Bridges - Ch 3#Arches]] \"Why does an arch stand at all?\"\n")
	edits, err := note.ParseEdits("--- AFTER ---\n" +
		"Builders learned this slowly, and at some cost.[^1]\n" +
		"--- INSERT ---\n" +
		"Of the parts which I{, }Jane Doe{, }have named, two matter most:\n\n" +
		"1. The keystone, which locks the ring.\n" +
		"2. The abutment, which takes the thrust.\n")
	if err != nil {
		t.Fatal(err)
	}
	got, err := n.Apply([]note.Edit{quoteonce.New(src).Edit(1, edits[0])})
	if err != nil {
		t.Fatal(err)
	}
	if want := "Builders learned this slowly, and at some cost.[^1] Of the parts which I, Jane Doe,[^2] have named, two matter most:[^3]\n\n" +
		"1. The keystone, which locks the ring.[^4]\n" +
		"2. The abutment, which takes the thrust.[^5]\n\n" +
		"Why does an arch stand at all?[^6]\n\n"; !strings.Contains(got.Render(), want) {
		t.Errorf("Apply gave no %q in:\n%s", want, got.Render())
	}
	before, after := mechcheck.Check(src, []*note.Note{n}).Stats, mechcheck.Check(src, []*note.Note{got}).Stats
	if after.Statements != before.Statements+3 || after.Fuzzy != before.Fuzzy || after.Broken != before.Broken || after.UntracedStatements != before.UntracedStatements {
		t.Errorf("step 1.2's counts are %+v after the edit, want %+v with 3 more statements, every new quote exact", after, before)
	}
}
