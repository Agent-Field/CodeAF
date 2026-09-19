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
	local metadata output status
	metadata="$(cat "$lock")"
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
		*"another heavy suite is already running on this box (pid ${metadata%% *}, started ${metadata#* })."*) ;;
		*) printf 'refusal did not name holder metadata %q: %s\n' "$metadata" "$output" >&2; return 1 ;;
	esac
	kill -TERM "$holder_pid"
	wait "$holder_pid" || true
}

run_order namespace-first
run_order host-first
printf 'one-suite namespace acceptance: ok\n'
