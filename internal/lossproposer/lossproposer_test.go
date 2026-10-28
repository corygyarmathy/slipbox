package lossproposer_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
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

// The invented chapter's aside, by file name.
const aside = "1 The Keeper's Watch - Aside - The log.md"

// repairedAside is the aside as step 1.4 left it: "they" named, so the
// re-check covers the sentence step 1.2's first run left partial.
var repairedAside = "# 1 The Keeper's Watch - Aside - The log\n\n" +
	"The log was read by nobody until a ship was lost.[^1]\n" +
	"Then the inspectors read every page.[^2]\n\n" +
	"[^1]: " + testfixture.LampTrace("The Keeper’s Watch", "The log was read by nobody until a ship was lost.") + "\n" +
	"[^2]: " + testfixture.LampTrace("The Keeper’s Watch", "Then the inspectors read every page.") + "\n"

// repairedRun makes a run folder for the invented chapter whose steps 1.1 to
// 1.4 are done: LampStage1 as step 1.1's notes, with step 1.2 run on them,
// and as the repaired notes but for the aside, repairedAside, with step 1.2's
// re-check run on those. The re-check leaves segment 0 fully covered, so it
// has no uncovered list, and segment 1 one line fewer than the first run.
func repairedRun(t *testing.T) (*source.Source, *run.Folder) {
	t.Helper()
	src, folder := testfixture.LampExtractRun(t)
	if err := mechcheck.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	repaired := testfixture.LampStage1()
	repaired[aside] = repairedAside
	testfixture.WriteFiles(t, filepath.Join(folder.Dir, filepath.FromSlash(run.RepairNotes)), repaired)
	if err := mechcheck.RunOn(src, folder, run.RepairNotes, run.MechanicalRecheckDir); err != nil {
		t.Fatal(err)
	}
	return src, folder
}

// segment is the key a fake matches a segment's call on: the label its user
// prompt gives it.
func segment(n int) string { return fmt.Sprintf("(segment %d: ", n) }

// serve returns a fake that answers segment n's k-th call, from 1, with
// reply(n, k). The two runs' prompts are the same, byte for byte, and run 2
// starts once run 1 is done, so a segment's k-th call is run k's.
func serve(t *testing.T, src *source.Source, reply func(n, k int) (string, error)) *testfixture.Fake {
	t.Helper()
	var mu sync.Mutex
	asked := map[int]int{}
	return testfixture.NewFake(func(req provider.Request) (provider.Reply, error) {
		for _, seg := range src.Segments {
			if strings.Contains(req.User, segment(seg.Number)) {
				mu.Lock()
				asked[seg.Number]++
				k := asked[seg.Number]
				mu.Unlock()
				text, err := reply(seg.Number, k)
				return provider.Reply{Text: text}, err
			}
		}
		t.Errorf("no segment's call: %.80q", req.User)
		return provider.Reply{}, fmt.Errorf("no segment's call: %.80q", req.User)
	})
}

// serveCases serves each case's replies for its segment, run 1's first.
func serveCases(t *testing.T, src *source.Source, cases []replyCase) *testfixture.Fake {
	t.Helper()
	return serve(t, src, func(n, k int) (string, error) {
		for _, c := range cases {
			if c.segment == n && k <= len(c.replies) {
				return c.replies[k-1], nil
			}
		}
		t.Errorf("segment %d: no reply for call %d", n, k)
		return "", fmt.Errorf("segment %d: no reply for call %d", n, k)
	})
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

func readNotes(t *testing.T, folder *run.Folder, dir string) []*note.Note {
	t.Helper()
	notes, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(dir)))
	if err != nil {
		t.Fatal(err)
	}
	return notes
}

// uncovered gives the re-check's uncovered lists, by segment number.
func uncovered(t *testing.T, folder *run.Folder) map[int]string {
	t.Helper()
	lists := map[int]string{}
	for name, text := range testfixture.Files(t, filepath.Join(folder.Dir, run.MechanicalRecheckDir, run.UncoveredDir)) {
		n, err := strconv.Atoi(strings.TrimSuffix(name, ".txt"))
		if err != nil {
			t.Fatalf("uncovered list %s: %v", name, err)
		}
		lists[n] = text
	}
	return lists
}

