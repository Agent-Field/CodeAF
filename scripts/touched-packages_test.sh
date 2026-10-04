#!/usr/bin/env bash
# Real commits in a throwaway repository pin the selection contract. The
# working repository's index and history are never touched by this fixture.
set -euo pipefail
export GOFLAGS=-buildvcs=false
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d /tmp/codeaf-touched-packages-test.XXXXXX)"
trap 'rm -rf -- "$tmp"' EXIT
export GIT_AUTHOR_NAME='tooling fixture' GIT_AUTHOR_EMAIL=fixture@example.invalid
export GIT_COMMITTER_NAME="$GIT_AUTHOR_NAME" GIT_COMMITTER_EMAIL="$GIT_AUTHOR_EMAIL"

fixture() {
	mkdir -p "$tmp/$1"
	cd "$tmp/$1"
	git init -q
	printf 'module example.invalid/fixture\n\ngo 1.23\n' >go.mod
	for package in internal/tui3 internal/session cmd/codeaf internal/one internal/two emptied; do
		mkdir -p "$package"
		printf 'package fixture\n' >"$package/file.go"
	done
	mkdir -p bench/bashloop/fixtures/nested/deep
	printf 'module example.invalid/nested\n\ngo 1.23\n' >bench/bashloop/fixtures/nested/go.mod
	printf 'package fixture\n' >bench/bashloop/fixtures/nested/deep/file.go
	printf 'docs\n' >README.md
	git add go.mod README.md internal cmd emptied bench
	git commit -qm base
	BASE="$(git rev-parse HEAD)"; export BASE
}

finish() {
	git add "$@"
	git commit -qm change
}

expect() {
	local actual
	actual="$("$root/scripts/touched-packages.sh")"
	[ "$actual" = "$1" ] || { printf 'packages: got <%s>, want <%s>\n' "$actual" "$1" >&2; exit 1; }
	printf '%s\n' "$actual" | python3 "$root/scripts/touched-matrix.py" >"$tmp/matrix"
	python3 - "$tmp/matrix" "$2" <<'PY'
import json, sys
outputs = dict(line.strip().split('=', 1) for line in open(sys.argv[1]))
legs = json.loads(outputs['matrix'])['include']
assert {row['leg'] for row in legs} == set(sys.argv[2].split()), legs
assert outputs['has-tests'] == ('true' if legs else 'false'), outputs
selected = [package for row in legs for package in row['packages'].split()]
assert len(selected) == len(set(selected)), legs
PY
}

fixture docs
printf 'more docs\n' >>README.md
finish README.md
expect '' ''

for leg in tui3 session codeaf; do
	fixture "$leg"
	case "$leg" in codeaf) package=cmd/codeaf ;; *) package=internal/$leg ;; esac
	printf '// Changed source.\n' >>"$package/file.go"
	finish "$package/file.go"
	expect "./$package" "$leg"
done

fixture rest
printf '// Changed source.\n' >>internal/one/file.go
printf '// Changed source.\n' >>internal/two/file.go
finish internal/one/file.go internal/two/file.go
expect $'./internal/one\n./internal/two' rest

fixture mixed
for package in internal/tui3 internal/session cmd/codeaf internal/one; do
	printf '// Changed source.\n' >>"$package/file.go"
done
finish internal/tui3/file.go internal/session/file.go cmd/codeaf/file.go internal/one/file.go
expect $'./cmd/codeaf\n./internal/one\n./internal/session\n./internal/tui3' 'codeaf rest session tui3'

for file in go.mod go.sum; do
	fixture "$file"
	if [ "$file" = go.mod ]; then printf '\n' >>go.mod; else touch go.sum; fi
	finish "$file"
	expect $'./cmd/codeaf\n./emptied\n./internal/one\n./internal/session\n./internal/tui3\n./internal/two' 'codeaf rest session tui3'
done

fixture excluded
rm emptied/file.go
printf '// Changed fixture.\n' >>bench/bashloop/fixtures/nested/deep/file.go
finish emptied/file.go bench/bashloop/fixtures/nested/deep/file.go
expect '' ''

# The real local target must consume exactly the same shared script. Substitute
# only the costly suite runner, then compare its package arguments with C2.
fixture local
mkdir scripts
cp "$root/scripts/touched-packages.sh" scripts/touched-packages.sh
cp "$root/scripts/touched-matrix.py" scripts/touched-matrix.py
cat >scripts/touched-verdict.sh <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$@" >>"$TOUCHED_ARGS"
SH
chmod +x scripts/touched-verdict.sh
printf '// Changed source.\n' >>internal/tui3/file.go
finish internal/tui3/file.go
TOUCHED_ARGS="$tmp/local-args" make -s -f "$root/Makefile" test-touched >/dev/null
python3 - "$tmp/local-args" <<'PY'
import sys
arguments = open(sys.argv[1]).read().splitlines()
assert arguments[0] == 'run', arguments
assert arguments[-1] == './internal/tui3', arguments
assert len([arg for arg in arguments if arg.startswith('./')]) == 1, arguments
PY

# C5 also holds across all four legs: no package is lost or repeated when the
# laptop executes the partition sequentially instead of on separate runners.
: >"$tmp/local-args"
for package in cmd/codeaf internal/session internal/one internal/two; do
	printf '// Changed source.\n' >>"$package/file.go"
done
finish cmd/codeaf/file.go internal/session/file.go internal/one/file.go internal/two/file.go
TOUCHED_ARGS="$tmp/local-args" make -s -f "$root/Makefile" test-touched >/dev/null
python3 - "$tmp/local-args" <<'PYTEST'
import sys
arguments = open(sys.argv[1]).read().splitlines()
assert arguments.count('run') == 4, arguments
packages = [argument for argument in arguments if argument.startswith('./')]
assert sorted(packages) == ['./cmd/codeaf', './internal/one', './internal/session', './internal/tui3', './internal/two'], arguments
PYTEST

printf 'touched-package selection acceptance: ok\n'
