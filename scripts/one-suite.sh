#!/usr/bin/env bash
# ONE FULL SUITE PER BOX AT A TIME.
#
# Nine sessions fired `go test ./...` at once on 2026-09-02 — a load average of
# a hundred, thirty-seven go test invocations, a hundred and twelve test
# binaries — and the reds that came out of it were manufactured by the load:
# packages cut off by a timeout that was fine on a quiet machine, races that
# only lose when the scheduler is starved. A timeout under that load attributes
# nothing, and a red under it proves nothing. So the full run takes a lock, and
# a second full run on the same box refuses to start and says who holds it,
# rather than joining the pile and reporting a red nobody caused.
#
# THE LOCK IS A PID WITH ITS COMMAND LINE CHECKED, not a file's existence. A
# session that died mid-run must not leave the box locked, and a pid that was
# reused by something else must not either; the lock is stale unless the pid
# is alive AND is still running this script, and a stale lock is simply taken.
# Packages named one at a time (`make test PKGS=./internal/x`) do not take it:
# the pile is made of whole-tree runs, and the touched-package proof a pull
# request needs should never wait on somebody else's nightly.
#
# The file is per box and per user, under /tmp rather than the session's own
# TMPDIR, because the point is to see the other session's run and a TMPDIR is
# exactly what sessions do not share.
set -euo pipefail

lock="/tmp/aforge-suite-$(id -u).lock"

holder_alive() {
	local pid="$1"
	[ -n "$pid" ] || return 1
	kill -0 "$pid" 2>/dev/null || return 1
	# Linux has /proc; elsewhere ps answers the same question more slowly.
	local args
	if [ -r "/proc/$pid/cmdline" ]; then
		args="$(tr '\0' ' ' <"/proc/$pid/cmdline")"
	else
		args="$(ps -o args= -p "$pid" 2>/dev/null || true)"
	fi
	case "$args" in *one-suite.sh*) return 0 ;; esac
	return 1
}

if [ -f "$lock" ]; then
	holder="$(head -n1 "$lock" 2>/dev/null || true)"
	if holder_alive "$holder"; then
		printf '%s\n' "another full suite is already running on this box (pid ${holder}, started $(sed -n 2p "$lock"))." \
			'Wait for it, or run the packages you touched: make test PKGS=./internal/whatever' >&2
		exit 1
	fi
fi
printf '%s\n%s\n' "$$" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$lock"

# THE SCRIPT STAYS ALIVE AS THE HOLDER. An exec would make the holder's command
# line the suite's own, and the check above would read its lock as stale; so
# the suite runs as a child, a stop reaches it, and the lock goes when it ends.
"$@" &
child=$!
trap 'rm -f "$lock"' EXIT
trap 'kill -INT "$child" 2>/dev/null || true' INT
trap 'kill -TERM "$child" 2>/dev/null || true' TERM
status=0
wait "$child" || status=$?
exit "$status"
