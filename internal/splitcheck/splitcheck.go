// Package splitcheck is step 2.1's first gate, the split check: did every
// stage 1 statement survive the split with its traces? Per segment folder, it
// pairs the segment's stage 1 statements with its atomic notes' statements by
// the traces they carry, and reports lost and extra traces, untraced
// statements, edited statement text and footnotes in the hub.
//
// A statement is a body line (ADR 0002, Consequences),
// and a trace is a footnote definition's whole value, `[[<file>#<section>]]
// "<quote>"`, or `?` for a reference with no definition. The traces are
// compared as one multiset per segment, so a line split into several, or
// lines merged, loses no trace.
//
// An extra trace that is a name span doesn't fail the gate (ADR 0010). A name
// span's quote is exact in the section it names, as step 1.2's trace match
// grades it (mechcheck.Locator.Exact), and it sits on an anchored statement,
// one that also carries a stage 1 trace of the segment that isn't extra. A
// trace no stage 1 statement of the segment carries needs no more. A trace one
// does carry is a name span only on an anchored statement whose text was
// edited, one no stage 1 statement of the segment has, and each of its copies
// on any other statement must sit on a statement whose text a stage 1
// statement carrying it has, copy for copy. Such a trace is taken once for
// each copy it is extra, in the statements' order. A statement copied into two
// notes, even reworded, a stage 1 trace copied onto another unedited
// statement, and a statement of added traces alone all still fail the gate.
// The manifest isn't read. The report lists the name spans it took after the
// other lines of their segment.
//
// The report is plain text (ADR 0002 §3): an edited statement is its text,
// quoted as Go quotes a string, and the note it is in.
//
// The frontmatter is read as the note model reads it: it closes at the first
// `---` line after the opening one, so an empty frontmatter, `---` then
// `---`, closes at the second line, and a closing `---` may end the file with
// no newline after it.
package splitcheck

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/corygyarmathy/slipbox/internal/counter"
	"github.com/corygyarmathy/slipbox/internal/gate"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/textrule"
)

// GateName names the gate when a segment falls back for failing it.
const GateName = "the split check"

var _ gate.Report = (*Report)(nil)

// Report is the check's result: one SegmentReport per segment folder, in the
// folders' order.
type Report struct {
	Segments []SegmentReport
}

// SegmentReport is one segment folder's result.
type SegmentReport struct {
	// Folder is the segment folder's name, `<n> <title>`.
	Folder string
	// AtomicNotes counts the folder's notes other than its hub.
	AtomicNotes int
	// StatementsBefore and StatementsAfter count the statements of the
	// segment's stage 1 notes and of its atomic notes.
	StatementsBefore, StatementsAfter int
	// TracesBefore and TracesAfter count their traces.
	TracesBefore, TracesAfter int
	// LostTraces are the traces stage 1 has more of than the atomic notes, a
	// trace once for each it lost, and ExtraTraces the reverse, name spans
	// included, in the order the notes first give them.
	LostTraces, ExtraTraces []string
	// NameSpans are the extra traces taken as name spans, each once for
	// each it is extra, in ExtraTraces' order.
	NameSpans []NameSpan
	// Untraced are the atomic notes' statements without a trace.
	Untraced []Statement
	// Edited are the atomic notes' traced statements whose text no stage 1
	// statement of the segment has. They are the name-span edits, and
	// allowed.
	Edited []Statement
	// HubFootnotes counts the footnote references in the hub, `<title>.md`,
	// each definition's label included.
	HubFootnotes int
}

// NameSpan is an extra trace taken as a name span, and the statement it sits
// on.
type NameSpan struct {
	Trace string
	Statement
}

// Statement is a statement as the split check reads it, and the note it is
// in.
type Statement struct {
	// Text is the body line with its footnote references and any list
	// marker dropped, and its whitespace collapsed.
	Text string
	// Note is the note's file name.
	Note string
}

