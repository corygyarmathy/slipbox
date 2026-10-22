package source_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/source"
)

// chapter is an invented source chapter, saved as chapterName, with text before
// its first `##` heading and the kinds of heading a chapter has: a
// numbered H1, a curly apostrophe, an abbreviation, sentences, dashes and a
// sub-heading. Its paragraphs include a blockquote, an endnote and a line
// starting with `#` but no space, which is no heading.
const (
	chapterName = "Bridges - Ch 3 - The Stone Arch"
	chapter     = "---\n" +
		"source: \"[[@A Short History of Bridges - J. Doe]]\"\n" +
		"chapter: 3\n" +
		"---\n\n" +
		"# 3. The Stone Arch\n\n" +
		"An arch stands by its shape.\n\n" +
		"> Stone works best in compression.\n\n" +
		"## The Builder’s Rule\n\n" +
		"Each stone leans on the next.\n\n" +
		"### A sub-heading\n\n" +
		"The rule is old.[^1]\n\n" +
		"[^1]: An endnote.\n\n" +
		"## Arch vs. Beam\n\n" +
		"#beams bend where arches push.\n\n" +
		"## Load. Thrust. Span.\n\n" +
		"A load becomes thrust.\n\n" +
		"## Rise—and Fall—of the Keystone\n\n" +
		"## 4. Key Lessons\n\n" +
		"Build the centring first.\n"
)

// Read names the source for its file, takes its `source:` line, numbers its
// segments from the text before the first heading, gives each a title, and
// keeps the file byte for byte as its frame.
func TestRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), chapterName+".md")
	if err := os.WriteFile(path, []byte(chapter), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := source.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if src.Name != chapterName {
		t.Errorf("Name = %q, want %q", src.Name, chapterName)
	}
	if want := "[[@A Short History of Bridges - J. Doe]]"; src.CitekeyLink != want {
		t.Errorf("CitekeyLink = %q, want %q", src.CitekeyLink, want)
	}
	if src.Frame != chapter {
		t.Error("Frame is not the file byte for byte")
	}
	type segment struct {
		Heading, Title string
		Paragraphs     []string
	}
	want := []segment{
		{"3. The Stone Arch", "The Stone Arch", []string{"An arch stands by its shape.", "> Stone works best in compression."}},
		{"The Builder’s Rule", "The Builder's Rule", []string{"Each stone leans on the next.", "The rule is old.[^1]", "[^1]: An endnote."}},
		{"Arch vs. Beam", "Arch vs. Beam", []string{"#beams bend where arches push."}},
		{"Load. Thrust. Span.", "Load. Thrust. Span.", []string{"A load becomes thrust."}},
		{"Rise—and Fall—of the Keystone", "Rise—and Fall—of the Keystone", nil},
		{"4. Key Lessons", "Key Lessons", []string{"Build the centring first."}},
	}
	var got []segment
	for i, seg := range src.Segments {
		if seg.Number != i {
			t.Errorf("segment %d has Number %d", i, seg.Number)
		}
		if seg.PreHeading != (i == 0) {
			t.Errorf("segment %d: PreHeading = %v", i, seg.PreHeading)
		}
		got = append(got, segment{seg.Heading, seg.Title, seg.Paragraphs})
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("segments:\n got %q\nwant %q", got, want)
	}
}

func TestParse_Segments(t *testing.T) {
	const fm = "---\nsource: \"[[@T - A]]\"\n---\n\n"
	tests := []struct {
		name       string
		body       string
		headings   []string
		preHeading bool
	}{
		{
			name:       "pre-heading text is segment 0, named by the H1",
			body:       "# 1. Ch\n\nIntro.\n\n## A\n\nA one.\n\nA two.\n",
			headings:   []string{"1. Ch", "A"},
			preHeading: true,
		},
		{
			name:     "no pre-heading text: the first section is segment 0",
			body:     "# 1. Ch\n\n## A\n\nA one.\n\n## B\n\nB one.\n",
			headings: []string{"A", "B"},
		},
		{
			name:     "an H1 after the first section is section text",
			body:     "## A\n\n# Not a title\n",
			headings: []string{"A"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := source.Parse("T", fm+tt.body)
			if err != nil {
				t.Fatal(err)
			}
			var headings []string
			for _, seg := range src.Segments {
				headings = append(headings, seg.Heading)
			}
			if !reflect.DeepEqual(headings, tt.headings) {
				t.Errorf("headings = %q, want %q", headings, tt.headings)
			}
			if src.Segments[0].PreHeading != tt.preHeading {
				t.Errorf("segment 0 PreHeading = %v, want %v", src.Segments[0].PreHeading, tt.preHeading)
			}
		})
	}
}

