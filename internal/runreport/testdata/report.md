# Run report: Tiny

What the run's model calls used, then what s2n added, dropped and still flags in stage 1, and the segments that fell back in stages 2 and 3. Links are to the run folder, `/runs/Tiny/x`.

## Summary

- Covered sentences: 2/4 (50%) before kept losses, 3/4 (75%) after.
- Losses added: 1. Statements that still fail: 1. Dropped: 2. Fallbacks: 1 in stage 2, 1 in stage 3.

| Step | Models | Calls | Cost | Seconds |
|---|---|---|---|---|
| 1.1-extract | opus (medium): claude-haiku-5-5, claude-opus-5-5 | 2 | $1.25 | 96 |
| 1.4-repair | opus (medium) | 1 | $0.00 | 2 |
| 1.5-loss-proposer | sonnet (medium): claude-sonnet-5-5 | 4 | $0.50 | 60 |
| 1.4-kept-loss-repair | opus (medium): claude-opus-5-5; sonnet (medium): claude-sonnet-5-5 | 3 | $0.75 | 70 |
| 2.1-split-and-hub | sonnet (medium): claude-sonnet-5-5 | 2 | $0.38 | 40 |
| 3.1-format | haiku (medium): claude-haiku-5-5 | 3 | $0.12 | 20 |
| Total |  | 15 | $3.00 | 288 |

Cost is in dollars at API rates. Seconds are the calls' time added up, more than the run took when calls ran in parallel. The total row adds up the steps' unrounded figures, so it can differ from the rows above by a cent or a second.

## Losses added

- Loss 0.1: Beta is second.

## Statements that still fail

- 0 Tiny: Alpha is first.[^1] (1.3a weak: thin)

## Dropped

- 1.4-repair, segment 0, `0 Tiny.md`: the edit at "Zeta.": no statement reads it
- 1.4-kept-loss-repair, segment 1: loss 1.1: the kept-loss call added no statement

## Fallbacks

- 2.1-gates, segment 1 (Two): fell back to its stage 1 notes: the split check failed it; its report is [split-check.txt](<../2.1-gates/split-check.txt>)
- 3.1-gate, segment 0 (1. Tiny): kept its notes unformatted: step 3.1's reply wrote "A.md" twice; its notes are in [3.1-gate/stage-3/0 Tiny](<../3.1-gate/stage-3/0 Tiny>)

## Asides and name spans

- Asides: 0
- Name spans that point outside their segment, author name spans included: 0
