package note_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/note"
)

// roundTrips are invented notes in each shape the stages write: a stage 1
// note with lists and a trace into the `source:` line, one saved with CRLF
// and frontmatter, one whose trace sits between two `---` lines at its top,
// stage 2 and 3 notes with callouts, a blockquote, links and tags, and lines
// the note model doesn't end, run together with characters some editors
// take for line breaks.
var roundTrips = map[string]string{
	"1 The Builder's Rule.md": "# 1 The Builder's Rule\n\n" +
		"Each stone leans on the next.[^1] The rule is old.[^2]   \n" +
		"J. Doe[^3] names three parts:\n" +
		"- the keystone[^4]\n" +
		"1. *Voussoirs*, the wedges.[^5]\n\n" +
		"[^1]: [[Bridges - Ch 3#The Builder’s Rule]] \"Each stone leans on the next.\"\n" +
		"[^2]: [[Bridges - Ch 3#The Builder's Rule]] \"The rule is old.\"\n" +
		"[^3]: [[Bridges - Ch 3]] \"J. Doe\"\n" +
		"[^4]: [[Bridges - Ch 3#The Builder's Rule]] \"the keystone\"\n" +
		"[^5]: [[Bridges - Ch 3#The Builder's Rule]] \"*voussoirs*, the wedges\"\n" +
		"[^4]: [[Bridges - Ch 3#Arches]] \"the keystone\"\n",
	"2 Arches.md":                    "---\r\ntags: [trial]\r\n---\r\n# 2 Arches\r\n\r\nAn arch stands by its shape.[^1]\r\n\r\n[^1]: [[Bridges - Ch 3#Arches]] \"An arch stands by its shape\"\r\n",
	"3 Load.md":                      "---\nA load becomes thrust.[^1]\n\n[^1]: [[Bridges - Ch 3#Load]] \"A load becomes thrust.\"\n---\n",
	"2 Arches - Aside - Centring.md": "# 2 Arches - Aside - Centring\n\nNo final newline.[^1]\n\n[^1]: [[Bridges - Ch 3#Arches]] \"No final newline.\"",
	"Keystone.md": "---\nsource: \"[[@A Short History of Bridges - J. Doe]]\"\ntags:\n  - bridges\n---\n# Keystone\n\n" +
		"> [!note] The last stone\n> It locks the ring.\n\n" +
		"> [!quote]- Folded\n> “Stone works best in compression.”\n\n" +
		"See [[Arch]] and [[Beam|beams]], #engineering.\n\n" +
		"[^1]: [[@A Short History of Bridges - J. Doe]]\n",
	"Arches.md":    "# Arches\n\n1. [[Keystone]]\n2. [[Abutment]]\n",
	"Separated.md": "# Heading\vrun on\n\nA\fB\x1cC\u0085D\u2028E\u2029F\rlone CR\r\n",
	"Empty.md":     "",
}

// A note renders back to the file it was read from, byte for byte, and is
// named for the file.
func TestRead_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	for name, text := range roundTrips {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		n, err := note.Read(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := n.Render(); got != text {
			t.Errorf("%s: Render = %q, want the file %q", name, got, text)
		}
		if n.Name != name {
			t.Errorf("%s: Name = %q", name, n.Name)
		}
	}
}

func TestParse_RoundTripsLineEndings(t *testing.T) {
	for _, text := range []string{
		"",
		"\n",
		"one",
		"one\ntwo\n",
		"one\r\ntwo\r\n",
		"one\rtwo\r",
		"mixed\r\n\r\rlast",
		"\n\n",
	} {
		if got := note.Parse("n.md", text).Render(); got != text {
			t.Errorf("Parse(%q).Render() = %q", text, got)
		}
	}
}

