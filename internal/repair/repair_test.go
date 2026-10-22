package repair_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/briefs"
	"github.com/corygyarmathy/slipbox/internal/dropped"
	"github.com/corygyarmathy/slipbox/internal/extract"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/readsalone"
	"github.com/corygyarmathy/slipbox/internal/repair"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/supportcheck"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// firstRun runs steps 1.1 to 1.3 into a new run folder for src, replaying
// gen, the generator's reply per segment, briefed quote-once if quoteOnce,
// and support and readsAlone, the checkers' replies per segment.
func firstRun(t *testing.T, src *source.Source, quoteOnce bool, gen, support, readsAlone []string) *run.Folder {
	t.Helper()
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	if err := folder.SaveSource(src.Name, []byte(src.Frame)); err != nil {
		t.Fatal(err)
	}
	var prompts []string
	for _, seg := range src.Segments {
		prompts = append(prompts, extract.Prompt(src, seg))
	}
	if err := extract.Run(context.Background(), testfixture.Replay(t, prompts, gen), src, extract.DefaultModel, extract.DefaultEffort, quoteOnce, folder); err != nil {
		t.Fatal(err)
	}
	if err := mechcheck.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	notes := readNotes(t, folder, run.ExtractNotes)
	prompts = nil
	for _, seg := range src.Segments {
		prompts = append(prompts, supportcheck.Prompt(notes, seg))
	}
	if err := supportcheck.Run(context.Background(), testfixture.Replay(t, prompts, support), src, folder); err != nil {
		t.Fatal(err)
	}
	prompts = nil
	for _, seg := range src.Segments {
		prompts = append(prompts, readsalone.Prompt(notes, seg))
	}
	if err := readsalone.Run(context.Background(), testfixture.Replay(t, prompts, readsAlone), src, folder); err != nil {
		t.Fatal(err)
	}
	return folder
}

func readNotes(t *testing.T, folder *run.Folder, dir string) []*note.Note {
	t.Helper()
	notes, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(dir)))
	if err != nil {
		t.Fatal(err)
	}
	return notes
}

func readFile(t *testing.T, folder *run.Folder, rel ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{folder.Dir}, rel...)...))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func stillFails(t *testing.T, folder *run.Folder) []repair.Entry {
	t.Helper()
	var entries []repair.Entry
	if err := json.Unmarshal([]byte(readFile(t, folder, run.StillFailsDir, run.StillFailsFile)), &entries); err != nil {
		t.Fatal(err)
	}
	return entries
}

// recordedReplies gives the replies a step of the Pragmatism run recorded, by
// segment number.
func recordedReplies(t *testing.T, dir string) map[int]string {
	t.Helper()
	out := map[int]string{}
	for name, text := range testfixture.Files(t, filepath.Join(dir, run.RawDir)) {
		n, err := strconv.Atoi(strings.TrimSuffix(name, ".txt"))
		if err != nil {
			t.Fatal(err)
		}
		out[n] = text
	}
	return out
}

