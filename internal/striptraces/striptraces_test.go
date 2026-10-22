package striptraces_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/fallback"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/splitgates"
	"github.com/corygyarmathy/slipbox/internal/striptraces"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// On the invented split, each atomic note keeps one footnote, the source
// footnote, with `[^1]` at the end of its H1 and its definition the
// source's `source:` value without its YAML quotes. Each hub is unchanged.
// Every other line of every note, frontmatter included, is as it was, but
// for its footnote references. The loose note in no segment folder isn't
// read.
func TestStrip_Split(t *testing.T) {
	src, err := source.Parse(testfixture.Lamp, testfixture.LampChapter)
	if err != nil {
		t.Fatal(err)
	}
	if src.CitekeyLink != testfixture.LampCitekeyLink {
		t.Fatalf("source: line %q, want %q", src.CitekeyLink, testfixture.LampCitekeyLink)
	}
	atomic, hubs := 0, 0
	folders := lampFolders(t)
	for _, f := range folders {
		for _, n := range f.Notes {
			hub := f.IsHub(n)
			got, err := striptraces.Strip(n, src.CitekeyLink, hub)
			if err != nil {
				t.Fatalf("%s/%s: %v", f.Name, n.Name, err)
			}
			if hub {
				hubs++
				if got.Render() != n.Render() {
					t.Errorf("%s/%s: hub changed:\n%s", f.Name, n.Name, got.Render())
				}
				continue
			}
			atomic++
			checkAtomic(t, f.Name+"/"+n.Name, n, got, src.CitekeyLink)
		}
	}
	if hubs != 4 || len(folders) != 4 || atomic != 9 {
		t.Errorf("%d hubs and %d atomic notes in %d folders, want 4, 9 and 4", hubs, atomic, len(folders))
	}
}

// lampFolders reads the invented split's segment folders.
func lampFolders(t *testing.T) []split.SegmentFolder {
	t.Helper()
	dir := t.TempDir()
	testfixture.WriteFiles(t, dir, testfixture.LampSplit())
	folders, err := split.ReadFolders(dir)
	if err != nil {
		t.Fatal(err)
	}
	return folders
}

// checkAtomic checks the stripped note got against the atomic note n it was
// made from.
func checkAtomic(t *testing.T, name string, n, got *note.Note, link string) {
	t.Helper()
	text := got.Render()
	if c := strings.Count(text, "[^"); c != 2 {
		t.Errorf("%s: %d footnote marks, want the source footnote's 2:\n%s", name, c, text)
	}
	defs := got.Defs()
	if len(defs) != 1 || defs[0] != (note.Def{Label: "1", Value: link}) {
		t.Errorf("%s: definitions %+v, want only [^1]: %s", name, defs, link)
	}
	var kept []note.Line
	for _, l := range got.Lines {
		switch {
		case l.Kind == note.H1:
			if !strings.HasSuffix(l.Text, "[^1]") {
				t.Errorf("%s: H1 %q doesn't end with the source footnote's reference", name, l.Text)
			}
			l.Text = strings.TrimSuffix(l.Text, "[^1]")
		case l.Kind == note.Definition:
			continue
		case len(l.Refs) > 0:
			t.Errorf("%s: %q keeps a footnote reference", name, l.Text)
		}
		kept = append(kept, l)
	}
	var want []note.Line
	for _, l := range n.Lines {
		if l.Kind == note.Definition {
			continue
		}
		for _, r := range l.Refs {
			l.Text = strings.Replace(l.Text, "[^"+r+"]", "", 1)
		}
		want = append(want, l)
	}
	if len(kept) != len(want) {
		t.Fatalf("%s: %d lines besides the definitions, want %d:\n%s", name, len(kept), len(want), text)
	}
	for i := range want {
		if kept[i].Text != want[i].Text || kept[i].EOL != want[i].EOL {
			t.Errorf("%s: line %d is %q%q, want %q%q", name, i+1, kept[i].Text, kept[i].EOL, want[i].Text, want[i].EOL)
		}
	}
}