func TestParse_Parts(t *testing.T) {
	text := "---\r\nsource: x\r\n[^3]: [[G#S]] \"in frontmatter\"\r\n---\r\n# Title[^9]\r\n\r\nA statement.[^1] And [^2][^1]\r\n# Not the H1\r\n\r\n[^1]: [[F#Sec]] \"a quote\"\r\n[^2]: [[@Citekey]]\r\n[^x]: not a definition\r\n"
	n := note.Parse("n.md", text)
	type line struct {
		Text string
		Kind note.Kind
		Refs []string
		Def  *note.Def
	}
	want := []line{
		{"---", note.Frontmatter, nil, nil},
		{"source: x", note.Frontmatter, nil, nil},
		{`[^3]: [[G#S]] "in frontmatter"`, note.Frontmatter, nil, &note.Def{Label: "3", Value: `[[G#S]] "in frontmatter"`}},
		{"---", note.Frontmatter, nil, nil},
		{"# Title[^9]", note.H1, []string{"9"}, nil},
		{"", note.Body, nil, nil},
		{"A statement.[^1] And [^2][^1]", note.Body, []string{"1", "2", "1"}, nil},
		{"# Not the H1", note.Body, nil, nil},
		{"", note.Body, nil, nil},
		{`[^1]: [[F#Sec]] "a quote"`, note.Definition, nil, &note.Def{Label: "1", Value: `[[F#Sec]] "a quote"`}},
		{"[^2]: [[@Citekey]]", note.Definition, nil, &note.Def{Label: "2", Value: "[[@Citekey]]"}},
		{"[^x]: not a definition", note.Body, nil, nil},
	}
	var got []line
	for _, l := range n.Lines {
		if l.EOL != "\r\n" {
			t.Errorf("line %q: EOL %q", l.Text, l.EOL)
		}
		got = append(got, line{l.Text, l.Kind, l.Refs, l.Def})
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("lines:\n got %+v\nwant %+v", got, want)
	}
	if defs := n.Defs(); len(defs) != 2 || defs[0].Label != "1" || defs[1].Label != "2" {
		t.Errorf("Defs() = %+v", defs)
	}
}

// A frontmatter's closing `---` may end the file with no newline after it.
func TestParse_FrontmatterClosedAtTheEndOfTheFile(t *testing.T) {
	n := note.Parse("n.md", "---\nx: y\n---")
	for i, l := range n.Lines {
		if l.Kind != note.Frontmatter {
			t.Errorf("line %d %q is %v, want frontmatter", i, l.Text, l.Kind)
		}
	}
}

func TestParse_UnclosedFrontmatterIsBody(t *testing.T) {
	n := note.Parse("n.md", "---\n# Title\n")
	if n.Lines[0].Kind != note.Body || n.Lines[1].Kind != note.H1 {
		t.Errorf("kinds = %v, %v", n.Lines[0].Kind, n.Lines[1].Kind)
	}
}

func TestDef_Trace(t *testing.T) {
	tests := []struct {
		value string
		want  note.Trace
		ok    bool
	}{
		{`[[S - Ch 3#Arches]] "a quote"`, note.Trace{File: "S - Ch 3", Section: "Arches", Quote: "a quote"}, true},
		{`[[S - Ch 3]] "J. Doe"`, note.Trace{File: "S - Ch 3", Quote: "J. Doe"}, true},
		{`[[S#Sec]] "with "inner" quotes"`, note.Trace{File: "S", Section: "Sec", Quote: `with "inner" quotes`}, true},
		{`[[S#Sec]] ""`, note.Trace{File: "S", Section: "Sec"}, true},
		{`[[S#Sec|alias]] "q"`, note.Trace{}, false},
		{`[[S#Sec]] "q" trailing`, note.Trace{}, false},
		{`[[S#Sec]]`, note.Trace{}, false},
		{`a note to self`, note.Trace{}, false},
	}
	for _, tt := range tests {
		got, ok := note.Def{Label: "1", Value: tt.value}.Trace()
		if got != tt.want || ok != tt.ok {
			t.Errorf("Trace() of %q = %+v, %v; want %+v, %v", tt.value, got, ok, tt.want, tt.ok)
		}
	}
}

func TestDef_SourceFootnote(t *testing.T) {
	if got, ok := (note.Def{Value: "[[@A Short History of Bridges - J. Doe]]"}).SourceFootnote(); !ok || got != "[[@A Short History of Bridges - J. Doe]]" {
		t.Errorf("SourceFootnote() = %q, %v", got, ok)
	}
	for _, v := range []string{`[[S#Sec]] "q"`, "[[a]] [[b]]", "text"} {
		if _, ok := (note.Def{Value: v}).SourceFootnote(); ok {
			t.Errorf("SourceFootnote() of %q is true", v)
		}
	}
}

