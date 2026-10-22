package note

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/corygyarmathy/slipbox/internal/textrule"
)

// Op is what an edit does at its statement.
type Op int

const (
	// Replace puts the edit's text, which may be empty, in place of the
	// statement and its traces.
	Replace Op = iota
	// Insert puts the edit's text after the statement, in its paragraph. Text
	// after a blank line, such as a list, goes in new paragraphs after the
	// statement's line, and the statements after it in its paragraph follow
	// them as a paragraph of their own.
	Insert
	// Paragraph puts the edit's text in a new paragraph after the
	// statement's paragraph, its last line included, or first in the body if
	// the statement is the note's H1.
	Paragraph
)

// Edit is one change to a note, addressed by the statement it is made at.
type Edit struct {
	Op Op
	// Statement is the statement as the edit quotes it. It matches the
	// note's statement whose text is the same once footnote references are
	// dropped and whitespace collapsed.
	Statement string
	// Text is the new statements, paragraphs separated by "\n\n" and a
	// paragraph's list items by "\n". An Insert's text may start "\n\n", which
	// puts none of it in the statement's paragraph. Its footnote references
	// are labelled within the edit, and Defs gives their traces.
	Text string
	Defs []Def
	// Every makes a Replace with no text at every statement that matches
	// Statement, not only one, so it drops each copy of a statement a note
	// has more than once.
	Every bool
}

// The part markers of an edit block: each edit is a statement part then a
// text part.
var (
	statementParts = map[string]bool{"--- REPLACE ---": true, "--- AFTER ---": true}
	textParts      = map[string]Op{"--- WITH ---": Replace, "--- INSERT ---": Insert, "--- PARAGRAPH ---": Paragraph}
)

// ParseEdits parses an edit block's body: one or more edits, each
//
//	--- REPLACE ---           --- AFTER ---
//	<statement>               <statement>
//	--- WITH ---              --- INSERT --- or --- PARAGRAPH ---
//	<text>                    <text>
//	<definitions>             <definitions>
//
// A part's lines are joined with spaces, and a blank line between them
// starts a new paragraph. In a text part, a line that starts with a list
// marker after a list item or a line ending with ":" starts a line of its
// own. In an INSERT, a list starts a new paragraph as a blank line would,
// and a list that opens the text puts nothing in the statement's paragraph
// (Text starts "\n\n"). A REPLACE's text goes in the statement's paragraph,
// so its lines are all joined. A footnote definition line in a text part is
// one of its traces.
// Every reference in the text needs a definition and every definition a
// reference. A REPLACE's text may be empty, which drops the statement; the
// other parts may not be.
func ParseEdits(body string) ([]Edit, error) {
	var (
		edits []Edit
		cur   *Edit
		part  string
		lines []string
	)
	end := func() error {
		switch {
		case part == "":
			if t := strings.TrimSpace(strings.Join(lines, "\n")); t != "" {
				return fmt.Errorf("text before the first edit: %q", t)
			}
		case statementParts[part]:
			cur.Statement = joinLines(lines, false)
			if cur.Statement == "" {
				return fmt.Errorf("edit %d: %s quotes no statement", len(edits)+1, part)
			}
		default:
			var text []string
			for _, l := range lines {
				if m := defLine.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
					cur.Defs = append(cur.Defs, Def{Label: m[1], Value: m[2]})
					continue
				}
				text = append(text, l)
			}
			cur.Text = joinLines(text, true)
			first, rest, found := strings.Cut(cur.Text, "\n\n")
			switch {
			case cur.Op == Replace:
				cur.Text = strings.ReplaceAll(first, "\n", " ")
			case cur.Op == Insert && listItem.MatchString(first):
				cur.Text = "\n\n" + first
			case cur.Op == Insert:
				lead, list, _ := strings.Cut(first, "\n")
				cur.Text = lead
				if list != "" {
					cur.Text += "\n\n" + list
				}
			}
			if found && cur.Op != Paragraph {
				cur.Text += "\n\n" + rest
			}
			if err := checkEdit(*cur); err != nil {
				return fmt.Errorf("edit %d: %w", len(edits)+1, err)
			}
			edits = append(edits, *cur)
			cur = nil
		}
		return nil
	}
	for _, l := range strings.Split(body, "\n") {
		m := strings.TrimSpace(l)
		_, isText := textParts[m]
		if !statementParts[m] && !isText {
			lines = append(lines, l)
			continue
		}
		if err := end(); err != nil {
			return nil, err
		}
		switch {
		case statementParts[m] && cur != nil:
			return nil, fmt.Errorf("edit %d: %s follows %s, not a text part", len(edits)+1, m, part)
		case statementParts[m]:
			cur = &Edit{}
		case cur == nil || cur.Statement == "":
			return nil, fmt.Errorf("edit %d: %s comes before a statement part", len(edits)+1, m)
		case (part == "--- REPLACE ---") != (m == "--- WITH ---"):
			return nil, fmt.Errorf("edit %d: %s follows %s", len(edits)+1, m, part)
		default:
			cur.Op = textParts[m]
		}
		part, lines = m, nil
	}
	if err := end(); err != nil {
		return nil, err
	}
	if cur != nil {
		return nil, fmt.Errorf("edit %d: %s has no text part after it", len(edits)+1, part)
	}
	if len(edits) == 0 {
		return nil, fmt.Errorf("no edits")
	}
	return edits, nil
}

