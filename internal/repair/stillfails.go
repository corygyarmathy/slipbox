package repair

import (
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/corygyarmathy/slipbox/internal/checker"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/note"
)

// The steps a still-failing flag can come from.
const (
	StepMechanical = "1.2"
	StepSupport    = "1.3a"
	StepReadsAlone = "1.3b"
)

// Step 1.2's grades, as the repair pass names its flags.
const (
	GradeBroken    = "broken"
	GradeUntraced  = "untraced"
	GradeLint      = "lint"
	GradeNoSegment = "no segment"
)

// Entry is one statement that still fails after the repair pass, with every
// flag on it from steps 1.2 and 1.3 merged.
type Entry struct {
	// File is the note's file name, or "" for a checker flag whose H1 names
	// no note.
	File string `json:"file"`
	// Statement is the statement as its note gives it, or "" if a flag
	// resolved to none: then Quote is the checker's quote.
	Statement string `json:"statement"`
	Quote     string `json:"quote,omitempty"`
	// Traces are the footnote definitions the statement cites, as written.
	Traces []string `json:"traces"`
	// Grades are every flag's grades, each once, in the order first given.
	Grades []string `json:"grades"`
	Flags  []Flag   `json:"flags"`
}

// Flag is one flag on an Entry's statement.
type Flag struct {
	// Step is StepMechanical, StepSupport or StepReadsAlone.
	Step   string   `json:"step"`
	Grades []string `json:"grades"`
	// Quote is the statement's start as the checker quoted it, for a step 1.3
	// flag.
	Quote  string `json:"quote,omitempty"`
	Reason string `json:"reason"`
}

// StillFails lists the statements of notes that still fail: step 1.2's
// failures in mech, support's flags with a grade that counts (Counts) and
// every one of readsAlone's flags. Each flag carries its note file, as
// checker.Run records it.
//
// The list has one entry per statement, however many flags it has and however
// differently a checker quoted it. A checker flag is resolved to
// a statement of its note by its quote, read up to its first "…" as the
// statement's start, both in the match form the mechanical check compares
// a quote in (note.MatchForm), which drops `*`, `_` and footnote references
// and straightens curly quotes. The statement is the first one, in note
// order, that the quote starts, possibly running into the statements after
// it on the same line, or else the first that holds it. A flag that resolves
// to no statement, or names no note, keeps its quote and gets an entry of its
// own, one per note file and quote.
//
// Entries are in note order, then statement order, then those with no
// statement, in the order their flags were given.
func StillFails(notes []*note.Note, mech *mechcheck.Report, support, readsAlone []checker.Flag) []Entry {
	l := &list{byKey: map[entryKey]*listEntry{}, notes: map[string]*noteStatements{}}
	for i, n := range notes {
		l.notes[n.Name] = newNoteStatements(i, n)
	}
	for _, nr := range mech.Notes {
		ns := l.notes[nr.Name]
		for _, f := range nr.Findings {
			if f.Failed() {
				l.addMechanical(nr.Name, ns, f)
			}
		}
	}
	for _, f := range countedSupport(support) {
		l.addChecker(StepSupport, f)
	}
	for _, f := range readsAlone {
		l.addChecker(StepReadsAlone, f)
	}
	return l.entries()
}

// noteStatements is a note's statements, with what resolving a quote reads.
type noteStatements struct {
	order int
	note  *note.Note
	stmts []note.Statement
	// forms is each statement in match form, and runs each statement in
	// match form together with the statements after it on its line.
	forms, runs []string
	// defs are the note's footnote definitions as written, by label.
	defs map[string]string
}

func newNoteStatements(order int, n *note.Note) *noteStatements {
	ns := &noteStatements{order: order, note: n, stmts: n.Statements(), defs: map[string]string{}}
	for _, l := range n.Lines {
		if l.Kind == note.Definition {
			if _, ok := ns.defs[l.Def.Label]; !ok {
				ns.defs[l.Def.Label] = l.Text
			}
		}
	}
	for i, s := range ns.stmts {
		ns.forms = append(ns.forms, note.MatchForm(s.Text))
		joined := s.Text
		for j := i + 1; j < len(ns.stmts) && ns.stmts[j].Line == s.Line; j++ {
			joined += " " + ns.stmts[j].Text
		}
		ns.runs = append(ns.runs, listMarker.ReplaceAllString(note.MatchForm(joined), ""))
	}
	return ns
}

// listMarker is a list item's marker at the start of a statement in match
// form, which has already dropped a `*` bullet.
var listMarker = regexp.MustCompile(`^(?:-|[0-9]+\.)\s*`)

// resolve gives the index of the statement quote names, or -1.
func (ns *noteStatements) resolve(quote string) int {
	q := quote
	if i := strings.Index(q, "…"); i >= 0 {
		q = q[:i]
	}
	if i := strings.Index(q, "..."); i >= 0 {
		q = q[:i]
	}
	q = note.MatchForm(q)
	if q == "" {
		return -1
	}
	for i, r := range ns.runs {
		if strings.HasPrefix(r, q) {
			return i
		}
	}
	// A quote that starts no statement resolves only to the one statement
	// that holds it: a fragment two statements hold is listed by its quote.
	found := -1
	for i, n := range ns.forms {
		if strings.Contains(n, q) {
			if found >= 0 {
				return -1
			}
			found = i
		}
	}
	return found
}

