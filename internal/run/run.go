// Package run lays out the run folder, the contract between steps (ADR 0002
// §5). Each step writes only its own directory and reads those of earlier
// steps. A step writes into `<name>.partial` and renames it to `<name>` once
// it succeeds, so a complete directory means a finished step. A step that
// stops leaves its partial directory, and the next attempt reuses the
// segments it finished (Folder.Begin).
//
// A run folder is `<root>/<source name>/<UTC timestamp>/`, where root
// defaults to `$XDG_STATE_HOME/s2n/runs` (`~/.local/state/s2n/runs`).
//
// The run's first act copies the source into the folder, so later steps and
// `s2n resume` read the frame the run started from:
//
//	source/<source name>.md   the source as the run read it
//
// A run a gate stopped has, at the top of its folder, a summary of the stop
// for the human: no step reads it, and `s2n resume` removes it when it
// starts:
//
//	STOPPED.md                what failed the gate, where, and what to do, as the
//	                          terminal gave it
//
// Step directories:
//
//	1.1-extract/              step 1.1, extract
//	  stage-1/<note>.md       the stage 1 notes, every segment's together
//	  reports/<n>.md          segment n's generator report
//	  raw/<n>.txt             segment n's raw reply
//	  requests/<n>.sha256     the hash of segment n's request, once its reply is finished,
//	                          which a resume checks before it reuses raw/<n>.txt; every
//	                          model step keeps these beside its raw replies
//	  usage.jsonl             one line per call, every attempt's: step, segment, model, effort, brief file, usage and the usage limits' reading, and a failed call's reply
//	  dropped.json            each file of a reply the step dropped, and why
//	1.2-mechanical-check/     step 1.2, the mechanical check
//	  report.txt              the gate report, up to its coverage section
//	  coverage.txt            the on-demand audit file: the coverage section and the totals
//	  uncovered/<n>.txt       segment n's uncovered list for its loss proposer, if it has one
//	  findings.json           every note's findings, the report's notes in its order, for
//	                          the steps that read step 1.2's findings
//	1.3a-support-check/       step 1.3a, the support and cuts check
//	  reports/<n>.md          segment n's checker report, the reply as received
//	  flags/<n>.json          segment n's report parsed: its flags, and its other lines kept as text
//	  raw/<n>.txt             segment n's raw reply
//	  usage.jsonl             one line per call, as in 1.1-extract
//	1.3b-reads-alone-check/   step 1.3b, the reads-alone check, laid out as 1.3a-support-check
//	1.4-repair/               step 1.4, the repair pass
//	  stage-1/<note>.md       every stage 1 note after repair: a note the call changed as it
//	                          wrote it, every other one copied from 1.1-extract/stage-1
//	  reports/<n>.md          segment n's repair report, if it was called
//	  raw/<n>.txt             segment n's raw reply, if it was called
//	  usage.jsonl             one line per call, as in 1.1-extract
//	  dropped.json            each file or edit of a reply the step dropped, and why
//	1.4-recheck-1.2-mechanical-check/    step 1.2 on the repaired notes, every segment's,
//	                                     laid out as 1.2-mechanical-check; coverage and the
//	                                     loss proposer read this one
//	1.4-recheck-1.3a-support-check/      step 1.3a on the segments the repair call changed,
//	                                     laid out as 1.3a-support-check
//	1.4-recheck-1.3b-reads-alone-check/  step 1.3b on the segments the repair call changed,
//	                                     laid out as 1.3b-reads-alone-check
//	1.4-still-fails/
//	  still-fails.json        the statements that still fail after the repair pass, for the
//	                          run report: one entry per statement, with its traces and flags
//	1.5-loss-proposer/        step 1.5, the loss proposer, run twice on the segments with an
//	                          uncovered list in 1.4-recheck-1.2-mechanical-check
//	  run-1/raw/<n>.txt       segment n's first raw reply
//	  run-2/raw/<n>.txt       segment n's second raw reply, to the same prompt
//	  usage.jsonl             one line per call, as in 1.1-extract, each naming its run
//	  proposed-losses.json    the union of the two runs: per segment, one entry per proposed
//	                          loss with each run's wording of it
//	1.4-kept-loss-repair/     every proposed loss sent through a repair call of its own, on
//	                          1.4-repair/stage-1, then re-checked. The re-check stops nothing:
//	                          what it finds goes to the run report, and stage-1-final drops
//	                          a broken or untraced statement
//	  kept-losses.json        every proposed loss, per segment
//	  stage-1/<note>.md       every stage 1 note after the call: a note it changed as it
//	                          wrote it, every other one copied from 1.4-repair/stage-1
//	  reports/<n>.md          segment n's report, if it had kept losses
//	  raw/<n>.txt             segment n's raw reply, if it had kept losses
//	  usage.jsonl             one line per call, as in 1.1-extract, the re-check's included
//	  dropped.json            each file or edit of a reply the step dropped, and each segment
//	                          whose notes gained no statement, with why
//	  still-fails.json        the new statements step 1.3a still flags, laid out as
//	                          1.4-still-fails's
//	  recheck-1.2-mechanical-check/  step 1.2 on stage-1/ here, every segment's, laid out as
//	                                 1.2-mechanical-check
//	  recheck-1.3a-support-check/    step 1.3a on the segments with kept losses, if any, laid
//	                                 out as 1.3a-support-check, without its usage log
//	stage-1-final/
//	  stage-1/<note>.md       the final stage 1 notes: 1.4-kept-loss-repair/stage-1 with each
//	                          broken or untraced statement dropped, and an aside left with no
//	                          statement deleted
//	  dropped.json            each statement and note the step dropped, and why
//
// Stage 2 goes on from the final stage 1 notes. In stages 2 and 3 a segment
// whose reply can't be written, or that fails a gate, falls back to the notes
// the stage before gave it, and the run goes on (package fallback):
//
//	2.1-split-and-hub/        step 2.1, split and hub
//	  stage-2/<n> <title>/    segment n's atomic notes, and its hub note, <title>.md, unless
//	                          its reply couldn't be written
//	  manifests/<n>.json      segment n's manifest, the reply's `=== MANIFEST ===` block
//	  raw/<n>.txt             segment n's raw reply
//	  usage.jsonl             one line per call, as in 1.1-extract
//	  fallbacks.json          each segment whose reply couldn't be written, and why
//	2.1-gates/                step 2.1's gates on the final stage 1 notes and
//	                          2.1-split-and-hub/stage-2, segment by segment: the split
//	                          check, then the trace diff on the segments that passed it,
//	                          then the strip check on those that passed both. A segment
//	                          that fails one falls back. Only a segment folder no segment
//	                          claims, or a fallback that can't be stripped, stops the
//	                          run, and the directory is left partial. `s2n resume` runs
//	                          the gates again, on the split notes as the human left them,
//	                          or on a new split if they deleted 2.1-split-and-hub
//	  split-check.txt         the split check's report. A lost or
//	                          extra trace, an untraced statement or a footnote in a hub
//	                          fails it
//	  trace-diff.txt          the trace diff's report, on the segments that passed the
//	                          split check. A lost or added quote that isn't a name span's
//	                          fails it
//	  strip-check.txt         the strip check's report, on the segments that passed the
//	                          trace diff. A definition that is neither a trace nor the
//	                          source footnote, or an atomic note with no H1, fails it, as
//	                          step 2.2 can't strip it
//	  fallback-strip-check.txt  the strip check's report on the fallbacks
//	  stage-2/<n> <title>/    every segment's notes as they go on: the split, for a segment
//	                          that passed every gate, and otherwise its fallback, its final
//	                          stage 1 notes under a hub with no statements or traces
//	  fallbacks.json          each segment that fell back, 2.1-split-and-hub's among them,
//	                          and why
//	2.2-strip-traces/         step 2.2, strip traces, once 2.1-gates is complete
//	  stage-2/<n> <title>/    2.1-gates/stage-2's notes with every footnote reference and
//	                          trace gone, and each atomic note given the source footnote
//
// Stage 3 goes on from the stripped notes:
//
//	3.1-format/               step 3.1, format, once 2.2-strip-traces is complete
//	  stage-3/<n> <title>/    segment n's notes, its hub among them, as the reply gave them:
//	                          every note of 2.2-strip-traces/stage-2/<n> <title>/, formatted
//	                          or not, unless the reply couldn't be written
//	  manifests/<n>.json      segment n's manifest, the reply's `=== MANIFEST ===` block
//	  raw/<n>.txt             segment n's raw reply
//	  usage.jsonl             one line per call, as in 1.1-extract
//	  fallbacks.json          each segment whose reply couldn't be written, and why
//	3.1-gate/                 step 3.1's gate, the format check, on
//	                          2.2-strip-traces/stage-2 and 3.1-format/stage-3. A segment
//	                          that fails it falls back
//	  format-check.txt        the format check's report. A
//	                          changed frontmatter, H1, footnote definition or word, a
//	                          heading or callout title not in the source, a forbidden
//	                          element, or a note not in 2.2-strip-traces fails it
//	  stage-3/<n> <title>/    every segment's notes as they go on: 3.1-format/stage-3's, for
//	                          a segment that passed, and otherwise its fallback, its
//	                          2.2-strip-traces/stage-2 notes unformatted
//	  fallbacks.json          each segment that fell back, 3.1-format's among them, and why
//	run-report/
//	  report.md               the run report: each loss added, each statement that still
//	                          fails, each item dropped, each segment that fell back, and the
//	                          asides and name spans that point outside their segment,
//	                          counted, with links into the run folder
//	  staged-report.md        the same report naming the notes without links, which step 3.2
//	                          copies into the staging folder
//	3.2-stage-out/            step 3.2, stage out, once 3.1-gate is complete. It copies
//	                          3.1-gate/stage-3's notes into the staging folder, outside
//	                          the run folder, with the name-clash list and the run report
//	  name-clashes.md         a copy of the staging folder's name-clash list
//
// Once step 3.2 is complete the run has succeeded, and its run folder is
// deleted. A run stopped earlier, by a gate, an error or -stop-after, keeps it.
package run

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SourceDir holds the run's copy of its source.
const SourceDir = "source"

