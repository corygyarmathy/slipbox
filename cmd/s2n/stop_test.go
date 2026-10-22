package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// pragmatismAtSplit is a run folder of the Pragmatism run with every step
// before step 2.1's gates complete, copied whole: its split passes the
// gates.
func pragmatismAtSplit(t *testing.T) string {
	t.Helper()
	return testfixture.PragmatismFolder(t, stepDirsBefore(run.SplitGatesDir)...).Dir
}

// resumeStop runs `s2n resume -stop-after stopAfter` on runDir with no
// model call, and gives its exit code and stderr.
func resumeStop(t *testing.T, runDir, stopAfter string) (int, string) {
	t.Helper()
	p := testfixture.Replay(t, nil, nil)
	var stdout, stderr bytes.Buffer
	code := runCommand(context.Background(), []string{"resume", "-stop-after", stopAfter, runDir}, &stdout, &stderr, p)
	if len(p.Calls()) != 0 {
		t.Errorf("%d model calls, want none", len(p.Calls()))
	}
	return code, stderr.String()
}

// wantStop checks that the resume stopped on a gate, and that its stderr
// says each of want, the resume command, and where STOPPED.md is, which
// holds the same summary. No gate stop says the bare "Fix it".
func wantStop(t *testing.T, runDir string, code int, stderr string, want ...string) {
	t.Helper()
	if code != ExitGate {
		t.Fatalf("exit %d, want %d: %s", code, ExitGate, stderr)
	}
	want = append(want, "\nThen run s2n resume "+runDir+".\n")
	for _, w := range want {
		if !strings.Contains(stderr, w) {
			t.Errorf("stderr does not say %q:\n%s", w, stderr)
		}
	}
	if strings.Contains(stderr, "Fix it") {
		t.Errorf("stderr says the bare \"Fix it\":\n%s", stderr)
	}
	wantStopped(t, runDir, stderr)
}

// wantStopped checks that stderr ends by saying where STOPPED.md is, and
// that it holds the summary stderr gives before that.
func wantStopped(t *testing.T, runDir, stderr string) {
	t.Helper()
	stopped := filepath.Join(runDir, run.StoppedFile)
	pointer := "This summary is also in " + stopped + ".\n"
	summary, ok := strings.CutSuffix(stderr, pointer)
	if !ok {
		t.Errorf("stderr does not end %q:\n%s", pointer, stderr)
	}
	if b, err := os.ReadFile(stopped); err != nil || string(b) != summary {
		t.Errorf("%s (err %v) is not the terminal's summary:\n%s", run.StoppedFile, err, b)
	}
}

