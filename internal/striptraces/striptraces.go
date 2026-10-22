// Package striptraces is step 2.2, strip traces: once step 2.1's gates pass,
// it writes a copy of every segment's split notes with every footnote
// reference and trace gone, and gives each atomic note one source footnote
// instead (ADR 0003). It is the end of the
// traces: no later step reads one.
package striptraces

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/corygyarmathy/slipbox/internal/gate"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
)

// SourceLabel is the source footnote's label.
const SourceLabel = "1"

// GateName names the strip check when a segment falls back for failing it.
const GateName = "the strip check"

var _ gate.Report = (*Report)(nil)

// Report is the strip check's result: what in each segment folder's notes
// Strip can't strip, in the folders' order, then their notes' order.
type Report struct {
	Problems []Problem
}

// Problem is one thing in a note that Strip can't strip.
type Problem struct {
	// Folder is the segment folder's name, `<n> <title>`, and Note the
	// note's file name.
	Folder, Note string
	// Line is the line number, from 1, of a definition that is neither a
	// trace nor the source footnote, and Label and Definition are its label
	// and line. Line is 0 for an atomic note without an H1.
	Line              int
	Label, Definition string
}

// Text says what the problem is, naming the note.
func (p Problem) Text() string {
	if p.Line == 0 {
		return fmt.Sprintf("%s has no H1 to put the source footnote after", p.Note)
	}
	return fmt.Sprintf("%s, line %d: footnote [^%s] is neither a trace nor the source footnote: %q", p.Note, p.Line, p.Label, p.Definition)
}

// Failed says whether any note can't be stripped.
func (r *Report) Failed() bool {
	return len(r.Problems) > 0
}

// FailingFolders names the folders with a note that can't be stripped, in
// the report's order, each once.
func (r *Report) FailingFolders() []string {
	var out []string
	for _, p := range r.Problems {
		if len(out) == 0 || out[len(out)-1] != p.Folder {
			out = append(out, p.Folder)
		}
	}
	return out
}

// String renders the report: a line per problem, or one saying there are
// none.
func (r *Report) String() string {
	if !r.Failed() {
		return "Every note can be stripped.\n"
	}
	var b strings.Builder
	for _, p := range r.Problems {
		fmt.Fprintf(&b, "%s: %s\n", p.Folder, p.Text())
	}
	return b.String()
}

// Check is the strip check, step 2.1's third gate: it runs Strip's rules on
// every note in folders, without stripping, so a split Strip would refuse
// falls back before step 2.2 is reached. The split
// check reads only the definitions a reference names, and the trace diff
// only the quotes, so neither catches a definition that is neither a trace
// nor the source footnote.
func Check(folders []split.SegmentFolder) *Report {
	r := &Report{}
	for _, f := range folders {
		for _, n := range f.Notes {
			for _, p := range problems(n, f.IsHub(n)) {
				p.Folder = f.Name
				r.Problems = append(r.Problems, p)
			}
		}
	}
	return r
}

// problems says what in n Strip can't strip: an atomic note without an H1,
// and a definition after the frontmatter that is neither a trace nor a
// source footnote.
// Each Problem's Folder is left "".
func problems(n *note.Note, hub bool) []Problem {
	var out []Problem
	h1 := false
	for i, l := range n.Lines {
		switch l.Kind {
		case note.H1:
			h1 = true
		case note.Definition:
			_, trace := l.Def.Trace()
			_, sourceFootnote := l.Def.SourceFootnote()
			if !trace && !sourceFootnote {
				out = append(out, Problem{Note: n.Name, Line: i + 1, Label: l.Def.Label, Definition: l.Text})
			}
		}
	}
	if !hub && !h1 {
		out = append([]Problem{{Note: n.Name}}, out...)
	}
	return out
}

// The kinds of the strip check's failing items.
const (
	NoH1          = "atomic note without an H1"
	OtherFootnote = "footnote that is neither a trace nor the source footnote"
)

