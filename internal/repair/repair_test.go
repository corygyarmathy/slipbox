package repair_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/briefs"
	"github.com/corygyarmathy/slipbox/internal/dropped"
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

// The invented chapter's notes, by file name.
const (
	keepers = "0 Keepers of the Lamp.md"
	watch   = "1 The Keeper's Watch.md"
	aside   = "1 The Keeper's Watch - Aside - The log.md"
	lens    = "2 The Lens.md"
	storms  = "3 Storms.md"
)

// Traces into the invented chapter's sections, as footnotes give them.
func watchTrace(quote string) string  { return testfixture.LampTrace("The Keeper’s Watch", quote) }
func lensTrace(quote string) string   { return testfixture.LampTrace("The Lens", quote) }
func stormsTrace(quote string) string { return testfixture.LampTrace("Storms", quote) }

// segment is the key a fake matches a segment's call on: the label the
// checkers' and the repair call's user prompts give it.
func segment(n int) string { return fmt.Sprintf("(segment %d: ", n) }

// checkedRun makes a run folder for the invented chapter whose steps 1.1 to
// 1.3 are done: LampStage1 as step 1.1's notes, with notes in place of any it
// names, step 1.2 run on them, and steps 1.3a and 1.3b run with support and
// readsAlone as their replies, by segment number.
func checkedRun(t *testing.T, notes map[string]string, support, readsAlone map[int]string) (*source.Source, *run.Folder) {
	t.Helper()
	src, folder := testfixture.LampExtractRun(t)
	testfixture.WriteFiles(t, filepath.Join(folder.Dir, filepath.FromSlash(run.ExtractNotes)), notes)
	if err := mechcheck.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	serve := func(replies map[int]string) *testfixture.Fake {
		var cases []testfixture.Case
		for n, r := range replies {
			cases = append(cases, testfixture.Case{Contains: segment(n), Reply: r})
		}
		return testfixture.Serve(t, cases...)
	}
	if err := supportcheck.Run(context.Background(), serve(support), src, folder); err != nil {
		t.Fatal(err)
	}
	if err := readsalone.Run(context.Background(), serve(readsAlone), src, folder); err != nil {
		t.Fatal(err)
	}
	return src, folder
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

// segmentNotes gives the notes in files, by file name, whose names start
// with segment n's number.
func segmentNotes(files map[string]string, n int) map[string]string {
	out := map[string]string{}
	for name, text := range files {
		if strings.HasPrefix(name, strconv.Itoa(n)+" ") {
			out[name] = text
		}
	}
	return out
}

// wantSystem is the system prompt of a repair call on the invented chapter
// briefed by brief, then rules, files in internal/briefs/. It is built here
// rather than with repair.Brief, so a fault there still shows.
func wantSystem(t *testing.T, brief, rules string) string {
	t.Helper()
	return testfixture.RepoFile(t, "internal/briefs/"+brief) + "\n\n" + testfixture.RepoFile(t, "internal/briefs/"+rules) +
		"\n\n=== FRAME: " + testfixture.Lamp + ".md ===\n" + testfixture.LampChapter + "\n=== END FRAME ===\n"
}

// TestBrief checks each reply shape's brief against the brief files. Whole
// notes have no quote-once brief, so they are briefed as trialled with
// quote-once or without.
func TestBrief(t *testing.T) {
	for _, c := range []struct {
		shape        repair.Reply
		quoteOnce    bool
		brief, rules string
	}{
		{repair.Whole, false, "repair.md", "stage-1.md"},
		{repair.Whole, true, "repair.md", "stage-1.md"},
		{repair.Edits, false, "repair-edits.md", "stage-1.md"},
		{repair.Edits, true, "repair-edits-quote-once.md", "stage-1-quote-once.md"},
	} {
		file, got := repair.Brief(c.shape, c.quoteOnce)
		want := testfixture.RepoFile(t, "internal/briefs/"+c.brief) + "\n\n" + testfixture.RepoFile(t, "internal/briefs/"+c.rules)
		if file != c.brief || got != want {
			t.Errorf("Brief(%s, %v) is %s and its text, want %s, a blank line, then %s", c.shape, c.quoteOnce, file, c.brief, c.rules)
		}
	}
}

// The first run TestPrompt and TestRun read, on the invented chapter.
var (
	// runNotes replaces segment 3's note with one whose first statement's
	// trace is broken, on a line with another statement, so step 1.2 fails
	// it and segment 3 has no other flag.
	runNotes = map[string]string{
		storms: "# 3 Storms\n\n" +
			"In a storm the light matters most.[^1] A wreck was his failure, whatever the weather.[^2]\n" +
			"He stayed at the lamp (in 1839, for nine days) through every gale.[^3]\n\n" +
			"[^1]: " + stormsTrace("In a calm the sea is kind.") + "\n" +
			"[^2]: " + stormsTrace("failure, whatever the weather") + "\n" +
			"[^3]: " + stormsTrace("stayed at the lamp (in 1839, for nine days) through every gale") + "\n",
	}
	// runSupport is step 1.3a's reply per segment. Segment 0's flags have
	// only the dangling and outside-frame grades step 1.3b governs, which
	// don't count. Segment 2's flag has one of them beside a grade that
	// counts.
	runSupport = map[int]string{
		0: "# 0 Keepers of the Lamp\n\n" +
			"- [dangling] \"It burns whether or not a ship is near.\": \"It\": the lighthouse, named in the statement before.\n" +
			"- [outside-frame] \"The keeper kept that promise…\": \"The keeper\": the keeper the last chapter introduced.\n" +
			"pass 1 / dangling 1 / outside-frame 1\n",
		1: "# 1 The Keeper's Watch\n\n" +
			"- [cut would do] \"He trimmed the wick…\": a cut to \"The keeper\" would name him.\n" +
			"pass 2 / cut would do 1\n\n" +
			"# 1 The Keeper's Watch - Aside - The log\n\npass 2\n",
		2: "# 2 The Lens\n\n" +
			"- [dangling / weak] \"Obviously, a keeper can take no notice…\": the section says a keeper can, not that one does.\n" +
			"pass 4 / weak 1\n",
		3: "# 3 Storms\n\npass 3\n",
	}
	// runReadsAlone is step 1.3b's reply per segment. Segment 2's report
	// flags the statement step 1.3a did, quoted whole, and one under
	// another segment's note's H1, which names none of segment 2's notes.
	runReadsAlone = map[int]string{
		0: "# 0 Keepers of the Lamp\n\npass 3 / dangling 0 / outside-frame 0\n",
		1: "# 1 The Keeper's Watch\n\n" +
			"- [dangling] \"He trimmed the wick every four hours.\": \"He\": no earlier statement names who.\n" +
			"pass 2 / dangling 1 / outside-frame 0\n\n" +
			"# 1 The Keeper's Watch - Aside - The log\n\n" +
			"- [dangling] \"Then they read every page.\": \"they\": the note never says who read it.\n" +
			"pass 1 / dangling 1 / outside-frame 0\n",
		2: "# 2 The Lens\n\n" +
			"- [dangling] \"Obviously, a keeper can take no notice of the lens on a clear night.\": \"the lens\": which lens is not said.\n" +
			"pass 4 / dangling 1 / outside-frame 0\n\n" +
			"# 3 Storms\n\n" +
			"- [dangling] \"He stayed at the lamp…\": \"He\": no earlier statement names who.\n",
		3: "# 3 Storms\n\npass 3 / dangling 0 / outside-frame 0\n",
	}
)

// TestPrompt pins each segment's user prompt on the first run: the
// segment's notes in full, its notes' sections of step 1.2's report, then its
// step 1.3a and 1.3b reports as received.
func TestPrompt(t *testing.T) {
	src, folder := checkedRun(t, runNotes, runSupport, runReadsAlone)
	notes := readNotes(t, folder, run.ExtractNotes)
	mech := readFile(t, folder, run.MechanicalCheckDir, run.ReportFile)
	for _, seg := range src.Segments {
		got := repair.Prompt(notes, seg, mech, runSupport[seg.Number], runReadsAlone[seg.Number])
		testfixture.Golden(t, "prompt/"+strconv.Itoa(seg.Number)+".txt", got)
		// The golden files can't say why a section of step 1.2's report is
		// in the prompt: it is one of the segment's own notes'.
		for _, n := range notes {
			in := strings.Contains(got, "### "+n.Name+"\n")
			own := strings.HasPrefix(n.Name, strconv.Itoa(seg.Number)+" ")
			if in != own {
				t.Errorf("%s: step 1.2's section of %q in the prompt: %v, want %v", seg, n.Name, in, own)
			}
		}
	}
	// A segment with no section in the report gets "No lines.".
	if got := repair.Prompt(notes, source.Segment{Number: 7, Heading: "None"}, mech, "", ""); !strings.Contains(got, "=== MECHANICAL CHECK ===\nNo lines.\n") {
		t.Errorf("Prompt for a segment with no report sections:\n%s", got)
	}
}

// runCase is a called segment's repair reply in TestRun, and the notes, by
// file name, and report it should leave.
type runCase struct {
	name    string
	segment int
	reply   string
	notes   map[string]string
	report  string
}

// runCases are the repair replies of TestRun's three called segments, in
// the default shape: edits, written quote-once.
var runCases = []runCase{
	// Quote-once edits to the main note and the aside. s2n traces each, the
	// name span "The keeper" as a quote of its own, and numbers the new
	// footnotes after the note's.
	{"quote-once edits to the main note and the aside", 1,
		"=== EDIT: " + watch + " ===\n" +
			"--- REPLACE ---\nHe trimmed the wick every four hours.[^2]\n--- WITH ---\nThe keeper{} trimmed the wick every four hours.\n" +
			"=== EDIT: " + aside + " ===\n" +
			"--- REPLACE ---\nThen they read every page.[^2]\n--- WITH ---\nThen the inspectors read every page.\n" +
			"=== REPORT ===\nNamed the keeper and the inspectors.\n",
		map[string]string{
			watch: "# 1 The Keeper's Watch\n\n" +
				"The watch began at dusk.[^1]\n" +
				"The keeper[^2] trimmed the wick every four hours.[^3]\n" +
				"He logged the weather in a book.[^4]\n\n" +
				"[^1]: " + watchTrace("The watch began at dusk.") + "\n" +
				"[^2]: " + watchTrace("The keeper") + "\n" +
				"[^3]: " + watchTrace("trimmed the wick every four hours") + "\n" +
				"[^4]: " + watchTrace("logged the weather in a book") + "\n",
			aside: "# 1 The Keeper's Watch - Aside - The log\n\n" +
				"The log was read by nobody until a ship was lost.[^1]\n" +
				"Then the inspectors read every page.[^2]\n\n" +
				"[^1]: " + watchTrace("The log was read by nobody until a ship was lost.") + "\n" +
				"[^2]: " + watchTrace("Then the inspectors read every page.") + "\n",
		},
		"Named the keeper and the inspectors.\n"},
	// A reply that keeps every flag changes no note.
	{"a reply that changes no note", 2,
		"=== REPORT ===\nKept the flag: the statement is the section's.\n",
		map[string]string{lens: testfixture.LampStage1()[lens]},
		"Kept the flag: the statement is the section's.\n"},
	// A quote-once insert of the author's first-person sentence with the
	// author's name set into it, which s2n traces to the source line. The
	// broken trace step 1.2 failed is left as it was.
	{"a quote-once insert with the author's name set in, the broken trace left", 3,
		"=== EDIT: " + storms + " ===\n" +
			"--- AFTER ---\nHe stayed at the lamp (in 1839, for nine days) through every gale.[^3]\n" +
			"--- INSERT ---\nI{, }Rhea Vale{, }have stood in that lamp room in a gale.\n" +
			"=== REPORT ===\nAdded the author's own account.\n",
		map[string]string{storms: "# 3 Storms\n\n" +
			"In a storm the light matters most.[^1] A wreck was his failure, whatever the weather.[^2]\n" +
			"He stayed at the lamp (in 1839, for nine days) through every gale.[^3] I, Rhea Vale,[^4] have stood in that lamp room in a gale.[^5]\n\n" +
			"[^1]: " + stormsTrace("In a calm the sea is kind.") + "\n" +
			"[^2]: " + stormsTrace("failure, whatever the weather") + "\n" +
			"[^3]: " + stormsTrace("stayed at the lamp (in 1839, for nine days) through every gale") + "\n" +
			"[^4]: [[" + testfixture.Lamp + "]] \"Rhea Vale\"\n" +
			"[^5]: " + stormsTrace("I have stood in that lamp room in a gale") + "\n"},
		"Added the author's own account.\n"},
}

// recheckSupport and recheckReadsAlone are the re-check's replies for the
// segments the repair call changed, 1 and 3. Segment 1's repair left nothing
// failing. Segment 3's flag a quote that starts mid-statement, at the
// author's name, and the statement that shares a line with the one whose
// trace is broken.
var (
	recheckSupport = map[int]string{
		1: "# 1 The Keeper's Watch\n\npass 3\n\n# 1 The Keeper's Watch - Aside - The log\n\npass 2\n",
		3: "# 3 Storms\n\n" +
			"- [reads wrong] \"…I, Rhea Vale, have stood…\": the author's name set into the first person reads oddly.\n" +
			"pass 3 / reads wrong 1\n",
	}
	recheckReadsAlone = map[int]string{
		1: "# 1 The Keeper's Watch\n\npass 3 / dangling 0 / outside-frame 0\n\n" +
			"# 1 The Keeper's Watch - Aside - The log\n\npass 2 / dangling 0 / outside-frame 0\n",
		3: "# 3 Storms\n\n" +
			"- [dangling] \"A wreck was his failure…\": \"his\": no earlier statement says whose failure.\n" +
			"pass 3 / dangling 1 / outside-frame 0\n",
	}
)

// TestRun runs step 1.4 on the first run in the default shape, then its
// re-check and the still-fails list, and checks the calls each made and
// what each wrote.
func TestRun(t *testing.T) {
	src, folder := checkedRun(t, runNotes, runSupport, runReadsAlone)
	first := readNotes(t, folder, run.ExtractNotes)
	mech := readFile(t, folder, run.MechanicalCheckDir, run.ReportFile)

	var cases []testfixture.Case
	for _, c := range runCases {
		cases = append(cases, testfixture.Case{Contains: segment(c.segment), Reply: c.reply})
	}
	p := testfixture.Serve(t, cases...)
	if err := repair.Run(context.Background(), p, src, "opus", repair.DefaultReply, true, folder); err != nil {
		t.Fatal(err)
	}

	files := testfixture.Files(t, filepath.Join(folder.Dir, run.RepairDir))
	repaired := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.RepairNotes)))
	for _, c := range runCases {
		t.Run(c.name, func(t *testing.T) {
			n := strconv.Itoa(c.segment)
			if got := files[run.RawDir+"/"+n+".txt"]; got != c.reply {
				t.Errorf("segment %d: raw reply is not the reply:\n got %q\nwant %q", c.segment, got, c.reply)
			}
			if got := files[run.ReportsDir+"/"+n+".md"]; got != c.report {
				t.Errorf("segment %d: report %q, want %q", c.segment, got, c.report)
			}
			if got := segmentNotes(repaired, c.segment); !reflect.DeepEqual(got, c.notes) {
				t.Errorf("segment %d: repaired notes:\n got %q\nwant %q", c.segment, got, c.notes)
			}
		})
	}
	// Segment 0, which has no call, keeps step 1.1's notes.
	if got, want := segmentNotes(repaired, 0), segmentNotes(testfixture.LampStage1(), 0); !reflect.DeepEqual(got, want) {
		t.Errorf("segment 0's notes are not step 1.1's:\n got %q\nwant %q", got, want)
	}
	if items, err := dropped.Read(folder, run.RepairDir); err != nil || len(items) != 0 {
		t.Errorf("dropped %+v, %v; want every edit made", items, err)
	}

	// One call per segment with a flag that counts, each with its prompt,
	// briefed by the quote-once edit brief and rules, at opus and medium.
	system := wantSystem(t, "repair-edits-quote-once.md", "stage-1-quote-once.md")
	calls := p.Calls()
	if len(calls) != len(runCases) {
		t.Fatalf("%d repair calls, want one per segment with a flag that counts (%d)", len(calls), len(runCases))
	}
	users := map[string]bool{}
	for _, c := range calls {
		if c.System != system {
			t.Errorf("call for %.80q: system prompt is not the quote-once edit brief, the quote-once stage 1 rules and the frame", c.User)
		}
		if c.Model != "opus" || c.Effort != "medium" {
			t.Errorf("call for %.80q: model %q at effort %q, want opus at medium", c.User, c.Model, c.Effort)
		}
		users[c.User] = true
	}
	for _, c := range runCases {
		seg := src.Segments[c.segment]
		if want := repair.Prompt(first, seg, mech, runSupport[c.segment], runReadsAlone[c.segment]); !users[want] {
			t.Errorf("%s: no call has its prompt %q", seg, want)
		}
	}
	if !strings.Contains(calls[0].User, segment(1)) {
		t.Errorf("first call was not segment 1's, the first called: %.80q", calls[0].User)
	}
	if usage := files[run.UsageFile]; !strings.Contains(usage, `"brief":"`+briefs.RepairEditsQuoteOnceFile+`"`) {
		t.Errorf("usage log does not name the quote-once edit brief:\n%s", usage)
	}

	changed, err := repair.Changed(src, folder)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 3}; !reflect.DeepEqual(changed, want) {
		t.Fatalf("changed segments %v, want %v", changed, want)
	}

	// The re-check calls each check on the changed segments only, on the
	// repaired notes.
	notes := readNotes(t, folder, run.RepairNotes)
	recheck := testfixture.NewFake(func(req provider.Request) (provider.Reply, error) {
		for _, n := range changed {
			switch req.User {
			case supportcheck.Prompt(notes, src.Segments[n]):
				return provider.Reply{Text: recheckSupport[n]}, nil
			case readsalone.Prompt(notes, src.Segments[n]):
				return provider.Reply{Text: recheckReadsAlone[n]}, nil
			}
		}
		t.Errorf("re-check call for %.80q is not a changed segment's on the repaired notes", req.User)
		return provider.Reply{}, fmt.Errorf("no reply for %.80q", req.User)
	})
	if err := repair.Recheck(context.Background(), recheck, src, folder); err != nil {
		t.Fatal(err)
	}
	if n := len(recheck.Calls()); n != 2*len(changed) {
		t.Errorf("%d re-check calls, want %d", n, 2*len(changed))
	}

	// Coverage is the repaired notes': segment 3's insert covers the
	// sentence the first run left uncovered.
	cov := readFile(t, folder, run.MechanicalRecheckDir, run.CoverageFile)
	if want := mechcheck.Check(src, notes).Coverage.String(); cov != want {
		t.Errorf("re-check coverage is not the repaired notes':\n got %s\nwant %s", cov, want)
	}
	if cov == readFile(t, folder, run.MechanicalCheckDir, run.CoverageFile) {
		t.Errorf("re-check coverage is the first run's")
	}

	if err := repair.WriteStillFails(src, folder); err != nil {
		t.Fatal(err)
	}
	// Segment 1's re-check found nothing, so its first run's flags are gone.
	// Segment 2, unchanged, carries its first run's: one entry for the
	// statement both checks flagged, quoted two ways, with only step 1.3a's
	// grade that counts, and one by its quote for the flag under another
	// segment's H1, which names none of its notes. Segment 3's broken trace
	// is on the statement that cites it, not its line's last. A quote that
	// starts mid-statement, at a "…", gives no statement's start, so it is
	// listed by its quote.
	want := []repair.Entry{
		{
			File: lens, Statement: "Obviously, a keeper can take no notice of the lens on a clear night.[^3]",
			Traces: []string{"[^3]: " + lensTrace("Obviously, a keeper can take no notice of the lens on a clear night.")},
			Grades: []string{"weak", "dangling"},
			Flags: []repair.Flag{
				{Step: repair.StepSupport, Grades: []string{"weak"}, Quote: "Obviously, a keeper can take no notice…", Reason: "the section says a keeper can, not that one does."},
				{Step: repair.StepReadsAlone, Grades: []string{"dangling"}, Quote: "Obviously, a keeper can take no notice of the lens on a clear night.", Reason: `"the lens": which lens is not said.`},
			},
		},
		{
			File: storms, Statement: "In a storm the light matters most.[^1]",
			Traces: []string{"[^1]: " + stormsTrace("In a calm the sea is kind.")},
			Grades: []string{"broken"},
			Flags:  []repair.Flag{{Step: repair.StepMechanical, Grades: []string{"broken"}, Reason: "BROKEN [^1] In a calm the sea is kind."}},
		},
		{
			File: storms, Statement: "A wreck was his failure, whatever the weather.[^2]",
			Traces: []string{"[^2]: " + stormsTrace("failure, whatever the weather")},
			Grades: []string{"dangling"},
			Flags:  []repair.Flag{{Step: repair.StepReadsAlone, Grades: []string{"dangling"}, Quote: "A wreck was his failure…", Reason: `"his": no earlier statement says whose failure.`}},
		},
		{
			File: storms, Quote: "…I, Rhea Vale, have stood…", Traces: []string{}, Grades: []string{"reads wrong"},
			Flags: []repair.Flag{{Step: repair.StepSupport, Grades: []string{"reads wrong"}, Quote: "…I, Rhea Vale, have stood…", Reason: "the author's name set into the first person reads oddly."}},
		},
		{
			File: "", Quote: "He stayed at the lamp…", Traces: []string{}, Grades: []string{"dangling"},
			Flags: []repair.Flag{{Step: repair.StepReadsAlone, Grades: []string{"dangling"}, Quote: "He stayed at the lamp…", Reason: `"He": no earlier statement names who.`}},
		},
	}
	if got := stillFails(t, folder); !reflect.DeepEqual(got, want) {
		t.Errorf("still fails:\n got %+v\nwant %+v", got, want)
	}
	// No second repair call, whatever the re-check found.
	if n := len(p.Calls()); n != len(runCases) {
		t.Errorf("%d repair calls after the re-check, want %d", n, len(runCases))
	}
}

