#!/usr/bin/env bash
# Check that every package in this module is gofmt-clean.
#
# One definition, used by the agent, the editor and CI alike. It checks and
# doesn't write: AGENTS.md tells the agent to run gofmt before committing, so
# formatting lands in its commits rather than as drift. The package list comes
# from `go list`, so `testdata/` and `docs/` are never walked.
#
#   scripts/check-format.sh          # prints nothing, exits 0, when clean
#   scripts/check-format.sh -d       # also print the diffs
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

mapfile -t dirs < <(go list -f '{{.Dir}}' ./...)
if [ ${#dirs[@]} -eq 0 ]; then
	echo "check-format: go list found no packages" >&2
	exit 1
fi

unformatted="$(gofmt -l "${dirs[@]}")"
if [ -n "$unformatted" ]; then
	printf '%s\n' "$unformatted"
	if [ "${1:-}" = "-d" ]; then
		gofmt -d "${dirs[@]}"
	fi
	exit 1
fi
