// Package mechcheck is step 1.2, the mechanical check. Per note it grades
// each trace exact, fuzzy or broken, lists untraced statements and
// cross-segment footnotes, and runs the punctuation lint. Over the whole frame
// it then runs the coverage check (coverage.go).
//
// The trace match ignores case on the quote's first character, so a quote
// taken from mid-sentence and capitalised still matches. A quote found as
// given is preferred, so its span, for coverage, is where the quote is as the
// model wrote it.
//
// The sections are the source model's segments, so a `###` heading, or an H1
// with no text before the first `##`, doesn't misnumber them. Text between
// the frontmatter and the H1 is in segment 0's section, as extract gave it to
// the model, so a trace can quote it.
//
// A note whose name starts with a number that isn't one of the source's
// segments gets a NoSegment finding, and the rest of the note is checked,
// since stage 1 flags never stop the run. A footnote that isn't a trace, in a
// note named for a segment, has no section, so it is left out of the
// cross-segment listing. It is still reported as broken by any statement that
// cites it.
//
// A statement is a line, not a sentence (ADR 0002, Consequences): any line
// but a blank, a heading or a footnote definition. A frontmatter line is one
// too, so it is linted and counted, and one with no trace is untraced.
//
// The report is plain text (ADR 0002 §3). A section that isn't found is
// quoted as Go quotes a string. A footnote that is missing or isn't a trace
// has no section to quote, so it reads `(not a trace)`.
// It isn't `(no section)`, since the stage 1 brief allows a trace with no
// section, the author name span.
package mechcheck

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/corygyarmathy/slipbox/internal/gate"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/seqmatch"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/textrule"
)

// FuzzyThreshold is the least similarity ratio a fuzzy match needs.
const FuzzyThreshold = 0.90

// RecordSection is the section a trace with no `#section` is looked for in:
// the source's `source:` line.
const RecordSection = "@record"

// Kind is what a finding reports.
type Kind int

const (
	// NoSegment is a note whose name starts with a number that isn't one of
	// the source's segments.
	NoSegment Kind = iota
	// CrossSegment is a trace into a section other than its note's segment:
	// a name-span candidate for the run report.
	CrossSegment
	// LeadIn is a line followed by a list item that doesn't end in ":".
	LeadIn
	// NoTerminal is a line, not a lead-in, without terminal punctuation.
	NoTerminal
	// LowercaseItem is a list item that starts with a lowercase letter.
	LowercaseItem
	// Untraced is a statement with no footnote reference.
	Untraced
	// SectionNotFound is a broken trace whose section isn't in the source,
	// or a reference to a footnote that isn't a trace.
	SectionNotFound
	// Broken is a trace whose quote isn't in its section, exactly or fuzzily.
	Broken
	// Fuzzy is a trace matched fuzzily.
	Fuzzy

	// numKinds is the number of kinds, for kindNames.
	numKinds
)

// Finding is one line of a note's report.
type Finding struct {
	Kind Kind `json:"kind"`
	// Line is the index in the note's Lines of the line the finding is about.
	// For CrossSegment it is the footnote's definition. For NoSegment, which
	// is about the note's name, it is -1.
	Line int `json:"line"`
	// Ref is the footnote label, for the trace kinds.
	Ref string `json:"ref"`
	// Section is the trace's section, in match form. For SectionNotFound it is
	// meaningless when NoTrace is set.
	Section string `json:"section"`
	// NoTrace is set on SectionNotFound when the footnote is missing or isn't
	// a trace.
	NoTrace bool `json:"no_trace"`
	// Quote is the trace's quote, in match form.
	Quote string `json:"quote"`
	// Matched is the source text a Fuzzy quote matched.
	Matched string `json:"matched"`
	// Text is the line as the lint read it, footnote references dropped, or
	// the Untraced statement. For NoSegment it is the note's number as its
	// name gives it.
	Text string `json:"text"`
	// Segments is the source's segment count, for NoSegment.
	Segments int `json:"segments"`
}

