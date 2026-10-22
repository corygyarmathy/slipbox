package keptloss_test

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/briefs"
	"github.com/corygyarmathy/slipbox/internal/dropped"
	"github.com/corygyarmathy/slipbox/internal/keptloss"
	"github.com/corygyarmathy/slipbox/internal/lossproposer"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/repair"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/supportcheck"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

const tinySource = `---
source: "[[@Tiny - A. Author]]"
---
# 1. Tiny

Alpha is first. Beta is second.

## Two

Gamma is third. Delta is fourth.
`

// Step 1.4's repaired notes: each segment's lost its second sentence.
const (
	tinyNote0 = "# 0 Tiny\n\nAlpha is first.[^1]\n\n[^1]: [[Tiny#1. Tiny]] \"Alpha is first.\"\n"
	tinyNote1 = "# 1 Two\n\nGamma is third.[^1]\n\n[^1]: [[Tiny#Two]] \"Gamma is third.\"\n"
	// tinyNote0Beta is tinyNote0 with loss 0.1 added, as a reply gives it.
	tinyNote0Beta = "# 0 Tiny\n\nAlpha is first.[^1] Beta is second.[^2]\n\n[^1]: [[Tiny#1. Tiny]] \"Alpha is first.\"\n[^2]: [[Tiny#1. Tiny]] \"Beta is second.\"\n"
)

// tinyLosses are step 1.5's union: loss 0.1 in segment 0, and 1.1 and 1.2 in
// segment 1.
func tinyLosses() []lossproposer.Segment {
	loss := func(idea string) lossproposer.Loss {
		return lossproposer.Loss{Wordings: []lossproposer.Item{{Run: 1, Idea: idea, Quotes: []string{idea}, Why: "it's the argument"}}}
	}
	return []lossproposer.Segment{
		{Segment: 0, Heading: "1. Tiny", Losses: []lossproposer.Loss{loss("Beta is second.")}, Other: []lossproposer.Line{}},
		{Segment: 1, Heading: "Two", Losses: []lossproposer.Loss{loss("Delta is fourth."), loss("Gamma and Delta.")}, Other: []lossproposer.Line{}},
	}
}

// afterProposer makes a run folder whose step 1.5 proposed the losses of
// tinyLosses in the segments segs, or every segment's if segs is empty.
func afterProposer(t *testing.T, segs ...int) (*source.Source, *run.Folder) {
	t.Helper()
	src, err := source.Parse("Tiny", tinySource)
	if err != nil {
		t.Fatal(err)
	}
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	if err := folder.SaveSource(src.Name, []byte(src.Frame)); err != nil {
		t.Fatal(err)
	}
	losses := tinyLosses()
	for i := range losses {
		if len(segs) > 0 && !slices.Contains(segs, losses[i].Segment) {
			losses[i].Losses = []lossproposer.Loss{}
		}
	}
	b, err := json.Marshal(losses)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, folder, b, run.LossProposerDir, run.ProposedLossesFile)
	writeFile(t, folder, []byte(tinyNote0), run.RepairDir, run.NotesDir, "0 Tiny.md")
	writeFile(t, folder, []byte(tinyNote1), run.RepairDir, run.NotesDir, "1 Two.md")
	return src, folder
}

