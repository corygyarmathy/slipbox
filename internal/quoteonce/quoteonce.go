// Package quoteonce writes the traces of statements written quote-once
// (ADR 0006). The model writes each quote once, in its
// statement, with the joining text in braces, and s2n finds each quote in the
// frame and writes its footnote, so the run folder holds traced notes as if
// the model had written them.
//
// In a statement written quote-once, text in braces, `{like this}`, is joining
// text, and the braces go. Everything else is quoted, and each run of it
// between braces is one contiguous quote, or several where it runs over more
// than one sentence: each sentence is a quote of its own, as each is a
// statement of its own. An empty `{}` ends one quote and starts the next, as
// at a mid-sentence cut. A list item's marker is not quoted. A run with no
// letter or digit in it is joining text too, and so are the characters that
// aren't letters or digits at a quote's ends, if the quote isn't found with
// them (mechcheck.Locator.Locate).
//
// Each quote's footnote reference goes after it and any closing punctuation
// that follows it, before the next space, as stage 1 writes it. Each
// footnote's quote is the source's own text where it is found exactly, with
// its emphasis markers, escapes and curly quotes, so a statement can write
// `_Stonework_` where the source has `*Stonework*`. A quote that isn't found
// is written as given, and step 1.2 grades it fuzzy or broken as it would a
// quote the model wrote.
//
// An author name span set into a quote, `I{, }<name>{, }…`, doesn't split
// it: the text either side is one trace, as stage 1 writes "I, <name>,[^m] …".
//
// Text the model wrote with footnotes and no braces, a whole note or an edit,
// is left as it is. So is a line with a footnote reference, and a line whose
// braces don't pair up: step 1.2 then finds it untraced, as it would any
// statement with no footnote.
package quoteonce

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/textrule"
)

// Default is whether a run writes its statements quote-once unless told
// otherwise: yes (ADR 0006).
const Default = true

// Tracer writes the traces of statements written quote-once from one source.
type Tracer struct {
	src *source.Source
	loc *mechcheck.Locator
}

// New gives a Tracer for src's frame.
func New(src *source.Source) *Tracer {
	return &Tracer{src: src, loc: mechcheck.NewLocator(src)}
}

// Note gives n, a note of segment seg, with every statement traced and its
// footnotes, numbered in reading order, after its body. A note with
// footnotes and no braces, as the model writes one when it doesn't write
// quote-once, is given back as it is. In a note with both, each line with a
// footnote reference is left as it is, and the others' footnotes are
// numbered after the note's own.
func (t *Tracer) Note(seg int, n *note.Note) *note.Note {
	defs := n.Defs()
	footnoted := len(defs) > 0
	braced := false
	for _, l := range n.Lines {
		footnoted = footnoted || len(l.Refs) > 0
		braced = braced || (l.Kind == note.Body && strings.ContainsAny(l.Text, "{}"))
	}
	if footnoted && !braced {
		return n
	}
	next := nextLabel(defs)
	var b strings.Builder
	for _, l := range n.Lines {
		if l.Kind == note.Definition {
			continue
		}
		text := l.Text
		if l.Kind == note.Body && len(l.Refs) == 0 && !strings.HasPrefix(text, "#") && strings.TrimSpace(text) != "" {
			var d []note.Def
			text, d = t.line(seg, text, next)
			next += len(d)
			defs = append(defs, d...)
		}
		b.WriteString(text + l.EOL)
	}
	if len(defs) == 0 {
		return n
	}
	body := strings.TrimRight(b.String(), "\r\n")
	b.Reset()
	b.WriteString(body + "\n\n")
	for _, d := range defs {
		b.WriteString("[^" + d.Label + "]: " + d.Value + "\n")
	}
	return note.Parse(n.Name, b.String())
}