// StoppedFile is the summary of a gate's stop, at the top of the run folder,
// there only while the run is stopped.
const StoppedFile = "STOPPED.md"

// Step directory names.
const (
	ExtractDir         = "1.1-extract"
	MechanicalCheckDir = "1.2-mechanical-check"
	SupportCheckDir    = "1.3a-support-check"
	ReadsAloneDir      = "1.3b-reads-alone-check"
	RepairDir          = "1.4-repair"
	StillFailsDir      = "1.4-still-fails"
	LossProposerDir    = "1.5-loss-proposer"
	KeptLossDir        = "1.4-kept-loss-repair"
	FinalDir           = "stage-1-final"
	RunReportDir       = "run-report"
	SplitDir           = "2.1-split-and-hub"
	SplitGatesDir      = "2.1-gates"
	StripDir           = "2.2-strip-traces"
	FormatDir          = "3.1-format"
	FormatGateDir      = "3.1-gate"
	StageOutDir        = "3.2-stage-out"
)

// The kept-loss repair's re-check directories, inside its own.
const (
	KeptLossMechanicalDir = "recheck-" + MechanicalCheckDir
	KeptLossSupportDir    = "recheck-" + SupportCheckDir
)

// Step 1.4's re-check directories: steps 1.2 and 1.3 run again on the
// repaired notes, each in a directory of its own laid out as the first run's.
const (
	recheckPrefix        = "1.4-recheck-"
	MechanicalRecheckDir = recheckPrefix + MechanicalCheckDir
	SupportRecheckDir    = recheckPrefix + SupportCheckDir
	ReadsAloneRecheckDir = recheckPrefix + ReadsAloneDir
)

