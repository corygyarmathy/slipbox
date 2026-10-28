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

// stagedReport is the run report lampFormatted's run wrote for step 3.2 to
// stage.
const stagedReport = "# Run report\n\nAn invented report, staged as it is.\n"

// lampFormatted is a run folder for the invented Lamp chapter whose step
// 3.1 gate is complete, having passed on LampStripped's notes, as a format
// that changed nothing would, and the notes added, by slash-separated path
// under the gate's notes; and whose run report wrote stagedReport. It gives
// the notes the gate passed on, by the same paths, and the run's source.
func lampFormatted(t *testing.T, added map[string]string) (*run.Folder, *source.Source, map[string]string) {
	t.Helper()
	src, folder := testfixture.LampRun(t)
	notes := testfixture.LampStripped(t)
	maps.Copy(notes, added)
	testfixture.WriteFiles(t, filepath.Join(folder.Dir, filepath.FromSlash(run.FormattedOrFallbackNotes)), notes)
	testfixture.WriteFiles(t, filepath.Join(folder.Dir, run.RunReportDir), map[string]string{run.StagedReportFile: stagedReport})
	return folder, src, notes
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

// staged is the notes of a run folder's gate, by slash-separated path under
// it, as step 3.2 should stage them: each flat by its file name, unless
// held, whose paths stay as they are.
func staged(t *testing.T, notes map[string]string, held ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for rel, text := range notes {
		if !slices.Contains(held, rel) {
			rel = filepath.Base(rel)
		}
		if _, ok := out[rel]; ok {
			t.Fatalf("two notes stage to %s; hold one", rel)
		}
		out[rel] = text
	}
	return out
}

// Given an empty vault, step 3.2 stages every note the gate passed on flat,
// byte for byte, beside the run report and a name-clash list with no clash.
func TestRun(t *testing.T) {
	folder, src, notes := lampFormatted(t, nil)
	v := vault(t)
	staging := DefaultStaging(v, src.Name)
	if err := Run(src, folder, v, staging); err != nil {
		t.Fatal(err)
	}
	if !folder.Complete(run.StageOutDir) {
		t.Errorf("step 3.2 is not complete")
	}
	got := testfixture.Files(t, staging)
	testfixture.Golden(t, "lamp/name-clashes.md", got[ClashListFile])
	if got[RunReportFile] != stagedReport {
		t.Errorf("staged %s = %q, want the run's staged report", RunReportFile, got[RunReportFile])
	}
	delete(got, ClashListFile)
	delete(got, RunReportFile)
	if want := staged(t, notes); !reflect.DeepEqual(got, want) {
		t.Errorf("staged notes %q, want the gate's %q, each flat as it came", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
	}
}

const (
	keepersDir = "0 Keepers of the Lamp"
	lensDir    = "2 The Lens"
	extraDir   = "4 Extra"
)

// Notes named twice in the run, ignoring case, stay in their segment
// folders, and the clash list names them, and each staged note named like a
// _Slip_Box note. Here a folder no segment of the source claims adds two
// notes named like segment 2's atomic note Rings of Prisms and segment 0's
// hub. Of the _Slip_Box notes, one is in a folder and in another case, and
// one is named like a staged note but hidden in a dot folder.
func TestRun_ListsNameClashes(t *testing.T) {
	added := map[string]string{
		extraDir + "/Rings of Prisms.md":     "# Rings of Prisms\n\nA note named as another segment's is.\n",
		extraDir + "/KEEPERS OF THE LAMP.md": "# KEEPERS OF THE LAMP\n\nA note named as a hub is, in another case.\n",
	}
	folder, src, notes := lampFormatted(t, added)
	v := vault(t, "rings of prisms.md", "Sub/THE LENS MAKES A BEAM.md", "Unrelated.md", ".hidden/Storms.md", "Storms.png")
	staging := DefaultStaging(v, src.Name)
	if err := Run(src, folder, v, staging); err != nil {
		t.Fatal(err)
	}

	got := testfixture.Files(t, staging)
	clashes := got[ClashListFile]
	testfixture.Golden(t, "clashes/name-clashes.md", clashes)
	delete(got, ClashListFile)
	delete(got, RunReportFile)
	want := staged(t, notes,
		keepersDir+"/Keepers of the Lamp.md", extraDir+"/KEEPERS OF THE LAMP.md",
		lensDir+"/Rings of Prisms.md", extraDir+"/Rings of Prisms.md")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("staged %q, want %q: every note flat as it came but the four named twice, each in its segment folder", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
	}
	if b, err := os.ReadFile(filepath.Join(folder.Dir, run.StageOutDir, ClashListFile)); err != nil || string(b) != clashes {
		t.Errorf("the step's directory has no copy of the clash list (err %v)", err)
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

// ReadSlipBox lists the .md files under _Slip_Box, nested ones too, and
// skips a file or folder whose name starts with a dot. These names come in
// the same order whether sorted by byte or walked, so this doesn't check the
// byte order ReadSlipBox's doc promises.
func TestReadSlipBox(t *testing.T) {
	v := vault(t, "b.md", "Sub/a.md", "A.md", "Storms.png", ".Hidden.md", ".hidden/Storms.md")
	if err := os.Mkdir(filepath.Join(v, SlipBoxDir, "Folder.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSlipBox(v)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"_Slip_Box/A.md", "_Slip_Box/Sub/a.md", "_Slip_Box/b.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ReadSlipBox = %q, want %q", got, want)
	}
}

func TestRun_StagingFolderOutsideTheVault(t *testing.T) {
	folder, src, _ := lampFormatted(t, nil)
	staging := filepath.Join(t.TempDir(), "elsewhere")
	if err := Run(src, folder, vault(t, "Rings of Prisms.md"), staging); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(staging, ClashListFile))
	if err != nil {
		t.Fatal(err)
	}
	if want := "- `Rings of Prisms.md` and [[_Slip_Box/Rings of Prisms|_Slip_Box/Rings of Prisms.md]]\n"; !strings.Contains(string(b), want) {
		t.Errorf("the clash list has no line %q, the staged note as code:\n%s", want, b)
	}
}

func TestRun_AnOccupiedStagingFolderStops(t *testing.T) {
	folder, src, _ := lampFormatted(t, nil)
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
	folder, src, _ := lampFormatted(t, nil)
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
	folder, src, _ := lampFormatted(t, map[string]string{
		extraDir + "/Name-Clashes.md": "# Name-Clashes\n\nA note of the source.\n",
		extraDir + "/Run-Report.md":   "# Run-Report\n\nA note of the source.\n",
	})
	v := vault(t)
	staging := DefaultStaging(v, src.Name)
	if err := Run(src, folder, v, staging); err != nil {
		t.Fatal(err)
	}
	got := testfixture.Files(t, staging)
	if !strings.HasPrefix(got[ClashListFile], "# Name clashes\n") {
		t.Errorf("staged %s is not the clash list:\n%s", ClashListFile, got[ClashListFile])
	}
	if got[extraDir+"/Name-Clashes.md"] != "# Name-Clashes\n\nA note of the source.\n" {
		t.Errorf("the note named like the clash list is not held in its segment folder: %q", got[extraDir+"/Name-Clashes.md"])
	}
	if want := "- [[Staging/" + src.Name + "/name-clashes|name-clashes.md]], [[Staging/" + src.Name + "/" + extraDir + "/Name-Clashes|" + extraDir + "/Name-Clashes.md]]\n"; !strings.Contains(got[ClashListFile], want) {
		t.Errorf("the clash list has no line %q:\n%s", want, got[ClashListFile])
	}
	// The run report's name is held the same way.
	if got[RunReportFile] != stagedReport || got[extraDir+"/Run-Report.md"] == "" {
		t.Errorf("the run report or the note named like it is not where it belongs")
	}
	if want := "- [[Staging/" + src.Name + "/run-report|run-report.md]], [[Staging/" + src.Name + "/" + extraDir + "/Run-Report|" + extraDir + "/Run-Report.md]]\n"; !strings.Contains(got[ClashListFile], want) {
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
	folder, src, _ := lampFormatted(t, nil)
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
	if err := Run(src, folder, vault(t), "staging"); err == nil || !strings.Contains(err.Error(), "gate is not complete") {
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
