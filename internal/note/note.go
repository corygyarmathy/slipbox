// Package note is the one note model (ADR 0002 §1). It parses a note, stage 1
// to stage 3 and hubs alike, into its lines, each marked as frontmatter, the
// H1, a footnote definition or a body line. A body line carries the footnote
// references in it, and a definition is read as a trace or as the source
// footnote. Parsing is lossless: Render gives back the input byte for byte.
//
// The model keeps the text as written. The forms text is compared in, such
// as the match form, are named functions beside it (forms.go).
//
// Lines end at `\n`, `\r\n` or a lone `\r`, and nowhere else: `\v`, `\f`,
// U+0085, U+2028 and U+2029 are within a line, as Markdown has them.
package note

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/corygyarmathy/slipbox/internal/textrule"
)

// Note is one note file.
type Note struct {
	// Name is the file name, with its .md extension.
	Name  string
	Lines []Line
}

// Kind is what part of a note a line is.
type Kind int

const (
	// Body is any line not one of the other kinds, blank lines included.
	Body Kind = iota
	// Frontmatter is a line of the frontmatter, its `---` fences included.
	Frontmatter
	// H1 is the note's title line, the first `# ` line after any frontmatter.
	H1
	// Definition is a footnote definition line, `[^<n>]: <value>`.
	Definition
)

// Line is one line of a note.
type Line struct {
	// Text is the line without its line ending.
	Text string
	// EOL is the line ending as written: "\n", "\r\n", "\r", or "" for a
	// last line without one.
	EOL  string
	Kind Kind
	// Refs are the labels of the footnote references `[^<n>]` in any line
	// without a Def, in the order they appear, repeats included.
	Refs []string
	// Def is set on a line with a footnote definition's shape: a Definition
	// line, or a Frontmatter one, so a trace the model wrote into the
	// frontmatter is still read as one: the mechanical check checks it and
	// the split check pairs it, rather than its reference reading as
	// undefined. Defs gives only the Definition lines'.
	Def *Def
}

// Def is a footnote definition.
type Def struct {
	// Label is the footnote's label, the n of `[^n]`.
	Label string
	// Value is the text after `]: `.
	Value string
}

// Trace is a footnote read as a trace: `[[<file>#<section>]] "<quote>"`.
type Trace struct {
	File string
	// Section is the text after `#`, or "" if the link has no section, in
	// which case the quote is looked for in the source's `source:` line.
	Section string
	Quote   string
}

var (
	defLine  = regexp.MustCompile(`^\[\^(` + textrule.Digit + `+)\]: (.*)$`)
	refs     = regexp.MustCompile(`\[\^(` + textrule.Digit + `+)\]`)
	traceDef = regexp.MustCompile(`^\[\[([^#\]]+)(?:#([^\]|]+))?\]\] "(.*)"$`)
	linkOnly = regexp.MustCompile(`^\[\[[^\]]+\]\]$`)
)

// Trace reads the definition as a trace. It is false if the value doesn't
// have a trace's shape.
func (d Def) Trace() (Trace, bool) {
	m := traceDef.FindStringSubmatch(d.Value)
	if m == nil {
		return Trace{}, false
	}
	return Trace{File: m[1], Section: m[2], Quote: m[3]}, true
}

// SourceFootnote reads the definition as the source footnote, a bare link
// such as `[[@A Short History of Bridges - J. Doe]]`, and
// gives the link.
func (d Def) SourceFootnote() (string, bool) {
	if linkOnly.MatchString(d.Value) {
		return d.Value, true
	}
	return "", false
}

// DropRefs drops each footnote reference `[^<n>]` from s and leaves the rest,
// the whitespace around it included, as written. StripRefs drops the
// whitespace before each one too.
func DropRefs(s string) string {
	return refs.ReplaceAllString(s, "")
}

// NotUTF8 is Read's error for a note file that isn't valid UTF-8. The run
// stops on one rather than guess at its encoding: a guessed or replaced
// character would make a verbatim quote fail to match, or match text the
// file doesn't hold.
type NotUTF8 struct {
	Path string
	// Line is the 1-based line of the first invalid byte, and Offset its
	// byte offset in the file.
	Line, Offset int
}

func (e *NotUTF8) Error() string {
	return fmt.Sprintf("%s: not valid UTF-8, first invalid byte on line %d (byte offset %d)", e.Path, e.Line, e.Offset)
}

// Read reads and parses the note at path. It gives a *NotUTF8 if the file
// isn't valid UTF-8.
func Read(path string) (*Note, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(b) {
		off := firstInvalid(b)
		return nil, &NotUTF8{Path: path, Line: lineOf(b, off), Offset: off}
	}
	return Parse(filepath.Base(path), string(b)), nil
}