// TestShared pins the shared block on the repaired notes: every note, in
// name order.
func TestShared(t *testing.T) {
	_, folder := repairedRun(t)
	testfixture.Golden(t, "shared.txt", lossproposer.Shared(readNotes(t, folder, run.RepairNotes)))
}

// TestPrompt pins each called segment's user prompt: its uncovered list from
// the re-check, as written.
func TestPrompt(t *testing.T) {
	src, folder := repairedRun(t)
	lists := uncovered(t, folder)
	if _, ok := lists[0]; ok {
		t.Fatal("segment 0 has an uncovered list; the run folder is not the one this test pins")
	}
	for n, list := range lists {
		got := lossproposer.Prompt(src.Segments[n], list)
		testfixture.Golden(t, "prompt/"+strconv.Itoa(n)+".txt", got)
		// The golden files can't say the list is the one the step wrote, byte
		// for byte, and not a copy laid out again.
		if !strings.Contains(got, "===\n"+list+"=== END ===\n") {
			t.Errorf("segment %d: prompt does not hold the uncovered list as written:\n%s", n, got)
		}
	}
}

// replyCase is a segment's two replies, run 1's then run 2's, and the entry
// their union should give it.
type replyCase struct {
	name    string
	segment int
	replies [2]string
	want    lossproposer.Segment
}

// Replies and items replyCases share.
var (
	watchKeeper1 = lossproposer.Item{Run: 1, Idea: "One keeper did both tasks",
		Quotes: []string{"The keeper trimmed the wick every four hours"}, Why: "The notes say \"he\" and never who.",
		Text: "1. **One keeper did both tasks**\n   - Quote: \"The keeper trimmed the wick every four hours\"\n   - Why: The notes say \"he\" and never who."}
	watchKeeper2 = lossproposer.Item{Run: 2, Idea: "The keeper kept the log as well as the wick",
		Quotes: []string{"and logged the weather in a book"}, Why: "Split in two, the line loses that one man did both.",
		Text: "1. **The keeper kept the log as well as the wick**\n   - Quote: \"and logged the weather in a book\"\n   - Why: Split in two, the line loses that one man did both."}
	watchInspectors = lossproposer.Item{Run: 2, Idea: "The inspectors read the log",
		Quotes: []string{"Then the inspectors read every page."}, Why: "Who read it matters.",
		Text: "2. **The inspectors read the log**\n   - Quote: \"Then the inspectors read every page.\"\n   - Why: Who read it matters."}
	stormsWreck = lossproposer.Item{Run: 1, Idea: "A wreck on the rocks",
		Quotes: []string{"A wreck on the rocks was the keeper’s failure"}, Why: "The rocks are where the failure lies.",
		Text: "1. **A wreck on the rocks**\n   - Quote: \"A wreck on the rocks was the keeper’s failure\"\n   - Why: The rocks are where the failure lies."}
	stormsGlass1 = lossproposer.Item{Run: 1, Idea: "The author stood in the lamp room",
		Quotes: []string{"I have stood in that lamp room … and the glass shook."}, Why: "The author's own witness.",
		Text: "2. **The author stood in the lamp room**\n   - Quote: “I have stood in that lamp room … and the glass shook.”\n   - Why: The author's own witness."}
	stormsGlass2 = lossproposer.Item{Run: 2, Idea: "The glass shook in a gale",
		Quotes: []string{"I have stood in that lamp room in a gale"}, Why: "Only the author saw it.",
		Text: "2. **The glass shook in a gale**\n   - Quote: \"I have stood in that lamp room in a gale\"\n   - Why: Only the author saw it."}
	stormsCovered = lossproposer.Item{Run: 2, Idea: "The nine days in 1839 are in the notes already",
		Quotes: []string{"none uncovered (the note on every gale covers it)."}, Why: "Already covered, so no item.",
		Text: "1. **The nine days in 1839 are in the notes already**\n   - Quote: none uncovered (the note on every gale covers it).\n   - Why: Already covered, so no item."}
)

