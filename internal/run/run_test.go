package run_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/corygyarmathy/slipbox/internal/run"
)

func TestStep_PartialUntilCommit(t *testing.T) {
	f, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	// A stopped attempt's output is dropped by the next Begin.
	stale := filepath.Join(f.Dir, "s.partial")
	if err := os.MkdirAll(filepath.Join(stale, "old"), 0o755); err != nil {
		t.Fatal(err)
	}
	step, err := f.Begin("s")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stale, "old")); !errors.Is(err, os.ErrNotExist) {
		t.Error("Begin kept the stale partial directory's contents")
	}
	if err := step.WriteFile("a/b.txt", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.Dir, "s")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the step looks complete before Commit")
	}
	if err := step.Commit(); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(f.Dir, "s", "a", "b.txt")); err != nil || string(b) != "x" {
		t.Errorf("after Commit: %q, %v", b, err)
	}
	if _, err := f.Begin("s"); err == nil {
		t.Error("Begin restarted a complete step")
	}
}

// A stopped attempt's usage log survives the next Begin, and the new
// attempt appends to it: those calls were spent.
func TestBegin_CarriesUsageOver(t *testing.T) {
	f, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.Begin("s")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.AppendFile(run.UsageFile, []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	second, err := f.Begin("s")
	if err != nil {
		t.Fatal(err)
	}
	if err := second.AppendFile(run.UsageFile, []byte("second\n")); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(second.Dir, run.UsageFile)); err != nil || string(b) != "first\nsecond\n" {
		t.Errorf("usage log after a retry: %q, %v", b, err)
	}
}

// Begin keeps what a resume reuses from a stopped attempt: the usage log, the
// raw replies and request records, in the step's directory, a numbered run's
// and a partial step's within it, and drops the rest.
func TestBegin_KeepsTheFinishedReplies(t *testing.T) {
	f, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	keep := []string{
		"usage.jsonl",
		"raw/0.txt", "requests/0.sha256",
		"run-1/raw/0.txt", "run-1/requests/0.sha256",
		"inner.partial/raw/0.txt", "inner.partial/requests/0.sha256", "inner.partial/usage.jsonl",
	}
	drop := []string{
		"stage-1/0 A.md", "reports/0.md", "dropped.json", "run-1/other.txt",
		"inner.partial/flags/0.json", "done/raw/0.txt", "done/requests/0.sha256",
	}
	partial := filepath.Join(f.Dir, "s"+run.PartialSuffix)
	for _, rel := range append(slices.Clone(keep), drop...) {
		p := filepath.Join(partial, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	step, err := f.Begin("s")
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range keep {
		if b, err := os.ReadFile(filepath.Join(step.Dir, filepath.FromSlash(rel))); err != nil || string(b) != rel {
			t.Errorf("Begin dropped %s: %q, %v", rel, b, err)
		}
	}
	for _, rel := range drop {
		if _, err := os.Stat(filepath.Join(step.Dir, filepath.FromSlash(rel))); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Begin kept %s (stat err %v)", rel, err)
		}
	}
}

func TestFolder_SaveSource(t *testing.T) {
	f, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.SourcePath(); err == nil {
		t.Error("SourcePath succeeded before SaveSource")
	}
	if err := f.SaveSource("Src", []byte("frame")); err != nil {
		t.Fatal(err)
	}
	p, err := f.SourcePath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(f.Dir, run.SourceDir, "Src.md"); p != want {
		t.Errorf("SourcePath = %q, want %q", p, want)
	}
	if b, err := os.ReadFile(p); err != nil || string(b) != "frame" {
		t.Errorf("saved source = %q, %v", b, err)
	}
	if err := f.SaveSource("Other", []byte("x")); err == nil {
		t.Error("SaveSource replaced the run's source")
	}
}

func TestNewIn(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 16, 9, 5, 7, 0, time.FixedZone("AEST", 10*3600))
	f, err := run.NewIn(root, "Src", now)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "Src", "20261015T230507Z"); f.Dir != want {
		t.Errorf("Dir = %q, want %q", f.Dir, want)
	}
	if _, err := run.NewIn(root, "Src", now); err == nil {
		t.Error("NewIn reused an existing run folder")
	}
}

// The state directory is s2n's in $XDG_STATE_HOME, or in ~/.local/state
// when the variable is unset or not an absolute path.
func TestStateDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	if got, err := run.StateDir(); err != nil || got != filepath.Join("/state", "s2n") {
		t.Errorf("StateDir() = %q, %v", got, err)
	}
	t.Setenv("HOME", "/home/u")
	for _, state := range []string{"", "relative"} {
		t.Setenv("XDG_STATE_HOME", state)
		if got, err := run.StateDir(); err != nil || got != filepath.Join("/home/u", ".local", "state", "s2n") {
			t.Errorf("XDG_STATE_HOME=%q: StateDir() = %q, %v", state, got, err)
		}
	}
}

// A run folder is logged once it holds the line it appended to the run log.
func TestFolder_Logged(t *testing.T) {
	f, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Logged() {
		t.Error("a new run folder is logged")
	}
	if err := f.WriteLogged([]byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if !f.Logged() {
		t.Errorf("a run folder holding %s is not logged", run.LoggedFile)
	}
}

// A new run folder holds no options, which ReadOptions says as
// fs.ErrNotExist, and once written they read back as written.
func TestFolder_Options(t *testing.T) {
	f, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.ReadOptions(); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReadOptions on a new run folder: %v, want fs.ErrNotExist", err)
	}
	if err := f.WriteOptions([]byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if b, err := f.ReadOptions(); err != nil || string(b) != "{}\n" {
		t.Errorf("ReadOptions = %q, %v, want what was written", b, err)
	}
	if got, want := f.OptionsPath(), filepath.Join(f.Dir, run.OptionsFile); got != want {
		t.Errorf("OptionsPath() = %q, want %q", got, want)
	}
}

func TestDefaultRoot(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	if got, err := run.DefaultRoot(); err != nil || got != filepath.Join("/state", "s2n", "runs") {
		t.Errorf("DefaultRoot() = %q, %v", got, err)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/u")
	if got, err := run.DefaultRoot(); err != nil || got != filepath.Join("/home/u", ".local", "state", "s2n", "runs") {
		t.Errorf("DefaultRoot() = %q, %v", got, err)
	}
}
