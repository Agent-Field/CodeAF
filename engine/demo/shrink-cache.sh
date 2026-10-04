#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
BIN="$ROOT/target/release/furrow"
WORK=$(mktemp -d "${TMPDIR:-/tmp}/furrow-shrink.XXXXXX")
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

step "Create a warm project with a 24 MiB ignored dependency tree"
mkdir -p "$REPO/node_modules/runtime" "$REPO/node_modules/compiler"
git -C "$REPO" init -q -b main
git -C "$REPO" config user.email demo@example.com
git -C "$REPO" config user.name Demo
printf 'node_modules/\n' >"$REPO/.gitignore"
printf 'console.log("ready");\n' >"$REPO/app.js"
dd if=/dev/zero of="$REPO/node_modules/runtime/archive.bin" bs=1048576 count=16 2>/dev/null
dd if=/dev/zero of="$REPO/node_modules/compiler/cache.bin" bs=1048576 count=8 2>/dev/null
printf 'runtime sentinel\n' >"$REPO/node_modules/runtime/version.txt"
git -C "$REPO" add .gitignore app.js
git -C "$REPO" commit -q -m initial
"$BIN" --repo "$REPO" watch --no-daemon >/dev/null
ok "the complete warm workspace is already deduplicated in the local store"

step "Preview reclaimable workspace bytes without changing anything"
"$BIN" --repo "$REPO" shrink
test -e "$REPO/node_modules/runtime/archive.bin" \
  || fail "preview changed the workspace"
ok "preview was read-only"

step "Remove the redundant workspace copy with an exact undo point"
REPORT="$WORK/shrink-report"
"$BIN" --repo "$REPO" shrink --yes | tee "$REPORT"
BEFORE=$(sed -n 's/^Undo with: furrow rewind //p' "$REPORT")
test "${#BEFORE}" -eq 64 || fail "shrink did not report an exact restore point"
test ! -e "$REPO/node_modules" || fail "dependency tree was not removed"
grep -q ready "$REPO/app.js" || fail "source was changed"
grep -q 'Estimated net disk reclaimed:' "$REPORT" || fail "net savings were not reported"
ok "dependency bytes were reclaimed without touching source"

step "Restore the exact warm dependency tree"
"$BIN" --repo "$REPO" rewind "$BEFORE" --yes >/dev/null
test "$(wc -c <"$REPO/node_modules/runtime/archive.bin" | tr -d ' ')" = 16777216 \
  || fail "runtime archive was not restored"
grep -q 'runtime sentinel' "$REPO/node_modules/runtime/version.txt" \
  || fail "dependency metadata was not restored"
ok "the complete cache returned from the local snapshot"

printf '\n%bDemo complete.%b Workspace retained at %s\n' "$green" "$reset" "$WORK"
