# ADR 0006: The model writes each quote once, and s2n writes the traces

- **Status:** Proposed
- **Date:** 2026-10-21
- **Related Artefacts:**
    - Builds on: [ADR 0003](0003-extractive-notes-traced-only-inside-the-run.md)
    - Evidence: [Explore quote-once](https://github.com/corygyarmathy/source-to-notes/issues/71) (private)

## Context

As trialled, the generator wrote every quote twice: once in its statement and again in the statement's footnote. That's most of stage 1's output tokens, and output is the costliest part of an Opus call. The second copy carries nothing a program couldn't find. The statement already holds the quote, and the frame holds the section it came from.

## Decision

1. In step 1.1, step 1.4's repair and the kept-loss repair, the model writes each statement with its joining text in braces and no footnotes. That's the **quote-once** form, and the briefs are the `-quote-once` variants.
2. `s2n` finds each quote the way step 1.2 will: first in the segment's own section, then in the other segments in order, then in the `source:` line. It writes the trace with the source's own bytes.
3. A quote it can't find exactly is written as given, so step 1.2 grades it fuzzy or broken, the same as a quote the model wrote. Writing a quote doesn't repair it.
4. `-quote-once=false` restores the trialled form, with the model writing the footnotes.

## Consequences

**Positive**

- On Ch 4 at `medium`, step 1.1's output fell from 125k to 73k tokens and its cost from $2.80 to $1.77. All 344 traces `s2n` wrote were exact.

**Negative**

- Coverage went from 88% to 84%, and proposed losses from 7 to 6. Each figure rests on one run.
- Steps 1.3b and 1.5 still read traced notes, as they were trialled, so they don't share the saving ([#2](https://github.com/corygyarmathy/slipbox/issues/2)).
