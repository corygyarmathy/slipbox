package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/runlog"
	"github.com/corygyarmathy/slipbox/internal/runreport"
	"github.com/corygyarmathy/slipbox/internal/stageout"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
)

// useState makes $XDG_STATE_HOME a new temporary directory for the rest of
// t, and gives the run log's path in it.
func useState(t *testing.T) string {
	t.Helper()
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	return filepath.Join(state, "s2n", runlog.File)
}

// emptyVault makes a vault with an empty slip box, for step 3.2.
func emptyVault(t *testing.T) string {
	t.Helper()
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, stageout.SlipBoxDir), 0o755); err != nil {
		t.Fatal(err)
	}
	return vault
}

// logLines gives the run log's lines at path, none if it doesn't exist.
func logLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 || b[len(b)-1] != '\n' {
		t.Fatalf("the run log %q does not end in a newline", b)
	}
	return strings.Split(string(b[:len(b)-1]), "\n")
}

// decodeLine decodes a run log line, failing the test if it isn't one.
func decodeLine(t *testing.T, s string) runlog.Line {
	t.Helper()
	var l runlog.Line
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&l); err != nil {
		t.Fatalf("the run log line %q: %v", s, err)
	}
	return l
}

// Each run that reaches step 3.2 appends one line to the run log in
// $XDG_STATE_HOME/s2n: the run report's summary as it is, the source's name,
// the run folder and the time the run finished, in UTC. The run folder kept
// with -keep keeps the line. A second run appends a second line and leaves
// the first as it was.
func TestRun_AppendsItsLineToTheRunLog(t *testing.T) {
	logPath := useState(t)
	src := testfixture.LampSource(t)

	start := time.Now()
	runDir, p, _ := lampRun(t, nil, "-vault", emptyVault(t), "-keep")
	end := time.Now()
	lines := logLines(t, logPath)
	if len(lines) != 1 {
		t.Fatalf("the run log has %d lines, want 1: %q", len(lines), lines)
	}
	got := decodeLine(t, lines[0])
	if got.Source != src.Name || got.RunFolder != runDir {
		t.Errorf("the line names the source %q and the run folder %q, want %q and %q", got.Source, got.RunFolder, src.Name, runDir)
	}
	if got.Finished.Location() != time.UTC || got.Finished.Before(start) || got.Finished.After(end) {
		t.Errorf("the run finished at %v, want a UTC time from %v to %v", got.Finished, start, end)
	}
	folder := &run.Folder{Dir: runDir}
	want, err := runreport.ReadSummary(src, folder)
	if err != nil {
		t.Fatal(err)
	}
	if want.Total.Calls != len(p.Calls()) {
		t.Errorf("the run's summary counts %d calls, but the run made %d", want.Total.Calls, len(p.Calls()))
	}
	var raw struct{ Summary json.RawMessage }
	if err := json.Unmarshal([]byte(lines[0]), &raw); err != nil {
		t.Fatal(err)
	}
	if w, err := json.Marshal(want); err != nil || !bytes.Equal(raw.Summary, w) {
		t.Errorf("the line's summary is\n%s\nwant the run report's\n%s", raw.Summary, w)
	}
	if b, err := os.ReadFile(folder.LoggedPath()); err != nil || string(b) != lines[0]+"\n" {
		t.Errorf("the kept run folder's %s holds %q (err %v), want the line appended", run.LoggedFile, b, err)
	}

	second, _, _ := lampRun(t, nil, "-vault", emptyVault(t))
	again := logLines(t, logPath)
	if len(again) != 2 {
		t.Fatalf("the run log has %d lines after a second run, want 2: %q", len(again), again)
	}
	if again[0] != lines[0] {
		t.Errorf("the second run changed the first line:\n%s\nwant\n%s", again[0], lines[0])
	}
	if l := decodeLine(t, again[1]); l.RunFolder != second {
		t.Errorf("the second line names the run folder %q, want %q", l.RunFolder, second)
	}
}

// A run stopped by -stop-after appends nothing to the run log. The resume
// that completes step 3.2 appends its one line, and resuming the run folder
// it kept again appends no other.
func TestResume_AppendsToTheRunLogOnceStageOutIsComplete(t *testing.T) {
	logPath := useState(t)
	runDir := lampAt(t, run.FormatGateDir)
	if lines := logLines(t, logPath); lines != nil {
		t.Fatalf("a run stopped after %s appended %q to the run log", run.FormatGateDir, lines)
	}
	vault := emptyVault(t)
	for i := range 2 {
		var stdout, stderr bytes.Buffer
		if code := runCommand(context.Background(), []string{"resume", "-vault", vault, "-keep", runDir}, &stdout, &stderr, testfixture.Serve(t)); code != 0 {
			t.Fatalf("resume %d: exit %d, want 0: %s", i+1, code, stderr.String())
		}
		lines := logLines(t, logPath)
		if len(lines) != 1 {
			t.Fatalf("after resume %d the run log has %d lines, want 1: %q", i+1, len(lines), lines)
		}
		if l := decodeLine(t, lines[0]); l.RunFolder != runDir {
			t.Errorf("the line names the run folder %q, want %q", l.RunFolder, runDir)
		}
	}
}

// A run whose line can't be appended to the run log, here as the log's path
// is a directory, exits 1, leaving the notes staged and the run folder kept,
// and says how to append it. The resume it names appends the line and
// deletes the run folder.
func TestRun_RunLogNotWritten(t *testing.T) {
	logPath := useState(t)
	if err := os.MkdirAll(logPath, 0o755); err != nil {
		t.Fatal(err)
	}
	vault := emptyVault(t)
	runDir := filepath.Join(t.TempDir(), "run")
	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"run", "-run-dir", runDir, "-vault", vault, lampSourcePath(t)}, &stdout, &stderr, lampModel(t, runDir, nil)); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stderr.String())
	}
	for _, w := range []string{"but the run folder " + runDir + " is kept", "could not be appended to the run log " + logPath, "Run s2n resume ", runDir + " to append it.\n"} {
		if !strings.Contains(stderr.String(), w) {
			t.Errorf("stderr %q does not say %q", stderr.String(), w)
		}
	}
	folder := &run.Folder{Dir: runDir}
	if !folder.Complete(run.StageOutDir) || folder.Logged() {
		t.Errorf("the run folder is not kept with %s complete and no line logged", run.StageOutDir)
	}
	staging := stageout.DefaultStaging(vault, testfixture.Lamp)
	staged := testfixture.Files(t, staging)
	if _, ok := staged[stageout.RunReportFile]; !ok || len(staged) < 3 {
		t.Errorf("the staging folder holds %q, want the staged notes and the run report", keys(staged))
	}

	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := runCommand(context.Background(), []string{"resume", "-vault", vault, runDir}, &stdout, &stderr, testfixture.Serve(t)); code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, stderr.String())
	}
	if lines := logLines(t, logPath); len(lines) != 1 {
		t.Errorf("the run log has %d lines after the resume, want 1: %q", len(lines), lines)
	}
	if _, err := os.Stat(runDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the run folder is not deleted after the resume (stat: %v)", err)
	}
	if got := testfixture.Files(t, staging); len(got) != len(staged) {
		t.Errorf("the resume changed the staging folder: %q, was %q", keys(got), keys(staged))
	}
}
