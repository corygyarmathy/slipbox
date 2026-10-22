package gate_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/gate"
)

// A summary gives each item's problem, paths and text, then what to do
// about each kind of item once, in the order the kinds first come.
func TestSummary(t *testing.T) {
	e := &gate.Stopped{
		Gate:   "the split check",
		Report: "/run/report.txt",
		Items: []gate.Item{
			{Kind: "lost trace", Problem: "A trace is lost.", Paths: []string{"/run/a.md", "/run/1 One"}, Text: `[[S#A]] "a"`},
			{Kind: "untraced statement", Problem: "This statement has no trace.", Paths: []string{"/run/1 One/b.md"}, Text: "B."},
			{Kind: "lost trace", Problem: "Another trace is lost.", Paths: []string{"/run/c.md"}},
		},
		Fixes: map[string]string{"lost trace": "add it back", "untraced statement": "trace it"},
	}
	want := "1. A trace is lost.\n   /run/a.md\n   /run/1 One\n   > [[S#A]] \"a\"\n" +
		"2. This statement has no trace.\n   /run/1 One/b.md\n   > B.\n" +
		"3. Another trace is lost.\n   /run/c.md\n" +
		"\nWhat to do:\n- For each lost trace: add it back.\n- For each untraced statement: trace it.\n"
	if got := e.Summary(); got != want {
		t.Errorf("Summary() =\n%s\nwant\n%s", got, want)
	}
	if got := (&gate.Stopped{Fix: "Rename it"}).Summary(); got != "" {
		t.Errorf("a stop with no items has the summary %q", got)
	}
}

// Text at fault that spans lines, such as a forbidden rule with the blank
// lines around it, keeps every line under its item, trimmed of the blank
// lines at its ends.
func TestSummary_TextOnSeveralLines(t *testing.T) {
	e := &gate.Stopped{
		Items: []gate.Item{{Kind: "a", Problem: "A problem.", Paths: []string{"/run/a.md"}, Text: "\nfirst\n---\n\n\n"}},
		Fixes: map[string]string{"a": "fix a"},
	}
	want := "1. A problem.\n   /run/a.md\n   > first\n   > ---\n\nWhat to do:\n- For each a: fix a.\n"
	if got := e.Summary(); got != want {
		t.Errorf("Summary() =\n%s\nwant\n%s", got, want)
	}
}

// Past MaxItems items, a summary gives MaxItems of them, then how many more
// there are and the report's path, and the fixes of every kind, those past
// the cap included.
func TestSummary_Cap(t *testing.T) {
	e := &gate.Stopped{Report: "/run/report.txt", Fixes: map[string]string{"a": "fix a", "b": "fix b"}}
	for i := range gate.MaxItems + 3 {
		kind := "a"
		if i == gate.MaxItems+2 {
			kind = "b"
		}
		e.Items = append(e.Items, gate.Item{Kind: kind, Problem: fmt.Sprintf("Problem %d.", i+1), Paths: []string{"/run/n.md"}})
	}
	got := e.Summary()
	if !strings.Contains(got, fmt.Sprintf("%d. Problem %d.\n", gate.MaxItems, gate.MaxItems)) {
		t.Errorf("summary does not give item %d:\n%s", gate.MaxItems, got)
	}
	if strings.Contains(got, fmt.Sprintf("Problem %d.", gate.MaxItems+1)) {
		t.Errorf("summary gives an item past the cap:\n%s", got)
	}
	if want := "\n...and 3 more. The report /run/report.txt gives them all.\n"; !strings.Contains(got, want) {
		t.Errorf("summary does not say %q:\n%s", want, got)
	}
	if !strings.HasSuffix(got, "What to do:\n- For each a: fix a.\n- For each b: fix b.\n") {
		t.Errorf("summary does not give both kinds' fixes:\n%s", got)
	}
}
