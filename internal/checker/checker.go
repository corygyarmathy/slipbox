// Package checker is what step 1.3's two calls share: the support and cuts
// check (1.3a) and the reads-alone check (1.3b) (ADR 0007). Each replies per segment with a report of one line
// per non-pass statement. The run keeps each report and parses it into flags
// of one shape. Any bulleted line graded with the check's own grades is a
// flag, whether or not it keeps the brief's shape (Parse), so the repair pass
// and the run report read both checks' flags with one type. Which check's
// flags count for which grade is theirs to decide: step 1.3b's govern reads
// alone.
package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/corygyarmathy/slipbox/internal/modelstep"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
)

// Grade is a non-pass grade from a checker brief's report line.
type Grade string

// The grades a flag can have, as the briefs spell them. Each check accepts
// only its own brief's grades.
const (
	Weak           Grade = "weak"
	Fail           Grade = "fail"
	Dangling       Grade = "dangling"
	OutsideFrame   Grade = "outside-frame"
	CutLostContent Grade = "cut lost content"
	Reworded       Grade = "reworded"
	WrongReferent  Grade = "wrong referent"
	NotANameSpan   Grade = "not a name span"
	CutWouldDo     Grade = "cut would do"
	ReadsWrong     Grade = "reads wrong"
)

// Flag is one non-pass statement from a checker report.
type Flag struct {
	// H1 is the title of the note the flag sits under, without its `#`s, or
	// "" if the flag came before any heading.
	H1 string `json:"h1"`
	// File is the file name of the segment's note H1 names, or "" if it names
	// none. Parse leaves it empty and Run fills it in (NoteFile).
	File string `json:"file"`
	// Grades is the bracket's grades, in order: usually one, but a checker
	// sometimes gives a statement two, as `[wrong referent / reads wrong]`.
	Grades []Grade `json:"grades"`
	// Statement is the statement's start as the checker quoted it, without
	// its quotes.
	Statement string `json:"statement"`
	Reason    string `json:"reason"`
}

// Line is a report line that isn't a flag, a heading or blank: the counts,
// the cross-section footnote grades, the generic "we"/"you" list and anything
// else the checker wrote.
type Line struct {
	H1   string `json:"h1"`
	Text string `json:"text"`
}

// Report is a checker report parsed.
type Report struct {
	Flags []Flag `json:"flags"`
	// Other holds every other non-blank line, in order, so nothing the
	// checker wrote is dropped.
	Other []Line `json:"other"`
}

// flagLine is a bullet, `-` or `*`, then a bracket. The briefs' shape is
// `- [<grade>] "<statement start…>": <reason>`, but Sonnet also puts trace
// refs (`[^n]`) or `(fn N)` before the colon, quotes two statements in one
// line, or quotes none; flagHead reads what follows the bracket.
var flagLine = regexp.MustCompile(`^[-*]\s+\[([^\]]+)\]\s*(.*)$`)

// heading is a Markdown heading of any level. The checkers are asked for each
// note's H1, but Sonnet sometimes writes it as `##`.
var heading = regexp.MustCompile(`^#+ (.*)$`)

// Parse parses a checker report, accepting the grades in grades. A line,
// with its surrounding whitespace trimmed, is:
//
//   - a heading if it starts with one or more `#` and a space; it names the
//     note for the lines after it
//   - one or more flags if it starts with a bullet and a bracket that holds
//     one or more of grades, split on `/` and spelled as the brief spells
//     them; flagHead says how many and what each holds
//   - blank, and skipped
//   - otherwise kept in Other under the current heading
//
// The parser reads shape, not intent: a flag line whose reason then says the
// statement passes is still a flag, and a statement flagged twice gives two
// flags.
func Parse(report string, grades []Grade) Report {
	r := Report{Flags: []Flag{}, Other: []Line{}}
	h1 := ""
	for _, line := range strings.Split(report, "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		if m := heading.FindStringSubmatch(s); m != nil {
			h1 = strings.TrimSpace(m[1])
			continue
		}
		if m := flagLine.FindStringSubmatch(s); m != nil {
			if gs, ok := parseGrades(m[1], grades); ok {
				statements, reason := flagHead(m[2])
				for _, st := range statements {
					r.Flags = append(r.Flags, Flag{H1: h1, Grades: gs, Statement: st, Reason: reason})
				}
				continue
			}
		}
		r.Other = append(r.Other, Line{H1: h1, Text: s})
	}
	return r
}

// flagHead splits what follows a flag line's bracket into its statements and
// its reason. The head runs up to the first `:` outside quotes, and each
// quoted span in it, straight or curly, is one statement, without its quotes;
// whatever else the head holds, such as trace refs or "and", is in none. The
// reason is what follows that colon, so the reads-alone brief's referring
// words stay at its start. A line with no such colon gives the spans it
// quotes, and all of rest as their reason. A line with no quoted span before
// the colon, or none at all, gives one statement, "", and all of rest as its
// reason.
func flagHead(rest string) (statements []string, reason string) {
	var closing rune
	start := 0
	for i, c := range rest {
		switch {
		case closing != 0:
			if c == closing {
				statements = append(statements, rest[start:i])
				closing = 0
			}
		case c == '"':
			closing, start = '"', i+1
		case c == '“':
			closing, start = '”', i+len("“")
		case c == ':':
			if len(statements) > 0 {
				return statements, strings.TrimSpace(rest[i+1:])
			}
			return []string{""}, rest
		}
	}
	if len(statements) > 0 {
		return statements, rest
	}
	return []string{""}, rest
}