// segment1Run makes a first run on the invented chapter in which only
// segment 1 has a flag that counts.
func segment1Run(t *testing.T) (*source.Source, *run.Folder) {
	t.Helper()
	support := map[int]string{
		0: "# 0 Keepers of the Lamp\n\npass 3\n",
		1: runSupport[1],
		2: "# 2 The Lens\n\npass 5\n",
		3: "# 3 Storms\n\npass 3\n",
	}
	readsAlone := map[int]string{}
	for n := range support {
		readsAlone[n] = "pass\n"
	}
	return checkedRun(t, nil, support, readsAlone)
}

// fixedAside is segment 1's aside given in full, changed from step 1.1's.
var fixedAside = "=== FILE: " + aside + " ===\n" +
	"# 1 The Keeper's Watch - Aside - The log\n\n" +
	"The log was read by nobody until a ship was lost.[^1]\n" +
	"Then the inspectors read every page.[^2]\n\n" +
	"[^1]: " + watchTrace("The log was read by nobody until a ship was lost.") + "\n" +
	"[^2]: " + watchTrace("Then the inspectors read every page.") + "\n"

// fixedAsideNote is the note fixedAside gives.
var fixedAsideNote = strings.TrimPrefix(fixedAside, "=== FILE: "+aside+" ===\n")

