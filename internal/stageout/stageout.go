// Package stageout is step 3.2, stage out, which ends the run. It puts the notes step 3.1's gate
// passed on, hubs among them, into the per-source staging folder, ready to move into
// `_Slip_Box`, with the name-clash list and the run report beside them. The list names each note
// whose name is a `_Slip_Box` note's, ignoring case, and each name used twice
// within the run. Nothing is renamed and no file is overwritten: replacing,
// merging or linking is stage 4, the operator's own work.
//
// A note lands flat in the staging folder, unless its name is used twice in
// the run. Then every note with that name stays in its segment folder,
// `<n> <title>/`, inside the staging folder, so neither overwrites the other
// and no one copy is favoured. A note named like the clash list or the run
// report is held the same way, and listed as a name used twice.
//
// The tool writes only to the staging folder, and reads the names of the
// `.md` files under `_Slip_Box` (ADR 0001 §6). Case is ignored by folding
// each name's letters as Unicode simple case folding does. Two names that
// differ only in their Unicode normalisation, as macOS can write them, are
// not taken for the same name.
package stageout

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
)

// ClashListFile is the name-clash list, in the staging folder beside the
// notes. It is a note itself, so its links can be followed in the vault, and
// it stays behind when the notes move into `_Slip_Box`.
const ClashListFile = "name-clashes.md"

// RunReportFile is the run report's copy in the staging folder, beside the
// clash list (package runreport). It stays behind when the notes move, as
// the clash list does.
const RunReportFile = "run-report.md"

// SlipBoxDir is the vault's slip box, whose note names the clash list checks.
const SlipBoxDir = "_Slip_Box"

// StagingDir is the vault's folder of per-source staging folders.
const StagingDir = "Staging"

// DefaultStaging is the staging folder of the source named sourceName in
// vault when none is given: `<vault>/Staging/<source name>/`.
func DefaultStaging(vault, sourceName string) string {
	return filepath.Join(vault, StagingDir, sourceName)
}

// Placed is a formatted note and where it lands in the staging folder.
type Placed struct {
	// Folder is the note's segment folder, `<n> <title>`, and Name its
	// file name.
	Folder, Name string
	// Held is true of a note whose name is used twice in the run, which
	// stays in its segment folder.
	Held bool
	// Text is the note as step 3.1's gate passed it on.
	Text string
}

// Path is the note's path in the staging folder, with forward slashes.
func (p Placed) Path() string {
	if p.Held {
		return p.Folder + "/" + p.Name
	}
	return p.Name
}

// List is the name-clash list.
type List struct {
	// SourceName is the staged source's name.
	SourceName string
	// SlipBox are the staged notes named like a `_Slip_Box` note, in the
	// order the notes are placed.
	SlipBox []SlipBoxClash
	// Twice are the names used twice in the run, each the staged paths of
	// its notes, ordered by each name's first note. A note named like the
	// clash list or the run report is listed here, after that file's name.
	Twice [][]string
}

// SlipBoxClash is a staged note named like one or more `_Slip_Box` notes.
type SlipBoxClash struct {
	// Staged is the note's path in the staging folder, and SlipBox the paths
	// of the `_Slip_Box` notes of its name, from the vault, in byte order.
	Staged  string
	SlipBox []string
}

// Place places the formatted notes in folders, in their order, and lists the
// clashes with the `_Slip_Box` notes slipBox, each a path from the vault
// such as `_Slip_Box/Arches.md`.
func Place(sourceName string, folders []split.SegmentFolder, slipBox []string) ([]Placed, *List) {
	byName := map[string][]int{}
	var placed []Placed
	for _, f := range folders {
		for _, n := range f.Notes {
			key := fold(n.Name)
			byName[key] = append(byName[key], len(placed))
			placed = append(placed, Placed{Folder: f.Name, Name: n.Name, Text: n.Render()})
		}
	}
	existing := map[string][]string{}
	for _, p := range slipBox {
		key := fold(path.Base(p))
		existing[key] = append(existing[key], p)
	}
	list := &List{SourceName: sourceName}
	reserved := map[string]string{fold(ClashListFile): ClashListFile, fold(RunReportFile): RunReportFile}
	for i := range placed {
		key := fold(placed[i].Name)
		_, held := reserved[key]
		placed[i].Held = len(byName[key]) > 1 || held
	}
	for i, p := range placed {
		key := fold(p.Name)
		if same := byName[key]; p.Held && same[0] == i {
			var paths []string
			if r, ok := reserved[key]; ok {
				paths = append(paths, r)
			}
			for _, j := range same {
				paths = append(paths, placed[j].Path())
			}
			list.Twice = append(list.Twice, paths)
		}
		if ex := existing[key]; len(ex) > 0 {
			list.SlipBox = append(list.SlipBox, SlipBoxClash{Staged: p.Path(), SlipBox: slices.Sorted(slices.Values(ex))})
		}
	}
	return placed, list
}

