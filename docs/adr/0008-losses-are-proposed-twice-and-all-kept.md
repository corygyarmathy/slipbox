# ADR 0008: Losses are proposed by two runs, and every one is kept

- **Status:** Proposed
- **Date:** 2026-10-21
- **Related Artefacts:**
    - Evidence: [Coverage review at padded volume](https://github.com/corygyarmathy/source-to-notes/issues/21) (private), [Loss proposer trial](https://github.com/corygyarmathy/source-to-notes/issues/23) (private), [Cost and model per step](https://github.com/corygyarmathy/source-to-notes/issues/29) (private), [Explore Sonnet extraction with an Opus loss proposer](https://github.com/corygyarmathy/source-to-notes/issues/76) (private), [Run a chapter unattended](https://github.com/corygyarmathy/source-to-notes/issues/104) (private)

## Context

The selection rule drops most of a chapter, so the coverage check's list of uncovered sentences is long. Reading it by hand at padded volume was too slow. A proposer that reads the frame, every stage 1 note and its segment's uncovered list found 4 losses the hand review had missed, and the user kept 6 of its 6 items. But a single Sonnet run found 4 of 5 known losses, and a second run missed a different one. Once losses were proposed, choosing among them by hand at a checkpoint proved as much work per chapter as making the notes by hand, and in the trial of an Opus proposer every proposed item read as a real loss.

## Decision

1. Step 1.5 runs the proposer twice per segment that has an uncovered list. The second run is mostly a cache read. It keeps the union of the two runs' items. Items from different runs that cite any of the same uncovered text merge into one.
2. An item whose quotes cite no line of the uncovered list isn't a loss. It's a **remark**, listed in the run report as dropped.
3. Every loss is kept, with no review. The kept-loss repair adds each segment's losses as new traced statements, and may only insert.
4. The full uncovered list stays an on-demand audit file in the run folder, outside the run report.

## Consequences

**Positive**

- The proposer's recall decides what's added. Added statements are traced and checked, so a false positive costs length, not accuracy.
- No human reads the coverage list.

**Negative**

- The proposer finds losses but doesn't prove completeness. Its recall on padded, story-heavy text rests on a spot-check.
- The kept-loss briefs still say a human kept the losses, as trialled ([#4](https://github.com/corygyarmathy/slipbox/issues/4)).

## Alternatives considered

- **Reading the coverage list by hand.** Rejected: too slow at padded volume.
- **One proposer run.** Rejected: each run misses a different loss.
- **A human chooses among the proposed losses.** Rejected: the checkpoint cost as much as hand notes, and nothing proposed was rejected.
