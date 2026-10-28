package keptloss_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/dropped"
	"github.com/corygyarmathy/slipbox/internal/keptloss"
	"github.com/corygyarmathy/slipbox/internal/lossproposer"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/repair"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/supportcheck"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// The invented chapter's notes, by file name.
const (
	aside  = "1 The Keeper's Watch - Aside - The log.md"
	lens   = "2 The Lens.md"
	storms = "3 Storms.md"
)

// Traces into the invented chapter's sections, as footnotes give them.
func lensTrace(quote string) string   { return testfixture.LampTrace("The Lens", quote) }
func stormsTrace(quote string) string { return testfixture.LampTrace("Storms", quote) }

// keptCall and recheckCall are the keys a fake matches segment n's
// kept-loss call and its step 1.3a re-check on: the labels their user
// prompts give it. Neither holds the other.
func keptCall(n int) string    { return fmt.Sprintf("YOUR SEGMENT'S NOTES (segment %d: ", n) }
func recheckCall(n int) string { return fmt.Sprintf("YOUR SEGMENT'S NOTES TO CHECK (segment %d: ", n) }

// isKeptCall says whether a request's user prompt is a kept-loss call's
// rather than a re-check's.
func isKeptCall(user string) bool { return strings.Contains(user, "=== KEPT LOSSES ===") }

// proposedRun makes a run folder for the invented chapter whose step 1.4
// repaired notes are LampStage1 and whose step 1.5 proposed losses.
func proposedRun(t *testing.T, losses []lossproposer.Segment) (*source.Source, *run.Folder) {
	t.Helper()
	src, folder := testfixture.LampExtractRun(t)
	testfixture.WriteFiles(t, filepath.Join(folder.Dir, filepath.FromSlash(run.RepairNotes)), testfixture.LampStage1())
	b, err := json.Marshal(losses)
	if err != nil {
		t.Fatal(err)
	}
	testfixture.WriteFiles(t, filepath.Join(folder.Dir, run.LossProposerDir), map[string]string{run.ProposedLossesFile: string(b)})
	return src, folder
}

// wantSystem is the system prompt of a kept-loss call on the invented
// chapter briefed by brief, then rules, files in internal/briefs/. It is
// built here rather than with keptloss.Brief, so a fault there still shows.
func wantSystem(t *testing.T, brief, rules string) string {
	t.Helper()
	return testfixture.RepoFile(t, "internal/briefs/"+brief) + "\n\n" + testfixture.RepoFile(t, "internal/briefs/"+rules) + "\n\n" + testfixture.LampFrame
}

// briefFiles names each reply shape's brief, then its rules, files in
// internal/briefs/, with quote-once and without. Whole notes have no
// quote-once brief, so they are briefed as trialled with quote-once or
// without.
var briefFiles = map[bool]map[repair.Reply][2]string{
	false: {repair.Whole: {"kept-loss.md", "stage-1.md"}, repair.Edits: {"kept-loss-edits.md", "stage-1.md"}},
	true:  {repair.Whole: {"kept-loss.md", "stage-1.md"}, repair.Edits: {"kept-loss-edits-quote-once.md", "stage-1-quote-once.md"}},
}

// TestBrief checks each reply shape's brief against the brief files.
func TestBrief(t *testing.T) {
	for quoteOnce, shapes := range briefFiles {
		for shape, files := range shapes {
			file, got := keptloss.Brief(shape, quoteOnce)
			want := testfixture.RepoFile(t, "internal/briefs/"+files[0]) + "\n\n" + testfixture.RepoFile(t, "internal/briefs/"+files[1])
			if file != files[0] || got != want {
				t.Errorf("Brief(%s, %v) is %s and its text, want %s, a blank line, then %s", shape, quoteOnce, file, files[0], files[1])
			}
		}
	}
}

// runLosses are step 1.5's union for TestPrompt and TestRun. Segment 0 has
// none. Segment 1 has two, the first worded by two runs, one of them with no
// why, the second with two quotes. Segments 2 and 3 have one each.
var runLosses = []lossproposer.Segment{
	{Segment: 0, Heading: "2. Keepers of the Lamp", Losses: []lossproposer.Loss{}, Other: []lossproposer.Line{}},
	{Segment: 1, Heading: "The Keeper’s Watch", Losses: []lossproposer.Loss{
		{Wordings: []lossproposer.Item{
			{Run: 1, Idea: "The keeper kept a weather log.", Quotes: []string{"logged the weather in a book"}, Why: "it says what the log held"},
			{Run: 2, Idea: "The weather went into a book.", Quotes: []string{"logged the weather in a book"}},
		}},
		{Wordings: []lossproposer.Item{
			{Run: 2, Idea: "Inspectors read the log only after a wreck.", Quotes: []string{"read by nobody until a ship was lost", "Then the inspectors read every page."}, Why: "it says who read the log"},
		}},
	}, Other: []lossproposer.Line{}},
	{Segment: 2, Heading: "The Lens", Losses: []lossproposer.Loss{
		{Wordings: []lossproposer.Item{{Run: 1, Idea: "The lens is named: the Fresnel lens.", Quotes: []string{"*The Fresnel lens*"}, Why: "the section names it"}}},
	}, Other: []lossproposer.Line{}},
	{Segment: 3, Heading: "Storms", Losses: []lossproposer.Loss{
		{Wordings: []lossproposer.Item{{Run: 1, Idea: "The author has stood in the lamp room in a gale.", Quotes: []string{"I have stood in that lamp room in a gale, and the glass shook."}, Why: "the author's own witness"}}},
	}, Other: []lossproposer.Line{}},
}