// Edit gives e, an edit to a note of segment seg, with its new statements
// traced, their footnotes numbered within the edit as note.Edit's are. An
// edit with footnotes is read as Note reads a note: with no braces it is
// given back as it is, and with braces only its lines, each a paragraph or a
// list item, with no footnote reference are traced.
func (t *Tracer) Edit(seg int, e note.Edit) note.Edit {
	if (len(e.Defs) > 0 || note.StripRefs(e.Text) != e.Text) && !strings.ContainsAny(e.Text, "{}") {
		return e
	}
	e.Defs = slices.Clone(e.Defs)
	next := nextLabel(e.Defs)
	lines := strings.Split(e.Text, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" || note.StripRefs(l) != l {
			continue
		}
		var d []note.Def
		lines[i], d = t.line(seg, l, next)
		next += len(d)
		e.Defs = append(e.Defs, d...)
	}
	e.Text = strings.Join(lines, "\n")
	return e
}

// nextLabel is the first footnote label after defs' numbered ones.
func nextLabel(defs []note.Def) int {
	next := 1
	for _, d := range defs {
		if k, err := strconv.Atoi(d.Label); err == nil && k >= next {
			next = k + 1
		}
	}
	return next
}

// listMarker is a list item's marker, as the punctuation lint reads one.
var listMarker = regexp.MustCompile(`^[` + textrule.Space + `]*(?:[-*]|` + textrule.Digit + `+\.)[` + textrule.Space + `]+`)

// closing is what may follow a quote before its footnote reference: closing
// punctuation and emphasis markers.
const closing = `.,;:?!'"’”)]_*`

// part is a run of a line written quote-once: quoted text, or joining text.
type part struct {
	text    string
	joining bool
}

// line traces one line written quote-once: it gives the line with braces
// gone and a footnote reference after each quote, labelled from next, and
// the footnotes' definitions. A line whose braces don't pair up is given back
// as it is, with no footnotes.
func (t *Tracer) line(seg int, text string, next int) (string, []note.Def) {
	parts, ok := split(text)
	if !ok {
		return text, nil
	}
	// The line with braces gone, and the byte range of each quote in it.
	var out strings.Builder
	var quotes [][2]int
	for _, p := range parts {
		s := p.text
		if strings.HasSuffix(out.String(), " ") {
			s = strings.TrimLeft(s, " ")
		}
		start := out.Len()
		out.WriteString(s)
		if p.joining {
			continue
		}
		for _, q := range sentences(s) {
			quotes = append(quotes, [2]int{start + q[0], start + q[1]})
		}
	}
	line := out.String()

	type ref struct {
		at  int
		def note.Def
	}
	var refs []ref
	addRef := func(i int, loc mechcheck.Located) {
		limit := len(line)
		if i+1 < len(quotes) {
			limit = quotes[i+1][0]
		}
		label := strconv.Itoa(next + len(refs))
		refs = append(refs, ref{refAt(line, quotes[i][1], limit), note.Def{Label: label, Value: t.value(loc)}})
	}
	quoted := func(i int) string { return line[quotes[i][0]:quotes[i][1]] }
	var after mechcheck.Place
	for i := 0; i < len(quotes); i++ {
		quote := quoted(i)
		if !hasAlnum(note.MatchForm(quote)) {
			continue
		}
		// An author name span set into a quote, as `I{, }<name>{, }…` sets
		// it, leaves the quote whole: the text either side of it is one
		// trace, after the name span's, as in "I, <name>,[^m] …".
		if i+2 < len(quotes) {
			if name := t.loc.Locate(seg, quoted(i+1), after); name.Exact && name.Section == "" {
				if whole := t.loc.Locate(seg, quote+" "+quoted(i+2), after); whole.Exact && whole.Section != "" {
					addRef(i+1, name)
					addRef(i+2, whole)
					after = whole.End
					i += 2
					continue
				}
			}
		}
		loc := t.loc.Locate(seg, quote, after)
		if loc.Exact {
			after = loc.End
		}
		addRef(i, loc)
	}

	var b strings.Builder
	var defs []note.Def
	from := 0
	for _, r := range refs {
		b.WriteString(line[from:r.at] + "[^" + r.def.Label + "]")
		from = r.at
		defs = append(defs, r.def)
	}
	b.WriteString(line[from:])
	return b.String(), defs
}

