// Command s2n turns a source's text into traced, refined notes (docs/design.md).
//
//	s2n run [-run-dir DIR] [flags] <source.md>
//	s2n resume [flags] <run folder>
//
// with the flags -stage-1-model, -extract-effort, -repair-model,
// -repair-reply, -quote-once, -proposer-model, -stage-2-model,
// -stage-3-model, -five-hour-usd, -vault, -staging and -stop-after (see -h).
// -vault defaults to $S2N_VAULT.
//
// A run prints its run folder's path and runs every step, unattended, from
// step 1.1 to step 3.2 (ADR 0009). Stage 1 stops on no flag: a reply's file
// or edit that can't be applied is dropped, step 1.5's proposed losses are
// all kept, the kept-loss repair's re-check stops nothing, and the final
// stage 1 notes drop a broken or untraced statement. Then step 2.1 splits
// the final notes into each segment's atomic notes and hub note, and its
// gates, the split check, the trace diff and the strip check, check the
// split. Step 2.2 strips the traces, giving each atomic note its source
// footnote, step 3.1 formats the stripped notes, and its gate, the format
// check, checks that formatting changed markup only. Stages 2 and 3 stop on
// no failing segment either: a segment whose reply can't be written, or that
// fails a gate, falls back to the notes the stage before gave it, and the
// rest go on as normal. The run report then lists each loss added, each
// statement that still fails, each item dropped, each segment that fell back,
// and the asides and name spans that point outside their segment. Last, step
// 3.2 stages the notes out into the staging folder, which defaults to
// <vault>/Staging/<source name>/, with the name-clash list and the run report
// beside them, and the run folder is deleted, once the run's model calls and
// their cost are printed. A staging folder that already holds anything stops
// the run before it writes there. A run that can reach step 3.2, with no
// -stop-after before it, checks the vault and the staging folder before it
// runs a step, so that a stop there comes before any model call. With
// -stop-after STEP the run stops after that step instead, with exit status 0.
//
// A run waits out the account's usage limits (ADR 0011, package
// usagelimit). Before the first model call it reads the account's usage, and
// before each call it predicts whether the 5-hour limit has room for it,
// taking the limit to hold -five-hour-usd dollars at API rates. A call that
// won't fit, or one the account refuses, waits for the limit to reset, and
// the run prints a line saying which limit and until when. Ctrl-C, during a
// wait or a call, stops the run with the command that resumes it. A limit
// whose reset time can't be read stops the run with exit status 1.
//
// Resume picks a run folder up where it stopped: it runs the first step whose
// directory isn't complete, and every step after it. It reads no flag's
// choice from the run folder, so it needs the run's flags again: the resume
// command a stop prints names each one given a value other than its default.
//
// A gate that stops the run, for a fault with nothing to fall back to, such
// as a segment folder no segment claims, exits 4, naming the gate, the
// segments and the gate's report, if it has one, then each item that failed
// it, up to gate.MaxItems, with the note's path, what is wrong and what to do
// The same summary is written to STOPPED.md at the top of the
// run folder, which resume removes when it starts, and resuming runs the
// gate's step again. An error exits 1, and a usage error 2.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/corygyarmathy/slipbox/internal/extract"
	"github.com/corygyarmathy/slipbox/internal/format"
	"github.com/corygyarmathy/slipbox/internal/formatcheck"
	"github.com/corygyarmathy/slipbox/internal/gate"
	"github.com/corygyarmathy/slipbox/internal/keptloss"
	"github.com/corygyarmathy/slipbox/internal/lossproposer"
	"github.com/corygyarmathy/slipbox/internal/mechcheck"
	"github.com/corygyarmathy/slipbox/internal/modelstep"
	"github.com/corygyarmathy/slipbox/internal/note"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/quoteonce"
	"github.com/corygyarmathy/slipbox/internal/readsalone"
	"github.com/corygyarmathy/slipbox/internal/repair"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/runreport"
	"github.com/corygyarmathy/slipbox/internal/source"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/splitgates"
	"github.com/corygyarmathy/slipbox/internal/stageout"
	"github.com/corygyarmathy/slipbox/internal/striptraces"
	"github.com/corygyarmathy/slipbox/internal/supportcheck"
	"github.com/corygyarmathy/slipbox/internal/usagelimit"
)

