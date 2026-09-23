#!/usr/bin/env bash
# Run one compiled Go test binary as disjoint concurrent shards. The package's
# tests are serial by construction, so separate processes recover the time the
# suite otherwise spends waiting without introducing shared in-process state.
set -euo pipefail

# PORTABLE TO THE BASH AND SED A MAC SHIPS. /bin/bash there is 3.2, which calls
# an empty array unbound under `set -u`, so every possibly-empty flag array is
# expanded as ${name[@]+"${name[@]}"}; and BSD sed has no \| in a basic
# expression, so every alternation below is written with -E. A runner that only
# works on GNU tools turns `make test` red on a laptop for nobody's change.

usage() {
	printf '%s\n' 'usage: scripts/shard-test.sh [GO TEST FLAGS] PACKAGE' >&2
	exit 2
}

cpu_count() {
	local count
	if command -v nproc >/dev/null 2>&1; then
		count="$(nproc)"
	elif command -v sysctl >/dev/null 2>&1; then
		count="$(sysctl -n hw.ncpu 2>/dev/null || true)"
	else
		count=1
	fi
	case "$count" in '' | *[!0-9]*) count=1 ;; esac
	if [ "$count" -gt 8 ]; then count=8; fi
	printf '%s\n' "$count"
}

shards="${SHARDS:-$(cpu_count)}"
case "$shards" in
	'' | *[!0-9]*) printf 'shard-test: SHARDS must be a positive integer, got %q\n' "$shards" >&2; exit 2 ;;
esac
if [ "$shards" -lt 1 ]; then
	printf 'shard-test: SHARDS must be a positive integer, got %q\n' "$shards" >&2
	exit 2
fi

package=
json=
user_run='^Test'
declare -a compile_flags=()
declare -a run_flags=()
declare -a list_flags=()

need_value() {
	if [ "$#" -eq 0 ]; then
		printf 'shard-test: flag %s needs a value\n' "$flag" >&2
		exit 2
	fi
}

# The compiled binary accepts the ordinary testing flags below. Flags whose
# output files or work modes cannot be shared by concurrent processes are
# refused explicitly instead of being dropped or producing corrupt artifacts.
while [ "$#" -gt 0 ]; do
	arg="$1"
	shift
	case "$arg" in
		-json | --json) json=yes ;;
		-count | --count | -test.count | --test.count)
			flag="$arg"; need_value "$@"; shift ;;
		-count=* | --count=* | -test.count=* | --test.count=*) ;;
		-p | --p)
			flag="$arg"; need_value "$@"; compile_flags+=("-p" "$1"); shift ;;
		-p=* | --p=*) compile_flags+=("-p=${arg#*=}") ;;
		-timeout | --timeout | -test.timeout | --test.timeout)
			flag="$arg"; need_value "$@"; run_flags+=("-test.timeout=$1"); shift ;;
		-timeout=* | --timeout=* | -test.timeout=* | --test.timeout=*)
			run_flags+=("-test.timeout=${arg#*=}") ;;
		-skip | --skip | -test.skip | --test.skip)
			flag="$arg"; need_value "$@"; run_flags+=("-test.skip=$1"); list_flags+=("-test.skip=$1"); shift ;;
		-skip=* | --skip=* | -test.skip=* | --test.skip=*)
			run_flags+=("-test.skip=${arg#*=}"); list_flags+=("-test.skip=${arg#*=}") ;;
		-run | --run | -test.run | --test.run)
			flag="$arg"; need_value "$@"; user_run="$1"; shift ;;
		-run=* | --run=* | -test.run=* | --test.run=*) user_run="${arg#*=}" ;;
		-v | --v | -test.v | --test.v) ;;
		-v=* | --v=* | -test.v=* | --test.v=*) ;;
		-short | --short | -test.short | --test.short) run_flags+=("-test.short") ;;
		-short=* | --short=* | -test.short=* | --test.short=*) run_flags+=("-test.short=${arg#*=}") ;;
		-failfast | --failfast | -test.failfast | --test.failfast) run_flags+=("-test.failfast") ;;
		-failfast=* | --failfast=* | -test.failfast=* | --test.failfast=*) run_flags+=("-test.failfast=${arg#*=}") ;;
		-fullpath | --fullpath | -test.fullpath | --test.fullpath) run_flags+=("-test.fullpath") ;;
		-fullpath=* | --fullpath=* | -test.fullpath=* | --test.fullpath=*) run_flags+=("-test.fullpath=${arg#*=}") ;;
		-parallel | --parallel | -test.parallel | --test.parallel | -cpu | --cpu | -test.cpu | --test.cpu | -shuffle | --shuffle | -test.shuffle | --test.shuffle)
			flag="$arg"; need_value "$@"; name="${arg#-}"; name="${name#-}"; name="${name#test.}"; run_flags+=("-test.$name=$1"); shift ;;
		-parallel=* | --parallel=* | -test.parallel=* | --test.parallel=* | -cpu=* | --cpu=* | -test.cpu=* | --test.cpu=* | -shuffle=* | --shuffle=* | -test.shuffle=* | --test.shuffle=*)
			name="${arg%%=*}"; name="${name#-}"; name="${name#-}"; name="${name#test.}"; run_flags+=("-test.$name=${arg#*=}") ;;
		-bench* | --bench* | -test.bench* | --test.bench* | -fuzz* | --fuzz* | -test.fuzz* | --test.fuzz* | -coverprofile* | --coverprofile* | -test.coverprofile* | --test.coverprofile* | -cpuprofile* | --cpuprofile* | -test.cpuprofile* | --test.cpuprofile* | -memprofile* | --memprofile* | -test.memprofile* | --test.memprofile* | -blockprofile* | --blockprofile* | -test.blockprofile* | --test.blockprofile* | -mutexprofile* | --mutexprofile* | -test.mutexprofile* | --test.mutexprofile* | -trace* | --trace* | -test.trace* | --test.trace* | -outputdir* | --outputdir* | -test.outputdir* | --test.outputdir* | -list* | --list* | -test.list* | --test.list*)
			printf 'shard-test: go-test flag %s cannot be used by concurrent test-binary shards\n' "$arg" >&2
			exit 2 ;;
		-*)
			printf 'shard-test: compiled test binary cannot take go-test flag %s\n' "$arg" >&2
			exit 2 ;;
		*)
			if [ -n "$package" ]; then usage; fi
			package="$arg" ;;
	esac
