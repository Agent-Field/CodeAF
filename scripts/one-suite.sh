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
# THE LOCK IS A PID WITH ITS COMMAND LINE CHECKED, not a path's existence. A
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

# The lock is a directory, because mkdir is atomic where a check-then-write
# of a file is not: two starters in the same instant would both see no holder
# and both write, and the first to finish would remove the other's lock.
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

# Take the lock, or refuse naming the holder. A stale lock — a holder that is
# dead, or a pid reused by something else — is MOVED ASIDE, not removed: two
# contenders can both judge it stale, and only one mv of the same directory
# succeeds, so the other cannot delete a lock the winner has just taken. The
# take is then tried once more; a failure after that is a live holder that
# arrived in between, and the refusal stands.
take() {
	mkdir "$lock" 2>/dev/null
}
if ! take; then
	holder="$(cat "$lock/pid" 2>/dev/null || true)"
	if holder_alive "$holder"; then
		printf '%s\n' "another full suite is already running on this box (pid ${holder}, started $(cat "$lock/since" 2>/dev/null || echo '?'))." \
			'Wait for it, or run the packages you touched: make test PKGS=./internal/whatever' >&2
		exit 1
	fi
	stale="$lock.stale.$$"
	if mv "$lock" "$stale" 2>/dev/null; then
		rm -rf "$stale"
	fi
	if ! take; then
		echo 'another full suite took the lock this instant; try again.' >&2
		exit 1
	fi
fi
printf '%s\n' "$$" >"$lock/pid"
date -u +%Y-%m-%dT%H:%M:%SZ >"$lock/since"

# THE SCRIPT STAYS ALIVE AS THE HOLDER. An exec would make the holder's command
# line the suite's own, and the check above would read its lock as stale; so
# the suite runs as a child, a stop reaches it, and the lock goes when it ends.
"$@" &
child=$!
trap 'rm -rf "$lock"' EXIT
trap 'kill -INT "$child" 2>/dev/null || true' INT
trap 'kill -TERM "$child" 2>/dev/null || true' TERM
# AND THE WAIT OUTLIVES THE SIGNAL. A trapped signal returns from `wait` at
# once, before the child has acted on the one forwarded to it; exiting then
# would drop the lock with the suite still running. So the wait is repeated
# until the child is really gone, and only its own status is kept.
status=0
while true; do
	if wait "$child"; then status=0; else status=$?; fi
	kill -0 "$child" 2>/dev/null || break
done
exit "$status"
