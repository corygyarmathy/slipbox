# ADR 0003: Notes are extractive, and their traces live only inside the run

- **Status:** Proposed
- **Date:** 2026-10-21
- **Related Artefacts:**
    - Vocabulary: [`GLOSSARY.md`](../../GLOSSARY.md)
    - Evidence: [Trace lifetime](https://github.com/corygyarmathy/source-to-notes/issues/31) (private)

## Context

The notes have to say what the source says, not what a model thinks it says. A model asked to summarise invents: in the trials Haiku reworded joins and wrote wrong name spans, and summaries can't be checked against the text. A statement built from verbatim quotes can be checked mechanically, by matching each quote, and judged on its quote alone. But the vault notes are for reading and linking, and footnoting every sentence back to the chapter is clutter there that no one re-checks.

## Decision

1. Every statement is built from contiguous verbatim quotes of its segment. The only new wording is joining text, and a name span is the only quote that may come from elsewhere in the frame or from the source's bibliographic record.
2. Each quote is a **trace**: a footnote naming its section and quoting the source. Traces are load-bearing from stage 1 until stage 2's gates, which pair statements by their quote sets.
3. Step 2.2 then strips every trace, and gives each atomic note one **source footnote** in their place. Stage 3, stage out and the vault never see a trace, and no traced copy is archived.

## Consequences

**Positive**

- Losing or inventing a statement in stages 1–2 is detected deterministically, not by judgement.
- Vault notes carry one footnote each, saying where they came from.

**Negative**

- A vault note can't be re-checked against its quotes later. The audit trail serves the run's gates, and nothing after them.

## Alternatives considered

- **Abstractive notes checked by a judge.** Rejected: nothing deterministic could catch an invention.
- **Keep traces in the vault notes.** Rejected: clutter that nothing reads after stage 2.
- **Archive a traced copy beside the vault.** Rejected: nothing re-checks it.
