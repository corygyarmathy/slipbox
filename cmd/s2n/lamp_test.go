package main

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/briefs"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// lampSourcePath writes Lamp where s2n run can read it, and gives its path.
func lampSourcePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	testfixture.WriteFiles(t, dir, map[string]string{testfixture.Lamp + ".md": testfixture.LampChapter})
	return filepath.Join(dir, testfixture.Lamp+".md")
}

// lampReplies are replies that lampModel gives in place of its own, by step,
// named by its directory as stepOf names it, then by segment number. A
// step's k-th call for a segment, from 0, is given the k-th reply, and a
// call past the last is given lampModel's own.
type lampReplies map[string]map[int][]string

// stepOf names the step that made req by its directory, from the brief its
// system prompt opens with. A re-check is named as the check it repeats:
// every call briefed as step 1.3a is SupportCheckDir's, and as step 1.3b
// ReadsAloneDir's.
func stepOf(req provider.Request) (string, bool) {
	for _, s := range []struct {
		dir    string
		briefs []string
	}{
		// The repair and kept-loss briefs come before the stage 1 rules in
		// their system prompts, so they are matched first.
		{run.RepairDir, []string{briefs.Repair, briefs.RepairEdits, briefs.RepairEditsQuoteOnce}},
		{run.KeptLossDir, []string{briefs.KeptLoss, briefs.KeptLossEdits, briefs.KeptLossEditsQuoteOnce}},
		{run.ExtractDir, []string{briefs.Stage1, briefs.Stage1QuoteOnce}},
		{run.SupportCheckDir, []string{briefs.CheckerSupport}},
		{run.ReadsAloneDir, []string{briefs.CheckerReadsAlone}},
		{run.LossProposerDir, []string{briefs.Proposer}},
		{run.SplitDir, []string{briefs.Stage2}},
		{run.FormatDir, []string{briefs.Stage3}},
	} {
		for _, b := range s.briefs {
			if strings.HasPrefix(req.System, b+"\n\n") {
				return s.dir, true
			}
		}
	}
	return "", false
}

// segmentOf gives the number of the Lamp segment req is a call for, from the
// label its user prompt gives the segment: step 1.1's, step 2.1's and step
// 3.1's each have their own, and the rest share one.
func segmentOf(req provider.Request) (int, bool) {
	for n := range 4 {
		for _, label := range []string{
			fmt.Sprintf("Your segment: number %d,", n),
			fmt.Sprintf("(segment %d: ", n),
			fmt.Sprintf("=== SEGMENT %d STAGE ", n),
		} {
			if strings.Contains(req.User, label) {
				return n, true
			}
		}
	}
	return 0, false
}

// lampModel is a model that takes Lamp through every step with no fault, in
// the run folder runDir: step 1.1 writes LampStage1, the checks flag
// nothing, so step 1.4 makes no call, step 1.5 proposes no loss, so the
// kept-loss repair makes none, step 2.1 writes LampSplit, which passes its
// gates, and step 3.1 gives back the stripped notes unchanged. over gives a
// step's call for a segment another reply. A call it has no reply for fails
// the test.
func lampModel(t *testing.T, runDir string, over lampReplies) *testfixture.Fake {
	t.Helper()
	var mu sync.Mutex
	asked := map[string]int{}
	fail := func(format string, args ...any) (provider.Reply, error) {
		err := fmt.Errorf("lampModel: "+format, args...)
		t.Errorf("%v", err)
		return provider.Reply{}, err
	}
	return testfixture.NewFake(func(req provider.Request) (provider.Reply, error) {
		step, ok := stepOf(req)
		if !ok {
			return fail("no step's brief opens the system prompt of the call %.80q", req.User)
		}
		n, ok := segmentOf(req)
		if !ok {
			return fail("no segment's call: %.80q", req.User)
		}
		key := fmt.Sprintf("%s %d", step, n)
		mu.Lock()
		k := asked[key]
		asked[key]++
		mu.Unlock()
		if rs := over[step][n]; k < len(rs) {
			return provider.Reply{Text: rs[k]}, nil
		}
		prefix := fmt.Sprintf("%d ", n)
		switch step {
		case run.ExtractDir:
			// A note given traced is written as given, quote-once or not.
			var b strings.Builder
			stage1 := testfixture.LampStage1()
			for _, name := range keys(stage1) {
				if strings.HasPrefix(name, prefix) {
					b.WriteString("=== FILE: " + name + " ===\n" + stage1[name])
				}
			}
			return provider.Reply{Text: b.String() + "=== REPORT ===\nNone.\n"}, nil
		case run.SupportCheckDir, run.ReadsAloneDir:
			return provider.Reply{Text: "pass\n"}, nil
		case run.LossProposerDir:
			return provider.Reply{Text: "No losses proposed.\n"}, nil
		case run.SplitDir:
			return provider.Reply{Text: splitReply(testfixture.LampSplit(), n)}, nil
		case run.FormatDir:
			folders, err := split.ReadFolders(filepath.Join(runDir, filepath.FromSlash(run.StrippedNotes)))
			if err != nil {
				return fail("%v", err)
			}
			for _, f := range folders {
				if strings.HasPrefix(f.Name, prefix) {
					return provider.Reply{Text: fileBlocks(f.Notes) + "=== MANIFEST ===\n{}\n"}, nil
				}
			}
		}
		return fail("no reply for step %s, segment %d", step, n)
	})
}

