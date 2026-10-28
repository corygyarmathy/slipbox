package repair_test

import (
	"reflect"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/checker"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/repair"
	"github.com/corygyarmathy/slipbox/internal/supportcheck"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// A support report that flags statements, then gives a correction that
// flags two of them again, quoted differently. Of its seven flags, five
// count, the rest having the reads-alone grades step 1.3b governs. Each
// statement is one entry, its flags in the order given, so a correction's
// flag joins its statement's entry rather than making one of its own.
func TestStillFails_OneEntryPerStatement(t *testing.T) {
	notes := testfixture.LampStage1Notes(t)
	reply := "# 2 The Lens\n\n" +
		"- [dangling] \"The lens bends light…\": \"The lens\": named in the last chapter.\n" +
		"- [weak] \"A beam carries farther than a glow.\": the section says farther, not how far.\n" +
		"- [outside-frame] \"Obviously, a keeper can take no notice…\": \"Obviously\": as the last chapter showed.\n" +
		"- [cut lost content] \"On a foggy night it is no help at all.\": the cut drops that the lens is no help.\n" +
		"- [reworded] \"It uses rings of prisms…\": the source names the Fresnel lens.\n\n" +
		"Correction to the above, since two entries were reasoning notes rather than faults:\n\n" +
		"- [weak] \"A beam carries farther…\": the section compares, it does not measure.\n" +
		"- [cut lost content] \"On a foggy night…\": the cut drops what \"it\" is.\n\n" +
		"pass 2 / weak 1 / cut lost content 1 / reworded 1\n"
	flags := withFiles(checker.Parse(reply, supportcheck.Grades).Flags, notes)
	if len(flags) != 7 {
		t.Fatalf("%d flags, want 7", len(flags))
	}
	got := repair.StillFails(notes, &mechcheck.Report{}, flags, nil)
	support := func(grade, quote, reason string) repair.Flag {
		return repair.Flag{Step: repair.StepSupport, Grades: []string{grade}, Quote: quote, Reason: reason}
	}
	want := []repair.Entry{
		{
			File: lens, Statement: "A beam carries farther than a glow.[^2]",
			Traces: []string{"[^2]: " + lensTrace("A beam carries farther than a glow.")}, Grades: []string{"weak"},
			Flags: []repair.Flag{
				support("weak", "A beam carries farther than a glow.", "the section says farther, not how far."),
				support("weak", "A beam carries farther…", "the section compares, it does not measure."),
			},
		},
		{
			File: lens, Statement: "On a foggy night it is no help at all.[^4]",
			Traces: []string{"[^4]: " + lensTrace("On a foggy night it is no help at all.")}, Grades: []string{"cut lost content"},
			Flags: []repair.Flag{
				support("cut lost content", "On a foggy night it is no help at all.", "the cut drops that the lens is no help."),
				support("cut lost content", "On a foggy night…", `the cut drops what "it" is.`),
			},
		},
		{
			File: lens, Statement: "It uses rings of prisms instead of one thick glass.[^5]",
			Traces: []string{"[^5]: " + lensTrace("uses rings of prisms instead of one thick glass")}, Grades: []string{"reworded"},
			Flags: []repair.Flag{support("reworded", "It uses rings of prisms…", "the source names the Fresnel lens.")},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("still fails:\n got %+v\nwant %+v", got, want)
	}
}

// A quote resolves to the statement it starts, run on into the statements
// after it on its line. A quote inside a statement resolves to it, unless
// another statement holds it too: then the flag is listed by its quote.
func TestStillFails_ResolvingAQuote(t *testing.T) {
	// runNotes's segment 3 note has two statements on a line.
	notes := testfixture.LampStage1Notes(t)
	for i, n := range notes {
		if n.Name == storms {
			notes[i] = note.Parse(storms, runNotes[storms])
		}
	}
	dangling := func(h1, quote string) checker.Flag {
		return checker.Flag{H1: h1, Grades: []checker.Grade{checker.Dangling}, Statement: quote, Reason: "r"}
	}
	flags := withFiles([]checker.Flag{
		dangling("3 Storms", "In a storm the light matters most. A wreck was his…"),
		dangling("2 The Lens", "rings of prisms"),
		dangling("2 The Lens", "night"),
	}, notes)
	var got []string
	for _, e := range repair.StillFails(notes, &mechcheck.Report{}, nil, flags) {
		got = append(got, e.File+": "+e.Statement+e.Quote)
	}
	want := []string{
		lens + ": It uses rings of prisms instead of one thick glass.[^5]",
		lens + ": night",
		storms + ": In a storm the light matters most.[^1]",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("entries by file and statement, or quote:\n got %q\nwant %q", got, want)
	}
}

// withFiles sets each flag's note file as checker.Run does.
func withFiles(flags []checker.Flag, notes []*note.Note) []checker.Flag {
	for i := range flags {
		flags[i].File = checker.NoteFile(flags[i].H1, notes)
	}
	return flags
}
