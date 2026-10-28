# Agent instructions

`slipbox` builds `s2n`, a CLI that turns a source's text into traced,
refined notes: three model-run stages, run unattended. Go, using only
the standard library (ADR 0001).

## Where things are

- [`docs/design.md`](docs/design.md) - how the tool works: the steps, traces,
  the unattended run and the run folder. Each step's package doc holds its
  detail.
- [`internal/briefs/`](internal/briefs/) - the system prompts, verbatim as
  last trialled. The maintainer decides when a change to one is worth a
  re-trial.
- [`GLOSSARY.md`](GLOSSARY.md) - the vocabulary.
- [`docs/adr/`](docs/adr/) - the decisions, and the reasoning behind them.
- The private archive,
  [`corygyarmathy/source-to-notes`](https://github.com/corygyarmathy/source-to-notes)
  (private), holds the trials behind the ADRs: their chapters and outputs, the
  history, and the issues the ADRs cite, 1-30 being the closed wayfinder map
  and its tickets (ADR 0001 §8).

## Conventions

- Use `GLOSSARY.md`'s words when you name things, and not the synonyms it lists
  under `_Avoid_`.
- `gofmt` is the formatter of record. Run it before you commit.
- Tests run offline and never call a model. They reach the provider seam
  through a fake that serves the replies the test supplies (ADR 0001 §7).
- Do not add a dependency. The answer is usually the standard library, and a
  dependency is an ADR-sized decision.
- A check's tests say what it does (ADR 0001 §4). A change to what a check
  does updates its tests, and says why in its package doc. Write code in
  idiomatic Go.
- Test what the code does with a reply, not that it reproduces a recorded run
  (ADR 0002 §8). Assert on the parts of a request that matter. Test reply
  handling on small reply cases in the step's test: one well-formed reply,
  then one per known deviation, each saying what it covers. Test
  deterministic output on golden files under `testdata/`, regenerated with
  `-update`. Keep end-to-end tests to orchestration, on short invented
  replies. A trial is an eval the maintainer runs, not a fixture: don't copy
  one in for a test to assert against.
- Write the reason into the comment, package doc or ADR. Don't point to an
  issue or PR in its place: an ADR may cite one as evidence, but needing to
  cite one elsewhere means documentation is missing.
- Notes are read through the one note model, and steps hand work on through the
  run folder (ADR 0002). Don't add a second parser or a side channel.
- The trial chapters and trial outputs are copyrighted. They stay in the
  private archive and never go into the public repository or an external
  service, except the model provider during a real run. A source or fixture
  added here is public domain or invented. The briefs are the one exception:
  they quote a few short phrases from the trial chapters as examples, and stay
  as trialled (ADR 0001 §8).

## Checks

```bash
scripts/check-format.sh     # prints nothing
go build ./...
go vet ./...
scripts/offline-test.sh     # go test ./... with no network
```

These four are the gate, in this order, in
[`.github/workflows/ci.yml`](.github/workflows/ci.yml) on every pull request.
Run them locally rather than waiting for CI.

## Do not

- Edit a brief in `internal/briefs/` to make a test pass.
- Make a real model call from a test, or from anything CI runs.

## Reporting

Say what you ran and what it said. Do not write that something is verified,
correct or unchanged unless you ran something that shows it.

## Agent skills

### Issue tracker

Issues live in GitHub Issues on `corygyarmathy/slipbox`, worked with `gh`. See `docs/agents/issue-tracker.md`.

### Triage labels

The six default triage labels, each named after its role (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `recipe-ticket`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: a root `GLOSSARY.md` plus `docs/adr/`. See `docs/agents/domain.md`.