// keptOf gives the kept losses of runLosses in segment n, numbered as
// keptloss.Losses numbers them. It is built here rather than with Losses, so
// a fault there still shows.
func keptOf(n int) []keptloss.Kept {
	var out []keptloss.Kept
	for _, seg := range runLosses {
		if seg.Segment != n {
			continue
		}
		for k, l := range seg.Losses {
			out = append(out, keptloss.Kept{Segment: n, Number: k + 1, Loss: l})
		}
	}
	return out
}

// TestPrompt pins each called segment's user prompt on LampStage1: the
// segment's notes in full, then its kept losses, each with every run's
// wording.
func TestPrompt(t *testing.T) {
	src := testfixture.LampSource(t)
	notes := testfixture.LampStage1Notes(t)
	for _, n := range []int{1, 2, 3} {
		got := keptloss.Prompt(notes, src.Segments[n], keptOf(n))
		testfixture.Golden(t, "prompt/"+strconv.Itoa(n)+".txt", got)
	}
}

// runCase is a called segment's kept-loss reply in TestRun, its step 1.3a
// re-check reply, and the notes, by file name, and report it should leave.
type runCase struct {
	name    string
	segment int
	reply   string
	support string
	notes   map[string]string
	report  string
}

// runCases are the kept-loss replies of TestRun's three called segments, in
// the default shape: edits, written quote-once.
var runCases = []runCase{
	// A reply that adds nothing leaves the notes as they were, and each of
	// the segment's kept losses is listed as dropped. The re-check's flag that
	// counts is on a statement the notes already had, so it isn't listed.
	{"a reply that adds nothing; a flag on an old statement", 1,
		"=== REPORT ===\n- Loss 1.1: already stated.\n- Loss 1.2: already in the aside.\n",
		"# 1 The Keeper's Watch\n\n" +
			"- [cut would do] \"He trimmed the wick…\": a cut to \"The keeper\" would name him.\n" +
			"pass 2 / cut would do 1\n\n" +
			"# 1 The Keeper's Watch - Aside - The log\n\npass 2\n",
		testfixture.SegmentNotes(testfixture.LampStage1(), 1),
		"- Loss 1.1: already stated.\n- Loss 1.2: already in the aside.\n"},
	// A quote-once insert, traced by s2n. The re-check flags the new
	// statement with a grade that counts beside one that doesn't, a statement
	// the notes already had, and a quote no statement holds.
	{"a quote-once insert; flags on a new, an old and no statement", 2,
		"=== EDIT: " + lens + " ===\n" +
			"--- AFTER ---\nIt uses rings of prisms instead of one thick glass.[^5]\n" +
			"--- INSERT ---\n*The Fresnel lens* uses rings of prisms instead of one thick glass.\n" +
			"=== REPORT ===\n- Loss 2.1: named the lens.\n",
		"# 2 The Lens\n\n" +
			"- [dangling / fail] \"*The Fresnel lens* uses rings…\": the section doesn't say it is the lens the keeper tends.\n" +
			"- [weak] \"The lens bends light…\": thin.\n" +
			"- [weak] \"The lamp is lit at dusk…\": thin.\n" +
			"pass 4 / weak 2 / fail 1\n",
		map[string]string{lens: "# 2 The Lens\n\n" +
			"The lens bends light into a beam.[^1]\n" +
			"A beam carries farther than a glow.[^2]\n" +
			"Obviously, a keeper can take no notice of the lens on a clear night.[^3]\n" +
			"On a foggy night it is no help at all.[^4]\n" +
			"It uses rings of prisms instead of one thick glass.[^5] *The Fresnel lens* uses rings of prisms instead of one thick glass.[^6]\n\n" +
			"[^1]: " + lensTrace("The lens bends light into a beam.") + "\n" +
			"[^2]: " + lensTrace("A beam carries farther than a glow.") + "\n" +
			"[^3]: " + lensTrace("Obviously, a keeper can take no notice of the lens on a clear night.") + "\n" +
			"[^4]: " + lensTrace("On a foggy night it is no help at all.") + "\n" +
			"[^5]: " + lensTrace("uses rings of prisms instead of one thick glass") + "\n" +
			"[^6]: " + lensTrace("*The Fresnel lens* uses rings of prisms instead of one thick glass.") + "\n"},
		"- Loss 2.1: named the lens.\n"},
	// A quote-once insert of the author's first-person sentence with the
	// author's name set into it, which s2n traces to the source line. The
	// re-check's flag on it has only a grade step 1.3b governs, which doesn't
	// count.
	{"a quote-once insert with the author's name set in; a grade that doesn't count", 3,
		"=== EDIT: " + storms + " ===\n" +
			"--- AFTER ---\nHe stayed at the lamp (in 1839, for nine days) through every gale.[^3]\n" +
			"--- INSERT ---\nI{, }Rhea Vale{, }have stood in that lamp room in a gale, and the glass shook.\n" +
			"=== REPORT ===\n- Loss 3.1: added the author's account.\n",
		"# 3 Storms\n\n" +
			"- [outside-frame] \"I, Rhea Vale, have stood…\": the reader may not know the author.\n" +
			"pass 4 / outside-frame 1\n",
		map[string]string{storms: "# 3 Storms\n\n" +
			"In a storm the light matters most.[^1]\n" +
			"A wreck was his failure, whatever the weather.[^2]\n" +
			"He stayed at the lamp (in 1839, for nine days) through every gale.[^3] I, Rhea Vale,[^4] have stood in that lamp room in a gale, and the glass shook.[^5]\n\n" +
			"[^1]: " + stormsTrace("In a storm the light matters most.") + "\n" +
			"[^2]: " + stormsTrace("failure, whatever the weather") + "\n" +
			"[^3]: " + stormsTrace("stayed at the lamp (in 1839, for nine days) through every gale") + "\n" +
			"[^4]: [[" + testfixture.Lamp + "]] \"Rhea Vale\"\n" +
			"[^5]: " + stormsTrace("I have stood in that lamp room in a gale, and the glass shook.") + "\n"},
		"- Loss 3.1: added the author's account.\n"},
}