// A name span's wording stays in its statement: only its footnote goes.
func TestStrip_KeepsNameSpans(t *testing.T) {
	n := note.Parse("Prisms.md", "# Prisms\n\nAll the lamps plus _the Fresnel lens_[^1] are lit at dusk.[^2]\n\n"+
		"[^1]: [[S#A]] \"lamps plus *the Fresnel lens*\"\n[^2]: [[S#A]] \"All these are lit at dusk.\"\n")
	got, err := striptraces.Strip(n, "[[@S]]", false)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Prisms[^1]\n\nAll the lamps plus _the Fresnel lens_ are lit at dusk.\n\n[^1]: [[@S]]\n"
	if got.Render() != want {
		t.Errorf("Strip =\n%q, want\n%q", got.Render(), want)
	}
}

// An atomic note gets the source footnote at its end, after a blank line,
// with the note's own line ending, even where its definitions came before
// more of its body. One whose H1 carries a statement's reference loses it
// before `[^1]` goes on, and a source footnote it already had is replaced.
func TestStrip_Shapes(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"no definitions", "---\r\ntags: []\r\n---\r\n# T\r\n\r\nNo trace.", "---\r\ntags: []\r\n---\r\n# T[^1]\r\n\r\nNo trace.\r\n\r\n[^1]: [[@S]]\r\n"},
		{"ends blank", "# T\n\nNo trace.\n\n", "# T[^1]\n\nNo trace.\n\n[^1]: [[@S]]\n"},
		{"H1 statement", "# A title statement.[^1]\n\n[^1]: [[S#A]] \"A title statement.\"", "# A title statement.[^1]\n\n[^1]: [[@S]]\n"},
		{"body after definitions", "# T\n\nA.[^1]\n\n[^1]: [[S#A]] \"A.\"\n\nB.\n", "# T[^1]\n\nA.\n\n\nB.\n\n[^1]: [[@S]]\n"},
		{"source footnote", "# T[^1]\n\nA.[^2]\n\n[^1]: [[@Old]]\n[^2]: [[S#A]] \"A.\"\n", "# T[^1]\n\nA.\n\n[^1]: [[@S]]\n"},
	}
	for _, tt := range tests {
		got, err := striptraces.Strip(note.Parse("n.md", tt.in), "[[@S]]", false)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if got.Render() != tt.want {
			t.Errorf("%s: Strip =\n%q, want\n%q", tt.name, got.Render(), tt.want)
		}
	}
}

// A definition that is neither a trace nor a source footnote, or an atomic
// note without an H1, is an error, not a guess.
func TestStrip_Rejects(t *testing.T) {
	for _, tt := range []struct {
		name, in, want string
	}{
		{"other definition", "# T\n\nA.[^1]\n\n[^1]: a note of the author's\n", `n.md, line 5: footnote [^1] is neither a trace nor the source footnote: "[^1]: a note of the author's"`},
		{"no H1", "A.[^1]\n\n[^1]: [[S#A]] \"A.\"\n", "n.md has no H1 to put the source footnote after"},
	} {
		_, err := striptraces.Strip(note.Parse("n.md", tt.in), "[[@S]]", false)
		if err == nil || err.Error() != tt.want {
			t.Errorf("%s: Strip error %v, want %q", tt.name, err, tt.want)
		}
	}
}

// The strip check passes the invented split, and fails each
// note Strip refuses, naming its folder: a definition that is neither a
// trace nor the source footnote, even in a hub, and an atomic note without an
// H1.
func TestCheck(t *testing.T) {
	if r := striptraces.Check(lampFolders(t)); r.Failed() || r.String() != "Every note can be stripped.\n" {
		t.Errorf("strip check failed on the invented split:\n%s", r)
	}

	folders := []split.SegmentFolder{
		{Name: "1 One", Notes: []*note.Note{
			note.Parse("One.md", "1. [[A]]\n\n[^9]: an aside\n"),
			note.Parse("A.md", "# A\n\nA.[^1]\n\n[^1]: [[S#A]] \"A.\"\n"),
		}},
		{Name: "2 Two", Notes: []*note.Note{
			note.Parse("B.md", "B.[^1]\n\n[^1]: [[S#A]] \"B.\"\n[^2]: the author's aside\n"),
			note.Parse("Two.md", "1. [[B]]\n"),
		}},
	}
	r := striptraces.Check(folders)
	want := "1 One: One.md, line 3: footnote [^9] is neither a trace nor the source footnote: \"[^9]: an aside\"\n" +
		"2 Two: B.md has no H1 to put the source footnote after\n" +
		"2 Two: B.md, line 4: footnote [^2] is neither a trace nor the source footnote: \"[^2]: the author's aside\"\n"
	if !r.Failed() || r.String() != want {
		t.Errorf("report =\n%s\nwant\n%s", r, want)
	}
	if got := r.FailingFolders(); !slices.Equal(got, []string{"1 One", "2 Two"}) {
		t.Errorf("failing folders %q", got)
	}
}

