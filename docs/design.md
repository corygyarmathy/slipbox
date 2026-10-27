# Design

`s2n` turns one chapter of a source into refined notes for an Obsidian vault,
in three model-run stages that run unattended. Every statement it writes is
built from verbatim source quotes and traced to them until the traces have
done their job. Losses are proposed and added as traced statements, rather
than taken on trust. Terms in **bold** are defined in
[`GLOSSARY.md`](../GLOSSARY.md), and the decisions are in [`adr/`](adr/).

## Input

A source is one Markdown file. Its frontmatter has a `source:` line that is the
source's citekey link, `"[[@<title> - <author>]]"`. That line names the author,
for author name spans, and becomes each output note's **source footnote**. The
text follows, with one paragraph per line and `##` headings for sections.

The **frame** is the whole chapter. The **segments** are the text before the
first `##` heading, if there is any, then each `##` section, numbered from 0
in file order. Every model step reads the whole frame and writes one segment
at a time.

## The run

Every model step makes one plain call per segment, with no tools. The step's
**brief** and the frame are the cached system prompt, and segment 0 runs first
so that it writes the cache ([ADR 0004](adr/0004-one-plain-cached-call-per-segment.md)).
The steps in between are deterministic code, and the **gates** among them
decide what goes on.

| Step | What it does | Run by | Package |
|---|---|---|---|
| 1.1 Extract | A main note and any aside notes per segment, made of quotes under the **selection rule** | Opus | `extract`, `quoteonce` |
| 1.2 Mechanical check | Matches every trace, finds untraced statements, lints punctuation, lists **coverage** | code | `mechcheck` |
| 1.3a Support + cuts | Grades each statement's support, cuts and name spans | Sonnet | `supportcheck` |
| 1.3b Reads alone | Flags references a reader can't resolve within the note | Sonnet | `readsalone` |
| 1.4 Repair | One repair call for each flagged segment, then steps 1.2–1.3 again | Opus | `repair` |
| 1.5 Loss proposer | Proposes each segment's **losses** from its **uncovered list**, in two runs, and keeps the union | Sonnet | `lossproposer` |
| Kept-loss repair | Adds every loss as traced statements, re-checks, and drops what's broken | Opus | `keptloss` |
| 2.1 Split + hub | Moves statements into **atomic notes** under a **hub note** | Sonnet | `split` |
| 2.1's gates | Split check, trace diff, strip check | code | `splitgates` |
| 2.2 Strip traces | Replaces the traces with one source footnote per note | code | `striptraces` |
| 3.1 Format | Adds or removes markup only | Haiku | `format` |
| 3.1's gate | Format check | code | `formatcheck` |
| 3.2 Stage out | Copies the notes, the **name-clash list** and the **run report** into the **staging folder** | code | `stageout` |

Each step's package doc says what it reads, what it writes and, for a check,
what it finds and why.
[ADR 0005](adr/0005-model-tier-per-step.md) gives the reason for each step's
tier, and [ADR 0007](adr/0007-stage-1-is-checked-by-separate-calls-and-repaired-once.md)
and [ADR 0008](adr/0008-losses-are-proposed-twice-and-all-kept.md) give the
shape of stage 1.

### Traces

Stage 1 is the only stage where an argument can be lost or invented, so it is
fully traced. The model writes each quote once, in braces-marked statements,
and `s2n` finds the quote in the frame and writes its trace
([ADR 0006](adr/0006-quote-once.md)). Stage 2's gates pair statements by
their quote sets, so a lost or invented statement fails deterministically.
Step 2.2 then strips every trace. Nothing after it, and nothing in the vault,
carries one ([ADR 0003](adr/0003-extractive-notes-traced-only-inside-the-run.md)).

### Unattended

No one reviews a run ([ADR 0009](adr/0009-the-run-is-unattended.md)):

- A model's judgement never stops it. A flag that survives repair goes to the
  run report.
- Stage 1 drops what fails deterministically: a reply's edit that can't be
  applied, or a statement still broken or untraced at the end.
- In stages 2 and 3, a segment that fails falls back to the notes the stage
  before gave it, and the other segments go on.
- The run stops only for a fault with nothing to fall back to.
- It waits out the account's **usage limits** rather than stop at one
  ([ADR 0011](adr/0011-the-run-waits-out-usage-limits-and-starts-no-call-that-wont-fit.md)).
  Every call's usage record keeps each limit's utilization and reset time
  as its response began, and before a run's or a resume's first call `s2n` reads the
  account's usage. Before each call it predicts the call's cost from its
  prompt's size, and starts it only if the 5-hour limit has room for it, for
  every call still running and for the calls that ended since the reading,
  taking the limit to hold `-five-hour-usd`
  dollars at API rates (Pro's $10 by default). The weekly limit, and any
  other, is waited for only once it is full. A call that won't fit, or one the
  account refuses, waits until the latest reset among the full limits, plus
  a minute, and the run says which limit and until when. The calls already
  running finish and keep their replies, and the first call after the wait
  runs alone to write the cache again. Ctrl-C stops a wait with the resume
  command, and a full limit whose reset time can't be read stops the run, as
  do a few refusals in a row after every reset time has passed.
  Packages `usagelimit` and `modelstep` say how.

The run report says what was added, what still fails, what was dropped and
what fell back. Choosing what to keep is stage 4.

### The run folder

Each run has a **run folder** outside the vault, which is the only channel
between steps ([ADR 0002](adr/0002-one-note-model-run-folder-and-fixture-tree.md)).
Each step writes only its own directory, and a complete directory means a
finished step. `s2n resume` picks up at the first step that isn't complete,
and reuses the segments that step had already finished. Package `run`
documents the layout. A successful run deletes the folder once its notes are
staged.

## Code

- **One note model** (`note`) and **one source model** (`source`). Nothing else
  parses a note or a source.
- **Gates are functions** (`gate`). A check takes parsed notes and returns a
  report, and its tests say what it does.
- **One model-step runner** (`modelstep`) over **one provider seam**
  (`provider`). The seam has a `claude -p` adapter for real runs and a replay
  adapter for tests, so tests never call a model.
- **The briefs** (`internal/briefs`) are the files as last trialled, embedded
  byte for byte. The maintainer decides when a change to one is worth a
  re-trial.
- **Fixtures** live under `testdata/`: a public-domain chapter in `sources/`,
  and its recorded run, copied whole with its provenance, and hand-written
  replies in `trials/`.

## Out of scope

- Getting a source's text.
- Stage 4: integrating notes into the vault, linking chapters and resolving
  name clashes. That's the user's own work.
- A source too long to give whole ([#5](https://github.com/corygyarmathy/slipbox/issues/5)).