// TestRun runs the kept-loss repair on runLosses in the default shape, with
// its re-check, then the final notes, and checks the calls made and what
// each wrote.
func TestRun(t *testing.T) {
	src, folder := proposedRun(t, runLosses)
	var cases []testfixture.Case
	for _, c := range runCases {
		cases = append(cases, testfixture.Case{Contains: keptCall(c.segment), Reply: c.reply}, testfixture.Case{Contains: recheckCall(c.segment), Reply: c.support})
	}
	p := testfixture.Serve(t, cases...)
	if err := keptloss.Run(context.Background(), p, src, "opus", repair.DefaultReply, true, folder); err != nil {
		t.Fatal(err)
	}

	files := testfixture.Files(t, filepath.Join(folder.Dir, run.KeptLossDir))
	after := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.KeptLossNotes)))
	for _, c := range runCases {
		t.Run(c.name, func(t *testing.T) {
			n := strconv.Itoa(c.segment)
			if got := files[run.RawDir+"/"+n+".txt"]; got != c.reply {
				t.Errorf("segment %d: raw reply is not the reply:\n got %q\nwant %q", c.segment, got, c.reply)
			}
			if got := files[run.ReportsDir+"/"+n+".md"]; got != c.report {
				t.Errorf("segment %d: report %q, want %q", c.segment, got, c.report)
			}
			if got := testfixture.SegmentNotes(after, c.segment); !reflect.DeepEqual(got, c.notes) {
				t.Errorf("segment %d: notes:\n got %q\nwant %q", c.segment, got, c.notes)
			}
		})
	}
	// Segment 0, with no proposed loss, has no call and keeps step 1.4's
	// notes.
	if got, want := testfixture.SegmentNotes(after, 0), testfixture.SegmentNotes(testfixture.LampStage1(), 0); !reflect.DeepEqual(got, want) {
		t.Errorf("segment 0's notes are not step 1.4's:\n got %q\nwant %q", got, want)
	}
	for _, rel := range []string{run.RawDir + "/0.txt", run.ReportsDir + "/0.md"} {
		if _, ok := files[rel]; ok {
			t.Errorf("segment 0, with no proposed loss, has %s", rel)
		}
	}

	// Every proposed loss is kept, in step 1.5's order.
	var kept []keptloss.Kept
	if err := json.Unmarshal([]byte(files[run.KeptLossesFile]), &kept); err != nil {
		t.Fatal(err)
	}
	if want := slices.Concat(keptOf(1), keptOf(2), keptOf(3)); !reflect.DeepEqual(kept, want) {
		t.Errorf("kept losses:\n got %+v\nwant %+v", kept, want)
	}

	// One kept-loss call per segment with a proposed loss, each with its own
	// losses on step 1.4's notes, briefed by the quote-once edit brief and
	// rules, at the model given and medium. Segment 1's, the first called,
	// is made first. Each re-check call is at the check's tier.
	system := wantSystem(t, "kept-loss-edits-quote-once.md", "stage-1-quote-once.md")
	var keptUsers, recheckUsers []string
	for _, c := range p.Calls() {
		if !isKeptCall(c.User) {
			if c.Model != supportcheck.Model || c.Effort != "medium" {
				t.Errorf("re-check call for %.80q: model %q at effort %q, want %s at medium", c.User, c.Model, c.Effort, supportcheck.Model)
			}
			recheckUsers = append(recheckUsers, c.User)
			continue
		}
		if c.System != system {
			t.Errorf("call for %.80q: system prompt is not the quote-once kept-loss edit brief, the quote-once stage 1 rules and the frame", c.User)
		}
		if c.Model != "opus" || c.Effort != "medium" {
			t.Errorf("call for %.80q: model %q at effort %q, want opus at medium", c.User, c.Model, c.Effort)
		}
		keptUsers = append(keptUsers, c.User)
	}
	if len(keptUsers) != len(runCases) {
		t.Errorf("%d kept-loss calls, want one per segment with a proposed loss (%d)", len(keptUsers), len(runCases))
	}
	before := testfixture.LampStage1Notes(t)
	for _, c := range runCases {
		seg := src.Segments[c.segment]
		if want := keptloss.Prompt(before, seg, keptOf(c.segment)); !slices.Contains(keptUsers, want) {
			t.Errorf("%s: no kept-loss call has its prompt %q", seg, want)
		}
	}
	if calls := p.Calls(); len(calls) == 0 || !strings.Contains(calls[0].User, keptCall(1)) {
		t.Errorf("the first call was not segment 1's kept-loss call, the first called")
	}

	// The re-check runs step 1.3a once per segment with a kept loss, on the
	// notes the call left: briefed by step 1.3a's brief, then the frame, then
	// the kept-loss notes, which step 1.3a's user prompt only names. The
	// shared block is supportcheck's, whose tests pin its layout.
	afterNotes, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(run.KeptLossNotes)))
	if err != nil {
		t.Fatal(err)
	}
	recheckSystem := testfixture.RepoFile(t, "internal/briefs/checker-support.md") + "\n\n" + testfixture.LampFrame + "\n" + supportcheck.Shared(afterNotes)
	for _, c := range p.Calls() {
		if !isKeptCall(c.User) && c.System != recheckSystem {
			t.Errorf("re-check call for %.80q: system prompt is not step 1.3a's brief, the frame and the kept-loss notes", c.User)
		}
	}
	if len(recheckUsers) != len(runCases) {
		t.Errorf("%d re-check calls, want one per segment with a kept loss (%d)", len(recheckUsers), len(runCases))
	}
	for _, c := range runCases {
		seg := src.Segments[c.segment]
		if want := supportcheck.Prompt(afterNotes, seg); !slices.Contains(recheckUsers, want) {
			t.Errorf("%s: no re-check call has its prompt on the kept-loss notes %q", seg, want)
		}
	}

	// The step's usage log holds every call it made, the re-check's
	// included, and the re-check keeps none of its own. Steps 1.2 and 1.3a
	// write their reports.
	usage := files[run.UsageFile]
	if n := strings.Count(usage, "\n"); n != len(p.Calls()) {
		t.Errorf("usage log has %d lines, want one per call (%d):\n%s", n, len(p.Calls()), usage)
	}
	for _, s := range []string{`"step":"` + run.KeptLossDir + `"`, `"step":"` + run.KeptLossSupportDir + `"`, `"brief":"kept-loss-edits-quote-once.md"`} {
		if !strings.Contains(usage, s) {
			t.Errorf("usage log does not hold %s:\n%s", s, usage)
		}
	}
	if _, ok := files[run.KeptLossSupportDir+"/"+run.UsageFile]; ok {
		t.Errorf("the re-check kept a usage log of its own")
	}
	for _, rel := range []string{run.KeptLossMechanicalDir + "/" + run.ReportFile, run.KeptLossSupportDir + "/" + run.ReportsDir + "/3.md"} {
		if _, ok := files[rel]; !ok {
			t.Errorf("no re-check file %s", rel)
		}
	}

	// Still fails lists the new statement step 1.3a fails, with that grade
	// alone, and the flag no statement holds, since it may be on a new one.
	// The flags on old statements and the grade that doesn't count aren't
	// listed.
	fails, items := still(t, folder)
	wantFails := []repair.Entry{
		{
			File: lens, Statement: "*The Fresnel lens* uses rings of prisms instead of one thick glass.[^6]",
			Traces: []string{"[^6]: " + lensTrace("*The Fresnel lens* uses rings of prisms instead of one thick glass.")},
			Grades: []string{"fail"},
			Flags:  []repair.Flag{{Step: repair.StepSupport, Grades: []string{"fail"}, Quote: "*The Fresnel lens* uses rings…", Reason: "the section doesn't say it is the lens the keeper tends."}},
		},
		{
			File: lens, Quote: "The lamp is lit at dusk…", Traces: []string{}, Grades: []string{"weak"},
			Flags: []repair.Flag{{Step: repair.StepSupport, Grades: []string{"weak"}, Quote: "The lamp is lit at dusk…", Reason: "thin."}},
		},
	}
	if !reflect.DeepEqual(fails, wantFails) {
		t.Errorf("still fails:\n got %+v\nwant %+v", fails, wantFails)
	}
	// Segment 1's notes gained no statement, so each of its kept losses is
	// listed as dropped.
	if want := []dropped.Item{unadded(1, 1), unadded(1, 2)}; !reflect.DeepEqual(items, want) {
		t.Errorf("dropped:\n got %+v\nwant %+v", items, want)
	}

	// Nothing the call wrote is broken or untraced, so the final notes are
	// the kept-loss repair's.
	if err := keptloss.WriteFinal(src, folder); err != nil {
		t.Fatal(err)
	}
	if got := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes))); !reflect.DeepEqual(got, after) {
		t.Errorf("final notes are not the kept-loss repair's: %q", got)
	}
}

