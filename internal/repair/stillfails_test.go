package repair_test

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/checker"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/repair"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/supportcheck"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// The Pragmatism run's support reply for segment 7 gives thirteen flags, then
// a "Correction to the above" and two more, on statements it flagged already.
// Six of its fifteen flags count, the rest being reads-alone grades, which
// step 1.3b governs. The correction's "But in this world…" is the second flag
// on that statement, and is in its entry, not one of its own.
func TestStillFails_OneEntryPerStatement(t *testing.T) {
	trial := testfixture.PragmatismRun(t)
	notes, err := note.ReadDir(filepath.Join(trial, run.ExtractDir, run.NotesDir))
	if err != nil {
		t.Fatal(err)
	}
	reply := testfixture.Files(t, filepath.Join(trial, run.SupportCheckDir, run.RawDir))["7.txt"]
	flags := withFiles(checker.Parse(reply, supportcheck.Grades).Flags, notes)
	if len(flags) != 15 {
		t.Fatalf("%d flags, want 15", len(flags))
	}
	entries := repair.StillFails(notes, &mechcheck.Report{}, flags, nil)
	var got []int
	for _, e := range entries {
		if e.Statement == "" || e.File != "7 The True Is the Good in the Way of Belief.md" {
			t.Errorf("flag %.60q resolves to no statement of its note", e.Quote)
		}
		got = append(got, len(e.Flags))
	}
	if want := []int{2, 1, 1, 1, 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("flags by entry %v, want %v", got, want)
	}
	if len(entries) > 0 && !strings.HasPrefix(entries[0].Statement, "But in this world,[^5] certain ideas") {
		t.Errorf("first entry %.60q, want the statement flagged twice", entries[0].Statement)
	}
}

// A statement flagged twice, quoted differently, once up to a "…" and once
// whole, is one entry with both flags.
func TestStillFails_AStatementQuotedTwoWays(t *testing.T) {
	notes := []*note.Note{note.Parse("1 Two.md", "# 1 Two\n\nGamma is third.[^1] Delta is fourth.[^2]\n\n"+
		"[^1]: [[Tiny#Two]] \"Gamma is third.\"\n[^2]: [[Tiny#Two]] \"Delta is fourth.\"\n")}
	flags := withFiles([]checker.Flag{
		{H1: "1 Two", Grades: []checker.Grade{checker.Dangling}, Statement: "Gamma is…", Reason: "of what"},
		{H1: "1 Two", Grades: []checker.Grade{checker.Dangling}, Statement: "Gamma is third.", Reason: "third of what"},
	}, notes)
	entries := repair.StillFails(notes, &mechcheck.Report{}, nil, flags)
	if len(entries) != 1 || entries[0].Statement != "Gamma is third.[^1]" || len(entries[0].Flags) != 2 {
		t.Errorf("entries %+v, want one, Gamma's, with both flags", entries)
	}
}

// A quote inside a statement resolves to it, unless another statement holds
// it too: then the flag is listed by its quote.
func TestStillFails_AFragmentResolvesOnlyWhenOneStatementHoldsIt(t *testing.T) {
	notes := []*note.Note{note.Parse("1 Two.md", "# 1 Two\n\nGamma is third.[^1] Delta is fourth.[^2]\n\n"+
		"[^1]: [[Tiny#Two]] \"Gamma is third.\"\n[^2]: [[Tiny#Two]] \"Delta is fourth.\"\n")}
	flags := withFiles([]checker.Flag{
		{H1: "1 Two", Grades: []checker.Grade{checker.Dangling}, Statement: "third", Reason: "third of what"},
		{H1: "1 Two", Grades: []checker.Grade{checker.Dangling}, Statement: "is", Reason: "is what"},
	}, notes)
	entries := repair.StillFails(notes, &mechcheck.Report{}, nil, flags)
	if len(entries) != 2 {
		t.Fatalf("%d entries, want 2: %+v", len(entries), entries)
	}
	if e := entries[0]; e.Statement != "Gamma is third.[^1]" {
		t.Errorf("first entry %+v, want it resolved to Gamma's statement", e)
	}
	if e := entries[1]; e.Statement != "" || e.Quote != "is" {
		t.Errorf("second entry %+v, want it listed by its quote", e)
	}
}

// withFiles sets each flag's note file as checker.Run does.
func withFiles(flags []checker.Flag, notes []*note.Note) []checker.Flag {
	for i := range flags {
		flags[i].File = checker.NoteFile(flags[i].H1, notes)
	}
	return flags
}