const usage = "usage: s2n run [-run-dir DIR] [-stage-1-model MODEL] [-extract-effort LEVEL] [-repair-model MODEL] [-repair-reply SHAPE] [-quote-once=false] [-proposer-model MODEL] [-stage-2-model MODEL] [-stage-3-model MODEL] [-five-hour-usd USD] [-vault DIR] [-staging DIR] [-stop-after STEP] <source.md>\n" +
	"       s2n resume [-stage-1-model MODEL] [-extract-effort LEVEL] [-repair-model MODEL] [-repair-reply SHAPE] [-quote-once=false] [-proposer-model MODEL] [-stage-2-model MODEL] [-stage-3-model MODEL] [-five-hour-usd USD] [-vault DIR] [-staging DIR] [-stop-after STEP] <run folder>"

// ExitGate is the exit code of a run a gate stopped, distinct from an error's
// 1 and a usage error's 2. 3 was the checkpoint's, which s2n no longer has.
const ExitGate = 4

// VaultEnv names the environment variable -vault defaults to.
const VaultEnv = "S2N_VAULT"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(runCommand(ctx, os.Args[1:], os.Stdout, os.Stderr, provider.Claude{}))
}

// accountUsage reads the account's usage before a run's first model call.
// Tests replace it, so that none reaches the network.
var accountUsage = usagelimit.ReadAccount

// clock is the gate's clock. Tests replace it, so that none waits.
var clock = usagelimit.RealClock

