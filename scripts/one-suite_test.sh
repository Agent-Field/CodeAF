#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d /tmp/codeaf-one-suite-test.XXXXXX)"
trap 'rm -rf "$tmp"' EXIT
helper="$tmp/codeaf-suite-lock"
go build -o "$helper" "$root/cmd/codeaf-suite-lock"

run_order() {
	local first="$1" lock="$tmp/$1.lock" fifo="$tmp/$1.fifo"
	mkfifo "$fifo"
	local -a holder=(env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" CODEAF_SUITE_DIRLOCK_PATH="$lock.dir" "$root/scripts/one-suite.sh" sh -c 'echo ready >"$1"; exec sleep 30' sh "$fifo")
	local -a contender=(env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" CODEAF_SUITE_DIRLOCK_PATH="$lock.dir" "$root/scripts/one-suite.sh" true)
	if [ "$first" = namespace-first ]; then
		bwrap --unshare-pid --bind / / --dev-bind /dev /dev --proc /proc -- "${holder[@]}" &
	else
		"${holder[@]}" &
	fi
	local holder_pid=$! ready
	read -r ready <"$fifo"
	[ "$ready" = ready ]
	local metadata output status suite_pid since
	metadata="$(cat "$lock")"
	# The lock file records the suite's pid, when it started, and the pid of the
	# holder that carries the lock beside it, so read the two the refusal quotes
	# by field rather than by splitting the line in two.
	suite_pid="${metadata%% *}"
	since="${metadata#* }"
	since="${since%% *}"
	set +e
	if [ "$first" = namespace-first ]; then
		output="$("${contender[@]}" 2>&1)"
	else
		output="$(bwrap --unshare-pid --bind / / --dev-bind /dev /dev --proc /proc -- "${contender[@]}" 2>&1)"
	fi
	status=$?
	set -e
	[ "$status" -eq 1 ]
	case "$output" in
		*"another heavy suite is already running on this box (pid ${suite_pid}, started ${since})."*) ;;
		*) printf 'refusal did not name holder metadata %q: %s\n' "$metadata" "$output" >&2; return 1 ;;
	esac
	if [ "$first" = host-first ]; then
		# The contender runs in a fresh pid namespace and cannot see the host
		# holder's pid, so the refusal must say the recorded holder is not
		# visible from here rather than call a live suite gone.
		case "$output" in
			*"pid ${metadata%% *} is not visible from here"*) ;;
			*) printf 'a namespace contender was not told the host pid is not visible: %s\n' "$output" >&2; return 1 ;;
		esac
	fi
	kill -TERM "$holder_pid"
	wait "$holder_pid" || true
}

