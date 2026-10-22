package lossproposer_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/dropped"
	"github.com/corygyarmathy/slipbox/internal/lossproposer"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/modelstep"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// pragmatismFolder makes a run folder whose repaired notes are the
// Pragmatism run's, 1.4-repair/stage-1, and runs step 1.2's re-check on them,
// which writes the uncovered lists its proposer runs read.
func pragmatismFolder(t *testing.T, src *source.Source) *run.Folder {
	t.Helper()
	folder := testfixture.PragmatismFolder(t, run.RepairDir)
	if err := mechcheck.RunOn(src, folder, run.RepairNotes, run.MechanicalRecheckDir); err != nil {
		t.Fatal(err)
	}
	return folder
}

// recorded gives each segment's prompt, twice, with the Pragmatism run's two
// recorded Sonnet runs' replies, for the segments in segs.
func recorded(t *testing.T, src *source.Source, folder *run.Folder, segs []int) (prompts, replies []string) {
	t.Helper()
	lists := testfixture.Files(t, filepath.Join(folder.Dir, run.MechanicalRecheckDir, run.UncoveredDir))
	dir := filepath.Join(testfixture.PragmatismRun(t), run.LossProposerDir)
	runs := []map[string]string{
		testfixture.Files(t, filepath.Join(dir, modelstep.RunDir(1), run.RawDir)),
		testfixture.Files(t, filepath.Join(dir, modelstep.RunDir(2), run.RawDir)),
	}
	for _, n := range segs {
		p := lossproposer.Prompt(src.Segments[n], lists[strconv.Itoa(n)+".txt"])
		for _, r := range runs {
			prompts, replies = append(prompts, p), append(replies, r[strconv.Itoa(n)+".txt"])
		}
	}
	return prompts, replies
}

func readLosses(t *testing.T, folder *run.Folder) []lossproposer.Segment {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(folder.Dir, run.LossProposerDir, run.ProposedLossesFile))
	if err != nil {
		t.Fatal(err)
	}
	var segs []lossproposer.Segment
	if err := json.Unmarshal(b, &segs); err != nil {
		t.Fatal(err)
	}
	return segs
}