// runCommand runs the command line args and returns the exit code. p is the
// provider every model step calls.
func runCommand(ctx context.Context, args []string, stdout, stderr io.Writer, p provider.Provider) int {
	if len(args) == 0 || (args[0] != "run" && args[0] != "resume") {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	name := "s2n " + args[0]
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var runDir *string
	if args[0] == "run" {
		runDir = fs.String("run-dir", "", "the new run folder (default $XDG_STATE_HOME/s2n/runs/<source name>/<UTC time>)")
	}
	var opts options
	fs.StringVar(&opts.stage1Model, "stage-1-model", extract.DefaultModel, "the model for the steps run at stage 1's tier, 1.1, and 1.4 and the kept-loss repair unless -repair-model is given (sonnet is the tested fallback)")
	fs.StringVar(&opts.extractEffort, "extract-effort", extract.DefaultEffort, fmt.Sprintf("step 1.1's effort level, one of %s", strings.Join(provider.Efforts, ", ")))
	fs.StringVar(&opts.repairModel, "repair-model", "", "the model for step 1.4's repair call and the kept-loss repair, if not -stage-1-model's")
	repairReply := fs.String("repair-reply", string(repair.DefaultReply), fmt.Sprintf("the reply shape of step 1.4's repair call and the kept-loss repair, one of %s", joinReplies()))
	fs.BoolVar(&opts.quoteOnce, "quote-once", quoteonce.Default, "step 1.1, and step 1.4 and the kept-loss repair with -repair-reply edits, write each quote once with the joining text marked, and s2n writes the traces; -quote-once=false has the model write them")
	fs.StringVar(&opts.proposerModel, "proposer-model", lossproposer.Model, "the model for both runs of step 1.5, the loss proposer (sonnet is the tier; the flag is for trialling another)")
	fs.StringVar(&opts.stage2Model, "stage-2-model", split.DefaultModel, "the model for step 2.1, split and hub (sonnet is the chosen tier; opus and haiku were trialled)")
	fs.StringVar(&opts.stage3Model, "stage-3-model", format.DefaultModel, "the model for step 3.1, format (haiku is the tested tier)")
	fs.Float64Var(&opts.fiveHourUSD, "five-hour-usd", usagelimit.DefaultFiveHourUSD, "the dollars at API rates the plan's 5-hour usage limit holds, for predicting whether a call fits (Pro's is about 10)")
	opts.vaultDefault = os.Getenv(VaultEnv)
	fs.StringVar(&opts.vault, "vault", opts.vaultDefault, "the vault step 3.2 stages the notes into, whose "+stageout.SlipBoxDir+" it reads note names from (default $"+VaultEnv+")")
	fs.StringVar(&opts.staging, "staging", "", "step 3.2's staging folder (default <vault>/"+stageout.StagingDir+"/<source name>)")
	fs.StringVar(&opts.stopAfter, "stop-after", "", "stop after this step, named by its directory in the run folder, such as "+run.ReadsAloneRecheckDir+", if the run reaches it")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if !slices.Contains(provider.Efforts, opts.extractEffort) {
		fmt.Fprintf(stderr, "%s: -extract-effort %q is not one of %s\n", name, opts.extractEffort, strings.Join(provider.Efforts, ", "))
		return 2
	}
	opts.repairReply = repair.Reply(*repairReply)
	if !slices.Contains(repair.Replies, opts.repairReply) {
		fmt.Fprintf(stderr, "%s: -repair-reply %q is not one of %s\n", name, *repairReply, joinReplies())
		return 2
	}
	if !(opts.fiveHourUSD > 0) {
		fmt.Fprintf(stderr, "%s: -five-hour-usd %v is not a positive number of dollars\n", name, opts.fiveHourUSD)
		return 2
	}
	if opts.repairModel == "" {
		opts.repairModel = opts.stage1Model
	}
	if dirs := stepDirs(); opts.stopAfter != "" && !slices.Contains(dirs, opts.stopAfter) {
		fmt.Fprintf(stderr, "%s: -stop-after %q is not a step; the steps are %s\n", name, opts.stopAfter, strings.Join(dirs, ", "))
		return 2
	}

	var (
		folder *run.Folder
		src    *source.Source
		err    error
	)
	if args[0] == "run" {
		folder, src, err = newRun(fs.Arg(0), *runDir, stdout)
	} else if folder, src, err = openRun(fs.Arg(0)); err == nil {
		err = folder.ClearStopped()
	}
	var sourceNotUTF8 *source.NotUTF8
	if args[0] == "run" && errors.As(err, &sourceNotUTF8) {
		fmt.Fprintf(stderr, "%s: the source %v. Re-save it as UTF-8, then run s2n run again.\n", name, err)
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return 1
	}
	opts.sourceName = src.Name
	if opts.staging == "" && opts.vault != "" {
		opts.staging = stageout.DefaultStaging(opts.vault, src.Name)
	}
	var stopped string
	if reachesStageOut(folder, opts) {
		err = stageout.Check(folder, opts.vault, opts.staging)
	}
	var latest *provider.Reading
	if err == nil {
		latest, err = modelstep.LatestReading(folder.Dir)
	}
	if err == nil {
		say := func(s string) { fmt.Fprintf(stderr, "%s: %s\n", name, s) }
		limits := usagelimit.NewGate(p, opts.fiveHourUSD, clock, say, accountUsage)
		limits.Observe(latest)
		stopped, err = runSteps(ctx, limits, src, opts, folder)
	}
	var (
		gateErr    *gate.Stopped
		occupied   *stageout.Occupied
		notUTF8    *note.NotUTF8
		unreadable *usagelimit.Unreadable
	)
	switch {
	case errors.As(err, &gateErr):
		printStop(stderr, name, folder, stopSummary(name, gateErr, resumeCommand(opts, folder.Dir)))
		return ExitGate
	case errors.Is(err, stageout.ErrNoVault):
		fmt.Fprintf(stderr, "%s: %v. Give -vault DIR or set $%s, then run %s.\n", name, err, VaultEnv, resumeCommand(opts, folder.Dir))
		return 1
	case errors.As(err, &occupied):
		fmt.Fprintf(stderr, "%s: %v. Move or delete what is there, or give -staging another folder, then run %s.\n", name, err, resumeCommand(opts, folder.Dir))
		return 1
	case errors.As(err, &notUTF8):
		fmt.Fprintf(stderr, "%s: %v. Re-save the note as UTF-8, then run %s.\n", name, err, resumeCommand(opts, folder.Dir))
		return 1
	case errors.As(err, &unreadable):
		fmt.Fprintf(stderr, "%s: %v. Once the limit has reset (claude's /usage says when), run %s.\n", name, unreadable, resumeCommand(opts, folder.Dir))
		return 1
	case errors.Is(err, context.Canceled):
		fmt.Fprintf(stderr, "%s: interrupted. Run %s to go on.\n", name, resumeCommand(opts, folder.Dir))
		return 1
	case err != nil:
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return 1
	case folder.Complete(run.StageOutDir):
		total, err := modelstep.Totals(folder.Dir)
		if err != nil {
			fmt.Fprintf(stderr, "%s: the notes are staged in %s, but the run folder %s is kept, as its usage could not be added up: %v\n", name, opts.staging, folder.Dir, err)
			return 1
		}
		if err := folder.Delete(); err != nil {
			fmt.Fprintf(stderr, "%s: the notes are staged in %s, but the run folder could not be deleted: %v\n", name, opts.staging, err)
			return 1
		}
		fmt.Fprintf(stderr, "%s: staged the notes in %s, with the name-clash list %s and the run report %s. The run's %d model calls cost $%.2f and took %.1f minutes, added up. The run folder %s is deleted.\n", name, opts.staging, filepath.Join(opts.staging, stageout.ClashListFile), filepath.Join(opts.staging, stageout.RunReportFile), total.Calls, total.CostUSD, total.Seconds/60, folder.Dir)
		return 0
	case stopped != "":
		fmt.Fprintf(stderr, "%s: stopped after %s, as -stop-after asked. Run %s to go on.\n", name, stopped, resumeCommand(opts, folder.Dir))
		return 0
	}
	return 0
}

// printStop prints summary, what command name says when a gate stops the
// run, and writes it to the run folder's StoppedFile, then says where that
// is, or that it could not be written.
func printStop(stderr io.Writer, name string, folder *run.Folder, summary string) {
	fmt.Fprint(stderr, summary)
	if err := folder.WriteStopped(summary); err != nil {
		fmt.Fprintf(stderr, "%s: the summary could not be written to the run folder: %v\n", name, err)
		return
	}
	fmt.Fprintf(stderr, "This summary is also in %s.\n", folder.StoppedPath())
}

// stopSummary is what command name says when the gate stop e halts the run:
// the gate, its segments and report, then its items and what to do about
// them, or its Fix, then resume, the command that resumes the run.
func stopSummary(name string, e *gate.Stopped, resume string) string {
	switch {
	case len(e.Items) > 0:
		return fmt.Sprintf("%s: %v.\n\n%s\nThen run %s.\n", name, e, e.Summary(), resume)
	case e.Fix != "":
		return fmt.Sprintf("%s: %v. %s, then run %s.\n", name, e, e.Fix, resume)
	}
	return fmt.Sprintf("%s: %v. Then run %s.\n", name, e, resume)
}

// resumeCommand is the command that resumes the run in dir with opts, but
// for -stop-after: resume reads no choice from the run folder, so it names
// each flag given a value other than its default.
func resumeCommand(opts options, dir string) string {
	args := []string{"s2n", "resume"}
	if opts.stage1Model != extract.DefaultModel {
		args = append(args, "-stage-1-model", opts.stage1Model)
	}
	if opts.extractEffort != extract.DefaultEffort {
		args = append(args, "-extract-effort", opts.extractEffort)
	}
	if opts.repairModel != opts.stage1Model {
		args = append(args, "-repair-model", opts.repairModel)
	}
	if opts.repairReply != repair.DefaultReply {
		args = append(args, "-repair-reply", string(opts.repairReply))
	}
	if opts.quoteOnce != quoteonce.Default {
		args = append(args, fmt.Sprintf("-quote-once=%t", opts.quoteOnce))
	}
	if opts.proposerModel != lossproposer.Model {
		args = append(args, "-proposer-model", opts.proposerModel)
	}
	if opts.stage2Model != split.DefaultModel {
		args = append(args, "-stage-2-model", opts.stage2Model)
	}
	if opts.stage3Model != format.DefaultModel {
		args = append(args, "-stage-3-model", opts.stage3Model)
	}
	if opts.fiveHourUSD != usagelimit.DefaultFiveHourUSD {
		args = append(args, "-five-hour-usd", strconv.FormatFloat(opts.fiveHourUSD, 'f', -1, 64))
	}
	if opts.vault != opts.vaultDefault {
		args = append(args, "-vault", opts.vault)
	}
	if opts.staging != "" && opts.staging != stageout.DefaultStaging(opts.vault, opts.sourceName) {
		args = append(args, "-staging", opts.staging)
	}
	return strings.Join(append(args, dir), " ")
}

// newRun makes a new run folder for the source at path, in runDir or else
// under the default root, prints its path and copies the source into it.
func newRun(path, runDir string, stdout io.Writer) (*run.Folder, *source.Source, error) {
	src, err := source.Read(path)
	if err != nil {
		return nil, nil, err
	}
	var folder *run.Folder
	if runDir != "" {
		folder, err = run.New(runDir)
	} else {
		var root string
		if root, err = run.DefaultRoot(); err == nil {
			folder, err = run.NewIn(root, src.Name, time.Now())
		}
	}
	if err != nil {
		return nil, nil, err
	}
	fmt.Fprintln(stdout, folder.Dir)
	if err := folder.SaveSource(src.Name, []byte(src.Frame)); err != nil {
		return nil, nil, err
	}
	return folder, src, nil
}

// openRun opens the run folder at dir and reads the run's copy of its source.
func openRun(dir string) (*run.Folder, *source.Source, error) {
	folder, err := run.Open(dir)
	if err != nil {
		return nil, nil, err
	}
	if folder.Complete(oldCheckpointDir) && !folder.Complete(run.KeptLossDir) {
		cp := filepath.Join(folder.Dir, oldCheckpointDir)
		return nil, nil, fmt.Errorf("the run folder %s stopped at the checkpoint for review, which s2n no longer has. Resuming it would add every proposed loss to step 1.4's notes and ignore the edits and keep boxes in %s. Start a new run with s2n run, or delete %s to resume without them", folder.Dir, cp, cp)
	}
	path, err := folder.SourcePath()
	if err != nil {
		return nil, nil, err
	}
	src, err := source.Read(path)
	if err != nil {
		return nil, nil, err
	}
	return folder, src, nil
}

// oldCheckpointDir is where a run folder made before the run became
// unattended stopped for the checkpoint's review.
const oldCheckpointDir = "checkpoint"

// options are a run's choices from the command line.
type options struct {
	// stage1Model is the model for the steps run at stage 1's tier, and
	// extractEffort step 1.1's effort level.
	stage1Model, extractEffort string
	// repairModel and repairReply are the model and reply shape of step
	// 1.4's repair call and the kept-loss repair.
	repairModel string
	repairReply repair.Reply
	// quoteOnce has the model steps that write statements write them
	// quote-once, and s2n write their traces.
	quoteOnce bool
	// proposerModel is the model for both runs of step 1.5.
	proposerModel string
	// stage2Model is the model for step 2.1, and stage3Model for step 3.1.
	stage2Model, stage3Model string
	// fiveHourUSD is the dollars at API rates the 5-hour usage limit holds.
	fiveHourUSD float64
	// vault is the vault step 3.2 stages into, or "", and vaultDefault
	// -vault's default, from the environment. staging is the staging
	// folder, given or defaulted once the source is read, and sourceName
	// the source's name.
	vault, vaultDefault, staging, sourceName string
	// stopAfter names the step after which the run stops, or is "".
	stopAfter string
}

func joinReplies() string {
	var names []string
	for _, r := range repair.Replies {
		names = append(names, string(r))
	}
	return strings.Join(names, ", ")
}

// step is one step of the run: the directory that marks it complete, and
// what runs it.
type step struct {
	dir string
	run func() error
}

// stepDirs gives the directories of the run's steps, in order.
func stepDirs() []string {
	var dirs []string
	for _, s := range steps(context.Background(), nil, nil, options{}, nil) {
		dirs = append(dirs, s.dir)
	}
	return dirs
}

// runSteps runs every step of folder's run that isn't complete, in order. A
// step keeps only the finished replies of a partial directory a stopped
// attempt left, and calls the model for the rest (run.Folder.Begin).
// The run stops after the step opts.stopAfter names, run or already complete,
// and stopped is the step it stopped after. It is "" for a run that went to
// the end.
func runSteps(ctx context.Context, p provider.Provider, src *source.Source, opts options, folder *run.Folder) (stopped string, err error) {
	for _, s := range steps(ctx, p, src, opts, folder) {
		if !folder.Complete(s.dir) {
			if err := s.run(); err != nil {
				return "", err
			}
		}
		if s.dir == opts.stopAfter {
			return s.dir, nil
		}
	}
	return "", nil
}

// reachesStageOut says whether the run in folder goes on to step 3.2 unless
// a gate or an error stops it: step 3.2 isn't complete, and -stop-after names
// no step before it.
func reachesStageOut(folder *run.Folder, opts options) bool {
	return !folder.Complete(run.StageOutDir) && (opts.stopAfter == "" || opts.stopAfter == run.StageOutDir)
}

// steps gives the run's steps, in order.
func steps(ctx context.Context, p provider.Provider, src *source.Source, opts options, folder *run.Folder) []step {
	return []step{
		{run.ExtractDir, func() error {
			return extract.Run(ctx, p, src, opts.stage1Model, opts.extractEffort, opts.quoteOnce, folder)
		}},
		{run.MechanicalCheckDir, func() error { return mechcheck.Run(src, folder) }},
		{run.SupportCheckDir, func() error { return supportcheck.Run(ctx, p, src, folder) }},
		{run.ReadsAloneDir, func() error { return readsalone.Run(ctx, p, src, folder) }},
		{run.RepairDir, func() error {
			return repair.Run(ctx, p, src, opts.repairModel, opts.repairReply, opts.quoteOnce, folder)
		}},
		{run.MechanicalRecheckDir, func() error { return repair.RecheckMechanical(src, folder) }},
		{run.SupportRecheckDir, func() error { return repair.RecheckSupport(ctx, p, src, folder) }},
		{run.ReadsAloneRecheckDir, func() error { return repair.RecheckReadsAlone(ctx, p, src, folder) }},
		{run.StillFailsDir, func() error { return repair.WriteStillFails(src, folder) }},
		{run.LossProposerDir, func() error { return lossproposer.Run(ctx, p, src, opts.proposerModel, folder) }},
		{run.KeptLossDir, func() error {
			return keptloss.Run(ctx, p, src, opts.repairModel, opts.repairReply, opts.quoteOnce, folder)
		}},
		{run.FinalDir, func() error { return keptloss.WriteFinal(src, folder) }},
		{run.SplitDir, func() error { return split.Run(ctx, p, src, opts.stage2Model, folder) }},
		{run.SplitGatesDir, func() error { return splitgates.Run(src, folder) }},
		{run.StripDir, func() error { return striptraces.Run(src, folder) }},
		{run.FormatDir, func() error { return format.Run(ctx, p, src, opts.stage3Model, folder) }},
		{run.FormatGateDir, func() error { return formatcheck.Run(src, folder) }},
		{run.RunReportDir, func() error { return runreport.Run(src, folder) }},
		{run.StageOutDir, func() error { return stageout.Run(src, folder, opts.vault, opts.staging) }},
	}
}