func writeFile(t *testing.T, folder *run.Folder, data []byte, rel ...string) {
	t.Helper()
	p := filepath.Join(append([]string{folder.Dir}, rel...)...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func notes(t *testing.T, dir string) []*note.Note {
	t.Helper()
	ns, err := note.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return ns
}

// keptPrompt is segment n's kept-loss prompt on step 1.4's notes, with
// every loss step 1.5 proposed in it.
func keptPrompt(t *testing.T, src *source.Source, folder *run.Folder, n int) string {
	t.Helper()
	kept, err := keptloss.Losses(folder)
	if err != nil {
		t.Fatal(err)
	}
	var mine []keptloss.Kept
	for _, k := range kept {
		if k.Segment == n {
			mine = append(mine, k)
		}
	}
	return keptloss.Prompt(notes(t, filepath.Join(folder.Dir, run.RepairDir, run.NotesDir)), src.Segments[n], mine)
}

// recheckPrompt is step 1.3a's prompt for segment n on notes, the notes as the
// kept-loss call leaves them.
func recheckPrompt(t *testing.T, src *source.Source, n int, texts map[string]string) string {
	t.Helper()
	var ns []*note.Note
	for _, name := range []string{"0 Tiny.md", "1 Two.md"} {
		ns = append(ns, note.Parse(name, texts[name]))
	}
	return supportcheck.Prompt(ns, src.Segments[n])
}

// runSegment0 runs the kept-loss repair with loss 0.1 proposed, serving reply to
// its call and support to step 1.3a's re-check of the notes after.
// It gives the number of calls made.
func runSegment0(t *testing.T, reply, after0, support string) (*run.Folder, int, error) {
	t.Helper()
	folder, calls, err := runSegment0As(t, repair.Whole, reply, after0, support)
	return folder, len(calls), err
}

// runSegment0As is runSegment0 briefed for shape, giving the calls made.
func runSegment0As(t *testing.T, shape repair.Reply, reply, after0, support string) (*run.Folder, []provider.Request, error) {
	t.Helper()
	src, folder := afterProposer(t, 0)
	prompts := []string{keptPrompt(t, src, folder, 0), recheckPrompt(t, src, 0, map[string]string{"0 Tiny.md": after0, "1 Two.md": tinyNote1})}
	p := testfixture.Replay(t, prompts, []string{reply, support})
	err := keptloss.Run(context.Background(), p, src, "opus", shape, false, folder)
	return folder, p.Calls(), err
}

func TestPrompt(t *testing.T) {
	src, folder := afterProposer(t)
	got := keptPrompt(t, src, folder, 1)
	want := "=== YOUR SEGMENT'S NOTES (segment 1: Two) ===\n" +
		"--- NOTE: 1 Two ---\n" + tinyNote1 + "\n" +
		"=== END YOUR SEGMENT'S NOTES (segment 1: Two) ===\n\n" +
		"=== KEPT LOSSES ===\n" +
		"Loss 1.1\n\nRun 1: Delta is fourth.\n- Quote: \"Delta is fourth.\"\n- Why: it's the argument\n\n" +
		"Loss 1.2\n\nRun 1: Gamma and Delta.\n- Quote: \"Gamma and Delta.\"\n- Why: it's the argument\n" +
		"=== END KEPT LOSSES ===\n"
	if got != want {
		t.Errorf("Prompt:\n got %q\nwant %q", got, want)
	}
}

// Only a segment with a proposed loss is called, with its losses. The new
// statement passes its re-check, and a flag on a statement the notes already
// had isn't listed. The step's usage log holds every call it made.
func TestRun_ProposedLossesReachTheCall(t *testing.T) {
	support := "# 0 Tiny\n\n- [weak] \"Alpha is first.\": thin\n\npass 1 / weak 1 / fail 0 / dangling 0 / outside-frame 0\n"
	folder, calls, err := runSegment0(t, "=== FILE: 0 Tiny.md ===\n"+tinyNote0Beta+"=== REPORT ===\n- Loss 0.1: added.\n", tinyNote0Beta, support)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("%d calls, want the kept-loss call and one re-check", calls)
	}
	dir := filepath.Join(folder.Dir, run.KeptLossDir)
	files := testfixture.Files(t, dir)
	if files[run.NotesDir+"/0 Tiny.md"] != tinyNote0Beta || files[run.NotesDir+"/1 Two.md"] != tinyNote1 {
		t.Errorf("notes are not the reply's and step 1.4's:\n%q\n%q", files[run.NotesDir+"/0 Tiny.md"], files[run.NotesDir+"/1 Two.md"])
	}
	if files[run.ReportsDir+"/0.md"] != "- Loss 0.1: added.\n" {
		t.Errorf("report 0 is %q", files[run.ReportsDir+"/0.md"])
	}
	if _, ok := files[run.ReportsDir+"/1.md"]; ok {
		t.Errorf("segment 1, with no proposed loss, has a report")
	}
	var kept []keptloss.Kept
	if err := json.Unmarshal([]byte(files[run.KeptLossesFile]), &kept); err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].ID() != "0.1" {
		t.Errorf("kept losses %+v, want 0.1 alone", kept)
	}
	if files[run.StillFailsFile] != "[]\n" || files[run.DroppedFile] != "[]\n" {
		t.Errorf("a passing re-check listed something:\n%s\n%s", files[run.StillFailsFile], files[run.DroppedFile])
	}
	usage := files[run.UsageFile]
	if strings.Count(usage, "\n") != 2 || !strings.Contains(usage, `"step":"`+run.KeptLossDir+`"`) || !strings.Contains(usage, `"step":"`+run.KeptLossSupportDir+`"`) {
		t.Errorf("usage log does not hold both calls:\n%s", usage)
	}
	for _, rel := range []string{run.KeptLossMechanicalDir + "/" + run.ReportFile, run.KeptLossSupportDir + "/" + run.ReportsDir + "/0.md"} {
		if _, ok := files[rel]; !ok {
			t.Errorf("no re-check file %s", rel)
		}
	}
	if _, ok := files[run.KeptLossSupportDir+"/"+run.UsageFile]; ok {
		t.Errorf("the re-check kept a usage log of its own")
	}

	src, _ := source.Parse("Tiny", tinySource)
	if err := keptloss.WriteFinal(src, folder); err != nil {
		t.Fatal(err)
	}
	if got := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes))); !reflect.DeepEqual(got, testfixture.Files(t, filepath.Join(dir, run.NotesDir))) {
		t.Errorf("final notes are not the kept-loss repair's: %q", got)
	}
}

