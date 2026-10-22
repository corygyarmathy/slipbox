package mechcheck

import (
	"strings"

	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/textrule"
)

// Locator finds a quote in the frame as the trace match does, so that s2n can
// write a statement's trace itself when the model wrote the statement
// quote-once (ADR 0006). It searches the sections the check searches, by the
// same match form, and gives back the source's own text.
type Locator struct {
	// sections are the segments' sections in segment order, then the
	// `source:` line's.
	sections []locatorSection
}

type locatorSection struct {
	// heading is the section as a trace names it: the segment's heading as
	// the file gives it, or "" for the `source:` line.
	heading string
	*section
	// paras are the section's paragraphs as written, and from maps each byte
	// of text to the paragraph it came from and its byte range there. A byte
	// of a paragraph join has paragraph -1.
	paras []string
	from  []textPlace
}

type textPlace struct{ para, start, end int }

// NewLocator gives a Locator for src.
func NewLocator(src *source.Source) *Locator {
	l := &Locator{}
	for _, seg := range src.Segments {
		l.add(seg.Heading, searchedParagraphs(seg))
	}
	l.add("", []string{src.CitekeyLink})
	return l
}

func (l *Locator) add(heading string, paras []string) {
	s := locatorSection{heading: heading, paras: paras}
	var text strings.Builder
	for i, p := range paras {
		if i > 0 {
			text.WriteString(paragraphJoin)
			for range len(paragraphJoin) {
				s.from = append(s.from, textPlace{para: -1})
			}
		}
		form, at := note.MatchFormOffsets(p)
		text.WriteString(form)
		for _, a := range at {
			s.from = append(s.from, textPlace{i, a[0], a[1]})
		}
	}
	s.section = newSection(text.String())
	l.sections = append(l.sections, s)
}

// Place is where in the frame a located quote ends, so that the next quote of
// a statement can be looked for after it. The zero Place is nowhere.
type Place struct {
	// section is an index into the Locator's sections, from 1, and end is a
	// byte offset in its text.
	section, end int
}

// Located is where Locate found a quote.
type Located struct {
	// Section is the section a trace to it names: a segment's heading as the
	// file gives it, or "" for the `source:` line.
	Section string
	// Quote is the source's own text where it was found exactly, with its
	// emphasis markers, escapes and curly quotes. Otherwise it is the quote as
	// given, stripped.
	Quote string
	// Exact and Fuzzy say how the trace match will grade it. Neither is set
	// for a quote that will be broken.
	Exact, Fuzzy bool
	// End is where it was found exactly.
	End Place
}

// Locate finds quote, written by a note of segment seg, as the trace match
// will look for it: first in seg's own section, then in the other segments'
// in order, then in the `source:` line, where an author name span is quoted
// from. In each section the first place after after is preferred, then the
// first place at all.
//
// If the quote isn't there exactly, it is looked for again with more and
// more of the characters that aren't letters or digits trimmed from its ends
// (trims), and those are then joining text. If it still isn't there, it is
// the quote as given, in the first section with a fuzzy match, or else in
// seg's own, where the trace match will find it broken.
func (l *Locator) Locate(seg int, quote string, after Place) Located {
	form := note.MatchForm(quote)
	// The segments' sections come first, and the `source:` line's last.
	own := seg >= 0 && seg < len(l.sections)-1
	var order []int
	if own {
		order = append(order, seg)
	}
	for i := range l.sections {
		if !own || i != seg {
			order = append(order, i)
		}
	}
	given := strings.TrimSpace(quote)
	if form == "" {
		return Located{Section: l.sections[order[0]].heading, Quote: given}
	}
	for _, q := range trims(form) {
		for _, i := range order {
			if loc, ok := l.exact(i, q, after); ok {
				return loc
			}
		}
	}
	for _, i := range order {
		if g, _, _ := find(form, l.sections[i].section); g == fuzzy {
			return Located{Section: l.sections[i].heading, Quote: given, Fuzzy: true}
		}
	}
	return Located{Section: l.sections[order[0]].heading, Quote: given}
}

// Exact says whether t's quote is in the section t names exactly, as step
// 1.2's trace match grades it: the last segment whose heading in match form
// is t's section, as step 1.2's map of sections keeps the last, or the
// `source:` line for a trace with no section. Unlike Locate, it searches no
// other section and trims nothing, and an empty quote isn't exact. Step
// 2.1's gates take it as the test of a name span (ADR 0010).
func (l *Locator) Exact(t note.Trace) bool {
	quote := note.MatchForm(t.Quote)
	if quote == "" {
		return false
	}
	named := len(l.sections) - 1
	if t.Section != "" {
		want := note.MatchForm(t.Section)
		segments := l.sections[:named]
		named = -1
		for i, s := range segments {
			if note.MatchForm(s.heading) == want {
				named = i
			}
		}
		if named < 0 {
			return false
		}
	}
	g, _, _ := find(quote, l.sections[named].section)
	return g == exact
}

// trims gives quote, then quote with the characters that aren't letters or
// digits at its ends trimmed, in every amount: the fewest trimmed first, and
// of those the fewest from its start.
func trims(quote string) []string {
	notAlnum := func(r rune) bool { return !textrule.IsAlnum(r) }
	rest := strings.TrimLeftFunc(quote, notAlnum)
	core := strings.TrimRightFunc(rest, notAlnum)
	if core == "" {
		return []string{quote}
	}
	lead := []rune(quote[:len(quote)-len(rest)])
	trail := []rune(rest[len(core):])
	var out []string
	for n := 0; n <= len(lead)+len(trail); n++ {
		for l := 0; l <= min(n, len(lead)); l++ {
			if t := n - l; t <= len(trail) {
				out = append(out, string(lead[l:])+core+string(trail[:len(trail)-t]))
			}
		}
	}
	return out
}

// exact finds form, a quote in match form, in section i exactly, as find()
// does, after after if it is in this section.
func (l *Locator) exact(i int, form string, after Place) (Located, bool) {
	s := l.sections[i]
	at := -1
	if after.section == i+1 {
		if k := indexFirstFolded(s.text[after.end:], form); k >= 0 {
			at = after.end + k
		}
	}
	if at < 0 {
		at = indexFirstFolded(s.text, form)
	}
	if at < 0 {
		return Located{}, false
	}
	end := at + len(form)
	first, last := s.from[at], s.from[end-1]
	if first.para < 0 || first.para != last.para {
		return Located{}, false
	}
	p := s.paras[first.para]
	start, stop := first.start, last.end
	// An emphasis run the quote opens or closes is kept whole.
	for _, m := range []byte{'*', '_'} {
		if strings.Count(p[start:stop], string(m))%2 == 1 {
			switch {
			case stop < len(p) && p[stop] == m:
				stop++
			case start > 0 && p[start-1] == m:
				start--
			}
		}
	}
	quote := p[start:stop]
	if note.MatchForm(quote) != s.text[at:end] {
		// Only a quote whose match form is what was found is the source's text.
		quote = s.text[at:end]
	}
	return Located{Section: s.heading, Quote: quote, Exact: true, End: Place{i + 1, end}}, true
}

// SentenceRanges gives the byte ranges of the sentences of p, a paragraph
// in match form, as the coverage check splits it.
func SentenceRanges(p string) [][2]int {
	runes := []rune(p)
	var out [][2]int
	for _, s := range splitSentences(runes) {
		start := len(string(runes[:s[0]]))
		out = append(out, [2]int{start, start + len(string(runes[s[0]:s[1]]))})
	}
	return out
}
