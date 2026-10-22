package mechcheck_test

import (
	"reflect"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/mechcheck"
)

// A sentence ends at `.`, `?` or `!`, then any closing `'`, `"` or `)`, then
// whitespace, before a capital or digit, perhaps behind an opening quote or
// bracket, and not after an abbreviation such as `e.g.` or `Mr.`. The
// ranges are in code points.
func TestSplitSentences(t *testing.T) {
	tests := []struct {
		p    string
		want [][2]int
	}{
		{"One. Two? Three! four. Five", [][2]int{{0, 5}, {5, 10}, {10, 23}, {23, 27}}},
		{"He said 'Stop.' Then (Later) it ended.) 'Quoted' next.", [][2]int{{0, 16}, {16, 40}, {40, 54}}},
		{"See e.g. Mars and i.e. This. Mr. Smith met Dr. Who vs. Them.", [][2]int{{0, 29}, {29, 60}}},
		{"Mrs. Jones. Whimrs. Next. ēSt. Next.", [][2]int{{0, 12}, {12, 20}, {20, 31}, {31, 36}}},
		{"Charm. 158c–59a is cited. 5a–b. Done", [][2]int{{0, 7}, {7, 26}, {26, 32}, {32, 36}}},
		{"End.   Spaces. 'A", [][2]int{{0, 7}, {7, 15}, {15, 17}}},
		{"Trailing. ", [][2]int{{0, 10}}},
		{"", nil},
		{"?! A. (B", [][2]int{{0, 3}, {3, 6}, {6, 8}}},
		{"Euthyph. 5a–b, where. Ἀ Greek. Ēnd. 9 lives.", [][2]int{{0, 9}, {9, 36}, {36, 44}}},
	}
	for _, tt := range tests {
		got := mechcheck.SplitSentences(tt.p)
		if len(got) == 0 && len(tt.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitSentences(%q) = %v, want %v", tt.p, got, tt.want)
		}
	}
}