// Every proposed loss is kept: each segment with one is called with all of
// its own.
func TestRun_EveryProposedLossIsKept(t *testing.T) {
	src, folder := afterProposer(t)
	two := tinyNote1 + "\nDelta is fourth.[^2]\n\n[^2]: [[Tiny#Two]] \"Delta is fourth.\"\n"
	p := testfixture.Replay(t,
		[]string{keptPrompt(t, src, folder, 0), keptPrompt(t, src, folder, 1),
			recheckPrompt(t, src, 0, map[string]string{"0 Tiny.md": tinyNote0Beta, "1 Two.md": two}),
			recheckPrompt(t, src, 1, map[string]string{"0 Tiny.md": tinyNote0Beta, "1 Two.md": two})},
		[]string{"=== FILE: 0 Tiny.md ===\n" + tinyNote0Beta + "=== REPORT ===\nr\n", "=== FILE: 1 Two.md ===\n" + two + "=== REPORT ===\nr\n",
			"# 0 Tiny\n\npass 2\n", "# 1 Two\n\npass 2\n"})
	if err := keptloss.Run(context.Background(), p, src, "opus", repair.Whole, false, folder); err != nil {
		t.Fatal(err)
	}
	if n := len(p.Calls()); n != 4 {
		t.Errorf("%d calls, want 2 kept-loss calls and 2 re-checks", n)
	}
	if !strings.Contains(p.Calls()[1].User, "Loss 1.1\n") || !strings.Contains(p.Calls()[1].User, "Loss 1.2\n") {
		t.Errorf("segment 1's call does not hold both its losses:\n%s", p.Calls()[1].User)
	}
}

// still checks that the step is complete, and gives what it lists as still
// failing and dropped.
func still(t *testing.T, folder *run.Folder) ([]repair.Entry, []dropped.Item) {
	t.Helper()
	if !folder.Complete(run.KeptLossDir) {
		t.Fatalf("the step is not complete")
	}
	var fails []repair.Entry
	if err := json.Unmarshal([]byte(testfixture.Files(t, filepath.Join(folder.Dir, run.KeptLossDir))[run.StillFailsFile]), &fails); err != nil {
		t.Fatal(err)
	}
	items, err := dropped.Read(folder, run.KeptLossDir)
	if err != nil {
		t.Fatal(err)
	}
	return fails, items
}

