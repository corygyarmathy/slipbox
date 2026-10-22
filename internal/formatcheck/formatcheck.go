// Package formatcheck is step 3.1's gate, the format check: did formatting
// add markup only? Per note, it compares step 3.1's formatted copy with the
// stripped note step 2.2 gave it. The frontmatter, the H1 and the footnote
// definitions must be byte-identical, and the body with its markup stripped
// the same word for word. Each heading or callout title the note has must
// occur in the source, and a forbidden element is flagged. Any of these is a
// problem, and fails the gate. The report also gives the markup each note
// gained and lost.
//
// A body whose words differ is reported as TEXT CHANGED, followed by the
// first 20 lines of seqmatch.Changes over the stripped note's words and the
// formatted copy's, joined by spaces: `- <word>` for each word removed and
// `+ <word>` for each added, with the words kept left out. The changes come
// in the note's order, and where a run of words is replaced, all of its `-`
// words come before all of its `+` words. A long replaced run can fill the
// 20 with its `-` words alone.
//
// Run also fails a stripped note with no formatted copy: step 3.1 falls back
// a reply that leaves a note out, but the formatted notes can be edited by
// hand before the gate runs again, and a note deleted then would otherwise
// pass. Check leaves this
// out, since only Run reads the folder. A segment that fails the gate falls
// back to its stripped notes, and the run goes on.
//
// The report is plain text (ADR 0002 §3): Report.String gives its shape. It
// lists markup in a fixed order of elements (compareElements), callouts first,
// by type in byte order, and inline emphasis last, not in the order a note
// first used them, so an element has the same place in every note's lines and
// in the totals.
//
// The body's words are read from the note model's body lines, so a line ends
// only at `\n`, `\r\n` or a lone `\r`. A heading that holds `\v`, `\f`,
// U+001C–U+001E, U+0085, U+2028 or U+2029 is one heading, so its title runs
// to the end of the line, as Markdown reads it.
//
// A note's `\r\n` and lone `\r` line endings read as `\n`, so a note whose
// line endings alone changed hasn't changed. The frontmatter is read as the
// note model reads it, as package splitcheck describes. The body leaves out
// the H1's line, not the first place the H1's text occurs, so a `## <title>`
// heading over a `# <title>` H1 is still checked.
package formatcheck

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/corygyarmathy/slipbox/internal/fallback"
	"github.com/corygyarmathy/slipbox/internal/gate"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/seqmatch"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/textrule"
)

// GateName names the gate when a segment falls back for failing it.
const GateName = "the format check"

// FolderGateName names the check that every formatted segment folder is a
// segment's.
const FolderGateName = "step 3.1's folder check"

var _ gate.Report = (*Report)(nil)

// Report is the check's result: one NoteReport per formatted note, by
// segment folder then note, each in byte order of its name.
type Report struct {
	Notes []NoteReport
	// Changed counts the notes whose text differs from their stage 2 note's.
	Changed int
	// Gained is the markup the notes gained, all together, in element order.
	Gained []Markup
	// Missing are the stage 2 notes with no formatted copy, each as
	// `<n> <title>/<note>`. Run fills it, and Check doesn't.
	Missing []string
}

// NoteReport is one formatted note's result.
type NoteReport struct {
	// Folder is the segment folder's name, `<n> <title>`, and Name the
	// note's file name.
	Folder, Name string
	// New is true of a note with no stage 2 note of its folder and name.
	// Nothing else is checked of it.
	New bool
	// Changed is true if the note's text differs from its stage 2 note's.
	Changed bool
	// Gained is the markup the note has more of than its stage 2 note, by
	// element, and Lost the markup it has less of, each in element order.
	Gained, Lost []Markup
	// Problems are what fail the gate, each as the report words it.
	Problems []string
}

// Markup counts one markup element: `callout:<type>`, `callout/quote line`,
// `h2` to `h6`, `list item`, `**x**` or `_x_`.
type Markup struct {
	Element string
	Count   int
}

// The markup elements strip counts, but for a callout, `callout:<type>`, and
// a heading, `h<level>`.
const (
	calloutElement    = "callout:"
	quoteLineElement  = "callout/quote line"
	listItemElement   = "list item"
	boldElement       = "**x**"
	underscoreElement = "_x_"
)

// elementOrder is the order of the markup elements in a report after the
// callouts, which come first, by type: block markup before inline, and
// headings by level.
var elementOrder = []string{quoteLineElement, "h2", "h3", "h4", "h5", "h6", listItemElement, boldElement, underscoreElement}

// compareElements orders markup elements as a report lists them: callouts
// by type, in byte order, then elementOrder.
func compareElements(a, b string) int {
	rank := func(e string) int {
		if strings.HasPrefix(e, calloutElement) {
			return -1
		}
		return slices.Index(elementOrder, e)
	}
	return cmp.Or(cmp.Compare(rank(a), rank(b)), strings.Compare(a, b))
}