// still checks that the step is complete, and gives what it lists as still
// failing and dropped. Still fails is a list even when empty, never null.
func still(t *testing.T, folder *run.Folder) ([]repair.Entry, []dropped.Item) {
	t.Helper()
	if !folder.Complete(run.KeptLossDir) {
		t.Fatalf("the step is not complete")
	}
	var fails []repair.Entry
	if err := json.Unmarshal([]byte(testfixture.Files(t, filepath.Join(folder.Dir, run.KeptLossDir))[run.StillFailsFile]), &fails); err != nil {
		t.Fatal(err)
	}
	if fails == nil {
		t.Errorf("%s is null, want a list", run.StillFailsFile)
	}
	items, err := dropped.Read(folder, run.KeptLossDir)
	if err != nil {
		t.Fatal(err)
	}
	return fails, items
}

// unadded is the dropped item for kept loss k of segment n, whose notes
// gained no statement.
func unadded(n, k int) dropped.Item {
	id := strconv.Itoa(n) + "." + strconv.Itoa(k)
	return dropped.Item{Segment: n, What: "loss " + id, Loss: id,
		Why: "the kept-loss call added no statement to the segment's notes; its report is 1.4-kept-loss-repair/reports/" + strconv.Itoa(n) + ".md"}
}

// segment3Losses are step 1.5's union with runLosses's segment 3 loss alone.
var segment3Losses = []lossproposer.Segment{
	{Segment: 0, Heading: "2. Keepers of the Lamp", Losses: []lossproposer.Loss{}, Other: []lossproposer.Line{}},
	{Segment: 1, Heading: "The Keeper’s Watch", Losses: []lossproposer.Loss{}, Other: []lossproposer.Line{}},
	{Segment: 2, Heading: "The Lens", Losses: []lossproposer.Loss{}, Other: []lossproposer.Line{}},
	runLosses[3],
}

