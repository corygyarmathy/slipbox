package supportcheck_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/checker"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/supportcheck"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// TestShared pins the notes block every call shares on the invented
// chapter's stage 1 notes: every note, the segment's own too, in name order.
func TestShared(t *testing.T) {
	testfixture.Golden(t, "shared.txt", supportcheck.Shared(testfixture.LampStage1Notes(t)))
}

// TestPrompt pins each segment's user prompt on the invented chapter's stage
// 1 notes: the names of its own notes, segment 1's aside included, and not
// their text, which the shared block gives.
func TestPrompt(t *testing.T) {
	notes := testfixture.LampStage1Notes(t)
	for _, seg := range testfixture.LampSource(t).Segments {
		got := supportcheck.Prompt(notes, seg)
		testfixture.Golden(t, "prompt/"+strconv.Itoa(seg.Number)+".txt", got)
		if strings.Contains(got, "--- NOTE: ") {
			t.Errorf("%s: prompt gives a note again: %q", seg, got)
		}
	}
}

// replyCase is a segment's reply and the flags Run should parse from it.
type replyCase struct {
	name    string
	segment int
	reply   string
	want    checker.Report
}

// replyCases are one reply per segment of the invented chapter, each
// covering a shape of report the step handles. Run makes one call per
// segment, so a case that covers several deviations gives them in one
// reply, and its name says which.
var replyCases = []replyCase{
	// A well-formed report: flags under the note's H1, each given the note's
	// file, and the counts line kept under it.
	{"well-formed, reworded graded", 0, "# 0 Keepers of the Lamp\n\n" +
		"- [weak] \"It burns whether or not a ship is near.\": the section says it burns, not that it burns for no ship.\n" +
		"- [reworded] \"The keeper kept that promise…\": the quote reads \"for the harbour\", which the statement moves.\n" +
		"pass 1 / weak 1 / fail 0 / dangling 0 / outside-frame 0\n",
		checker.Report{
			Flags: []checker.Flag{
				{H1: "0 Keepers of the Lamp", File: "0 Keepers of the Lamp.md", Grades: []checker.Grade{checker.Weak},
					Statement: "It burns whether or not a ship is near.", Reason: "the section says it burns, not that it burns for no ship."},
				{H1: "0 Keepers of the Lamp", File: "0 Keepers of the Lamp.md", Grades: []checker.Grade{checker.Reworded},
					Statement: "The keeper kept that promise…", Reason: `the quote reads "for the harbour", which the statement moves.`},
			},
			Other: []checker.Line{{H1: "0 Keepers of the Lamp", Text: "pass 1 / weak 1 / fail 0 / dangling 0 / outside-frame 0"}},
		}},
	// Two statements graded on one line give a flag each. A flag under the
	// aside's H1 is given the aside's file. An H1 that names none of the
	// segment's notes gives its flag no file: here one with a dropped
	// apostrophe, and one naming another segment's note.
	{"two statements on a line, aside, H1 of no note of the segment", 1, "# 1 The Keeper's Watch\n\n" +
		"- [dangling] \"He trimmed the wick…\" / \"He logged the weather…\": \"He\" has no antecedent in the note.\n\n" +
		"# 1 The Keeper's Watch - Aside - The log\n\n" +
		"- [dangling] \"Then they read every page.\": \"they\" has no antecedent in the note.\n\n" +
		"# 1 The Keepers Watch\n\n" +
		"- [cut would do] \"The watch began at dusk.\": the whole sentence is quoted where \"at dusk\" would do.\n\n" +
		"# 2 The Lens\n\n" +
		"- [weak] \"Each ring bends the light…\": the section gives no rings.\n",
		checker.Report{
			Flags: []checker.Flag{
				{H1: "1 The Keeper's Watch", File: "1 The Keeper's Watch.md", Grades: []checker.Grade{checker.Dangling},
					Statement: "He trimmed the wick…", Reason: `"He" has no antecedent in the note.`},
				{H1: "1 The Keeper's Watch", File: "1 The Keeper's Watch.md", Grades: []checker.Grade{checker.Dangling},
					Statement: "He logged the weather…", Reason: `"He" has no antecedent in the note.`},
				{H1: "1 The Keeper's Watch - Aside - The log", File: "1 The Keeper's Watch - Aside - The log.md", Grades: []checker.Grade{checker.Dangling},
					Statement: "Then they read every page.", Reason: `"they" has no antecedent in the note.`},
				{H1: "1 The Keepers Watch", File: "", Grades: []checker.Grade{checker.CutWouldDo},
					Statement: "The watch began at dusk.", Reason: `the whole sentence is quoted where "at dusk" would do.`},
				{H1: "2 The Lens", File: "", Grades: []checker.Grade{checker.Weak},
					Statement: "Each ring bends the light…", Reason: "the section gives no rings."},
			},
			Other: []checker.Line{},
		}},
	// A report that flags statements, then withdraws the flags. They stand:
	// a report's withdrawals are the repair call's to read, and the
	// withdrawal is kept as text.
	{"withdrawn flags stand", 2, "# 2 The Lens\n\n" +
		"- [fail] \"Obviously, a keeper can take no notice…\": the section says nothing of the keeper.\n" +
		"- [outside-frame] \"It uses rings of prisms…\": \"It\" is the Fresnel lens, which only the source names.\n\n" +
		"Correcting the above: both pass. The first quotes the section word for word, and \"It\" is the lens the note names.\n" +
		"pass 5 / weak 0 / fail 0 / dangling 0 / outside-frame 0\n",
		checker.Report{
			Flags: []checker.Flag{
				{H1: "2 The Lens", File: "2 The Lens.md", Grades: []checker.Grade{checker.Fail},
					Statement: "Obviously, a keeper can take no notice…", Reason: "the section says nothing of the keeper."},
				{H1: "2 The Lens", File: "2 The Lens.md", Grades: []checker.Grade{checker.OutsideFrame},
					Statement: "It uses rings of prisms…", Reason: `"It" is the Fresnel lens, which only the source names.`},
			},
			Other: []checker.Line{
				{H1: "2 The Lens", Text: `Correcting the above: both pass. The first quotes the section word for word, and "It" is the lens the note names.`},
				{H1: "2 The Lens", Text: "pass 5 / weak 0 / fail 0 / dangling 0 / outside-frame 0"},
			},
		}},
	// A report with no flag: its flags are an empty list.
	{"no flag", 3, "# 3 Storms\n\npass 3 / weak 0 / fail 0 / dangling 0 / outside-frame 0\n",
		checker.Report{
			Flags: []checker.Flag{},
			Other: []checker.Line{{H1: "3 Storms", Text: "pass 3 / weak 0 / fail 0 / dangling 0 / outside-frame 0"}},
		}},
}

