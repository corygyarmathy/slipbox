#!/usr/bin/env bash
# Run `go test` with no network available.
#
# AGENTS.md says tests run offline (ADR 0001 §7). That is a constraint on the tests, so it is
# enforced here rather than trusted: a test that reaches a model passes on a
# developer's laptop and spends a Pro window doing it.
#
# CI calls this script rather than `go test` directly, so the gate and the
# developer run the same thing.
#
# Loopback stays up. A test serving itself over 127.0.0.1 is not network access,
# and forbidding it would rule out httptest for no gain.
#
# Arguments are `go test` flags, and ./... is always appended after them, so the
# script never has to tell a flag's value from a package path. Naming a package
# as well is harmless - `go test` deduplicates the list - but -run is the way to
# narrow a run here:
#
#   scripts/offline-test.sh
#   scripts/offline-test.sh -run TestMain_ExitCodes -v
set -euo pipefail

# The toolchain must not reach out either. The module uses only the standard
# library (ADR 0001 §4), so there is nothing legitimate to fetch: GOPROXY=off
# turns a stray import into a failure here instead of a download, and
# GOTOOLCHAIN=local reports "go.mod requires a newer Go" rather than trying to
# fetch that newer Go.
export GOFLAGS="${GOFLAGS:-} -mod=readonly"
export GOPROXY=off
export GOTOOLCHAIN=local

# Compile with the network still present, so a failure to isolate below is not
# confused with a build failure, and so the build cache is written as the
# invoking user rather than through the namespace's uid mapping. Without "$@":
# those are `go test` flags, and `go build` rejects most of them.
go build ./...

# The environment is passed explicitly rather than inherited, because the
# privileged path below crosses `sudo` and `runuser`, each of which resets it.
# Losing PATH there makes `go` resolve to whatever toolchain the runner image
# has. Resolving `go` and its caches out here, where the environment is still
# ours, keeps both paths on the same toolchain and the same caches.
go_env=(
	"PATH=$PATH"
	"HOME=$HOME"
	"GOFLAGS=$GOFLAGS"
	"GOPROXY=$GOPROXY"
	"GOTOOLCHAIN=$GOTOOLCHAIN"
	"GOCACHE=$(go env GOCACHE)"
	"GOMODCACHE=$(go env GOMODCACHE)"
)
# `env` and `go` by absolute path for the same reason: the inner shell may have
# a PATH neither of us chose.
test_cmd=("$(command -v env)" "${go_env[@]}" "$(command -v go)" test "$@" ./...)

# `ip link set lo up` is best-effort: a host without iproute2 still runs the
# tests, it just has no loopback, and saying so beats a mystery dial failure.
readonly inner='ip link set lo up 2>/dev/null || printf "offline-test: could not bring loopback up; tests using 127.0.0.1 will fail\n" >&2
exec "$@"'

if unshare --user --map-root-user --net true 2>/dev/null; then
	# Unprivileged: a user namespace makes us root inside it, which is enough
	# to own a fresh network namespace that has no route anywhere.
	printf 'offline-test: network isolated by an unprivileged user namespace\n' >&2
	exec unshare --user --map-root-user --net -- \
		bash -c "$inner" offline-test "${test_cmd[@]}"
fi

if sudo -n true 2>/dev/null; then
	# Fallback for hosts that refuse unprivileged user namespaces - Ubuntu's
	# AppArmor restriction is the one that bites, and a GitHub runner is such a
	# host. Root owns the namespace and hands the tests straight back to the
	# invoking user, so nothing runs as root and the caches keep their owner.
	printf 'offline-test: network isolated by sudo unshare --net\n' >&2
	exec sudo -n unshare --net -- \
		bash -c "$inner" offline-test runuser -u "$(id -un)" -- "${test_cmd[@]}"
fi

# Never fall through to a networked `go test`: a gate that quietly stops gating
# is worse than no gate, because it reports success.
printf 'offline-test: cannot isolate the network - unprivileged user namespaces are unavailable and there is no passwordless sudo.\n' >&2
printf 'offline-test: refusing to run the tests with the network up. Run `go test ./...` directly if you accept that.\n' >&2
exit 1