// replyCase is segment 1's repair reply, briefed for shape and quoteOnce,
// the segment's notes it should leave, by file name, and what it should
// drop.
type replyCase struct {
	name      string
	shape     repair.Reply
	quoteOnce bool
	reply     string
	notes     map[string]string
	dropped   []dropped.Item
}

// replyCases are a reply per shape the step applies, then one per block or
// edit it drops. Each is run alone, on segment1Run, as segment 1's reply.
var replyCases = []replyCase{
	// Whole notes, as trialled: a FILE block replaces its note. A reply in
	// the other shape is applied all the same, so the EDIT block is made.
	{name: "whole notes: a note in full, and an edit made all the same", shape: repair.Whole,
		reply: fixedAside +
			"=== EDIT: " + watch + " ===\n--- REPLACE ---\nThe watch began at dusk.[^1]\n--- WITH ---\n" +
			"=== REPORT ===\nr\n",
		notes: map[string]string{
			watch: "# 1 The Keeper's Watch\n\n" +
				"He trimmed the wick every four hours.[^1]\n" +
				"He logged the weather in a book.[^2]\n\n" +
				"[^1]: " + watchTrace("trimmed the wick every four hours") + "\n" +
				"[^2]: " + watchTrace("logged the weather in a book") + "\n",
			aside: fixedAsideNote,
		}},
	// Edits made through the note model: a statement split into two quotes,
	// and one replaced by a statement with a trace of its own.
	{name: "edits: a statement split into two quotes, and one replaced", shape: repair.Edits,
		reply: "=== EDIT: " + watch + " ===\n" +
			"--- REPLACE ---\nHe trimmed the wick every four hours.[^2]\n--- WITH ---\nThe keeper[^1] trimmed the wick every four hours.[^2]\n" +
			"[^1]: " + watchTrace("The keeper") + "\n[^2]: " + watchTrace("trimmed the wick every four hours") + "\n" +
			"--- REPLACE ---\nHe logged the weather in a book.[^3]\n--- WITH ---\nThe keeper logged the weather in a book.[^1]\n" +
			"[^1]: " + watchTrace("logged the weather in a book") + "\n" +
			"=== REPORT ===\nr\n",
		notes: map[string]string{
			watch: "# 1 The Keeper's Watch\n\n" +
				"The watch began at dusk.[^1]\n" +
				"The keeper[^2] trimmed the wick every four hours.[^3]\n" +
				"The keeper logged the weather in a book.[^4]\n\n" +
				"[^1]: " + watchTrace("The watch began at dusk.") + "\n" +
				"[^2]: " + watchTrace("The keeper") + "\n" +
				"[^3]: " + watchTrace("trimmed the wick every four hours") + "\n" +
				"[^4]: " + watchTrace("logged the weather in a book") + "\n",
			aside: testfixture.LampStage1()[aside],
		}},
	// With quote-once, an edit written with its joining text marked is
	// traced by s2n, and a note given in full with footnotes is taken as
	// written.
	{name: "quote-once: an edit traced, and a note in full taken as written", shape: repair.Edits, quoteOnce: true,
		reply: "=== EDIT: " + watch + " ===\n" +
			"--- REPLACE ---\nHe logged the weather in a book.[^3]\n--- WITH ---\nThe keeper{ also }logged the weather in a book.\n" +
			fixedAside + "=== REPORT ===\nr\n",
		notes: map[string]string{
			watch: "# 1 The Keeper's Watch\n\n" +
				"The watch began at dusk.[^1]\n" +
				"He trimmed the wick every four hours.[^2]\n" +
				"The keeper[^3] also logged the weather in a book.[^4]\n\n" +
				"[^1]: " + watchTrace("The watch began at dusk.") + "\n" +
				"[^2]: " + watchTrace("trimmed the wick every four hours") + "\n" +
				"[^3]: " + watchTrace("The keeper") + "\n" +
				"[^4]: " + watchTrace("logged the weather in a book.") + "\n",
			aside: fixedAsideNote,
		}},
	{name: "an aside deleted", shape: repair.Edits,
		reply: "=== DELETE: " + aside + " ===\n=== REPORT ===\nr\n",
		notes: map[string]string{watch: testfixture.LampStage1()[watch]}},
	// A block that can't be applied is dropped, and the reply's other block
	// is still made.
	{name: "the main note deleted", shape: repair.Edits,
		reply:   "=== DELETE: " + watch + " ===\n" + fixedAside + "=== REPORT ===\nr\n",
		notes:   map[string]string{watch: testfixture.LampStage1()[watch], aside: fixedAsideNote},
		dropped: []dropped.Item{{Segment: 1, Note: watch, What: "the reply's DELETE", Why: "it is the main note; only an aside may be deleted"}}},
	{name: "a missing note deleted", shape: repair.Edits,
		reply:   "=== DELETE: 1 The Keeper's Watch - Aside - Nope.md ===\n" + fixedAside + "=== REPORT ===\nr\n",
		notes:   map[string]string{watch: testfixture.LampStage1()[watch], aside: fixedAsideNote},
		dropped: []dropped.Item{{Segment: 1, Note: "1 The Keeper's Watch - Aside - Nope.md", What: "the reply's DELETE", Why: "it is not a note"}}},
	{name: "a new note", shape: repair.Edits,
		reply:   "=== FILE: 1 The Keeper's Watch - Aside - New.md ===\n# 1 The Keeper's Watch - Aside - New\n" + fixedAside + "=== REPORT ===\nr\n",
		notes:   map[string]string{watch: testfixture.LampStage1()[watch], aside: fixedAsideNote},
		dropped: []dropped.Item{{Segment: 1, Note: "1 The Keeper's Watch - Aside - New.md", What: "the reply's FILE block", Why: "it is not a note"}}},
	// Another segment's note is a note, so only the segment's prefix keeps
	// it out.
	{name: "another segment's note", shape: repair.Edits,
		reply:   "=== FILE: " + keepers + " ===\n# 0 Keepers of the Lamp\n" + fixedAside + "=== REPORT ===\nr\n",
		notes:   map[string]string{watch: testfixture.LampStage1()[watch], aside: fixedAsideNote},
		dropped: []dropped.Item{{Segment: 1, Note: keepers, What: "the reply's FILE block", Why: "it is not one of the segment's notes"}}},
	// An edit the note model can't make is dropped, and its block's other
	// edits are made.
	{name: "an edit at no statement, beside one made", shape: repair.Edits,
		reply: "=== EDIT: " + watch + " ===\n" +
			"--- REPLACE ---\nThe lamp was lit.\n--- WITH ---\n" +
			"--- REPLACE ---\nThe watch began at dusk.[^1]\n--- WITH ---\n" +
			"=== REPORT ===\nr\n",
		notes: map[string]string{
			watch: "# 1 The Keeper's Watch\n\n" +
				"He trimmed the wick every four hours.[^1]\n" +
				"He logged the weather in a book.[^2]\n\n" +
				"[^1]: " + watchTrace("trimmed the wick every four hours") + "\n" +
				"[^2]: " + watchTrace("logged the weather in a book") + "\n",
			aside: testfixture.LampStage1()[aside],
		},
		dropped: []dropped.Item{{Segment: 1, Note: watch, What: `the edit at "The lamp was lit."`, Why: `edit 1: no statement in 1 The Keeper's Watch.md reads "The lamp was lit."`}}},
	// The call may replace and insert, not add a paragraph.
	{name: "a paragraph added", shape: repair.Edits,
		reply: "=== EDIT: " + watch + " ===\n" +
			"--- AFTER ---\nThe watch began at dusk.[^1]\n--- PARAGRAPH ---\nThe lamp was lit.[^1]\n[^1]: " + watchTrace("The watch began at dusk.") + "\n" +
			"=== REPORT ===\nr\n",
		notes:   map[string]string{watch: testfixture.LampStage1()[watch], aside: testfixture.LampStage1()[aside]},
		dropped: []dropped.Item{{Segment: 1, Note: watch, What: `the edit at "The watch began at dusk.[^1]"`, Why: "it adds a paragraph, which this call may not do"}}},
	// A block that doesn't parse as edits, or names a note that doesn't
	// exist, is dropped whole.
	{name: "a reference with no definition", shape: repair.Edits,
		reply: "=== EDIT: " + watch + " ===\n--- REPLACE ---\nThe watch began at dusk.\n--- WITH ---\nThe watch began at dusk.[^1]\n" +
			"=== REPORT ===\nr\n",
		notes:   map[string]string{watch: testfixture.LampStage1()[watch], aside: testfixture.LampStage1()[aside]},
		dropped: []dropped.Item{{Segment: 1, Note: watch, What: "the reply's EDIT block", Why: "1 The Keeper's Watch.md: edit 1: [^1] in its text has no definition in the edit"}}},
	{name: "an edit block of a missing note", shape: repair.Edits,
		reply:   "=== EDIT: 1 The Keeper's Watch - Aside - Nope.md ===\n--- REPLACE ---\nA.\n--- WITH ---\n=== REPORT ===\nr\n",
		notes:   map[string]string{watch: testfixture.LampStage1()[watch], aside: testfixture.LampStage1()[aside]},
		dropped: []dropped.Item{{Segment: 1, Note: "1 The Keeper's Watch - Aside - Nope.md", What: "the reply's EDIT block", Why: "it is not a note"}}},
	// Which of an edit and a rewrite or delete of one note comes first is
	// not the reply's to say, so both blocks are dropped, though the EDIT
	// names the note without its .md. Each item gives the note as its block
	// named it.
	{name: "a note edited and deleted", shape: repair.Edits,
		reply: "=== EDIT: " + strings.TrimSuffix(aside, ".md") + " ===\n--- REPLACE ---\nThen they read every page.\n--- WITH ---\n" +
			"=== DELETE: " + aside + " ===\n=== REPORT ===\nr\n",
		notes: map[string]string{watch: testfixture.LampStage1()[watch], aside: testfixture.LampStage1()[aside]},
		dropped: []dropped.Item{
			{Segment: 1, Note: strings.TrimSuffix(aside, ".md"), What: "the reply's EDIT block", Why: "the reply both edits the note and gives it in full or deletes it"},
			{Segment: 1, Note: aside, What: "the reply's DELETE", Why: "the reply both edits the note and gives it in full or deletes it"},
		}},
}

