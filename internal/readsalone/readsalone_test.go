package readsalone_test

import (
	"context"
	"encoding/json"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/checker"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/readsalone"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// readsAlonePrompt is the trialled checker's user prompt without the other
// segments' block, transcribed: files maps each note's file name to its
// file's text.
func readsAlonePrompt(files map[string]string, n int, heading string) string {
	var mine []string
	for name := range files {
		if strings.HasPrefix(name, strconv.Itoa(n)+" ") {
			mine = append(mine, name)
		}
	}
	sort.Strings(mine)
	label := "YOUR SEGMENT'S NOTES (segment " + strconv.Itoa(n) + ": " + heading + ")"
	out := "=== " + label + " ===\n"
	for _, name := range mine {
		out += "--- NOTE: " + name + " ---\n" + files[name] + "\n"
	}
	return out + "=== END " + label + " ===\n\n"
}

func TestPrompt_HoldsOnlyTheSegmentsNotes(t *testing.T) {
	src, folder := testfixture.PragmatismSource(t), testfixture.PragmatismFolder(t, run.ExtractDir)
	stage1 := filepath.Join(folder.Dir, filepath.FromSlash(run.ExtractNotes))
	notes, err := note.ReadDir(stage1)
	if err != nil {
		t.Fatal(err)
	}
	files := testfixture.Files(t, stage1)
	if len(notes) != len(files) || len(files) != 8 {
		t.Fatalf("read %d notes from %d files, want 8", len(notes), len(files))
	}
	for _, seg := range src.Segments {
		got := readsalone.Prompt(notes, seg)
		if want := readsAlonePrompt(files, seg.Number, seg.Heading); got != want {
			t.Errorf("%s: prompt differs from the trialled one without the other segments:\n got %q\nwant %q", seg, got, want)
		}
		if strings.Contains(got, "OTHER SEGMENTS") {
			t.Errorf("%s: prompt has an other segments' block", seg)
		}
		mine := 0
		for _, n := range notes {
			in := strings.Contains(got, "--- NOTE: "+n.Name+" ---")
			own := strings.HasPrefix(n.Name, strconv.Itoa(seg.Number)+" ")
			if in != own {
				t.Errorf("%s: note %q in the prompt: %v, want %v", seg, n.Name, in, own)
			}
			if own {
				mine++
			}
		}
		if mine == 0 {
			t.Errorf("%s: no notes of its own", seg)
		}
	}
}

// flagLines counts the lines of report that open with a bullet and a
// bracketed reads-alone grade, the shape a reader would take for a flag.
func flagLines(report string) int {
	n := 0
	for _, line := range strings.Split(report, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "- [dangling] ") || strings.HasPrefix(s, "- [outside-frame] ") {
			n++
		}
	}
	return n
}

// TestRun_ReplaysThePragmatismReadsAlone runs step 1.3b on the Pragmatism
// run's stage 1 notes with its recorded Sonnet replies.
func TestRun_ReplaysThePragmatismReadsAlone(t *testing.T) {
	src, trial, folder := testfixture.PragmatismSource(t), testfixture.PragmatismRun(t), testfixture.PragmatismFolder(t, run.ExtractDir)
	files := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.ExtractNotes)))
	recordedDir := filepath.Join(trial, run.ReadsAloneDir)
	recorded := testfixture.RawReplies(t, recordedDir, len(src.Segments))
	var prompts []string
	for _, seg := range src.Segments {
		prompts = append(prompts, readsAlonePrompt(files, seg.Number, seg.Heading))
	}
	p := testfixture.Replay(t, prompts, recorded)
	if err := readsalone.Run(context.Background(), p, src, folder); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(folder.Dir, run.ReadsAloneDir)
	testfixture.SameRequests(t, dir, recordedDir, "")

	got := testfixture.Files(t, dir)
	reports := map[int]checker.Report{}
	var counts []int
	for _, seg := range src.Segments {
		n := strconv.Itoa(seg.Number)
		if got[path.Join(run.ReportsDir, n+".md")] != recorded[seg.Number] {
			t.Errorf("%s: report is not the recorded reply", seg)
		}
		var r checker.Report
		if err := json.Unmarshal([]byte(got[path.Join(run.FlagsDir, n+".json")]), &r); err != nil {
			t.Errorf("%s: flags: %v", seg, err)
			continue
		}
		// Every recorded flag's H1 names one of its segment's notes, which
		// Run records as the flag's file.
		for i, f := range r.Flags {
			if f.File != f.H1+".md" || !strings.HasPrefix(f.File, n+" ") {
				t.Errorf("%s: flag %.60q has file %q, want its H1's note", seg, f.Statement, f.File)
			}
			r.Flags[i].File = ""
		}
		if !reflect.DeepEqual(r, checker.Parse(recorded[seg.Number], readsalone.Grades)) {
			t.Errorf("%s: flags file is not the parsed report", seg)
		}
		if want := flagLines(recorded[seg.Number]); len(r.Flags) != want {
			t.Errorf("%s: %d flags, want one per flag line (%d)", seg, len(r.Flags), want)
		}
		reports[seg.Number] = r
		counts = append(counts, len(r.Flags))
	}
	// Each flag is a dangling one; segments 0, 2 and 3 have none.
	if want := []int{0, 1, 0, 0, 1, 3, 2, 2}; !reflect.DeepEqual(counts, want) {
		t.Errorf("flags by segment %v, want %v", counts, want)
	}

	want7 := []checker.Flag{
		{
			H1:        "7 The True Is the Good in the Way of Belief",
			Grades:    []checker.Grade{checker.Dangling},
			Statement: "My belief in the Absolute, based on the good it does me…",
			Reason:    `"the Absolute": a technical term used as if already introduced. No earlier statement in the note says what it is.`,
		},
		{
			H1:        "7 The True Is the Good in the Way of Belief",
			Grades:    []checker.Grade{checker.Dangling},
			Statement: "Grant that it may be true in giving me a moral holiday.",
			Reason:    `"a moral holiday": a term used as if already understood. The note never says what a moral holiday is or how the Absolute gives one.`,
		},
	}
	if got7 := reports[7].Flags; !reflect.DeepEqual(got7, want7) {
		t.Errorf("segment 7 flags:\n got %+v\nwant %+v", got7, want7)
	}
	if l := reports[7].Other; len(l) != 1 || l[0] != (checker.Line{H1: "7 The True Is the Good in the Way of Belief", Text: "pass 46 / dangling 2 / outside-frame 0"}) {
		t.Errorf("segment 7's counts line is not kept under its note: %+v", l)
	}

	brief := testfixture.RepoFile(t, "internal/briefs/checker-reads-alone.md")
	frame := testfixture.PragmatismFrame(t)
	wantSystem := brief + "\n\n=== FRAME: " + testfixture.Pragmatism + ".md ===\n" + frame + "\n=== END FRAME ===\n"
	calls := p.Calls()
	if len(calls) != len(src.Segments) {
		t.Fatalf("%d calls, want %d", len(calls), len(src.Segments))
	}
	for _, c := range calls {
		if c.System != wantSystem {
			t.Errorf("call for %.80q: system prompt is not checker-reads-alone.md plus the frame", c.User)
		}
		if c.Model != "sonnet" {
			t.Errorf("call for %.80q: model %q, want sonnet", c.User, c.Model)
		}
	}
	if calls[0].User != prompts[0] {
		t.Errorf("first call was not segment 0's")
	}
}