// Failed says whether the segment fails the gate: a lost trace, an extra
// trace that isn't a name span, an untraced statement or a footnote in the
// hub. Edited text and name spans alone don't.
func (s SegmentReport) Failed() bool {
	return len(s.LostTraces) > 0 || len(s.ExtraTraces) > len(s.NameSpans) || len(s.Untraced) > 0 || s.HubFootnotes > 0
}

// Failed says whether any segment failed the gate.
func (r *Report) Failed() bool {
	for _, s := range r.Segments {
		if s.Failed() {
			return true
		}
	}
	return false
}

// FailingFolders names the folders that fail the gate, in the report's
// order.
func (r *Report) FailingFolders() []string {
	var out []string
	for _, s := range r.Segments {
		if s.Failed() {
			out = append(out, s.Folder)
		}
	}
	return out
}

// String renders the report: a trace as the definition's value, and a
// statement as its quoted text and the note it is in. Each segment's name
// spans follow its other lines, a trace then the statement it sits on.
func (r *Report) String() string {
	var b strings.Builder
	for _, s := range r.Segments {
		fmt.Fprintf(&b, "\n## %s: %d atomic notes, statements %d->%d, traces %d->%d, hub footnotes %d\n",
			s.Folder, s.AtomicNotes, s.StatementsBefore, s.StatementsAfter, s.TracesBefore, s.TracesAfter, s.HubFootnotes)
		for _, t := range s.LostTraces {
			fmt.Fprintf(&b, "  lost_traces: %s\n", t)
		}
		for _, t := range s.ExtraTraces {
			fmt.Fprintf(&b, "  extra_traces: %s\n", t)
		}
		for _, st := range s.Untraced {
			fmt.Fprintf(&b, "  untraced_after: %s\n", st.rendered())
		}
		for _, st := range s.Edited {
			fmt.Fprintf(&b, "  edited_text: %s\n", st.rendered())
		}
		for _, ns := range s.NameSpans {
			fmt.Fprintf(&b, "  name_span: %s on %s\n", ns.Trace, ns.rendered())
		}
	}
	return b.String()
}

// rendered is the statement as its quoted text and the note it is in.
func (s Statement) rendered() string {
	return strconv.Quote(s.Text) + " in " + s.Note
}

// traced is a statement and its traces.
type traced struct {
	Statement
	traces []string
}

// listMarker is a list item's marker at the start of a line with its
// whitespace collapsed.
var listMarker = regexp.MustCompile(`^(?:[-*]|` + textrule.Digit + `+\.)[` + textrule.Space + `]+`)

// statements reads n's statements: each body line that isn't blank, a heading
// or `Aside.`, with the traces its footnote references name.
func statements(n *note.Note) []traced {
	defs := map[string]string{}
	for _, l := range n.Lines {
		if l.Def != nil {
			defs[l.Def.Label] = l.Def.Value
		}
	}
	var out []traced
	for _, l := range n.Lines {
		if l.Kind == note.Frontmatter || l.Kind == note.Definition {
			continue
		}
		line := strings.TrimSpace(l.Text)
		if line == "" || strings.HasPrefix(line, "#") || line == "Aside." {
			continue
		}
		text := listMarker.ReplaceAllString(note.CollapseSpace(note.StripRefs(line)), "")
		st := traced{Statement: Statement{Text: text, Note: n.Name}, traces: []string{}}
		for _, ref := range l.Refs {
			if v, ok := defs[ref]; ok {
				st.traces = append(st.traces, v)
			} else {
				st.traces = append(st.traces, "?")
			}
		}
		out = append(out, st)
	}
	return out
}

