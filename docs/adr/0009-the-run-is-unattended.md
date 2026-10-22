# ADR 0009: The run is unattended: it drops, falls back and reports, and stops only with nothing to fall back to

- **Status:** Proposed
- **Date:** 2026-10-21
- **Related Artefacts:**
    - Evidence: [Per-step verdict and checkpoints](https://github.com/corygyarmathy/source-to-notes/issues/15) (private), [Run a chapter unattended](https://github.com/corygyarmathy/source-to-notes/issues/104) (private), [A failing segment falls back](https://github.com/corygyarmathy/source-to-notes/issues/123) (private)

## Context

The run used to stop at a checkpoint after stage 1 for a human review, and at any failed gate. In the trial of an Opus proposer beside a Sonnet extractor, the maintainer found the checkpoint as much work per chapter as making the notes by hand, and acted on nothing in it but the losses. The run stopped five times, and none of the stops found a real fault in the notes. Three were `s2n` bugs, one was a missing vault, and one was a "reworded" grade on a correct name span. Meanwhile the procedure's guarantees don't need a human: an invention is caught deterministically, and a bad split can't change meaning.

## Decision

1. No one reviews a run. `s2n` runs every step from 1.1 to stage out without stopping for a person.
2. A model's judgement never stops the run. A flag that survives repair goes to the run report.
3. A deterministic failure stops only what failed:
    - In stage 1, an item that can't be applied is dropped, and so is a statement step 1.2 finds broken or untraced in the final notes.
    - In stage 2 or 3, a segment whose reply can't be written or that fails a gate **falls back** to the notes the stage before gave it, and every other segment goes on. A reply is never retried. A call refused for a usage limit was never answered, so it is waited out and called again ([ADR 0011](0011-the-run-waits-out-usage-limits-and-starts-no-call-that-wont-fit.md)).
4. The run stops only for a fault with nothing to fall back to: a segment folder no segment claims, or a stage 2 fallback that can't be stripped.
5. The **run report** says what was added, what still fails, what was dropped and what fell back. It needs no action. Choosing what to keep happens in stage 4.

## Consequences

**Positive**

- A chapter's run finishes, and no statement is lost to a fallback.
- Every stop is a real fault, and a resume reruns only what it stopped.

**Negative**

- If the unattended notes prove to lose or invent too much in use, stage 1 goes back to hand work, with stages 2–3 still automated.
- A fallback segment reaches the vault unsplit or unformatted, and fixing it is stage 4's work.

## Alternatives considered

- **Keep the checkpoint.** Rejected: it cost as much as hand notes, and found nothing.
- **Retry a failing segment.** Rejected: a second call costs as much as the first, and a fallback loses nothing. This is about a reply that came back bad. A call refused for a usage limit cost nothing, and ADR 0011 waits it out.
- **Stop on any failed gate.** Rejected: it stopped the trial run five times for no fault in the notes.
