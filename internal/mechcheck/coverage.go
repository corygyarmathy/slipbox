package mechcheck

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/corygyarmathy/slipbox/internal/textrule"
)

// Coverage is the coverage check: over the whole frame, every source
// sentence that verified traces don't fully span.
// A sentence of three letters or digits or fewer, such as a list numeral, is
// neither listed nor counted. Endnote definitions aren't in the sections'
// text, so their sentences aren't either.
//
// A segment's uncovered list is named for its segment's number, so a fully
// covered segment, which has no list and so no loss proposer, leaves a gap in
// the numbers rather than shifting the lists after it. A frame with no
// sentences to count has no percentage in its totals.
type Coverage struct {
	// Sections are the segments with a sentence not fully covered, in segment
	// order.
	Sections []SectionCoverage
	// Covered and Total count the frame's sentences.
	Covered, Total int
	// Stats are the mechanical check's counts, which the report prints with
	// the coverage totals.
	Stats Stats
}

// SectionCoverage is one segment's sentences that verified traces don't fully
// span.
type SectionCoverage struct {
	Segment int
	// Heading is the segment's heading, in match form.
	Heading   string
	Sentences []UncoveredSentence
}

// UncoveredSentence is a source sentence that verified traces don't fully
// span.
type UncoveredSentence struct {
	// Paragraph is the sentence's paragraph in its section, from 1, counting
	// the paragraphs the trace match searches.
	Paragraph int
	// Partial is set when traces span some of the sentence.
	Partial bool
	// Text is the sentence, with its uncovered letters and digits in
	// capitals when Partial is set.
	Text string
}

// String renders the sentence as its uncovered list holds it.
func (u UncoveredSentence) String() string {
	if u.Partial {
		return fmt.Sprintf("  ¶%d PARTIAL (uncovered in CAPS): %s", u.Paragraph, u.Text)
	}
	return fmt.Sprintf("  ¶%d UNCOVERED: %s", u.Paragraph, u.Text)
}

// File is the name of the segment's uncovered list in the run folder.
func (s SectionCoverage) File() string { return strconv.Itoa(s.Segment) + ".txt" }

// List renders the segment's uncovered list for its loss proposer: the
// segment's heading, then a line per sentence.
func (s SectionCoverage) List() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", s.Heading)
	for _, u := range s.Sentences {
		b.WriteString(u.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// ParseList reads segment's uncovered list as List renders it: a `# `
// heading line, then one line per sentence. A line in neither shape fails.
func ParseList(segment int, list string) (SectionCoverage, error) {
	s := SectionCoverage{Segment: segment}
	lines := strings.Split(strings.TrimSuffix(list, "\n"), "\n")
	heading, ok := strings.CutPrefix(lines[0], "# ")
	if !ok {
		return s, fmt.Errorf("uncovered list %d: first line %q is not a heading", segment, lines[0])
	}
	s.Heading = heading
	for _, line := range lines[1:] {
		m := listLine.FindStringSubmatch(line)
		if m == nil {
			return s, fmt.Errorf("uncovered list %d: line %q is not an uncovered sentence", segment, line)
		}
		para, _ := strconv.Atoi(m[1])
		s.Sentences = append(s.Sentences, UncoveredSentence{Paragraph: para, Partial: m[2] != "UNCOVERED", Text: m[3]})
	}
	return s, nil
}

// listLine is an uncovered list's sentence line, as UncoveredSentence.String
// renders it.
var listLine = regexp.MustCompile(`^  ¶(\d+) (UNCOVERED|PARTIAL \(uncovered in CAPS\)): (.*)$`)

// String renders the coverage check, its uncovered sentences then its
// totals: the on-demand audit file.
func (c *Coverage) String() string {
	var b strings.Builder
	b.WriteString("\n## Coverage: source sentences not fully spanned by verified traces\n")
	for _, s := range c.Sections {
		b.WriteByte('\n')
		b.WriteString(s.List())
	}
	st := c.Stats
	fmt.Fprintf(&b, "\n## Totals\n  exact %d, fuzzy %d, broken %d, untraced statements %d, statements %d\n",
		st.Exact, st.Fuzzy, st.Broken, st.UntracedStatements, st.Statements)
	fmt.Fprintf(&b, "  covered sentences: %d/%d", c.Covered, c.Total)
	if c.Total > 0 {
		fmt.Fprintf(&b, " (%.0f%%)", float64(100*c.Covered)/float64(c.Total))
	}
	b.WriteByte('\n')
	return b.String()
}

// coverage runs the coverage check on the verified traces' spans in r.
func coverage(segs []segmentSection, r *Report) *Coverage {
	c := &Coverage{Stats: r.Stats}
	for _, seg := range segs {
		text := seg.runes
		covered := make([]bool, len(text))
		for _, sp := range seg.spans {
			for i := sp.Start; i < min(sp.End, len(text)); i++ {
				covered[i] = true
			}
		}
		sc := SectionCoverage{Segment: seg.number, Heading: seg.heading}
		off := 0
		for pi, p := range seg.paras {
			rp := []rune(p)
			for _, s := range splitSentences(rp) {
				base := off + s[0]
				sentence := text[base : off+s[1]]
				need, missed := 0, 0
				for i, ch := range sentence {
					if textrule.IsAlnum(ch) {
						need++
						if !covered[base+i] {
							missed++
						}
					}
				}
				if need <= 3 { // list numerals like "i." / "5."
					continue
				}
				c.Total++
				if missed == 0 {
					c.Covered++
					continue
				}
				u := UncoveredSentence{Paragraph: pi + 1, Partial: missed < need}
				if u.Partial {
					shown := make([]rune, len(sentence))
					for i, ch := range sentence {
						if !covered[base+i] && textrule.IsAlnum(ch) {
							ch = unicode.ToUpper(ch)
						}
						shown[i] = ch
					}
					sentence = shown
				}
				u.Text = strings.TrimSpace(string(sentence))
				sc.Sentences = append(sc.Sentences, u)
			}
			off += len(rp) + len([]rune(paragraphJoin))
		}
		if len(sc.Sentences) > 0 {
			c.Sections = append(c.Sections, sc)
		}
	}
	return c
}