// listItem is a list item's marker at the start of a stripped line, as
// traceDiffList reads one.
var listItem = regexp.MustCompile(`^(?:[-*]|` + textrule.Digit + `+\.)[` + textrule.Space + `]+`)

// joinLines joins a part's lines: each stripped, those in a run joined with
// a space, and runs separated by a blank line joined with "\n\n". With
// lists, a line that starts with a list marker is joined to the line before
// it with "\n" instead, if it follows a list item or a line ending with ":".
// A line wrapped mid-sentence before a number, such as "399. He died.", is
// still joined with a space.
func joinLines(lines []string, lists bool) string {
	var paras []string
	var para strings.Builder
	// inList says whether the line being joined started as a list item, and
	// prev is the last stripped line joined.
	inList, prev := false, ""
	for _, l := range append(lines, "") {
		switch l = strings.TrimSpace(l); {
		case l == "":
			if para.Len() > 0 {
				paras = append(paras, para.String())
				para.Reset()
			}
			continue
		case para.Len() == 0:
			inList = listItem.MatchString(l)
		case lists && listItem.MatchString(l) && (inList || strings.HasSuffix(strings.TrimSpace(StripRefs(prev)), ":")):
			para.WriteString("\n")
			inList = true
		default:
			para.WriteString(" ")
		}
		para.WriteString(l)
		prev = l
	}
	return strings.Join(paras, "\n\n")
}

// checkEdit checks that an edit's text and definitions agree.
func checkEdit(e Edit) error {
	if e.Every && (e.Op != Replace || e.Text != "") {
		return fmt.Errorf("only a Replace with no text can be made at every statement that matches")
	}
	if e.Text == "" && e.Op != Replace {
		return fmt.Errorf("it adds no text")
	}
	if e.Op == Replace && strings.Contains(e.Text, "\n\n") {
		return fmt.Errorf("its text has a blank line, which would start a paragraph mid-way through the statement's")
	}
	if first, _, _ := strings.Cut(e.Text, "\n\n"); e.Op != Paragraph && strings.Contains(first, "\n") {
		return fmt.Errorf("its text has a line break before any blank line, which would start a line mid-way through the statement's paragraph")
	}
	defined := map[string]bool{}
	for _, d := range e.Defs {
		if defined[d.Label] {
			return fmt.Errorf("[^%s] is defined twice", d.Label)
		}
		defined[d.Label] = true
	}
	used := map[string]bool{}
	for _, m := range refs.FindAllStringSubmatch(e.Text, -1) {
		if !defined[m[1]] {
			return fmt.Errorf("[^%s] in its text has no definition in the edit", m[1])
		}
		used[m[1]] = true
	}
	for _, d := range e.Defs {
		if !used[d.Label] {
			return fmt.Errorf("[^%s] is defined but its text doesn't use it", d.Label)
		}
	}
	return nil
}

// StatementKey is what an edit's statement is matched on: the text with its
// footnote references dropped and its whitespace collapsed.
func StatementKey(s string) string {
	return strings.TrimSpace(whitespace.ReplaceAllString(StripRefs(s), " "))
}

