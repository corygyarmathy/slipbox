package supportcheck_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/checker"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/supportcheck"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// notesBlock is the trialled notes block, transcribed: files maps each
// note's file name to its file's text.
func notesBlock(files map[string]string, names []string, label string) string {
	out := "=== " + label + " ===\n"
	for _, name := range names {
		out += "--- NOTE: " + name + " ---\n" + files[name] + "\n"
	}
	return out + "=== END " + label + " ===\n\n"
}

// sharedNotes is the trialled checker's notes moved to the system prompt:
// every note, the segment's own too, so it is the same for every segment.
func sharedNotes(files map[string]string) string {
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return notesBlock(files, names, "ALL SEGMENTS' NOTES (check only the notes your segment's list names; the rest are reference only)")
}

// checkPrompt is what is left of the trialled checker's user prompt: the
// names of segment n's own notes, which the shared notes give in full.
func checkPrompt(files map[string]string, n int, heading string) string {
	var mine []string
	for name := range files {
		if strings.HasPrefix(name, strconv.Itoa(n)+" ") {
			mine = append(mine, name)
		}
	}
	sort.Strings(mine)
	label := "YOUR SEGMENT'S NOTES TO CHECK (segment " + strconv.Itoa(n) + ": " + heading + ")"
	return "=== " + label + " ===\n" + strings.Join(mine, "\n") + "\n=== END " + label + " ===\n"
}

func TestPrompt_MatchesCheck(t *testing.T) {
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
	if got, want := supportcheck.Shared(notes), sharedNotes(files); got != want {
		t.Errorf("shared notes differ from the notes block of every note:\n got %q\nwant %q", got, want)
	}
	for _, seg := range src.Segments {
		got := supportcheck.Prompt(notes, seg)
		if want := checkPrompt(files, seg.Number, seg.Heading); got != want {
			t.Errorf("%s: prompt differs from the list of the segment's notes:\n got %q\nwant %q", seg, got, want)
		}
		if strings.Contains(got, "--- NOTE: ") {
			t.Errorf("%s: prompt gives a note again: %q", seg, got)
		}
	}
}

// flagLines counts the lines of report that open with a bullet and a
// bracketed non-pass grade, the shape a reader would take for a flag.
func flagLines(report string) int {
	grades := []string{"weak", "fail", "dangling", "outside-frame", "cut lost content", "reworded",
		"wrong referent", "not a name span", "cut would do", "reads wrong"}
	n := 0
	for _, line := range strings.Split(report, "\n") {
		for _, g := range grades {
			if strings.HasPrefix(strings.TrimSpace(line), "- ["+g+"] ") {
				n++
			}
		}
	}
	return n
}

