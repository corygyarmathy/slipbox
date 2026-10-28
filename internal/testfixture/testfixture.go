// Package testfixture is the one way tests open fixtures
// ([ADR 0002] §8): an invented chapter with its stage 1 notes and split
// (lamp.go), golden files, and any other file in the repository, such as a
// brief, through RepoFile.
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
	"testing"
)

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
