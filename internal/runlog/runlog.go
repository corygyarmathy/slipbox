// Package runlog keeps the run log, one JSON line per run that succeeded,
// so that runs can be compared without reading each one's run report. It is
// `run-log.jsonl` in s2n's state directory (run.StateDir), beside the
// default root of the run folders, not inside it.
//
// A run appends its line once step 3.2 is complete, before its run folder is
// deleted or kept. A run that stops, by a gate, an error, -stop-after or
// Ctrl-C, appends nothing, and neither does a resume of it, until one
// completes step 3.2. A line is the run report's summary as it is
// (runreport.Summary), with the source's name, the run folder's path, which
// a deleted folder no longer has, and the time the run finished, in UTC.
//
// Each run appends one line: the run folder keeps the line it appended
// (run.LoggedFile), and a run folder that has it appends no other, as when a
// run kept with -keep is resumed again. A run whose line can't be appended
// keeps its run folder, so a resume appends it. A line that is appended but
// that the run folder can't keep (ErrNotKept) is in the run log, so the run
// has succeeded: only a resume of the run folder, kept with -keep, would
// append it again.
package runlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/runreport"
)

// File is the run log's name in the state directory.
const File = "run-log.jsonl"

// ErrNotKept is what Append gives when it has appended the line, but the run
// folder could not keep it.
var ErrNotKept = errors.New("the run folder could not keep the line")

// Line is one run's line in the run log.
type Line struct {
	Source    string            `json:"source"`
	RunFolder string            `json:"run_folder"`
	Finished  time.Time         `json:"finished"`
	Summary   runreport.Summary `json:"summary"`
}

// Path is the run log's path: File in run.StateDir.
func Path() (string, error) {
	state, err := run.StateDir()
	if err != nil {
		return "", fmt.Errorf("run log: %w", err)
	}
	return filepath.Join(state, File), nil
}

// Append appends line, folder's run's, to the run log at path, making its
// directory if need be, then keeps it in folder, unless folder already
// keeps a line it appended. The line is written in one write, so runs that
// finish together don't interleave theirs.
func Append(path string, folder *run.Folder, line Line) error {
	if folder.Logged() {
		return nil
	}
	data, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("run log: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("run log: %w", err)
	}
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("run log: %w", err)
	}
	if _, err := fh.Write(data); err != nil {
		fh.Close()
		return fmt.Errorf("run log: %w", err)
	}
	if err := fh.Close(); err != nil {
		return fmt.Errorf("run log: %w", err)
	}
	if err := folder.WriteLogged(data); err != nil {
		return fmt.Errorf("run log: the line is appended to %s, but %w: %w", path, ErrNotKept, err)
	}
	return nil
}
