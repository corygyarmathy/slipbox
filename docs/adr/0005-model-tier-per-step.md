# ADR 0005: Each model step runs at the lowest tier that passed its trial

- **Status:** Proposed
- **Date:** 2026-10-21
- **Related Artefacts:**
    - Evidence: [Cheaper-tier trial](https://github.com/corygyarmathy/source-to-notes/issues/28) (private), [Cost and model per step](https://github.com/corygyarmathy/source-to-notes/issues/29) (private), [Re-trial step 2.1 below Opus](https://github.com/corygyarmathy/source-to-notes/issues/87) (private)

## Context

The steps differ in what a mistake costs. Stage 1 is the only place where an argument can be lost or invented. Stage 2 moves statements behind gates that catch a lost or invented trace. Stage 3 adds markup behind a gate that catches any change of wording. Checking and proposing are judgements whose mistakes the next step absorbs. Paying the strongest tier everywhere would cost more and buy nothing where a gate already guards the output.

## Decision

| Step | Tier | Why |
|---|---|---|
| 1.1 Extract, 1.4 repair, kept-loss repair | Opus | Haiku invents (reworded joins, wrong name spans). A loss is tolerable, but an invention breaks extraction. Sonnet is the tested fallback when quota runs short. On step 1.4, Sonnet with whole notes broke traces and cut content. |
| 1.3a, 1.3b checks | Sonnet | Reads alone at Sonnet over-flags (29 flags for 7 real ones), but the repair pass absorbs false alarms cheaply. |
| 1.5 Loss proposer | Sonnet, run twice | See ADR 0008. |
| 2.1 Split + hub | Sonnet | On Ch 4, the edit check found 2 clear faults in Opus's name-span edits, none in Sonnet's and about 5 in Haiku's. Sonnet cost $1.56, against $2.85 for Opus and $0.20 for Haiku. |
| 3.1 Format | Haiku | 43 notes changed with 0 text changes, behind a gate on wording. |

`-repair-model` and `-proposer-model` override a tier for one run, for trials.

## Consequences

**Positive**

- Ch 4's quote-once run cost $8.70 at API rates through step 2.1, $7.14 of it in stage 1. Splitting on Opus would have made it $9.98. Cost flips no tier.

**Negative**

- Each tier ran once, on one chapter. Stage 2's comparison was one split each, graded by an edit check that is itself noisy.
- Stage 3 was trialled on traced notes and now reads stripped ones. That's untested on Haiku, though its gate catches any change of wording.
