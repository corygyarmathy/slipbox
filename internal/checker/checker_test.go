package checker_test

import (
	"reflect"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/checker"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/readsalone"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/supportcheck"
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
		"- [pass-with-note] \"A pass, hyphenated\": kept\n" +
		"- \"we\": generic, no bracket\n" +
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
			{H1: "1 A", Text: `- [pass-with-note] "A pass, hyphenated": kept`},
			{H1: "1 A", Text: `- "we": generic, no bracket`},
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
// A span ends only at its own closing quote, so a statement keeps the single
// quotes and apostrophes it holds. A line whose reason says the statement
// passes is still a flag: the parser reads shape, not intent.
func TestParse_GradedShapes(t *testing.T) {
	report := "# 1 A\n" +
		"- [weak] \"One trace ref…\" [^15]: reason\n" +
		"- [cut lost content] \"Two trace refs…\" [^61][^62]: the quote cuts \"which\"\n" +
		"- [fail] \"A footnote number.\" (fn 40): not in the section\n" +
		"- [weak] \"First…\" [^15] and “Second…” [^16]: the pair works\n" +
		"- [dangling] \"One…\" / \"Other…\": each a flag\n" +
		"- [weak] \"Wick…\", \"Oil…\", \"Glass…\", \"Log…\": a list of four\n" +
		"- [cut lost content] \"The keeper's 'great' lamp…\": the cut \"of the harbour\" named it\n" +
		"- [dangling] \"He lit it.\": \"He\" is the keeper. Pass, not dangling.\n" +
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
			{H1: "1 A", Grades: []checker.Grade{checker.Weak}, Statement: "Wick…", Reason: "a list of four"},
			{H1: "1 A", Grades: []checker.Grade{checker.Weak}, Statement: "Oil…", Reason: "a list of four"},
			{H1: "1 A", Grades: []checker.Grade{checker.Weak}, Statement: "Glass…", Reason: "a list of four"},
			{H1: "1 A", Grades: []checker.Grade{checker.Weak}, Statement: "Log…", Reason: "a list of four"},
			{H1: "1 A", Grades: []checker.Grade{checker.CutLostContent}, Statement: "The keeper's 'great' lamp…", Reason: `the cut "of the harbour" named it`},
			{H1: "1 A", Grades: []checker.Grade{checker.Dangling}, Statement: "He lit it.", Reason: `"He" is the keeper. Pass, not dangling.`},
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

// A report need not end in a newline: a checker's reply often doesn't, and
// its last line is read like any other.
func TestParse_NoFinalNewline(t *testing.T) {
	report := "# 1 A\n\n- [weak] \"Last…\": the reply ends here"
	want := checker.Report{
		Flags: []checker.Flag{{H1: "1 A", Grades: []checker.Grade{checker.Weak}, Statement: "Last…", Reason: "the reply ends here"}},
		Other: []checker.Line{},
	}
	if got := checker.Parse(report, supportcheck.Grades); !reflect.DeepEqual(got, want) {
		t.Errorf("Parse:\n got %+v\nwant %+v", got, want)
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
