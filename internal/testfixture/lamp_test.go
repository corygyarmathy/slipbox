package testfixture

import (
	"path"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/modelstep"
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

// TestLampFrame checks LampFrame is the frame the steps give after the brief
// in a system prompt on LampChapter, so a test that builds its expected
// system prompt from LampFrame expects what the steps send.
func TestLampFrame(t *testing.T) {
	if got, want := modelstep.SystemPrompt("BRIEF", LampSource(t), ""), "BRIEF\n\n"+LampFrame; got != want {
		t.Errorf("system prompt is\n%q\nwant\n%q", got, want)
	}
}

// TestSegmentNotes checks SegmentNotes gives every note of the segment,
// asides too, and none of another segment's, even one whose number starts
// with the segment's (10 for 1).
func TestSegmentNotes(t *testing.T) {
	files := map[string]string{
		"0 Zero.md":            "zero",
		"1 One.md":             "one",
		"1 One - Aside - A.md": "aside",
		"10 Ten.md":            "ten",
		"2 Two 1.md":           "two",
	}
	want := map[string]string{"1 One.md": "one", "1 One - Aside - A.md": "aside"}
	if got := SegmentNotes(files, 1); !reflect.DeepEqual(got, want) {
		t.Errorf("segment 1's notes are %q, want %q", got, want)
	}
	if got := SegmentNotes(files, 3); len(got) != 0 {
		t.Errorf("segment 3's notes are %q, want none", got)
	}
}