// With the Pragmatism run's two recorded Sonnet runs, the union holds every
// item from either run, and items sharing a quote appear once with both
// wordings. Each call is the request the run made.
func TestRun_TheTwoRecordedRuns(t *testing.T) {
	src := testfixture.PragmatismSource(t)
	folder := pragmatismFolder(t, src)
	all := []int{0, 1, 2, 3, 4, 5, 6, 7}
	prompts, replies := recorded(t, src, folder, all)
	p := testfixture.Replay(t, prompts, replies)
	if err := lossproposer.Run(context.Background(), p, src, lossproposer.Model, folder); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(folder.Dir, run.LossProposerDir)
	for k := 1; k <= lossproposer.Runs; k++ {
		testfixture.SameRequests(t, dir, filepath.Join(testfixture.PragmatismRun(t), run.LossProposerDir), modelstep.RunDir(k))
	}

	segs := readLosses(t, folder)
	var got []int
	for _, s := range segs {
		got = append(got, s.Segment)
	}
	if !reflect.DeepEqual(got, all) {
		t.Fatalf("proposed losses for segments %v, want %v", got, all)
	}

	// Every item from either run is in the union, as the reply wrote it.
	for i, r := range replies {
		n, k := all[i/2], i%2+1
		want := lossproposer.Parse(r).Items
		var have []string
		for _, l := range segs[n].Losses {
			for _, w := range l.Wordings {
				if w.Run == k {
					have = append(have, w.Text)
				}
			}
		}
		if len(have) != len(want) {
			t.Errorf("segment %d run %d: %d items in the union, want %d", n, k, len(have), len(want))
		}
		for _, it := range want {
			if !slices.Contains(have, it.Text) {
				t.Errorf("segment %d run %d: item %q is not in the union", n, k, it.Idea)
			}
		}
	}

	// Segment 0: each run proposes the squirrel dispute's distinction,
	// quoting it from a different start, and the two are one loss. No other
	// segment has a loss: each run replies "No losses proposed.".
	for _, s := range segs {
		var have []string
		for _, l := range s.Losses {
			var runs []string
			for _, w := range l.Wordings {
				runs = append(runs, strconv.Itoa(w.Run))
			}
			have = append(have, strings.Join(runs, "+"))
		}
		var want []string
		other := []lossproposer.Line{{Run: 1, Text: "No losses proposed."}, {Run: 2, Text: "No losses proposed."}}
		if s.Segment == 0 {
			want, other = []string{"1+2"}, []lossproposer.Line{}
		}
		if !reflect.DeepEqual(have, want) {
			t.Errorf("segment %d losses by run = %q, want %q", s.Segment, have, want)
		}
		if !reflect.DeepEqual(s.Other, other) {
			t.Errorf("segment %d's other lines = %+v, want %+v", s.Segment, s.Other, other)
		}
	}
	if w := segs[0].Losses[0].Wordings; !strings.HasPrefix(w[0].Quotes[0], "Mindful of the scholastic adage") || !strings.HasPrefix(w[1].Quotes[0], "Which party is right,") {
		t.Errorf("segment 0's merged loss is not the squirrel dispute's distinction: %+v", w)
	}
	// No item in either run is a remark, so the step dropped nothing.
	if d := readDropped(t, folder); d != "[]\n" {
		t.Errorf("dropped = %q, want an empty list", d)
	}

	// Both calls per segment carry identical prompts, and the second run
	// starts once the first is done, so its calls find them cached.
	calls := p.Calls()
	if len(calls) != len(prompts) {
		t.Fatalf("%d calls, want %d", len(calls), len(prompts))
	}
	users := func(cs []provider.Request) []string {
		var out []string
		for _, c := range cs {
			out = append(out, c.User)
		}
		slices.Sort(out)
		return out
	}
	half := len(calls) / 2
	if first, second := users(calls[:half]), users(calls[half:]); !reflect.DeepEqual(first, second) || len(slices.Compact(first)) != half {
		t.Error("the second run's calls are not the first's prompts, each once, made after the first run")
	}
	for i, c := range calls {
		if c.System != calls[0].System || c.Model != lossproposer.Model {
			t.Errorf("call %d: system prompt or model %q differs", i, c.Model)
		}
	}
	// Every call's system prompt is the brief, the frame and all the
	// repaired notes, so the calls after the first read the notes from cache.
	notes, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(run.RepairNotes)))
	if err != nil {
		t.Fatal(err)
	}
	brief := testfixture.RepoFile(t, "internal/briefs/proposer.md")
	frame := testfixture.PragmatismFrame(t)
	wantSystem := brief + "\n\n=== FRAME: " + testfixture.Pragmatism + ".md ===\n" + frame + "\n=== END FRAME ===\n\n" + lossproposer.Shared(notes)
	if calls[0].System != wantSystem {
		t.Error("system prompt is not the proposer brief, the frame and all the notes")
	}
	if !strings.HasPrefix(lossproposer.Shared(notes), "=== ALL STAGE 1 NOTES FOR THE FRAME ===\n--- NOTE: "+notes[0].Name+" ---\n") {
		t.Error("shared block does not open with the notes block and the first note")
	}

	files := testfixture.Files(t, dir)
	for i, r := range replies {
		name := modelstep.RunDir(i%2+1) + "/" + run.RawDir + "/" + strconv.Itoa(all[i/2]) + ".txt"
		if files[name] != r {
			t.Errorf("%s is not the recorded reply", name)
		}
	}
	if n := strings.Count(files[run.UsageFile], "\n"); n != len(prompts) {
		t.Errorf("usage log has %d lines, want one per call (%d)", n, len(prompts))
	}
}

