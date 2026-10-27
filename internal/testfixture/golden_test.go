package testfixture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withUpdate sets -update to on for the rest of t.
func withUpdate(t *testing.T, on bool) {
	was := *update
	*update = on
	t.Cleanup(func() { *update = was })
}

func TestGolden_PassesOnEqualContent(t *testing.T) {
	withUpdate(t, false)
	path := filepath.Join(t.TempDir(), "report.md")
	WriteFiles(t, filepath.Dir(path), map[string]string{"report.md": "line 1\nline 2\n"})
	msgs := (&recorder{TB: t}).run(func(tb testing.TB) { golden(tb, path, "line 1\nline 2\n") })
	if len(msgs) != 0 {
		t.Errorf("the test was told %q, want nothing", msgs)
	}
}

func TestGolden_FailsOnAMismatch(t *testing.T) {
	withUpdate(t, false)
	for _, tc := range []struct {
		name, got, want, says string
	}{
		{"a line differs", "line 1\nline 2\n", "line 1\nline two\n", "line 2:\n\tgot:  \"line 2\\n\"\n\twant: \"line two\\n\""},
		{"got is shorter", "line 1\n", "line 1\nline 2\n", "line 2:\n\tgot:  (end of text)\n\twant: \"line 2\\n\""},
		{"got is longer", "line 1\nline 2\n", "line 1\n", "line 2:\n\tgot:  \"line 2\\n\"\n\twant: (end of text)"},
		{"only the last newline differs", "line 1", "line 1\n", "line 1:\n\tgot:  \"line 1\"\n\twant: \"line 1\\n\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "report.md")
			WriteFiles(t, filepath.Dir(path), map[string]string{"report.md": tc.want})
			msgs := (&recorder{TB: t}).run(func(tb testing.TB) { golden(tb, path, tc.got) })
			if len(msgs) != 1 || !strings.Contains(msgs[0], path) || !strings.Contains(msgs[0], "-update") || !strings.Contains(msgs[0], tc.says) {
				t.Errorf("the test was told %q, want one failure naming %s, saying %q and suggesting -update", msgs, path, tc.says)
			}
			if b, _ := os.ReadFile(path); string(b) != tc.want {
				t.Error("the golden file was changed without -update")
			}
		})
	}
}

func TestGolden_FailsOnAMissingFile(t *testing.T) {
	withUpdate(t, false)
	path := filepath.Join(t.TempDir(), "missing.md")
	msgs := (&recorder{TB: t}).run(func(tb testing.TB) { golden(tb, path, "anything") })
	if len(msgs) != 1 || !strings.Contains(msgs[0], path) || !strings.Contains(msgs[0], "-update") {
		t.Errorf("the test was told %q, want one failure naming %s and suggesting -update", msgs, path)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("the golden file was written without -update")
	}
}

func TestGolden_UpdateWritesTheFileAndItsDirectories(t *testing.T) {
	withUpdate(t, true)
	path := filepath.Join(t.TempDir(), "golden", "stage-out", "report.md")
	msgs := (&recorder{TB: t}).run(func(tb testing.TB) { golden(tb, path, "new\n") })
	if len(msgs) != 0 {
		t.Errorf("the test was told %q, want nothing", msgs)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "new\n" {
		t.Errorf("golden file holds %q, %v; want %q", b, err, "new\n")
	}
	// An existing file is rewritten.
	msgs = (&recorder{TB: t}).run(func(tb testing.TB) { golden(tb, path, "newer\n") })
	if b, _ := os.ReadFile(path); len(msgs) != 0 || string(b) != "newer\n" {
		t.Errorf("after a second update the file holds %q and the test was told %q", b, msgs)
	}
}

func TestGolden_ResolvesUnderThePackagesTestdata(t *testing.T) {
	withUpdate(t, true)
	t.Chdir(t.TempDir())
	Golden(t, "golden/report.md", "x\n")
	if b, err := os.ReadFile(filepath.Join("testdata", "golden", "report.md")); err != nil || string(b) != "x\n" {
		t.Errorf("testdata/golden/report.md holds %q, %v; want %q", b, err, "x\n")
	}
}
