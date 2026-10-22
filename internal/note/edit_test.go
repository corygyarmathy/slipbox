package note_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/note"
)

// editNote is a stage 1 note with a name span mid-statement, so a statement
// can hold more than one reference.
const editNote = "# 2 Definition\n\n" +
	"The shape shown by[^1] the examples is clear.[^2] It has a name.[^3]\n\n" +
	"The builder is not sure of it.[^4]\n\n" +
	"[^1]: [[S#D]] \"The shape shown by\"\n" +
	"[^2]: [[S#D]] \"the examples is clear.\"\n" +
	"[^3]: [[S#D]] \"It has a name.\"\n" +
	"[^4]: [[S#D]] \"The builder is not sure of it.\"\n"

func applyEdits(t *testing.T, text, block string) string {
	t.Helper()
	edits, err := note.ParseEdits(block)
	if err != nil {
		t.Fatal(err)
	}
	n, err := note.Parse("2 Definition.md", text).Apply(edits)
	if err != nil {
		t.Fatal(err)
	}
	return n.Render()
}

func TestApply(t *testing.T) {
	tests := []struct {
		name, block, want string
	}{
		{
			name: "a statement replaced, quoted without its references",
			block: "--- REPLACE ---\nIt has a name.\n--- WITH ---\nIt has a famous name.[^1]\n" +
				"[^1]: [[S#D]] \"It has a famous name.\"\n",
			want: "# 2 Definition\n\n" +
				"The shape shown by[^1] the examples is clear.[^2] It has a famous name.[^3]\n\n" +
				"The builder is not sure of it.[^4]\n\n" +
				"[^1]: [[S#D]] \"The shape shown by\"\n" +
				"[^2]: [[S#D]] \"the examples is clear.\"\n" +
				"[^3]: [[S#D]] \"It has a famous name.\"\n" +
				"[^4]: [[S#D]] \"The builder is not sure of it.\"\n",
		},
		{
			// An antecedent put before a statement, as segment 2's baseline
			// repair did, renumbers every later footnote.
			name: "a statement split into two quotes, with an antecedent before it",
			block: "--- REPLACE ---\nThe shape shown by[^1] the examples is clear.[^2]\n--- WITH ---\n" +
				"In winter it is ice.[^1] The shape shown by[^2] the examples is\nclear.[^3]\n" +
				"[^1]: [[S#D]] \"In winter it is ice.\"\n" +
				"[^2]: [[S#D]] \"The shape shown by\"\n" +
				"[^3]: [[S#D]] \"the examples is clear.\"\n",
			want: "# 2 Definition\n\n" +
				"In winter it is ice.[^1] The shape shown by[^2] the examples is clear.[^3] It has a name.[^4]\n\n" +
				"The builder is not sure of it.[^5]\n\n" +
				"[^1]: [[S#D]] \"In winter it is ice.\"\n" +
				"[^2]: [[S#D]] \"The shape shown by\"\n" +
				"[^3]: [[S#D]] \"the examples is clear.\"\n" +
				"[^4]: [[S#D]] \"It has a name.\"\n" +
				"[^5]: [[S#D]] \"The builder is not sure of it.\"\n",
		},
		{
			name:  "a statement dropped, and its paragraph with it",
			block: "--- REPLACE ---\nThe builder is not sure of it.[^4]\n--- WITH ---\n",
			want: "# 2 Definition\n\n" +
				"The shape shown by[^1] the examples is clear.[^2] It has a name.[^3]\n\n" +
				"[^1]: [[S#D]] \"The shape shown by\"\n" +
				"[^2]: [[S#D]] \"the examples is clear.\"\n" +
				"[^3]: [[S#D]] \"It has a name.\"\n",
		},
		{
			name:  "a statement dropped from within its paragraph",
			block: "--- REPLACE ---\nThe shape shown by the examples is clear.\n--- WITH ---\n",
			want: "# 2 Definition\n\n" +
				"It has a name.[^1]\n\n" +
				"The builder is not sure of it.[^2]\n\n" +
				"[^1]: [[S#D]] \"It has a name.\"\n" +
				"[^2]: [[S#D]] \"The builder is not sure of it.\"\n",
		},
		{
			name: "statements inserted after one, and a paragraph after another and after the H1",
			block: "--- AFTER ---\nThe shape shown by[^1] the examples is clear.[^2]\n--- INSERT ---\nA new one.[^1]\n[^1]: [[S#D]] \"A new one.\"\n" +
				"--- AFTER ---\nThe builder is not sure of it.[^4]\n--- PARAGRAPH ---\nA last one.[^7]\n[^7]: [[S#D]] \"A last one.\"\n" +
				"--- AFTER ---\n# 2 Definition\n--- PARAGRAPH ---\nA first one.[^1]\n[^1]: [[S#D]] \"A first one.\"\n",
			want: "# 2 Definition\n\n" +
				"A first one.[^1]\n\n" +
				"The shape shown by[^2] the examples is clear.[^3] A new one.[^4] It has a name.[^5]\n\n" +
				"The builder is not sure of it.[^6]\n\n" +
				"A last one.[^7]\n\n" +
				"[^1]: [[S#D]] \"A first one.\"\n" +
				"[^2]: [[S#D]] \"The shape shown by\"\n" +
				"[^3]: [[S#D]] \"the examples is clear.\"\n" +
				"[^4]: [[S#D]] \"A new one.\"\n" +
				"[^5]: [[S#D]] \"It has a name.\"\n" +
				"[^6]: [[S#D]] \"The builder is not sure of it.\"\n" +
				"[^7]: [[S#D]] \"A last one.\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := applyEdits(t, editNote, tt.block); got != tt.want {
				t.Errorf("Apply:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

// An Insert with a blank line in its text splits the statement's paragraph:
// the text before the blank line follows the statement, the rest follows the
// paragraph's line, and the paragraph's later statements follow that.
func TestApply_InsertSplitsParagraph(t *testing.T) {
	got := applyEdits(t, editNote, "--- AFTER ---\nThe shape shown by[^1] the examples is clear.[^2]\n--- INSERT ---\n"+
		"It would be:[^1]\n\n1. One.[^2]\n2. Two.[^3]\n"+
		"[^1]: [[S#D]] \"It would be:\"\n[^2]: [[S#D]] \"One.\"\n[^3]: [[S#D]] \"Two.\"\n")
	want := "# 2 Definition\n\n" +
		"The shape shown by[^1] the examples is clear.[^2] It would be:[^3]\n\n" +
		"1. One.[^4]\n2. Two.[^5]\n\n" +
		"It has a name.[^6]\n\n" +
		"The builder is not sure of it.[^7]\n\n" +
		"[^1]: [[S#D]] \"The shape shown by\"\n" +
		"[^2]: [[S#D]] \"the examples is clear.\"\n" +
		"[^3]: [[S#D]] \"It would be:\"\n" +
		"[^4]: [[S#D]] \"One.\"\n" +
		"[^5]: [[S#D]] \"Two.\"\n" +
		"[^6]: [[S#D]] \"It has a name.\"\n" +
		"[^7]: [[S#D]] \"The builder is not sure of it.\"\n"
	if got != want {
		t.Errorf("Apply:\n got %q\nwant %q", got, want)
	}
}

// A split line's paragraph goes on after it: its later lines follow the
// Insert's paragraphs as a paragraph of their own, with the line's tail when
// there is one.
func TestApply_InsertSplitsListParagraph(t *testing.T) {
	const listNote = "# 2 D\n\nA.[^1] B.[^2]\n1. One.[^3]\n\n[^1]: [[S#D]] \"A.\"\n[^2]: [[S#D]] \"B.\"\n[^3]: [[S#D]] \"One.\"\n"
	for _, tt := range []struct{ name, after, want string }{
		{"with a tail", "A.[^1]", "A.[^1] X.[^2]\n\nY.[^3]\n\nB.[^4]\n1. One.[^5]\n\n"},
		{"with no tail", "B.[^2]", "A.[^1] B.[^2] X.[^3]\n\nY.[^4]\n\n1. One.[^5]\n\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := applyEdits(t, listNote, "--- AFTER ---\n"+tt.after+"\n--- INSERT ---\nX.[^1]\n\nY.[^2]\n[^1]: [[S#D]] \"X.\"\n[^2]: [[S#D]] \"Y.\"\n")
			if !strings.Contains(got, "# 2 D\n\n"+tt.want) {
				t.Errorf("Apply:\n got %q\nwant it to start %q", got, tt.want)
			}
		})
	}
}

// An Insert of a list alone, after a mid-paragraph statement, puts the list
// after the statement's line, and nothing in it.
func TestApply_InsertListAlone(t *testing.T) {
	got := applyEdits(t, editNote, "--- AFTER ---\nThe shape shown by[^1] the examples is clear.[^2]\n--- INSERT ---\n"+
		"1. One.[^1]\n2. Two.[^2]\n[^1]: [[S#D]] \"One.\"\n[^2]: [[S#D]] \"Two.\"\n")
	if want := "the examples is clear.[^2]\n\n1. One.[^3]\n2. Two.[^4]\n\nIt has a name.[^5]\n\n"; !strings.Contains(got, want) {
		t.Errorf("Apply:\n got %q\nwant it to hold %q", got, want)
	}
}

// After a paragraph's last statement, an Insert with a blank line gives the
// notes that an Insert of the text before it, then a Paragraph of the rest,
// give.
func TestApply_InsertAtParagraphEnd(t *testing.T) {
	got := applyEdits(t, editNote, "--- AFTER ---\nIt has a name.[^3]\n--- INSERT ---\nIt would be:[^1]\n\n1. One.[^2]\n2. Two.[^3]\n"+
		"[^1]: [[S#D]] \"It would be:\"\n[^2]: [[S#D]] \"One.\"\n[^3]: [[S#D]] \"Two.\"\n")
	inserted := applyEdits(t, editNote, "--- AFTER ---\nIt has a name.[^3]\n--- INSERT ---\nIt would be:[^1]\n[^1]: [[S#D]] \"It would be:\"\n")
	want := applyEdits(t, inserted, "--- AFTER ---\nIt would be:[^4]\n--- PARAGRAPH ---\n1. One.[^1]\n2. Two.[^2]\n"+
		"[^1]: [[S#D]] \"One.\"\n[^2]: [[S#D]] \"Two.\"\n")
	if got != want {
		t.Errorf("Apply:\n got %q\nwant %q", got, want)
	}
}

// A Paragraph's text follows the statement's whole paragraph, so a lead-in's
// list stays with it, and in a batch it comes after an emptied last line
// goes, after an Insert's split of the last line, and in the order of its
// statements.
func TestApply_ParagraphAfterParagraph(t *testing.T) {
	const (
		leadIn = "# 2 D\n\nA.[^1]\n1. One.[^2]\n\n[^1]: [[S#D]] \"A.\"\n[^2]: [[S#D]] \"One.\"\n"
		lines  = "# 2 D\n\nA.[^1]\nB.[^2]\n\nC.[^3]\n\n[^1]: [[S#D]] \"A.\"\n[^2]: [[S#D]] \"B.\"\n[^3]: [[S#D]] \"C.\"\n"
		x      = "--- AFTER ---\nA.[^1]\n--- PARAGRAPH ---\nX.[^1]\n[^1]: [[S#D]] \"X.\"\n"
	)
	for _, tt := range []struct{ name, text, block, want string }{
		{
			"at a paragraph's first line", leadIn, x,
			"# 2 D\n\nA.[^1]\n1. One.[^2]\n\nX.[^3]\n\n" +
				"[^1]: [[S#D]] \"A.\"\n[^2]: [[S#D]] \"One.\"\n[^3]: [[S#D]] \"X.\"\n",
		},
		{
			"with the paragraph's last line emptied", lines, "--- REPLACE ---\nB.\n--- WITH ---\n" + x,
			"# 2 D\n\nA.[^1]\n\nX.[^2]\n\nC.[^3]\n\n" +
				"[^1]: [[S#D]] \"A.\"\n[^2]: [[S#D]] \"X.\"\n[^3]: [[S#D]] \"C.\"\n",
		},
		{
			"with the paragraph's last line split", lines,
			"--- AFTER ---\nB.\n--- INSERT ---\nY.[^1]\n\nZ.[^2]\n[^1]: [[S#D]] \"Y.\"\n[^2]: [[S#D]] \"Z.\"\n" + x,
			"# 2 D\n\nA.[^1]\nB.[^2] Y.[^3]\n\nZ.[^4]\n\nX.[^5]\n\nC.[^6]\n\n" +
				"[^1]: [[S#D]] \"A.\"\n[^2]: [[S#D]] \"B.\"\n[^3]: [[S#D]] \"Y.\"\n[^4]: [[S#D]] \"Z.\"\n" +
				"[^5]: [[S#D]] \"X.\"\n[^6]: [[S#D]] \"C.\"\n",
		},
		{
			"two in one paragraph", lines,
			"--- AFTER ---\nB.\n--- PARAGRAPH ---\nY.[^1]\n[^1]: [[S#D]] \"Y.\"\n" + x,
			"# 2 D\n\nA.[^1]\nB.[^2]\n\nX.[^3]\n\nY.[^4]\n\nC.[^5]\n\n" +
				"[^1]: [[S#D]] \"A.\"\n[^2]: [[S#D]] \"B.\"\n[^3]: [[S#D]] \"X.\"\n[^4]: [[S#D]] \"Y.\"\n[^5]: [[S#D]] \"C.\"\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := applyEdits(t, tt.text, tt.block); got != tt.want {
				t.Errorf("Apply:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

// A heading ends a paragraph, so a Paragraph's text goes before a heading
// that follows the statement's line with no blank line between.
func TestApply_ParagraphStopsAtHeading(t *testing.T) {
	got := applyEdits(t, "# 2 D\n\nA.[^1]\n## Sub\nB.[^2]\n\n[^1]: [[S#D]] \"A.\"\n[^2]: [[S#D]] \"B.\"\n",
		"--- AFTER ---\nA.[^1]\n--- PARAGRAPH ---\nX.[^1]\n[^1]: [[S#D]] \"X.\"\n")
	if want := "# 2 D\n\nA.[^1]\n\nX.[^2]\n## Sub\nB.[^3]\n\n[^1]: [[S#D]] \"A.\"\n[^2]: [[S#D]] \"X.\"\n[^3]: [[S#D]] \"B.\"\n"; got != want {
		t.Errorf("Apply:\n got %q\nwant %q", got, want)
	}
}

// Emptying a paragraph's last line leaves the paragraph, and the blank line
// after it, so the next paragraph stays its own.
func TestApply_EmptiedLastLine(t *testing.T) {
	got := applyEdits(t, "# 2 D\n\nA.[^1]\nB.[^2]\n\nC.[^3]\n\n[^1]: [[S#D]] \"A.\"\n[^2]: [[S#D]] \"B.\"\n[^3]: [[S#D]] \"C.\"\n",
		"--- REPLACE ---\nB.\n--- WITH ---\n")
	if want := "# 2 D\n\nA.[^1]\n\nC.[^2]\n\n[^1]: [[S#D]] \"A.\"\n[^2]: [[S#D]] \"C.\"\n"; got != want {
		t.Errorf("Apply:\n got %q\nwant %q", got, want)
	}
}

// A kept-loss edit that puts a lead-in and a list mid-way through a
// paragraph, after one of its statements, breaks the paragraph at the list,
// as the source's paragraph breaks there: the lead-in joins the statement it
// follows, the list stands on its own, and the paragraph's later statements
// start a paragraph of their own. The lead-in holds an author name span, so
// one of its statements has two references.
func TestApply_ListMidParagraph(t *testing.T) {
	const (
		section = "[[Bridges - Ch 3#Arches]] "
		after   = "An arch carries its load by pushing outward.[^3]"
		text    = "# 2 Arches\n\n" +
			"The arch is older than the beam.[^1]\n\n" +
			"Builders learned this slowly.[^2] " + after + " Why does it stand at all?[^4] It stands by its shape.[^5]\n\n" +
			"[^1]: " + section + "\"The arch is older than the beam.\"\n" +
			"[^2]: " + section + "\"Builders learned this slowly.\"\n" +
			"[^3]: " + section + "\"An arch carries its load by pushing outward.\"\n" +
			"[^4]: " + section + "\"Why does it stand at all?\"\n" +
			"[^5]: " + section + "\"It stands by its shape.\"\n"
	)
	block := "--- AFTER ---\n" + after + "\n--- INSERT ---\n" +
		"Of the parts which I[^1], J. Doe,[^2] have named, two matter most:[^3]\n\n" +
		"1. The keystone, which locks the ring.[^4]\n" +
		"2. The abutment, which takes the thrust.[^5]\n" +
		"[^1]: " + section + "\"Of the parts which I\"\n" +
		"[^2]: [[Bridges - Ch 3]] \"J. Doe\"\n" +
		"[^3]: " + section + "\"have named, two matter most:\"\n" +
		"[^4]: " + section + "\"The keystone, which locks the ring.\"\n" +
		"[^5]: " + section + "\"The abutment, which takes the thrust.\"\n"
	edits, err := note.ParseEdits(block)
	if err != nil {
		t.Fatal(err)
	}
	n := note.Parse("2 Arches.md", text)
	got, err := n.Apply(edits)
	if err != nil {
		t.Fatal(err)
	}

	if want := "Builders learned this slowly.[^2] An arch carries its load by pushing outward.[^3] Of the parts which I[^4], J. Doe,[^5] have named, two matter most:[^6]\n\n" +
		"1. The keystone, which locks the ring.[^7]\n" +
		"2. The abutment, which takes the thrust.[^8]\n\n" +
		"Why does it stand at all?[^9] It stands by its shape.[^10]\n\n"; !strings.Contains(got.Render(), want) {
		t.Errorf("Apply gave no %q in:\n%s", want, got.Render())
	}
	// Every statement keeps its traces: the note's, with the edit's new
	// statements after the one it quotes.
	var want []string
	for _, s := range traced(n) {
		want = append(want, s)
		if strings.HasPrefix(s, note.StripRefs(after)+" <- ") {
			want = append(want, traced(note.Parse("edit.md", edits[0].Text+"\n\n"+defLines(edits[0].Defs)))...)
		}
	}
	if g := traced(got); !reflect.DeepEqual(g, want) {
		t.Errorf("statements and traces:\n got %q\nwant %q", g, want)
	}
}

// traced gives each of a note's statements, references dropped, with the
// values of its references' definitions.
func traced(n *note.Note) []string {
	defs := map[string]string{}
	for _, d := range n.Defs() {
		defs[d.Label] = d.Value
	}
	var out []string
	for _, s := range n.Statements() {
		var values []string
		for _, r := range s.Refs {
			values = append(values, defs[r])
		}
		out = append(out, note.StripRefs(s.Text)+" <- "+strings.Join(values, " | "))
	}
	return out
}

func defLines(defs []note.Def) string {
	var b strings.Builder
	for _, d := range defs {
		b.WriteString("[^" + d.Label + "]: " + d.Value + "\n")
	}
	return b.String()
}

// An edit to a note with no footnotes yet ends the note with its traces.
func TestApply_NoteWithoutDefinitions(t *testing.T) {
	got := applyEdits(t, "# 0 A\n\nUntraced.",
		"--- REPLACE ---\nUntraced.\n--- WITH ---\nTraced.[^1]\n[^1]: [[S#A]] \"Traced.\"\n")
	if want := "# 0 A\n\nTraced.[^1]\n\n[^1]: [[S#A]] \"Traced.\"\n"; got != want {
		t.Errorf("Apply:\n got %q\nwant %q", got, want)
	}
}

func TestParseEdits(t *testing.T) {
	got, err := note.ParseEdits("\n--- AFTER ---\n  A  \nstatement.[^3]\n--- PARAGRAPH ---\nOne.[^1]\n\nTwo.[^2]\n[^1]: [[S]] \"One.\"\n  [^2]: [[S]] \"Two.\"\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []note.Edit{{
		Op: note.Paragraph, Statement: "A statement.[^3]", Text: "One.[^1]\n\nTwo.[^2]",
		Defs: []note.Def{{Label: "1", Value: `[[S]] "One."`}, {Label: "2", Value: `[[S]] "Two."`}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseEdits:\n got %+v\nwant %+v", got, want)
	}
}

// A list's items stay on lines of their own, except in text that goes in
// the statement's paragraph.
func TestParseEdits_Lists(t *testing.T) {
	for _, tt := range []struct{ name, block, want string }{
		{"a lead-in and a list, inserted", "--- AFTER ---\nA.\n--- INSERT ---\nIt\nwould be:\n\n1. One\nitem.\n2. Two.\n", "It would be:\n\n1. One item.\n2. Two."},
		{"a lead-in and a list, in a paragraph", "--- AFTER ---\nA.\n--- PARAGRAPH ---\nIt\nwould be:\n\n1. One\nitem.\n2. Two.\n", "It would be:\n\n1. One item.\n2. Two."},
		{"a list in a paragraph's first lines", "--- AFTER ---\nA.\n--- PARAGRAPH ---\nIt would be:\n- One.\n- Two.\n", "It would be:\n- One.\n- Two."},
		{"a lead-in and a list with no blank line, inserted", "--- AFTER ---\nA.\n--- INSERT ---\nIt would be:\n1. One.\n2. Two.\n\nB.\n", "It would be:\n\n1. One.\n2. Two.\n\nB."},
		{"a list alone, inserted", "--- AFTER ---\nA.\n--- INSERT ---\n1. One.\n* Two.\n", "\n\n1. One.\n* Two."},
		{"prose wrapped before a number", "--- AFTER ---\nA.\n--- PARAGRAPH ---\nThe bridge opened in\n1899. It stood.\n", "The bridge opened in 1899. It stood."},
		{"a list in a replacement, joined", "--- REPLACE ---\nA.\n--- WITH ---\nIt would be:\n1. One.\n", "It would be: 1. One."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			edits, err := note.ParseEdits(tt.block)
			if err != nil {
				t.Fatal(err)
			}
			if got := edits[0].Text; got != tt.want {
				t.Errorf("Text = %q, want %q", got, tt.want)
			}
		})
	}
}

// A malformed edit is an error, not a changed note.
func TestParseEdits_Malformed(t *testing.T) {
	for _, tt := range []struct{ name, block, want string }{
		{"empty", "\n", "no edits"},
		{"text before the first edit", "Here:\n--- REPLACE ---\nA.\n--- WITH ---\n", "text before"},
		{"no text part", "--- REPLACE ---\nA.\n", "no text part"},
		{"no statement", "--- REPLACE ---\n--- WITH ---\nB.\n", "quotes no statement"},
		{"text part first", "--- WITH ---\nB.\n", "before a statement part"},
		{"two statement parts", "--- REPLACE ---\nA.\n--- AFTER ---\nB.\n", "not a text part"},
		{"REPLACE with INSERT", "--- REPLACE ---\nA.\n--- INSERT ---\nB.\n", "follows --- REPLACE ---"},
		{"AFTER with WITH", "--- AFTER ---\nA.\n--- WITH ---\nB.\n", "follows --- AFTER ---"},
		{"an insert of nothing", "--- AFTER ---\nA.\n--- INSERT ---\n", "adds no text"},
		{"a reference with no definition", "--- REPLACE ---\nA.\n--- WITH ---\nB.[^9]\n", "[^9] in its text has no definition"},
		{"a definition with no reference", "--- REPLACE ---\nA.\n--- WITH ---\nB.\n[^1]: [[S]] \"B.\"\n", "[^1] is defined but"},
		{"a blank line in a replacement", "--- REPLACE ---\nA.\n--- WITH ---\nB.\n\nC.\n", "has a blank line"},
		{"a definition twice", "--- REPLACE ---\nA.\n--- WITH ---\nB.[^1]\n[^1]: x\n[^1]: y\n", "defined twice"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := note.ParseEdits(tt.block); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

// An edit may quote a sentence that the note model reads as more than one
// statement, or as part of one.
func TestApply_Sentences(t *testing.T) {
	tests := []struct{ name, text, block, want string }{
		{
			name: "a sentence cut after a quoted question's reference",
			text: "# 2 D\n\nHe asks \"what is piety?\"[^1] and answers it.[^2] Then more.[^3]\n\n" +
				"[^1]: [[S#D]] \"what is piety?\"\n[^2]: [[S#D]] \"and answers it.\"\n[^3]: [[S#D]] \"Then more.\"\n",
			block: "--- REPLACE ---\nHe asks \"what is piety?\" and answers it.\n--- WITH ---\nHe asks a question.[^1]\n" +
				"[^1]: [[S#D]] \"He asks a question.\"\n",
			want: "# 2 D\n\nHe asks a question.[^1] Then more.[^2]\n\n" +
				"[^1]: [[S#D]] \"He asks a question.\"\n[^2]: [[S#D]] \"Then more.\"\n",
		},
		{
			name:  "one sentence of an untraced statement",
			text:  "# 2 D\n\nUntraced one. Untraced two.\n",
			block: "--- REPLACE ---\nUntraced two.\n--- WITH ---\nTraced two.[^1]\n[^1]: [[S#D]] \"Traced two.\"\n",
			want:  "# 2 D\n\nUntraced one. Traced two.[^1]\n\n[^1]: [[S#D]] \"Traced two.\"\n",
		},
		{
			name: "one sentence of a traced statement, ending in joining text",
			text: "# 2 D\n\nA prerequisite[^1] is a platitude. Clearly, it needs no definition.[^2]\n\n" +
				"[^1]: [[S#D]] \"A prerequisite\"\n[^2]: [[S#D]] \"Clearly, it needs no definition.\"\n",
			block: "--- REPLACE ---\nA prerequisite[^1] is a platitude.\n--- WITH ---\nThis is a platitude.[^1]\n" +
				"[^1]: [[S#D]] \"This is a platitude.\"\n",
			want: "# 2 D\n\nThis is a platitude.[^1] Clearly, it needs no definition.[^2]\n\n" +
				"[^1]: [[S#D]] \"This is a platitude.\"\n[^2]: [[S#D]] \"Clearly, it needs no definition.\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := applyEdits(t, tt.text, tt.block); got != tt.want {
				t.Errorf("Apply:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestApply_Rejects(t *testing.T) {
	n := note.Parse("2 Definition.md", editNote+"\nIt has a name.[^5]\n")
	for _, tt := range []struct{ name, block, want string }{
		{"no such statement", "--- REPLACE ---\nIt has no name.\n--- WITH ---\n", "no statement in 2 Definition.md reads"},
		{"two statements match", "--- REPLACE ---\nIt has a name.\n--- WITH ---\n", "2 statements in 2 Definition.md read"},
		{"two edits at one statement", "--- REPLACE ---\nThe builder is not sure of it.\n--- WITH ---\n" +
			"--- AFTER ---\nThe builder is not sure of it.\n--- INSERT ---\nMore.[^1]\n[^1]: x\n", "same statement"},
		{"an edit inside a run another replaces", "--- REPLACE ---\nThe shape shown by the examples is clear. It has a name.[^3]\n--- WITH ---\n" +
			"--- AFTER ---\nThe shape shown by the examples is clear.\n--- INSERT ---\nMore.[^1]\n[^1]: x\n", "same statement"},
		{"the H1 replaced", "--- REPLACE ---\n# 2 Definition\n--- WITH ---\n", "no statement in"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			edits, err := note.ParseEdits(tt.block)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := n.Apply(edits); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

// An edit made other than by ParseEdits can't break a line mid-way through
// a paragraph.
func TestApply_RejectsLineBreak(t *testing.T) {
	n := note.Parse("2 Definition.md", editNote)
	for _, e := range []note.Edit{
		{Op: note.Insert, Statement: "It has a name.", Text: "It would be:\n1. One."},
		{Op: note.Replace, Statement: "It has a name.", Text: "It would be:\n1. One."},
	} {
		if _, err := n.Apply([]note.Edit{e}); err == nil || !strings.Contains(err.Error(), "line break") {
			t.Errorf("Apply(%+v): err = %v, want one about a line break", e, err)
		}
	}
}

// Renumber keeps a definition nothing uses unless told to drop it.
func TestRenumber(t *testing.T) {
	n := note.Parse("n.md", "# T\n\nB.[^5] A.[^2]\n\n[^2]: a\n[^5]: b\n[^7]: kept\n[^8]: dropped\n")
	got := n.Renumber(map[string]bool{"8": true}).Render()
	if want := "# T\n\nB.[^1] A.[^2]\n\n[^1]: b\n[^2]: a\n[^3]: kept\n"; got != want {
		t.Errorf("Renumber:\n got %q\nwant %q", got, want)
	}
}

// An Every edit drops each copy of a statement a note has twice, which an
// edit at one statement can't name: stage-1-final drops a broken or
// untraced statement, however many times its note has it.
func TestApply_EveryDropsEachCopy(t *testing.T) {
	text := "# 1 Two\n\nGamma is third.[^1]\n\nAn untraced claim.\n\nDelta is fourth.[^2] An untraced claim.\n\n" +
		"[^1]: [[S#D]] \"Gamma is third.\"\n[^2]: [[S#D]] \"Delta is fourth.\"\n"
	n := note.Parse("1 Two.md", text)
	if _, err := n.Apply([]note.Edit{{Op: note.Replace, Statement: "An untraced claim."}}); err == nil {
		t.Fatal("Apply without Every made an edit at one of two matching statements")
	}
	got, err := n.Apply([]note.Edit{{Op: note.Replace, Statement: "An untraced claim.", Every: true}})
	if err != nil {
		t.Fatal(err)
	}
	want := "# 1 Two\n\nGamma is third.[^1]\n\nDelta is fourth.[^2]\n\n" +
		"[^1]: [[S#D]] \"Gamma is third.\"\n[^2]: [[S#D]] \"Delta is fourth.\"\n"
	if got.Render() != want {
		t.Errorf("Apply:\n got %q\nwant %q", got.Render(), want)
	}
	if _, err := n.Apply([]note.Edit{{Op: note.Replace, Statement: "An untraced claim.", Text: "A claim.", Every: true}}); err == nil {
		t.Error("Apply made an Every edit that adds text")
	}
}