// A source saved with CRLF or CR line endings parses as if saved with LF:
// the same segments, and a frame with `\n` line endings.
func TestParse_LineEndings(t *testing.T) {
	const lf = "---\nsource: \"[[@T]]\"\n---\n\n# Ch\n\nIntro.\n\n## A\n\nText.\n"
	want, err := source.Parse("T", lf)
	if err != nil {
		t.Fatal(err)
	}
	for name, ending := range map[string]string{"CRLF": "\r\n", "CR": "\r"} {
		t.Run(name, func(t *testing.T) {
			got, err := source.Parse("T", strings.ReplaceAll(lf, "\n", ending))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Parse = %+v, want %+v", got, want)
			}
		})
	}
}

func TestParse_Errors(t *testing.T) {
	tests := map[string]string{
		"no source line":         "---\nchapter: x\n---\n\n## A\n\nText.\n",
		"unclosed frontmatter":   "---\nsource: \"[[@T]]\"\n\n## A\n",
		"pre-heading text no H1": "---\nsource: \"[[@T]]\"\n---\n\nIntro.\n\n## A\n\nText.\n",
		"no segments":            "---\nsource: \"[[@T]]\"\n---\n\n# Ch\n",
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := source.Parse("T", text); err == nil {
				t.Error("Parse succeeded, want an error")
			}
		})
	}
}

func TestTitle(t *testing.T) {
	tests := map[string]string{
		"3. The Stone Arch":   "The Stone Arch",
		"The Builder’s Rule":  "The Builder's Rule",
		"Load. Thrust. Span.": "Load. Thrust. Span.",
		"12. Twelve":          "Twelve",
		"1984 Revisited":      "1984 Revisited",
	}
	for heading, want := range tests {
		if got := source.Title(heading); got != want {
			t.Errorf("Title(%q) = %q, want %q", heading, got, want)
		}
	}
	if strings.ContainsRune(source.Title("‘Quoted’"), '‘') {
		t.Error("Title left a left curly apostrophe")
	}
}

// A segment's paragraphs are its non-blank lines as written, headings of any
// level left out. Segment boundaries are unchanged by sub-headings, and text
// before the H1 is segment 0's, as extract gives it to the model.
func TestParse_Paragraphs(t *testing.T) {
	src, err := source.Parse("T", "---\nsource: \"[[@T]]\"\n---\n\nBefore the H1.\n\n# Ch\n\nIntro *one*.\n\n> Quoted.\n\n## A\n\n### Sub\n\nA one.  \n\nA two.\n[^7]: An endnote.\n\n## B\n")
	if err != nil {
		t.Fatal(err)
	}
	var got [][]string
	for _, seg := range src.Segments {
		got = append(got, seg.Paragraphs)
	}
	want := [][]string{{"Before the H1.", "Intro *one*.", "> Quoted."}, {"A one.  ", "A two.", "[^7]: An endnote."}, nil}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("paragraphs = %q, want %q", got, want)
	}
}

// A source that isn't valid UTF-8 is not read: the error names the file and
// the first invalid byte.
func TestRead_RejectsInvalidUTF8(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Bad.md")
	if err := os.WriteFile(path, []byte("---\nsource: \"[[@X]]\"\n---\n## Caf\xc3\xa9\ncaf\xe9 bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := source.Read(path)
	var notUTF8 *source.NotUTF8
	if !errors.As(err, &notUTF8) {
		t.Fatalf("err = %v, want a *source.NotUTF8", err)
	}
	if notUTF8.Path != path || notUTF8.Line != 5 || notUTF8.Offset != 37 {
		t.Errorf("got path %q line %d offset %d, want %q line 5 offset 37", notUTF8.Path, notUTF8.Line, notUTF8.Offset, path)
	}
	if want := path + ": not valid UTF-8, first invalid byte on line 5 (byte offset 37)"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}

	// The line counts line endings as Parse does: `\r\n` ends one line, and
	// so does a lone `\r`, the one just before the invalid byte included.
	for _, tt := range []struct {
		name, text   string
		line, offset int
	}{
		{"lone CR", "---\rsource: \"[[@X]]\"\r---\r## Caf\xc3\xa9\rcaf\xe9 bar\r", 5, 37},
		{"CRLF", "---\r\nsource: \"[[@X]]\"\r\n---\r\n## Caf\xc3\xa9\r\ncaf\xe9 bar\r\n", 5, 41},
		{"CR before the byte", "---\r\nsource: \"[[@X]]\"\r\n---\r\n## A\r\xe9\n", 5, 33},
	} {
		if err := os.WriteFile(path, []byte(tt.text), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := source.Read(path)
		if !errors.As(err, &notUTF8) {
			t.Fatalf("%s: err = %v, want a *source.NotUTF8", tt.name, err)
		}
		if notUTF8.Line != tt.line || notUTF8.Offset != tt.offset {
			t.Errorf("%s: got line %d offset %d, want line %d offset %d", tt.name, notUTF8.Line, notUTF8.Offset, tt.line, tt.offset)
		}
	}
}
