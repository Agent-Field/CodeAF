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
	local -a holder=(env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" CODEAF_SUITE_DIRLOCK_PATH="$dir" "$root/scripts/one-suite.sh" bash -c "echo ready >'$fifo'; exec sleep 30")
	"${holder[@]}" &
	local holder_pid=$! ready
	read -r ready <"$fifo"
	[ "$ready" = ready ]

	# ONE: a reader of the DIRECTORY mechanism sees the holder. That is the road
	# a stale checkout takes and the only one it has.
	[ -d "$dir" ] || { printf 'a held suite did not take the directory lock %s\n' "$dir" >&2; kill -TERM "$holder_pid"; return 1; }
	# WAIT FOR THE FILE LOCK'S METADATA, NOT FOR THE DIRECTORY'S PID FILE.
	#
	# The directory names its holder from the moment it is CLAIMED, which is
	# before the suite exists and before the lock holder beside it does. Waiting
	# on that file therefore lets this arm run while the wrapper is still the
	# only owner, and killing it there lands in the one window nothing can clean
	# up, because a signal runs no deferred work. This arm did exactly that and
	# leaked the directory every run, which is the window being demonstrated
	# rather than a defect in the lock.
	#
	# The lock file's metadata is written AFTER the holder has taken the
	# descriptor, so its arrival is the honest "everything is in place" signal,
	# and it is the one the older arms above already wait on.
	local named waited=0
	while [ ! -s "$lock" ] && [ "$waited" -lt 50 ]; do sleep 0.1; waited=$((waited + 1)); done
	[ -s "$lock" ] || { printf 'the file lock was never named, so the holder never took it\n' >&2; kill -TERM "$holder_pid"; return 1; }
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

	# The holder beside the suite, read from the lock file it wrote. The release
	# assertion below needs it to tell a lock that was never dropped from one
	# whose owner is simply still finishing.
	local owner; owner="$(sed -n 's/.*holder=\([0-9]*\).*/\1/p' "$lock" 2>/dev/null)"

	kill -TERM "$holder_pid"
	wait "$holder_pid" || true

	# THREE: RELEASE DROPS BOTH, so neither population is left reading a holder
	# that is gone. The flock frees with the holder's descriptor; the directory
	# has to be removed by that same holder, so this arm is what fails if the
	# removal is put in the wrapper, where killing the wrapper would free the
	# directory under a suite that is still running.
	# WAIT FOR THE HOLDER TO BE GONE, THEN ASSERT. NOT FOR A NUMBER OF SECONDS.
	#
	# The invariant is causal, not temporal: THE DIRECTORY MUST NOT OUTLIVE ITS
	# HOLDER. A release that takes a hundred milliseconds on a quiet box and ten
	# seconds on a loaded one satisfies it equally, and an arm that asserts a
	# duration instead flakes exactly when the box is busy, which is when a gate
	# is most likely to be running. This arm did that, red then green on one
	# machine an hour apart, and was rewritten rather than given a longer wait:
	# a longer wait is the same defect further away.
	#
	# The suite above is `exec sleep` for the neighbouring reason. A shell
	# WAITING on a foreground child takes the signal itself and leaves the child
	# running, and the holder then correctly goes on holding for that child's
	# whole life, which is the lock behaving properly and a test reading it as a
	# leak.
	#
	# A holder that never dies is a real failure and is NOT this arm's: the
	# wrapper reports it by name (main.go's reportLingeringHolder). The bound
	# here exists only so that failure ends the run instead of hanging it.
	[ -n "${owner:-}" ] || { printf 'the lock file named no holder, so nothing here can be asserted\n' >&2; return 1; }
	waited=0
	while kill -0 "$owner" 2>/dev/null && [ "$waited" -lt 600 ]; do sleep 0.1; waited=$((waited + 1)); done
	if kill -0 "$owner" 2>/dev/null; then
		printf 'the lock holder %s outlived its suite by a minute; that is the wrapper lingering, not this arm\n' "$owner" >&2
		return 1
	fi
	[ ! -d "$dir" ] || { printf 'the directory lock %s outlived its holder %s: a lock with no owner, which pins every reader forever\n' "$dir" "$owner" >&2; return 1; }
	flock -n "$lock" -c true 2>/dev/null || { printf 'the flock outlived its holder %s\n' "$owner" >&2; return 1; }
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

# THE SUITE DOES NOT INHERIT THE LOCK'S NAME, and this arm exists because the
# absence of it cost a gate.
#
# The wrapper is told the directory lock through the environment, and the
# obvious way to pass it on is to export it. Exporting it hands it to the SUITE
# as well, and then anything the suite starts is refused by the lock the suite
# itself is running under. Measured: two of this repository's own lock tests
# failed inside `make check` for exactly that reason, reading an empty pipe from
# a wrapper that had correctly refused itself.
#
# The suite never sees the file lock's DESCRIPTOR for the same class of reason.
# This is that rule applied to the name.
run_suite_does_not_inherit_the_lock_name() {
	local lock="$tmp/inherit.lock" dir="$tmp/inherit.lock.dir" seen
	seen="$(env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" CODEAF_SUITE_DIRLOCK_PATH="$dir" \
		"$root/scripts/one-suite.sh" sh -c 'printf %s "${CODEAF_SUITE_DIRLOCK_PATH:-ABSENT}"')"
	[ "$seen" = ABSENT ] || { printf 'the suite inherited the directory lock name: %q\n' "$seen" >&2; return 1; }
	# And the run still took the lock it was told about, so the arm cannot pass
	# by the wrapper having ignored the setting altogether.
	[ ! -d "$dir" ] || { printf 'the directory lock outlived a suite that had already ended\n' >&2; return 1; }
}

run_order namespace-first
run_order host-first
run_host_b_after_cell
run_both_locks
run_refused_by_directory
run_suite_does_not_inherit_the_lock_name
printf 'one-suite namespace and dual-lock acceptance: ok\n'
