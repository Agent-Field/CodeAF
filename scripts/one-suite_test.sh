#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d /tmp/codeaf-one-suite-test.XXXXXX)"
trap 'rm -rf "$tmp"' EXIT
helper="$tmp/codeaf-suite-lock"
# The pre-#1264 reader, kept as it was apart from a private lock path.
legacy="$root/scripts/testdata/pre-1264/one-suite.sh"
go build -o "$helper" "$root/cmd/codeaf-suite-lock"

# read_lock_line waits for the file lock's line and prints it.
#
# THE LOCK IS NAMED AFTER THE SUITE STARTS, never before (main.go), so a suite
# that has said "ready" says nothing yet about the file. Reading it at once lost
# that race on a loaded box and quoted an empty line; the Go test beside this
# script reads until the line is there for the same reason, and so does this.
read_lock_line() {
	local waited=0
	while [ ! -s "$1" ] && [ "$waited" -lt 100 ]; do sleep 0.1; waited=$((waited + 1)); done
	cat "$1"
}

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
	metadata="$(read_lock_line "$lock")"
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
	local metadata suite_pid since; metadata="$(read_lock_line "$lock")"
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
#
# THE HOLDER IS A REAL OLD CHECKOUT, not a pid written by hand. Since #1324 a
# directory whose pid is not an old checkout's is a dead lock and is taken back
# (the arm after next), so the only honest stand-in for an old tree is the old
# script itself, kept under testdata with a private lock path.
run_refused_by_directory() {
	local lock="$tmp/stale.lock" dir="$tmp/stale.lock.dir" fifo="$tmp/stale.fifo"
	mkfifo "$fifo"
	env CODEAF_LEGACY_SUITE_DIRLOCK="$dir" "$legacy" sh -c 'echo ready >"$1"; exec sleep 30' sh "$fifo" &
	local old_pid=$! ready
	read -r ready <"$fifo"
	[ "$ready" = ready ]
	local out status
	set +e
	out="$(env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" CODEAF_SUITE_DIRLOCK_PATH="$dir" "$root/scripts/one-suite.sh" true 2>&1)"
	status=$?
	set -e
	[ "$status" -eq 1 ] || { printf 'a run started beside a directory-lock holder (status %d): %s\n' "$status" "$out" >&2; kill -TERM "$old_pid"; return 1; }
	case "$out" in
		*"another heavy suite is already running on this box (directory lock $dir, pid $old_pid)."*) ;;
		*) printf 'the refusal did not name the directory holder %s: %s\n' "$old_pid" "$out" >&2; kill -TERM "$old_pid"; return 1 ;;
	esac
	# A REFUSED RUN LEAVES THE LOCK WHERE IT FOUND IT. It never held it, so
	# clearing it here would unlock a box somebody else is still using.
	[ -d "$dir" ] || { printf 'a refused run removed a directory lock it never held\n' >&2; kill -TERM "$old_pid"; return 1; }
	[ "$(cat "$dir/pid")" = "$old_pid" ] || { printf 'a refused run rewrote the holder name\n' >&2; kill -TERM "$old_pid"; return 1; }
	kill -TERM "$old_pid"
	wait "$old_pid" || true
	[ ! -d "$dir" ] || { printf 'the old checkout left its directory lock behind\n' >&2; return 1; }
}

# AN OLD CHECKOUT SEES A CURRENT HOLDER AS ALIVE (#1324).
#
# The arm above is one direction; this is the other, and the one the second
# lock exists for. The old reader accepts a live pid only when its command line
# says one-suite.sh, and the directory used to name the SUITE, whose command
# line does not — so the old tree judged a live lock stale, moved it aside and
# ran its suite beside ours. This arm runs THE OLD READER ITSELF against a
# current holder, which run_both_locks never did: it only checked that the
# directory existed and named a live pid, and a live pid was never the old
# reader's whole test.
run_old_reader_sees_current_holder() {
	local lock="$tmp/old-reader.lock" dir="$tmp/old-reader.lock.dir" fifo="$tmp/old-reader.fifo"
	mkfifo "$fifo"
	env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" CODEAF_SUITE_DIRLOCK_PATH="$dir" \
		"$root/scripts/one-suite.sh" bash -c "echo ready >'$fifo'; exec sleep 30" &
	local wrapper_pid=$! ready
	read -r ready <"$fifo"
	[ "$ready" = ready ]
	local waited=0
	while [ ! -s "$lock" ] && [ "$waited" -lt 50 ]; do sleep 0.1; waited=$((waited + 1)); done
	local owner; owner="$(sed -n 's/.*holder=\([0-9]*\).*/\1/p' "$lock" 2>/dev/null)"
	[ -n "$owner" ] || { printf 'the file lock named no holder\n' >&2; kill -TERM "$wrapper_pid"; return 1; }
	# The directory is asked twice: once whoever it names now, and once the
	# holder has named itself, which is the state it spends the suite in.
	local pass out status
	for pass in early holder; do
		if [ "$pass" = holder ]; then
			waited=0
			while [ "$(cat "$dir/pid" 2>/dev/null)" != "$owner" ] && [ "$waited" -lt 50 ]; do sleep 0.1; waited=$((waited + 1)); done
			[ "$(cat "$dir/pid" 2>/dev/null)" = "$owner" ] || { printf 'the holder %s never named itself in the directory lock\n' "$owner" >&2; kill -TERM "$wrapper_pid"; return 1; }
		fi
		set +e
		out="$(env CODEAF_LEGACY_SUITE_DIRLOCK="$dir" "$legacy" sh -c 'echo STALE TREE RAN ITS SUITE BESIDE THE CURRENT ONE' 2>&1)"
		status=$?
		set -e
		case "$out" in
			*"STALE TREE RAN ITS SUITE BESIDE THE CURRENT ONE"*)
				printf 'the old reader (%s pass) ran its suite beside a live current holder: %s\n' "$pass" "$out" >&2; kill -TERM "$wrapper_pid"; return 1 ;;
		esac
		[ "$status" -eq 1 ] || { printf 'the old reader (%s pass) exited %d, want its refusal: %s\n' "$pass" "$status" "$out" >&2; kill -TERM "$wrapper_pid"; return 1; }
		case "$out" in
			*"another heavy suite is already running on this box"*) ;;
			*) printf 'the old reader (%s pass) did not refuse in its own words: %s\n' "$pass" "$out" >&2; kill -TERM "$wrapper_pid"; return 1 ;;
		esac
		[ -d "$dir" ] || { printf 'the old reader (%s pass) moved a live lock aside\n' "$pass" >&2; kill -TERM "$wrapper_pid"; return 1; }
	done
	kill -TERM "$wrapper_pid"
	wait "$wrapper_pid" || true
	waited=0
	while kill -0 "$owner" 2>/dev/null && [ "$waited" -lt 600 ]; do sleep 0.1; waited=$((waited + 1)); done
	[ ! -d "$dir" ] || { printf 'the directory lock %s outlived its holder %s\n' "$dir" "$owner" >&2; return 1; }
}

