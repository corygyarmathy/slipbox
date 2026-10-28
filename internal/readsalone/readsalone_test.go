package readsalone_test

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
	"github.com/corygyarmathy/slipbox/internal/readsalone"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// TestPrompt pins each segment's user prompt on the invented chapter's stage
// 1 notes: its own notes in full, segment 1's aside included, and no other
// segment's, as the trialled prompt was without the other segments' block.
func TestPrompt(t *testing.T) {
	notes := testfixture.LampStage1Notes(t)
	for _, seg := range testfixture.LampSource(t).Segments {
		got := readsalone.Prompt(notes, seg)
		testfixture.Golden(t, "prompt/"+strconv.Itoa(seg.Number)+".txt", got)
		if strings.Contains(got, "OTHER SEGMENTS") {
			t.Errorf("%s: prompt has an other segments' block", seg)
		}
		for _, n := range notes {
			in := strings.Contains(got, "--- NOTE: "+n.Name+" ---")
			own := strings.HasPrefix(n.Name, strconv.Itoa(seg.Number)+" ")
			if in != own {
				t.Errorf("%s: note %q in the prompt: %v, want %v", seg, n.Name, in, own)
			}
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
	// A well-formed report: a flag for each of the check's two grades under
	// the note's H1, each given the note's file, and the counts line kept
	// under it.
	{"well-formed, dangling and outside-frame", 3, "# 3 Storms\n\n" +
		"- [dangling] \"A wreck was his failure…\": \"his\": no earlier statement in the note says whose failure.\n" +
		"- [outside-frame] \"He stayed at the lamp…\": \"He\": the keeper the last chapter introduced, whom the note never names.\n" +
		"pass 1 / dangling 1 / outside-frame 1\n",
		checker.Report{
			Flags: []checker.Flag{
				{H1: "3 Storms", File: "3 Storms.md", Grades: []checker.Grade{checker.Dangling},
					Statement: "A wreck was his failure…", Reason: `"his": no earlier statement in the note says whose failure.`},
				{H1: "3 Storms", File: "3 Storms.md", Grades: []checker.Grade{checker.OutsideFrame},
					Statement: "He stayed at the lamp…", Reason: `"He": the keeper the last chapter introduced, whom the note never names.`},
			},
			Other: []checker.Line{{H1: "3 Storms", Text: "pass 1 / dangling 1 / outside-frame 1"}},
		}},
	// Two statements graded on one line give a flag each. A flag under the
	// aside's H1 is given the aside's file. A flag under another segment's
	// note's H1 is given no file: the prompt held only this segment's notes,
	// so that H1 names none of them.
	{"two statements on a line, aside, H1 of another segment's note", 1, "# 1 The Keeper's Watch\n\n" +
		"- [dangling] \"He trimmed the wick…\" / \"He logged the weather…\": \"He\": no earlier statement names who.\n" +
		"pass 1 / dangling 2 / outside-frame 0\n\n" +
		"# 1 The Keeper's Watch - Aside - The log\n\n" +
		"- [dangling] \"Then they read every page.\": \"they\": the note never says who read it.\n" +
		"pass 1 / dangling 1 / outside-frame 0\n\n" +
		"# 2 The Lens\n\n" +
		"- [dangling] \"On a foggy night it is no help at all.\": \"it\": the lens or the night.\n",
		checker.Report{
			Flags: []checker.Flag{
				{H1: "1 The Keeper's Watch", File: "1 The Keeper's Watch.md", Grades: []checker.Grade{checker.Dangling},
					Statement: "He trimmed the wick…", Reason: `"He": no earlier statement names who.`},
				{H1: "1 The Keeper's Watch", File: "1 The Keeper's Watch.md", Grades: []checker.Grade{checker.Dangling},
					Statement: "He logged the weather…", Reason: `"He": no earlier statement names who.`},
				{H1: "1 The Keeper's Watch - Aside - The log", File: "1 The Keeper's Watch - Aside - The log.md", Grades: []checker.Grade{checker.Dangling},
					Statement: "Then they read every page.", Reason: `"they": the note never says who read it.`},
				{H1: "2 The Lens", File: "", Grades: []checker.Grade{checker.Dangling},
					Statement: "On a foggy night it is no help at all.", Reason: `"it": the lens or the night.`},
			},
			Other: []checker.Line{
				{H1: "1 The Keeper's Watch", Text: "pass 1 / dangling 2 / outside-frame 0"},
				{H1: "1 The Keeper's Watch - Aside - The log", Text: "pass 1 / dangling 1 / outside-frame 0"},
			},
		}},
	// A report that also grades a statement's support, which is the support
	// check's to grade. The line is kept as text, not a flag: only
	// checker-reads-alone.md's grades are flags here.
	{"a support check grade is not a flag", 2, "# 2 The Lens\n\n" +
		"- [weak] \"Obviously, a keeper can take no notice…\": the section says a keeper can, not that one does.\n" +
		"- [dangling] \"It uses rings of prisms…\": \"It\": the note has named the lens, the beam and the night.\n" +
		"pass 4 / dangling 1 / outside-frame 0\n",
		checker.Report{
			Flags: []checker.Flag{
				{H1: "2 The Lens", File: "2 The Lens.md", Grades: []checker.Grade{checker.Dangling},
					Statement: "It uses rings of prisms…", Reason: `"It": the note has named the lens, the beam and the night.`},
			},
			Other: []checker.Line{
				{H1: "2 The Lens", Text: `- [weak] "Obviously, a keeper can take no notice…": the section says a keeper can, not that one does.`},
				{H1: "2 The Lens", Text: "pass 4 / dangling 1 / outside-frame 0"},
			},
		}},
	// A report with no flag: its flags are an empty list.
	{"no flag", 0, "# 0 Keepers of the Lamp\n\npass 3 / dangling 0 / outside-frame 0\n",
		checker.Report{
			Flags: []checker.Flag{},
			Other: []checker.Line{{H1: "0 Keepers of the Lamp", Text: "pass 3 / dangling 0 / outside-frame 0"}},
		}},
}

// TestRun runs step 1.3b on the invented chapter, serving each segment its
// reply case, and checks the requests it made and the reports and flags it
// wrote.
func TestRun(t *testing.T) {
	src, folder := testfixture.LampExtractRun(t)
	var cases []testfixture.Case
	for _, c := range replyCases {
		cases = append(cases, testfixture.Case{Contains: fmt.Sprintf("(segment %d: ", c.segment), Reply: c.reply})
	}
	p := testfixture.Serve(t, cases...)
	if err := readsalone.Run(context.Background(), p, src, folder); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(folder.Dir, run.ReadsAloneDir)
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
	brief := testfixture.RepoFile(t, "internal/briefs/checker-reads-alone.md")
	wantSystem := brief + "\n\n=== FRAME: " + testfixture.Lamp + ".md ===\n" + testfixture.LampChapter + "\n=== END FRAME ===\n"
	calls := p.Calls()
	if len(calls) != len(src.Segments) {
		t.Fatalf("%d calls, want one per segment (%d)", len(calls), len(src.Segments))
	}
	var users []string
	for _, c := range calls {
		if c.System != wantSystem {
			t.Errorf("call for %.80q: system prompt is not checker-reads-alone.md and the frame alone", c.User)
		}
		if c.Model != "sonnet" || c.Effort != "medium" {
			t.Errorf("call for %.80q: model %q at effort %q, want sonnet at medium", c.User, c.Model, c.Effort)
		}
		users = append(users, c.User)
	}
	for _, seg := range src.Segments {
		if want := readsalone.Prompt(notes, seg); !slices.Contains(users, want) {
			t.Errorf("%s: no call has its prompt %q", seg, want)
		}
	}
	if !strings.Contains(calls[0].User, "(segment 0: ") {
		t.Errorf("first call was not segment 0's: %.80q", calls[0].User)
	}
}
