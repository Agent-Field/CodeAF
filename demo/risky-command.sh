#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
BIN="$ROOT/target/release/furrow"
WORK=$(mktemp -d "${TMPDIR:-/tmp}/furrow-try.XXXXXX")
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

step "Create an ordinary repository with state Git cannot restore"
mkdir -p "$REPO/cache"
git -C "$REPO" init -q -b main
git -C "$REPO" config user.email demo@example.com
git -C "$REPO" config user.name Demo
printf 'API_TOKEN=irreplaceable\n' >"$REPO/.env"
printf '.env\ncache/\n' >"$REPO/.gitignore"
printf 'console.log("working");\n' >"$REPO/app.js"
printf 'warm dependency bytes\n' >"$REPO/cache/dependency.bin"
git -C "$REPO" add .gitignore app.js
git -C "$REPO" commit -q -m initial

step "Run a destructive command with no prior furrow setup"
REPORT="$WORK/try-report"
"$BIN" --repo "$REPO" try -m "destructive cleanup" -- \
  /bin/sh -c 'rm -rf .env cache; printf "%s\n" '\''console.log("broken");'\'' > app.js' \
  2>"$REPORT"
cat "$REPORT"
BEFORE=$(sed -n 's/^Protected //p' "$REPORT")
test "${#BEFORE}" -eq 64 || fail "furrow did not report an exact restore point"
test ! -e "$REPO/.env" || fail "the demo command did not delete .env"
grep -q broken "$REPO/app.js" || fail "the demo command did not damage app.js"
ok "the risky command ran normally and its damage is visible"

step "Undo the complete command"
"$BIN" --repo "$REPO" rewind "$BEFORE" --yes >/dev/null
grep -q 'API_TOKEN=irreplaceable' "$REPO/.env" \
  || fail "ignored secret was not restored"
grep -q 'console.log("working")' "$REPO/app.js" \
  || fail "tracked source was not restored"
grep -q 'warm dependency bytes' "$REPO/cache/dependency.bin" \
  || fail "ignored dependency cache was not restored"
ok "tracked, ignored, and dependency state recovered from one restore point"

printf '\n%bDemo complete.%b Workspace retained at %s\n' "$green" "$reset" "$WORK"