// Failed says whether the finding is a flag for the repair pass. Cross-segment
// footnotes and fuzzy matches are not.
func (f Finding) Failed() bool {
	return f.Kind != CrossSegment && f.Kind != Fuzzy
}

// BrokenOrUntraced says whether the finding is a broken trace or an untraced
// statement, the findings for which the final stage 1 notes drop a statement.
// Lint and a note name with no segment don't.
func (f Finding) BrokenOrUntraced() bool {
	return f.Kind == Broken || f.Kind == SectionNotFound || f.Kind == Untraced
}

// String renders the finding as the report prints it. A section name is
// quoted, as Go quotes a string.
func (f Finding) String() string {
	switch f.Kind {
	case NoSegment:
		return fmt.Sprintf("  NO SEGMENT %q: the source's segments are 0 to %d", f.Text, f.Segments-1)
	case CrossSegment:
		return fmt.Sprintf("  CROSS-SEGMENT [^%s] -> %s: %s", f.Ref, f.Section, textrule.Head(f.Quote, 100))
	case LeadIn:
		return "  PUNCT lead-in without ':': " + textrule.Tail(f.Text, 80)
	case NoTerminal:
		return "  PUNCT no terminal punctuation: " + textrule.Tail(f.Text, 80)
	case LowercaseItem:
		return "  PUNCT list item lowercase: " + textrule.Head(f.Text, 80)
	case Untraced:
		return "  UNTRACED statement: " + f.Text
	case SectionNotFound:
		if f.NoTrace {
			return fmt.Sprintf("  BROKEN [^%s] (not a trace)", f.Ref)
		}
		return fmt.Sprintf("  BROKEN [^%s] (section %q not found)", f.Ref, f.Section)
	case Broken:
		return fmt.Sprintf("  BROKEN [^%s] %s", f.Ref, textrule.Head(f.Quote, 90))
	case Fuzzy:
		return fmt.Sprintf("  fuzzy  [^%s] quote : %s\n                source: %s", f.Ref, textrule.Head(f.Quote, 110), textrule.Head(f.Matched, 110))
	}
	panic(fmt.Sprintf("mechcheck: unknown finding kind %d", f.Kind))
}

// NoteReport is one note's findings: first the cross-segment footnotes and
// the lint, which concern the note as a whole, then the traces statement by
// statement, down the note.
type NoteReport struct {
	Name     string    `json:"name"`
	Findings []Finding `json:"findings"`
}

// Stats are the check's counts over every note, which the coverage report
// prints beside its own (Coverage.Stats).
type Stats struct {
	Exact, Fuzzy, Broken int
	// UntracedStatements counts statements with no footnote reference and
	// statements whose every trace is broken.
	UntracedStatements int
	Statements         int
}

// Span is a verified trace's place in its section's text, in code points,
// end exclusive. A fuzzy span may run past the text's end.
type Span struct{ Start, End int }

var _ gate.Report = (*Report)(nil)

// Report is the check's result.
type Report struct {
	Notes []NoteReport
	Stats Stats
	// Spans are the verified traces' spans, by section name in match form.
	Spans map[string][]Span
	// Coverage is the coverage check over the whole frame, from each
	// segment's own spans.
	Coverage *Coverage
}

// Failed says whether any note has a flag for the repair pass. Stage 1's
// flags never stop the run.
func (r *Report) Failed() bool {
	for _, n := range r.Notes {
		for _, f := range n.Findings {
			if f.Failed() {
				return true
			}
		}
	}
	return false
}

