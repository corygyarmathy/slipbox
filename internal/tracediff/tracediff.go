// Package tracediff is step 2.1's second gate, the trace diff: per segment
// folder, it compares the segment's stage 1 notes with its atomic notes on
// two multisets, ignoring line layout. The quote multiset must be equal, so a
// lost or added quote fails the gate. The sentence multiset's differences are
// the name-span edits, and are reported.
//
// A quote is what note.QuotedTexts reads from a footnote definition, and
// a sentence is what note.BodySentences splits from a body. Unlike the
// split check's statements, sentences don't follow lines, and a body line
// such as `Aside.` is a sentence too.
//
// An added quote that is a name span's doesn't fail the gate (ADR 0010). It
// is a
// name span's when the atomic notes have at least as many footnotes with that
// quote as it is added, each with a trace whose quote is exact in the section
// it names, as step 1.2's trace match grades it (mechcheck.Locator.Exact).
// Where the footnotes sit is the split check's to test: it runs first, and
// has already failed a name span on a statement that carries no stage 1 trace
// that isn't extra, a duplicated statement and a stage 1 trace copied onto
// another unedited statement, so the trace diff doesn't test them again. The
// report lists the name spans it took last in their segment, after the lost
// and added quotes and the sentences, since a name span is also listed among
// the added quotes.
//
// The frontmatter is read as the note model reads it, as package splitcheck
// describes. A segment's notes are read in byte order of their names, so the
// report's lines come in the same order on every system.
package tracediff

import (
	"fmt"
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
const GateName = "the trace diff"

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
	// AtomicNotes counts the folder's notes less one, for its hub.
	AtomicNotes int
	// QuotesBefore and QuotesAfter count the quotes of the segment's stage 1
	// notes and of its atomic notes.
	QuotesBefore, QuotesAfter int
	// LostQuotes are the quotes stage 1 has more of than the atomic notes,
	// each with how many more, and AddedQuotes the reverse, name spans'
	// included, each in the order the notes first give them.
	LostQuotes, AddedQuotes []counter.Entry
	// NameSpans are the added quotes taken as name spans', in AddedQuotes'
	// order.
	NameSpans []counter.Entry
	// SentencesBefore are the sentences stage 1 has more of than the atomic
	// notes, each once, and SentencesAfter the reverse.
	SentencesBefore, SentencesAfter []string
}

// Failed says whether the segment fails the gate: a lost quote, or an added
// one that isn't a name span's. Sentence differences and name spans alone
// don't.
func (s SegmentReport) Failed() bool {
	return len(s.LostQuotes) > 0 || len(s.AddedQuotes) > len(s.NameSpans)
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

// String renders the report: per segment a heading line with its counts, then
// a line for each lost or added quote, each differing sentence and each name
// span. A quote is cut to its first 120 code points and a sentence to its
// first 160, enough to find it in the notes.
func (r *Report) String() string {
	var b strings.Builder
	for _, s := range r.Segments {
		fmt.Fprintf(&b, "## %s: %d atomic notes; quotes %d->%d\n", s.Folder, s.AtomicNotes, s.QuotesBefore, s.QuotesAfter)
		for _, e := range s.LostQuotes {
			fmt.Fprintf(&b, "  QUOTE LOST x%d: %s\n", e.Count, textrule.Head(e.Key, 120))
		}
		for _, e := range s.AddedQuotes {
			fmt.Fprintf(&b, "  QUOTE ADDED x%d: %s\n", e.Count, textrule.Head(e.Key, 120))
		}
		for _, t := range s.SentencesBefore {
			fmt.Fprintf(&b, "  SENTENCE before: %s\n", textrule.Head(t, 160))
		}
		for _, t := range s.SentencesAfter {
			fmt.Fprintf(&b, "  SENTENCE after:  %s\n", textrule.Head(t, 160))
		}
		for _, e := range s.NameSpans {
			fmt.Fprintf(&b, "  NAME SPAN x%d: %s\n", e.Count, textrule.Head(e.Key, 120))
		}
	}
	return b.String()
}

// Check diffs the split in folders against the stage 1 notes it split,
// stage1, given in byte order of their names, with name spans looked for in
// src. Segment `<n>`'s stage 1 notes are those named `<n> *.md`, and its hub
// is the note in its folder named after the folder minus its number.
func Check(src *source.Source, stage1 []*note.Note, folders []split.SegmentFolder) *Report {
	loc := mechcheck.NewLocator(src)
	r := &Report{}
	for _, f := range folders {
		var quotesBefore, quotesAfter, sentencesBefore, sentencesAfter counter.Counter
		for _, n := range f.Stage1(stage1) {
			count(n, &quotesBefore, &sentencesBefore)
		}
		var atomic []*note.Note
		for _, n := range f.Notes {
			if !f.IsHub(n) {
				count(n, &quotesAfter, &sentencesAfter)
				atomic = append(atomic, n)
			}
		}
		added := quotesAfter.Minus(&quotesBefore)
		r.Segments = append(r.Segments, SegmentReport{
			Folder:          f.Name,
			AtomicNotes:     len(f.Notes) - 1,
			QuotesBefore:    quotesBefore.Total(),
			QuotesAfter:     quotesAfter.Total(),
			LostQuotes:      quotesBefore.Minus(&quotesAfter),
			AddedQuotes:     added,
			NameSpans:       nameSpans(loc, added, atomic),
			SentencesBefore: keys(sentencesBefore.Minus(&sentencesAfter)),
			SentencesAfter:  keys(sentencesAfter.Minus(&sentencesBefore)),
		})
	}
	return r
}

// nameSpans gives the added quotes, added, that are name spans': each that
// the atomic notes have at least as many footnotes with as it is added, whose
// quotes are exact in the sections they name.
func nameSpans(loc *mechcheck.Locator, added []counter.Entry, atomic []*note.Note) []counter.Entry {
	out := []counter.Entry{}
	for _, e := range added {
		spans := 0
		for _, n := range atomic {
			for _, d := range n.Defs() {
				if q, ok := d.QuotedText(); !ok || q != e.Key {
					continue
				}
				if tr, ok := d.Trace(); ok && loc.Exact(tr) {
					spans++
				}
			}
		}
		if spans >= e.Count {
			out = append(out, e)
		}
	}
	return out
}

// count adds n's quotes and sentences to theirs.
func count(n *note.Note, quotes, sentences *counter.Counter) {
	for _, q := range n.QuotedTexts() {
		quotes.Add(q)
	}
	for _, s := range n.BodySentences() {
		sentences.Add(s)
	}
}

// keys gives each entry's key, once.
func keys(entries []counter.Entry) []string {
	out := []string{}
	for _, e := range entries {
		out = append(out, e.Key)
	}
	return out
}
