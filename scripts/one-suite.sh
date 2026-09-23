#!/usr/bin/env bash
# ONE HEAVY SUITE PER BOX AT A TIME.
#
# Nine sessions fired `go test ./...` at once on 2026-09-02 — a load average of
# a hundred, thirty-seven go test invocations, a hundred and twelve test
# binaries — and the reds that came out of it were manufactured by the load:
# packages cut off by a timeout that was fine on a quiet machine, races that
# only lose when the scheduler is starved. A timeout under that load attributes
# nothing, and a red under it proves nothing. So a whole-tree run and the two
# heavy packages, internal/tui3 and internal/session, take one lock. A second
# run on the same box refuses to start and says who holds it rather than joining
# the pile and reporting a red nobody caused.
#
# THE LOCK IS A KERNEL LOCK HELD BY AN OPEN FILE. Its lifetime does not depend
# on one pid namespace seeing another, and the kernel drops it if the holder
# dies. The file contents retain the holder pid and start time for a refusal.
# The Makefile invokes this wrapper around each heavy package's complete
# sharded run from `make test` or `test-report`, and around the rest of the
# tree when PKGS is `./...`. A `test-focus` selector and lighter
# packages do not take it, so cheap, independent proofs remain independent.
#
# The file is per box and per user, under /tmp rather than the session's own
# TMPDIR, because the point is to see the other session's run and a TMPDIR is
# exactly what sessions do not share.
set -euo pipefail

lock="${CODEAF_SUITE_LOCK_PATH:-/tmp/codeaf-suite-$(id -u).lockfile}"
# THE SECOND LOCK, AND THE CONFIGURATION SAYS BOTH NAMES RATHER THAN DERIVING
# ONE FROM THE OTHER. A checkout behind #1264 takes a DIRECTORY lock and cannot
# see the flock above, so a current tree takes both and becomes visible to every
# stale reader still on the box without a single old checkout being changed
# (#1307). Deriving this name by trimming the one above would make two names one
# fact, which is a fact that can disagree with itself, and it would silently
# follow a test harness driving a private path somewhere it was never meant to
# go. Empty means this run takes no directory lock at all.
export CODEAF_SUITE_DIRLOCK_PATH="${CODEAF_SUITE_DIRLOCK_PATH-/tmp/codeaf-suite-$(id -u).lock}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
helper="${CODEAF_SUITE_LOCK_HELPER:-$root/bin/codeaf-suite-lock}"
if [ -z "${CODEAF_SUITE_LOCK_HELPER:-}" ]; then
	mkdir -p "$root/bin"
	tmp="$helper.tmp.$$"
	trap 'rm -f "$tmp"' EXIT
	go build -o "$tmp" "$root/cmd/codeaf-suite-lock"
	mv "$tmp" "$helper"
	trap - EXIT
fi

# AND THIS SCRIPT EXPORTS NO STATE ROOT OF ITS OWN.
#
# An empty CODEAF_HOME for the whole run is the obvious answer to a test that
# reads the machine's real state, and it was tried and dropped, on clean dev,
# with the measurement: an empty home turns `internal/lane` green and
# `internal/rtk` red (its resolution order wants the managed `~/.codeaf/bin/rtk`
# to be there), and `internal/resident` red as well (a recurring-skill check
# that fails only when the package runs in sequence under the override). The
# packages disagree about what a state root should hold, and each is right about
# its own subject, so no one environment satisfies them all: a wrapper that
# picked one would trade a red somebody understands for a red nobody does.
# Isolation belongs at each package's own resolution seam, which is where #475
# put it for `internal/lane`.

# WHAT THE WRAPPER DOES OWE THE RUN IS THAT IT REALLY RAN.
#
# `go test` answers a package it has already run with the same inputs out of its
# cache, so a heavy suite can report an earlier tree: a package whose red was
# fixed by something the cache key does not cover still reads red, and one whose
# green was cached reads as a proof nobody performed. A heavy run is what a
# person quotes; it says nothing unless every package in it actually ran. The
# caller may say otherwise — only a caller that named no count gets this one.
suite=("$@")
if [ "${suite[0]:-}" = go ] && [ "${suite[1]:-}" = test ]; then
	counted=
	for arg in "${suite[@]}"; do
		case "$arg" in -count | -count=* | --count | --count=*) counted=yes ;; esac
	done
	[ -n "$counted" ] || suite=(go test -count=1 "${suite[@]:2}")
fi

# Replace the shell with the lock holder so killing the starter closes the
# descriptor immediately. The helper forwards ordinary stops to the suite and
# waits for it before releasing.
#
# AND IT KEEPS THIS SCRIPT'S NAME AS ITS argv[0]. A checkout behind #1264 reads
# the directory lock's pid as alive only when that pid's command line contains
# `one-suite.sh`, and until the holder names itself the directory names this
# process. Without the name an old tree judged a live lock stale and ran its
# suite beside ours (#1324).
exec -a "$0" "$helper" "$lock" "${suite[@]}"