// splitReply is step 2.1's reply for segment n that writes its folder of
// files, a split by slash-separated path such as LampSplit: its notes as
// FILE blocks, then an empty manifest.
func splitReply(files map[string]string, n int) string {
	var b strings.Builder
	for _, rel := range keys(files) {
		folder, name, ok := strings.Cut(rel, "/")
		if ok && strings.HasPrefix(folder, fmt.Sprintf("%d ", n)) && strings.HasSuffix(name, ".md") {
			b.WriteString("=== FILE: " + name + " ===\n" + files[rel])
		}
	}
	return b.String() + "=== MANIFEST ===\n{}\n"
}

// lampRun runs `s2n run` on Lamp in a new run folder with flags, served by
// lampModel with over, and gives the run folder, the model and what the run
// said on stderr. A run that doesn't exit 0 fails the test.
func lampRun(t *testing.T, over lampReplies, flags ...string) (string, *testfixture.Fake, string) {
	t.Helper()
	runDir := filepath.Join(t.TempDir(), "run")
	p := lampModel(t, runDir, over)
	var stdout, stderr bytes.Buffer
	args := append(append([]string{"run", "-run-dir", runDir}, flags...), lampSourcePath(t))
	if code := runCommand(context.Background(), args, &stdout, &stderr, p); code != 0 {
		t.Fatalf("s2n run %q: exit %d, want 0: %s", flags, code, stderr.String())
	}
	return runDir, p, stderr.String()
}

// lampAt is a run folder of Lamp, run by lampModel with no fault, stopped
// after the step stopAfter.
func lampAt(t *testing.T, stopAfter string) string {
	t.Helper()
	runDir, _, _ := lampRun(t, nil, "-stop-after", stopAfter)
	return runDir
}

// The replies lampEveryStep gives in place of lampModel's own, and what they
// make step 1.4 and the kept-loss repair write.
const (
	// watchDangling is step 1.3b's report on segment 1, flagging a
	// statement that says "he" for the keeper.
	watchDangling = "# 1 The Keeper's Watch\n\n" +
		"- [dangling] \"He logged the weather in a book.\": \"He\" names no one.\n" +
		"pass 2 / dangling 1 / outside-frame 0\n"
	// watchRepair is step 1.4's reply on segment 1: an edit written
	// quote-once that names the keeper.
	watchRepair = "=== EDIT: 1 The Keeper's Watch.md ===\n" +
		"--- REPLACE ---\nHe logged the weather in a book.[^3]\n" +
		"--- WITH ---\nThe keeper{ also }logged the weather in a book.\n" +
		"=== REPORT ===\n- Named the keeper.\n"
	// watchLoss is step 1.5's reply on segment 1: who read the log, which
	// the notes say only as "they", as a loss.
	watchLoss = "# The Keeper's Watch\n\n" +
		"1. **The inspectors read the log**\n" +
		"   - Quote: \"Then the inspectors read every page.\"\n" +
		"   - Why: Who read it matters.\n"
	// watchKept is the kept-loss repair's reply on segment 1: the sentence
	// inserted quote-once after the statement step 1.4 repaired.
	watchKept = "=== EDIT: 1 The Keeper's Watch.md ===\n" +
		"--- AFTER ---\nThe keeper[^3] also logged the weather in a book.[^4]\n" +
		"--- INSERT ---\nThen the inspectors read every page.\n" +
		"=== REPORT ===\n- Loss 1.1: named the inspectors.\n"
)

// lampEveryStep are replies that take Lamp through every model step: step
// 1.3b flags a statement of segment 1, which step 1.4 repairs and its
// re-checks pass, and step 1.5 proposes a loss in segment 1, which the
// kept-loss repair adds after the repaired statement, so it is given step
// 1.4's notes, and its re-check passes. Step 2.1 splits segment 1 as its
// final notes have it, so every segment passes its gates.
func lampEveryStep() lampReplies {
	splitFiles := testfixture.LampSplit()
	const dusk = "1 The Keeper's Watch/The Watch Began at Dusk.md"
	splitFiles[dusk] = strings.NewReplacer(
		"The keeper[^4] logged the weather in a book.[^5]\n",
		"The keeper[^4] also logged the weather in a book.[^5] Then the inspectors read every page.[^6]\n",
		`"logged the weather in a book"`+"\n",
		`"logged the weather in a book."`+"\n[^6]: "+testfixture.LampTrace("The Keeper’s Watch", "Then the inspectors read every page.")+"\n",
	).Replace(splitFiles[dusk])
	return lampReplies{
		run.ReadsAloneDir:   {1: {watchDangling}},
		run.RepairDir:       {1: {watchRepair}},
		run.LossProposerDir: {1: {watchLoss, watchLoss}},
		run.KeptLossDir:     {1: {watchKept}},
		run.SplitDir:        {1: {splitReply(splitFiles, 1)}},
	}
}
