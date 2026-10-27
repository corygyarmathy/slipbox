package testfixture

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// update is the -update flag: `go test ./... -update` rewrites each golden
// file with what its test got. It is declared here, once, so every package
// whose tests import testfixture takes it.
var update = flag.Bool("update", false, "rewrite golden files under testdata/ with what the tests got")

// Golden checks got against the golden file name, a slash-separated path
// under the testdata/ of the package whose test calls it, as `go test` runs
// a package's tests in its own directory. With -update it writes got to the
// file instead, making the directories it needs.
func Golden(t testing.TB, name, got string) {
	t.Helper()
	golden(t, filepath.Join("testdata", filepath.FromSlash(name)), got)
}

// golden is Golden on the file at path.
func golden(t testing.TB, path, got string) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("testfixture: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("testfixture: %v", err)
		}
		return
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s: no golden file; run go test with -update to write it", path)
		return
	}
	if err != nil {
		t.Fatalf("testfixture: %v", err)
	}
	if want := string(b); got != want {
		t.Errorf("%s: got differs from the golden file, %s; run go test with -update to rewrite it, and review the diff",
			path, firstDiff(got, want))
	}
}

// firstDiff says where got and want, which differ, first part: the line,
// and the two lines there. Each splits into lines that end in a newline and
// a last one that doesn't, possibly empty, so they part before either runs
// out, and an empty line there is where its text ended.
func firstDiff(got, want string) string {
	g, w := strings.SplitAfter(got, "\n"), strings.SplitAfter(want, "\n")
	i := 0
	for g[i] == w[i] {
		i++
	}
	return fmt.Sprintf("first at line %d:\n\tgot:  %s\n\twant: %s", i+1, quoteLine(g[i]), quoteLine(w[i]))
}

// quoteLine quotes a line for firstDiff, or says the text ended there.
func quoteLine(s string) string {
	if s == "" {
		return "(end of text)"
	}
	return fmt.Sprintf("%q", s)
}