// The Storms note and the statement segment 3's kept loss adds, as a reply
// writes them.
var (
	stormsNote = testfixture.LampStage1()[storms]
	stood      = "I have stood in that lamp room in a gale, and the glass shook."
	// stormsStood is stormsNote with stood added after its last statement,
	// traced as [^4].
	stormsStood = strings.Replace(stormsNote, "every gale.[^3]\n", "every gale.[^3] "+stood+"[^4]\n", 1) +
		"[^4]: " + stormsTrace(stood) + "\n"
	// stormsBroken is stormsStood with the new statement's trace quoting
	// what the source doesn't say.
	stormsBroken = strings.Replace(stormsStood, stormsTrace(stood), stormsTrace("I have stood in that lamp room in a storm."), 1)
)

// replyCase is segment 3's kept-loss reply, briefed for shape and
// quoteOnce, the segment's notes it should leave, by file name, and what it
// should drop.
type replyCase struct {
	name      string
	shape     repair.Reply
	quoteOnce bool
	reply     string
	notes     map[string]string
	dropped   []dropped.Item
}

// replyCases are a reply per shape the step applies, then one per block or
// edit it drops. Each is run alone, on segment3Losses, as segment 3's reply,
// and its re-check passes.
var replyCases = []replyCase{
	// Whole notes: a FILE block replaces its note.
	{name: "whole notes: the note in full", shape: repair.Whole,
		reply: "=== FILE: " + storms + " ===\n" + stormsStood + "=== REPORT ===\nr\n",
		notes: map[string]string{storms: stormsStood}},
	// A new statement whose trace is broken is kept, and isn't listed as
	// still failing: step 1.2's flags are the final notes' to act on
	// (TestWriteFinal_DropsABrokenStatement).
	{name: "whole notes: a new statement with a broken trace", shape: repair.Whole,
		reply: "=== FILE: " + storms + " ===\n" + stormsBroken + "=== REPORT ===\nr\n",
		notes: map[string]string{storms: stormsBroken}},
	// Edits, made through the note model, may insert a statement.
	{name: "edits: an insert", shape: repair.Edits,
		reply: "=== EDIT: " + storms + " ===\n" +
			"--- AFTER ---\nHe stayed at the lamp (in 1839, for nine days) through every gale.[^3]\n" +
			"--- INSERT ---\n" + stood + "[^1]\n[^1]: " + stormsTrace(stood) + "\n" +
			"=== REPORT ===\nr\n",
		notes: map[string]string{storms: stormsStood}},
	// Unlike step 1.4, the call may add a paragraph.
	{name: "edits: a paragraph added", shape: repair.Edits,
		reply: "=== EDIT: " + storms + " ===\n" +
			"--- AFTER ---\nHe stayed at the lamp (in 1839, for nine days) through every gale.[^3]\n" +
			"--- PARAGRAPH ---\n" + stood + "[^1]\n[^1]: " + stormsTrace(stood) + "\n" +
			"=== REPORT ===\nr\n",
		notes: map[string]string{storms: strings.Replace(stormsStood, "every gale.[^3] ", "every gale.[^3]\n\n", 1)}},
	// With quote-once, a note given in full without footnotes is traced by
	// s2n.
	{name: "quote-once: the note in full traced", shape: repair.Edits, quoteOnce: true,
		reply: "=== FILE: " + storms + " ===\n# 3 Storms\n\n" +
			"In a storm the light matters most.\n" +
			"{A wreck was his }failure, whatever the weather.\n" +
			"{He }stayed at the lamp (in 1839, for nine days) through every gale.\n" +
			stood + "\n" +
			"=== REPORT ===\nr\n",
		notes: map[string]string{storms: "# 3 Storms\n\n" +
			"In a storm the light matters most.[^1]\n" +
			"A wreck was his failure, whatever the weather.[^2]\n" +
			"He stayed at the lamp (in 1839, for nine days) through every gale.[^3]\n" +
			stood + "[^4]\n\n" +
			"[^1]: " + stormsTrace("In a storm the light matters most.") + "\n" +
			"[^2]: " + stormsTrace("failure, whatever the weather.") + "\n" +
			"[^3]: " + stormsTrace("stayed at the lamp (in 1839, for nine days) through every gale.") + "\n" +
			"[^4]: " + stormsTrace(stood) + "\n"}},
	// The call adds statements and changes none, so an edit that replaces
	// one is dropped, and the segment's kept loss with it.
	{name: "an edit replacing a statement", shape: repair.Edits,
		reply: "=== EDIT: " + storms + " ===\n--- REPLACE ---\nIn a storm the light matters most.[^1]\n--- WITH ---\n=== REPORT ===\nr\n",
		notes: map[string]string{storms: stormsNote},
		dropped: []dropped.Item{
			{Segment: 3, Note: storms, What: `the edit at "In a storm the light matters most.[^1]"`, Why: "it replaces a statement, which this call may not do"},
			unadded(3, 1),
		}},
	// The call rewrites only the segment's existing notes: any other block
	// is dropped.
	{name: "the note deleted", shape: repair.Edits,
		reply: "=== DELETE: " + storms + " ===\n=== REPORT ===\nr\n",
		notes: map[string]string{storms: stormsNote},
		dropped: []dropped.Item{
			{Segment: 3, Note: storms, What: "the reply's DELETE", Why: "the kept-loss call deletes no note"},
			unadded(3, 1),
		}},
	// A block whose name is no note file name is dropped with the note
	// model's reason.
	{name: "a name that is no note's", shape: repair.Edits,
		reply: "=== FILE: ../3 Storms.md ===\n" + stormsStood + "=== REPORT ===\nr\n",
		notes: map[string]string{storms: stormsNote},
		dropped: []dropped.Item{
			{Segment: 3, Note: "../3 Storms.md", What: "the reply's FILE block", Why: `note name "../3 Storms.md" is not a plain file name`},
			unadded(3, 1),
		}},
	{name: "a new note", shape: repair.Edits,
		reply: "=== FILE: 3 Storms - Aside - The gale.md ===\n# 3 Storms - Aside - The gale\n\n" + stood + "\n=== REPORT ===\nr\n",
		notes: map[string]string{storms: stormsNote},
		dropped: []dropped.Item{
			{Segment: 3, Note: "3 Storms - Aside - The gale.md", What: "the reply's FILE block", Why: "it is not a note; the kept-loss call adds no note"},
			unadded(3, 1),
		}},
	// Another segment's note is a note, so only the segment's prefix keeps
	// it out.
	{name: "another segment's note", shape: repair.Edits,
		reply: "=== FILE: " + lens + " ===\n# 2 The Lens\n\n" + stood + "\n=== REPORT ===\nr\n",
		notes: map[string]string{storms: stormsNote},
		dropped: []dropped.Item{
			{Segment: 3, Note: lens, What: "the reply's FILE block", Why: "it is not one of the segment's notes"},
			unadded(3, 1),
		}},
	// Which of an edit and a rewrite of one note comes first is not the
	// reply's to say, so both blocks are dropped.
	{name: "the note edited and given in full", shape: repair.Edits,
		reply: "=== EDIT: " + storms + " ===\n" +
			"--- AFTER ---\nHe stayed at the lamp (in 1839, for nine days) through every gale.[^3]\n" +
			"--- INSERT ---\n" + stood + "[^1]\n[^1]: " + stormsTrace(stood) + "\n" +
			"=== FILE: " + storms + " ===\n" + stormsStood + "=== REPORT ===\nr\n",
		notes: map[string]string{storms: stormsNote},
		dropped: []dropped.Item{
			{Segment: 3, Note: storms, What: "the reply's EDIT block", Why: "the reply both edits the note and gives it in full or deletes it"},
			{Segment: 3, Note: storms, What: "the reply's FILE block", Why: "the reply both edits the note and gives it in full or deletes it"},
			unadded(3, 1),
		}},
}