// parseGrades reads a flag line's bracket as one or more grades joined by
// `/`. It reports false if any part is not one of grades.
func parseGrades(bracket string, grades []Grade) ([]Grade, bool) {
	var gs []Grade
	for _, part := range strings.Split(bracket, "/") {
		g := Grade(strings.TrimSpace(part))
		if !slices.Contains(grades, g) {
			return nil, false
		}
		gs = append(gs, g)
	}
	return gs, true
}

// SegmentPrefix is how the name of each of seg's notes starts: seg's number
// and a space. Every step finds a segment's notes by it, so a note named
// without it is filed under no segment, or the wrong one.
func SegmentPrefix(seg source.Segment) string {
	return strconv.Itoa(seg.Number) + " "
}

// MainNote is the file name of seg's stage 1 main note, `<n> <title>.md`.
func MainNote(seg source.Segment) string {
	return SegmentPrefix(seg) + seg.Title + ".md"
}

// SegmentNotes splits notes, every stage 1 note in name order, into seg's
// own and the other segments'.
func SegmentNotes(notes []*note.Note, seg source.Segment) (mine, others []*note.Note) {
	prefix := SegmentPrefix(seg)
	for _, n := range notes {
		if strings.HasPrefix(n.Name, prefix) {
			mine = append(mine, n)
		} else {
			others = append(others, n)
		}
	}
	return mine, others
}

// NotesBlock writes notes as a prompt block, as trialled: a `=== <label> ===`
// line, each note under a `--- NOTE: <name> ---` line, then
// `=== END <label> ===` and a blank line.
func NotesBlock(b *strings.Builder, notes []*note.Note, label string) {
	fmt.Fprintf(b, "=== %s ===\n", label)
	for _, n := range notes {
		fmt.Fprintf(b, "--- NOTE: %s ---\n%s\n", n.Name, n.Render())
	}
	fmt.Fprintf(b, "=== END %s ===\n\n", label)
}

// NoteFile is the file name of the note in notes whose name, without .md, is
// h1, or "" if there is none. notes are the flag's segment's own.
func NoteFile(h1 string, notes []*note.Note) string {
	for _, n := range notes {
		if strings.TrimSuffix(n.Name, ".md") == h1 {
			return n.Name
		}
	}
	return ""
}

// Check is what a checker step supplies to Run.
type Check struct {
	// Dir is the step's directory name in the run folder.
	Dir   string
	Brief string
	// BriefFile is Brief's file in internal/briefs/, for the usage log.
	BriefFile string
	Model     string
	Effort    string
	// FirstCallRate is the check's modelstep.Spec.FirstCallRate.
	FirstCallRate float64
	Grades        []Grade
	// Prompt gives seg's user prompt from notes, every stage 1 note in name
	// order.
	Prompt func(notes []*note.Note, seg source.Segment) string
	// Shared gives what every call shares from notes, for the system prompt
	// after the frame (modelstep.Spec.Shared), or is nil for nothing.
	Shared func(notes []*note.Note) string
	// Notes is the notes' directory in the run folder, slash-separated:
	// run.ExtractNotes, or run.RepairNotes for step 1.4's re-check.
	Notes string
	// Segments are the numbers of the segments to check, or nil for every
	// segment.
	Segments []int
}

// Run runs check on src's segments, reading the notes in check.Notes in
// folder. It writes each segment's report as `reports/<n>.md` and its parsed
// flags, each with its note file, as `flags/<n>.json`. A report with flags is
// not an error: they feed the repair pass and the run report.
func Run(ctx context.Context, p provider.Provider, src *source.Source, folder *run.Folder, check Check) error {
	if check.Notes == "" {
		return fmt.Errorf("%s: no notes directory", check.Dir)
	}
	notes, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(check.Notes)))
	if err != nil {
		return fmt.Errorf("%s: %w", check.Dir, err)
	}
	step, err := folder.Begin(check.Dir)
	if err != nil {
		return err
	}
	prompt := func(_ *source.Source, seg source.Segment) (string, bool) {
		if check.Segments != nil && !slices.Contains(check.Segments, seg.Number) {
			return "", false
		}
		return check.Prompt(notes, seg), true
	}
	spec := modelstep.Spec{Brief: check.Brief, BriefFile: check.BriefFile, Model: check.Model, Effort: check.Effort, FirstCallRate: check.FirstCallRate, Prompt: prompt}
	if check.Shared != nil {
		spec.Shared = check.Shared(notes)
	}
	results, err := modelstep.Run(ctx, p, src, spec, step)
	if err != nil {
		return fmt.Errorf("%s: %w", check.Dir, err)
	}
	for _, r := range results {
		n := strconv.Itoa(r.Segment.Number)
		if err := step.WriteFile(path.Join(run.ReportsDir, n+".md"), []byte(r.Text)); err != nil {
			return err
		}
		report := Parse(r.Text, check.Grades)
		mine, _ := SegmentNotes(notes, r.Segment)
		for i := range report.Flags {
			report.Flags[i].File = NoteFile(report.Flags[i].H1, mine)
		}
		flags, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if err := step.WriteFile(path.Join(run.FlagsDir, n+".json"), append(flags, '\n')); err != nil {
			return err
		}
	}
	return step.Commit()
}
