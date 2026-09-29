#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
BIN="$ROOT/target/release/furrow"
WORK=$(mktemp -d "${TMPDIR:-/tmp}/furrow-bisect-demo.XXXXXX")
REPO="$WORK/project"
export FURROW_DATA_DIR="$WORK/data"
export FURROW_NO_DAEMON=1

bold='\033[1m'
green='\033[0;32m'
reset='\033[0m'

step() { printf '\n%b%s%b\n' "$bold" "$1" "$reset"; }
ok() { printf '%bPASS%b  %s\n' "$green" "$reset" "$1"; }
fail() { printf 'FAIL  %s\n' "$1" >&2; exit 1; }

step "Build furrow"
cargo build --release --quiet --manifest-path "$ROOT/Cargo.toml"

step "Create a warm project timeline"
mkdir -p "$REPO/node_modules/runtime"
git -C "$REPO" init -q -b main
git -C "$REPO" config user.email demo@example.com
git -C "$REPO" config user.name Demo
printf 'node_modules/\n' >"$REPO/.gitignore"
printf 'status=good\n' >"$REPO/app.conf"
dd if=/dev/zero of="$REPO/node_modules/runtime/cache.bin" bs=1048576 count=16 2>/dev/null
git -C "$REPO" add .gitignore app.conf
git -C "$REPO" commit -q -m initial
"$BIN" --repo "$REPO" watch --no-daemon >/dev/null

printf 'one\n' >"$REPO/feature.txt"
"$BIN" --repo "$REPO" snap -m "feature started" >/dev/null
printf 'two\n' >>"$REPO/feature.txt"
"$BIN" --repo "$REPO" snap -m "feature still passes" >/dev/null
printf 'status=broken\n' >"$REPO/app.conf"
FIRST_BAD=$("$BIN" --repo "$REPO" --json snap -m "regression introduced" \
  | sed -n 's/.*"snapshot":"\([0-9a-f]*\)".*/\1/p')
printf 'later work\n' >"$REPO/later.txt"
"$BIN" --repo "$REPO" snap -m "later failing state" >/dev/null
printf 'more work\n' >>"$REPO/later.txt"
"$BIN" --repo "$REPO" snap -m "latest failing state" >/dev/null
test "${#FIRST_BAD}" -eq 64 || fail "could not record the expected bad snapshot"
ok "six complete states recorded around one regression"

step "Find the first failure with logarithmic checks"
REPORT="$WORK/bisect-report"
"$BIN" --repo "$REPO" bisect -- /bin/sh -c \
  'grep -q "status=good" app.conf; code=$?; printf "probe-only\n" > app.conf; exit $code' \
  | tee "$REPORT"
FOUND=$(sed -n 's/^First bad: //p' "$REPORT")
CHECKS=$(grep -Ec '^[0-9a-f]{12}  ' "$REPORT" || true)
test "$FOUND" = "$FIRST_BAD" || fail "bisect selected the wrong snapshot"
test "$CHECKS" -le 5 || fail "bisect used more than logarithmic checks"
grep -q 'status=broken' "$REPO/app.conf" || fail "probe side effects reached the source"
ok "first bad state found in $CHECKS isolated checks; source stayed untouched"

printf '\n%bDemo complete.%b Workspace retained at %s\n' "$green" "$reset" "$WORK"
