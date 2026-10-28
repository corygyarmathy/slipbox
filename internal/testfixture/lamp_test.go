package testfixture

import (
	"path"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/splitgates"
	"github.com/corygyarmathy/slipbox/internal/striptraces"
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

// TestLampStripped checks LampStripped is what step 2.2 writes for LampRun
// once step 2.1's gates have run: every note of LampSplit's segment folders,
// stripped, and nothing else.
func TestLampStripped(t *testing.T) {
	src, folder := LampRun(t)
	if err := splitgates.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	if err := striptraces.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	want := Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.StrippedNotes)))
	if got := LampStripped(t); !reflect.DeepEqual(got, want) {
		t.Errorf("LampStripped gives\n%q\nwant what step 2.2 wrote\n%q", got, want)
	}
}