// Items gives the failing items, for a stop's summary, in the report's
// order: each note that can't be stripped, at the path notePath gives it,
// and why.
func (r *Report) Items(notePath func(Problem) string) []gate.Item {
	var out []gate.Item
	for _, p := range r.Problems {
		path := notePath(p)
		if p.Line == 0 {
			out = append(out, gate.Item{
				Kind:    NoH1,
				Problem: "This atomic note has no H1 for step 2.2 to put the source footnote after.",
				Paths:   []string{path},
			})
			continue
		}
		out = append(out, gate.Item{
			Kind:    OtherFootnote,
			Problem: fmt.Sprintf("Footnote [^%s], on line %d, is neither a trace nor the source footnote, so step 2.2 can't strip it.", p.Label, p.Line),
			Paths:   []string{path},
			Text:    p.Definition,
		})
	}
	return out
}

// Strip gives n with every footnote reference `[^<n>]` dropped from its H1
// and body, and every footnote definition after its frontmatter dropped. A
// name span's wording stays in its statement, as only the reference goes.
// The frontmatter is left as written. Unless n is a hub, it gains the source
// footnote, citekeyLink: `[^1]` at the end of its H1, and `[^1]:
// <citekeyLink>` at the end of the note, after a blank line, with the note's
// own line ending. A note the strip check fails is an error.
func Strip(n *note.Note, citekeyLink string, hub bool) (*note.Note, error) {
	if p := problems(n, hub); len(p) > 0 {
		return nil, errors.New(p[0].Text())
	}
	eol := "\n"
	for _, l := range n.Lines {
		if l.EOL != "" {
			eol = l.EOL
			break
		}
	}

	out := &note.Note{Name: n.Name}
	add := func(text, eol string) { out.Lines = append(out.Lines, note.Line{Text: text, EOL: eol}) }
	for _, l := range n.Lines {
		switch l.Kind {
		case note.Frontmatter:
			add(l.Text, l.EOL)
		case note.Definition:
		case note.H1:
			text := note.DropRefs(l.Text)
			if !hub {
				text += "[^" + SourceLabel + "]"
			}
			add(text, l.EOL)
		default:
			add(note.DropRefs(l.Text), l.EOL)
		}
	}
	if !hub {
		if last := len(out.Lines) - 1; out.Lines[last].EOL == "" {
			out.Lines[last].EOL = eol
		}
		if out.Lines[len(out.Lines)-1].Text != "" {
			add("", eol)
		}
		add("[^"+SourceLabel+"]: "+citekeyLink, eol)
	}
	return note.Parse(n.Name, out.Render()), nil
}

// Run runs step 2.2 on the notes step 2.1's gates in folder passed on, each
// segment's split or its fallback, writing each segment folder's notes,
// stripped, to the same place under the step's `stage-2/`. It runs only once
// step 2.1's gates are complete. The source footnote is src's `source:` value
// without its YAML quotes.
func Run(src *source.Source, folder *run.Folder) error {
	if !folder.Complete(run.SplitGatesDir) {
		return errors.New("strip traces: step 2.1's gates are not complete, so the traces can't be stripped yet")
	}
	folders, err := split.ReadFolders(filepath.Join(folder.Dir, filepath.FromSlash(run.SplitOrFallbackNotes)))
	if err != nil {
		return fmt.Errorf("strip traces: %w", err)
	}
	step, err := folder.Begin(run.StripDir)
	if err != nil {
		return err
	}
	for _, f := range folders {
		for _, n := range f.Notes {
			stripped, err := Strip(n, src.CitekeyLink, f.IsHub(n))
			if err != nil {
				return fmt.Errorf("strip traces: %s: %w", f.Name, err)
			}
			if err := step.WriteFile(path.Join(run.Stage2NotesDir, f.Name, n.Name), []byte(stripped.Render())); err != nil {
				return err
			}
		}
	}
	return step.Commit()
}
