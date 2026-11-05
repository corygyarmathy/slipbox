# ADR 0012: Stage 2 writes one note per argument

- **Status:** Proposed
- **Date:** 2026-11-05
- **Related Artefacts:**
    - Vocabulary: [`GLOSSARY.md`](../../GLOSSARY.md) (**Argument note**, **Aside**, **Name-clash list**, **Fallback**)
    - Amends: [ADR 0003](0003-extractive-notes-traced-only-inside-the-run.md) §3 and [ADR 0005](0005-model-tier-per-step.md)'s step 2.1 row, in wording only
    - Keeps: [ADR 0010](0010-step-2-1s-gates-take-name-spans.md), as a division can still need a name span
    - Evidence: [Notes from using s2n on a book](https://github.com/corygyarmathy/slipbox/issues/48), [Explore: one note per argument](https://github.com/corygyarmathy/slipbox/issues/50)

## Context

Stage 2 split each segment's stage 1 notes into atomic notes, about one stage 1 paragraph each, under a hub note per segment. That shape came from the procedure spec, and no trial tested it against how the notes get used. On four chapters of one book, 28 segments gave 193 staged notes, and none was kept as written. Each kept note was rebuilt from a segment's statements, so deciding what to cut from an atomic note meant reassembling its segment first: one contrast-shaped segment became eight atomic notes, and neither half of the contrast read correctly alone. No hub was used. The kept notes had a common shape: usually one note per segment, sometimes two, named after the argument's claim or the heading when the heading states it, with the segment's `###` sub-sections as headings, and numbered in book order.

## Decision

1. **Stage 2 writes argument notes.** Each holds one argument of a segment whole, its premises and its conclusion. A segment usually gives one, two when it makes two arguments. No argument note spans segments: two adjacent segments can only be in one call together if ADR 0004's one call per segment is broken, and a merge happened twice in 28 segments, so it is stage 4's work. There is no hub.
2. **An argument note is a contiguous run** of its segment's main-note statements, in stage 1 order. Each aside note becomes one argument note of its own, placed in book order after the argument it digresses from, and nothing in the output marks it as an aside.
3. **The step 2.1 call names the division, and code builds the notes.** Its reply gives each argument note's first statement and title, each aside's title and the argument it follows, and any name span the division needs. The model writes no statement, so it can't drop, duplicate or reword one. Every segment gets its call: whether a segment holds one argument, and whether its heading states the claim, are judgements.
4. **A name span still repairs a division**, under ADR 0010: when a statement refers to a referent in another note, the source's shortest span naming the referent replaces the referring words. If none does, the division is wrong.
5. **The title** is the segment's heading when the heading states the argument's claim, and otherwise the claim shortened in the source's words, in Title Case. It stays the one place stage 2 writes new words.
6. **Segments stay at `##`, and code puts the sub-sections back as headings.** While the traces still exist, a statement goes under the sub-section of its first quote, name spans aside. A heading is written wherever the sub-section changes, so it can repeat, and a sub-section with no kept statement gets none. Each heading moves one level up (`###` to `##`, `####` to `###`), as the H1 is the note's title. No model writes a heading.
7. **Book-order numbers name the files.** A segment's one note is `<n>. <title>.md`, and a segment with several, asides included, numbers them `<n>.1. `, `<n>.2. `, and so on, in book order. The H1 carries no number. The name-clash list compares a note's name less its number, against the slip box and against the run's other notes, since the number is dropped when a note moves into the slip box.
8. **The gates stay.** The split check, the trace diff and the strip check run on the notes code built, and check its moves and the name spans. The hub-footnote check goes with the hub.
9. **A reply that can't be used falls the segment back** (ADR 0009): first-statement numbers that don't start at 1 or don't rise, a missing title, an aside with no argument to follow, or a name span that can't be applied. Nothing is guessed. A fallback keeps its stage 1 notes undivided, titled after their headings, and still gets the sub-headings and the book-order numbers, so it sorts with the rest.
10. **Step 2.1 stays on Sonnet**, with `-stage-2-model` to override it, until the re-trial says otherwise. ADR 0005 chose Sonnet on its name-span edits, and the step's main job is now the division and the titles.

## Consequences

**Positive**

- A staged note is the unit stage 4 keeps or cuts, so it no longer has to be reassembled first.
- The reply is a few lines per segment, and most segments' is a title.
- A division can't lose or invent a statement, as code moves every one; the gates now guard the code and the name spans.
- The staging folder reads in book order.

**Negative**

- The brief is new and untrialled. Its trial is a re-trial on copies of the four chapters' run folders, resumed from step 2.1 and compared against the hand-kept notes. Its result goes on #50.
- The evidence is one reader's hand pass over four chapters of one book, matched by a word-overlap heuristic.
- An argument note can run long where a segment is long. Stage 4 cuts it.
- A segment's arguments that a book develops across chapters stay in separate notes until stage 4 ([#51](https://github.com/corygyarmathy/slipbox/issues/51)).

## Alternatives considered

- **Atomic notes under a hub.** Rejected: on four chapters none was kept as staged, and no hub was used.
- **Segment at `###`.** Rejected: more calls, and the kept notes used the sub-sections as structure inside one argument, not as arguments.
- **Merge adjacent segments in stage 2.** Rejected: it needs two segments in one call, against ADR 0004, for a case seen twice in 28 segments.
- **Skip the call for a segment with one argument.** Rejected: nothing deterministic tells one argument from two, or a heading that states a claim from one that doesn't.
- **Keep the model writing the notes as files.** Rejected: it copies every statement and footnote back, which is where a statement can be dropped or changed, for no judgement the division doesn't already carry.
- **Mark asides in the title.** Rejected: with no hub, an aside is an argument note like any other, and the vault has no use for the mark.