// runSegment3 runs the kept-loss repair on segment3Losses, briefed for
// shape and quoteOnce, serving reply to segment 3's call and a passing
// report to its re-check.
func runSegment3(t *testing.T, shape repair.Reply, quoteOnce bool, reply string) (*source.Source, *run.Folder, *testfixture.Fake) {
	t.Helper()
	src, folder := proposedRun(t, segment3Losses)
	p := testfixture.Serve(t,
		testfixture.Case{Contains: keptCall(3), Reply: reply},
		testfixture.Case{Contains: recheckCall(3), Reply: "# 3 Storms\n\npass\n"})
	if err := keptloss.Run(context.Background(), p, src, "opus", shape, quoteOnce, folder); err != nil {
		t.Fatal(err)
	}
	return src, folder, p
}

// TestRun_Replies runs the kept-loss repair with each reply case as segment
// 3's reply, the only one called, and checks the call and what the step
// wrote.
func TestRun_Replies(t *testing.T) {
	for _, c := range replyCases {
		t.Run(c.name, func(t *testing.T) {
			_, folder, p := runSegment3(t, c.shape, c.quoteOnce, c.reply)

			files := briefFiles[c.quoteOnce][c.shape]
			var kept int
			for _, call := range p.Calls() {
				if !isKeptCall(call.User) {
					continue
				}
				kept++
				if call.System != wantSystem(t, files[0], files[1]) {
					t.Errorf("the call is not briefed by %s and %s, then the frame", files[0], files[1])
				}
			}
			if kept != 1 {
				t.Errorf("%d kept-loss calls, want 1: only segment 3 has a proposed loss", kept)
			}
			step := testfixture.Files(t, filepath.Join(folder.Dir, run.KeptLossDir))
			if usage := step[run.UsageFile]; !strings.Contains(usage, `"brief":"`+files[0]+`"`) {
				t.Errorf("usage log does not name %s:\n%s", files[0], usage)
			}
			if got := step[run.RawDir+"/3.txt"]; got != c.reply {
				t.Errorf("raw reply is not the reply:\n got %q\nwant %q", got, c.reply)
			}
			if got := step[run.ReportsDir+"/3.md"]; got != "r\n" {
				t.Errorf("report %q, want the reply's", got)
			}

			after := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.KeptLossNotes)))
			if got := testfixture.SegmentNotes(after, 3); !reflect.DeepEqual(got, c.notes) {
				t.Errorf("segment 3's notes:\n got %q\nwant %q", got, c.notes)
			}
			for _, n := range []int{0, 1, 2} {
				if got, want := testfixture.SegmentNotes(after, n), testfixture.SegmentNotes(testfixture.LampStage1(), n); !reflect.DeepEqual(got, want) {
					t.Errorf("segment %d's notes are not step 1.4's", n)
				}
			}
			fails, items := still(t, folder)
			if len(fails) != 0 {
				t.Errorf("still fails %+v, want nothing: the re-check passed", fails)
			}
			if want := c.dropped; !reflect.DeepEqual(items, want) && (len(items) != 0 || len(want) != 0) {
				t.Errorf("dropped:\n got %+v\nwant %+v", items, want)
			}
		})
	}
}