// surplus gives the elements a counts more of than b, each with how many
// more, in element order. With b nil, it is a's counts in element order.
func surplus(a, b map[string]int) []Markup {
	out := []Markup{}
	for _, e := range slices.SortedFunc(maps.Keys(a), compareElements) {
		if n := a[e] - b[e]; n > 0 {
			out = append(out, Markup{Element: e, Count: n})
		}
	}
	return out
}

func (n NoteReport) failed() bool { return n.New || len(n.Problems) > 0 }

// Failed says whether any note fails the gate: a new note, one with a
// problem, or a missing one.
func (r *Report) Failed() bool {
	if len(r.Missing) > 0 {
		return true
	}
	for _, n := range r.Notes {
		if n.failed() {
			return true
		}
	}
	return false
}

// FailingFolders names the folders with a note that fails the gate, in the
// report's order, each once, then those with a missing note.
func (r *Report) FailingFolders() []string {
	var out []string
	for _, n := range r.Notes {
		if n.failed() && (len(out) == 0 || out[len(out)-1] != n.Folder) {
			out = append(out, n.Folder)
		}
	}
	for _, rel := range r.Missing {
		if f, _, _ := strings.Cut(rel, "/"); !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out
}

// String renders the report: a block for each note that changed, then a
// line of totals. A text is quoted, as Go quotes a string, and a forbidden
// element is the text it matched, less the whitespace around it. A missing
// note has a line of its own before the totals, and counts as one with
// problems.
func (r *Report) String() string {
	var b strings.Builder
	bad := 0
	for _, n := range r.Notes {
		rel := n.Folder + "/" + n.Name
		if n.failed() {
			bad++
		}
		if n.New {
			fmt.Fprintf(&b, "NEW NOTE (not in stage 2): %s\n", rel)
			continue
		}
		if len(n.Problems) == 0 && len(n.Gained) == 0 && !n.Changed {
			continue
		}
		fmt.Fprintf(&b, "\n%s\n", rel)
		if len(n.Gained) > 0 {
			fmt.Fprintf(&b, "  gained: %s\n", counts(n.Gained))
		}
		if len(n.Lost) > 0 {
			fmt.Fprintf(&b, "  lost: %s\n", counts(n.Lost))
		}
		for _, p := range n.Problems {
			fmt.Fprintf(&b, "  PROBLEM: %s\n", p)
		}
	}
	for _, rel := range r.Missing {
		bad++
		fmt.Fprintf(&b, "MISSING NOTE (not in stage 3): %s\n", rel)
	}
	fmt.Fprintf(&b, "\n== %d notes changed; %d with problems; markup gained: %s\n", r.Changed, bad, counts(r.Gained))
	return b.String()
}

// counts renders the markup counts as `<element> <count>`, comma-separated
// in their order, or as `none`.
func counts(markup []Markup) string {
	if len(markup) == 0 {
		return "none"
	}
	parts := make([]string, len(markup))
	for i, m := range markup {
		parts[i] = m.Element + " " + strconv.Itoa(m.Count)
	}
	return strings.Join(parts, ", ")
}

// Check checks the formatted notes in stage3 against the stage 2 notes in
// stage2, the same note being the one of the same folder and name, and the
// titles against src. Each segment's folders are in byte order of their
// names, as split.ReadFolders gives them.
func Check(stage2, stage3 []split.SegmentFolder, src *source.Source) *Report {
	before := map[string]*note.Note{}
	for _, f := range stage2 {
		for _, n := range f.Notes {
			before[f.Name+"/"+n.Name] = n
		}
	}
	sourceText := flat(src.Frame)
	r := &Report{}
	totals := map[string]int{}
	for _, f := range stage3 {
		for _, n3 := range f.Notes {
			nr := NoteReport{Folder: f.Name, Name: n3.Name}
			n2, ok := before[f.Name+"/"+n3.Name]
			if !ok {
				nr.New = true
				r.Notes = append(r.Notes, nr)
				continue
			}
			p2, p3 := parse(n2), parse(n3)
			w2, _, m2 := strip(p2.body)
			w3, titles, m3 := strip(p3.body)
			if p2.front != p3.front {
				nr.Problems = append(nr.Problems, "frontmatter changed")
			}
			if p2.h1 != p3.h1 {
				nr.Problems = append(nr.Problems, fmt.Sprintf("H1 changed: %s -> %s", p2.h1, p3.h1))
			}
			if !slices.Equal(p2.defs, p3.defs) {
				nr.Problems = append(nr.Problems, "footnote definitions changed")
			}
			if !slices.Equal(w2, w3) {
				diff := seqmatch.Changes(w2, w3)
				nr.Problems = append(nr.Problems, "TEXT CHANGED: "+strings.Join(diff[:min(20, len(diff))], " "))
			}
			for _, t := range titles {
				if !strings.Contains(sourceText, flat(t)) {
					nr.Problems = append(nr.Problems, "title not in source: "+strconv.Quote(t))
				}
			}
			for _, m := range forbidden.FindAllString(p3.bodyText(), -1) {
				nr.Problems = append(nr.Problems, "forbidden element: "+strconv.Quote(strings.TrimSpace(m)))
			}
			nr.Gained, nr.Lost = surplus(m3, m2), surplus(m2, m3)
			nr.Changed = text(n2) != text(n3)
			if nr.Changed {
				r.Changed++
			}
			for _, m := range nr.Gained {
				totals[m.Element] += m.Count
			}
			r.Notes = append(r.Notes, nr)
		}
	}
	r.Gained = surplus(totals, nil)
	return r
}

// text is the note's text with every line ending read as `\n`.
func text(n *note.Note) string {
	var b strings.Builder
	for _, l := range n.Lines {
		b.WriteString(l.Text)
		if l.EOL != "" {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// parts are a note split into the parts the check compares.
type parts struct {
	// front is the frontmatter's text, fences and line endings included.
	front string
	// h1 is the H1 line, quoted, or `none`.
	h1 string
	// defs are the footnote definition lines after the frontmatter.
	defs []string
	// body is the lines after the frontmatter, with the H1 and every
	// definition blank, and eols says which of them end in a newline.
	body []string
	eols []bool
}

// bodyText is the body's text, every line ending `\n`.
func (p parts) bodyText() string {
	var b strings.Builder
	for i, l := range p.body {
		b.WriteString(l)
		if p.eols[i] {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func parse(n *note.Note) parts {
	p := parts{h1: "none"}
	var front strings.Builder
	for _, l := range n.Lines {
		line := l.Text
		switch l.Kind {
		case note.Frontmatter:
			front.WriteString(l.Text + "\n")
			continue
		case note.H1:
			p.h1 = strconv.Quote(l.Text)
			line = ""
		case note.Definition:
			p.defs = append(p.defs, l.Text)
			line = ""
		}
		p.body = append(p.body, line)
		p.eols = append(p.eols, l.EOL != "")
	}
	p.front = front.String()
	return p
}

var (
	callout  = regexp.MustCompile(`^>[` + textrule.Space + `]*\[!([` + textrule.Word + `]+)\][-+]?[` + textrule.Space + `]*(.*)$`)
	heading  = regexp.MustCompile(`^(#{2,6})[` + textrule.Space + `]+(.*)$`)
	listItem = regexp.MustCompile(`^(?:[-*+]|` + textrule.Digit + `+\.)[` + textrule.Space + `]+`)
	bold     = regexp.MustCompile(`\*\*[^*]+\*\*`)
	// forbidden matches what formatting may not add: the callouts reserved
	// or unused, highlights, rules, comments, embeds and tasks.
	forbidden = regexp.MustCompile(`(?im)\[!(?:Note|Quote|Tip|Warning)\]|==[^=]+==|^[` + textrule.Space + `]*(?:---|\*\*\*)[` + textrule.Space + `]*$|%%|!\[\[|^[` + textrule.Space + `]*- \[[ x]\]`)
	spaces    = regexp.MustCompile(`[` + textrule.Space + `]+`)
	marks     = strings.NewReplacer("*", "", "_", "")
)

// strip gives the body's words with its markup stripped, the titles its
// markup gives, and a count of the markup by element. A callout's first line
// gives its type and title and no words.
func strip(body []string) (words, titles []string, m map[string]int) {
	m = map[string]int{}
	for _, line := range body {
		s := strings.TrimSpace(line)
		if c := callout.FindStringSubmatch(s); c != nil {
			m[calloutElement+c[1]]++
			if c[2] != "" {
				titles = append(titles, c[2])
			}
			continue
		}
		if strings.HasPrefix(s, ">") {
			s = strings.TrimSpace(strings.TrimLeft(s, "> "))
			m[quoteLineElement]++
		}
		if h := heading.FindStringSubmatch(s); h != nil {
			m["h"+strconv.Itoa(len(h[1]))]++
			titles = append(titles, h[2])
			continue
		}
		if loc := listItem.FindStringIndex(s); loc != nil {
			m[listItemElement]++
			s = s[loc[1]:]
		}
		m[boldElement] += len(bold.FindAllStringIndex(s, -1))
		m[underscoreElement] += underscored(s)
		s = dropUnderscores(strings.ReplaceAll(s, "*", ""))
		words = append(words, strings.Fields(s)...)
	}
	return words, titles, m
}

// underscored counts the `_x_` emphasis in s: an underscore not after a word
// character or `*`, the text up to the next underscore, and that underscore,
// not before a word character. Go's regexp has no lookaround to spell it.
func underscored(s string) int {
	r := []rune(s)
	n := 0
	for i := 0; i < len(r); i++ {
		if r[i] != '_' || (i > 0 && (textrule.IsWord(r[i-1]) || r[i-1] == '*')) {
			continue
		}
		j := i + 1
		for j < len(r) && r[j] != '_' {
			j++
		}
		if j == i+1 || j == len(r) || (j+1 < len(r) && textrule.IsWord(r[j+1])) {
			continue
		}
		n++
		i = j
	}
	return n
}

// dropUnderscores drops each underscore not between two word characters, so
// `_x_` emphasis goes and a snake_case word keeps its underscore. An
// underscore counts as a word character.
func dropUnderscores(s string) string {
	r := []rune(s)
	var b strings.Builder
	for i, c := range r {
		if c == '_' && (i == 0 || !textrule.IsWord(r[i-1]) || i == len(r)-1 || !textrule.IsWord(r[i+1])) {
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

// missing names each note in stage2 with no note of the same folder and
// name in stage3, as `<n> <title>/<note>`, in stage2's order.
func missing(stage2, stage3 []split.SegmentFolder) []string {
	have := map[string]bool{}
	for _, f := range stage3 {
		for _, n := range f.Notes {
			have[f.Name+"/"+n.Name] = true
		}
	}
	var out []string
	for _, f := range stage2 {
		for _, n := range f.Notes {
			if rel := f.Name + "/" + n.Name; !have[rel] {
				out = append(out, rel)
			}
		}
	}
	return out
}

// flat gives s in the form a title is looked for in the source: emphasis
// markers dropped, whitespace collapsed to single spaces and stripped, and
// lower-cased.
func flat(s string) string {
	return strings.ToLower(strings.TrimSpace(spaces.ReplaceAllString(marks.Replace(s), " ")))
}

// Run runs step 3.1's gate on step 2.2's stripped notes in folder and step
// 3.1's formatted copy of them, writing its report to the step's directory.
// It also fails a stripped note with no formatted copy. It runs only once
// step 3.1 is complete.
//
// A segment with a note that fails the gate falls back, as does each segment
// step 3.1 listed, for a reply it couldn't write, which the gate doesn't
// check: it goes on with its stripped notes, unformatted. The step writes,
// under its `stage-3/`, each segment's notes as they go on, formatted or
// fallen back, and lists the fallbacks in its run.FallbacksFile.
//
// Before the gate, a formatted folder no segment of src claims stops the
// run, with no report, as step 2.1's folder check does: only a hand edit
// makes one, and it has no stripped notes to fall back to.
func Run(src *source.Source, folder *run.Folder) error {
	if !folder.Complete(run.FormatDir) {
		return errors.New("format check: step 3.1 has not formatted the notes, so there are none to check yet")
	}
	stripped, err := split.ReadFolders(filepath.Join(folder.Dir, filepath.FromSlash(run.StrippedNotes)))
	if err != nil {
		return fmt.Errorf("format check: %w", err)
	}
	formattedNotes := filepath.Join(folder.Dir, filepath.FromSlash(run.FormattedNotes))
	formatted, err := split.ReadFolders(formattedNotes)
	if err != nil {
		return fmt.Errorf("format check: %w", err)
	}
	replyFaults, err := fallback.Read(folder, run.FormatDir)
	if err != nil {
		return fmt.Errorf("format check: %w", err)
	}
	step, err := folder.Begin(run.FormatGateDir)
	if err != nil {
		return err
	}
	if unclaimed := split.Unclaimed(src, formatted); len(unclaimed) > 0 {
		return split.UnclaimedStop(FolderGateName, src, formattedNotes, unclaimed)
	}
	fb := split.NewFallbacks(src, replyFaults)
	checkedStripped, checkedFormatted := fb.Without(stripped), fb.Without(formatted)
	r := Check(checkedStripped, checkedFormatted, src)
	r.Missing = missing(checkedStripped, checkedFormatted)
	if err := step.WriteFile(run.FormatCheckFile, []byte(r.String())); err != nil {
		return err
	}
	fb.Fail(r.FailingFolders(), GateName, run.FormatCheckFile)

	byName := map[string]split.SegmentFolder{}
	for _, f := range formatted {
		byName[f.Name] = f
	}
	for _, f := range stripped {
		on := f
		if !fb.Has(f.Name) {
			on = byName[f.Name]
		}
		for _, n := range on.Notes {
			if err := step.WriteFile(path.Join(run.Stage3NotesDir, f.Name, n.Name), []byte(n.Render())); err != nil {
				return err
			}
		}
	}
	if err := fallback.Write(step, fb.Items()); err != nil {
		return err
	}
	return step.Commit()
}
