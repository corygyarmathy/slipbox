package mechcheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
)

// Run runs step 1.2 on the stage 1 notes step 1.1 wrote in folder. It writes
// the report, the coverage audit file, each segment's uncovered list, the
// findings, for ReadFindings, and the coverage totals, for ReadCoverage, to
// the step's directory. A report with flags is
// not an error: stage 1's flags feed the repair pass and the run report.
func Run(src *source.Source, folder *run.Folder) error {
	return RunOn(src, folder, run.ExtractNotes, run.MechanicalCheckDir)
}

// RunOn runs step 1.2 as Run does, on the notes in notesDir and into the step
// directory dir, both in folder and slash-separated. Step 1.4's re-check runs
// it on the repaired notes.
func RunOn(src *source.Source, folder *run.Folder, notesDir, dir string) error {
	notes, err := note.ReadDir(filepath.Join(folder.Dir, filepath.FromSlash(notesDir)))
	if err != nil {
		return fmt.Errorf("mechanical check: %w", err)
	}
	report := Check(src, notes)
	step, err := folder.Begin(dir)
	if err != nil {
		return err
	}
	if err := step.WriteFile(run.ReportFile, []byte(report.String())); err != nil {
		return err
	}
	if err := step.WriteFile(run.CoverageFile, []byte(report.Coverage.String())); err != nil {
		return err
	}
	findings, err := report.findingsJSON()
	if err != nil {
		return err
	}
	if err := step.WriteFile(run.FindingsFile, findings); err != nil {
		return err
	}
	totals, err := json.Marshal(report.Coverage.Totals())
	if err != nil {
		return err
	}
	if err := step.WriteFile(run.CoverageTotalsFile, append(totals, '\n')); err != nil {
		return err
	}
	for _, s := range report.Coverage.Sections {
		if err := step.WriteFile(filepath.Join(run.UncoveredDir, s.File()), []byte(s.List())); err != nil {
			return err
		}
	}
	return step.Commit()
}

// ReadCoverage reads back the coverage totals step 1.2 wrote into the step
// directory dir, in folder and slash-separated.
func ReadCoverage(folder *run.Folder, dir string) (CoverageTotals, error) {
	var t CoverageTotals
	p := filepath.Join(folder.Dir, filepath.FromSlash(dir), run.CoverageTotalsFile)
	b, err := os.ReadFile(p)
	if err != nil {
		return t, err
	}
	if err := json.Unmarshal(b, &t); err != nil {
		return t, fmt.Errorf("%s: %w", p, err)
	}
	return t, nil
}