// String renders the report up to its coverage section. Coverage renders the
// rest.
func (r *Report) String() string {
	var b strings.Builder
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "\n### %s\n", n.Name)
		for _, f := range n.Findings {
			b.WriteString(f.String())
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// section is one section's text in match form, as find() searches it.
type section struct {
	text   string
	runes  []rune
	starts []int
	// spans are the spans of the verified traces that resolved to this
	// section. Two segments with the same heading have a section each, and
	// a trace resolves to only one of them.
	spans []Span
}

// Check runs the mechanical check on notes, in the order given, against src.
func Check(src *source.Source, notes []*note.Note) *Report {
	segs := segmentSections(src)
	sections := map[string]*section{}
	for _, s := range segs {
		sections[s.heading] = s.section
	}
	sections[RecordSection] = newSection(note.MatchForm(src.CitekeyLink))

	r := &Report{Spans: map[string][]Span{}}
	for _, n := range notes {
		r.Notes = append(r.Notes, checkNote(src, sections, n, r))
	}
	r.Coverage = coverage(segs, r)
	return r
}

// segmentSection is a segment's section: its number, its heading and
// paragraphs in match form, and its text.
type segmentSection struct {
	number  int
	heading string
	paras   []string
	*section
}

// segmentSections gives each of src's segments its section, in segment order.
func segmentSections(src *source.Source) []segmentSection {
	var out []segmentSection
	for _, seg := range src.Segments {
		var paras []string
		for _, p := range searchedParagraphs(seg) {
			paras = append(paras, note.MatchForm(p))
		}
		out = append(out, segmentSection{seg.Number, note.MatchForm(seg.Heading), paras, newSection(strings.Join(paras, paragraphJoin))})
	}
	return out
}

// searchedParagraphs gives the paragraphs of seg that the trace match
// searches, as written but for a blockquote's `> `: endnote definitions are
// left out, and so is a paragraph with nothing left in match form.
func searchedParagraphs(seg source.Segment) []string {
	var out []string
	for _, p := range seg.Paragraphs {
		if strings.HasPrefix(p, "[^") { // endnote definitions: references, not text
			continue
		}
		if p = strings.TrimLeft(p, "> "); note.MatchForm(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

const paragraphJoin = " ¶ "

func newSection(text string) *section {
	return &section{text: text, runes: []rune(text), starts: sentenceStarts(text)}
}

// trace is a footnote as checkNote reads it.
type trace struct {
	ok             bool
	section, quote string
	line           int
}

func checkNote(src *source.Source, sections map[string]*section, n *note.Note, r *Report) NoteReport {
	nr := NoteReport{Name: n.Name}
	add := func(f Finding) { nr.Findings = append(nr.Findings, f) }

	// Footnotes by label, in the order the note first defines them, so the
	// cross-segment findings read down the note. A label defined twice takes
	// its last definition, as the split check reads it too.
	var labels []string
	traces := map[string]trace{}
	for i, l := range n.Lines {
		if l.Def == nil {
			continue
		}
		t := trace{line: i}
		if tr, ok := l.Def.Trace(); ok {
			t.ok, t.quote, t.section = true, note.MatchForm(tr.Quote), RecordSection
			if tr.Section != "" {
				t.section = note.MatchForm(tr.Section)
			}
		}
		if _, seen := traces[l.Def.Label]; !seen {
			labels = append(labels, l.Def.Label)
		}
		traces[l.Def.Label] = t
	}

	seg, num, ok := noteSegment(src, n.Name)
	if !ok {
		add(Finding{Kind: NoSegment, Line: -1, Text: num, Segments: len(src.Segments)})
	}
	if seg != "" {
		for _, label := range labels {
			if t := traces[label]; t.ok && t.section != seg {
				add(Finding{Kind: CrossSegment, Line: t.line, Ref: label, Section: t.section, Quote: t.quote})
			}
		}
	}

	lint(n, add)

	for i, l := range n.Lines {
		if l.Def != nil || strings.HasPrefix(l.Text, "#") {
			continue
		}
		line := strings.TrimSpace(l.Text)
		if line == "" {
			continue
		}
		r.Stats.Statements++
		if len(l.Refs) == 0 {
			r.Stats.UntracedStatements++
			add(Finding{Kind: Untraced, Line: i, Text: note.StripRefs(line)})
			continue
		}
		verified := 0
		for _, ref := range l.Refs {
			t := traces[ref]
			sec, ok := sections[t.section]
			if !t.ok || !ok {
				r.Stats.Broken++
				add(Finding{Kind: SectionNotFound, Line: i, Ref: ref, Section: t.section, NoTrace: !t.ok})
				continue
			}
			grade, start, end := find(t.quote, sec)
			switch grade {
			case broken:
				r.Stats.Broken++
				add(Finding{Kind: Broken, Line: i, Ref: ref, Section: t.section, Quote: t.quote})
				continue
			case exact:
				r.Stats.Exact++
			case fuzzy:
				r.Stats.Fuzzy++
				add(Finding{Kind: Fuzzy, Line: i, Ref: ref, Section: t.section, Quote: t.quote,
					Matched: string(sec.runes[start:min(end, len(sec.runes))])})
			}
			verified++
			r.Spans[t.section] = append(r.Spans[t.section], Span{start, end})
			sec.spans = append(sec.spans, Span{start, end})
		}
		if verified == 0 {
			r.Stats.UntracedStatements++
		}
	}
	return nr
}

// noteSegment is the heading, in match form, of the segment a note's name starts
// with the number of, or "" for a note whose name doesn't start with a digit.
// It is false, with the number as the name gives it, if that number isn't
// one of src's segments.
func noteSegment(src *source.Source, name string) (heading, num string, ok bool) {
	if first, _ := utf8.DecodeRuneInString(name); !unicode.IsDigit(first) {
		return "", "", true
	}
	num, _, _ = strings.Cut(name, " ")
	k, err := strconv.Atoi(num)
	if err != nil || k < 0 || k >= len(src.Segments) {
		return "", num, false
	}
	return note.MatchForm(src.Segments[k].Heading), num, true
}

var (
	listItem = regexp.MustCompile(`^[` + textrule.Space + `]*(?:[-*]|` + textrule.Digit + `+\.)[` + textrule.Space + `]+`)
	terminal = regexp.MustCompile(`[.?!:]['"’”)_*]*$`)
	itemMark = regexp.MustCompile(`^[` + textrule.Space + `\-*` + textrule.Digit + `.]+[_*'"‘“(]*`)
)

// lint is the punctuation lint, over every non-blank line that isn't a
// heading or a footnote.
func lint(n *note.Note, add func(Finding)) {
	type lintLine struct {
		text string
		at   int
	}
	var lines []lintLine
	for i, l := range n.Lines {
		if strings.TrimSpace(l.Text) != "" && !strings.HasPrefix(l.Text, "#") && !strings.HasPrefix(l.Text, "[^") {
			lines = append(lines, lintLine{strings.TrimRightFunc(l.Text, unicode.IsSpace), i})
		}
	}
	for i, l := range lines {
		bare := strings.TrimSpace(note.StripRefs(l.text))
		item := listItem.MatchString(l.text)
		leadIn := !item && i+1 < len(lines) && listItem.MatchString(lines[i+1].text)
		if leadIn && !strings.HasSuffix(bare, ":") {
			add(Finding{Kind: LeadIn, Line: l.at, Text: bare})
		} else if !leadIn && !terminal.MatchString(bare) {
			add(Finding{Kind: NoTerminal, Line: l.at, Text: bare})
		}
		if item {
			first, w := utf8.DecodeRuneInString(itemMark.ReplaceAllString(bare, ""))
			if w > 0 && !unicode.IsUpper(first) {
				add(Finding{Kind: LowercaseItem, Line: l.at, Text: bare})
			}
		}
	}
}

type grade int

const (
	broken grade = iota
	exact
	fuzzy
)

// find gives the quote's place in the section's text, exactly, or else the
// best window the quote's length that starts at a sentence start, if its
// ratio is at least FuzzyThreshold. Places are in code points. Both ignore
// case on the quote's first character.
func find(quote string, sec *section) (grade, int, int) {
	if i := indexFirstFolded(sec.text, quote); i >= 0 {
		start := utf8.RuneCountInString(sec.text[:i])
		return exact, start, start + utf8.RuneCountInString(quote)
	}
	n := utf8.RuneCountInString(quote)
	best, bestStart := 0.0, 0
	for _, s := range sec.starts {
		window := string(sec.runes[s:min(s+n, len(sec.runes))])
		r := seqmatch.Ratio(firstCaseOf(window, quote), window)
		if r > best {
			best, bestStart = r, s
		}
	}
	if best >= FuzzyThreshold {
		return fuzzy, bestStart, bestStart + n
	}
	return broken, 0, 0
}

// indexFirstFolded is strings.Index, ignoring case on quote's first
// character: the earliest place quote is in text as given, or else the
// earliest with its first character in another case. Preferring the quote as
// given puts its span where the quote is as the model wrote it, not at an
// earlier place that differs only in case.
func indexFirstFolded(text, quote string) int {
	at := strings.Index(text, quote)
	if at >= 0 {
		return at
	}
	first, size := utf8.DecodeRuneInString(quote)
	for r := unicode.SimpleFold(first); r != first; r = unicode.SimpleFold(r) {
		if i := strings.Index(text, string(r)+quote[size:]); i >= 0 && (at < 0 || i < at) {
			at = i
		}
	}
	return at
}

// firstCaseOf gives quote with its first character in the case window's first
// character has, if the two differ only in case.
func firstCaseOf(window, quote string) string {
	w, _ := utf8.DecodeRuneInString(window)
	q, size := utf8.DecodeRuneInString(quote)
	if w != q && strings.EqualFold(string(w), string(q)) {
		return string(w) + quote[size:]
	}
	return quote
}

// sentenceStarts gives the code-point offset of each sentence start in a
// section's text, paragraph by paragraph.
func sentenceStarts(text string) []int {
	var out []int
	off := 0
	for _, p := range strings.Split(text, paragraphJoin) {
		rp := []rune(p)
		for _, s := range splitSentences(rp) {
			out = append(out, off+s[0])
		}
		off += len(rp) + utf8.RuneCountInString(paragraphJoin)
	}
	return out
}

// splitSentences gives a paragraph's sentences as code-point ranges. A
// sentence ends at `.`, `?` or `!`, then any closing quotes or brackets, then
// whitespace, where the next sentence starts with a capital or digit, perhaps
// behind an opening quote or bracket. A period after one of the abbreviations
// in abbreviations ends no sentence.
func splitSentences(p []rune) [][2]int {
	var out [][2]int
	start := 0
	for i := 0; i < len(p); i++ {
		if !strings.ContainsRune(".?!", p[i]) {
			continue
		}
		j := i + 1
		for j < len(p) && strings.ContainsRune(`'")`, p[j]) {
			j++
		}
		k := j
		for k < len(p) && unicode.IsSpace(p[k]) {
			k++
		}
		if k == j || !startsSentence(p[k:]) {
			continue
		}
		if !abbreviation(p[start : i+1]) {
			out = append(out, [2]int{start, k})
			start = k
		}
		i = k - 1
	}
	out = append(out, [2]int{start, len(p)})
	kept := out[:0]
	for _, s := range out {
		if strings.TrimSpace(string(p[s[0]:s[1]])) != "" {
			kept = append(kept, s)
		}
	}
	return kept
}

func startsSentence(p []rune) bool {
	capOrDigit := func(r rune) bool { return (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') }
	if len(p) > 0 && capOrDigit(p[0]) {
		return true
	}
	return len(p) > 1 && strings.ContainsRune(`'"(`, p[0]) && capOrDigit(p[1])
}

var abbreviations = []string{"St", "Mr", "Mrs", "Dr", "vs", "e.g", "i.e"}

// EndsInAbbreviation says whether s ends in one of the abbreviations whose
// period ends no sentence, such as "e.g.", as the coverage check reads it.
func EndsInAbbreviation(s string) bool { return abbreviation([]rune(s)) }

// abbreviation says whether s ends in one of abbreviations and a period, the
// abbreviation starting a word.
func abbreviation(s []rune) bool {
	for _, a := range abbreviations {
		ra := []rune(a + ".")
		at := len(s) - len(ra)
		if at >= 0 && string(s[at:]) == string(ra) && (at == 0 || !textrule.IsWord(s[at-1])) {
			return true
		}
	}
	return false
}
