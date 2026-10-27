// Package testfixture is the one way tests open fixtures
// ([ADR 0002] §8): the public-domain chapter (Pragmatism) in
// testdata/sources/ and its recorded run, the hand-written replies in
// testdata/trials/hand-written/, an invented chapter with its stage 1 notes
// and split (lamp.go), and golden files.
//
// It also holds the two helpers tests use to check replies as text (fake.go,
// golden.go):
//
//   - Fake is the provider a test reaches the seam through. The test
//     supplies a function that decides each reply, and asserts on the
//     requests the Fake records. Serve builds one from reply cases matched
//     on a piece of the user prompt, so a test doesn't depend on how the
//     whole prompt is laid out. Neither serves by call order: a step makes
//     its first segment's call alone and the rest in parallel, so the order
//     calls arrive in isn't fixed.
//   - Golden checks deterministic output against a golden file under the
//     calling package's testdata/. `go test ./internal/<pkg> -update`
//     rewrites that package's files instead, and their diff is reviewed in
//     the pull request. The -update flag is declared here, so only the
//     packages whose tests import testfixture take it: run it on those
//     packages, as `go test ./... -update` fails in the rest.
//
// [ADR 0002]: https://github.com/corygyarmathy/slipbox/blob/main/docs/adr/0002-one-note-model-run-folder-and-replies-tested-as-text.md
package testfixture

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
)

// Pragmatism is the public-domain chapter's name, William James's
// Pragmatism, Lecture II, the source file name without .md. Nothing in it or
// in its recorded run is copyrighted.
const Pragmatism = "Pragmatism - Lecture II - What Pragmatism Means"

// root is the repository root, found from this file's place in it.
func root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// RepoFile reads the file at rel, a slash-separated path from the
// repository root, such as a brief under internal/briefs/.
func RepoFile(t testing.TB, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root(), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("testfixture: %v", err)
	}
	return string(b)
}

// SourcePath is the path of the source named name (without .md).
func SourcePath(t testing.TB, name string) string {
	t.Helper()
	p := filepath.Join(root(), "testdata", "sources", name+".md")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("testfixture: %v", err)
	}
	return p
}

// TrialRun is the directory of a trial run copied whole, such as
// TrialRun(t, "pragmatism-first-run", "lecture-2"). PROVENANCE.md in the trial's
// directory says where it came from.
func TrialRun(t testing.TB, trial, runName string) string {
	t.Helper()
	p := filepath.Join(root(), "testdata", "trials", trial, runName)
	if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
		t.Fatalf("testfixture: no trial run %s/%s: %v", trial, runName, err)
	}
	return p
}

// PragmatismSource reads the Pragmatism chapter.
func PragmatismSource(t testing.TB) *source.Source {
	t.Helper()
	src, err := source.Read(SourcePath(t, Pragmatism))
	if err != nil {
		t.Fatalf("testfixture: %v", err)
	}
	return src
}

// PragmatismRun is the run folder of the one real run recorded on the
// Pragmatism chapter, copied whole. Its PROVENANCE.md gives the commit,
// briefs, models and effort levels it ran with: the defaults, so quote-once
// and edit-shaped repair replies.
func PragmatismRun(t testing.TB) string {
	t.Helper()
	return TrialRun(t, "pragmatism-first-run", "lecture-2")
}

// PragmatismStaged is the staging folder the Pragmatism run's step 3.2
// staged out into, copied whole: the notes, the name-clash list and the run
// report. The run was given an empty vault, so its name-clash list is empty.
func PragmatismStaged(t testing.TB) string {
	t.Helper()
	return TrialRun(t, "pragmatism-first-run", "staged")
}

// PragmatismFrame is the Pragmatism chapter's file as it is on disk, the
// frame a step's system prompt should hold. Tests compare with it rather than
// with the parsed source's Frame, so a fault in parsing still shows.
func PragmatismFrame(t testing.TB) string {
	t.Helper()
	return RepoFile(t, "testdata/sources/"+Pragmatism+".md")
}

// PragmatismFolder is a new run folder holding the Pragmatism chapter and the
// recorded run's step directories named by steps, copied whole, so a test of
// one step starts from the steps before it as the run left them.
func PragmatismFolder(t testing.TB, steps ...string) *run.Folder {
	t.Helper()
	folder, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatalf("testfixture: %v", err)
	}
	src := PragmatismSource(t)
	if err := folder.SaveSource(src.Name, []byte(src.Frame)); err != nil {
		t.Fatalf("testfixture: %v", err)
	}
	recorded := PragmatismRun(t)
	for _, step := range steps {
		if err := os.CopyFS(filepath.Join(folder.Dir, step), os.DirFS(filepath.Join(recorded, step))); err != nil {
			t.Fatalf("testfixture: %v", err)
		}
	}
	return folder
}

// SameRequests checks that the step directory got holds the request records,
// `requests/<n>.sha256` under sub, that the recorded step directory want
// does: so the step made, for each segment, the request the recorded run
// made, its model, effort level, system prompt and user prompt alike. The
// records are hashes, so a difference can't say which part changed; it is
// reported once, with the records that differ, since a change to a brief,
// model or effort level changes every segment's request and needs the run
// recorded again rather than the test changed.
func SameRequests(t testing.TB, got, want, sub string) {
	t.Helper()
	g := Files(t, filepath.Join(got, sub, "requests"))
	w := Files(t, filepath.Join(want, sub, "requests"))
	if len(w) == 0 {
		t.Fatalf("testfixture: %s records no requests", filepath.Join(want, sub))
	}
	var differ, extra []string
	for name, key := range w {
		if g[name] != key {
			differ = append(differ, name)
		}
	}
	for name := range g {
		if _, ok := w[name]; !ok {
			extra = append(extra, name)
		}
	}
	if len(differ) == 0 && len(extra) == 0 {
		return
	}
	slices.Sort(differ)
	slices.Sort(extra)
	t.Errorf("%s: the step's requests are not the recorded run's (differ: %v; not recorded: %v). "+
		"If a brief, model or effort level changed, the recorded replies no longer answer these requests: "+
		"record testdata/trials/pragmatism-first-run again, as its PROVENANCE.md's \"How it was run\" gives it",
		filepath.Join(filepath.Base(want), sub), differ, extra)
}

// RawReplies reads a trial run's `raw/<n>.txt` replies for segments 0 to
// segments-1.
func RawReplies(t testing.TB, runDir string, segments int) []string {
	t.Helper()
	out := make([]string, segments)
	for n := range segments {
		b, err := os.ReadFile(filepath.Join(runDir, "raw", strconv.Itoa(n)+".txt"))
		if err != nil {
			t.Fatalf("testfixture: %v", err)
		}
		out[n] = string(b)
	}
	return out
}

// Replay returns a replay provider serving replies[n] for the user prompt
// prompts[n]. A prompt given more than once is served its replies in order.
func Replay(t testing.TB, prompts, replies []string) *provider.Replay {
	t.Helper()
	if len(prompts) != len(replies) {
		t.Fatalf("testfixture: %d prompts for %d replies", len(prompts), len(replies))
	}
	m := make(map[string][]provider.Reply, len(prompts))
	for i, p := range prompts {
		m[p] = append(m[p], provider.Reply{Text: replies[i]})
	}
	return provider.NewReplay(m)
}

// Files reads every regular file under dir into a map keyed by its path
// relative to dir, with forward slashes.
func Files(t testing.TB, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("testfixture: %v", err)
	}
	return out
}
