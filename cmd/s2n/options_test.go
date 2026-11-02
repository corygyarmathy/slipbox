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

	"github.com/corygyarmathy/slipbox/internal/extract"
	"github.com/corygyarmathy/slipbox/internal/format"
	"github.com/corygyarmathy/slipbox/internal/lossproposer"
	"github.com/corygyarmathy/slipbox/internal/provider"
	"github.com/corygyarmathy/slipbox/internal/quoteonce"
	"github.com/corygyarmathy/slipbox/internal/repair"
	"github.com/corygyarmathy/slipbox/internal/run"
	"github.com/corygyarmathy/slipbox/internal/split"
	"github.com/corygyarmathy/slipbox/internal/stageout"
	"github.com/corygyarmathy/slipbox/internal/testfixture"
	"github.com/corygyarmathy/slipbox/internal/usagelimit"
)

// readStored reads the options recorded in the run folder runDir.
func readStored(t *testing.T, runDir string) storedOptions {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(runDir, run.OptionsFile))
	if err != nil {
		t.Fatal(err)
	}
	var s storedOptions
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		t.Fatalf("%s: %v", run.OptionsFile, err)
	}
	return s
}

// resumeWith runs `s2n resume` with args, then the run folder runDir, and
// gives its exit code and stderr.
func resumeWith(t *testing.T, p provider.Provider, runDir string, args ...string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runCommand(context.Background(), append(append([]string{"resume"}, args...), runDir), &stdout, &stderr, p)
	return code, stderr.String()
}

// A run records every flag that shapes it, but -stop-after, with the vault
// as $S2N_VAULT gave it and made absolute, and the staging folder "" while
// it defaults from the vault.
func TestRun_RecordsItsOptions(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(VaultEnv, "vault")
	runDir, _, _ := lampRun(t, nil, "-stage-1-model", "sonnet", "-extract-effort", "low", "-repair-reply", "whole", "-quote-once=false",
		"-proposer-model", "opus", "-stage-2-model", "haiku", "-stage-3-model", "sonnet", "-five-hour-usd", "35", "-keep", "-stop-after", run.ExtractDir)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want := storedOptions{
		Stage1Model: "sonnet", ExtractEffort: "low", RepairModel: "sonnet", RepairReply: "whole", QuoteOnce: false,
		ProposerModel: "opus", Stage2Model: "haiku", Stage3Model: "sonnet", FiveHourUSD: 35,
		Vault: filepath.Join(wd, "vault"), Staging: "", Keep: true,
	}
	if got := readStored(t, runDir); got != want {
		t.Errorf("recorded %+v, want %+v", got, want)
	}
}

// A run given non-default models, stopped by -stop-after, resumes with no
// flags on the models it was given: each resumed step's requests name its
// recorded model.
func TestResume_UsesTheRecordedModels(t *testing.T) {
	useState(t)
	runDir, _, stderr := lampRun(t, lampEveryStep(), "-stage-1-model", "sonnet", "-repair-model", "haiku", "-proposer-model", "opus",
		"-stage-2-model", "haiku", "-stage-3-model", "sonnet", "-vault", emptyVault(t), "-stop-after", run.ExtractDir)
	if want := "Run s2n resume " + runDir + " to go on."; !strings.Contains(stderr, want) {
		t.Errorf("stderr %q does not say %q", stderr, want)
	}
	p := lampModel(t, runDir, lampEveryStep())
	if code, stderr := resumeWith(t, p, runDir); code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, stderr)
	}
	for step, model := range map[string]string{
		run.RepairDir:       "haiku",
		run.KeptLossDir:     "haiku",
		run.LossProposerDir: "opus",
		run.SplitDir:        "haiku",
		run.FormatDir:       "sonnet",
	} {
		calls := stepCalls(p, step)
		if len(calls) == 0 {
			t.Errorf("the resume made no call for step %s", step)
		}
		for _, c := range calls {
			if c.Model != model {
				t.Errorf("step %s's call: model %q, want the recorded %q", step, c.Model, model)
			}
		}
	}
}