done
[ -n "$package" ] || usage

tmp="$(mktemp -d "${TMPDIR:-/tmp}/codeaf-shard-test.XXXXXX")"
trap 'rm -rf -- "$tmp"' EXIT
binary="$tmp/package.test"

# -p belongs only to compilation. Every execution below is the same freshly
# compiled binary, so -count would only repeat work and is intentionally gone.
if ! go test -c -o "$binary" ${compile_flags[@]+"${compile_flags[@]}"} "$package"; then
	exit 1
fi

info="$(go list -f '{{.ImportPath}}{{printf "\t"}}{{.Dir}}' "$package")"
if [ -z "$info" ] || [ "${info#*$'\n'}" != "$info" ]; then
	printf 'shard-test: package selector %q must resolve to exactly one package\n' "$package" >&2
	exit 2
fi
IFS=$'\t' read -r import_path package_dir <<<"$info"

# -test.skip applies to listing as well as execution. Listing through the same
# binary therefore makes the accounting set exactly the set the shards owe.
if ! (cd "$package_dir" && "$binary" -test.list "$user_run" ${list_flags[@]+"${list_flags[@]}"}) >"$tmp/list.raw" 2>"$tmp/list.err"; then
	cat "$tmp/list.err" >&2
	printf 'shard-test: could not list tests for %s\n' "$import_path" >&2
	exit 1
fi
LC_ALL=C sort "$tmp/list.raw" >"$tmp/tests"
listed_count="$(wc -l <"$tmp/tests" | tr -d ' ')"
unique_count="$(LC_ALL=C sort -u "$tmp/tests" | wc -l | tr -d ' ')"
if [ "$listed_count" -ne "$unique_count" ]; then
	printf 'shard-test: %s listed %s tests but only %s names were unique\n' "$import_path" "$listed_count" "$unique_count" >&2
	exit 1
fi

i=1
while [ "$i" -le "$shards" ]; do
	: >"$tmp/shard.$i.tests"
	i=$((i + 1))
done
index=0
while IFS= read -r test_name; do
	shard=$((index % shards + 1))
	printf '%s\n' "$test_name" >>"$tmp/shard.$shard.tests"
	index=$((index + 1))
done <"$tmp/tests"

# A trie keeps the selector below the kernel's per-argument limit even for a
# two-shard override. The suite has thousands of long names with shared words;
# a flat alternation can exceed that limit before the test binary starts.
make_pattern() {
	LC_ALL=C awk '
	function escaped(c) { return c ~ /[][(){}.*+?^$|\\]/ ? "\\" c : c }
	function emit(node,    edge,count,out,part) {
		count = terminal[node] ? 1 : 0
		out = ""
		for (edge = first[node]; edge; edge = sibling[edge]) {
			part = escaped(label[edge]) emit(destination[edge])
			if (count++) out = out "|"
			out = out part
		}
		return count > 1 ? "(?:" out ")" : out
	}
	BEGIN { nodes = 1 }
	{
		node = 1
		for (i = 1; i <= length($0); i++) {
			c = substr($0, i, 1)
			key = node SUBSEP c
			if (!(key in child)) {
				child[key] = ++nodes
				edge = ++edges
				label[edge] = c
				destination[edge] = child[key]
				if (last[node]) sibling[last[node]] = edge; else first[node] = edge
				last[node] = edge
			}
			node = child[key]
		}
		terminal[node] = 1
	}
	END { print "^" emit(1) "$" }
	' "$1"
}

