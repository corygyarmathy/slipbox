# Provenance: hand-written

Not a trial run. These are replies written by hand to reach the parts of a
step that `../pragmatism-first-run/lecture-2/` doesn't reach. Nothing was
copied from a trial, so the provenance fields for a copied run (repository,
branch, commit, path) don't apply.

The notes and replies that were written here against the trial chapters,
for the Python references and later for s2n's own tests, were deleted once
invented-text tests and the Pragmatism run covered their cases. They quoted
copyrighted text. Git history in the private archive holds them (ADR 0001
§8).

## `proposer-remark/`

One reply written by hand for `internal/lossproposer`'s tests, which replay
step 1.5 of `../pragmatism-first-run/lecture-2/`. Neither of that run's
proposer runs wrote an item that quotes no uncovered line, so nothing reached
step 1.5's remarks, which it writes as dropped.

- `4.txt`: run 2's reply for segment 4, The Formation of New Beliefs, used
  in place of the recorded one, which proposes no loss. Its one item's
  `Quote:` line cites nothing ("none uncovered"), and a free line follows it.
  The tests check that the item is written as step 1.5's one dropped item,
  and still is after a resume that reuses segment 4's replies.

Its wording is invented: no line of it is a source's or a model's.

## Replies in the tests

Short replies, sources and notes written by hand inside the tests rather
than kept here, for the cases `../pragmatism-first-run/lecture-2/` doesn't
reach. Each test's doc comment says what its invented text stands in for.
Their wording is invented: no line of them is a source's or a model's.

- `cmd/s2n`: loss 4.1, which `addLoss41` adds to step 1.5's union of the
  run, and its kept-loss reply, `- Loss 4.1: already stated.`; the
  one-segment `Tiny` source and its replies, for `-quote-once=false` with
  either repair reply shape (`TestRunCommand_QuoteOnceOff`); the format
  replies of the fallback tests; and the sources and replies of
  `GoesUnattended` and `FallsBack`.
- `internal/split` and `internal/format`: tiny sources and replies, for a
  manifest with no trailing newline, a step run at the model it is given, a
  bad reply's fallback, and a failing segment named with a curly apostrophe.
- `internal/stageout`: an `8 Extra` folder beside the run's notes, whose notes
  are named like two of the run's, and a vault whose `_Slip_Box` holds
  names that clash with the run's notes.