// Run on another model asks for it in both runs' calls, and each line of
// the usage log records it. The prompts and the union don't change.
func TestRun_AnotherModel(t *testing.T) {
	src := testfixture.PragmatismSource(t)
	folder := pragmatismFolder(t, src)
	all := []int{0, 1, 2, 3, 4, 5, 6, 7}
	prompts, replies := recorded(t, src, folder, all)
	p := testfixture.Replay(t, prompts, replies)
	if err := lossproposer.Run(context.Background(), p, src, "opus", folder); err != nil {
		t.Fatal(err)
	}
	calls := p.Calls()
	if len(calls) != len(prompts) {
		t.Fatalf("%d calls, want %d", len(calls), len(prompts))
	}
	for i, c := range calls {
		if c.Model != "opus" || c.Effort != lossproposer.Effort {
			t.Errorf("call %d: model %q at %q, want opus at %q", i, c.Model, c.Effort, lossproposer.Effort)
		}
	}
	usage := testfixture.Files(t, filepath.Join(folder.Dir, run.LossProposerDir))[run.UsageFile]
	lines := strings.Split(strings.TrimSuffix(usage, "\n"), "\n")
	if len(lines) != len(prompts) {
		t.Fatalf("usage log has %d lines, want one per call (%d)", len(lines), len(prompts))
	}
	runs := map[int]int{}
	for _, l := range lines {
		var u struct {
			Run   int    `json:"run"`
			Model string `json:"model"`
		}
		if err := json.Unmarshal([]byte(l), &u); err != nil {
			t.Fatal(err)
		}
		if u.Model != "opus" {
			t.Errorf("usage line records model %q, want opus: %s", u.Model, l)
		}
		runs[u.Run]++
	}
	if want := map[int]int{1: len(all), 2: len(all)}; !reflect.DeepEqual(runs, want) {
		t.Errorf("usage lines by run = %v, want %v", runs, want)
	}
	if segs := readLosses(t, folder); len(segs) != len(all) {
		t.Errorf("proposed losses for %d segments, want %d", len(segs), len(all))
	}
}

// A segment with no uncovered list, one fully covered, gets no call and no
// entry.
func TestRun_NoCallForAFullyCoveredSegment(t *testing.T) {
	src := testfixture.PragmatismSource(t)
	folder := pragmatismFolder(t, src)
	if err := os.Remove(filepath.Join(folder.Dir, run.MechanicalRecheckDir, run.UncoveredDir, "2.txt")); err != nil {
		t.Fatal(err)
	}
	prompts, replies := recorded(t, src, folder, []int{0, 1, 3, 4, 5, 6, 7})
	p := testfixture.Replay(t, prompts, replies)
	if err := lossproposer.Run(context.Background(), p, src, lossproposer.Model, folder); err != nil {
		t.Fatal(err)
	}
	if n := len(p.Calls()); n != 14 {
		t.Errorf("%d calls, want 14: two for each segment with a list", n)
	}
	for _, s := range readLosses(t, folder) {
		if s.Segment == 2 {
			t.Error("segment 2 has an entry, but no uncovered list")
		}
	}
	if _, err := os.Stat(filepath.Join(folder.Dir, run.LossProposerDir, "run-1", run.RawDir, "2.txt")); err == nil {
		t.Error("segment 2 has a raw reply, but no uncovered list")
	}
}

// A failed call in either run fails the step and leaves no complete step
// directory, so no partial union reaches the kept-loss repair.
func TestRun_AFailedCallLeavesNoProposedLosses(t *testing.T) {
	src := testfixture.PragmatismSource(t)
	// recorded gives segment n's run 1 reply at 2n and its run 2 reply at
	// 2n+1. A dropped reply makes its call fail.
	for name, drop := range map[string][]int{"run 1 fails": {0, 1}, "run 2 fails": {7}} {
		t.Run(name, func(t *testing.T) {
			folder := pragmatismFolder(t, src)
			prompts, replies := recorded(t, src, folder, []int{0, 1, 2, 3, 4, 5, 6, 7})
			for _, i := range slices.Backward(drop) {
				prompts, replies = slices.Delete(prompts, i, i+1), slices.Delete(replies, i, i+1)
			}
			p := testfixture.Replay(t, prompts, replies)
			if err := lossproposer.Run(context.Background(), p, src, lossproposer.Model, folder); err == nil {
				t.Fatal("Run succeeded, want the failure")
			}
			if _, err := os.Stat(filepath.Join(folder.Dir, run.LossProposerDir)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("complete step directory exists after a failure (stat err %v)", err)
			}
		})
	}
}