// A new statement with a broken trace stops nothing in the step: the final
// notes drop it, with its trace, and list it.
func TestWriteFinal_DropsABrokenStatement(t *testing.T) {
	src, folder, _ := runSegment3(t, repair.Whole, false, "=== FILE: "+storms+" ===\n"+stormsBroken+"=== REPORT ===\nr\n")
	if err := keptloss.WriteFinal(src, folder); err != nil {
		t.Fatal(err)
	}
	final := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes)))
	if !reflect.DeepEqual(final, testfixture.LampStage1()) {
		t.Errorf("final notes:\n%q\nwant step 1.4's, the broken statement and its trace gone", final[storms])
	}
	items, err := dropped.Read(folder, run.FinalDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []dropped.Item{{Segment: 3, Note: storms, What: stood + "[^4]", Why: "BROKEN [^4] I have stood in that lamp room in a storm."}}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("dropped:\n got %+v\nwant %+v", items, want)
	}
}

// keptLossNotes makes a run folder for the invented chapter whose kept-loss
// repair wrote LampStage1, with notes in place of any it names, and ran
// step 1.2 on them, as its re-check does.
func keptLossNotes(t *testing.T, notes map[string]string) (*source.Source, *run.Folder) {
	t.Helper()
	src, folder := testfixture.LampExtractRun(t)
	dir := filepath.Join(folder.Dir, filepath.FromSlash(run.KeptLossNotes))
	testfixture.WriteFiles(t, dir, testfixture.LampStage1())
	testfixture.WriteFiles(t, dir, notes)
	inner := &run.Folder{Dir: filepath.Join(folder.Dir, run.KeptLossDir)}
	if err := mechcheck.RunOn(src, inner, run.NotesDir, run.KeptLossMechanicalDir); err != nil {
		t.Fatal(err)
	}
	return src, folder
}