// A new statement with a step 1.3a grade that counts is listed, with that
// grade alone, and stops nothing.
func TestRun_NewStatementFailingSupportIsListed(t *testing.T) {
	support := "# 0 Tiny\n\n- [fail] \"Beta is…\": not what the quote says\n"
	folder, _, err := runSegment0(t, "=== FILE: 0 Tiny.md ===\n"+tinyNote0Beta+"=== REPORT ===\nAdded.\n", tinyNote0Beta, support)
	if err != nil {
		t.Fatal(err)
	}
	fails, items := still(t, folder)
	if len(fails) != 1 || fails[0].Statement != "Beta is second.[^2]" || !reflect.DeepEqual(fails[0].Grades, []string{"fail"}) || len(items) != 0 {
		t.Errorf("still fails %+v, dropped %+v; want the new statement's fail alone", fails, items)
	}
}

// Step 1.3a's dangling and outside-frame grades don't count, on a new
// statement or any other.
func TestRun_ReadsAloneGradesArentListed(t *testing.T) {
	support := "# 0 Tiny\n\n- [dangling] \"Beta is…\": which Beta\n"
	folder, _, err := runSegment0(t, "=== FILE: 0 Tiny.md ===\n"+tinyNote0Beta+"=== REPORT ===\nAdded.\n", tinyNote0Beta, support)
	if err != nil {
		t.Fatal(err)
	}
	if fails, _ := still(t, folder); len(fails) != 0 {
		t.Errorf("still fails %+v, want nothing", fails)
	}
}

// A flag that matches no statement may be on a new one, so it is listed.
func TestRun_UnmatchedFlagIsListed(t *testing.T) {
	support := "# 0 Tiny\n\n- [weak] \"Zeta is…\": thin\n"
	folder, _, err := runSegment0(t, "=== FILE: 0 Tiny.md ===\n"+tinyNote0Beta+"=== REPORT ===\nAdded.\n", tinyNote0Beta, support)
	if err != nil {
		t.Fatal(err)
	}
	if fails, _ := still(t, folder); len(fails) != 1 || fails[0].Statement != "" || fails[0].Quote != "Zeta is…" {
		t.Errorf("still fails %+v, want the unmatched flag", fails)
	}
}

// A new statement with a broken trace stops nothing in the step: the final
// notes drop it, with its trace, and list it.
func TestWriteFinal_DropsABrokenStatement(t *testing.T) {
	broken := strings.Replace(tinyNote0Beta, "\"Beta is second.\"", "\"Beta is twelfth.\"", 1)
	folder, _, err := runSegment0(t, "=== FILE: 0 Tiny.md ===\n"+broken+"=== REPORT ===\nAdded.\n", broken, "# 0 Tiny\n\npass 2\n")
	if err != nil {
		t.Fatal(err)
	}
	if fails, _ := still(t, folder); len(fails) != 0 {
		t.Errorf("still fails %+v: step 1.2's flags are the final notes' to act on", fails)
	}
	src, _ := source.Parse("Tiny", tinySource)
	if err := keptloss.WriteFinal(src, folder); err != nil {
		t.Fatal(err)
	}
	final := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes)))
	if final["0 Tiny.md"] != tinyNote0 || final["1 Two.md"] != tinyNote1 {
		t.Errorf("final notes:\n%q\n%q\nwant the broken statement and its trace gone", final["0 Tiny.md"], final["1 Two.md"])
	}
	items, err := dropped.Read(folder, run.FinalDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Segment != 0 || items[0].Note != "0 Tiny.md" || items[0].What != "Beta is second.[^2]" || !strings.Contains(items[0].Why, "BROKEN [^2] Beta is twelfth.") {
		t.Errorf("dropped %+v, want the broken statement", items)
	}
}