// lineOf gives the 1-based line of offset off in b, counting line endings as
// Parse does: `\r\n`, `\n` and a lone `\r` each end one line. A `\r` just
// before off is a lone one, as b[off] is the invalid byte and so not `\n`.
func lineOf(b []byte, off int) int {
	b = b[:off]
	return bytes.Count(b, []byte("\n")) + bytes.Count(b, []byte("\r")) - bytes.Count(b, []byte("\r\n")) + 1
}

// firstInvalid gives the offset of the first byte of b that doesn't start a
// valid UTF-8 encoding, or len(b) if every one does.
func firstInvalid(b []byte) int {
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 1 {
			return i
		}
		i += size
	}
	return len(b)
}

// ReadDir reads every `.md` note in dir, in byte order of their names. A
// name starting with a dot is not a note: it is hidden, such as the `._`
// metadata files macOS writes beside a file on a non-Apple disk.
func ReadDir(dir string) ([]*Note, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	notes := make([]*Note, 0, len(paths))
	for _, p := range paths {
		if strings.HasPrefix(filepath.Base(p), ".") {
			continue
		}
		n, err := Read(p)
		if err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, nil
}

// Parse parses a note's text. name is its file name.
func Parse(name, text string) *Note {
	n := &Note{Name: name}
	for text != "" {
		i := strings.IndexAny(text, "\r\n")
		if i < 0 {
			n.Lines = append(n.Lines, Line{Text: text})
			break
		}
		eol := text[i : i+1]
		if strings.HasPrefix(text[i:], "\r\n") {
			eol = "\r\n"
		}
		n.Lines = append(n.Lines, Line{Text: text[:i], EOL: eol})
		text = text[i+len(eol):]
	}

	body := 0
	if len(n.Lines) > 0 && n.Lines[0].Text == "---" {
		for i := 1; i < len(n.Lines); i++ {
			if n.Lines[i].Text == "---" {
				for j := 0; j <= i; j++ {
					n.Lines[j].Kind = Frontmatter
				}
				body = i + 1
				break
			}
		}
	}
	h1 := false
	for i := range n.Lines {
		l := &n.Lines[i]
		if m := defLine.FindStringSubmatch(l.Text); m != nil {
			if i >= body {
				l.Kind = Definition
			}
			l.Def = &Def{Label: m[1], Value: m[2]}
			continue
		}
		if i >= body {
			if !h1 && strings.HasPrefix(l.Text, "# ") {
				l.Kind = H1
				h1 = true
			}
		}
		for _, m := range refs.FindAllStringSubmatch(l.Text, -1) {
			l.Refs = append(l.Refs, m[1])
		}
	}
	return n
}

// Render gives the note's text back, byte for byte as parsed.
func (n *Note) Render() string {
	var b strings.Builder
	for _, l := range n.Lines {
		b.WriteString(l.Text)
		b.WriteString(l.EOL)
	}
	return b.String()
}

// SetH1 sets the note's H1 to `# <title>`, keeping its line ending. A note
// with no H1 gets one as its first line after any frontmatter, ending as the
// line after it does.
func (n *Note) SetH1(title string) {
	h1 := Line{Text: "# " + title, Kind: H1}
	at := 0
	for i := range n.Lines {
		l := &n.Lines[i]
		if l.Kind == H1 {
			l.Text, l.Refs = h1.Text, nil
			return
		}
		if l.Kind == Frontmatter {
			at = i + 1
		}
	}
	if at > 0 && n.Lines[at-1].EOL == "" {
		n.Lines[at-1].EOL = "\n"
	}
	if at < len(n.Lines) {
		h1.EOL = cmp.Or(n.Lines[at].EOL, "\n")
	}
	n.Lines = slices.Insert(n.Lines, at, h1)
}

// Defs gives the note's footnote definitions in line order, leaving out any
// in the frontmatter.
func (n *Note) Defs() []Def {
	var out []Def
	for _, l := range n.Lines {
		if l.Kind == Definition {
			out = append(out, *l.Def)
		}
	}
	return out
}

// FootnoteMarks counts the note's footnote marks: each reference `[^<n>]`
// in every line, frontmatter included, and each definition's own label. The
// split check fails a hub with any, since a hub carries no traces, so it
// counts every place a footnote can appear rather than references alone.
func (n *Note) FootnoteMarks() int {
	count := 0
	for _, l := range n.Lines {
		if l.Def != nil {
			count += 1 + len(refs.FindAllStringIndex(l.Def.Value, -1))
		} else {
			count += len(l.Refs)
		}
	}
	return count
}