# host A holds, a cell (pid namespace) starter is refused, and a host B arriving
# AFTER the cell's attempt is STILL refused and still names host A. This is the
# defect the old pid-staleness lock had: a cell's namespace-local pid rewrote the
# lock so a later host read it as stale. The fd-held flock cannot be rewritten by
# a starter that never acquired it, so host A's metadata stays and host B refuses.
run_host_b_after_cell() {
	local lock="$tmp/hostb.lock" fifo="$tmp/hostb.fifo"
	mkfifo "$fifo"
	local -a holder=(env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" CODEAF_SUITE_DIRLOCK_PATH="$lock.dir" "$root/scripts/one-suite.sh" sh -c 'echo ready >"$1"; exec sleep 30' sh "$fifo")
	local -a cell=(env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" CODEAF_SUITE_DIRLOCK_PATH="$lock.dir" "$root/scripts/one-suite.sh" true)
	local -a hostb=(env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" CODEAF_SUITE_DIRLOCK_PATH="$lock.dir" "$root/scripts/one-suite.sh" true)
	"${holder[@]}" &
	local holder_pid=$! ready
	read -r ready <"$fifo"
	[ "$ready" = ready ]
	local metadata suite_pid since; metadata="$(cat "$lock")"
	suite_pid="${metadata%% *}"
	since="${metadata#* }"
	since="${since%% *}"
	set +e
	local cell_out cell_status hostb_out hostb_status
	cell_out="$(bwrap --unshare-pid --bind / / --dev-bind /dev /dev --proc /proc -- "${cell[@]}" 2>&1)"; cell_status=$?
	hostb_out="$("${hostb[@]}" 2>&1)"; hostb_status=$?
	set -e
	[ "$cell_status" -eq 1 ] || { printf 'cell starter was not refused: %s\n' "$cell_out" >&2; kill -TERM "$holder_pid"; return 1; }
	[ "$hostb_status" -eq 1 ] || { printf 'host B was not refused after the cell attempt: %s\n' "$hostb_out" >&2; kill -TERM "$holder_pid"; return 1; }
	case "$hostb_out" in
		*"another heavy suite is already running on this box (pid ${suite_pid}, started ${since})."*) ;;
		*) printf 'host B refusal did not still name host A %q: %s\n' "$metadata" "$hostb_out" >&2; kill -TERM "$holder_pid"; return 1 ;;
	esac
	kill -TERM "$holder_pid"
	wait "$holder_pid" || true
}

# BOTH LOCKS, WHICH IS WHAT MAKES A CURRENT TREE VISIBLE TO A STALE ONE.
#
# A checkout behind #1264 reads only the DIRECTORY lock and will never be taught
# to read the flock, because being unaware of the flock is what makes it stale.
# So the flock alone leaves a current holder invisible to half the box (#1307).
#
# The third assertion is the one that matters and the one that is awkward to
# build: a half release leaves the box locked to one of the two populations
# forever, which is worse than the defect it fixes, because the defect at least
# fails open.
run_both_locks() {
	local lock="$tmp/both.lock" dir="$tmp/both.lock.dir" fifo="$tmp/both.fifo"
	mkfifo "$fifo"
	local -a holder=(env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" CODEAF_SUITE_DIRLOCK_PATH="$dir" "$root/scripts/one-suite.sh" bash -c "echo ready >'$fifo'; sleep 30")
	"${holder[@]}" &
	local holder_pid=$! ready
	read -r ready <"$fifo"
	[ "$ready" = ready ]

	# ONE: a reader of the DIRECTORY mechanism sees the holder. That is the road
	# a stale checkout takes and the only one it has.
	[ -d "$dir" ] || { printf 'a held suite did not take the directory lock %s\n' "$dir" >&2; kill -TERM "$holder_pid"; return 1; }
	# The pid file lands after the suite reports ready, by design (main.go says
	# why), so wait for it rather than read once and call an empty file a defect.
	local named waited=0
	while [ ! -s "$dir/pid" ] && [ "$waited" -lt 50 ]; do sleep 0.1; waited=$((waited + 1)); done
	named="$(cat "$dir/pid" 2>/dev/null || true)"
	case "$named" in
		'' | *[!0-9]*) printf 'the directory lock names no suite pid: %q\n' "$named" >&2; kill -TERM "$holder_pid"; return 1 ;;
	esac
	kill -0 "$named" 2>/dev/null || { printf 'the directory lock names pid %s, which is not running\n' "$named" >&2; kill -TERM "$holder_pid"; return 1; }

	# TWO: a reader of the FLOCK still sees it, unchanged. Taking a second lock
	# must not cost the first one anything.
	if flock -n "$lock" -c true 2>/dev/null; then
		printf 'the flock read free while a suite held it\n' >&2; kill -TERM "$holder_pid"; return 1
	fi

	kill -TERM "$holder_pid"
	wait "$holder_pid" || true

	# THREE: RELEASE DROPS BOTH, so neither population is left reading a holder
	# that is gone. The flock frees with the holder's descriptor; the directory
	# has to be removed by that same holder, so this arm is what fails if the
	# removal is put in the wrapper, where killing the wrapper would free the
	# directory under a suite that is still running.
	waited=0
	while [ -d "$dir" ] && [ "$waited" -lt 50 ]; do sleep 0.1; waited=$((waited + 1)); done
	[ ! -d "$dir" ] || { printf 'the directory lock %s outlived the suite that took it\n' "$dir" >&2; return 1; }
	flock -n "$lock" -c true 2>/dev/null || { printf 'the flock outlived the suite that took it\n' >&2; return 1; }
}

# AND A STALE HOLDER TURNS A CURRENT RUN AWAY, which is the direction that has
# been costing unattributable reds: a current tree reads the flock, sees free,
# and starts beside a suite it cannot see.
run_refused_by_directory() {
	local lock="$tmp/stale.lock" dir="$tmp/stale.lock.dir"
	mkdir "$dir"
	printf '%d\n' "$$" >"$dir/pid"
	local out status
	set +e
	out="$(env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" CODEAF_SUITE_DIRLOCK_PATH="$dir" "$root/scripts/one-suite.sh" true 2>&1)"
	status=$?
	set -e
	[ "$status" -eq 1 ] || { printf 'a run started beside a directory-lock holder (status %d): %s\n' "$status" "$out" >&2; return 1; }
	case "$out" in
		*"another heavy suite is already running on this box (directory lock $dir, pid $$)."*) ;;
		*) printf 'the refusal did not name the directory holder: %s\n' "$out" >&2; return 1 ;;
	esac
	# A REFUSED RUN LEAVES THE LOCK WHERE IT FOUND IT. It never held it, so
	# clearing it here would unlock a box somebody else is still using.
	[ -d "$dir" ] || { printf 'a refused run removed a directory lock it never held\n' >&2; return 1; }
	[ "$(cat "$dir/pid")" = "$$" ] || { printf 'a refused run rewrote the holder name\n' >&2; return 1; }
	rm -rf "$dir"
}

run_order namespace-first
run_order host-first
run_host_b_after_cell
run_both_locks
run_refused_by_directory
printf 'one-suite namespace and dual-lock acceptance: ok\n'
