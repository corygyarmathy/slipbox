package note

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/corygyarmathy/slipbox/internal/textrule"
)

var (
	whitespace   = regexp.MustCompile(`[` + textrule.Space + `]+`)
	refsAndSpace = regexp.MustCompile(`[` + textrule.Space + `]*\[\^` + textrule.Digit + `+\]`)
	matchMarks   = strings.NewReplacer("*", "", "_", "", `\`, "")
	matchQuotes  = strings.NewReplacer("’", "'", "‘", "'", "“", `"`, "”", `"`, "\u00a0", " ")
)

// MatchForm gives s's match form: footnote references, emphasis markers and
// backslashes dropped, curly quotes straightened, no-break spaces made plain,
// and whitespace collapsed to single spaces and stripped. A quote is looked
// for in the source in it, so the trace match, the coverage check,
// quote-once, the repair pass and the loss and kept-loss steps put quotes,
// section names and source paragraphs alike into it before comparing them.
// It leaves out what the model may restyle in a verbatim quote, since
// markup, typographic quotes and line wrapping aren't the source's words.
func MatchForm(s string) string {
	s = refs.ReplaceAllString(s, "")
	s = matchMarks.Replace(s)
	s = matchQuotes.Replace(s)
	return strings.TrimSpace(whitespace.ReplaceAllString(s, " "))
}

// CollapseSpace collapses each run of whitespace in s to a single space and
// strips it, so the split check compares statements without their line
// wrapping. Unlike MatchForm it keeps markup, so a statement the split
// restyled reads as edited.
func CollapseSpace(s string) string {
	return strings.TrimSpace(whitespace.ReplaceAllString(s, " "))
}

// StripRefs drops each footnote reference `[^<n>]` and the whitespace before
// it, so a statement reads as it would without its traces: `text [^1].`
// becomes `text.`.
func StripRefs(s string) string {
	return refsAndSpace.ReplaceAllString(s, "")
}

// MatchFormOffsets is MatchForm, also giving where in s each byte
// of its result came from: at[i] is the byte range of s that byte i of form
// was made from. A collapsed run of whitespace is made from the whole run.
// It lets a caller that found a quote in another text, both in match form,
// take the text as written.
func MatchFormOffsets(s string) (form string, at [][2]int) {
	drop := make([]bool, len(s)+1)
	for _, m := range refs.FindAllStringIndex(s, -1) {
		for k := m[0]; k < m[1]; k++ {
			drop[k] = true
		}
	}
	type char struct {
		r    rune
		from [2]int
	}
	var chars []char
	for i, r := range s {
		if drop[i] {
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		switch r {
		case '*', '_', '\\':
			continue
		case '’', '‘':
			r = '\''
		case '“', '”':
			r = '"'
		case ' ':
			r = ' '
		}
		chars = append(chars, char{r, [2]int{i, i + size}})
	}
	var b strings.Builder
	for k := 0; k < len(chars); {
		if !unicode.IsSpace(chars[k].r) {
			// An invalid byte stays as it is, as the regexps leave it.
			text := string(chars[k].r)
			if chars[k].r == utf8.RuneError && chars[k].from[1]-chars[k].from[0] == 1 {
				text = s[chars[k].from[0]:chars[k].from[1]]
			}
			b.WriteString(text)
			for range len(text) {
				at = append(at, chars[k].from)
			}
			k++
			continue
		}
		j := k
		for j < len(chars) && unicode.IsSpace(chars[j].r) {
			j++
		}
		if k > 0 && j < len(chars) {
			b.WriteByte(' ')
			at = append(at, [2]int{chars[k].from[0], chars[j-1].from[1]})
		}
		k = j
	}
	return b.String(), at
}
