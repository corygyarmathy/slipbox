# ADR 0007: Stage 1 is checked by separate calls and repaired once

- **Status:** Proposed
- **Date:** 2026-10-21
- **Related Artefacts:**
    - Evidence: [Cheaper-tier trial](https://github.com/corygyarmathy/source-to-notes/issues/28) (private), [Time trial on new material](https://github.com/corygyarmathy/source-to-notes/issues/17) (private), [Classifier trial](https://github.com/corygyarmathy/source-to-notes/issues/30) (private), [Repair trial](https://github.com/corygyarmathy/source-to-notes/issues/42) (private), [Repair trial: edit-shaped replies and Sonnet](https://github.com/corygyarmathy/source-to-notes/issues/70) (private)

## Context

A generator doesn't catch its own faults. A support fault (a statement its quote doesn't support) and a dangling reference both read fine to the call that wrote them. When the support, cuts and reads-alone checks shared one call, the reads-alone recall fell at every tier, Opus included. A repair loop that re-checks until clean could run forever on a checker's false alarms, and Sonnet's reads-alone check gave 29 flags for 7 real ones.

## Decision

1. Step 1.2, a deterministic check, matches every quote, finds untraced statements, lints punctuation and lists coverage.
2. Two model checks follow, each in its own call and never the generator's: **1.3a**, support and cuts, and **1.3b**, reads alone. Step 1.3b governs reads alone, so 1.3a's own dangling and outside-frame grades don't count.
3. Every flagged statement goes **once** to a repair call, with each check's report verbatim. The call may keep a statement whose flag it judges wrong. The segments it changes are re-checked, and what still fails goes to the run report. There is no second pass.
4. The repair is a fresh call with the stage 1 rules. It doesn't continue the generator's conversation. Its reply is **edits** to named statements rather than whole notes, and `s2n` applies them through the note model.

## Consequences

**Positive**

- On Ch 4, edits on Opus cut the repair's output from 61k to 22k tokens and its cost from $2.05 to $1.30, with no fail-type grade on re-check.
- In the trial, the repair kept every reads-alone false alarm and fixed the 3 real flags.

**Negative**

- The repair ran as a plain call once, on 3 segments. It has never repaired a real support fault, and with no tools it can't fix its own broken traces.
- `checker-support.md` still carries the old combined reads-alone judgement as output nothing uses ([#138](https://github.com/corygyarmathy/source-to-notes/issues/138) (private)).

## Alternatives considered

- **One checker call.** Rejected: reads-alone recall fell at every tier.
- **Classifiers (MiniCheck, Jev) as the support check.** Rejected: they missed every known support fault, the author's "I" and the coverage losses. Jev might still serve as a pre-filter ([#3](https://github.com/corygyarmathy/slipbox/issues/3)).
- **Repair until clean.** Rejected: false alarms would never converge.
- **Sonnet repairs.** Rejected: with whole notes it broke 2 traces, stitched quotes, cut content and deleted an aside. With edits it stopped on a malformed edit.