// sentenceEnd is where a sentence ends within an untraced statement: terminal
// punctuation, any closing quotes, brackets or emphasis markers, then
// whitespace.
var sentenceEnd = regexp.MustCompile(`[.?!:]['"’”)_*]*[` + textrule.Space + `]+`)

// targets gives what an edit's statement may name, in the note model's terms
// or as a sentence: each statement; each run of statements on one line, since
// a sentence with a reference mid-way, after `?"[^1] `, is cut into two; and
// each sentence of a statement holding more than one, since the note model
// reads an untraced line as one statement, and cuts a traced one only after a
// reference, not after a sentence that ends in joining text.
func (n *Note) targets() []Statement {
	stmts := n.Statements()
	out := slices.Clone(stmts)
	for i, s := range stmts {
		text := n.Lines[s.Line].Text
		for j := i + 1; j < len(stmts) && stmts[j].Line == s.Line; j++ {
			var refs []string
			for _, t := range stmts[i : j+1] {
				refs = append(refs, t.Refs...)
			}
			out = append(out, Statement{Line: s.Line, Text: text[s.Start:stmts[j].End], Refs: refs, Start: s.Start, End: stmts[j].End})
		}
		var sentences []Statement
		start := 0
		for _, m := range append(sentenceEnd.FindAllStringIndex(s.Text, -1), []int{len(s.Text), len(s.Text)}) {
			if t := strings.TrimSpace(s.Text[start:m[1]]); t != "" {
				from := s.Start + start + strings.Index(s.Text[start:m[1]], t)
				sentence := Statement{Line: s.Line, Text: t, Start: from, End: from + len(t)}
				for _, r := range refs.FindAllStringSubmatch(t, -1) {
					sentence.Refs = append(sentence.Refs, r[1])
				}
				sentences = append(sentences, sentence)
			}
			start = m[1]
		}
		if len(sentences) > 1 {
			out = append(out, sentences...)
		}
	}
	return out
}