// The notes' directories, from the run folder: step 1.1's stage 1 notes, and
// step 1.4's repaired copy of them.
const (
	ExtractNotes = ExtractDir + "/" + NotesDir
	RepairNotes  = RepairDir + "/" + NotesDir
)

// The notes' directories after step 1.5, from the run folder: the kept-loss
// repair's and the final stage 1 notes.
const (
	KeptLossNotes = KeptLossDir + "/" + NotesDir
	FinalNotes    = FinalDir + "/" + NotesDir
)

// SplitNotes is step 2.1's directory of segment folders,
// SplitOrFallbackNotes its gates', StrippedNotes step 2.2's, FormattedNotes
// step 3.1's and FormattedOrFallbackNotes its gate's, from the run folder. A
// gate's holds every segment's notes as they go on: the model step's for a
// segment that passed, and its fallback for one that didn't.
const (
	SplitNotes               = SplitDir + "/" + Stage2NotesDir
	SplitOrFallbackNotes     = SplitGatesDir + "/" + Stage2NotesDir
	StrippedNotes            = StripDir + "/" + Stage2NotesDir
	FormattedNotes           = FormatDir + "/" + Stage3NotesDir
	FormattedOrFallbackNotes = FormatGateDir + "/" + Stage3NotesDir
)

// StillFailsFile is step 1.4's still-fails list, in its own directory, and
// the kept-loss repair's, in its.
const StillFailsFile = "still-fails.json"