// Check checks the split in folders against the stage 1 notes it split,
// stage1, given in byte order of their names, with name spans looked for in
// src. Segment `<n>`'s stage 1 notes are those named `<n> *.md`, and its hub
// is the note in its folder named after the folder minus its number.
func Check(src *source.Source, stage1 []*note.Note, folders []split.SegmentFolder) *Report {
	loc := mechcheck.NewLocator(src)
	r := &Report{}
	for _, f := range folders {
		s := SegmentReport{Folder: f.Name}

		var before []traced
		for _, n := range f.Stage1(stage1) {
			before = append(before, statements(n)...)
		}
		var after []traced
		for _, n := range f.Notes {
			if f.IsHub(n) {
				s.HubFootnotes = n.FootnoteMarks()
				continue
			}
			s.AtomicNotes++
			after = append(after, statements(n)...)
		}

		var tracesBefore, tracesAfter counter.Counter
		textBefore := map[string]bool{}
		for _, st := range before {
			textBefore[st.Text] = true
			for _, t := range st.traces {
				tracesBefore.Add(t)
			}
		}
		for _, st := range after {
			for _, t := range st.traces {
				tracesAfter.Add(t)
			}
			switch {
			case len(st.traces) == 0:
				s.Untraced = append(s.Untraced, st.Statement)
			case !textBefore[st.Text]:
				s.Edited = append(s.Edited, st.Statement)
			}
		}
		s.StatementsBefore, s.StatementsAfter = len(before), len(after)
		s.TracesBefore, s.TracesAfter = tracesBefore.Total(), tracesAfter.Total()
		s.LostTraces = counter.Elements(tracesBefore.Minus(&tracesAfter))
		extra := tracesAfter.Minus(&tracesBefore)
		s.ExtraTraces = counter.Elements(extra)
		s.NameSpans = nameSpans(loc, extra, before, after, textBefore)
		r.Segments = append(r.Segments, s)
	}
	return r
}

// nameSpans gives the extra traces, extra, that are name spans, with the
// statements they sit on: each whose quote is exact in the section it names.
// One that no stage 1 statement of the segment, before, carries is taken at
// each of its places on a statement of after that is anchored. One that
// before carries is taken as stage1Spans gives it. A statement is anchored
// when it carries a stage 1 trace that isn't extra, so a statement whose
// stage 1 traces were all copied, a duplicate however reworded, is not.
// textBefore holds the texts of before.
func nameSpans(loc *mechcheck.Locator, extra []counter.Entry, before, after []traced, textBefore map[string]bool) []NameSpan {
	inBefore := map[string]bool{}
	for _, st := range before {
		for _, t := range st.traces {
			inBefore[t] = true
		}
	}
	anchors := maps.Clone(inBefore)
	for _, e := range extra {
		delete(anchors, e.Key)
	}
	out := []NameSpan{}
	for _, e := range extra {
		if tr, ok := (note.Def{Value: e.Key}).Trace(); !ok || !loc.Exact(tr) {
			continue
		}
		if inBefore[e.Key] {
			out = append(out, stage1Spans(e, before, after, anchors, textBefore)...)
			continue
		}
		for _, st := range after {
			if !anchored(st, anchors) {
				continue
			}
			for _, t := range st.traces {
				if t == e.Key {
					out = append(out, NameSpan{Trace: e.Key, Statement: st.Statement})
				}
			}
		}
	}
	return out
}

// stage1Spans gives the places where an extra trace that a stage 1 statement
// carries, e, is a name span: its copies on statements of after
// that are anchored by anchors and whose text no statement of before has,
// textBefore, the first e.Count of them. It gives none if a copy on any other
// statement of after sits on a text that no copy in before is left on,
// matched copy for copy, since that copy is the trace moved or a statement
// duplicated.
func stage1Spans(e counter.Entry, before, after []traced, anchors, textBefore map[string]bool) []NameSpan {
	left := map[string]int{}
	for _, st := range before {
		for _, t := range st.traces {
			if t == e.Key {
				left[st.Text]++
			}
		}
	}
	var out []NameSpan
	for _, st := range after {
		edited := !textBefore[st.Text] && anchored(st, anchors)
		for _, t := range st.traces {
			switch {
			case t != e.Key:
			case edited:
				out = append(out, NameSpan{Trace: e.Key, Statement: st.Statement})
			case left[st.Text] > 0:
				left[st.Text]--
			default:
				return nil
			}
		}
	}
	return out[:min(len(out), e.Count)]
}

// anchored says whether st carries one of anchors, the segment's stage 1
// traces that aren't extra.
func anchored(st traced, anchors map[string]bool) bool {
	return slices.ContainsFunc(st.traces, func(t string) bool { return anchors[t] })
}
