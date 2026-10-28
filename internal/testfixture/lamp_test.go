package testfixture

import (
	"path"
	"reflect"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/run"
)

// TestLampExtractRun checks the run folder holds the invented chapter as its
// source and LampStage1 as step 1.1's notes, byte for byte, and nothing else:
// no later step's notes.
func TestLampExtractRun(t *testing.T) {
	src, folder := LampExtractRun(t)
	if src.Name != Lamp || src.Frame != LampChapter {
		t.Errorf("source is %q, not LampChapter", src.Name)
	}
	want := map[string]string{path.Join(run.SourceDir, Lamp+".md"): LampChapter}
	for name, text := range LampStage1() {
		want[path.Join(run.ExtractNotes, name)] = text
	}
	if got := Files(t, folder.Dir); !reflect.DeepEqual(got, want) {
		t.Errorf("run folder holds\n%q\nwant\n%q", got, want)
	}
}