// TestRun_ReplaysThePragmatismRepair runs steps 1.1 to 1.3 as the Pragmatism
// run did, then step 1.4 and its re-check, with the run's recorded replies:
// repair calls replying with edits written quote-once, and Sonnet re-checks
// of the repaired notes. Each call is the request the run made, so the
// repaired notes, which every re-check call's system prompt holds, are the
// run's too.
func TestRun_ReplaysThePragmatismRepair(t *testing.T) {
	src, trial := testfixture.PragmatismSource(t), testfixture.PragmatismRun(t)
	segs := len(src.Segments)
	at := func(dir string) string { return filepath.Join(trial, filepath.FromSlash(dir)) }
	support := testfixture.RawReplies(t, at(run.SupportCheckDir), segs)
	readsAlone := testfixture.RawReplies(t, at(run.ReadsAloneDir), segs)
	folder := firstRun(t, src, true, testfixture.RawReplies(t, at(run.ExtractDir), segs), support, readsAlone)

	// Segments 0 and 3 have no flag that counts: segment 0's are step 1.3a's
	// reads-alone grades, and segment 3 has none. The other six are called.
	recorded := recordedReplies(t, at(run.RepairDir))
	notes := readNotes(t, folder, run.ExtractNotes)
	mech := readFile(t, folder, run.MechanicalCheckDir, run.ReportFile)
	var prompts, replies []string
	var called []int
	for _, seg := range src.Segments {
		if r, ok := recorded[seg.Number]; ok {
			prompts = append(prompts, repair.Prompt(notes, seg, mech, support[seg.Number], readsAlone[seg.Number]))
			replies = append(replies, r)
			called = append(called, seg.Number)
		}
	}
	if want := []int{1, 2, 4, 5, 6, 7}; !reflect.DeepEqual(called, want) {
		t.Fatalf("the run repaired segments %v, want %v", called, want)
	}
	p := testfixture.Replay(t, prompts, replies)
	if err := repair.Run(context.Background(), p, src, "opus", repair.DefaultReply, true, folder); err != nil {
		t.Fatal(err)
	}
	testfixture.SameRequests(t, filepath.Join(folder.Dir, run.RepairDir), at(run.RepairDir), "")

	frame := testfixture.PragmatismFrame(t)
	wantSystem := testfixture.RepoFile(t, "internal/briefs/repair-edits-quote-once.md") + "\n\n" +
		testfixture.RepoFile(t, "internal/briefs/stage-1-quote-once.md") +
		"\n\n=== FRAME: " + testfixture.Pragmatism + ".md ===\n" + frame + "\n=== END FRAME ===\n"
	calls := p.Calls()
	if len(calls) != len(called) {
		t.Fatalf("%d repair calls, want one per flagged segment (%d)", len(calls), len(called))
	}
	for _, c := range calls {
		if c.System != wantSystem {
			t.Errorf("call for %.80q: system prompt is not the quote-once edit brief, the quote-once stage 1 rules and the frame", c.User)
		}
		if c.Model != "opus" || c.Effort != repair.Effort {
			t.Errorf("call for %.80q: model %q at %q, want opus at %q", c.User, c.Model, c.Effort, repair.Effort)
		}
	}
	if calls[0].User != prompts[0] {
		t.Errorf("first call was not segment 1's")
	}

	got := testfixture.Files(t, filepath.Join(folder.Dir, run.RepairDir))
	for i, n := range called {
		if got[run.RawDir+"/"+strconv.Itoa(n)+".txt"] != replies[i] {
			t.Errorf("raw reply %d is not the recorded reply", n)
		}
		if _, ok := got[run.ReportsDir+"/"+strconv.Itoa(n)+".md"]; !ok {
			t.Errorf("segment %d has no repair report", n)
		}
	}
	if items, err := dropped.Read(folder, run.RepairDir); err != nil || len(items) != 0 {
		t.Errorf("dropped %+v, %v; want every edit made", items, err)
	}

	// Segments 4 and 6's replies change no note: the notes of the segments
	// that weren't changed are step 1.1's.
	changed, err := repair.Changed(src, folder)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 2, 5, 7}; !reflect.DeepEqual(changed, want) {
		t.Fatalf("changed segments %v, want %v", changed, want)
	}
	first := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.ExtractNotes)))
	gotNotes := under(got, run.NotesDir)
	if !reflect.DeepEqual(keys(gotNotes), keys(first)) {
		t.Errorf("repaired notes are %q, want step 1.1's names %q", keys(gotNotes), keys(first))
	}
	for name, text := range gotNotes {
		n, _ := strconv.Atoi(name[:1])
		if same := text == first[name]; same == slices.Contains(changed, n) {
			t.Errorf("repaired note %s is step 1.1's: %v, want %v", name, same, !same)
		}
	}
	// The edits are written quote-once, with no footnotes, and s2n traced
	// them.
	for _, n := range changed {
		if strings.Contains(recorded[n], "]: [[") {
			t.Errorf("segment %d's reply writes traces, so s2n traced nothing", n)
		}
	}

	// The re-check calls the checkers on the changed segments only, with
	// the repaired notes, as the run did.
	repaired := readNotes(t, folder, run.RepairNotes)
	recheckSupport := recordedReplies(t, at(run.SupportRecheckDir))
	recheckReadsAlone := recordedReplies(t, at(run.ReadsAloneRecheckDir))
	prompts, replies = nil, nil
	for _, n := range changed {
		prompts = append(prompts, supportcheck.Prompt(repaired, src.Segments[n]), readsalone.Prompt(repaired, src.Segments[n]))
		replies = append(replies, recheckSupport[n], recheckReadsAlone[n])
	}
	recheck := testfixture.Replay(t, prompts, replies)
	if err := repair.Recheck(context.Background(), recheck, src, folder); err != nil {
		t.Fatal(err)
	}
	if n := len(recheck.Calls()); n != 2*len(changed) {
		t.Errorf("%d re-check calls, want %d", n, 2*len(changed))
	}
	for _, dir := range []string{run.SupportRecheckDir, run.ReadsAloneRecheckDir} {
		testfixture.SameRequests(t, filepath.Join(folder.Dir, dir), at(dir), "")
	}

	// Coverage is the repaired notes'. The repairs traced text the first
	// run left uncovered.
	cov := readFile(t, folder, run.MechanicalRecheckDir, run.CoverageFile)
	if want := mechcheck.Check(src, repaired).Coverage.String(); cov != want {
		t.Errorf("re-check coverage is not the repaired notes':\n got %s\nwant %s", cov, want)
	}
	if cov == readFile(t, folder, run.MechanicalCheckDir, run.CoverageFile) {
		t.Errorf("re-check coverage is the first run's")
	}

	if err := repair.WriteStillFails(src, folder); err != nil {
		t.Fatal(err)
	}
	entries := stillFails(t, folder)
	bySegment := map[int]int{}
	twice := 0
	var byQuote []string
	for _, e := range entries {
		if e.File == "" {
			t.Errorf("entry %+v has no note", e)
			continue
		}
		if e.Statement == "" {
			byQuote = append(byQuote, e.Quote)
		} else if len(e.Traces) == 0 {
			t.Errorf("entry %+v has no traces", e)
		}
		n, _ := strconv.Atoi(e.File[:1])
		bySegment[n]++
		if len(e.Flags) > 1 {
			twice++
			if len(e.Flags) != 2 || e.Flags[0].Step != repair.StepSupport || e.Flags[1].Step != repair.StepReadsAlone {
				t.Errorf("entry %.60q has flags %+v, want one from each check", e.Statement, e.Flags)
			}
		}
	}
	// Segments 1 and 7's repairs left nothing failing; segments 4 and 6,
	// which their replies didn't change, carry their first run's flags.
	if want := map[int]int{2: 2, 4: 3, 5: 10, 6: 3}; !reflect.DeepEqual(bySegment, want) {
		t.Errorf("still-failing statements by segment %v, want %v", bySegment, want)
	}
	// A flag no statement holds is listed by its quote: segment 5's on the
	// author name span the run put mid-quote, which no statement's text has
	// whole.
	if want := []string{"…to which I, William James, have opposed…"}; !reflect.DeepEqual(byQuote, want) {
		t.Errorf("entries listed by their quote %q, want %q", byQuote, want)
	}
	// A statement both checks flag is one entry, with both flags.
	if twice != 2 {
		t.Errorf("%d entries with more than one flag, want 2", twice)
	}
	// No second repair call, whatever the re-check found.
	if n := len(p.Calls()); n != len(called) {
		t.Errorf("%d repair calls after the re-check, want %d", n, len(called))
	}
}