// An untraced statement in an aside is dropped, and so is the aside it
// leaves with no statement.
func TestWriteFinal_DropsAnEmptiedAside(t *testing.T) {
	src, folder := afterProposer(t, 0)
	writeFile(t, folder, []byte(tinyNote0), run.KeptLossDir, run.NotesDir, "0 Tiny.md")
	writeFile(t, folder, []byte(tinyNote1), run.KeptLossDir, run.NotesDir, "1 Two.md")
	writeFile(t, folder, []byte("# 1 Two - Aside - X\n\nAn untraced aside.\n"), run.KeptLossDir, run.NotesDir, "1 Two - Aside - X.md")
	inner := &run.Folder{Dir: filepath.Join(folder.Dir, run.KeptLossDir)}
	if err := mechcheckRunOn(src, inner); err != nil {
		t.Fatal(err)
	}
	if err := keptloss.WriteFinal(src, folder); err != nil {
		t.Fatal(err)
	}
	final := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes)))
	if _, ok := final["1 Two - Aside - X.md"]; ok || len(final) != 2 {
		t.Errorf("final notes %q, want the aside gone", keys(final))
	}
	items, err := dropped.Read(folder, run.FinalDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].What != "An untraced aside." || items[1].What != "the note" || items[1].Segment != 1 {
		t.Errorf("dropped %+v, want the statement then the aside", items)
	}
}

// An untraced statement a note has twice is dropped both times, and the
// step goes on: the failing item is dropped, not the run stopped.
func TestWriteFinal_DropsEachCopyOfAStatement(t *testing.T) {
	src, folder := afterProposer(t, 0)
	writeFile(t, folder, []byte(tinyNote0), run.KeptLossDir, run.NotesDir, "0 Tiny.md")
	dup := strings.Replace(tinyNote1, "\n\n[^1]", "\n\nAn untraced claim.\n\nAn untraced claim.\n\n[^1]", 1)
	writeFile(t, folder, []byte(dup), run.KeptLossDir, run.NotesDir, "1 Two.md")
	inner := &run.Folder{Dir: filepath.Join(folder.Dir, run.KeptLossDir)}
	if err := mechcheckRunOn(src, inner); err != nil {
		t.Fatal(err)
	}
	if err := keptloss.WriteFinal(src, folder); err != nil {
		t.Fatal(err)
	}
	final := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes)))
	if final["1 Two.md"] != tinyNote1 {
		t.Errorf("final 1 Two.md:\n%q\nwant both copies of the untraced statement gone", final["1 Two.md"])
	}
	items, err := dropped.Read(folder, run.FinalDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Note != "1 Two.md" || items[0].What != "An untraced claim." || strings.Count(items[0].Why, "An untraced claim.") != 1 {
		t.Errorf("dropped %+v, want the untraced statement", items)
	}
}

func keys(m map[string]string) []string {
	return slices.Sorted(maps.Keys(m))
}

// A segment with a kept loss whose notes gain no statement has each of its
// losses listed as dropped.
func TestRun_NothingAddedIsListed(t *testing.T) {
	folder, _, err := runSegment0(t, "=== REPORT ===\n- Loss 0.1: already stated.\n", tinyNote0, "# 0 Tiny\n\npass 1\n")
	if err != nil {
		t.Fatal(err)
	}
	_, items := still(t, folder)
	want := []dropped.Item{{Segment: 0, What: "loss 0.1", Why: "the kept-loss call added no statement to the segment's notes; its report is 1.4-kept-loss-repair/reports/0.md", Loss: "0.1"}}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("dropped %+v, want %+v", items, want)
	}
}

