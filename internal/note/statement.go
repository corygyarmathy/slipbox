package note

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/corygyarmathy/slipbox/internal/textrule"
)

// Statement is one statement of a note's body, as GLOSSARY.md defines one: a
// sentence or a list item. The gates read a body line as one statement (ADR
// 0002, Consequences). The repair pass's still-fails list needs the finer
// unit, because a checker flags sentences.
type Statement struct {
	// Line is the index in the note's Lines of the line the statement is on.
	Line int
	// Text is the statement as written, its footnote references included,
	// without the whitespace around it. The first statement of a list item
	// keeps the item's marker.
	Text string
	// Refs are the labels of the footnote references in Text, in order.
	Refs []string
	// Start and End are Text's byte offsets in its line's Text.
	Start, End int
}

// statementEnd is where stage 1 ends a statement: terminal punctuation, any
// closing quotes, brackets or emphasis markers, then the statement's footnote
// references, then whitespace. A name span's reference inside a sentence
// follows no terminal punctuation, so it ends nothing.
var statementEnd = regexp.MustCompile(`[.?!:]['"’”)_*]*(?:\[\^` + textrule.Digit + `+\])+[` + textrule.Space + `]+`)

// Statements gives the statements of the note's body lines, in order. A body
// line that isn't blank or a heading is split after each statementEnd. A line
// with no footnote references, such as an untraced statement, is one
// statement however many sentences it holds.
func (n *Note) Statements() []Statement {
	var out []Statement
	for i, l := range n.Lines {
		if l.Kind != Body || strings.HasPrefix(l.Text, "#") || strings.TrimSpace(l.Text) == "" {
			continue
		}
		start := 0
		cut := func(end int) {
			if text := strings.TrimSpace(l.Text[start:end]); text != "" {
				from := start + strings.Index(l.Text[start:end], text)
				st := Statement{Line: i, Text: text, Start: from, End: from + len(text)}
				for _, m := range refs.FindAllStringSubmatch(text, -1) {
					st.Refs = append(st.Refs, m[1])
				}
				out = append(out, st)
			}
			start = end
		}
		for _, m := range statementEnd.FindAllStringIndex(l.Text, -1) {
			cut(m[1])
		}
		cut(len(l.Text))
	}
	return out
}

// IsAside says whether the note file name names an aside note: stage 1 titles
// one `<n> <segment title> - Aside - <short title>`.
func IsAside(name string) bool {
	return strings.Contains(name, " - Aside - ")
}

// FileName gives a note's file name from the name a reply gave it, adding .md
// if the reply left it off: the briefs ask for `<note title>.md`, and a name
// without the extension still names the note. A name that would leave the
// notes directory is refused.
func FileName(name string) (string, error) {
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`) || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("note name %q is not a plain file name", name)
	}
	if !strings.HasSuffix(name, ".md") {
		name += ".md"
	}
	return name, nil
}