// under returns the entries of files under dir/, keyed by the rest of the path.
func under(files map[string]string, dir string) map[string]string {
	out := map[string]string{}
	for k, v := range files {
		if rest, ok := strings.CutPrefix(k, dir+"/"); ok {
			out[rest] = v
		}
	}
	return out
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// tinySource is a two-segment source made for the tests below.
const tinySource = `---
source: "[[@Tiny - A. Author]]"
---
# 1. Tiny

Alpha is first. Beta is second.

## Two

Gamma is third. Delta is fourth.

Epsilon is fifth.
`

// The tiny source's notes: segment 1's has an aside.
const (
	tinyNote0      = "# 0 Tiny\n\nAlpha is first.[^1] Beta is second.[^2]\n\n[^1]: [[Tiny#1. Tiny]] \"Alpha is first.\"\n[^2]: [[Tiny#1. Tiny]] \"Beta is second.\"\n"
	tinyNote1      = "# 1 Two\n\nGamma is third.[^1] Delta is fourth.[^2]\n\n[^1]: [[Tiny#Two]] \"Gamma is third.\"\n[^2]: [[Tiny#Two]] \"Delta is fourth.\"\n"
	tinyNote1Fifth = "# 1 Two - Aside - Fifth\n\nEpsilon is fifth.[^1]\n\n[^1]: [[Tiny#Two]] \"Epsilon is fifth.\"\n"
)

// tinyGen is the generator's reply for each of the tiny source's segments.
var tinyGen = []string{
	"=== FILE: 0 Tiny.md ===\n" + tinyNote0 + "=== REPORT ===\nNone.\n",
	"=== FILE: 1 Two.md ===\n" + tinyNote1 + "=== FILE: 1 Two - Aside - Fifth.md ===\n" + tinyNote1Fifth + "=== REPORT ===\nNone.\n",
}

// tinySupport is step 1.3a's reply for each segment. Segment 0 has only the
// reads-alone grades step 1.3b governs; segment 1 has the name-span grades,
// one of them beside a dangling grade.
var tinySupport = []string{
	"# 0 Tiny\n\n- [dangling] \"Alpha is first.\": \"it\": no antecedent\n- [outside-frame] \"Beta is…\": the last chapter\n\npass 0\n",
	"# 1 Two\n\n- [not a name span] \"Gamma is third.\": not the source's words\n" +
		"- [cut would do] \"Delta is fourth.\": a cut would do\n" +
		"- [dangling / reads wrong] \"Delta…\": reads wrong too\n" +
		"# 1 Two - Aside - Fifth\n\n- [reads wrong] \"Epsilon is fifth.\": reads wrong\n",
}

// tinyReadsAlone is step 1.3b's reply for each segment: none for segment 0,
// and for segment 1 a flag under a heading that names no note.
var tinyReadsAlone = []string{
	"# 0 Tiny\n\npass 2 / dangling 0 / outside-frame 0\n",
	"# 1 Renamed\n\n- [dangling] \"Gamma is…\": \"third\": third of what\n",
}

func tinyRun(t *testing.T) (*source.Source, *run.Folder) {
	t.Helper()
	src, err := source.Parse("Tiny", tinySource)
	if err != nil {
		t.Fatal(err)
	}
	return src, firstRun(t, src, false, tinyGen, tinySupport, tinyReadsAlone)
}

// tinyRepair runs step 1.4's call on tinyRun's folder with reply as segment
// 1's, the only one called, briefed for whole notes.
func tinyRepair(t *testing.T, reply string) (*source.Source, *run.Folder, error) {
	t.Helper()
	src, folder, _, err := tinyRepairAs(t, repair.Whole, false, reply)
	return src, folder, err
}

// tinyRepairAs is tinyRepair briefed for shape and quoteOnce, giving the call
// made too.
func tinyRepairAs(t *testing.T, shape repair.Reply, quoteOnce bool, reply string) (*source.Source, *run.Folder, provider.Request, error) {
	t.Helper()
	src, folder := tinyRun(t)
	notes := readNotes(t, folder, run.ExtractNotes)
	mech := readFile(t, folder, run.MechanicalCheckDir, run.ReportFile)
	if mechBlock := "### 0 Tiny.md\n\n### 1 Two - Aside - Fifth.md\n\n### 1 Two.md\n"; mech != "\n"+mechBlock {
		t.Fatalf("tiny notes have mechanical check flags:\n%s", mech)
	}
	prompt := repair.Prompt(notes, src.Segments[1], mech, tinySupport[1], tinyReadsAlone[1])
	p := testfixture.Replay(t, []string{prompt}, []string{reply})
	err := repair.Run(context.Background(), p, src, "opus", shape, quoteOnce, folder)
	calls := p.Calls()
	if len(calls) != 1 {
		t.Fatalf("%d repair calls, want 1: segment 0 has no flag that counts", len(calls))
	}
	return src, folder, calls[0], err
}

func TestPrompt_Tiny(t *testing.T) {
	src, folder := tinyRun(t)
	notes := readNotes(t, folder, run.ExtractNotes)
	mech := readFile(t, folder, run.MechanicalCheckDir, run.ReportFile)
	got := repair.Prompt(notes, src.Segments[1], mech, "support\n", "reads alone\n")
	want := "=== YOUR SEGMENT'S NOTES (segment 1: Two) ===\n" +
		"--- NOTE: 1 Two - Aside - Fifth ---\n" + tinyNote1Fifth + "\n" +
		"--- NOTE: 1 Two ---\n" + tinyNote1 + "\n" +
		"=== END YOUR SEGMENT'S NOTES (segment 1: Two) ===\n\n" +
		"=== MECHANICAL CHECK ===\n### 1 Two - Aside - Fifth.md\n\n### 1 Two.md\n=== END MECHANICAL CHECK ===\n\n" +
		"=== SUPPORT AND CUTS CHECK ===\nsupport\n\n=== END SUPPORT AND CUTS CHECK ===\n\n" +
		"=== READS-ALONE CHECK ===\nreads alone\n\n=== END READS-ALONE CHECK ===\n"
	if got != want {
		t.Errorf("Prompt:\n got %q\nwant %q", got, want)
	}
	// A segment with no section in the report gets "No lines.".
	if got := repair.Prompt(notes, source.Segment{Number: 7, Heading: "None"}, mech, "", ""); !strings.Contains(got, "=== MECHANICAL CHECK ===\nNo lines.\n") {
		t.Errorf("Prompt for a segment with no report sections:\n%s", got)
	}
}

// A segment whose only flags are step 1.3a's reads-alone grades gets no call.
// A segment whose call changes nothing gets no re-check, and its flags carry
// over.
func TestRun_UnchangedSegmentCarriesItsFlags(t *testing.T) {
	src, folder, err := tinyRepair(t, "=== FILE: 1 Two.md ===\n"+tinyNote1+"=== REPORT ===\nKept every flag.\n")
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := repair.Changed(src, folder); err != nil || len(changed) != 0 {
		t.Fatalf("changed = %v, %v; want none: the reply rewrote a note as it was", changed, err)
	}
	recheck := testfixture.Replay(t, nil, nil)
	if err := repair.Recheck(context.Background(), recheck, src, folder); err != nil {
		t.Fatal(err)
	}
	if n := len(recheck.Calls()); n != 0 {
		t.Errorf("%d re-check calls, want none", n)
	}
	if err := repair.WriteStillFails(src, folder); err != nil {
		t.Fatal(err)
	}
	got := stillFails(t, folder)
	two := []string{`[^1]: [[Tiny#Two]] "Gamma is third."`}
	want := []repair.Entry{
		{
			File: "1 Two - Aside - Fifth.md", Statement: "Epsilon is fifth.[^1]",
			Traces: []string{`[^1]: [[Tiny#Two]] "Epsilon is fifth."`}, Grades: []string{"reads wrong"},
			Flags: []repair.Flag{{Step: repair.StepSupport, Grades: []string{"reads wrong"}, Quote: "Epsilon is fifth.", Reason: "reads wrong"}},
		},
		{
			File: "1 Two.md", Statement: "Gamma is third.[^1]", Traces: two, Grades: []string{"not a name span"},
			Flags: []repair.Flag{{Step: repair.StepSupport, Grades: []string{"not a name span"}, Quote: "Gamma is third.", Reason: "not the source's words"}},
		},
		{
			File: "1 Two.md", Statement: "Delta is fourth.[^2]",
			Traces: []string{`[^2]: [[Tiny#Two]] "Delta is fourth."`}, Grades: []string{"cut would do", "reads wrong"},
			Flags: []repair.Flag{
				{Step: repair.StepSupport, Grades: []string{"cut would do"}, Quote: "Delta is fourth.", Reason: "a cut would do"},
				{Step: repair.StepSupport, Grades: []string{"reads wrong"}, Quote: "Delta…", Reason: "reads wrong too"},
			},
		},
		{
			File: "", Quote: "Gamma is…", Traces: []string{}, Grades: []string{"dangling"},
			Flags: []repair.Flag{{Step: repair.StepReadsAlone, Grades: []string{"dangling"}, Quote: "Gamma is…", Reason: `"third": third of what`}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("still fails:\n got %+v\nwant %+v", got, want)
	}
}

// Step 1.4 reads step 1.2's findings from its directory, and without them it
// stops, naming the file, rather than running the check again.
func TestRun_NeedsTheMechanicalCheckFindings(t *testing.T) {
	src, folder := tinyRun(t)
	findings := filepath.Join(folder.Dir, run.MechanicalCheckDir, run.FindingsFile)
	if err := os.Remove(findings); err != nil {
		t.Fatal(err)
	}
	p := testfixture.Replay(t, nil, nil)
	err := repair.Run(context.Background(), p, src, "opus", repair.Whole, false, folder)
	if err == nil || !strings.Contains(err.Error(), findings) {
		t.Fatalf("err = %v, want one naming %s", err, findings)
	}
	if len(p.Calls()) != 0 {
		t.Errorf("%d repair calls, want none", len(p.Calls()))
	}
}

// The still-fails list reads step 1.2's findings from the re-check's
// directory, and without them it stops, naming the file.
func TestWriteStillFails_NeedsTheRecheckFindings(t *testing.T) {
	src, folder, err := tinyRepair(t, "=== FILE: 1 Two.md ===\n"+tinyNote1+"=== REPORT ===\nKept every flag.\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := repair.Recheck(context.Background(), testfixture.Replay(t, nil, nil), src, folder); err != nil {
		t.Fatal(err)
	}
	findings := filepath.Join(folder.Dir, run.MechanicalRecheckDir, run.FindingsFile)
	if err := os.Remove(findings); err != nil {
		t.Fatal(err)
	}
	if err := repair.WriteStillFails(src, folder); err == nil || !strings.Contains(err.Error(), findings) {
		t.Fatalf("err = %v, want one naming %s", err, findings)
	}
}

// A block of a reply that can't be applied is dropped, and listed with why,
// and the step goes on: its other blocks are applied, and the note it named
// is left as it was.
func TestRun_DropsBlocks(t *testing.T) {
	fixed := "=== FILE: 1 Two - Aside - Fifth.md ===\n# 1 Two - Aside - Fifth\n\nEpsilon is fifth, fixed.[^1]\n\n[^1]: [[Tiny#Two]] \"Epsilon is fifth.\"\n"
	for _, c := range []struct{ name, block, note, want string }{
		{"the main note deleted", "=== DELETE: 1 Two.md ===\n", "1 Two.md", "main note"},
		{"a missing note deleted", "=== DELETE: 1 Two - Aside - Nope.md ===\n", "1 Two - Aside - Nope.md", "not a note"},
		{"a new note", "=== FILE: 1 Two - Aside - New.md ===\n# 1 Two - Aside - New\n", "1 Two - Aside - New.md", "not a note"},
		{"another segment's note", "=== FILE: 0 Tiny.md ===\n# 0 Tiny\n", "0 Tiny.md", "not one of the segment's notes"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, folder, err := tinyRepair(t, c.block+fixed+"=== REPORT ===\nr\n")
			if err != nil {
				t.Fatal(err)
			}
			checkDropped(t, folder, c.note, c.want)
			files := testfixture.Files(t, filepath.Join(folder.Dir, run.RepairDir))
			if files[run.NotesDir+"/0 Tiny.md"] != tinyNote0 || files[run.NotesDir+"/1 Two.md"] != tinyNote1 {
				t.Errorf("a dropped block changed a note")
			}
			if !strings.Contains(files[run.NotesDir+"/1 Two - Aside - Fifth.md"], "fixed") {
				t.Errorf("the reply's other block is not applied")
			}
		})
	}
}

// checkDropped checks that step 1.4 in folder is complete and dropped one
// item, of the note named name, why it contains want.
func checkDropped(t *testing.T, folder *run.Folder, name, want string) {
	t.Helper()
	if !folder.Complete(run.RepairDir) {
		t.Fatalf("the repair step is not complete")
	}
	items, err := dropped.Read(folder, run.RepairDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Segment != 1 || items[0].Note != name || !strings.Contains(items[0].Why, want) {
		t.Errorf("dropped %+v, want one item of %q saying %q", items, name, want)
	}
}

// A call that deletes an aside changes its segment, which alone is
// re-checked, on the repaired notes.
func TestRun_DeletesAnAside(t *testing.T) {
	src, folder, err := tinyRepair(t, "=== DELETE: 1 Two - Aside - Fifth.md ===\n=== REPORT ===\nDropped the aside.\n")
	if err != nil {
		t.Fatal(err)
	}
	repaired := readNotes(t, folder, run.RepairNotes)
	var names []string
	for _, n := range repaired {
		names = append(names, n.Name)
	}
	if want := []string{"0 Tiny.md", "1 Two.md"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("repaired notes %q, want %q", names, want)
	}
	if changed, err := repair.Changed(src, folder); err != nil || !reflect.DeepEqual(changed, []int{1}) {
		t.Fatalf("changed = %v, %v; want [1]", changed, err)
	}
	seg := src.Segments[1]
	recheck := testfixture.Replay(t,
		[]string{supportcheck.Prompt(repaired, seg), readsalone.Prompt(repaired, seg)},
		[]string{"# 1 Two\n\npass 2\n", "# 1 Two\n\n- [outside-frame] \"Delta is fourth.\": as we saw\n"})
	if err := repair.Recheck(context.Background(), recheck, src, folder); err != nil {
		t.Fatal(err)
	}
	if n := len(recheck.Calls()); n != 2 {
		t.Errorf("%d re-check calls, want 2", n)
	}
	if err := repair.WriteStillFails(src, folder); err != nil {
		t.Fatal(err)
	}
	// The re-check's flags replace the first run's for the changed segment.
	got := stillFails(t, folder)
	if len(got) != 1 || got[0].File != "1 Two.md" || got[0].Statement != "Delta is fourth.[^2]" || !reflect.DeepEqual(got[0].Grades, []string{"outside-frame"}) {
		t.Errorf("still fails: %+v, want only the re-check's outside-frame flag", got)
	}
}

// An edit-shaped reply is briefed by RepairEdits and made through the note
// model: a statement replaced, and one split into two quotes.
func TestRun_EditReply(t *testing.T) {
	reply := "=== EDIT: 1 Two.md ===\n" +
		"--- REPLACE ---\nGamma is third.[^1]\n--- WITH ---\nGamma[^1] is third.[^2]\n" +
		"[^1]: [[Tiny#Two]] \"Gamma\"\n[^2]: [[Tiny#Two]] \"is third.\"\n" +
		"--- REPLACE ---\nDelta is fourth.[^2]\n--- WITH ---\nDelta is fourth, as Gamma is third.[^1]\n" +
		"[^1]: [[Tiny#Two]] \"Delta is fourth.\"\n" +
		"=== REPORT ===\nFixed two.\n"
	src, folder, call, err := tinyRepairAs(t, repair.Edits, false, reply)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(call.System, briefs.RepairEdits+"\n\n"+briefs.Stage1) {
		t.Errorf("the call is not briefed by RepairEdits")
	}
	want := "# 1 Two\n\nGamma[^1] is third.[^2] Delta is fourth, as Gamma is third.[^3]\n\n" +
		"[^1]: [[Tiny#Two]] \"Gamma\"\n[^2]: [[Tiny#Two]] \"is third.\"\n[^3]: [[Tiny#Two]] \"Delta is fourth.\"\n"
	files := testfixture.Files(t, filepath.Join(folder.Dir, run.RepairDir))
	if got := files[run.NotesDir+"/1 Two.md"]; got != want {
		t.Errorf("repaired note:\n got %q\nwant %q", got, want)
	}
	if files[run.NotesDir+"/1 Two - Aside - Fifth.md"] != tinyNote1Fifth || files[run.ReportsDir+"/1.md"] != "Fixed two.\n" {
		t.Errorf("the aside or the report is not as the reply left them")
	}
	if usage := files[run.UsageFile]; !strings.Contains(usage, `"brief":"repair-edits.md"`) {
		t.Errorf("usage log does not name the edit brief:\n%s", usage)
	}
	if changed, err := repair.Changed(src, folder); err != nil || !reflect.DeepEqual(changed, []int{1}) {
		t.Errorf("changed = %v, %v; want [1]", changed, err)
	}
}

// The whole-note brief is the trialled one, with quote-once or without, since
// whole notes have no quote-once brief.
func TestBrief_Whole(t *testing.T) {
	for _, quoteOnce := range []bool{false, true} {
		if file, got := repair.Brief(repair.Whole, quoteOnce); file != briefs.RepairFile || got != briefs.Repair+"\n\n"+briefs.Stage1 {
			t.Errorf("Brief(Whole, %v) is not the trialled brief", quoteOnce)
		}
	}
	if file, got := repair.Brief(repair.Edits, true); file != briefs.RepairEditsQuoteOnceFile || got != briefs.RepairEditsQuoteOnce+"\n\n"+briefs.Stage1QuoteOnce {
		t.Errorf("Brief(Edits, true) is not the quote-once edit brief and the quote-once stage 1 rules")
	}
}

// A whole-note reply's call is briefed by the trialled whole-note brief and
// the stage 1 rules, then the frame, and the usage log names that brief.
func TestRun_WholeReplyIsBriefedByRepair(t *testing.T) {
	_, folder, call, err := tinyRepairAs(t, repair.Whole, false, "=== FILE: 1 Two.md ===\n"+tinyNote1+"=== REPORT ===\nKept every flag.\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(call.System, briefs.Repair+"\n\n"+briefs.Stage1+"\n\n=== FRAME: ") {
		t.Errorf("the call is not briefed by repair.md and stage-1.md, then the frame")
	}
	usage := testfixture.Files(t, filepath.Join(folder.Dir, run.RepairDir))[run.UsageFile]
	if !strings.Contains(usage, `"brief":"`+briefs.RepairFile+`"`) {
		t.Errorf("usage log does not name the whole-note brief:\n%s", usage)
	}
}

// With quote-once, edits written with their joining text marked and no
// footnotes are traced by s2n before they are made. A note given in full with
// footnotes is taken as written.
func TestRun_QuoteOnceEditReply(t *testing.T) {
	reply := "=== EDIT: 1 Two.md ===\n" +
		"--- REPLACE ---\nGamma is third.[^1]\n--- WITH ---\nGamma{} is third.\n" +
		"--- REPLACE ---\nDelta is fourth.[^2]\n--- WITH ---\nDelta is fourth{, as }Gamma is third.\n" +
		"=== FILE: 1 Two - Aside - Fifth.md ===\n" + tinyNote1Fifth +
		"=== REPORT ===\nFixed two.\n"
	_, folder, call, err := tinyRepairAs(t, repair.Edits, true, reply)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(call.System, briefs.RepairEditsQuoteOnce+"\n\n"+briefs.Stage1QuoteOnce) {
		t.Errorf("the call is not briefed by RepairEditsQuoteOnce")
	}
	want := "# 1 Two\n\nGamma[^1] is third.[^2] Delta is fourth,[^3] as Gamma is third.[^4]\n\n" +
		"[^1]: [[Tiny#Two]] \"Gamma\"\n[^2]: [[Tiny#Two]] \"is third.\"\n[^3]: [[Tiny#Two]] \"Delta is fourth\"\n[^4]: [[Tiny#Two]] \"Gamma is third.\"\n"
	files := testfixture.Files(t, filepath.Join(folder.Dir, run.RepairDir))
	if got := files[run.NotesDir+"/1 Two.md"]; got != want {
		t.Errorf("repaired note:\n got %q\nwant %q", got, want)
	}
	if files[run.NotesDir+"/1 Two - Aside - Fifth.md"] != tinyNote1Fifth {
		t.Errorf("the aside is not as the reply gave it")
	}
	if usage := files[run.UsageFile]; !strings.Contains(usage, `"brief":"repair-edits-quote-once.md"`) {
		t.Errorf("usage log does not name the quote-once edit brief:\n%s", usage)
	}
}

// An edit the note model can't make is dropped and listed, and the block's
// other edits are made. A block that doesn't parse as edits, or names a note
// that doesn't exist, or one the reply also deletes, is dropped whole, and
// changes no note.
func TestRun_DropsEdits(t *testing.T) {
	good := "--- REPLACE ---\nDelta is fourth.[^2]\n--- WITH ---\nDelta is fourth, fixed.[^1]\n[^1]: [[Tiny#Two]] \"Delta is fourth.\"\n"
	for _, c := range []struct {
		name, reply, note, want string
		fixed                   bool
	}{
		{"no such statement", "=== EDIT: 1 Two.md ===\n--- REPLACE ---\nZeta is last.\n--- WITH ---\n" + good, "1 Two.md", "no statement in 1 Two.md reads", true},
		{"a reference with no definition", "=== EDIT: 1 Two.md ===\n--- REPLACE ---\nGamma is third.\n--- WITH ---\nGamma is third.[^1]\n", "1 Two.md", "has no definition", false},
		{"a missing note", "=== EDIT: 1 Two - Aside - Nope.md ===\n--- REPLACE ---\nA.\n--- WITH ---\n", "1 Two - Aside - Nope.md", "not a note", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, folder, _, err := tinyRepairAs(t, repair.Edits, false, c.reply+"=== REPORT ===\nr\n")
			if err != nil {
				t.Fatal(err)
			}
			checkDropped(t, folder, c.note, c.want)
			got := testfixture.Files(t, filepath.Join(folder.Dir, run.RepairDir))[run.NotesDir+"/1 Two.md"]
			if fixed := strings.Contains(got, "fixed"); fixed != c.fixed || (!c.fixed && got != tinyNote1) {
				t.Errorf("1 Two.md = %q; want the good edit made: %v", got, c.fixed)
			}
		})
	}
	t.Run("a note edited and deleted", func(t *testing.T) {
		_, folder, _, err := tinyRepairAs(t, repair.Edits, false, "=== EDIT: 1 Two - Aside - Fifth ===\n--- REPLACE ---\nEpsilon is fifth.\n--- WITH ---\n=== DELETE: 1 Two - Aside - Fifth.md ===\n=== REPORT ===\nr\n")
		if err != nil {
			t.Fatal(err)
		}
		items, err := dropped.Read(folder, run.RepairDir)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 2 || !strings.Contains(items[0].Why, "both edits") || !strings.Contains(items[1].Why, "both edits") {
			t.Errorf("dropped %+v, want both blocks", items)
		}
		if got := testfixture.Files(t, filepath.Join(folder.Dir, run.RepairDir))[run.NotesDir+"/1 Two - Aside - Fifth.md"]; got != tinyNote1Fifth {
			t.Errorf("the aside is not as it was: %q", got)
		}
	})
}

// Of two edits that can each be made alone but not together, at
// overlapping statements, the later is dropped and the earlier made.
func TestEditNote_DropsOverlappingEdits(t *testing.T) {
	body := "--- REPLACE ---\nGamma is third.[^1] Delta is fourth.[^2]\n--- WITH ---\n" +
		"--- REPLACE ---\nDelta is fourth.[^2]\n--- WITH ---\n"
	text, drops, err := repair.EditNote("1 Two.md", tinyNote1, body, nil, note.Replace)
	if err != nil {
		t.Fatal(err)
	}
	if text != "# 1 Two\n\n" || len(drops) != 1 || drops[0].Statement != "Delta is fourth.[^2]" {
		t.Errorf("EditNote = %q, %+v; want the first edit made and the second dropped", text, drops)
	}
}

// An edit that clashes with nothing is made, though two others in its block
// clash with each other: only the failing item is dropped.
func TestEditNote_KeepsAnEditClearOfAClash(t *testing.T) {
	text := "# 0 Tiny\n\nAlpha is first.[^1]\n\nBeta is second.[^2]\n\n[^1]: [[Tiny#1. Tiny]] \"Alpha is first.\"\n[^2]: [[Tiny#1. Tiny]] \"Beta is second.\"\n"
	body := "--- AFTER ---\nAlpha is first.\n--- INSERT ---\nOne.\n--- AFTER ---\nAlpha is first.\n--- INSERT ---\nTwo.\n--- AFTER ---\nBeta is second.\n--- INSERT ---\nThree.\n"
	out, drops, err := repair.EditNote("0 Tiny.md", text, body, nil, note.Replace, note.Insert)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Alpha is first.[^1] One.") || strings.Contains(out, "Two.") || !strings.Contains(out, "Beta is second.[^2] Three.") {
		t.Errorf("EditNote = %q; want the first and third edits made", out)
	}
	if len(drops) != 1 || drops[0].Statement != "Alpha is first." {
		t.Errorf("dropped %+v, want the second edit", drops)
	}
}

// An edit whose operation the call may not make is dropped, and the others
// are made.
func TestEditNote_DropsAnOpNotAllowed(t *testing.T) {
	body := "--- AFTER ---\nGamma is third.[^1]\n--- INSERT ---\nNew.[^1]\n[^1]: [[Tiny#Two]] \"New.\"\n" +
		"--- REPLACE ---\nDelta is fourth.[^2]\n--- WITH ---\n"
	text, drops, err := repair.EditNote("1 Two.md", tinyNote1, body, nil, note.Insert)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "New.") || !strings.Contains(text, "Delta is fourth.") {
		t.Errorf("EditNote = %q, want the insert made and the replace not", text)
	}
	if len(drops) != 1 || drops[0].Statement != "Delta is fourth.[^2]" || !strings.Contains(drops[0].Why, "may not do") {
		t.Errorf("dropped %+v, want the replace", drops)
	}
}