// extractFolder makes a run folder for the invented chapter whose step 1.1
// wrote its stage 1 notes.
func extractFolder(t *testing.T) (*source.Source, *run.Folder) {
	t.Helper()
	src := testfixture.LampSource(t)
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	if err := folder.SaveSource(src.Name, []byte(src.Frame)); err != nil {
		t.Fatal(err)
	}
	testfixture.WriteFiles(t, filepath.Join(folder.Dir, filepath.FromSlash(run.ExtractNotes)), testfixture.LampStage1())
	return src, folder
}

// TestRun runs step 1.3a on the invented chapter, serving each segment its
// reply case, and checks the requests it made and the reports and flags it
// wrote.
func TestRun(t *testing.T) {
	src, folder := extractFolder(t)
	var cases []testfixture.Case
	for _, c := range replyCases {
		cases = append(cases, testfixture.Case{Contains: fmt.Sprintf("(segment %d: ", c.segment), Reply: c.reply})
	}
	p := testfixture.Serve(t, cases...)
	if err := supportcheck.Run(context.Background(), p, src, folder); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(folder.Dir, run.SupportCheckDir)
	for _, c := range replyCases {
		t.Run(c.name, func(t *testing.T) {
			n := strconv.Itoa(c.segment)
			report, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path.Join(run.ReportsDir, n+".md"))))
			if err != nil {
				t.Fatal(err)
			}
			if string(report) != c.reply {
				t.Errorf("segment %d: report is not the reply:\n got %q\nwant %q", c.segment, report, c.reply)
			}
			b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path.Join(run.FlagsDir, n+".json"))))
			if err != nil {
				t.Fatal(err)
			}
			var got checker.Report
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("segment %d: flags: %v", c.segment, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("segment %d: flags:\n got %+v\nwant %+v", c.segment, got, c.want)
			}
		})
	}

	notes := testfixture.LampStage1Notes(t)
	brief := testfixture.RepoFile(t, "internal/briefs/checker-support.md")
	wantSystem := brief + "\n\n=== FRAME: " + testfixture.Lamp + ".md ===\n" + testfixture.LampChapter + "\n=== END FRAME ===\n\n" + supportcheck.Shared(notes)
	calls := p.Calls()
	if len(calls) != len(src.Segments) {
		t.Fatalf("%d calls, want one per segment (%d)", len(calls), len(src.Segments))
	}
	var users []string
	for _, c := range calls {
		if c.System != wantSystem {
			t.Errorf("call for %.80q: system prompt is not checker-support.md, the frame and every note", c.User)
		}
		if c.Model != "sonnet" || c.Effort != "medium" {
			t.Errorf("call for %.80q: model %q at effort %q, want sonnet at medium", c.User, c.Model, c.Effort)
		}
		users = append(users, c.User)
	}
	for _, seg := range src.Segments {
		if want := supportcheck.Prompt(notes, seg); !slices.Contains(users, want) {
			t.Errorf("%s: no call has its prompt %q", seg, want)
		}
	}
	if !strings.Contains(calls[0].User, "(segment 0: ") {
		t.Errorf("first call was not segment 0's: %.80q", calls[0].User)
	}
}