// replyCases are the replies of TestRun's three called segments. Segment 0,
// fully covered by the repaired notes, gets no call.
var replyCases = []replyCase{
	// The well-formed shape: each run proposes the same loss, quoting the
	// line from a different start, and the two are one loss. Run 2's second
	// item quotes the sentence the repair covered: it is on step 1.2's first
	// list but not the re-check's, so it is a remark. The entry's heading is
	// the uncovered list's, which step 1.2 writes with a straight apostrophe.
	{"one loss quoted from two starts, and a remark on a line the repair covered", 1, [2]string{
		"# The Keeper’s Watch\n\n" +
			"1. **One keeper did both tasks**\n" +
			"   - Quote: \"The keeper trimmed the wick every four hours\"\n" +
			"   - Why: The notes say \"he\" and never who.\n\n" +
			"No other losses proposed.\n",
		"# The Keeper’s Watch\n\n" +
			"1. **The keeper kept the log as well as the wick**\n" +
			"   - Quote: \"and logged the weather in a book\"\n" +
			"   - Why: Split in two, the line loses that one man did both.\n\n" +
			"2. **The inspectors read the log**\n" +
			"   - Quote: \"Then the inspectors read every page.\"\n" +
			"   - Why: Who read it matters.\n",
	}, lossproposer.Segment{Segment: 1, Heading: "The Keeper's Watch",
		Losses:  []lossproposer.Loss{{Wordings: []lossproposer.Item{watchKeeper1, watchKeeper2}}},
		Remarks: []lossproposer.Item{watchInspectors},
		Other:   []lossproposer.Line{{Run: 1, Text: "No other losses proposed."}},
	}},
	// Neither run proposes a loss: the segment has an entry with its free
	// lines and nothing else.
	{"no losses from either run", 2, [2]string{
		"# The Lens\n\nNo losses proposed.\n",
		"No losses proposed.",
	}, lossproposer.Segment{Segment: 2, Heading: "The Lens",
		Losses: []lossproposer.Loss{},
		Other:  []lossproposer.Line{{Run: 1, Text: "No losses proposed."}, {Run: 2, Text: "No losses proposed."}},
	}},
	// Run 1 quotes a PARTIAL line in the source's case with a curly
	// apostrophe, and shortens a line with an ellipsis inside curly quotes.
	// Run 2 opens with an item that quotes "none uncovered", a remark, then
	// quotes the shortened line's start, which merges with run 1's.
	{"case, a curly apostrophe and an ellipsis, and a none-uncovered remark", 3, [2]string{
		"# Storms\n\n" +
			"1. **A wreck on the rocks**\n" +
			"   - Quote: \"A wreck on the rocks was the keeper’s failure\"\n" +
			"   - Why: The rocks are where the failure lies.\n\n" +
			"2. **The author stood in the lamp room**\n" +
			"   - Quote: “I have stood in that lamp room … and the glass shook.”\n" +
			"   - Why: The author's own witness.\n",
		"# Storms\n\n" +
			"1. **The nine days in 1839 are in the notes already**\n" +
			"   - Quote: none uncovered (the note on every gale covers it).\n" +
			"   - Why: Already covered, so no item.\n\n" +
			"2. **The glass shook in a gale**\n" +
			"   - Quote: \"I have stood in that lamp room in a gale\"\n" +
			"   - Why: Only the author saw it.\n\n" +
			"No other losses proposed.\n",
	}, lossproposer.Segment{Segment: 3, Heading: "Storms",
		Losses: []lossproposer.Loss{
			{Wordings: []lossproposer.Item{stormsWreck}},
			{Wordings: []lossproposer.Item{stormsGlass1, stormsGlass2}},
		},
		Remarks: []lossproposer.Item{stormsCovered},
		Other:   []lossproposer.Line{{Run: 2, Text: "No other losses proposed."}},
	}},
}

