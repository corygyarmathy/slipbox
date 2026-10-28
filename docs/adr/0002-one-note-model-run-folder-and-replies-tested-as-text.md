# ADR 0002: Every step shares one note model and one run folder, and tests check replies as text

- **Status:** Proposed
- **Date:** 2026-10-16
- **Related Artefacts:**
    - Builds on: [ADR 0001](0001-a-standalone-go-cli-in-its-own-repository.md)
    - Design: [`docs/design.md`](../design.md)
    - History: amended when the checkpoint was removed ([#104](https://github.com/corygyarmathy/source-to-notes/issues/104) (private)), when a resume began reusing finished segments ([#116](https://github.com/corygyarmathy/source-to-notes/issues/116) (private)), when stages 2–3 began falling back ([#123](https://github.com/corygyarmathy/source-to-notes/issues/123) (private)), when the note model's forms were named for what they do ([#157](https://github.com/corygyarmathy/source-to-notes/issues/157) (private)), when the references' recorded output was deleted from the fixture tree ([#159](https://github.com/corygyarmathy/source-to-notes/issues/159) (private)), when the trial chapters and their runs were deleted from it ([#171](https://github.com/corygyarmathy/source-to-notes/issues/171) (private)), and when tests stopped checking against a recorded run and began testing replies as text ([#11](https://github.com/corygyarmathy/slipbox/issues/11)). All seven are folded in.

## Context

The build was cut into tickets, one step or gate each, each accepted when its port matched its Python reference. Each reference parses notes its own way. A faithful port of each would have given four note parsers, four copies of the same fixtures, and a run sequence re-invented in each model step. The seams between tickets had to be fixed before the first one landed.

## Decision

1. **One note model.** One package parses every note, stage 1 to stage 3 and hubs alike, into frontmatter, H1, body lines and footnote definitions, each definition read as a trace or as the source footnote. Rendering gives back the input byte for byte. The forms text is compared in, such as the match form, are named functions beside it. No gate or step parses a note itself.
2. **One source model.** One package reads a source into its `source:` line, its frame and its numbered segments.
3. **Gates are functions, and the run does the I/O.** A ported check takes parsed notes and source and returns a report, and the run writes the report's rendered text. Each report rendered as its reference's standard output when its port was proven ([ADR 0001](0001-a-standalone-go-cli-in-its-own-repository.md) §4). The rendering is now s2n's own: a gate keeps its current text until it changes for another reason, and then renders plainly, without Python's `repr`, dicts or tuples.
4. **One model-step runner over one provider seam.** The seam takes model, system prompt and user prompt, and returns the reply and its usage. It has a `claude -p` adapter, and a fake for tests that serves the replies the test supplies. A step supplies only its brief, model and per-segment prompt.
5. **The run folder is the contract between steps.** Each step writes only its own directory and reads earlier steps'. A step that changes notes writes a new copy. A step writes to a partial directory and renames it when it's done, so a complete directory means a finished step.
6. **A resume reuses finished segments.** `s2n resume` runs the first step that isn't complete. Within it, a segment is finished if its raw reply is on disk, its step accepted the reply, and the hash of its request (model, effort, system prompt and user prompt) matches the request the step would make now. Only the other segments are called again.
7. **The briefs are embedded where they are.** `internal/briefs/` embeds the trialled `.md` files, so the bytes sent are those files, with no copy to drift.
8. **Tests check what the code does with a reply, not that it reproduces a recorded run.** To the code under test a reply is just markdown, so what a step does with one is tested apart from the request that produced it.
    - **Request building** is tested by targeted assertions: the system prompt is the brief, then the frame, then any material every call shares; the model and effort come from the flags and defaults; and the user prompt holds the segment and its notes. No test compares a request with a recorded run's.
    - **Reply handling** is tested, for each step, on a few reply cases written in its test: one well-formed reply, then one for each known deviation, each saying what it covers. When a real run turns up a new deviation, it's added as a minimal reply case.
    - **Deterministic output**, such as what stage-out writes, the format check's report and the run report, is tested on golden files under `testdata/`. They're regenerated with an `-update` flag, and their diff is reviewed in the pull request.
    - **End-to-end tests check orchestration only**: steps handing work on through the run folder, `resume`, `-stop-after`, usage-limit waits and the run folder's lifecycle, on short invented replies. What a check finds is for its own unit tests to say.
    - **Trials are evals** the maintainer runs when they choose. A trial produces a report to read, not a fixture that CI asserts against.

    Tests open golden files, and anything else under `testdata/`, only through one helper package. A reply case or golden file is invented, or trimmed from a run on a public-domain source. The ports were proven on their references' recorded output, and the references and that output were then deleted, so s2n's tests and package docs are the only statement of what it does. The trial chapters, the runs made from them, the references and their output are copyrighted or quote what is, so none is in this repository, and git history in the private archive holds them ([ADR 0001](0001-a-standalone-go-cli-in-its-own-repository.md) §8). A recorded run on a public-domain chapter served the tests until they were migrated off it, and was then deleted.

## Consequences

**Positive**

- A note-format fix is made once and reaches every gate.
- A port's proof needed no judgement: its report equalled the recorded output, or it didn't.
- A stopped run picks up where it stopped. A reply whose inputs changed, through a hand edit or a new brief, is called again.
- A change to a brief, a default, prompt assembly or a step's output breaks only the tests that assert on it, and they say what changed. None needs a paid re-run to fix.

**Negative**

- Reproducing Python's output formatting, `repr` included, was code that existed only so the proofs matched. The ported reports dropped it together, so none mixes Python's formatting with s2n's own, and a test of a report checks what it finds, not that it equals the recorded output.
- The ported gates keep their references' unit and count a body line as a statement, not a sentence ([#6](https://github.com/corygyarmathy/slipbox/issues/6)).
- The loss proposer's shared material holds every note, so any note change calls all its segments again.
- A hand fix to step 2.1's notes, followed by deleting its gates' directory, is the one place a human edits a step's output in place. After one, that run's stage 2 is no longer a model's output, so a reply case can't be trimmed from it.
- CI says nothing about whether the notes are good, or whether a real model still replies in the shapes the reply cases cover. Only a trial shows either, and only when the maintainer runs one. A new deviation is caught by a run, not a test, until it's added as a reply case.

## Alternatives considered

- **Port each check with its own parsing.** Rejected: four parsers to keep in step.
- **Hold the run in memory, or in one state file.** Rejected: a stop has to leave its evidence on disk, and a resume has to read it.
- **Compare a port to its reference on parsed findings.** Rejected: it needs a parser of each reference's output, and a judgement per check.
- **Copy each brief into the package that sends it.** Rejected: embedding leaves nothing to drift.
- **Test against a recorded run, request hashes and every file it wrote included.** Tried, and rejected: a change to a brief, a default model or effort, prompt assembly, or how any step writes what later steps build their prompts from, broke the tests, and fixing them took a paid re-run of the whole chapter. A failed hash didn't say what changed, and a byte-for-byte match said nothing about whether the notes were good.
- **Record s2n's own outputs on the trial runs as the new reference.** Rejected: it's a recorded run by another name, with the same costs, for checks simple enough to test on reply cases.
