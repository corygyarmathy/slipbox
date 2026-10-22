# Provenance: pragmatism-first-run

The first real `s2n run` of a public-domain chapter, William James's
_Pragmatism_, Lecture II, "What Pragmatism Means", copied as its run folder
holds it (ADR 0002 §8). Nothing in it is copyrighted: the source is public
domain, and the notes are the model's.

## `lecture-2/`

- **Run of:** commit `04a7e9c` on branch `claude/youthful-maxwell-ngsc2u`,
  and the resume for step 3.2 of `0851e57`, which changes only this file.
  Both have `main`'s code at `0ee3f17` (the branch adds only testdata). Made
  on 2026-10-22, by the maintainer. Its 72 model calls ran from 12:17 to
  12:30 (+08:00), cost $5.14 and took 31.3 minutes, added up.
- **Source:** `source/` is the run's copy of
  `testdata/sources/Pragmatism - Lecture II - What Pragmatism Means.md`, byte
  for byte. That file's front matter gives the edition (Project Gutenberg
  eBook #5116), why the text is public domain, and what was changed in
  bringing it into s2n's source format: its `##` headings are editorial, and
  its paragraphs are unwrapped onto one line each.
- **Briefs, models and effort:** the briefs embedded at those commits, and
  each flag at its default but `-run-dir`, `-stop-after`, `-vault` and
  `-staging`, as the commands below give them. At the defaults, steps 1.1,
  1.4 and the kept-loss repair run on `opus`, steps 1.3a, 1.3b, 1.5 and 2.1
  on `sonnet`, and step 3.1 on `haiku`, all at `medium` effort, with
  quote-once on and step 1.4's repair replying with edits. Each step's usage
  log records the model, effort level and brief of every call, and they
  match these.
- **How it was run:** stopped after the run report, so that the run folder
  could be copied before step 3.2 stages the notes out and deletes it, then
  resumed for step 3.2 alone:

  ```bash
  s2n run -run-dir ~/s2n-runs/pragmatism-lecture-2 -stop-after run-report \
    "testdata/sources/Pragmatism - Lecture II - What Pragmatism Means.md"
  mkdir -p testdata/trials/pragmatism-first-run
  cp -a ~/s2n-runs/pragmatism-lecture-2 testdata/trials/pragmatism-first-run/lecture-2
  mkdir -p ~/s2n-runs/empty-vault/_Slip_Box
  s2n resume -vault ~/s2n-runs/empty-vault \
    -staging ~/s2n-runs/pragmatism-lecture-2-staged ~/s2n-runs/pragmatism-lecture-2
  cp -a ~/s2n-runs/pragmatism-lecture-2-staged testdata/trials/pragmatism-first-run/staged
  ```

  So `lecture-2/` has every step's directory except `3.2-stage-out/`, and
  `staged/` is what step 3.2 staged out: the notes, the name-clash list and
  the run report.
- **Where it stopped or fell back:** once, at step 3.2, outside the model
  calls: a first resume, given `-staging` but no `-vault`, stopped because
  step 3.2 reads the vault's `_Slip_Box`. It came after `lecture-2/` was
  copied, so nothing here comes from it, and the resume above, given the
  empty vault, ran step 3.2 in full. Otherwise nowhere. No gate stopped it
  and no call waited for a usage limit: every call's usage reading is
  `allowed`, and the 5-hour limit was at most 49% used. The run report drops
  nothing and lists no fallback. It adds one loss and lists 21 statements
  that still fail, two of them from the run's own name spans, such as "I,
  William James," inserted mid-quote in segment 5.
- **Rebuilt replies:** none. Each step's `raw/` holds the replies as recorded.
- **Used by:** the replay tests of steps 1.1 to 1.5, 2.1 and 3.1, of checker
  parsing and of step 3.2: `internal/extract`, `internal/checker`,
  `internal/supportcheck`, `internal/readsalone`, `internal/repair`,
  `internal/lossproposer`, `internal/split`, `internal/format` and
  `internal/stageout`, through `testfixture.PragmatismRun` and
  `PragmatismFolder`. `cmd/s2n`'s tests run `s2n run` and `s2n resume` on it,
  from its source to stage out, and start resumes from its folder. Those that
  run a step check that it makes the requests the run recorded in
  `requests/`.
  So a change to a brief, model or effort level that one of those steps uses
  fails them until this run is recorded again, as **How it was run** gives it:
  its recorded replies no longer answer the requests s2n makes.

## `staged/`

Step 3.2's staging folder from the resume above, copied whole, which
`internal/stageout`'s and `cmd/s2n`'s tests compare with what they stage,
through `testfixture.PragmatismStaged`. Its
`run-report.md` is byte for byte `lecture-2/run-report/staged-report.md`.
Step 3.2 reads the vault's `_Slip_Box` note names for its name-clash list, so
the run was given an empty vault: the fixture depends on no one's vault, and
names none of its notes. Its name-clash list is empty for that reason.