// The call rewrites only the segment's existing notes: any other block is
// dropped, and listed.
func TestRun_DropsBlocks(t *testing.T) {
	for _, c := range []struct{ reply, want string }{
		{"=== DELETE: 0 Tiny.md ===\n=== REPORT ===\nDeleted.\n", "deletes no note"},
		{"=== FILE: 0 Tiny - Aside - Beta.md ===\n# 0 Tiny - Aside - Beta\n=== REPORT ===\nAdded.\n", "adds no note"},
		{"=== FILE: 1 Two.md ===\n# 1 Two\n=== REPORT ===\nOops.\n", "not one of the segment's notes"},
	} {
		folder, _, err := runSegment0(t, c.reply, tinyNote0, "# 0 Tiny\n\npass 1\n")
		if err != nil {
			t.Fatalf("reply %q: %v", c.reply, err)
		}
		_, items := still(t, folder)
		if len(items) != 2 || !strings.Contains(items[0].Why, c.want) || items[1].What != "loss 0.1" {
			t.Errorf("reply %q: dropped %+v, want the block, then loss 0.1 as not added", c.reply, items)
		}
		files := testfixture.Files(t, filepath.Join(folder.Dir, run.KeptLossDir))
		if files[run.NotesDir+"/0 Tiny.md"] != tinyNote0 || files[run.NotesDir+"/1 Two.md"] != tinyNote1 {
			t.Errorf("reply %q: a dropped block changed a note", c.reply)
		}
	}
}

// An edit-shaped reply is briefed by KeptLossEdits, and its insert is made
// through the note model.
func TestRun_EditReply(t *testing.T) {
	reply := "=== EDIT: 0 Tiny.md ===\n--- AFTER ---\nAlpha is first.[^1]\n--- INSERT ---\nBeta is second.[^1]\n" +
		"[^1]: [[Tiny#1. Tiny]] \"Beta is second.\"\n=== REPORT ===\n- Loss 0.1: added.\n"
	folder, calls, err := runSegment0As(t, repair.Edits, reply, tinyNote0Beta, "# 0 Tiny\n\npass 2\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(calls[0].System, briefs.KeptLossEdits+"\n\n"+briefs.Stage1) {
		t.Errorf("the call is not briefed by KeptLossEdits")
	}
	if got := testfixture.Files(t, filepath.Join(folder.Dir, run.KeptLossDir))[run.NotesDir+"/0 Tiny.md"]; got != tinyNote0Beta {
		t.Errorf("note 0:\n got %q\nwant %q", got, tinyNote0Beta)
	}
}

// The call adds statements and changes none, so an edit that replaces one is
// dropped, and listed.
func TestRun_DropsAReplacingEdit(t *testing.T) {
	reply := "=== EDIT: 0 Tiny.md ===\n--- REPLACE ---\nAlpha is first.\n--- WITH ---\n=== REPORT ===\nr\n"
	folder, _, err := runSegment0As(t, repair.Edits, reply, tinyNote0, "# 0 Tiny\n\npass 1\n")
	if err != nil {
		t.Fatal(err)
	}
	_, items := still(t, folder)
	if len(items) != 2 || !strings.Contains(items[0].Why, "replaces a statement, which this call may not do") {
		t.Errorf("dropped %+v, want the replacing edit", items)
	}
}

// The kept-loss brief for edits written quote-once is the quote-once
// kept-loss brief, then the quote-once stage 1 rules. Whole notes have none.
func TestBrief_QuoteOnce(t *testing.T) {
	if file, got := keptloss.Brief(repair.Edits, true); file != briefs.KeptLossEditsQuoteOnceFile || got != briefs.KeptLossEditsQuoteOnce+"\n\n"+briefs.Stage1QuoteOnce {
		t.Errorf("Brief(Edits, true) is not the quote-once kept-loss brief and the quote-once stage 1 rules")
	}
	if file, got := keptloss.Brief(repair.Whole, true); file != briefs.KeptLossFile || got != briefs.KeptLoss+"\n\n"+briefs.Stage1 {
		t.Errorf("Brief(Whole, true) is not the kept-loss brief")
	}
}

// mechcheckRunOn runs step 1.2 on the kept-loss repair's notes in its
// directory, inner, as its re-check does.
func mechcheckRunOn(src *source.Source, inner *run.Folder) error {
	return mechcheck.RunOn(src, inner, run.NotesDir, run.KeptLossMechanicalDir)
}