// Apply gives the note with edits made, each at the one statement it quotes,
// and its footnotes renumbered in reading order (Renumber). A replaced
// statement's traces go unless another statement still uses them, and an
// edit's own labels are given new ones. A paragraph left empty is removed.
// An Insert whose text has a blank line splits the statement's paragraph.
// A Paragraph edit's text follows the statement's whole paragraph, after its
// last line that isn't emptied.
// An edit may quote a run of statements on one line, or one sentence of a
// statement, as well as a statement (targets).
//
// It fails, and changes nothing, if an edit's statement matches none of
// these or more than one (unless the edit is Every), or two edits are made
// at overlapping statements.
// The H1 can be a statement only for a Paragraph edit, which then puts its
// text first.
func (n *Note) Apply(edits []Edit) (*Note, error) {
	stmts := n.targets()
	h1 := -1
	for i, l := range n.Lines {
		if l.Kind == H1 {
			h1 = i
			break
		}
	}
	// at[i] is the statement edits[i] is made at: an index into stmts, or -1
	// for the H1. extra are the copies of an Every edit for each statement it
	// matches after its first, made at extraAt.
	at := make([]int, len(edits))
	var extra []Edit
	var extraAt []int
	for i, e := range edits {
		if err := checkEdit(e); err != nil {
			return nil, fmt.Errorf("edit %d: %w", i+1, err)
		}
		key := StatementKey(e.Statement)
		var found []int
		for j, s := range stmts {
			if StatementKey(s.Text) == key {
				found = append(found, j)
			}
		}
		if len(found) == 0 && e.Op == Paragraph && h1 >= 0 && key == StatementKey(n.Lines[h1].Text) {
			found = []int{-1}
		}
		switch {
		case len(found) == 0:
			return nil, fmt.Errorf("edit %d: no statement in %s reads %q", i+1, n.Name, e.Statement)
		case len(found) > 1 && !e.Every:
			return nil, fmt.Errorf("edit %d: %d statements in %s read %q", i+1, len(found), n.Name, e.Statement)
		}
		for c, f := range found {
			for _, k := range slices.Concat(at[:i], extraAt, found[:c]) {
				if overlap(stmts, k, f) {
					return nil, fmt.Errorf("edit %d: an earlier edit is made at the same statement, %q", i+1, e.Statement)
				}
			}
			if c == 0 {
				at[i] = f
				continue
			}
			extra, extraAt = append(extra, e), append(extraAt, f)
		}
	}
	// An Every edit's other statements are made as edits of their own.
	edits, at = append(slices.Clone(edits), extra...), append(at, extraAt...)

	next := 1
	for _, l := range n.Lines {
		labels := l.Refs
		if l.Def != nil {
			labels = append(slices.Clone(labels), l.Def.Label)
		}
		for _, label := range labels {
			if k, err := strconv.Atoi(label); err == nil && k >= next {
				next = k + 1
			}
		}
	}
	texts := make([]string, len(edits))
	var newDefs []string
	dropped := map[string]bool{}
	for i, e := range edits {
		labels := map[string]string{}
		for _, d := range e.Defs {
			labels[d.Label] = strconv.Itoa(next)
			newDefs = append(newDefs, "[^"+labels[d.Label]+"]: "+d.Value)
			next++
		}
		texts[i] = relabel(e.Text, labels)
		if e.Op == Replace {
			for _, r := range stmts[at[i]].Refs {
				dropped[r] = true
			}
		}
	}

	lines := make([]string, len(n.Lines))
	for i, l := range n.Lines {
		lines[i] = l.Text
	}
	// In-line edits go from each line's end, so earlier offsets hold, and
	// new paragraphs follow their line.
	order := make([]int, len(edits))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return start(stmts, at[order[a]]) > start(stmts, at[order[b]]) })
	// An Insert that splits line i writes its paragraphs into the line, so
	// they come before the paragraph's later lines and before after[i].
	// splitEnds[i] says whether the line then ends with one of the Insert's
	// paragraphs, not the line's own tail, so a later line of its paragraph
	// needs a blank line before it.
	splitEnds := map[int]bool{}
	after := map[int][]string{}
	emptied := map[int]bool{}
	// paras[i] are the Paragraph edits' texts that follow the paragraph
	// ending at line i, in the order of their statements.
	paras := map[int][]string{}
	for _, i := range order {
		if at[i] < 0 {
			after[h1] = append(after[h1], texts[i])
			continue
		}
		s := stmts[at[i]]
		l := lines[s.Line]
		switch edits[i].Op {
		case Replace:
			if texts[i] != "" {
				lines[s.Line] = l[:s.Start] + texts[i] + l[s.End:]
				break
			}
			before, rest := strings.TrimRight(l[:s.Start], " \t"), strings.TrimLeft(l[s.End:], " \t")
			if before != "" && rest != "" {
				before += " "
			}
			lines[s.Line] = before + rest
			emptied[s.Line] = strings.TrimSpace(lines[s.Line]) == ""
		case Insert:
			first, rest, found := strings.Cut(texts[i], "\n\n")
			if !found {
				lines[s.Line] = l[:s.End] + " " + texts[i] + l[s.End:]
				break
			}
			lines[s.Line] = l[:s.End]
			if first != "" {
				lines[s.Line] += " " + first
			}
			lines[s.Line] += "\n\n" + rest
			tail := strings.TrimSpace(l[s.End:])
			if tail != "" {
				lines[s.Line] += "\n\n" + tail
			}
			// Edits go from the line's end, so the first split seen is the
			// line's last.
			if _, ok := splitEnds[s.Line]; !ok {
				splitEnds[s.Line] = tail == ""
			}
		case Paragraph:
			end := n.paragraphEnd(s.Line)
			paras[end] = append([]string{texts[i]}, paras[end]...)
		}
	}
	// A paragraph's new paragraphs follow its last line that isn't emptied.
	// The statement a Paragraph edit is made at keeps its line, so there is
	// one.
	for end, ps := range paras {
		for emptied[end] {
			end--
		}
		after[end] = append(after[end], ps...)
	}

	lastDef := -1
	for i, l := range n.Lines {
		if l.Kind == Definition {
			lastDef = i
		}
	}
	// An emptied line goes, and a paragraph left empty goes with the blank
	// line after it. kept says whether a line of the paragraph so far stays.
	skip := map[int]bool{}
	kept := false
	for i, l := range n.Lines {
		if strings.TrimSpace(l.Text) == "" {
			kept = false
			continue
		}
		if !emptied[i] || len(after[i]) > 0 {
			kept = true
			continue
		}
		skip[i] = true
		if !kept && i+1 < len(n.Lines) && strings.TrimSpace(n.Lines[i+1].Text) == "" {
			skip[i+1] = true
		}
	}
	var b strings.Builder
	for i, l := range n.Lines {
		if skip[i] {
			continue
		}
		b.WriteString(lines[i])
		for _, p := range after[i] {
			b.WriteString("\n\n" + p)
		}
		eol := l.EOL
		if splitEnds[i] && len(after[i]) == 0 && i+1 < len(n.Lines) && !skip[i+1] && strings.TrimSpace(lines[i+1]) != "" {
			eol += eol
		}
		if eol == "" && len(newDefs) > 0 && (i == lastDef || lastDef < 0) {
			eol = "\n"
		}
		b.WriteString(eol)
		if i == lastDef {
			for _, d := range newDefs {
				b.WriteString(d + "\n")
			}
		}
	}
	if lastDef < 0 && len(newDefs) > 0 {
		b.WriteString("\n" + strings.Join(newDefs, "\n") + "\n")
	}
	return Parse(n.Name, b.String()).Renumber(dropped), nil
}

