package runlog_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/corygyarmathy/slipbox/internal/modelstep"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/runlog"
	"github.com/corygyarmathy/slipbox/internal/runreport"
)

// The run log is File in the state directory, beside the default root.
func TestPath(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/state")
	root, err := run.DefaultRoot()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := runlog.Path(); err != nil || got != filepath.Join(filepath.Dir(root), runlog.File) {
		t.Errorf("Path() = %q, %v, want %s beside %s", got, err, runlog.File, root)
	}
}

func newFolder(t *testing.T) *run.Folder {
	t.Helper()
	f, err := run.New(filepath.Join(t.TempDir(), "run"))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Append makes the run log's directory, writes the line as one line of
// JSON, and keeps it in the run folder. Another run's line goes after it,
// and a run folder that keeps its line appends no other.
func TestAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s2n", runlog.File)
	first, second := newFolder(t), newFolder(t)
	line := runlog.Line{
		Source:    "Src",
		RunFolder: first.Dir,
		Finished:  time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC),
		Summary:   runreport.Summary{Total: modelstep.Total{Calls: 3, CostUSD: 0.5, Seconds: 12}, LossesAdded: 1},
	}
	if err := runlog.Append(path, first, line); err != nil {
		t.Fatal(err)
	}
	other := line
	other.RunFolder = second.Dir
	if err := runlog.Append(path, second, other); err != nil {
		t.Fatal(err)
	}
	if err := runlog.Append(path, first, line); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(b), "\n")
	if len(lines) != 3 || lines[2] != "" {
		t.Fatalf("the run log holds %q, want two lines", b)
	}
	for i, want := range []runlog.Line{line, other} {
		var got runlog.Line
		if err := json.Unmarshal([]byte(lines[i]), &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("line %d is %+v, want %+v", i+1, got, want)
		}
	}
	if kept, err := os.ReadFile(first.LoggedPath()); err != nil || string(kept) != lines[0] {
		t.Errorf("the run folder keeps %q (err %v), want %q", kept, err, lines[0])
	}
	if !strings.Contains(lines[0], `"finished":"2026-11-02T09:30:00Z"`) {
		t.Errorf("line %q does not give the finish time in UTC", lines[0])
	}
}

// A run log that can't be written is an error, and the run folder keeps no
// line, so a resume appends it.
func TestAppend_NotWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), runlog.File)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	f := newFolder(t)
	if err := runlog.Append(path, f, runlog.Line{RunFolder: f.Dir}); err == nil {
		t.Error("Append to a directory succeeded")
	}
	if f.Logged() {
		t.Error("the run folder keeps a line that wasn't appended")
	}
}

// A line appended that the run folder can't keep is in the run log, and the
// error says so: it is ErrNotKept.
func TestAppend_NotKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), runlog.File)
	f := &run.Folder{Dir: filepath.Join(t.TempDir(), "gone")}
	err := runlog.Append(path, f, runlog.Line{RunFolder: f.Dir})
	if !errors.Is(err, runlog.ErrNotKept) {
		t.Errorf("Append to a run folder that can't keep the line: %v, want ErrNotKept", err)
	}
	if b, rerr := os.ReadFile(path); rerr != nil || strings.Count(string(b), "\n") != 1 {
		t.Errorf("the run log holds %q (err %v), want the one line", b, rerr)
	}
}
