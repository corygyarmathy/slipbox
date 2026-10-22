package striptraces_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/gate"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/striptraces"
)

// Each note that can't be stripped is an item naming the note: an atomic
// note without an H1, and a footnote that is neither a trace nor the source
// footnote, with its line, each with its own sentence.
func TestReport_Items(t *testing.T) {
	folders := []split.SegmentFolder{
		{Name: "2 Two", Notes: []*note.Note{
			note.Parse("B.md", "B.[^1]\n\n[^1]: [[S#A]] \"B.\"\n[^2]: the author's aside\n"),
			note.Parse("Two.md", "1. [[B]]\n"),
		}},
	}
	splitDir := filepath.Join("/run", "stage-2")
	path := filepath.Join(splitDir, "2 Two", "B.md")
	want := []gate.Item{{
		Kind:    striptraces.NoH1,
		Problem: "This atomic note has no H1 for step 2.2 to put the source footnote after.",
		Paths:   []string{path},
	}, {
		Kind:    striptraces.OtherFootnote,
		Problem: "Footnote [^2], on line 4, is neither a trace nor the source footnote, so step 2.2 can't strip it.",
		Paths:   []string{path},
		Text:    "[^2]: the author's aside",
	}}
	if got := striptraces.Check(folders).Items(func(p striptraces.Problem) string { return filepath.Join(splitDir, p.Folder, p.Note) }); !reflect.DeepEqual(got, want) {
		t.Errorf("Items() =\n%+v\nwant\n%+v", got, want)
	}
}