// A dotfile, such as macOS's `._2 B.md`, is not a note: it is hidden.
func TestReadDir_SortsByNameSkippingDotfiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"2 B.md", "2 B - Aside - x.md", "10 Z.md", "notes.txt", "._2 B.md", ".md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	notes, err := note.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, n := range notes {
		names = append(names, n.Name)
	}
	if want := []string{"10 Z.md", "2 B - Aside - x.md", "2 B.md"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names = %q, want %q", names, want)
	}
}

// A note that isn't valid UTF-8 is not read: the error names the file and
// the first invalid byte. A valid non-ASCII note reads as written.
func TestRead_RejectsInvalidUTF8(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(bad, []byte("# Caf\xc3\xa9\ncaf\xe9 au lait\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := note.Read(bad)
	var notUTF8 *note.NotUTF8
	if !errors.As(err, &notUTF8) {
		t.Fatalf("err = %v, want a *note.NotUTF8", err)
	}
	if notUTF8.Path != bad || notUTF8.Line != 2 || notUTF8.Offset != 11 {
		t.Errorf("got path %q line %d offset %d, want %q line 2 offset 11", notUTF8.Path, notUTF8.Line, notUTF8.Offset, bad)
	}
	if want := bad + ": not valid UTF-8, first invalid byte on line 2 (byte offset 11)"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
	if _, err := note.ReadDir(dir); !errors.As(err, &notUTF8) {
		t.Errorf("ReadDir err = %v, want a *note.NotUTF8", err)
	}

	// The line counts line endings as Parse does: `\r\n` ends one line, and
	// so does a lone `\r`, the one just before the invalid byte included.
	for _, tt := range []struct {
		name, text   string
		line, offset int
	}{
		{"lone CR", "# Caf\xc3\xa9\ra\rb\rc\rcaf\xe9 au lait\r", 5, 17},
		{"CRLF", "# Caf\xc3\xa9\r\nok\r\ncaf\xe9 au lait\r\n", 3, 16},
		{"CR before the byte", "# A\r\nb\r\xe9\n", 3, 7},
	} {
		path := filepath.Join(t.TempDir(), "bad.md")
		if err := os.WriteFile(path, []byte(tt.text), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := note.Read(path)
		if !errors.As(err, &notUTF8) {
			t.Fatalf("%s: err = %v, want a *note.NotUTF8", tt.name, err)
		}
		if notUTF8.Line != tt.line || notUTF8.Offset != tt.offset {
			t.Errorf("%s: got line %d offset %d, want line %d offset %d", tt.name, notUTF8.Line, notUTF8.Offset, tt.line, tt.offset)
		}
	}

	good := filepath.Join(dir, "good.md")
	text := "# Caf\u00e9\ncaf\u00e9 au lait\n"
	if err := os.WriteFile(good, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := note.Read(good)
	if err != nil {
		t.Fatal(err)
	}
	if got := n.Render(); got != text {
		t.Errorf("Render() = %q, want %q", got, text)
	}
}

func TestMatchForm(t *testing.T) {
	tests := map[string]string{
		"  *Apology*,\tso_on\\ [^12] ’a‘ “b”\u00a0c  ": `Apology, soon 'a' "b" c`,
		"line\nbreak\u2003em\u3000space":               "line break em space",
		"":                                             "",
	}
	for in, want := range tests {
		if got := note.MatchForm(in); got != want {
			t.Errorf("MatchForm(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCollapseSpace(t *testing.T) {
	if got, want := note.CollapseSpace("  *a*\t b\n\u2003c\u00a0d  "), "*a* b c d"; got != want {
		t.Errorf("CollapseSpace = %q, want %q", got, want)
	}
}

// FootnoteMarks counts every reference, in the frontmatter and in a
// definition's value too, and a definition's label as one.
func TestFootnoteMarks(t *testing.T) {
	n := note.Parse("Hub.md", "---\nx: [^7]\n---\n# Hub[^1]\n\n1. [[A]] [^2][^3]\n\n[^1]: see [^4]\n")
	if got, want := n.FootnoteMarks(), 6; got != want {
		t.Errorf("FootnoteMarks = %d, want %d", got, want)
	}
}

// SetH1 replaces the H1 in place, or puts one after any frontmatter.
func TestSetH1(t *testing.T) {
	tests := map[string]struct{ text, want string }{
		"replaced":                              {"# 4 A\r\n\nText.\n", "# 0 A\r\n\nText.\n"},
		"replaced after frontmatter":            {"---\nx: y\n---\n# 4 A\nText.\n", "---\nx: y\n---\n# 0 A\nText.\n"},
		"only the first":                        {"# 4 A\n# 4 B\n", "# 0 A\n# 4 B\n"},
		"inserted":                              {"Text.\n", "# 0 A\nText.\n"},
		"inserted, ending as the next line":     {"Text.\r\n", "# 0 A\r\nText.\r\n"},
		"inserted before an unended line":       {"Text.", "# 0 A\nText."},
		"inserted after frontmatter":            {"---\nx: y\n---\nText.\n", "---\nx: y\n---\n# 0 A\nText.\n"},
		"inserted after an unended frontmatter": {"---\nx: y\n---", "---\nx: y\n---\n# 0 A"},
		"into an empty note":                    {"", "# 0 A"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			n := note.Parse("0 A.md", tt.text)
			n.SetH1("0 A")
			if got := n.Render(); got != tt.want {
				t.Errorf("Render = %q, want %q", got, tt.want)
			}
			if got := note.Parse(n.Name, n.Render()); !reflect.DeepEqual(got, n) {
				t.Errorf("note does not parse back as set:\n got %+v\nwant %+v", got, n)
			}
		})
	}
}

func TestStripRefs(t *testing.T) {
	if got, want := note.StripRefs("a [^1] b.[^2]\t[^3] [^x]"), "a b. [^x]"; got != want {
		t.Errorf("StripRefs = %q, want %q", got, want)
	}
}

func TestDropRefs(t *testing.T) {
	if got, want := note.DropRefs("a [^1] b.[^2]\t[^3] [^x]"), "a  b.\t [^x]"; got != want {
		t.Errorf("DropRefs = %q, want %q", got, want)
	}
}

// MatchFormOffsets gives MatchForm's result on every line of the invented
// notes and on lines in each form a source or note writes, and each range it
// gives normalises back to the text it was made from.
func TestMatchFormOffsets(t *testing.T) {
	lines := []string{
		"  *Apology*,\tso_on\\ [^12] ’a‘ “b”\u00a0c  ",
		"It is *so*, he said (“so!”).",
		"_‘Stonework’_ is \\*not\\* emphasis, nor is snake_case_word.",
		"Two refs.[^1][^2] A spaced one [^3] and [^x] not one.",
		"The *pōise* of a bridge; e\u0301 combined — and … elided.",
		"> A blockquote line",
		"- a list item[^4]",
		"1. A numbered one.[^5]",
		"em\u2003space\u3000ideographic\u00a0no-break",
		"a line ending in CR\r",
		"***",
		"   ",
		"",
		"a \xff b",
		"\ufffd x",
	}
	for _, text := range roundTrips {
		lines = append(lines, strings.Split(text, "\n")...)
	}
	for _, s := range lines {
		norm, at := note.MatchFormOffsets(s)
		if want := note.MatchForm(s); norm != want {
			t.Errorf("MatchFormOffsets(%q) = %q, want %q", s, norm, want)
			continue
		}
		if len(at) != len(norm) {
			t.Fatalf("MatchFormOffsets(%q): %d offsets for %d bytes", s, len(at), len(norm))
		}
		// Every word's range normalises to the word.
		for _, w := range wordRanges(norm) {
			if got := note.MatchForm(s[at[w[0]][0]:at[w[1]-1][1]]); got != norm[w[0]:w[1]] {
				t.Errorf("%q: the range of %q normalises to %q", s, norm[w[0]:w[1]], got)
			}
		}
	}
}

// wordRanges gives the byte ranges of the space-separated words of s.
func wordRanges(s string) [][2]int {
	var out [][2]int
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ' ' {
			if i > start {
				out = append(out, [2]int{start, i})
			}
			start = i + 1
		}
	}
	return out
}
