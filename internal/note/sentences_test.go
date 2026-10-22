package note_test

import (
	"reflect"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/note"
)

// A sentence ends at `.`, `?`, `!` or `:` before an ASCII capital or one of
// `‘'"(_*`, and not before a `“` or a lower-case letter. An abbreviation
// such as `St.` before a capital ends one too.
func TestSplitSentences(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"One. Two? three! (Four) x: _Five_ “Six” ‘Seven’ 'Eight' \"Nine\" *Ten* St. Ives",
			[]string{"One.", "Two? three!", "(Four) x:", "_Five_ “Six” ‘Seven’ 'Eight' \"Nine\" *Ten* St.", "Ives"}},
		{"", nil},
		{"  A. B  ", []string{"A.", "B"}},
		{"End. Next", []string{"End.", "Next"}},
	}
	for _, tt := range tests {
		if got := note.SplitSentences(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitSentences(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The frontmatter, headings and definitions are dropped, list markers go
// even across a blank line, and a sentence runs across lines. A quote runs
// from the value's first `"` to its last, and a value without one has none.
func TestQuotedTextsAndBodySentences(t *testing.T) {
	n := note.Parse("A.md", "---\na: 1\n---\n# H\n\n- One.[^1]\n\n  2. Two\nthree.[^2] Four\n#tag line\n[^1]: [[S#X]] \"a \"quoted\" b\"\n[^2]: no quote\n")
	if got, want := n.QuotedTexts(), []string{`a "quoted" b`}; !reflect.DeepEqual(got, want) {
		t.Errorf("QuotedTexts() = %q, want %q", got, want)
	}
	if got, want := n.BodySentences(), []string{"One.", "Two three.", "Four"}; !reflect.DeepEqual(got, want) {
		t.Errorf("BodySentences() = %q, want %q", got, want)
	}
}

// A list marker on the last line goes too, its line ending being the
// whitespace after it. Without a line ending it is just text.
func TestBodySentences_LastLineMarker(t *testing.T) {
	for _, text := range []string{"Alpha holds.\n-\n", "Alpha holds.\r\n1.\r\n"} {
		if got, want := note.Parse("A.md", text).BodySentences(), []string{"Alpha holds."}; !reflect.DeepEqual(got, want) {
			t.Errorf("BodySentences() of %q = %q, want %q", text, got, want)
		}
	}
	if got, want := note.Parse("A.md", "Alpha holds.\n-").BodySentences(), []string{"Alpha holds. -"}; !reflect.DeepEqual(got, want) {
		t.Errorf("BodySentences() without a last line ending = %q, want %q", got, want)
	}
}