// A segment that fails the split check falls back to its final stage 1
// notes, and one of them with no H1 can't be stripped, so with nothing left
// to fall back to, the fallback's strip check stops the run. It names the
// stage 1 note, what's wrong and what to do. STOPPED.md holds the same
// summary. A resume that stops again writes it anew, and once the stage 1
// note is fixed, a resume falls back and removes it.
func TestStop_FallbackStripCheck(t *testing.T) {
	runDir := pragmatismAtSplit(t)
	folder := filepath.Join(runDir, filepath.FromSlash(run.SplitNotes), "1 History of the Method")
	notes, err := note.ReadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	target := notes[0]
	if target.Name == "History of the Method.md" {
		target = notes[1]
	}
	for i, l := range target.Lines {
		if l.Kind == note.Body && len(l.Refs) > 0 {
			target.Lines = append(target.Lines[:i], target.Lines[i+1:]...)
			break
		}
	}
	if err := os.WriteFile(filepath.Join(folder, target.Name), []byte(target.Render()), 0o644); err != nil {
		t.Fatal(err)
	}
	stage1 := filepath.Join(runDir, filepath.FromSlash(run.FinalNotes), "1 History of the Method.md")
	b, err := os.ReadFile(stage1)
	if err != nil {
		t.Fatal(err)
	}
	original := string(b)
	noH1 := strings.Replace(original, "# 1 History of the Method\n", "", 1)
	if noH1 == original {
		t.Fatalf("%s has no H1 to drop", stage1)
	}
	if err := os.WriteFile(stage1, []byte(noH1), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stderr := resumeStop(t, runDir, run.SplitGatesDir)
	wantStop(t, runDir, code, stderr,
		"s2n resume: the strip check of the fallback notes failed on segment 1 (History of the Method); its report is "+filepath.Join(runDir, run.SplitGatesDir+run.PartialSuffix, run.FallbackStripCheckFile)+".\n",
		"This atomic note has no H1 for step 2.2 to put the source footnote after.\n   "+stage1+"\n",
		"- For each atomic note without an H1: the segment failed step 2.1 and falls back to this final stage 1 note, so give it an H1, `# <title>`, above its statements; or delete "+filepath.Join(runDir, run.SplitDir)+" to split every segment again.\n",
	)

	stopped := filepath.Join(runDir, run.StoppedFile)
	if err := os.WriteFile(stopped, []byte("an earlier stop's\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stderr = resumeStop(t, runDir, run.SplitGatesDir)
	wantStop(t, runDir, code, stderr, "For each atomic note without an H1: ")

	if err := os.WriteFile(stage1, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, stderr := resumeStop(t, runDir, run.SplitGatesDir); code != 0 {
		t.Fatalf("resume after the fix: exit %d, want 0: %s", code, stderr)
	}
	if _, err := os.Stat(stopped); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s is kept after a resume that passed (stat: %v)", run.StoppedFile, err)
	}
}

// A resume removes STOPPED.md when it starts, before it runs a step, even
// one that then stops on something other than a gate.
func TestResume_RemovesStopped(t *testing.T) {
	runDir := pragmatismAtSplit(t)
	stopped := filepath.Join(runDir, run.StoppedFile)
	if err := os.WriteFile(stopped, []byte("an earlier stop's\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(runDir, filepath.FromSlash(run.SplitNotes))); err != nil {
		t.Fatal(err)
	}
	if code, stderr := resumeStop(t, runDir, run.SplitGatesDir); code != 1 {
		t.Fatalf("exit %d, want 1, for the missing split: %s", code, stderr)
	}
	if _, err := os.Stat(stopped); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s is kept after a resume started (stat: %v)", run.StoppedFile, err)
	}
}

// A STOPPED.md that can't be removed, such as a directory with a file in it,
// stops a resume before it runs a step, naming the path.
func TestResume_StoppedNotRemoved(t *testing.T) {
	runDir := pragmatismAtSplit(t)
	stopped := filepath.Join(runDir, run.StoppedFile)
	if err := os.MkdirAll(filepath.Join(stopped, "kept"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, stderr := resumeStop(t, runDir, run.SplitGatesDir)
	if code != 1 || !strings.Contains(stderr, stopped) {
		t.Errorf("exit %d, want 1 and stderr naming %s: %s", code, stopped, stderr)
	}
	for _, dir := range []string{run.SplitGatesDir, run.SplitGatesDir + run.PartialSuffix} {
		if _, err := os.Stat(filepath.Join(runDir, dir)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s exists (stat: %v), so a step ran", dir, err)
		}
	}
}

// A summary that can't be written to the run folder is still printed, with
// why STOPPED.md is missing in place of where it is.
func TestPrintStop_NotWritten(t *testing.T) {
	folder := &run.Folder{Dir: filepath.Join(t.TempDir(), "missing")}
	var stderr bytes.Buffer
	printStop(&stderr, "s2n resume", folder, "the summary\n")
	got := stderr.String()
	if want := "the summary\ns2n resume: the summary could not be written to the run folder: "; !strings.HasPrefix(got, want) {
		t.Errorf("stderr = %q, want it to start %q", got, want)
	}
	if strings.Contains(got, "This summary is also in") {
		t.Errorf("stderr points to a STOPPED.md that wasn't written: %q", got)
	}
}

// A segment folder no segment claims stops the folder check, which says
// what its stop always said, then where STOPPED.md is, which holds the
// same text.
func TestStop_FolderCheck(t *testing.T) {
	runDir := pragmatismAtSplit(t)
	extra := filepath.Join(runDir, filepath.FromSlash(run.SplitNotes), "99 Extra")
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extra, "Stray.md"), []byte("# Stray\n\nA statement.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stderr := resumeStop(t, runDir, run.SplitGatesDir)
	if code != ExitGate {
		t.Fatalf("exit %d, want %d: %s", code, ExitGate, stderr)
	}
	for _, want := range []string{
		"s2n resume: step 2.1's folder check failed on folder \"99 Extra\". No segment of the source has the folder name of " + extra + ".",
		"Rename it to the folder of the segment its notes belong to, move its notes into that folder, or delete it, then run s2n resume " + runDir + ".\n",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not say %q:\n%s", want, stderr)
		}
	}
	wantStopped(t, runDir, stderr)
}
