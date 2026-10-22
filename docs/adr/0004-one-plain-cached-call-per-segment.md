# ADR 0004: Every model step is one plain call per segment, on a cached prefix

- **Status:** Proposed
- **Date:** 2026-10-21
- **Related Artefacts:**
    - Builds on: [ADR 0001](0001-a-standalone-go-cli-in-its-own-repository.md) §3
    - Evidence: [Token cost per chapter](https://github.com/corygyarmathy/source-to-notes/issues/26) (private), [Cache the shared notes](https://github.com/corygyarmathy/source-to-notes/issues/68) (private), [Effort level per step](https://github.com/corygyarmathy/source-to-notes/issues/69) (private), [claude -p loads MCP connectors](https://github.com/corygyarmathy/source-to-notes/issues/85) (private)

## Context

Every segment is written with the whole frame in view, so each step sends the same chapter once per segment. Run inside an agent session with tools, the harness overhead was 55–65% of the measured bill. Caching the frame is what makes that affordable, and the trials found two ways it silently stops working. `claude -p` writes user-prompt content to the cache again on every call, so shared material there is paid for in full each time. And the account's claude.ai connectors put about 3.6k tokens before the system prompt on some calls and not others, so later calls can't read the cache the first one wrote.

## Decision

1. A model step makes one call per segment, with no tools. The step's brief and the frame are the system prompt. A step whose calls all read the same notes puts those notes in the system prompt after the frame, so they're cached with it. The segment's own material is the user prompt.
2. Segment 0 runs first so its call writes the cache, then the rest run in parallel.
3. The reply carries its files in the brief's `=== FILE: … ===` shape, and `s2n` parses them. Orchestration is plain code and costs no tokens.
4. `claude -p` is passed `--strict-mcp-config` with no `--mcp-config`, so no MCP server loads.
5. Every step's effort level is pinned at `medium` and recorded in its usage log, so a run's thinking spend doesn't depend on whoever runs it. Only step 1.1's level was trialled: on Ch 4, `low` saved $0.99 but cut coverage from 88% to 84% and raised proposed losses from 7 to 11. `-extract-effort` overrides step 1.1's level for one run.

## Consequences

**Positive**

- After the first segment, each step pays mostly cache reads for the frame.
- A provider change only has to give one cached-prefix call per step and segment.

**Negative**

- A call can't use tools, so a repair can't look up and fix its own broken traces the way the trial agents did.
- Segment 0's latency sits before the rest of each step.
- The levels of steps other than 1.1 aren't trialled. `medium` is just what every recorded run used.

## Alternatives considered

- **An agent session with tools per step.** Rejected: harness overhead of 55–65% of the bill, and the run would no longer be deterministic.
- **All segments in parallel from the start.** Rejected: every call would write the cache.