// ProposedLossesFile is step 1.5's union of its two runs, in its directory.
const ProposedLossesFile = "proposed-losses.json"

// KeptLossesFile is the kept-loss repair's list of kept losses, in its
// directory.
const KeptLossesFile = "kept-losses.json"

// DroppedFile lists what a stage 1 step dropped instead of stopping the run,
// in its directory (package dropped).
const DroppedFile = "dropped.json"

// FallbacksFile lists the segments a stage 2 or 3 step failed, which fell
// back instead of stopping the run, in its directory (package fallback).
const FallbacksFile = "fallbacks.json"

// RunReportFile is the run report, in its directory.
const RunReportFile = "report.md"

// StagedReportFile is the run report that step 3.2 stages, in the run
// report's directory. It names the notes in place of linking them, since a
// successful run deletes the run folder its links point into.
const StagedReportFile = "staged-report.md"

// Names inside a model step's directory.
const (
	RawDir = "raw"
	// RequestsDir holds, beside RawDir, the hash of each finished segment's
	// request, `<n>.sha256`, so `s2n resume` can reuse its raw reply.
	RequestsDir = "requests"
	// RunPrefix begins the directory of a numbered run of a model step,
	// `run-<k>`, which `s2n resume` cleans like the step's own.
	RunPrefix  = "run-"
	UsageFile  = "usage.jsonl"
	NotesDir   = "stage-1"
	ReportsDir = "reports"
	// Stage2NotesDir holds a stage 2 step's segment folders, each
	// `<n> <title>/`.
	Stage2NotesDir = "stage-2"
	// Stage3NotesDir holds step 3.1's segment folders, each `<n> <title>/`.
	Stage3NotesDir = "stage-3"
	// ManifestsDir holds a stage 2 or 3 model step's manifests, one `<n>.json` per
	// segment.
	ManifestsDir = "manifests"
)

// ReportFile is a check's gate report inside its step's directory.
const ReportFile = "report.txt"

// The reports of step 2.1's gates, in its gates' directory.
const (
	SplitCheckFile = "split-check.txt"
	TraceDiffFile  = "trace-diff.txt"
	StripCheckFile = "strip-check.txt"
	// FallbackStripCheckFile is the strip check's report on the stage 2
	// fallbacks.
	FallbackStripCheckFile = "fallback-strip-check.txt"
)

// FormatCheckFile is the format check's report, in step 3.1's gate's
// directory.
const FormatCheckFile = "format-check.txt"

// Names inside the mechanical check's directory, beside its report.
const (
	CoverageFile = "coverage.txt"
	UncoveredDir = "uncovered"
	FindingsFile = "findings.json"
)

// FlagsDir holds a checker step's parsed reports, beside its reports.
const FlagsDir = "flags"

// PartialSuffix ends the name of a step's directory while the step runs.
const PartialSuffix = ".partial"

// Folder is one run's folder.
type Folder struct {
	Dir string
}

// DefaultRoot is where run folders go when none is given:
// `$XDG_STATE_HOME/s2n/runs`, falling back to `~/.local/state/s2n/runs`.
// It is outside the vault, which receives only the staged output.
func DefaultRoot() (string, error) {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" || !filepath.IsAbs(state) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("run folder: %w", err)
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "s2n", "runs"), nil
}

// NewIn makes a new run folder for sourceName under root, named for now.
func NewIn(root, sourceName string, now time.Time) (*Folder, error) {
	return New(filepath.Join(root, sourceName, now.UTC().Format("20060102T150405Z")))
}

// New makes a new, empty run folder at dir. It fails if dir exists.
func New(dir string) (*Folder, error) {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, fmt.Errorf("run folder: %w", err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		return nil, fmt.Errorf("run folder: %w", err)
	}
	return &Folder{Dir: dir}, nil
}

// Open opens the existing run folder at dir, as `s2n resume` does. It fails
// if dir is not a directory or holds no copy of its source.
func Open(dir string) (*Folder, error) {
	fi, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("run folder: %w", err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("run folder: %s is not a directory", dir)
	}
	f := &Folder{Dir: dir}
	if _, err := f.SourcePath(); err != nil {
		return nil, err
	}
	return f, nil
}

// Delete deletes the run folder, once the run has succeeded.
func (f *Folder) Delete() error {
	return os.RemoveAll(f.Dir)
}

// StoppedPath is the path of the run's StoppedFile.
func (f *Folder) StoppedPath() string {
	return filepath.Join(f.Dir, StoppedFile)
}