// An untraced statement in an aside is dropped, and so is an aside it
// leaves with no statement. An aside with a statement left is kept, and so
// is one that had no statement to begin with: only a drop empties an aside.
func TestWriteFinal_DropsAnEmptiedAside(t *testing.T) {
	const bare = "3 Storms - Aside - Calm.md"
	src, folder := keptLossNotes(t, map[string]string{
		"2 The Lens - Aside - Fresnel.md": "# 2 The Lens - Aside - Fresnel\n\nThe Fresnel lens was a French idea.\n",
		aside:                             strings.Replace(testfixture.LampStage1()[aside], "every page.[^2]\n", "every page.[^2]\nThe log was kept in ink.\n", 1),
		bare:                              "# 3 Storms - Aside - Calm\n",
	})
	if err := keptloss.WriteFinal(src, folder); err != nil {
		t.Fatal(err)
	}
	final := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes)))
	wantNotes := testfixture.LampStage1()
	wantNotes[bare] = "# 3 Storms - Aside - Calm\n"
	if !reflect.DeepEqual(final, wantNotes) {
		t.Errorf("final notes %q, want LampStage1 and the bare aside: the emptied aside gone, the other's untraced statement dropped", final)
	}
	items, err := dropped.Read(folder, run.FinalDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []dropped.Item{
		{Segment: 1, Note: aside, What: "The log was kept in ink.", Why: "UNTRACED statement: The log was kept in ink."},
		{Segment: 2, Note: "2 The Lens - Aside - Fresnel.md", What: "The Fresnel lens was a French idea.", Why: "UNTRACED statement: The Fresnel lens was a French idea."},
		{Segment: 2, Note: "2 The Lens - Aside - Fresnel.md", What: "the note", Why: "an aside left with no statement"},
	}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("dropped:\n got %+v\nwant %+v", items, want)
	}
}

// An untraced statement a note has twice is dropped both times, and listed
// once.
func TestWriteFinal_DropsEachCopyOfAStatement(t *testing.T) {
	dup := strings.Replace(stormsNote, "\n\n[^1]", "\n\nThe sea was cruel.\n\nThe sea was cruel.\n\n[^1]", 1)
	src, folder := keptLossNotes(t, map[string]string{storms: dup})
	if err := keptloss.WriteFinal(src, folder); err != nil {
		t.Fatal(err)
	}
	final := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes)))
	if final[storms] != stormsNote {
		t.Errorf("final %s:\n%q\nwant both copies of the untraced statement gone", storms, final[storms])
	}
	items, err := dropped.Read(folder, run.FinalDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []dropped.Item{{Segment: 3, Note: storms, What: "The sea was cruel.", Why: "UNTRACED statement: The sea was cruel."}}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("dropped:\n got %+v\nwant %+v", items, want)
	}
}
