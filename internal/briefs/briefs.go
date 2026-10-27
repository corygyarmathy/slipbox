// Package briefs embeds the trialled system prompts where they are, so the
// bytes sent to the model are these files themselves, with no copy to drift
// (ADR 0002 §7). A brief stays as it was last trialled, and the maintainer
// decides when a change to one is worth a re-trial.
//
// The first seven are the trial files of personal-notes' prototype/cheaper-tier
// branch (commit dd6d3939), renamed: stage-1.md was RULES.md,
// checker-support.md CHECKER.md, checker-reads-alone.md READS-ALONE.md,
// proposer.md PROPOSER.md, stage-2.md STAGE2.md, stage-2-edit-check.md
// EDIT-CHECK.md and stage-3.md STAGE3.md. repair.md is the prompt the
// first-person rerun's orchestrator gave its repair agents
// (prototype/first-person-rerun), adapted for a call with no tools. The rest
// were written by s2n's builds, as each brief's doc says. Each brief's
// PROTOTYPE header line is part of what was run.
//
// stage-2-edit-check.md has no step: it graded stage 2's name-span edits by
// hand, and s2n doesn't run it.
package briefs

import _ "embed"

// The files of the briefs a model step is given. Each call's usage record
// names its step's, so a run folder says which brief each call had.
const (
	Stage1File                 = "stage-1.md"
	Stage1QuoteOnceFile        = "stage-1-quote-once.md"
	CheckerSupportFile         = "checker-support.md"
	CheckerReadsAloneFile      = "checker-reads-alone.md"
	RepairFile                 = "repair.md"
	RepairEditsFile            = "repair-edits.md"
	RepairEditsQuoteOnceFile   = "repair-edits-quote-once.md"
	KeptLossFile               = "kept-loss.md"
	KeptLossEditsFile          = "kept-loss-edits.md"
	KeptLossEditsQuoteOnceFile = "kept-loss-edits-quote-once.md"
	ProposerFile               = "proposer.md"
	Stage2File                 = "stage-2.md"
	Stage3File                 = "stage-3.md"
)

// Stage1 is step 1.1's brief, the extract generator's rules.
//
//go:embed stage-1.md
var Stage1 string

// Stage1QuoteOnce is step 1.1's brief for statements written quote-once:
// Stage1 with each quote written once, in its statement, and the joining text
// marked, so s2n writes the traces (package quoteonce). It is used as Stage1
// is, and is the default (ADR 0006).
//
//go:embed stage-1-quote-once.md
var Stage1QuoteOnce string

// CheckerSupport is step 1.3a's brief, the support and cuts check.
//
//go:embed checker-support.md
var CheckerSupport string

// CheckerReadsAlone is step 1.3b's brief, the reads-alone check.
//
//go:embed checker-reads-alone.md
var CheckerReadsAlone string

// Repair is step 1.4's brief, the repair pass. As trialled, the step's
// brief is Repair, a blank line, then Stage1, and the frame follows it.
//
//go:embed repair.md
var Repair string

// RepairEdits is step 1.4's brief for an edit-shaped reply: Repair with the
// reply given as edits to the notes rather than the changed notes in full. It
// is used as Repair is, and is the default reply shape (ADR 0007).
//
//go:embed repair-edits.md
var RepairEdits string

// RepairEditsQuoteOnce is RepairEdits with the new statements written
// quote-once. As trialled, it is followed by a blank line and Stage1QuoteOnce.
// It is the default (ADR 0006).
//
//go:embed repair-edits-quote-once.md
var RepairEditsQuoteOnce string

// KeptLoss is the kept-loss repair's brief: after step 1.5, the kept losses,
// every loss it proposed, go through a repair call of their
// own. The brief still says a human kept them, as trialled. Its brief is
// KeptLoss, a blank line, then Stage1, and the frame follows it. It was
// written by the resume build, and its first recorded run is its trial.
//
//go:embed kept-loss.md
var KeptLoss string

// KeptLossEdits is the kept-loss repair's brief for an edit-shaped reply:
// KeptLoss with the reply given as edits that add statements, used as KeptLoss
// is. It was written by the repair trial's build, and its first
// recorded run is its trial.
//
//go:embed kept-loss-edits.md
var KeptLossEdits string

// KeptLossEditsQuoteOnce is KeptLossEdits with the new statements written
// quote-once, followed by a blank line and Stage1QuoteOnce. It is the default
// (ADR 0006), and quote-once's trial was its first recorded run.
//
//go:embed kept-loss-edits-quote-once.md
var KeptLossEditsQuoteOnce string

// Proposer is step 1.5's brief, the loss proposer.
//
//go:embed proposer.md
var Proposer string

// Stage2 is step 2.1's brief, the split and hub.
//
//go:embed stage-2.md
var Stage2 string

// Stage2EditCheck is the brief for the stage 2 edit check.
//
//go:embed stage-2-edit-check.md
var Stage2EditCheck string

// Stage3 is step 3.1's brief, the format step.
//
//go:embed stage-3.md
var Stage3 string