// WriteStopped writes summary, a gate's stop, to the run's StoppedFile.
func (f *Folder) WriteStopped(summary string) error {
	return os.WriteFile(f.StoppedPath(), []byte(summary), 0o644)
}

// ClearStopped removes the run's StoppedFile, if it has one, as `s2n
// resume` does when it starts.
func (f *Folder) ClearStopped() error {
	if err := os.Remove(f.StoppedPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Complete says whether the step named name is complete: its directory, not
// a partial one, exists.
func (f *Folder) Complete(name string) bool {
	fi, err := os.Stat(filepath.Join(f.Dir, name))
	return err == nil && fi.IsDir()
}

// SaveSource writes the run's copy of the source named name (without .md).
// It fails if the run already has one.
func (f *Folder) SaveSource(name string, frame []byte) error {
	dir := filepath.Join(f.Dir, SourceDir)
	if err := os.Mkdir(dir, 0o755); err != nil {
		return fmt.Errorf("run folder: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, name+".md"), frame, 0o644)
}

// SourcePath is the path of the run's copy of its source.
func (f *Folder) SourcePath() (string, error) {
	paths, err := filepath.Glob(filepath.Join(f.Dir, SourceDir, "*.md"))
	if err != nil {
		return "", err
	}
	if len(paths) != 1 {
		return "", fmt.Errorf("run folder %s holds %d sources, want 1", f.Dir, len(paths))
	}
	return paths[0], nil
}

// Begin starts the step named name. It fails if the step is already
// complete. A stopped attempt's partial directory is cleaned rather than
// dropped (ADR 0002 §6): its usage log stays, because its calls
// were spent, and so do its raw replies and request records, so the step's
// model calls can reuse the segments it finished. Everything else the
// attempt wrote is removed, and the step writes it again.
func (f *Folder) Begin(name string) (*Step, error) {
	final := filepath.Join(f.Dir, name)
	if _, err := os.Stat(final); err == nil {
		return nil, fmt.Errorf("step %s is already complete in %s", name, f.Dir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	partial := final + PartialSuffix
	if err := clean(partial); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(partial, 0o755); err != nil {
		return nil, err
	}
	return &Step{Name: name, Dir: partial, final: final}, nil
}

// clean removes from a stopped attempt's directory dir all but what Begin
// keeps: the usage log, `raw/` and `requests/`, and the same inside a
// numbered run's `run-<k>/` and inside a step the step ran within its own
// directory and left partial (the kept-loss repair's re-check). A missing
// dir is clean.
func clean(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		p := filepath.Join(dir, name)
		switch {
		case name == UsageFile && !e.IsDir():
		case (name == RawDir || name == RequestsDir) && e.IsDir():
		case e.IsDir() && (strings.HasPrefix(name, RunPrefix) || strings.HasSuffix(name, PartialSuffix)):
			if err := clean(p); err != nil {
				return err
			}
		default:
			if err := os.RemoveAll(p); err != nil {
				return err
			}
		}
	}
	return nil
}

// Step is a step's directory while the step runs.
type Step struct {
	// Name is the step's directory name, such as 1.1-extract.
	Name string
	// Dir is the partial directory the step writes into.
	Dir   string
	final string

	mu sync.Mutex
}

// WriteFile writes data to rel inside the step's directory, making parent
// directories as needed. It is safe for concurrent use.
func (s *Step) WriteFile(rel string, data []byte) error {
	p, err := s.path(rel)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// AppendFile appends data to rel inside the step's directory. It is safe for
// concurrent use.
func (s *Step) AppendFile(rel string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.path(rel)
	if err != nil {
		return err
	}
	fh, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := fh.Write(data); err != nil {
		fh.Close()
		return err
	}
	return fh.Close()
}

// Mkdir makes the directory rel, and any parent, in the step's directory.
func (s *Step) Mkdir(rel string) error {
	return os.MkdirAll(filepath.Join(s.Dir, rel), 0o755)
}

// Remove removes rel inside the step's directory, if it exists.
func (s *Step) Remove(rel string) error {
	err := os.Remove(filepath.Join(s.Dir, rel))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// path gives rel's path inside the step's directory, making its parent
// directories.
func (s *Step) path(rel string) (string, error) {
	p := filepath.Join(s.Dir, rel)
	return p, os.MkdirAll(filepath.Dir(p), 0o755)
}

// Commit marks the step complete by renaming its partial directory.
func (s *Step) Commit() error {
	return os.Rename(s.Dir, s.final)
}
