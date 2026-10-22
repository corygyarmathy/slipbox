package lossproposer

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/note"
)

// Item is one proposed loss as a run's reply gives it.
type Item struct {
	// Run is the run that proposed it, from 1.
	Run int `json:"run"`
	// Idea is the item's one-line idea, without its number or bold markers.
	Idea   string   `json:"idea"`
	Quotes []string `json:"quotes"`
	Why    string   `json:"why"`
	// Text is the item as the reply writes it, from its numbered line to its
	// last line.
	Text string `json:"text"`
}

// Line is a reply line outside any item, such as "No losses proposed.", with
// the run that wrote it.
type Line struct {
	Run  int    `json:"run"`
	Text string `json:"text"`
}

// Reply is a proposer reply parsed.
type Reply struct {
	Items []Item
	// Other holds every non-blank line outside an item but the heading, so
	// nothing the proposer wrote is dropped.
	Other []string
}

// itemStart is a numbered item's first line, `<n>. <idea>`.
var itemStart = regexp.MustCompile(`^\d+\.\s+(.*)$`)

// bullet is a list item's first line.
var bullet = regexp.MustCompile(`^[-*]\s`)

// field is an item's `- Quote: …` or `- Why: …` line, the bullet `-` or `*`.
var field = regexp.MustCompile(`^[-*]\s+(Quote|Why):\s*(.*)$`)

// Parse parses a proposer reply. The parser reads shape, not intent: an item
// whose Why line says it is not a loss is still an item. A line is:
//
//   - the heading, if it is the first line that starts with `# `
//   - an item's first line, if it starts with a number and a dot
//   - part of the current item, if it is indented or a bullet; a Quote or
//     Why line also gives the item its quote or why
//   - blank, and skipped
//   - otherwise kept in Other, and it ends the current item
func Parse(reply string) Reply {
	r := Reply{Items: []Item{}, Other: []string{}}
	var cur *Item
	var text []string
	end := func() {
		if cur != nil {
			cur.Text = strings.TrimRight(strings.Join(text, "\n"), "\n")
			r.Items = append(r.Items, *cur)
			cur, text = nil, nil
		}
	}
	heading := false
	for _, line := range strings.Split(reply, "\n") {
		s := strings.TrimSpace(line)
		switch {
		case s == "":
			if cur != nil {
				text = append(text, "")
			}
		case !heading && cur == nil && strings.HasPrefix(s, "# "):
			heading = true
		case !indented(line) && itemStart.MatchString(s):
			end()
			cur = &Item{Idea: bold(itemStart.FindStringSubmatch(s)[1]), Quotes: []string{}}
			text = []string{strings.TrimRight(line, " \t\r")}
		case cur != nil && (indented(line) || bullet.MatchString(s)):
			text = append(text, strings.TrimRight(line, " \t\r"))
			if m := field.FindStringSubmatch(s); m != nil {
				if m[1] == "Quote" {
					cur.Quotes = append(cur.Quotes, quoted(m[2]))
				} else {
					cur.Why = m[2]
				}
			}
		default:
			end()
			r.Other = append(r.Other, s)
		}
	}
	end()
	return r
}

// indented says whether line starts with a space or tab.
func indented(line string) bool {
	return line != "" && (line[0] == ' ' || line[0] == '\t')
}

// bold strips the `**` around an item's idea.
func bold(s string) string {
	if t, ok := strings.CutPrefix(s, "**"); ok {
		if t, ok := strings.CutSuffix(t, "**"); ok {
			return t
		}
	}
	return s
}

// quoted is a Quote line's quote: the text between its first opening quote
// mark and its last closing one, or the whole text if it has none.
func quoted(s string) string {
	start := strings.IndexAny(s, "\"“")
	if start < 0 {
		return s
	}
	_, size := utf8.DecodeRuneInString(s[start:])
	rest := s[start+size:]
	end := strings.LastIndexAny(rest, "\"”")
	if end < 0 {
		return rest
	}
	return rest[:end]
}

// Loss is one entry in the union: one item, or items from different runs
// that cite any of the same uncovered text, merged.
type Loss struct {
	// Wordings are the merged items, at most one per run, in run order.
	Wordings []Item `json:"wordings"`
}

// Segment is one segment's proposed losses: the union of its runs.
type Segment struct {
	Segment int    `json:"segment"`
	Heading string `json:"heading"`
	Losses  []Loss `json:"losses"`
	// Remarks are every run's items that cite no uncovered line, in run
	// order, or nil if there are none.
	Remarks []Item `json:"remarks,omitempty"`
	// Other is every run's lines outside an item, such as "No losses
	// proposed.".
	Other []Line `json:"other"`
}