// TestRun_Replies runs step 1.4 with each reply case as segment 1's reply,
// the only one called, and checks the call and what the step wrote.
func TestRun_Replies(t *testing.T) {
	briefFiles := map[bool]map[repair.Reply][2]string{
		false: {repair.Whole: {"repair.md", "stage-1.md"}, repair.Edits: {"repair-edits.md", "stage-1.md"}},
		true:  {repair.Whole: {"repair.md", "stage-1.md"}, repair.Edits: {"repair-edits-quote-once.md", "stage-1-quote-once.md"}},
	}
	for _, c := range replyCases {
		t.Run(c.name, func(t *testing.T) {
			src, folder := segment1Run(t)
			p := testfixture.Serve(t, testfixture.Case{Contains: segment(1), Reply: c.reply})
			if err := repair.Run(context.Background(), p, src, "opus", c.shape, c.quoteOnce, folder); err != nil {
				t.Fatal(err)
			}
			if !folder.Complete(run.RepairDir) {
				t.Fatalf("the repair step is not complete")
			}

			files := briefFiles[c.quoteOnce][c.shape]
			calls := p.Calls()
			if len(calls) != 1 {
				t.Fatalf("%d repair calls, want 1: only segment 1 has a flag that counts", len(calls))
			}
			if calls[0].System != wantSystem(t, files[0], files[1]) {
				t.Errorf("the call is not briefed by %s and %s, then the frame", files[0], files[1])
			}
			step := testfixture.Files(t, filepath.Join(folder.Dir, run.RepairDir))
			if usage := step[run.UsageFile]; !strings.Contains(usage, `"brief":"`+files[0]+`"`) {
				t.Errorf("usage log does not name %s:\n%s", files[0], usage)
			}
			if got := step[run.RawDir+"/1.txt"]; got != c.reply {
				t.Errorf("raw reply is not the reply:\n got %q\nwant %q", got, c.reply)
			}
			if got := step[run.ReportsDir+"/1.md"]; got != "r\n" {
				t.Errorf("report %q, want the reply's", got)
			}

			repaired := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.RepairNotes)))
			if got := segmentNotes(repaired, 1); !reflect.DeepEqual(got, c.notes) {
				t.Errorf("segment 1's notes:\n got %q\nwant %q", got, c.notes)
			}
			for _, n := range []int{0, 2, 3} {
				if got, want := segmentNotes(repaired, n), segmentNotes(testfixture.LampStage1(), n); !reflect.DeepEqual(got, want) {
					t.Errorf("segment %d's notes are not step 1.1's", n)
				}
			}
			items, err := dropped.Read(folder, run.RepairDir)
			if err != nil {
				t.Fatal(err)
			}
			if want := c.dropped; !reflect.DeepEqual(items, want) && (len(items) != 0 || len(want) != 0) {
				t.Errorf("dropped:\n got %+v\nwant %+v", items, want)
			}

			changed, err := repair.Changed(src, folder)
			if err != nil {
				t.Fatal(err)
			}
			want := []int{}
			if !reflect.DeepEqual(c.notes, segmentNotes(testfixture.LampStage1(), 1)) {
				want = []int{1}
			}
			if !reflect.DeepEqual(changed, want) {
				t.Errorf("changed segments %v, want %v", changed, want)
			}
		})
	}
}