// fold is s with each letter case-folded, so that two names that differ only
// in case fold the same: each rune becomes the least rune of its simple case
// folding orbit.
func fold(s string) string {
	return strings.Map(func(r rune) rune {
		least := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			least = min(least, f)
		}
		return least
	}, s)
}

// Markdown renders the list as the note ClashListFile. staging is the
// staging folder's path from the vault, with forward slashes, or "" if it is
// outside the vault. A path is a link where the vault can resolve it, and
// otherwise code.
func (l *List) Markdown(staging string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Name clashes\n\nThe notes of %s, staged by s2n. Nothing was renamed or overwritten: replacing, merging or linking each clash is stage 4's work.\n", l.SourceName)
	staged := func(p string) string {
		if staging == "" {
			return code(p)
		}
		return link(staging+"/"+p, p)
	}
	b.WriteString("\n## Named like a _Slip_Box note\n\nIgnoring case.\n\n")
	if len(l.SlipBox) == 0 {
		b.WriteString("None.\n")
	}
	for _, c := range l.SlipBox {
		var ex []string
		for _, p := range c.SlipBox {
			ex = append(ex, link(p, p))
		}
		fmt.Fprintf(&b, "- %s and %s\n", staged(c.Staged), strings.Join(ex, ", "))
	}
	b.WriteString("\n## Named twice in this run\n\nIgnoring case. Each stays in its segment folder, so neither overwrites the other.\n\n")
	if len(l.Twice) == 0 {
		b.WriteString("None.\n")
	}
	for _, paths := range l.Twice {
		var ps []string
		for _, p := range paths {
			ps = append(ps, staged(p))
		}
		fmt.Fprintf(&b, "- %s\n", strings.Join(ps, ", "))
	}
	return b.String()
}

// link is a wikilink to the note at p, a path from the vault, shown as
// shown, or shown as code if p holds a character a wikilink can't.
func link(p, shown string) string {
	if strings.ContainsAny(p+shown, "#|^[]`") {
		return code(shown)
	}
	return "[[" + strings.TrimSuffix(p, ".md") + "|" + shown + "]]"
}

func code(s string) string {
	if strings.Contains(s, "`") {
		return "``" + s + "``"
	}
	return "`" + s + "`"
}

// ReadSlipBox gives the path from vault of each `.md` file under vault's
// `_Slip_Box`, in byte order. A file or folder whose name starts with a dot
// is skipped.
func ReadSlipBox(vault string) ([]string, error) {
	root, err := filepath.EvalSymlinks(filepath.Join(vault, SlipBoxDir))
	if err != nil {
		return nil, err
	}
	var out []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, SlipBoxDir+"/"+filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

// ErrNoVault is Run's error when it is given no vault.
var ErrNoVault = errors.New("stage out: no vault is given, so there is no staging folder or _Slip_Box")

// Occupied is the error of a staging folder that already holds something,
// which stops the run, so that no file in it is overwritten.
type Occupied struct {
	// Dir is the staging folder, and Entries the names in it.
	Dir     string
	Entries []string
}

func (e *Occupied) Error() string {
	return fmt.Sprintf("stage out: the staging folder %s already holds %s", e.Dir, strings.Join(e.Entries, ", "))
}

// Check says whether Run could stage the notes of the run in folder into
// staging, against the `_Slip_Box` of vault, before any step runs: it gives
// ErrNoVault, an *Occupied, or the error reading `_Slip_Box` or the staging
// folder, which Run would give too.
func Check(folder *run.Folder, vault, staging string) error {
	_, err := check(folder, vault, staging)
	return err
}

// check is Check, and gives the `_Slip_Box` notes, as ReadSlipBox does.
func check(folder *run.Folder, vault, staging string) ([]string, error) {
	if vault == "" {
		return nil, ErrNoVault
	}
	if inside(folder.Dir, staging) {
		return nil, fmt.Errorf("stage out: the staging folder %s is inside the run folder %s, which is deleted once the run succeeds", staging, folder.Dir)
	}
	slipBox, err := ReadSlipBox(vault)
	if err != nil {
		return nil, fmt.Errorf("stage out: %w", err)
	}
	if entries, err := os.ReadDir(staging); err == nil && len(entries) > 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return nil, &Occupied{Dir: staging, Entries: names}
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("stage out: %w", err)
	}
	return slipBox, nil
}