// Union is the union of replies, one per run in run order, to the segment
// whose uncovered list is list. Every item is kept. One whose quotes cite no
// line of list, such as "Quote: none uncovered", isn't a loss: it goes to
// Remarks. Any other is a loss, whatever its why says, and merges
// into the first loss, in reply order, that holds no item from its run and
// cites any of the same uncovered text: a line of list its quotes fall in,
// or, for a quote that falls in no line, the same quote. Items from one run
// are never merged, since the brief gives one item per idea. Quotes and lines are
// compared after note.MatchForm and ignoring case, since a PARTIAL line
// gives its uncovered part in capitals and the proposer restores the
// source's case. A quote is split into pieces at each ellipsis and at
// sentence ends, so one that shortens a line with "…" or runs from one line
// into the next still cites its lines. Pieces an ellipsis separates fall in
// each line that holds them all in order. Failing that, and for a piece
// with no ellipsis beside it, a piece falls in the line it equals, or if it
// equals none, in each line where either holds the other: a piece may cite
// part of a sentence. A quote cites what its pieces cite.
func Union(list mechcheck.SectionCoverage, replies []Reply) Segment {
	seg := Segment{Segment: list.Segment, Heading: list.Heading, Losses: []Loss{}, Other: []Line{}}
	lines := make([]string, len(list.Sentences))
	for i, u := range list.Sentences {
		lines[i] = fold(u.Text)
	}

	// cited[n] is the text loss n cites, through its items' quotes.
	var cited []map[citation]bool
	for k, r := range replies {
		for _, it := range r.Items {
			it.Run = k + 1
			keys := map[citation]bool{}
			for _, q := range it.Quotes {
				for _, c := range cites(q, lines) {
					keys[c] = true
				}
			}
			if !citesLine(keys) {
				seg.Remarks = append(seg.Remarks, it)
				continue
			}
			n := mergeInto(seg.Losses, cited, it.Run, keys)
			if n < 0 {
				n = len(seg.Losses)
				seg.Losses = append(seg.Losses, Loss{})
				cited = append(cited, map[citation]bool{})
			}
			seg.Losses[n].Wordings = append(seg.Losses[n].Wordings, it)
			maps.Copy(cited[n], keys)
		}
		for _, o := range r.Other {
			seg.Other = append(seg.Other, Line{Run: k + 1, Text: o})
		}
	}
	return seg
}

// mergeInto gives the index of the first of losses, which cite cited, that
// holds no item from run and cites any of keys, or -1 if there is none.
func mergeInto(losses []Loss, cited []map[citation]bool, run int, keys map[citation]bool) int {
	for n, l := range losses {
		if slices.ContainsFunc(l.Wordings, func(w Item) bool { return w.Run == run }) {
			continue
		}
		for c := range keys {
			if cited[n][c] {
				return n
			}
		}
	}
	return -1
}

// citesLine says whether keys hold a line of the uncovered list.
func citesLine(keys map[citation]bool) bool {
	for c := range keys {
		if c.line >= 0 {
			return true
		}
	}
	return false
}

// Remark is a remark as one line: its run, then its lines as the reply
// writes them, each trimmed, joined by spaces.
func Remark(it Item) string {
	var parts []string
	for _, l := range strings.Split(it.Text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			parts = append(parts, l)
		}
	}
	return fmt.Sprintf("run %d's item: %s", it.Run, strings.Join(parts, " "))
}

// citation is uncovered text a quote cites: a line of the uncovered list by
// its index, or a piece of a quote that falls in no line, folded.
type citation struct {
	line  int
	quote string
}

// ellipsis is where a quote cuts words out of its line: "…", "..." or
// ". . .", in square brackets or not.
var ellipsis = regexp.MustCompile(`\[?(?:…|\.\.\.|\. \. \.)\]?`)

// chains gives a quote's pieces, folded, in chains: the quote split at each
// ellipsis, and each part split into sentences as the coverage check splits
// a paragraph, so a quote that shortens a line or runs across lines gives
// pieces that each fall in a line. Pieces an ellipsis separates are one
// chain, a line with words cut out; a sentence end starts a new chain.
// Sentences are split before folding, since a sentence starts with a
// capital. A quote that is one sentence with no ellipsis is one chain of one
// piece. A piece with no letter or digit, such as the full stop of a quote
// ending "....", is dropped.
func chains(q string) [][]string {
	var out [][]string
	for k, part := range ellipsis.Split(q, -1) {
		part = note.MatchForm(part)
		for j, r := range mechcheck.SentenceRanges(part) {
			p := fold(part[r[0]:r[1]])
			if !strings.ContainsFunc(p, func(c rune) bool { return unicode.IsLetter(c) || unicode.IsDigit(c) }) {
				continue
			}
			if k > 0 && j == 0 && len(out) > 0 {
				out[len(out)-1] = append(out[len(out)-1], p)
			} else {
				out = append(out, []string{p})
			}
		}
	}
	return out
}

// cites gives what a quote cites: what each of its chains cites. A chain of
// several pieces cites each line of lines, folded, that holds all its pieces
// in order. Failing that, as for a chain of one piece, each piece cites as
// citesPiece gives it. An empty quote, or an empty line, cites nothing.
func cites(q string, lines []string) []citation {
	var out []citation
	for _, ch := range chains(q) {
		var holding []citation
		for i, l := range lines {
			if len(ch) > 1 && l != "" && inOrder(l, ch) {
				holding = append(holding, citation{line: i})
			}
		}
		if len(holding) > 0 {
			out = append(out, holding...)
			continue
		}
		for _, p := range ch {
			out = append(out, citesPiece(p, lines)...)
		}
	}
	return out
}

// inOrder says whether l holds each of pieces, each after the one before.
func inOrder(l string, pieces []string) bool {
	for _, p := range pieces {
		k := strings.Index(l, p)
		if k < 0 {
			return false
		}
		l = l[k+len(p):]
	}
	return true
}

// citesPiece gives what a folded piece of a quote cites: the lines of lines,
// folded, that it equals, or if none, each line it falls in or that falls in
// it, or if none, the piece itself, as text off the list.
func citesPiece(p string, lines []string) []citation {
	var exact, within []citation
	for i, l := range lines {
		switch {
		case l == "":
		case l == p:
			exact = append(exact, citation{line: i})
		case strings.Contains(l, p) || strings.Contains(p, l):
			within = append(within, citation{line: i})
		}
	}
	if len(exact) > 0 {
		return exact
	}
	if len(within) > 0 {
		return within
	}
	return []citation{{line: -1, quote: p}}
}

// fold is s as quotes and lines are compared: in match form, as the trace
// match compares it, and in lower case.
func fold(s string) string {
	return strings.ToLower(note.MatchForm(s))
}