// Step 1.4 reads step 1.2's findings from its directory, and without them it
// stops, naming the file, rather than running the check again.
func TestRun_NeedsTheMechanicalCheckFindings(t *testing.T) {
	src, folder := segment1Run(t)
	findings := filepath.Join(folder.Dir, run.MechanicalCheckDir, run.FindingsFile)
	if err := os.Remove(findings); err != nil {
		t.Fatal(err)
	}
	p := testfixture.NewFake(func(provider.Request) (provider.Reply, error) { return provider.Reply{}, nil })
	err := repair.Run(context.Background(), p, src, "opus", repair.Edits, true, folder)
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
	src, folder := segment1Run(t)
	p := testfixture.Serve(t, testfixture.Case{Contains: segment(1), Reply: "=== REPORT ===\nKept every flag.\n"})
	if err := repair.Run(context.Background(), p, src, "opus", repair.Edits, true, folder); err != nil {
		t.Fatal(err)
	}
	if err := repair.RecheckMechanical(src, folder); err != nil {
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

// Of two edits that can each be made alone but not together, at
// overlapping statements, the later is dropped and the earlier made.
func TestEditNote_DropsOverlappingEdits(t *testing.T) {
	text := testfixture.LampStage1()[keepers]
	body := "--- REPLACE ---\nA lighthouse is a promise made to ships. It burns whether or not a ship is near.[^1][^2]\n--- WITH ---\n" +
		"--- REPLACE ---\nIt burns whether or not a ship is near.[^1][^2]\n--- WITH ---\n"
	got, drops, err := repair.EditNote(keepers, text, body, nil, note.Replace)
	if err != nil {
		t.Fatal(err)
	}
	want := "# 0 Keepers of the Lamp\n\n" +
		"The keeper kept that promise for the harbour.[^1]\n\n" +
		"[^1]: " + testfixture.LampTrace("2. Keepers of the Lamp", "The keeper kept that promise for the harbour.") + "\n"
	if got != want || len(drops) != 1 || drops[0].Statement != "It burns whether or not a ship is near.[^1][^2]" {
		t.Errorf("EditNote = %q, %+v; want the first edit made and the second dropped", got, drops)
	}
}

// An edit that clashes with nothing is made, though two others in its block
// clash with each other: only the failing edit is dropped.
func TestEditNote_KeepsAnEditClearOfAClash(t *testing.T) {
	text := testfixture.LampStage1()[watch]
	body := "--- AFTER ---\nThe watch began at dusk.\n--- INSERT ---\nOne.\n" +
		"--- AFTER ---\nThe watch began at dusk.\n--- INSERT ---\nTwo.\n" +
		"--- AFTER ---\nHe logged the weather in a book.\n--- INSERT ---\nThree.\n"
	got, drops, err := repair.EditNote(watch, text, body, nil, note.Replace, note.Insert)
	if err != nil {
		t.Fatal(err)
	}
	want := "# 1 The Keeper's Watch\n\n" +
		"The watch began at dusk.[^1] One.\n" +
		"He trimmed the wick every four hours.[^2]\n" +
		"He logged the weather in a book.[^3] Three.\n\n" +
		"[^1]: " + watchTrace("The watch began at dusk.") + "\n" +
		"[^2]: " + watchTrace("trimmed the wick every four hours") + "\n" +
		"[^3]: " + watchTrace("logged the weather in a book") + "\n"
	if got != want {
		t.Errorf("EditNote = %q; want the first and third edits made", got)
	}
	if len(drops) != 1 || drops[0].Statement != "The watch began at dusk." {
		t.Errorf("dropped %+v, want the second edit", drops)
	}
}

// An edit whose operation the call may not make is dropped, and the others
// are made.
func TestEditNote_DropsAnOpNotAllowed(t *testing.T) {
	text := testfixture.LampStage1()[watch]
	body := "--- AFTER ---\nThe watch began at dusk.[^1]\n--- INSERT ---\nThe keeper kept it.[^1]\n[^1]: " + watchTrace("The keeper") + "\n" +
		"--- REPLACE ---\nHe logged the weather in a book.[^3]\n--- WITH ---\n"
	got, drops, err := repair.EditNote(watch, text, body, nil, note.Insert)
	if err != nil {
		t.Fatal(err)
	}
	want := "# 1 The Keeper's Watch\n\n" +
		"The watch began at dusk.[^1] The keeper kept it.[^2]\n" +
		"He trimmed the wick every four hours.[^3]\n" +
		"He logged the weather in a book.[^4]\n\n" +
		"[^1]: " + watchTrace("The watch began at dusk.") + "\n" +
		"[^2]: " + watchTrace("The keeper") + "\n" +
		"[^3]: " + watchTrace("trimmed the wick every four hours") + "\n" +
		"[^4]: " + watchTrace("logged the weather in a book") + "\n"
	if got != want {
		t.Errorf("EditNote = %q, want the insert made and the replace not", got)
	}
	wantDrops := repair.DroppedEdits{{Statement: "He logged the weather in a book.[^3]", Why: "it replaces a statement, which this call may not do"}}
	if !reflect.DeepEqual(drops, wantDrops) {
		t.Errorf("dropped %+v, want %+v", drops, wantDrops)
	}
}
