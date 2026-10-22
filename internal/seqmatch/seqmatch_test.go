package seqmatch_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/seqmatch"
)

// The FUZZY threshold (mechcheck.FuzzyThreshold, 0.90) was calibrated on
// Ratio's numbers, so these are pinned exactly. Each case is kept for one way
// the ratio could drift: an empty side, comparing bytes rather than code
// points, breaking a tie the other way, a long string with many repeats, and
// values near the threshold, where a ratio decides a trace. Every want is
// written as Go prints the float64, which reads back as the same value, so
// equality is the test.
func TestRatio(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want float64
	}{
		{"both empty", "", "", 1.0},
		{"a empty", "", "lantern", 0.0},
		{"b empty", "lantern", "", 0.0},
		{"identical", "the lantern hung over the harbour", "the lantern hung over the harbour", 1.0},
		{"no common characters", "xyz", "lantern", 0.0},
		{"no common characters, non-ASCII", "‘’", "''", 0.0},
		// ‘ and ’ share two of their three UTF-8 bytes, so comparing bytes
		// gives a ratio above 0.
		{"curly quotes share UTF-8 bytes", "‘", "’", 0.0},
		{"accented letters", "café naïve résumé", "cafe naive resume", 0.7647058823529411},
		{"repeated characters", "aaaa", "aa", 0.6666666666666666},
		// Either a in a matches the a in b. The rule takes the one that
		// starts first in a, which leaves b to match after it. Taking the
		// other would leave nothing, and give 0.3333333333333333.
		{"tie: equal blocks either way", "aba", "acb", 0.6666666666666666},
		{"just below the FUZZY threshold", "the old lantern hung low", "the old lanterns swung lower", 0.8846153846153846},
		{"exactly the FUZZY threshold", "abcdefghij", "abcdefghiX", 0.9},
		{"just above the FUZZY threshold", "the lantern hung low", "the lanterns swung low", 0.9047619047619048},
		// Every character of b is common in it, and every one can start a
		// match.
		{"long, with many repeats", strings.Repeat("lantern quay ", 16), strings.Repeat("lantern quay ", 15) + "harbour", 0.9658536585365853},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := seqmatch.Ratio(tt.a, tt.b); got != tt.want {
				t.Errorf("Ratio(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestChanges(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want []string
	}{
		{"both empty", nil, nil, nil},
		{"identical", []string{"the", "red", "lantern"}, []string{"the", "red", "lantern"}, nil},
		{"a empty", nil, []string{"red", "lantern"}, []string{"+ red", "+ lantern"}},
		{"b empty", []string{"red", "lantern"}, nil, []string{"- red", "- lantern"}},
		{"one inserted", []string{"the", "lantern", "swung"}, []string{"the", "red", "lantern", "swung"}, []string{"+ red"}},
		{"one deleted", []string{"the", "red", "lantern", "swung"}, []string{"the", "lantern", "swung"}, []string{"- red"}},
		{"one replaced", []string{"the", "red", "lantern", "swung"}, []string{"the", "green", "lantern", "swung"}, []string{"- red", "+ green"}},
		{
			"replaced hunk: all its - lines, then all its + lines",
			[]string{"the", "cat", "sat", "on", "mat", "today"},
			[]string{"the", "cats", "sit", "upon", "mats", "today"},
			[]string{"- cat", "- sat", "- on", "- mat", "+ cats", "+ sit", "+ upon", "+ mats"},
		},
		{
			"replaced hunk, b's side longer",
			[]string{"x", "delta", "y"},
			[]string{"x", "alpha", "beta", "gamma", "y"},
			[]string{"- delta", "+ alpha", "+ beta", "+ gamma"},
		},
		{
			"separate hunks, in order",
			[]string{"a", "b", "c", "d", "e", "f", "g"},
			[]string{"a", "B", "c", "d", "x", "e", "g"},
			[]string{"- b", "+ B", "+ x", "- f"},
		},
		{"changes at both ends", []string{"a", "b", "c"}, []string{"x", "b", "y"}, []string{"- a", "+ x", "- c", "+ y"}},
		// x or y could be kept: a's line is removed first, so y is.
		{"tie between alignments", []string{"x", "y"}, []string{"y", "x"}, []string{"- x", "+ x"}},
		// The walk keeps the first a it can, not the last.
		{"tie on a repeated line", []string{"a"}, []string{"b", "a", "b", "a", "c", "a"}, []string{"+ b", "+ b", "+ a", "+ c", "+ a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := seqmatch.Changes(tt.a, tt.b); !slices.Equal(got, tt.want) {
				t.Errorf("Changes(%q, %q) =\n%q\nwant\n%q", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
