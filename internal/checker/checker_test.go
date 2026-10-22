package checker_test

import (
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/checker"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/readsalone"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/supportcheck"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

func TestParse(t *testing.T) {
	report := "- [weak] \"Before any H1\": reason\n" +
		"# 1 A\n" +
		"\n" +
		"- [cut lost content] \"Drops a reason…\": the cut \"because\" carried the reason.\n" +
		"  * [reads wrong] “Curly quotes”: no sentence left\r\n" +
		"- [wrong referent / reads wrong] \"Two grades\": both\n" +
		"- [pass] \"A pass\": fine\n" +
		"- [weak / unclear] \"Half a grade\": kept\n" +
		"- [unclear] \"Not a grade\": kept\n" +
		"- [fail] no quotes: a flag\n" +
		"- [^6] \"a name span\": pass.\n" +
		"pass 3 / weak 0 / fail 0 / dangling 0 / outside-frame 0\n" +
		"#  2 B \n" +
		"- [not a name span] \"Empty reason\":\n" +
		"## 3 C\n" +
		"- [dangling] \"Under an H2\": \"this\": no antecedent\n" +
		"#no space\n"
	want := checker.Report{
		Flags: []checker.Flag{
			{H1: "", Grades: []checker.Grade{checker.Weak}, Statement: "Before any H1", Reason: "reason"},
			{H1: "1 A", Grades: []checker.Grade{checker.CutLostContent}, Statement: "Drops a reason…", Reason: `the cut "because" carried the reason.`},
			{H1: "1 A", Grades: []checker.Grade{checker.ReadsWrong}, Statement: "Curly quotes", Reason: "no sentence left"},
			{H1: "1 A", Grades: []checker.Grade{checker.WrongReferent, checker.ReadsWrong}, Statement: "Two grades", Reason: "both"},
			{H1: "1 A", Grades: []checker.Grade{checker.Fail}, Statement: "", Reason: "no quotes: a flag"},
			{H1: "2 B", Grades: []checker.Grade{checker.NotANameSpan}, Statement: "Empty reason", Reason: ""},
			{H1: "3 C", Grades: []checker.Grade{checker.Dangling}, Statement: "Under an H2", Reason: `"this": no antecedent`},
		},
		Other: []checker.Line{
			{H1: "1 A", Text: `- [pass] "A pass": fine`},
			{H1: "1 A", Text: `- [weak / unclear] "Half a grade": kept`},
			{H1: "1 A", Text: `- [unclear] "Not a grade": kept`},
			{H1: "1 A", Text: `- [^6] "a name span": pass.`},
			{H1: "1 A", Text: "pass 3 / weak 0 / fail 0 / dangling 0 / outside-frame 0"},
			{H1: "3 C", Text: "#no space"},
		},
	}
	if got := checker.Parse(report, supportcheck.Grades); !reflect.DeepEqual(got, want) {
		t.Errorf("Parse:\n got %+v\nwant %+v", got, want)
	}
	if got := checker.Parse("", supportcheck.Grades); got.Flags == nil || got.Other == nil {
		t.Errorf("Parse(\"\") = %+v, want empty lists, which encode as [] not null", got)
	}
}

// A check accepts only its own brief's grades: a reads-alone report line
// graded as the support check would grade it is kept as text.
func TestParse_OnlyTheCheckGrades(t *testing.T) {
	report := "# 1 A\n" +
		"- [dangling] \"Dangles\": \"it\": no antecedent\n" +
		"- [outside-frame] \"Points out\": \"the last chapter\": outside the source\n" +
		"- [weak] \"Support grade\": not this check's\n" +
		"- [dangling / reads wrong] \"Half its own\": kept\n"
	want := checker.Report{
		Flags: []checker.Flag{
			{H1: "1 A", Grades: []checker.Grade{checker.Dangling}, Statement: "Dangles", Reason: `"it": no antecedent`},
			{H1: "1 A", Grades: []checker.Grade{checker.OutsideFrame}, Statement: "Points out", Reason: `"the last chapter": outside the source`},
		},
		Other: []checker.Line{
			{H1: "1 A", Text: `- [weak] "Support grade": not this check's`},
			{H1: "1 A", Text: `- [dangling / reads wrong] "Half its own": kept`},
		},
	}
	if got := checker.Parse(report, readsalone.Grades); !reflect.DeepEqual(got, want) {
		t.Errorf("Parse:\n got %+v\nwant %+v", got, want)
	}
}

// A graded line need not have the briefs' shape to be a flag. Each quoted
// span before the first colon outside quotes, or in the line if it has no
// such colon, is a statement; a line with none is one flag with no statement.
func TestParse_GradedShapes(t *testing.T) {
	report := "# 1 A\n" +
		"- [weak] \"One trace ref…\" [^15]: reason\n" +
		"- [cut lost content] \"Two trace refs…\" [^61][^62]: the quote cuts \"which\"\n" +
		"- [fail] \"A footnote number.\" (fn 40): not in the section\n" +
		"- [weak] \"First…\" [^15] and “Second…” [^16]: the pair works\n" +
		"- [dangling] \"One…\" / \"Other…\": each a flag\n" +
		"- [reworded] The same statement: the source reads \"given\"\n" +
		"- [dangling] \"Its quote: has a colon\": \"this\": no antecedent\n" +
		"- [weak] \"No colon\" anywhere\n"
	want := checker.Report{
		Flags: []checker.Flag{
			{H1: "1 A", Grades: []checker.Grade{checker.Weak}, Statement: "One trace ref…", Reason: "reason"},
			{H1: "1 A", Grades: []checker.Grade{checker.CutLostContent}, Statement: "Two trace refs…", Reason: `the quote cuts "which"`},
			{H1: "1 A", Grades: []checker.Grade{checker.Fail}, Statement: "A footnote number.", Reason: "not in the section"},
			{H1: "1 A", Grades: []checker.Grade{checker.Weak}, Statement: "First…", Reason: "the pair works"},
			{H1: "1 A", Grades: []checker.Grade{checker.Weak}, Statement: "Second…", Reason: "the pair works"},
			{H1: "1 A", Grades: []checker.Grade{checker.Dangling}, Statement: "One…", Reason: "each a flag"},
			{H1: "1 A", Grades: []checker.Grade{checker.Dangling}, Statement: "Other…", Reason: "each a flag"},
			{H1: "1 A", Grades: []checker.Grade{checker.Reworded}, Statement: "", Reason: `The same statement: the source reads "given"`},
			{H1: "1 A", Grades: []checker.Grade{checker.Dangling}, Statement: "Its quote: has a colon", Reason: `"this": no antecedent`},
			{H1: "1 A", Grades: []checker.Grade{checker.Weak}, Statement: "No colon", Reason: `"No colon" anywhere`},
		},
		Other: []checker.Line{},
	}
	if got := checker.Parse(report, supportcheck.Grades); !reflect.DeepEqual(got, want) {
		t.Errorf("Parse:\n got %+v\nwant %+v", got, want)
	}
}

// TestParse_RecordedReplies parses every reply of both checks that the
// Pragmatism run recorded, step 1.4's re-checks included, and checks that no
// line with a bracket naming one of the check's grades is left out of the
// flags.
func TestParse_RecordedReplies(t *testing.T) {
	support := regexp.MustCompile(`\b(weak|fail|dangling|outside-frame|cut lost content|reworded|wrong referent|not a name span|cut would do|reads wrong)\b`)
	readsAlone := regexp.MustCompile(`\b(dangling|outside-frame)\b`)
	trial := testfixture.PragmatismRun(t)
	n := 0
	for _, c := range []struct {
		dir     string
		replies int
		grades  []checker.Grade
		grade   *regexp.Regexp
	}{
		{"1.3a-support-check", 8, supportcheck.Grades, support},
		{"1.3b-reads-alone-check", 8, readsalone.Grades, readsAlone},
		{"1.4-recheck-1.3a-support-check", 4, supportcheck.Grades, support},
		{"1.4-recheck-1.3b-reads-alone-check", 4, readsalone.Grades, readsAlone},
		{"1.4-kept-loss-repair/recheck-1.3a-support-check", 1, supportcheck.Grades, support},
	} {
		replies := testfixture.Files(t, filepath.Join(trial, filepath.FromSlash(c.dir), "raw"))
		if len(replies) != c.replies {
			t.Errorf("%s: %d replies, want %d", c.dir, len(replies), c.replies)
		}
		bracket := regexp.MustCompile(`^[-*]\s+\[([^\]]+)\]`)
		for name, reply := range replies {
			r := checker.Parse(reply, c.grades)
			n += len(r.Flags)
			for _, l := range r.Other {
				if m := bracket.FindStringSubmatch(l.Text); m != nil && c.grade.MatchString(m[1]) {
					t.Errorf("%s/%s: graded line is not a flag: %.80q", c.dir, name, l.Text)
				}
			}
		}
	}
	if n == 0 {
		t.Error("no flags in any recorded reply, so the test shows nothing")
	}
}

func TestSegmentNotes(t *testing.T) {
	notes := []*note.Note{
		note.Parse("0 A.md", "# 0 A\n"),
		note.Parse("1 B - Aside - C.md", "# 1 B - Aside - C\n"),
		note.Parse("1 B.md", "# 1 B\n"),
		note.Parse("10 D.md", "# 10 D\n"),
	}
	mine, others := checker.SegmentNotes(notes, source.Segment{Number: 1})
	if want := notes[1:3]; !reflect.DeepEqual(mine, want) {
		t.Errorf("mine = %v, want %v", names(mine), names(want))
	}
	if want := []*note.Note{notes[0], notes[3]}; !reflect.DeepEqual(others, want) {
		t.Errorf("others = %v, want %v", names(others), names(want))
	}
}

func names(notes []*note.Note) []string {
	var out []string
	for _, n := range notes {
		out = append(out, n.Name)
	}
	return out
}

// A flag's note file is its segment's note its H1 names, or "" if it names
// none.
func TestNoteFile(t *testing.T) {
	notes := []*note.Note{note.Parse("1 B - Aside - C.md", "# 1 B - Aside - C\n"), note.Parse("1 B.md", "# 1 B\n")}
	for h1, want := range map[string]string{
		"1 B":             "1 B.md",
		"1 B - Aside - C": "1 B - Aside - C.md",
		"1 B.md":          "",
		"1 Renamed":       "",
		"":                "",
	} {
		if got := checker.NoteFile(h1, notes); got != want {
			t.Errorf("NoteFile(%q) = %q, want %q", h1, got, want)
		}
	}
}