// Run stages the notes step 3.1's gate in folder passed on, each segment's
// formatted notes or its fallback, into the staging folder staging, with the
// clash list, against the `_Slip_Box` of vault. It runs only once step 3.1's
// gate is complete. The staging folder must
// be empty or not exist, or Run returns an *Occupied, and must be outside
// the run folder, which is deleted once the run succeeds. If writing a file
// fails, Run removes what it wrote. It writes the clash list into the step's
// directory in folder too. The run report's staged copy is copied from its
// step's directory into the staging folder as RunReportFile.
func Run(src *source.Source, folder *run.Folder, vault, staging string) error {
	if !folder.Complete(run.FormatGateDir) {
		return errors.New("stage out: step 3.1's gate is not complete, so there are no notes to stage yet")
	}
	slipBox, err := check(folder, vault, staging)
	if err != nil {
		return err
	}
	folders, err := split.ReadFolders(filepath.Join(folder.Dir, filepath.FromSlash(run.FormattedOrFallbackNotes)))
	if err != nil {
		return fmt.Errorf("stage out: %w", err)
	}
	report, err := os.ReadFile(filepath.Join(folder.Dir, run.RunReportDir, run.StagedReportFile))
	if err != nil {
		return fmt.Errorf("stage out: %w", err)
	}
	step, err := folder.Begin(run.StageOutDir)
	if err != nil {
		return err
	}
	placed, list := Place(src.Name, folders, slipBox)
	clashes := list.Markdown(fromVault(vault, staging))
	if err := step.WriteFile(ClashListFile, []byte(clashes)); err != nil {
		return err
	}
	files := map[string]string{ClashListFile: clashes, RunReportFile: string(report)}
	for _, p := range placed {
		files[p.Path()] = p.Text
	}
	if err := write(staging, files); err != nil {
		return fmt.Errorf("stage out: %w", err)
	}
	return step.Commit()
}

// inside says whether dir is root or inside it, once each path's symbolic
// links are followed as far as it exists.
func inside(root, dir string) bool {
	return fromVault(resolved(root), resolved(dir)) != ""
}

// resolved is p made absolute, with the symbolic links of its longest part
// that exists followed.
func resolved(p string) string {
	p, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	var rest []string
	for {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(append([]string{r}, rest...)...)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(append([]string{p}, rest...)...)
		}
		rest = append([]string{filepath.Base(p)}, rest...)
		p = parent
	}
}

// fromVault is the path of dir from vault, with forward slashes, or "" if
// dir is outside vault.
func fromVault(vault, dir string) string {
	rel, err := filepath.Rel(vault, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// write writes files, each keyed by its slash-separated path in dir, into
// dir, which is empty or doesn't exist. It creates each file new, so it
// overwrites none. If one fails, it removes what it made.
func write(dir string, files map[string]string) (err error) {
	made := firstMissing(dir)
	var wrote []string
	defer func() {
		if err == nil {
			return
		}
		if made != "" {
			os.RemoveAll(made)
			return
		}
		for _, p := range wrote {
			os.RemoveAll(p)
		}
	}()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if parent := filepath.Dir(p); parent != filepath.Clean(dir) {
			// A held note's segment folder, which the empty dir didn't have.
			if err := os.Mkdir(parent, 0o755); err == nil {
				wrote = append(wrote, parent)
			} else if !errors.Is(err, fs.ErrExist) {
				return err
			}
		}
		fh, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		wrote = append(wrote, p)
		if _, err := fh.WriteString(files[rel]); err != nil {
			fh.Close()
			return err
		}
		if err := fh.Close(); err != nil {
			return err
		}
	}
	return nil
}

// firstMissing is the outermost of dir and its parents that doesn't exist,
// or "" if dir exists.
func firstMissing(dir string) string {
	missing := ""
	for p := filepath.Clean(dir); ; p = filepath.Dir(p) {
		if _, err := os.Lstat(p); err == nil {
			return missing
		}
		missing = p
		if filepath.Dir(p) == p {
			return missing
		}
	}
}