// A resume given a fixed flag with a value other than the recorded one is a
// usage error, naming the flag and both values, and makes no model call and
// leaves the run folder's stop and options as they were. Given the recorded
// value, it is accepted.
func TestResume_RefusesAChangedModel(t *testing.T) {
	runDir := lampAt(t, run.ExtractDir)
	stopped := filepath.Join(runDir, run.StoppedFile)
	if err := os.WriteFile(stopped, []byte("a stop\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readStored(t, runDir)
	p := testfixture.Serve(t)
	code, stderr := resumeWith(t, p, runDir, "-stage-2-model", "haiku")
	if code != 2 || len(p.Calls()) != 0 {
		t.Fatalf("resume with another -stage-2-model: exit %d with %d calls, want 2 with none: %s", code, len(p.Calls()), stderr)
	}
	for _, want := range []string{"-stage-2-model " + split.DefaultModel, "change it to haiku", "mix models within one run"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr %q does not say %q", stderr, want)
		}
	}
	if _, err := os.Stat(stopped); err != nil {
		t.Errorf("the refused resume removed %s: %v", run.StoppedFile, err)
	}
	if got := readStored(t, runDir); got != before {
		t.Errorf("the refused resume recorded %+v, want %+v as before", got, before)
	}

	if code, stderr := resumeWith(t, p, runDir, "-stage-2-model", split.DefaultModel, "-stop-after", run.ExtractDir); code != 0 {
		t.Errorf("resume with the recorded -stage-2-model: exit %d, want 0: %s", code, stderr)
	}
}

// A resume given -vault stages in that vault, and records it, so that a
// later resume with no flags stages there too. The staging folder, left to
// default, follows the new vault. The first resume finds the new vault's
// staging folder occupied, so that it stops before staging and leaves a
// later resume to do it.
func TestResume_KeepsANewVault(t *testing.T) {
	useState(t)
	old, vault := emptyVault(t), emptyVault(t)
	src := testfixture.LampSource(t)
	staging := stageout.DefaultStaging(vault, src.Name)
	occupant := filepath.Join(staging, "occupant.md")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(occupant, []byte("here first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runDir, _, _ := lampRun(t, nil, "-vault", old, "-stop-after", run.FormatGateDir)
	code, stderr := resumeWith(t, testfixture.Serve(t), runDir, "-vault", vault)
	if want := "the staging folder " + staging + " already holds occupant.md"; code != 1 || !strings.Contains(stderr, want) {
		t.Fatalf("resume with a new vault: exit %d, want 1 saying %q: %s", code, want, stderr)
	}
	if got := readStored(t, runDir).Vault; got != vault {
		t.Errorf("recorded vault %q, want the new %q", got, vault)
	}
	if err := os.Remove(occupant); err != nil {
		t.Fatal(err)
	}
	code, stderr = resumeWith(t, testfixture.Serve(t), runDir)
	if code != 0 {
		t.Fatalf("resume with no flags: exit %d, want 0: %s", code, stderr)
	}
	if want := "staged the notes in " + staging; !strings.Contains(stderr, want) {
		t.Errorf("stderr %q does not say %q", stderr, want)
	}
	if _, err := os.Stat(filepath.Join(staging, stageout.RunReportFile)); err != nil {
		t.Errorf("nothing staged in the new vault: %v", err)
	}
	if _, err := os.Stat(filepath.Join(old, stageout.StagingDir)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the old vault was staged into (stat: %v)", err)
	}
}

// A resume given -staging, -five-hour-usd or -keep records it in place of
// the run's value, and a later resume not given it keeps the recorded one.
func TestResume_RecordsTheFlagsItMayChange(t *testing.T) {
	runDir := lampAt(t, run.ExtractDir)
	staging := filepath.Join(t.TempDir(), "staging")
	if code, stderr := resumeWith(t, testfixture.Serve(t), runDir, "-staging", staging, "-five-hour-usd", "20", "-keep", "-stop-after", run.ExtractDir); code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, stderr)
	}
	want := readStored(t, runDir)
	if want.Staging != staging || want.FiveHourUSD != 20 || !want.Keep {
		t.Fatalf("recorded staging %q, five-hour-usd %v, keep %t, want %q, 20, true", want.Staging, want.FiveHourUSD, want.Keep, staging)
	}
	if code, stderr := resumeWith(t, testfixture.Serve(t), runDir, "-stop-after", run.ExtractDir); code != 0 {
		t.Fatalf("resume with no flags: exit %d, want 0: %s", code, stderr)
	}
	if got := readStored(t, runDir); got != want {
		t.Errorf("a resume with no flags recorded %+v, want %+v as before", got, want)
	}
}

// A resume given -repair-model "", which stands for the stage 1 model, is
// given the recorded repair model when the run's repair model was its stage
// 1 model, so it is accepted, though the default -stage-1-model differs.
func TestResume_AcceptsAnEmptyRepairModel(t *testing.T) {
	runDir, _, _ := lampRun(t, nil, "-stage-1-model", "sonnet", "-stop-after", run.ExtractDir)
	if code, stderr := resumeWith(t, testfixture.Serve(t), runDir, "-repair-model", "", "-stop-after", run.ExtractDir); code != 0 {
		t.Errorf("resume with -repair-model \"\": exit %d, want 0: %s", code, stderr)
	}
}

// A run folder whose options file isn't JSON stops the resume, naming the
// file, before any model call.
func TestResume_UnreadableOptions(t *testing.T) {
	runDir := lampAt(t, run.ExtractDir)
	options := filepath.Join(runDir, run.OptionsFile)
	if err := os.WriteFile(options, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := testfixture.Serve(t)
	code, stderr := resumeWith(t, p, runDir)
	if code != 1 || len(p.Calls()) != 0 {
		t.Fatalf("resume: exit %d with %d calls, want 1 with none: %s", code, len(p.Calls()), stderr)
	}
	if !strings.Contains(stderr, options) {
		t.Errorf("stderr %q does not name %s", stderr, options)
	}
}

// Every flag of s2n run is recorded, but -run-dir, which names the run
// folder the options are in, and -stop-after, which applies to one command
// only, so that a flag added later can't be left out of the record and a
// resume run it on its default.
func TestRun_RecordsEveryFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runCommand(context.Background(), []string{"run", "-h"}, &stdout, &stderr, testfixture.Serve(t)); code != 2 {
		t.Fatalf("s2n run -h: exit %d, want 2: %s", code, stderr.String())
	}
	runDir := lampAt(t, run.ExtractDir)
	b, err := os.ReadFile(filepath.Join(runDir, run.OptionsFile))
	if err != nil {
		t.Fatal(err)
	}
	var recorded map[string]any
	if err := json.Unmarshal(b, &recorded); err != nil {
		t.Fatal(err)
	}
	var flags int
	for line := range strings.Lines(stderr.String()) {
		name, ok := strings.CutPrefix(line, "  -")
		if !ok {
			continue
		}
		name, _, _ = strings.Cut(strings.TrimSpace(name), " ")
		if flags++; name == "run-dir" || name == "stop-after" {
			continue
		}
		if _, ok := recorded[strings.ReplaceAll(name, "-", "_")]; !ok {
			t.Errorf("-%s is not recorded in %s", name, run.OptionsFile)
		}
	}
	if flags == 0 {
		t.Fatalf("s2n run -h names no flag: %s", stderr.String())
	}
}

// A run folder with no options file, one made before runs recorded them,
// resumes from the flags it is given, as before, and is not given one. Its
// resume command names each flag given a value other than its default.
func TestResume_AFolderWithNoOptions(t *testing.T) {
	runDir := lampAt(t, run.FinalDir)
	options := filepath.Join(runDir, run.OptionsFile)
	if err := os.Remove(options); err != nil {
		t.Fatal(err)
	}
	p := lampModel(t, runDir, nil)
	code, stderr := resumeWith(t, p, runDir, "-stage-2-model", "haiku", "-stop-after", run.SplitDir)
	if code != 0 {
		t.Fatalf("resume: exit %d, want 0: %s", code, stderr)
	}
	calls := stepCalls(p, run.SplitDir)
	if len(calls) == 0 {
		t.Fatal("the resume made no step 2.1 call")
	}
	for _, c := range calls {
		if c.Model != "haiku" {
			t.Errorf("step 2.1's call: model %q, want haiku, as given", c.Model)
		}
	}
	if want := "Run s2n resume -stage-2-model haiku " + runDir + " to go on."; !strings.Contains(stderr, want) {
		t.Errorf("stderr %q does not say %q", stderr, want)
	}
	if _, err := os.Stat(options); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the resume wrote %s (stat: %v)", run.OptionsFile, err)
	}
}

// A run whose options are recorded is told to resume with no flag, however
// far its options are from the defaults.
func TestResumeCommand_Recorded(t *testing.T) {
	opts := options{stage1Model: "sonnet", extractEffort: "low", repairModel: "haiku", repairReply: "whole", quoteOnce: !quoteonce.Default,
		proposerModel: "opus", stage2Model: "haiku", stage3Model: "sonnet", fiveHourUSD: 35, vault: "w", staging: "elsewhere", keep: true, recorded: true}
	if got, want := resumeCommand(opts, "dir"), "s2n resume dir"; got != want {
		t.Errorf("resumeCommand = %q, want %q", got, want)
	}
	opts = options{stage1Model: extract.DefaultModel, extractEffort: extract.DefaultEffort, repairModel: extract.DefaultModel,
		repairReply: repair.DefaultReply, quoteOnce: quoteonce.Default, proposerModel: lossproposer.Model, stage2Model: split.DefaultModel, stage3Model: format.DefaultModel, fiveHourUSD: usagelimit.DefaultFiveHourUSD, keep: true}
	if got, want := resumeCommand(opts, "dir"), "s2n resume -keep dir"; got != want {
		t.Errorf("resumeCommand with no options recorded = %q, want %q", got, want)
	}
}