// The shared block then the prompt are the trialled prompt after the frame:
// only where the notes go has changed.
func TestPrompt_IsTheTrialledShape(t *testing.T) {
	notes := []*note.Note{note.Parse("0 A.md", "# 0 A\n\nOne.[^1]\n\n[^1]: [[S#A]] \"One.\"\n")}
	seg := source.Segment{Number: 3, Heading: "Arches"}
	got := lossproposer.Shared(notes) + lossproposer.Prompt(seg, "# Arches\n  ¶1 UNCOVERED: Two.\n")
	want := "=== ALL STAGE 1 NOTES FOR THE FRAME ===\n--- NOTE: 0 A.md ---\n" + notes[0].Render() + "\n=== END ALL STAGE 1 NOTES FOR THE FRAME ===\n\n" +
		"=== YOUR SEGMENT'S UNCOVERED LIST (segment 3: Arches) ===\n# Arches\n  ¶1 UNCOVERED: Two.\n=== END ===\n"
	if got != want {
		t.Errorf("prompt\n%s\nwant\n%s", got, want)
	}
}

// remarkFolder makes pragmatismFolder's folder, and gives each segment's
// prompt, twice, with the two recorded runs' replies, but for run 2's reply
// for segment 4: the hand-written one in hand-written/proposer-remark/, whose
// one item quotes no uncovered line.
func remarkFolder(t *testing.T, src *source.Source) (folder *run.Folder, prompts, replies []string) {
	t.Helper()
	folder = pragmatismFolder(t, src)
	prompts, replies = recorded(t, src, folder, []int{0, 1, 2, 3, 4, 5, 6, 7})
	replies[2*4+1] = testfixture.Files(t, testfixture.TrialRun(t, "hand-written", "proposer-remark"))["4.txt"]
	return folder, prompts, replies
}

func readDropped(t *testing.T, folder *run.Folder) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(folder.Dir, run.LossProposerDir, run.DroppedFile))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A remark, the hand-written run 2 "Quote: none uncovered" item for segment
// 4, is written as step 1.5's one dropped item, with its segment, run and
// reason. The runs' free lines are not.
func TestRun_WritesEachRemarkAsDropped(t *testing.T) {
	src := testfixture.PragmatismSource(t)
	folder, prompts, replies := remarkFolder(t, src)
	p := testfixture.Replay(t, prompts, replies)
	if err := lossproposer.Run(context.Background(), p, src, lossproposer.Model, folder); err != nil {
		t.Fatal(err)
	}
	items, err := dropped.Read(folder, run.LossProposerDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []dropped.Item{{
		Segment: 4,
		What:    "run 2's item: 1. **The part the older truths play in a new belief is in the notes already** - Quote: none uncovered (the notes on loyalty to the older truths cover it). - Why: Already covered, so no item.",
		Why:     lossproposer.RemarkWhy,
	}}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("dropped =\n%+v\nwant\n%+v", items, want)
	}
	if segs := readLosses(t, folder); len(segs[4].Other) == 0 || len(segs[4].Losses) != 0 {
		t.Errorf("segment 4 = %+v, want free lines and no loss", segs[4])
	}
}

// A resumed step 1.5 lists every segment's remarks, not just those of the
// segments it called again: here only segment 0's run 2 is called again,
// and segment 4's remark comes from a reply the resume reused.
func TestRun_AResumeListsTheReusedSegmentsRemarks(t *testing.T) {
	src := testfixture.PragmatismSource(t)
	folder, prompts, replies := remarkFolder(t, src)
	if err := lossproposer.Run(context.Background(), testfixture.Replay(t, prompts, replies), src, lossproposer.Model, folder); err != nil {
		t.Fatal(err)
	}
	want, err := dropped.Read(folder, run.LossProposerDir)
	if err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(folder.Dir, run.LossProposerDir)
	partial := final + run.PartialSuffix
	if err := os.Rename(final, partial); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(partial, modelstep.RunDir(2), run.RequestsDir, "0.sha256")); err != nil {
		t.Fatal(err)
	}
	p := testfixture.Replay(t, prompts[1:2], replies[1:2])
	if err := lossproposer.Run(context.Background(), p, src, lossproposer.Model, folder); err != nil {
		t.Fatal(err)
	}
	if n := len(p.Calls()); n != 1 {
		t.Fatalf("resume made %d calls, want 1", n)
	}
	got, err := dropped.Read(folder, run.LossProposerDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0].Segment != 4 || !reflect.DeepEqual(got, want) {
		t.Errorf("dropped after resume =\n%+v\nwant segment 4's remark, as before\n%+v", got, want)
	}
}