// overlap says whether the statements at a and b, indices into stmts or -1
// for the H1, share any of a line.
func overlap(stmts []Statement, a, b int) bool {
	if a < 0 || b < 0 {
		return a == b
	}
	s, t := stmts[a], stmts[b]
	return s.Line == t.Line && s.Start <= t.End && t.Start <= s.End
}

// start orders edits within a note: by line, then by offset in it, with the
// H1 first.
func start(stmts []Statement, at int) int {
	if at < 0 {
		return -1
	}
	return stmts[at].Line<<20 + stmts[at].Start
}

// Renumber gives the note with its footnotes numbered 1, 2, … in the order
// the references first appear outside the definitions, and its definitions
// in that order, where the first one was. A definition no reference uses
// goes if its label is in drop, and otherwise is kept after the others,
// numbered on from them.
func (n *Note) Renumber(drop map[string]bool) *Note {
	labels := map[string]string{}
	for _, l := range n.Lines {
		if l.Kind == Definition || l.Kind == Frontmatter {
			continue
		}
		for _, r := range l.Refs {
			if _, ok := labels[r]; !ok {
				labels[r] = strconv.Itoa(len(labels) + 1)
			}
		}
	}
	type def struct {
		k    int
		text string
	}
	var defs []def
	first := -1
	for i, l := range n.Lines {
		if l.Kind != Definition {
			continue
		}
		if first < 0 {
			first = i
		}
		label, ok := labels[l.Def.Label]
		if !ok {
			if drop[l.Def.Label] {
				continue
			}
			label = strconv.Itoa(len(labels) + 1)
			labels[l.Def.Label] = label
		}
		k, _ := strconv.Atoi(label)
		defs = append(defs, def{k, "[^" + label + "]: " + l.Def.Value})
	}
	sort.SliceStable(defs, func(a, b int) bool { return defs[a].k < defs[b].k })

	var b strings.Builder
	for i, l := range n.Lines {
		switch {
		case i == first:
			for _, d := range defs {
				b.WriteString(d.text + "\n")
			}
		case l.Kind == Definition:
		case l.Kind == Frontmatter:
			b.WriteString(l.Text + l.EOL)
		default:
			b.WriteString(relabel(l.Text, labels) + l.EOL)
		}
	}
	return Parse(n.Name, b.String())
}

// relabel gives text with each footnote reference's label replaced by the one
// labels maps it to.
func relabel(text string, labels map[string]string) string {
	return refs.ReplaceAllStringFunc(text, func(r string) string {
		return "[^" + labels[refs.FindStringSubmatch(r)[1]] + "]"
	})
}

// paragraphEnd gives the last line of the paragraph line i is in: the run of
// body lines that aren't blank or a heading.
func (n *Note) paragraphEnd(i int) int {
	for i+1 < len(n.Lines) {
		l := n.Lines[i+1]
		if l.Kind != Body || strings.HasPrefix(l.Text, "#") || strings.TrimSpace(l.Text) == "" {
			break
		}
		i++
	}
	return i
}
