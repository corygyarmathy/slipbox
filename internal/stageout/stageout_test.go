package stageout

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// pragmatismFormatted is a run folder holding the Pragmatism run's steps up
// to its run report, so step 3.1's gate is complete with the notes it passed
// on, and the run's source.
func pragmatismFormatted(t *testing.T) (*run.Folder, *source.Source) {
	t.Helper()
	return testfixture.PragmatismFolder(t, run.FormatGateDir, run.RunReportDir), testfixture.PragmatismSource(t)
}

// stagedReport is the Pragmatism run's report as step 3.2 stages it.
func stagedReport(t *testing.T) string {
	t.Helper()
	return testfixture.Files(t, filepath.Join(testfixture.PragmatismRun(t), run.RunReportDir))[run.StagedReportFile]
}

// vault makes a vault whose _Slip_Box holds the notes named, each a path
// from _Slip_Box.
func vault(t *testing.T, names ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "vault")
	if err := os.MkdirAll(filepath.Join(dir, SlipBoxDir), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		p := filepath.Join(dir, SlipBoxDir, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("an existing note\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Given the empty vault the Pragmatism run was given, step 3.2 stages what
// the run staged, byte for byte: every note flat, the run report, and a
// name-clash list with no clash.
func TestRun_StagesThePragmatismRun(t *testing.T) {
	folder, src := pragmatismFormatted(t)
	v := vault(t)
	staging := DefaultStaging(v, src.Name)
	if err := Run(src, folder, v, staging); err != nil {
		t.Fatal(err)
	}
	if !folder.Complete(run.StageOutDir) {
		t.Errorf("step 3.2 is not complete")
	}
	got, want := testfixture.Files(t, staging), testfixture.Files(t, testfixture.PragmatismStaged(t))
	if !reflect.DeepEqual(slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want))) {
		t.Fatalf("staged %q, want the run's %q", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
	}
	for name, text := range want {
		if got[name] != text {
			t.Errorf("staged %s is not the run's", name)
		}
	}
	if len(want) != 66 {
		t.Errorf("the run staged %d files, want its 64 notes, the clash list and the run report", len(want))
	}
}

const (
	whatItMeans = "0 What Pragmatism Means"
	truth       = "3 Pragmatism as a Theory of Truth"
	extra       = "8 Extra"
)

// Notes named twice in the run, ignoring case, stay in their segment
// folders, and the clash list names them, and each staged note named like a
// _Slip_Box note. Here a folder no segment of the source claims adds two
// notes named like segment 3's atomic note Inductive Logic and segment 0's
// hub.
func TestRun_ListsNameClashes(t *testing.T) {
	folder, src := pragmatismFormatted(t)
	checked := filepath.Join(folder.Dir, filepath.FromSlash(run.FormattedOrFallbackNotes))
	if err := os.Mkdir(filepath.Join(checked, extra), 0o755); err != nil {
		t.Fatal(err)
	}
	added := map[string]string{
		"Inductive Logic.md":       "# Inductive Logic\n\nA note named as another segment's is.\n",
		"WHAT PRAGMATISM MEANS.md": "# WHAT PRAGMATISM MEANS\n\nA note named as a hub is, in another case.\n",
	}
	for name, text := range added {
		if err := os.WriteFile(filepath.Join(checked, extra, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v := vault(t, "inductive logic.md", "Sub/THE INSTRUMENTAL VIEW OF TRUTH.md", "Unrelated.md", ".hidden/History of the Method.md", "History of the Method.png")
	staging := DefaultStaging(v, src.Name)
	if err := Run(src, folder, v, staging); err != nil {
		t.Fatal(err)
	}

	held := map[string]string{
		"Inductive Logic.md":       truth,
		"What Pragmatism Means.md": whatItMeans,
	}
	want := map[string]string{}
	for name, text := range testfixture.Files(t, testfixture.PragmatismStaged(t)) {
		if name == ClashListFile {
			continue
		}
		if f, ok := held[name]; ok {
			name = f + "/" + name
		}
		want[name] = text
	}
	for name, text := range added {
		want[extra+"/"+name] = text
	}
	got := testfixture.Files(t, staging)
	clashes := got[ClashListFile]
	delete(got, ClashListFile)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("staged %d files, want %d: every note flat as it came but the four named twice, each in its segment folder", len(got), len(want))
		for rel := range want {
			if _, ok := got[rel]; !ok {
				t.Errorf("missing %s", rel)
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(folder.Dir, run.StageOutDir, ClashListFile)); err != nil || string(b) != clashes {
		t.Errorf("the step's directory has no copy of the clash list (err %v)", err)
	}

	link := func(p string) string {
		return "[[Staging/" + src.Name + "/" + strings.TrimSuffix(p, ".md") + "|" + p + "]]"
	}
	for _, line := range []string{
		"- " + link(truth+"/Inductive Logic.md") + " and [[_Slip_Box/inductive logic|_Slip_Box/inductive logic.md]]\n",
		"- " + link("The Instrumental View of Truth.md") + " and [[_Slip_Box/Sub/THE INSTRUMENTAL VIEW OF TRUTH|_Slip_Box/Sub/THE INSTRUMENTAL VIEW OF TRUTH.md]]\n",
		"- " + link(extra+"/Inductive Logic.md") + " and [[_Slip_Box/inductive logic|_Slip_Box/inductive logic.md]]\n",
		"- " + link(whatItMeans+"/What Pragmatism Means.md") + ", " + link(extra+"/WHAT PRAGMATISM MEANS.md") + "\n",
		"- " + link(truth+"/Inductive Logic.md") + ", " + link(extra+"/Inductive Logic.md") + "\n",
	} {
		if !strings.Contains(clashes, line) {
			t.Errorf("the clash list has no line %q:\n%s", line, clashes)
		}
	}
	if n := strings.Count(clashes, "\n- "); n != 5 {
		t.Errorf("the clash list has %d entries, want 5:\n%s", n, clashes)
	}
	if strings.Contains(clashes, "Unrelated") || strings.Contains(clashes, ".hidden") || strings.Contains(clashes, ".png") {
		t.Errorf("the clash list names a note it shouldn't:\n%s", clashes)
	}

	for rel := range testfixture.Files(t, v) {
		if !strings.HasPrefix(rel, StagingDir+"/"+src.Name+"/") && !strings.HasPrefix(rel, SlipBoxDir+"/") {
			t.Errorf("%s was written in the vault, outside the staging folder", rel)
		}
	}
	if got, want := len(testfixture.Files(t, filepath.Join(v, SlipBoxDir))), 5; got != want {
		t.Errorf("_Slip_Box holds %d files, want its %d", got, want)
	}
}

func TestRun_StagingFolderOutsideTheVault(t *testing.T) {
	folder, src := pragmatismFormatted(t)
	staging := filepath.Join(t.TempDir(), "elsewhere")
	if err := Run(src, folder, vault(t, "Inductive Logic.md"), staging); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(staging, ClashListFile))
	if err != nil {
		t.Fatal(err)
	}
	if want := "- `Inductive Logic.md` and [[_Slip_Box/Inductive Logic|_Slip_Box/Inductive Logic.md]]\n"; !strings.Contains(string(b), want) {
		t.Errorf("the clash list has no line %q, the staged note as code:\n%s", want, b)
	}
}

func TestRun_AnOccupiedStagingFolderStops(t *testing.T) {
	folder, src := pragmatismFormatted(t)
	v := vault(t)
	staging := DefaultStaging(v, src.Name)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(staging, "Earlier.md")
	if err := os.WriteFile(old, []byte("an earlier run's\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Run(src, folder, v, staging)
	var occupied *Occupied
	if !errors.As(err, &occupied) || occupied.Dir != staging || !reflect.DeepEqual(occupied.Entries, []string{"Earlier.md"}) {
		t.Fatalf("Run = %v, want an *Occupied naming Earlier.md in %s", err, staging)
	}
	if got := testfixture.Files(t, staging); !reflect.DeepEqual(got, map[string]string{"Earlier.md": "an earlier run's\n"}) {
		t.Errorf("the staging folder holds %v, want only what was there", got)
	}
	if folder.Complete(run.StageOutDir) {
		t.Errorf("step 3.2 is complete")
	}

	// Once the folder is cleared, the run stages out.
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	if err := Run(src, folder, v, staging); err != nil {
		t.Fatal(err)
	}
}

func TestRun_AStagingFolderInTheRunFolderStops(t *testing.T) {
	folder, src := pragmatismFormatted(t)
	staging := filepath.Join(folder.Dir, "out", "staged")
	if err := Run(src, folder, vault(t), staging); err == nil || !strings.Contains(err.Error(), "inside the run folder") {
		t.Fatalf("Run = %v, want an error: the staged notes would go when the run folder is deleted", err)
	}
	if _, err := os.Stat(filepath.Join(folder.Dir, "out")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a stopped Run made the staging folder (stat: %v)", err)
	}

	// A symbolic link to the run folder leads inside it too.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(folder.Dir, link); err != nil {
		t.Fatal(err)
	}
	if err := Check(folder, vault(t), filepath.Join(link, "staged")); err == nil {
		t.Errorf("Check of a staging folder in the run folder through a link = nil, want an error")
	}
}

func TestRun_ANoteNamedLikeTheClashListOrTheRunReportIsHeld(t *testing.T) {
	folder, src := pragmatismFormatted(t)
	seg := filepath.Join(folder.Dir, filepath.FromSlash(run.FormattedOrFallbackNotes), extra)
	if err := os.MkdirAll(seg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seg, "Name-Clashes.md"), []byte("# Name-Clashes\n\nA note of the source.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seg, "Run-Report.md"), []byte("# Run-Report\n\nA note of the source.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := vault(t)
	staging := DefaultStaging(v, src.Name)
	if err := Run(src, folder, v, staging); err != nil {
		t.Fatal(err)
	}
	got := testfixture.Files(t, staging)
	if !strings.HasPrefix(got[ClashListFile], "# Name clashes\n") {
		t.Errorf("staged %s is not the clash list:\n%s", ClashListFile, got[ClashListFile])
	}
	if got[extra+"/Name-Clashes.md"] != "# Name-Clashes\n\nA note of the source.\n" {
		t.Errorf("the note named like the clash list is not held in its segment folder: %q", got[extra+"/Name-Clashes.md"])
	}
	if want := "- [[Staging/" + src.Name + "/name-clashes|name-clashes.md]], [[Staging/" + src.Name + "/" + extra + "/Name-Clashes|" + extra + "/Name-Clashes.md]]\n"; !strings.Contains(got[ClashListFile], want) {
		t.Errorf("the clash list has no line %q:\n%s", want, got[ClashListFile])
	}
	// The run report's name is held the same way.
	if got[RunReportFile] != stagedReport(t) || got[extra+"/Run-Report.md"] == "" {
		t.Errorf("the run report or the note named like it is not where it belongs")
	}
	if want := "- [[Staging/" + src.Name + "/run-report|run-report.md]], [[Staging/" + src.Name + "/" + extra + "/Run-Report|" + extra + "/Run-Report.md]]\n"; !strings.Contains(got[ClashListFile], want) {
		t.Errorf("the clash list has no line %q:\n%s", want, got[ClashListFile])
	}
}

func TestWrite_RemovesWhatItMadeOnAFailure(t *testing.T) {
	// "a.md/b.md" can't be made, as a.md is a file by then.
	files := map[string]string{"a.md": "a\n", "a.md/b.md": "b\n", "0 A/c.md": "c\n"}

	// A staging folder write made is removed, and so are its parents it made.
	parent := filepath.Join(t.TempDir(), "Staging")
	if err := write(filepath.Join(parent, "src"), files); err == nil {
		t.Fatal("write = nil, want an error")
	}
	if _, err := os.Stat(parent); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("write left the folder it made (stat: %v)", err)
	}

	// An empty staging folder that was there stays, emptied again.
	dir := t.TempDir()
	if err := write(dir, files); err == nil {
		t.Fatal("write = nil, want an error")
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) > 0 {
		t.Errorf("write left %v in the staging folder (err %v)", entries, err)
	}
}

func TestRun_Errors(t *testing.T) {
	folder, src := pragmatismFormatted(t)
	if err := Run(src, folder, "", "staging"); !errors.Is(err, ErrNoVault) {
		t.Errorf("Run with no vault = %v, want ErrNoVault", err)
	}
	noSlipBox := t.TempDir()
	if err := Run(src, folder, noSlipBox, filepath.Join(noSlipBox, "staging")); err == nil || !strings.Contains(err.Error(), SlipBoxDir) {
		t.Errorf("Run on a vault with no _Slip_Box = %v, want an error naming it", err)
	}
	if _, err := os.Stat(filepath.Join(noSlipBox, "staging")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a failed Run made the staging folder (stat: %v)", err)
	}
	if err := os.Rename(filepath.Join(folder.Dir, run.FormatGateDir), filepath.Join(folder.Dir, run.FormatGateDir+run.PartialSuffix)); err != nil {
		t.Fatal(err)
	}
	if err := Run(src, folder, vault(t), "staging"); err == nil || !strings.Contains(err.Error(), "gate") {
		t.Errorf("Run before step 3.1's gate = %v, want an error", err)
	}
}

func TestPlace_NamesTwiceIgnoringCase(t *testing.T) {
	folders := []split.SegmentFolder{
		{Name: "0 A", Notes: notes("Kelvin.md", "Straße.md", "Ünique.md")},
		{Name: "1 B", Notes: notes("KELVIN.md", "straße.md", "Other.md")},
	}
	placed, list := Place("src", folders, []string{"_Slip_Box/ünique.md", "_Slip_Box/x/Unrelated.md"})
	var paths []string
	for _, p := range placed {
		paths = append(paths, p.Path())
	}
	if want := []string{"0 A/Kelvin.md", "0 A/Straße.md", "Ünique.md", "1 B/KELVIN.md", "1 B/straße.md", "Other.md"}; !reflect.DeepEqual(paths, want) {
		t.Errorf("paths = %q, want %q", paths, want)
	}
	if want := [][]string{{"0 A/Kelvin.md", "1 B/KELVIN.md"}, {"0 A/Straße.md", "1 B/straße.md"}}; !reflect.DeepEqual(list.Twice, want) {
		t.Errorf("Twice = %q, want %q", list.Twice, want)
	}
	if want := []SlipBoxClash{{Staged: "Ünique.md", SlipBox: []string{"_Slip_Box/ünique.md"}}}; !reflect.DeepEqual(list.SlipBox, want) {
		t.Errorf("SlipBox = %+v, want %+v", list.SlipBox, want)
	}
	if fold("\u212aelvin.md") != fold("kelvin.md") {
		t.Errorf("the Kelvin sign does not fold as k")
	}
}

func TestMarkdown_NoClashes(t *testing.T) {
	_, list := Place("src", []split.SegmentFolder{{Name: "0 A", Notes: notes("A.md")}}, nil)
	got := list.Markdown("Staging/src")
	if strings.Count(got, "None.\n") != 2 || strings.Contains(got, "\n- ") {
		t.Errorf("Markdown of no clashes:\n%s", got)
	}
}

func TestMarkdown_ALinkCantHoldAHash(t *testing.T) {
	if got, want := link("Staging/src/C# Notes.md", "C# Notes.md"), "`C# Notes.md`"; got != want {
		t.Errorf("link = %q, want %q", got, want)
	}
}

// notes are notes of the names given, each with an H1.
func notes(names ...string) []*note.Note {
	var out []*note.Note
	for _, n := range names {
		out = append(out, note.Parse(n, "# "+strings.TrimSuffix(n, ".md")+"\n"))
	}
	return out
}