// An atomic note with a footnote that no reference names and that isn't a
// trace passes the split check and the trace diff, so the strip check fails
// its segment at step 2.1's gates, not at step 2.2. The segment falls back,
// and step 2.2 strips its fallback, the segment's final stage 1 notes and a
// hub, as it strips a split.
func TestRun_StripCheckFallsBack(t *testing.T) {
	src, folder := testfixture.LampRun(t)
	dir := filepath.Join(folder.Dir, filepath.FromSlash(run.SplitNotes), "1 The Keeper's Watch")
	target := filepath.Join(dir, "The Watch Began at Dusk.md")
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, append(b, "[^99]: the author's aside, kept from the source\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := splitgates.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	items, err := fallback.Read(folder, run.SplitGatesDir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []fallback.Item{{Segment: 1, Why: striptraces.GateName, Report: run.StripCheckFile}}; !reflect.DeepEqual(items, want) {
		t.Fatalf("fallbacks %+v, want %+v", items, want)
	}
	if err := striptraces.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	got := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.StrippedNotes)))
	stage1, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(run.FinalNotes)))
	if err != nil {
		t.Fatal(err)
	}
	fb := split.Fallback(src.Segments[1], stage1)
	for _, n := range fb.Notes {
		want, err := striptraces.Strip(n, src.CitekeyLink, fb.IsHub(n))
		if err != nil {
			t.Fatal(err)
		}
		if got[fb.Name+"/"+n.Name] != want.Render() {
			t.Errorf("%s/%s is not the fallback's note stripped:\n%s", fb.Name, n.Name, got[fb.Name+"/"+n.Name])
		}
	}
	if _, ok := got[fb.Name+"/"+filepath.Base(target)]; ok {
		t.Errorf("the split's note that failed went on")
	}
}

// Once the invented split passes its gates, the step writes every note they
// passed on, stripped, under the same folder and name, and is complete.
func TestRun(t *testing.T) {
	src, folder := testfixture.LampRun(t)
	if err := splitgates.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	if err := striptraces.Run(src, folder); err != nil {
		t.Fatal(err)
	}
	if !folder.Complete(run.StripDir) {
		t.Fatal("the step is not complete")
	}
	folders, err := split.ReadFolders(filepath.Join(folder.Dir, filepath.FromSlash(run.SplitOrFallbackNotes)))
	if err != nil {
		t.Fatal(err)
	}
	got := testfixture.Files(t, filepath.Join(folder.Dir, filepath.FromSlash(run.StrippedNotes)))
	count := 0
	for _, f := range folders {
		for _, n := range f.Notes {
			count++
			want, err := striptraces.Strip(n, src.CitekeyLink, f.IsHub(n))
			if err != nil {
				t.Fatal(err)
			}
			if got[f.Name+"/"+n.Name] != want.Render() {
				t.Errorf("%s/%s is not the note stripped", f.Name, n.Name)
			}
		}
	}
	if len(got) != count {
		t.Errorf("%d notes written, want the split's %d", len(got), count)
	}
}

// The step doesn't run until step 2.1's gates are complete: it writes no
// directory.
func TestRun_GatesNotPassed(t *testing.T) {
	src, folder := testfixture.LampRun(t)
	if err := os.Mkdir(filepath.Join(folder.Dir, run.SplitGatesDir+run.PartialSuffix), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := striptraces.Run(src, folder); err == nil {
		t.Fatal("Run succeeded before step 2.1's gates were complete")
	}
	for _, d := range []string{run.StripDir, run.StripDir + run.PartialSuffix} {
		if _, err := os.Stat(filepath.Join(folder.Dir, d)); !os.IsNotExist(err) {
			t.Errorf("%s exists (stat: %v)", d, err)
		}
	}
}
