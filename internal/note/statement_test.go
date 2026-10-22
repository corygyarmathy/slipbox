package note_test

import (
	"reflect"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/note"
)

func TestStatements(t *testing.T) {
	text := "---\nsource: x.[^9] Not a body line.\n---\n# Title.[^8] Not a statement.\n\n" +
		"We consider it.[^1] Dorcanes[^2] had been warned.[^3][^4]  Then a lead-in:[^5]\n" +
		"\n" +
		"1. _A label_.[^6] Its item, with a span[^7], ends.[^8]\n" +
		"- ‘Quoted.’[^9] Untraced. Two sentences.\n" +
		"## A heading.[^1] Not one.\n" +
		"No reference at the end.[^1]x and more.[^2]\n" +
		"\n" +
		"[^1]: [[F#S]] \"q\"\n"
	type stmt struct {
		Line int
		Text string
		Refs []string
	}
	want := []stmt{
		{5, "We consider it.[^1]", []string{"1"}},
		{5, "Dorcanes[^2] had been warned.[^3][^4]", []string{"2", "3", "4"}},
		{5, "Then a lead-in:[^5]", []string{"5"}},
		{7, "1. _A label_.[^6]", []string{"6"}},
		{7, "Its item, with a span[^7], ends.[^8]", []string{"7", "8"}},
		{8, "- ‘Quoted.’[^9]", []string{"9"}},
		{8, "Untraced. Two sentences.", nil},
		{10, "No reference at the end.[^1]x and more.[^2]", []string{"1", "2"}},
	}
	var got []stmt
	for _, s := range note.Parse("n.md", text).Statements() {
		got = append(got, stmt{s.Line, s.Text, s.Refs})
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Statements:\n got %+v\nwant %+v", got, want)
	}
}

func TestFileName(t *testing.T) {
	for name, want := range map[string]string{"0 A": "0 A.md", "0 A.md": "0 A.md"} {
		if got, err := note.FileName(name); err != nil || got != want {
			t.Errorf("FileName(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
	for _, name := range []string{"../escape.md", "a/b.md", `a\b`, ".", "..", "a\x00b"} {
		if _, err := note.FileName(name); err == nil {
			t.Errorf("FileName(%q) accepted it", name)
		}
	}
}
