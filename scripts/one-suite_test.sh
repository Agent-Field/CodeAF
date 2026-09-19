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
	local -a holder=(env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" "$root/scripts/one-suite.sh" sh -c 'echo ready >"$1"; exec sleep 30' sh "$fifo")
	local -a contender=(env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" "$root/scripts/one-suite.sh" true)
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
	local -a holder=(env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" "$root/scripts/one-suite.sh" sh -c 'echo ready >"$1"; exec sleep 30' sh "$fifo")
	local -a cell=(env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" "$root/scripts/one-suite.sh" true)
	local -a hostb=(env CODEAF_SUITE_LOCK_HELPER="$helper" CODEAF_SUITE_LOCK_PATH="$lock" "$root/scripts/one-suite.sh" true)
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

run_order namespace-first
run_order host-first
run_host_b_after_cell
printf 'one-suite namespace acceptance: ok\n'