// onLine gives the indexes of the statements on the note's line i.
func (ns *noteStatements) onLine(i int) []int {
	var out []int
	for j, s := range ns.stmts {
		if s.Line == i {
			out = append(out, j)
		}
	}
	return out
}

// entryKey identifies an entry: a note's statement, or a note file and a
// quote that resolved to none.
type entryKey struct {
	file  string
	stmt  int
	quote string
}

type listEntry struct {
	Entry
	order, stmt, seen int
}

type list struct {
	notes map[string]*noteStatements
	byKey map[entryKey]*listEntry
}

// entry gives the entry for key, making it if it's new.
func (l *list) entry(key entryKey, newEntry func() Entry) *listEntry {
	if e, ok := l.byKey[key]; ok {
		return e
	}
	e := &listEntry{Entry: newEntry(), stmt: key.stmt, seen: len(l.byKey)}
	if e.Traces == nil {
		e.Traces = []string{}
	}
	e.Grades, e.Flags = []string{}, []Flag{}
	e.order = len(l.notes)
	if ns, ok := l.notes[key.file]; ok {
		e.order = ns.order
	}
	l.byKey[key] = e
	return e
}

// statementEntry gives the entry for statement i of the note file.
func (l *list) statementEntry(file string, ns *noteStatements, i int) *listEntry {
	return l.entry(entryKey{file: file, stmt: i}, func() Entry {
		s := ns.stmts[i]
		e := Entry{File: file, Statement: s.Text}
		for _, ref := range s.Refs {
			if d, ok := ns.defs[ref]; ok && !slices.Contains(e.Traces, d) {
				e.Traces = append(e.Traces, d)
			}
		}
		return e
	})
}

// unresolvedEntry gives the entry for a flag on file, quoting quote, that
// resolved to no statement.
func (l *list) unresolvedEntry(file, quote string) *listEntry {
	return l.entry(entryKey{file: file, stmt: -1, quote: note.MatchForm(quote)}, func() Entry {
		return Entry{File: file, Quote: quote}
	})
}

func (e *listEntry) add(f Flag) {
	e.Flags = append(e.Flags, f)
	for _, g := range f.Grades {
		if !slices.Contains(e.Grades, g) {
			e.Grades = append(e.Grades, g)
		}
	}
}

func (l *list) addChecker(step string, f checker.Flag) {
	grades := make([]string, len(f.Grades))
	for i, g := range f.Grades {
		grades[i] = string(g)
	}
	flag := Flag{Step: step, Grades: grades, Quote: f.Statement, Reason: f.Reason}
	if ns, ok := l.notes[f.File]; ok && f.File != "" {
		if i := ns.resolve(f.Statement); i >= 0 {
			l.statementEntry(f.File, ns, i).add(flag)
			return
		}
	}
	l.unresolvedEntry(f.File, f.Statement).add(flag)
}

// addMechanical adds a step 1.2 finding on the note file. A trace's finding is
// the statement's that cites it; a lint finding about a line's end is its last
// statement's, and one about a list item's start its first statement's. A
// finding with no statement, such as NoSegment, which is about the note's
// name, gets an entry with its finding as its quote.
func (l *list) addMechanical(file string, ns *noteStatements, f mechcheck.Finding) {
	var grade string
	switch f.Kind {
	case mechcheck.Broken, mechcheck.SectionNotFound:
		grade = GradeBroken
	case mechcheck.Untraced:
		grade = GradeUntraced
	case mechcheck.LeadIn, mechcheck.NoTerminal, mechcheck.LowercaseItem:
		grade = GradeLint
	default:
		grade = GradeNoSegment
	}
	reason := strings.TrimSpace(f.String())
	flag := Flag{Step: StepMechanical, Grades: []string{grade}, Reason: reason}
	if ns != nil && f.Line >= 0 {
		if on := ns.onLine(f.Line); len(on) > 0 {
			i := on[len(on)-1]
			switch f.Kind {
			case mechcheck.LowercaseItem:
				i = on[0]
			case mechcheck.Broken, mechcheck.SectionNotFound:
				for _, j := range on {
					if slices.Contains(ns.stmts[j].Refs, f.Ref) {
						i = j
						break
					}
				}
			}
			l.statementEntry(file, ns, i).add(flag)
			return
		}
	}
	l.unresolvedEntry(file, reason).add(flag)
}

func (l *list) entries() []Entry {
	all := make([]*listEntry, 0, len(l.byKey))
	for _, e := range l.byKey {
		all = append(all, e)
	}
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.order != b.order {
			return a.order < b.order
		}
		if (a.stmt < 0) != (b.stmt < 0) {
			return b.stmt < 0
		}
		if a.stmt != b.stmt {
			return a.stmt < b.stmt
		}
		return a.seen < b.seen
	})
	out := make([]Entry, len(all))
	for i, e := range all {
		out[i] = e.Entry
	}
	return out
}