// TestRun runs step 1.5 on the repaired notes with replyCases, and checks
// the calls it made and what it wrote.
func TestRun(t *testing.T) {
	src, folder := repairedRun(t)
	p := serveCases(t, src, replyCases)
	if err := lossproposer.Run(context.Background(), p, src, lossproposer.Model, folder); err != nil {
		t.Fatal(err)
	}

	files := testfixture.Files(t, filepath.Join(folder.Dir, run.LossProposerDir))
	segs := readLosses(t, folder)
	if len(segs) != len(replyCases) {
		t.Fatalf("proposed losses for %d segments, want %d: %+v", len(segs), len(replyCases), segs)
	}
	for i, c := range replyCases {
		t.Run(c.name, func(t *testing.T) {
			for k, reply := range c.replies {
				name := modelstep.RunDir(k+1) + "/" + run.RawDir + "/" + strconv.Itoa(c.segment) + ".txt"
				if got := files[name]; got != reply {
					t.Errorf("%s is not run %d's reply:\n got %q\nwant %q", name, k+1, got, reply)
				}
			}
			if !reflect.DeepEqual(segs[i], c.want) {
				t.Errorf("segment %d's entry:\n got %+v\nwant %+v", c.segment, segs[i], c.want)
			}
		})
	}
	// Segment 0 has no uncovered list, so no call, no raw reply and no entry.
	for k := 1; k <= lossproposer.Runs; k++ {
		if _, ok := files[modelstep.RunDir(k)+"/"+run.RawDir+"/0.txt"]; ok {
			t.Errorf("segment 0 has a run %d raw reply, but no uncovered list", k)
		}
	}

	// Each remark is a dropped item, in segment then run order, with the
	// reply's lines as written.
	items, err := dropped.Read(folder, run.LossProposerDir)
	if err != nil {
		t.Fatal(err)
	}
	wantDropped := []dropped.Item{
		{Segment: 1, What: "run 2's item: 2. **The inspectors read the log** - Quote: \"Then the inspectors read every page.\" - Why: Who read it matters.", Why: lossproposer.RemarkWhy},
		{Segment: 3, What: "run 2's item: 1. **The nine days in 1839 are in the notes already** - Quote: none uncovered (the note on every gale covers it). - Why: Already covered, so no item.", Why: lossproposer.RemarkWhy},
	}
	if !reflect.DeepEqual(items, wantDropped) {
		t.Errorf("dropped =\n%+v\nwant\n%+v", items, wantDropped)
	}

	// Two calls per called segment, each its re-check list's prompt, briefed
	// by the proposer brief, the frame and every repaired note, at sonnet and
	// medium.
	notes := readNotes(t, folder, run.RepairNotes)
	brief := testfixture.RepoFile(t, "internal/briefs/proposer.md")
	wantSystem := brief + "\n\n=== FRAME: " + testfixture.Lamp + ".md ===\n" + testfixture.LampChapter + "\n=== END FRAME ===\n\n" + lossproposer.Shared(notes)
	calls := p.Calls()
	if len(calls) != lossproposer.Runs*len(replyCases) {
		t.Fatalf("%d calls, want %d: two per segment with an uncovered list", len(calls), lossproposer.Runs*len(replyCases))
	}
	for _, c := range calls {
		if c.System != wantSystem {
			t.Errorf("call for %.80q: system prompt is not the proposer brief, the frame and every repaired note", c.User)
		}
		if c.Model != "sonnet" || c.Effort != "medium" {
			t.Errorf("call for %.80q: model %q at effort %q, want sonnet at medium", c.User, c.Model, c.Effort)
		}
	}
	lists := uncovered(t, folder)
	var want []string
	for _, c := range replyCases {
		want = append(want, lossproposer.Prompt(src.Segments[c.segment], lists[c.segment]))
	}
	// The second run starts once the first is done, and asks each prompt
	// again, so its calls find the first's prompts cached.
	users := func(cs []provider.Request) []string {
		var out []string
		for _, c := range cs {
			out = append(out, c.User)
		}
		slices.Sort(out)
		return out
	}
	half := len(calls) / 2
	if first := users(calls[:half]); !reflect.DeepEqual(first, want) {
		t.Errorf("run 1's prompts are not one per called segment's re-check list:\n got %q\nwant %q", first, want)
	}
	if second := users(calls[half:]); !reflect.DeepEqual(second, want) {
		t.Errorf("run 2's prompts, after run 1's, are not run 1's:\n got %q\nwant %q", second, want)
	}
	if n := strings.Count(files[run.UsageFile], "\n"); n != len(calls) {
		t.Errorf("usage log has %d lines, want one per call (%d)", n, len(calls))
	}
}

