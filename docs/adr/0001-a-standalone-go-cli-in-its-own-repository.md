# ADR 0001: The tool is a standalone Go CLI in its own repository

- **Status:** Proposed
- **Date:** 2026-10-12
- **Related Artefacts:**
    - Design: [`docs/design.md`](../design.md)
    - Vocabulary: [`GLOSSARY.md`](../../GLOSSARY.md)
    - History: written against the procedure spec, and amended when the run became unattended ([#104](https://github.com/corygyarmathy/source-to-notes/issues/104) (private)), when step 2.1's gates began taking name spans ([#110](https://github.com/corygyarmathy/source-to-notes/issues/110) (private)), when the ports, once proven, stopped following their references, when the fuzzy match's ratio was stated as s2n's own rule ([#156](https://github.com/corygyarmathy/source-to-notes/issues/156) (private)), when the references and their recorded output were deleted ([#159](https://github.com/corygyarmathy/source-to-notes/issues/159) (private)), when the repository was split into a public one and a private archive ([#150](https://github.com/corygyarmathy/source-to-notes/issues/150) (private)), when re-trialling a brief became the maintainer's call ([#10](https://github.com/corygyarmathy/slipbox/issues/10)), and when tests stopped replaying a recorded run ([#11](https://github.com/corygyarmathy/slipbox/issues/11)). All eight changes are folded in.

## Context

The procedure was decided and trialled in `corygyarmathy/personal-notes`, the Obsidian vault the notes are written into, and it left the harness, the provider and the code to the build. A tool doesn't belong in a vault: it has its own cadence, its own tests and its own tracker.

## Decision

1. **It's a tool for the operator's own use, written by agents.** The value is in the notes it produces, not in the code. Agents write the code and the operator reviews pull requests.
2. **It's a standalone CLI, `s2n`.** It's a deterministic runner that makes one model call per step and segment, runs the gates between them, and runs unattended to stage out ([ADR 0009](0009-the-run-is-unattended.md)). It isn't a Claude Code skill: harness overhead was 55–65% of the bill inside a session, and the gates must be deterministic.
3. **It calls the model through a narrow provider seam.** The first backend is `claude -p` on the operator's Pro subscription, in the trialled call shape ([ADR 0004](0004-one-plain-cached-call-per-segment.md)). An Anthropic API backend would be a second implementation of the seam, not a change to the pipeline.
4. **It's written in Go, using only the standard library.** The Python reference checks were ported, not shipped. Each port was proven by giving its reference's results on the trial outputs; step 2.1's gates were proven once their name-span lines were removed ([ADR 0010](0010-step-2-1s-gates-take-name-spans.md)). Once every port was proven, the references and their recorded output were deleted; git history in the private archive keeps them (§8). A check is s2n's own code: its tests and package doc are the only statement of what it does, and it changes wherever the procedure needs it to, with the reason in its package doc. Code is written in idiomatic Go.
5. **The fuzzy quote match is a Ratcliff/Obershelp `ratio()` written here.** It's 2·M/T over code points, where M is found by taking the longest common substring, earliest in the first string and then the second on a tie, and recursing on each side. Package `seqmatch` fixes that rule, ties included: the 0.90 threshold was calibrated on its numbers, so it keeps its trialled meaning only while they stay the same.
6. **The tool lives outside the vault and is pointed at it.** It writes only to the staging folder, which defaults to `<vault>/Staging/<source name>/`, and it reads `_Slip_Box` file names for the name-clash list.
7. **Tests never call a model.** They reach the provider seam through a fake that serves the replies the test supplies, so `go test` and CI are offline, deterministic and free. What the tests check, and on what, is [ADR 0002](0002-one-note-model-run-folder-and-replies-tested-as-text.md) §8.
8. **There are two repositories: a public one and a private archive.** The public repository holds the code, the briefs, the docs, and fixtures that are public domain or invented, such as reply cases and golden files ([ADR 0002](0002-one-note-model-run-folder-and-replies-tested-as-text.md) §8). It started from a clean tree, so no commit in it holds a trial chapter or anything a trial made from one; the briefs' phrases below are the one exception. The private archive, [`corygyarmathy/source-to-notes`](https://github.com/corygyarmathy/source-to-notes) (private), is the repository the tool was built in, kept unedited. It holds the three trial chapters, among them the Ch 4 that the ADRs and code comments cite, and every trial run made from them, which quote copyrighted text; the git history; the map and tickets that decided the procedure, transferred from `personal-notes` as issues 1–30; and every later issue and PR. The docs cite archive issues by full URL, marked private, and each sentence that cites one states its finding without it. The issues still open at the split were filed again in the public repository's tracker, rewritten to quote no trial text, and the docs cite those copies. The briefs quote a few short phrases from the trial chapters as examples of what a rule means. They stay because a brief stays as it was last trialled. Only the maintainer changes or removes one of those phrases, and re-trials the brief when they do; an agent doesn't.
9. **The working setup follows `afk-agent`'s, without vendored skills.** That means `AGENTS.md`, `docs/adr/`, `GLOSSARY.md`, a Nix dev shell, and CI running format, build, vet and offline tests on pull requests.

## Consequences

**Positive**

- A provider change, or wrapping the tool in `afk-agent`, adds code rather than rewriting it.
- A gate can change what it does on purpose without a carve-out from its reference.

**Negative**

- Every reference check had to be ported and proved, rather than tidied up and reused.
- With `claude -p` on Pro, cost per chapter can only be observed, not metered.
- Nothing re-checks a check against its reference, which is gone. Its tests and package doc are the only record of what it should do.
- A reader of the public repository can't open the trials an ADR cites. Each ADR states what its evidence showed, so it doesn't depend on the link.

## Alternatives considered

- **Keep the tool in `personal-notes`.** Rejected: the vault isn't a code repository, and its automatic backups would mix into the tool's history.
- **Python, tidying up the references.** Rejected: it would be the operator's only Python repository, and harder to package in Nix.
- **`pmezard/go-difflib` for the fuzzy match.** Rejected: a partial port, unmaintained for eight years.
- **A different similarity metric.** Rejected: it would mean re-calibrating a threshold that works.
- **Keep every port matching its reference, with a carve-out for each divergence.** Rejected: the gates now diverge on purpose. Each carve-out would make a planned change look like an exception, and reproducing Python's formatting is code no reader needs.
- **Rewrite this repository's history to remove the copyrighted text, and make it public.** Rejected: GitHub keeps each pull request's original commits under `refs/pull/<n>/head`, and removing them needs GitHub Support.