run_shard() {
	local number="$1" pattern started elapsed status
	pattern="$(make_pattern "$tmp/shard.$number.tests")"
	started=$SECONDS
	if [ -n "$json" ]; then
		if (cd "$package_dir" && go tool test2json -t -p "$import_path" "$binary" -test.v=test2json ${run_flags[@]+"${run_flags[@]}"} "-test.run=$pattern") >"$tmp/shard.$number.out" 2>&1; then
			status=0
		else
			status=$?
		fi
	else
		if (cd "$package_dir" && "$binary" ${run_flags[@]+"${run_flags[@]}"} -test.v "-test.run=$pattern") >"$tmp/shard.$number.out" 2>&1; then
			status=0
		else
			status=$?
		fi
	fi
	elapsed=$((SECONDS - started))
	printf '%s %s\n' "$status" "$elapsed" >"$tmp/shard.$number.status"
	return 0
}

declare -a pids=()
run_started=$SECONDS
i=1
while [ "$i" -le "$shards" ]; do
	run_shard "$i" &
	pids[$i]=$!
	i=$((i + 1))
done
i=1
while [ "$i" -le "$shards" ]; do
	wait "${pids[$i]}"
	i=$((i + 1))
done

status=0
: >"$tmp/ran"
: >"$tmp/failed"
i=1
while [ "$i" -le "$shards" ]; do
	read -r shard_status shard_elapsed <"$tmp/shard.$i.status"
	if [ -n "$json" ]; then
		sed -nE 's/.*"Action":"(pass|fail|skip)".*"Test":"(Test[^"]*)".*/\2/p' "$tmp/shard.$i.out" | grep -v / >>"$tmp/ran" || true
		sed -n 's/.*"Action":"fail".*"Test":"\(Test[^"]*\)".*/\1/p' "$tmp/shard.$i.out" | grep -v / >>"$tmp/failed" || true
	else
		sed -nE 's/^--- (PASS|FAIL|SKIP): (Test[^[:space:]]*).*/\2/p' "$tmp/shard.$i.out" | grep -v / >>"$tmp/ran" || true
		sed -n 's/^--- FAIL: \(Test[^[:space:]]*\).*/\1/p' "$tmp/shard.$i.out" | grep -v / >>"$tmp/failed" || true
		sed -n 's/^[[:space:]]*\(Test[^[:space:]]*\) ([^)]*)$/\1/p' "$tmp/shard.$i.out" | grep -v / >>"$tmp/failed" || true
	fi
	if [ "$shard_status" -ne 0 ]; then status=1; fi
	i=$((i + 1))
done
LC_ALL=C sort "$tmp/ran" >"$tmp/ran.sorted"
ran_count="$(wc -l <"$tmp/ran.sorted" | tr -d ' ')"
if ! cmp -s "$tmp/tests" "$tmp/ran.sorted"; then
	status=1
	comm -23 "$tmp/tests" "$tmp/ran.sorted" >"$tmp/missing"
	if [ ! -s "$tmp/failed" ]; then cat "$tmp/missing" >>"$tmp/failed"; fi
	printf 'shard-test: %s listed %s tests but the shards reported %s terminal test results\n' "$import_path" "$listed_count" "$ran_count" >&2
fi
LC_ALL=C sort -u "$tmp/failed" >"$tmp/failed.sorted"
failed_names="$(paste -sd, "$tmp/failed.sorted")"
wall=$((SECONDS - run_started))

if [ -n "$json" ]; then
	printf '{"Action":"start","Package":"%s"}\n' "$import_path"
	i=1
	while [ "$i" -le "$shards" ]; do
		while IFS= read -r line; do
			case "$line" in
				*'"Test":'*) printf '%s\n' "$line" ;;
				*'"Action":"start"'*) ;;
				*'"Action":"pass"'* | *'"Action":"fail"'* | *'"Action":"skip"'*) ;;
				*) printf '%s\n' "$line" ;;
			esac
		done <"$tmp/shard.$i.out"
		i=$((i + 1))
	done
	if [ "$status" -eq 0 ]; then action=pass; else action=fail; fi
	printf '{"Action":"%s","Package":"%s","Elapsed":%s}\n' "$action" "$import_path" "$wall"
	if [ "$status" -eq 0 ]; then
		printf 'ok  %s  %s shards  %ss\n' "$import_path" "$shards" "$wall" >&2
	else
		printf 'FAIL  %s  %s shards  %ss  failing: %s\n' "$import_path" "$shards" "$wall" "${failed_names:-unknown}" >&2
	fi
	if [ "$status" -eq 0 ]; then exit 0; else exit 1; fi
fi

i=1
while [ "$i" -le "$shards" ]; do
	read -r shard_status shard_elapsed <"$tmp/shard.$i.status"
	if [ "$shard_status" -eq 0 ]; then
		printf 'ok  %s  shard %s/%s  %ss\n' "$import_path" "$i" "$shards" "$shard_elapsed"
	else
		printf 'FAIL  %s  shard %s/%s  %ss\n' "$import_path" "$i" "$shards" "$shard_elapsed"
		cat "$tmp/shard.$i.out"
	fi
	i=$((i + 1))
done
if [ "$status" -eq 0 ]; then
	printf 'ok  %s  %s shards  %ss\n' "$import_path" "$shards" "$wall"
	exit 0
fi
printf 'FAIL  %s  %s shards  %ss  failing: %s\n' "$import_path" "$shards" "$wall" "${failed_names:-unknown}"
exit 1
