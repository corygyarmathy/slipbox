package textrule_test

import (
	"regexp"
	"testing"
	"unicode"

	"github.com/corygyarmathy/slipbox/internal/textrule"
)

func TestHeadTail(t *testing.T) {
	s := "‘aē’b"
	for n, want := range map[int]string{0: "", 1: "‘", 3: "‘aē", 5: s, 9: s} {
		if got := textrule.Head(s, n); got != want {
			t.Errorf("Head(%q, %d) = %q, want %q", s, n, got, want)
		}
	}
	for n, want := range map[int]string{0: "", 1: "b", 3: "ē’b", 5: s, 9: s} {
		if got := textrule.Tail(s, n); got != want {
			t.Errorf("Tail(%q, %d) = %q, want %q", s, n, got, want)
		}
	}
}

// Space matches what unicode.IsSpace is true of, over every code point.
func TestSpace(t *testing.T) {
	space := regexp.MustCompile(`^[` + textrule.Space + `]$`)
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		if got, want := space.MatchString(string(r)), unicode.IsSpace(r); got != want {
			t.Errorf("Space matches %U: %v, unicode.IsSpace: %v", r, got, want)
		}
	}
}

// IsWord is true of what Word matches, over every code point, and IsAlnum of
// the same less the underscore.
func TestIsWord(t *testing.T) {
	word := regexp.MustCompile(`^[` + textrule.Word + `]$`)
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		if got, want := textrule.IsWord(r), word.MatchString(string(r)); got != want {
			t.Errorf("IsWord(%U) = %v, Word matches it: %v", r, got, want)
		}
		if got, want := textrule.IsAlnum(r), textrule.IsWord(r) && r != '_'; got != want {
			t.Errorf("IsAlnum(%U) = %v, want %v", r, got, want)
		}
	}
}

func TestClasses(t *testing.T) {
	if !regexp.MustCompile(`^` + textrule.Digit + `$`).MatchString("٣") {
		t.Error("Digit does not match an Arabic-Indic digit")
	}
	word := regexp.MustCompile(`^[` + textrule.Word + `]$`)
	for r, want := range map[rune]bool{'a': true, 'ē': true, '_': true, '٣': true, '½': true, '.': false, ' ': false, '’': false} {
		if word.MatchString(string(r)) != want {
			t.Errorf("Word matches %q: %v, want %v", r, !want, want)
		}
	}
}