# A DEAD DIRECTORY LOCK IS TAKEN BACK, NOT OBEYED FOR EVER (#1324).
#
# An out-of-memory sweep or a SIGKILL of the holder frees the flock, because
# the kernel closes a dead process's descriptors, and leaves the directory,
# because nothing closes a name. Until #1324 every later run was then refused
# naming a pid that no longer existed. The holder killed here is the one this
# arm started, read back from the lock file its own launch wrote.
run_dead_directory_lock_is_taken_back() {
	local lock="$tmp/dead.lock" dir="$tmp/dead.lock.dir" fifo="$tmp/dead.fifo"
	mkfifo "$fifo"
	env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" CODEAF_SUITE_DIRLOCK_PATH="$dir" \
		"$root/scripts/one-suite.sh" bash -c "echo ready >'$fifo'; exec sleep 30" &
	local wrapper_pid=$! ready
	read -r ready <"$fifo"
	[ "$ready" = ready ]
	local waited=0
	while [ ! -s "$lock" ] && [ "$waited" -lt 50 ]; do sleep 0.1; waited=$((waited + 1)); done
	local owner; owner="$(sed -n 's/.*holder=\([0-9]*\).*/\1/p' "$lock" 2>/dev/null)"
	[ -n "$owner" ] || { printf 'the file lock named no holder\n' >&2; kill -TERM "$wrapper_pid"; return 1; }
	waited=0
	while [ "$(cat "$dir/pid" 2>/dev/null)" != "$owner" ] && [ "$waited" -lt 50 ]; do sleep 0.1; waited=$((waited + 1)); done
	kill -KILL "$owner"
	waited=0
	while ! flock -n "$lock" -c true 2>/dev/null && [ "$waited" -lt 50 ]; do sleep 0.1; waited=$((waited + 1)); done
	[ -d "$dir" ] || { printf 'the killed holder %s took its directory with it, so this arm tests nothing\n' "$owner" >&2; kill -TERM "$wrapper_pid"; return 1; }
	local out status
	set +e
	out="$(env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" CODEAF_SUITE_DIRLOCK_PATH="$dir" "$root/scripts/one-suite.sh" true 2>&1)"
	status=$?
	set -e
	kill -TERM "$wrapper_pid"
	wait "$wrapper_pid" || true
	[ "$status" -eq 0 ] || { printf 'a dead directory lock (holder %s, killed) still refused the next run (status %d): %s\n' "$owner" "$status" "$out" >&2; return 1; }
	[ ! -d "$dir" ] || { printf 'the run that took back the dead lock left its own directory behind\n' >&2; return 1; }
	flock -n "$lock" -c true 2>/dev/null || { printf 'the flock is still held after both runs ended\n' >&2; return 1; }
	ls -d "$dir".stale.* >/dev/null 2>&1 && { printf 'the dead directory was moved aside and left there\n' >&2; return 1; }
	return 0
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

# NAMED ARMS RUN ALONE, so a red arm can be shown red without the arms before
# it deciding whether it runs at all: `scripts/one-suite_test.sh run_dead_directory_lock_is_taken_back`.
if [ "$#" -gt 0 ]; then
	for arm in "$@"; do
		"$arm"
		printf '%s: ok\n' "$arm"
	done
	exit 0
fi

# The namespace arms need bubblewrap to make one host PID have two different
# views. The lock's other acceptance remains meaningful without that fixture,
# so a missing tool skips only these arms rather than blocking on a readiness
# pipe whose holder could never start.
if command -v bwrap >/dev/null 2>&1; then
	namespace_ran=yes
	run_order namespace-first
	run_order host-first
	run_host_b_after_cell
else
	namespace_ran=
	printf 'one-suite namespace acceptance: SKIP (bwrap is not installed)\n'
fi
run_both_locks
run_refused_by_directory
run_old_reader_sees_current_holder
run_dead_directory_lock_is_taken_back
run_suite_does_not_inherit_the_lock_name
if [ -n "$namespace_ran" ]; then
	printf 'one-suite namespace and dual-lock acceptance: ok\n'
else
	printf 'one-suite dual-lock acceptance: ok (namespace skipped)\n'
fi
