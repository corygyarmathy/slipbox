package mechcheck

import (
	"fmt"
	"path/filepath"

	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/source"
)

// Run runs step 1.2 on the stage 1 notes step 1.1 wrote in folder. It writes
// the report, the coverage audit file, each segment's uncovered list and the
// findings, for ReadFindings, to the step's directory. A report with flags is
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
	for _, s := range report.Coverage.Sections {
		if err := step.WriteFile(filepath.Join(run.UncoveredDir, s.File()), []byte(s.List())); err != nil {
			return err
		}
	}
	return step.Commit()
}
