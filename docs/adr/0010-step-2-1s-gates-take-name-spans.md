# ADR 0010: Step 2.1's gates take a name span where their references fail it

- **Status:** Proposed
- **Date:** 2026-10-21
- **Related Artefacts:**
    - Narrows: [ADR 0001](0001-a-standalone-go-cli-in-its-own-repository.md) §4, the acceptance rule for ported checks
    - Evidence: [Stage 2 gates fail every name-span edit](https://github.com/corygyarmathy/source-to-notes/issues/110) (private), [Step 2.1's gates stop a name-span edit whose trace stage 1 already has](https://github.com/corygyarmathy/source-to-notes/issues/128) (private)

## Context

Stage 2's brief allows a name span: when a split leaves a statement opening on a reference whose referent went to another note, the statement may quote the source's name for it, with a new trace. `split_check.py` fails every extra trace, and `trace_diff.py` fails every added quote. So every tier's split of Ch 4 failed the split check, and only on the name spans the brief asked for. Taking any added trace would instead let a statement of added traces alone through, or a stage 1 trace copied onto another statement.

## Decision

1. An added trace passes only as a name span: its quote is exact in the section it names, as step 1.2 grades an exact match. A fuzzy or broken match fails.
2. The split check also tests where a name span sits. It must be on a statement that carries one of the segment's stage 1 traces that isn't extra. A copy of a stage 1 trace passes only on an edited statement, one whose text no stage 1 statement of the segment has. Every copy on an unedited statement must be matched by a stage 1 statement with that text carrying it.
3. The trace diff runs only on segments the split check passed, and takes as many exact copies of a quote as were added.
4. Both reports list the name spans they took. Neither limits a span's length or judges what it says.
5. The two ports were proven by giving their reference's results on the trial outputs once their name-span lines were removed. From then on, their tests say what they do ([ADR 0001](0001-a-standalone-go-cli-in-its-own-repository.md) §4).

## Consequences

**Positive**

- Opus, Sonnet and Haiku all pass step 2.1's gates on Ch 4.
- An invented statement or a duplicated statement still fails, however reworded.

**Negative**

- These two ports are no longer byte-for-byte their references.
- Nothing checks what a name span says. The edit check that does isn't run ([#1](https://github.com/corygyarmathy/slipbox/issues/1)).
