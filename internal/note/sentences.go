package note

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/corygyarmathy/slipbox/internal/textrule"
)

var (
	quotedText = regexp.MustCompile(`^.*?"(.*)"$`)
	bodyList   = regexp.MustCompile(`(?m)^[` + textrule.Space + `]*(?:[-*]|` + textrule.Digit + `+\.)[` + textrule.Space + `]+`)
)

// QuotedTexts gives the quoted text of each footnote definition below the
// frontmatter, in line order, as QuotedText reads it. The trace diff compares
// them.
func (n *Note) QuotedTexts() []string {
	var out []string
	for _, d := range n.Defs() {
		if q, ok := d.QuotedText(); ok {
			out = append(out, q)
		}
	}
	return out
}

// QuotedText gives the text from the definition value's first `"` to the
// `"` that ends the value. It is false if the value doesn't end in a `"`
// after an earlier one. It reads the quote alone, whatever the link before
// it, unlike Def.Trace: the split check compares whole traces, and the trace
// diff asks only whether every quote survived the split.
func (d Def) QuotedText() (string, bool) {
	if m := quotedText.FindStringSubmatch(d.Value); m != nil {
		return m[1], true
	}
	return "", false
}

// BodySentences gives the note's body as sentences, regardless of how its
// lines lay them out, for the trace diff. The body below the frontmatter,
// less its footnote definitions and every line starting with `#`, is joined
// with every line ending read as "\n". Its footnote references and list
// markers are dropped, its whitespace collapsed, and it is split by
// SplitSentences.
func (n *Note) BodySentences() []string {
	var lines []string
	for _, l := range n.Lines {
		switch {
		case l.Kind == Frontmatter:
		case l.Def != nil || strings.HasPrefix(l.Text, "#"):
			lines = append(lines, "")
		default:
			lines = append(lines, l.Text)
		}
	}
	if k := len(n.Lines); k > 0 && n.Lines[k-1].EOL != "" {
		// The last line's ending, which a list marker's `\s+` can take.
		lines = append(lines, "")
	}
	body := refs.ReplaceAllString(strings.Join(lines, "\n"), "")
	body = bodyList.ReplaceAllString(body, "")
	return SplitSentences(whitespace.ReplaceAllString(body, " "))
}

// SplitSentences splits s, text whose whitespace is collapsed, into
// sentences, each stripped, with no empty ones. A sentence ends at `.`, `?`,
// `!` or `:` followed by whitespace and an ASCII capital, `‘`, `'`, `"`,
// `(`, `_` or `*`. A `“` doesn't start one.
//
// The split is coarse on purpose: the trace diff splits both sides of a
// split the same way and only reports the sentences that differ, as the
// name-span edits, so a boundary it misses joins the same two sentences on
// both sides and hides nothing. It is not the coverage check's split of the
// source (mechcheck.SentenceRanges).
func SplitSentences(s string) []string {
	var out []string
	keep := func(part string) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	start := 0
	for i := 0; i < len(s); {
		r, w := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsSpace(r) {
			i += w
			continue
		}
		end := i
		for i < len(s) {
			r, w := utf8.DecodeRuneInString(s[i:])
			if !unicode.IsSpace(r) {
				break
			}
			i += w
		}
		before, _ := utf8.DecodeLastRuneInString(s[:end])
		after, _ := utf8.DecodeRuneInString(s[i:])
		if end > 0 && i < len(s) && strings.ContainsRune(".?!:", before) && sentenceStart(after) {
			keep(s[start:end])
			start = i
		}
	}
	keep(s[start:])
	return out
}

// sentenceStart says whether r can start a sentence after a split point.
func sentenceStart(r rune) bool {
	return (r >= 'A' && r <= 'Z') || strings.ContainsRune("‘'\"(_*", r)
}