// Run on another model asks for it in both runs' calls, and each line of
// the usage log records it, with its run.
func TestRun_AnotherModel(t *testing.T) {
	src, folder := repairedRun(t)
	p := serveCases(t, src, replyCases)
	if err := lossproposer.Run(context.Background(), p, src, "opus", folder); err != nil {
		t.Fatal(err)
	}
	calls := p.Calls()
	for _, c := range calls {
		if c.Model != "opus" || c.Effort != lossproposer.Effort {
			t.Errorf("call for %.80q: model %q at %q, want opus at %q", c.User, c.Model, c.Effort, lossproposer.Effort)
		}
	}
	usage := testfixture.Files(t, filepath.Join(folder.Dir, run.LossProposerDir))[run.UsageFile]
	lines := strings.Split(strings.TrimSuffix(usage, "\n"), "\n")
	if len(lines) != len(calls) {
		t.Fatalf("usage log has %d lines, want one per call (%d)", len(lines), len(calls))
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
	if want := map[int]int{1: len(replyCases), 2: len(replyCases)}; !reflect.DeepEqual(runs, want) {
		t.Errorf("usage lines by run = %v, want %v", runs, want)
	}
}

// A failed call in either run fails the step and leaves no complete step
// directory, so no partial union reaches the kept-loss repair.
func TestRun_AFailedCallLeavesNoProposedLosses(t *testing.T) {
	for name, fail := range map[string][2]int{"run 1 fails": {1, 1}, "run 2 fails": {3, 2}} {
		t.Run(name, func(t *testing.T) {
			src, folder := repairedRun(t)
			p := serve(t, src, func(n, k int) (string, error) {
				if n == fail[0] && k == fail[1] {
					return "", errors.New("the call failed")
				}
				return "No losses proposed.", nil
			})
			if err := lossproposer.Run(context.Background(), p, src, lossproposer.Model, folder); err == nil {
				t.Fatal("Run succeeded, want the failure")
			}
			if _, err := os.Stat(filepath.Join(folder.Dir, run.LossProposerDir)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("complete step directory exists after a failure (stat err %v)", err)
			}
		})
	}
}

// A resumed step 1.5 lists every segment's remarks, not just those of the
// segments it called again: here only segment 1's run 2 is called again,
// and segment 3's remark comes from a reply the resume reused.
func TestRun_AResumeListsTheReusedSegmentsRemarks(t *testing.T) {
	src, folder := repairedRun(t)
	if err := lossproposer.Run(context.Background(), serveCases(t, src, replyCases), src, lossproposer.Model, folder); err != nil {
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
	if err := os.Remove(filepath.Join(partial, modelstep.RunDir(2), run.RequestsDir, "1.sha256")); err != nil {
		t.Fatal(err)
	}
	p := serve(t, src, func(n, k int) (string, error) {
		if n != 1 || k != 1 {
			t.Errorf("resume called segment %d, its call %d; want only segment 1's run 2", n, k)
		}
		return replyCases[0].replies[1], nil
	})
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
	if len(got) != 2 || got[1].Segment != 3 || !reflect.DeepEqual(got, want) {
		t.Errorf("dropped after resume =\n%+v\nwant segment 1's and segment 3's remarks, as before\n%+v", got, want)
	}
}