// TestRun_ReplaysThePragmatismCheck runs step 1.3a on the Pragmatism run's
// stage 1 notes with its recorded Sonnet replies.
func TestRun_ReplaysThePragmatismCheck(t *testing.T) {
	src, trial, folder := testfixture.PragmatismSource(t), testfixture.PragmatismRun(t), testfixture.PragmatismFolder(t, run.ExtractDir)
	files := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.ExtractNotes)))
	recordedDir := filepath.Join(trial, run.SupportCheckDir)
	recorded := testfixture.RawReplies(t, recordedDir, len(src.Segments))
	var prompts []string
	for _, seg := range src.Segments {
		prompts = append(prompts, checkPrompt(files, seg.Number, seg.Heading))
	}
	p := testfixture.Replay(t, prompts, recorded)
	if err := supportcheck.Run(context.Background(), p, src, folder); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(folder.Dir, run.SupportCheckDir)
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
		if !reflect.DeepEqual(r, checker.Parse(recorded[seg.Number], supportcheck.Grades)) {
			t.Errorf("%s: flags file is not the parsed report", seg)
		}
		// Segment 6's reply grades two statements on one line,
		// `"It is the absolutistic brand…" / "It keeps no connexion…"`, which
		// is a flag for each.
		want := flagLines(recorded[seg.Number])
		if seg.Number == 6 {
			want++
		}
		if len(r.Flags) != want {
			t.Errorf("%s: %d flags, want one per flag line and statement (%d)", seg, len(r.Flags), want)
		}
		reports[seg.Number] = r
		counts = append(counts, len(r.Flags))
	}
	// Segments 1 and 3 have no flag; the rest have reads-alone grades, cuts
	// and rewordings.
	if want := []int{4, 0, 20, 0, 15, 7, 20, 15}; !reflect.DeepEqual(counts, want) {
		t.Errorf("flags by segment %v, want %v", counts, want)
	}

	// Segment 0's reply flags four statements, then withdraws them all
	// under "Correcting the above". The flags stand: a report's withdrawals
	// are the repair call's to read, and reads-alone grades in step 1.3a's
	// report call no repair.
	r0 := reports[0]
	var grades []string
	for _, f := range r0.Flags {
		if f.H1 != "0 What Pragmatism Means" {
			t.Errorf("segment 0 flag %.60q is under %q", f.Statement, f.H1)
		}
		grades = append(grades, fmt.Sprint(f.Grades))
	}
	if want := []string{"[outside-frame]", "[dangling]", "[dangling]", "[dangling]"}; !reflect.DeepEqual(grades, want) {
		t.Errorf("segment 0 grades %q, want %q", grades, want)
	}
	if f := r0.Flags[0]; f.Statement != "The pragmatic method is primarily a method of settling metaphysical disputes…" ||
		!strings.HasPrefix(f.Reason, `the statement uses "The pragmatic method", which the segment's own notes never introduce.`) {
		t.Errorf("segment 0's first flag = %+v", f)
	}
	if !containsLine(r0.Other, "0 What Pragmatism Means", "pass 6 / weak 0 / fail 0 / dangling 0 / outside-frame 0") {
		t.Errorf("segment 0's counts line is not kept under its note: %+v", r0.Other)
	}
	var pair []string
	for _, f := range reports[6].Flags {
		if f.Reason == `"It" is resolvable to the brand of pantheism. Pass.` {
			pair = append(pair, f.Statement)
		}
	}
	if want := []string{"It is the absolutistic brand…", "It keeps no connexion…"}; !reflect.DeepEqual(pair, want) {
		t.Errorf("segment 6's two-statement line gives %q, want %q", pair, want)
	}
	// Segment 5's reply gives a quoted statement a reworded grade.
	if !slices.ContainsFunc(reports[5].Flags, func(f checker.Flag) bool {
		return reflect.DeepEqual(f.Grades, []checker.Grade{checker.Reworded}) && strings.HasPrefix(f.Statement, "Pragmatism only follows here")
	}) {
		t.Errorf("segment 5 has no reworded flag: %+v", reports[5].Flags)
	}

	brief := testfixture.RepoFile(t, "internal/briefs/checker-support.md")
	frame := testfixture.PragmatismFrame(t)
	wantSystem := brief + "\n\n=== FRAME: " + testfixture.Pragmatism + ".md ===\n" + frame + "\n=== END FRAME ===\n\n" + sharedNotes(files)
	calls := p.Calls()
	if len(calls) != len(src.Segments) {
		t.Fatalf("%d calls, want %d", len(calls), len(src.Segments))
	}
	for _, c := range calls {
		if c.System != wantSystem {
			t.Errorf("call for %.80q: system prompt is not checker-support.md, the frame and every note", c.User)
		}
		if c.Model != "sonnet" {
			t.Errorf("call for %.80q: model %q, want sonnet", c.User, c.Model)
		}
	}
	if calls[0].User != prompts[0] {
		t.Errorf("first call was not segment 0's")
	}
}

func containsLine(lines []checker.Line, h1, text string) bool {
	for _, l := range lines {
		if l.H1 == h1 && l.Text == text {
			return true
		}
	}
	return false
}