// refAt is where the footnote reference of a quote ending at end in line
// goes: after the closing punctuation and emphasis markers that follow it,
// short of limit, or at end itself if they run into a word, as the joining
// text in `<name>{'s}` does.
func refAt(line string, end, limit int) int {
	at := end
	for at < limit {
		r, size := utf8.DecodeRuneInString(line[at:])
		if !strings.ContainsRune(closing, r) {
			break
		}
		at += size
	}
	if r, _ := utf8.DecodeRuneInString(line[at:]); at > end && at < len(line) && textrule.IsAlnum(r) {
		return end
	}
	return at
}

// value is a trace's definition: `[[<source>#<section>]] "<quote>"`, or with
// no section for a quote from the `source:` line.
func (t *Tracer) value(loc mechcheck.Located) string {
	link := t.src.Name
	if loc.Section != "" {
		link += "#" + loc.Section
	}
	return "[[" + link + `]] "` + loc.Quote + `"`
}

// split cuts a line written quote-once into its parts: a list marker, then
// quoted runs and braced joining text in turn. It is false if a brace doesn't
// pair up.
func split(text string) ([]part, bool) {
	var parts []part
	if m := listMarker.FindString(text); m != "" {
		parts = append(parts, part{m, true})
		text = text[len(m):]
	}
	for text != "" {
		open := strings.IndexAny(text, "{}")
		if open < 0 {
			parts = append(parts, part{text, false})
			break
		}
		if text[open] == '}' {
			return nil, false
		}
		end := strings.IndexAny(text[open+1:], "{}")
		if end < 0 || text[open+1+end] == '{' {
			return nil, false
		}
		end += open + 1
		if open > 0 {
			parts = append(parts, part{text[:open], false})
		}
		// An empty `{}` still ends the quote before it.
		parts = append(parts, part{text[open+1 : end], true})
		text = text[end+1:]
	}
	return parts, true
}

// sentences gives the byte ranges in s, a quoted run, of its statements,
// each without the space around it. A statement ends where stage 1 would
// put a footnote reference: at `.`, `?` or `!`, then any closing quotes,
// brackets or emphasis markers, then whitespace, where the next word starts
// with a capital letter, perhaps behind an opening quote, bracket or
// emphasis marker. A period after an abbreviation such as "e.g." ends
// nothing, and nor does a mark inside a quotation the run opens with `‘` or
// `“`, or inside an emphasis run. So unlike the coverage check's sentences,
// a statement doesn't end before a citation such as "(_Apol_. 21b)", or
// mid-way through a quoted question.
func sentences(s string) [][2]int {
	var out [][2]int
	start, depth := 0, 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == '‘' || r == '“':
			depth++
			continue
		case r == '’' || r == '”':
			depth = max(depth-1, 0)
			continue
		case !strings.ContainsRune(".?!", r):
			continue
		}
		// The closing marks after it, which may close the quotation it is in.
		mark, j, d := i-size, i, depth
		for j < len(s) {
			c, w := utf8.DecodeRuneInString(s[j:])
			if !strings.ContainsRune(closing, c) {
				break
			}
			if c == '’' || c == '”' {
				d = max(d-1, 0)
			}
			j += w
		}
		k := j
		for k < len(s) && unicode.IsSpace(rune(s[k])) {
			k++
		}
		first, _ := utf8.DecodeRuneInString(strings.TrimLeft(s[k:], `'"(_*‘“`))
		emphasis := (strings.Count(s[start:mark], "_")+strings.Count(s[start:mark], "*"))%2 == 1
		if d > 0 || emphasis || k == j || !unicode.IsUpper(first) || (r == '.' && mechcheck.EndsInAbbreviation(s[start:mark+1])) {
			continue
		}
		out = append(out, [2]int{start, j})
		start, depth, i = k, 0, k
	}
	if end := len(strings.TrimRightFunc(s, unicode.IsSpace)); end > start {
		out = append(out, [2]int{start, end})
	}
	for i := range out {
		for out[i][0] < out[i][1] && unicode.IsSpace(rune(s[out[i][0]])) {
			out[i][0]++
		}
	}
	return out
}

func hasAlnum(s string) bool {
	return strings.IndexFunc(s, textrule.IsAlnum) >= 0
}
