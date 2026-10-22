package counter_test

import (
	"reflect"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/counter"
)

// Minus lists keys in the order they were first counted, not by count or
// name, and leaves out a key the other counts as many or more of.
func TestMinus_FirstCountedOrder(t *testing.T) {
	var a, b counter.Counter
	for _, k := range []string{"zeta", "alpha", "zeta", "mu", "alpha", "zeta", "beta"} {
		a.Add(k)
	}
	for _, k := range []string{"beta", "alpha", "alpha", "zeta"} {
		b.Add(k)
	}
	want := []counter.Entry{{Key: "zeta", Count: 2}, {Key: "mu", Count: 1}}
	if got := a.Minus(&b); !reflect.DeepEqual(got, want) {
		t.Errorf("a.Minus(b) = %v, want %v", got, want)
	}
	if got, want := counter.Elements(want), []string{"zeta", "zeta", "mu"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Elements = %q, want %q", got, want)
	}
	if got := b.Minus(&a); len(got) != 0 || got == nil {
		t.Errorf("b.Minus(a) = %#v, want an empty list", got)
	}
	if a.Total() != 7 || b.Total() != 4 {
		t.Errorf("Total() = %d, %d, want 7, 4", a.Total(), b.Total())
	}
}
