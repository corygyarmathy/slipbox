# ADR 0011: The run waits out usage limits, and starts no call that won't fit

- **Status:** Proposed
- **Date:** 2026-10-21
- **Related Artefacts:**
    - Evidence: [Model and tool options](https://github.com/corygyarmathy/source-to-notes/issues/27) (private)

## Context

On a Pro plan, a run draws on a 5-hour usage limit and a weekly one, and a book's chapters run back to back will reach them. The first real run of Ch 4 hit the 5-hour limit after $6.81 of calls at API rates. The refused call cost nothing, but the run exited 1 and waited for someone to resume it, which an unattended run can't count on (ADR 0009). The runner also cancelled every call running beside a failed one, so the replies those calls were partway through, already paid for, were lost.

The usage is readable. Each response carries the account's utilization of each usage limit and its reset time, and `claude -p --output-format stream-json` passes them on as a `rate_limit_event`, whose `unifiedWindows` holds both limits. claude.ai's `api/oauth/usage` gives the same before any call. A 5-hour limit holds about $10 at API rates. That figure came from trial calls, and no figure exists yet for the weekly limit. Each call's API-rate cost is known after the call, and before it the call's prompt is fully built.

## Decision

1. Every call's usage record keeps the reading of each usage limit as the call's response began: utilization and reset time, from its `rate_limit_event`. That reading doesn't hold what the call spent, nor any call that ended after it was taken. The run takes its latest reading from the usage logs in the run folder, so a resume keeps it. Before a run's or a resume's first call, `s2n` reads the account's usage from `api/oauth/usage`, with Claude Code's own OAuth token. It sends the token only there, and never logs or refreshes it. If that read fails, the first call starts unchecked.
2. Before each call, the run predicts the call's cost from its prompt's size: at the step's own rate once it has made a call, and at a rate measured on the trial runs for its first. It starts the call only if the 5-hour limit has room for it, for every call still running and for every call that ended since the reading, at a fixed number of dollars per limit. That figure is Pro's, and a flag overrides it. The weekly limit, and any per-model limit, is not predicted. The run waits for it only when its reading shows no room.
3. A call that won't fit isn't started. The run waits until the latest reset among the limits without room, however far off, and says which limit it is waiting for and until when. Overage counts as no room: a run never spends past a usage limit.
4. A call refused for a usage limit is waited out in the same way, and called again after the reset. It isn't a retry (ADR 0009 §3): it was never answered. It doesn't cancel the calls running beside it, which finish and keep their replies. A refusal with no reset still to come is waited out for a minute, and a few in a row stop the run, since a limit that should have reset and still refuses can't be waited out. Any failure `s2n` doesn't recognise stops the run, as before. Network and overload errors are left to `claude -p`'s own retries.

## Consequences

**Positive**

- A book's run doesn't stop at a usage limit, and no call is started that the limit would refuse or cut short.
- Waiting stops the spend on calls that can't finish. The only spend a reset costs is the step's cache, written again by the first call after it.

**Negative**

- The prediction rests on `unifiedWindows` and `api/oauth/usage`, neither of them documented. If `unifiedWindows` goes, a run can still wait out a refusal, but it can't predict one. If `api/oauth/usage` goes, a run's first call starts unchecked.
- The dollars a 5-hour limit holds were measured once, on Pro, and Anthropic can change the figure without notice. If the figure is too low, the run waits early. If it is too high, the run reacts to the limit rather than predicting it.
- `s2n` reads a credential it doesn't own.
- A run that waits for a weekly limit can sit for days.

## Alternatives considered

- **Exit at the limit, and resume by hand.** Rejected: an unattended run would sit idle until someone came back to it.
- **Only react: wait after a refusal.** Rejected: if the limit cuts a call off partway through, the spend on it is lost, and nothing would stop a run from starting one.
- **Check the whole step before it starts.** Rejected: a step that needs more than is left would wait out a reset, and the rest of the limit would go unused.
- **Learn the dollars a limit holds from the run's own readings.** Rejected: utilization comes in steps of 1%, and the measured figure was enough. If the figure drifts, the flag sets it.
- **A probe call to get the first reading.** Rejected: the account already reports its usage.
- **Remember the last reading between runs.** Rejected: it would be stale, and it would need a store outside the run folder.
