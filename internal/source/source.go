// Package source is the one source model (ADR 0002 §2): it reads a source
// file into its `source:` line, its frame text and its numbered segments.
package source

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Source is one source file: a frame, given whole, cut into segments.
type Source struct {
	// Name is the file name without its .md extension. Traces link to it.
	Name string
	// CitekeyLink is the `source:` line's value, unquoted, such as
	// `[[@A Short History of Bridges - J. Doe]]`.
	CitekeyLink string
	// Frame is the whole file, frontmatter included, byte for byte except
	// that each line ending is `\n` (Parse).
	Frame string
	// Segments are numbered from 0 in file order.
	Segments []Segment
}

// Segment is a section of the frame, or the untitled text before its first
// `##` heading.
type Segment struct {
	Number int
	// Heading is the section heading as the file gives it, without `## `.
	// For the pre-heading segment it is the H1 text without `# `.
	Heading string
	// Title is the heading with any leading chapter number dropped and curly
	// apostrophes straightened. Note names are built from it.
	Title string
	// PreHeading is true for the text before the first `##` heading.
	PreHeading bool
	// Paragraphs are the segment's non-blank lines, one paragraph per line,
	// as the file gives them. Heading lines of any level are left out: a
	// heading names a section, and a trace quotes the section's text, not its
	// name.
	Paragraphs []string
}

// NotUTF8 is Read's error for a source file that isn't valid UTF-8. The run
// stops on one rather than guess at its encoding: a trace quotes the source
// verbatim, and a guessed or replaced character would put words in it the
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

// Read reads and parses the source file at path. It gives a *NotUTF8 if the
// file isn't valid UTF-8.
func Read(path string) (*Source, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(b) {
		off := firstInvalid(b)
		return nil, &NotUTF8{Path: path, Line: lineOf(b, off), Offset: off}
	}
	return Parse(strings.TrimSuffix(filepath.Base(path), ".md"), string(b))
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

// Parse parses a source's text. name is the file name without .md. `\r\n`
// and lone `\r` line endings become `\n` first, as Markdown (CommonMark)
// counts all three as line endings: a source saved on Windows or by an old
// Mac editor reads the same as one saved with `\n`, and the note model ends
// lines at the same three.
func Parse(name, text string) (*Source, error) {
	text = newlines.Replace(text)
	s := &Source{Name: name, Frame: text}
	lines := strings.Split(text, "\n")

	body := 0
	if len(lines) > 0 && lines[0] == "---" {
		end := -1
		for i := 1; i < len(lines); i++ {
			if lines[i] == "---" {
				end = i
				break
			}
			if v, ok := strings.CutPrefix(lines[i], "source:"); ok {
				s.CitekeyLink = strings.Trim(strings.TrimSpace(v), `"`)
			}
		}
		if end < 0 {
			return nil, errors.New("source: frontmatter is not closed")
		}
		body = end + 1
	}
	if s.CitekeyLink == "" {
		return nil, errors.New("source: no `source:` line in the frontmatter")
	}

	var h1 string
	var pre bool
	var preParagraphs []string
	for _, line := range lines[body:] {
		if heading, ok := strings.CutPrefix(line, "## "); ok {
			s.Segments = append(s.Segments, Segment{Heading: heading})
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(s.Segments) > 0 {
			if !headingLine.MatchString(line) {
				last := &s.Segments[len(s.Segments)-1]
				last.Paragraphs = append(last.Paragraphs, line)
			}
			continue
		}
		if heading, ok := strings.CutPrefix(line, "# "); ok && h1 == "" {
			h1 = heading
			continue
		}
		pre = true
		if !headingLine.MatchString(line) {
			preParagraphs = append(preParagraphs, line)
		}
	}

	if pre {
		if h1 == "" {
			return nil, errors.New("source: text before the first `##` heading but no H1 to name it")
		}
		s.Segments = append([]Segment{{Heading: h1, PreHeading: true, Paragraphs: preParagraphs}}, s.Segments...)
	}
	if len(s.Segments) == 0 {
		return nil, errors.New("source: no segments")
	}
	for i := range s.Segments {
		s.Segments[i].Number = i
		s.Segments[i].Title = Title(s.Segments[i].Heading)
	}
	return s, nil
}

var newlines = strings.NewReplacer("\r\n", "\n", "\r", "\n")

// headingLine is a heading of any level: one or more `#` then a space.
var headingLine = regexp.MustCompile(`^#+ `)

var chapterNumber = regexp.MustCompile(`^\d+\.\s+`)

var straightenApostrophes = strings.NewReplacer("’", "'", "‘", "'")

// Title gives a heading's title: any leading chapter number (`4. `) dropped
// and curly apostrophes straightened, so `3. The Builder’s Rule` gives
// `The Builder's Rule`.
func Title(heading string) string {
	return straightenApostrophes.Replace(chapterNumber.ReplaceAllString(heading, ""))
}

// String names a segment the way errors and logs refer to it.
func (seg Segment) String() string { return fmt.Sprintf("segment %d (%s)", seg.Number, seg.Heading) }
