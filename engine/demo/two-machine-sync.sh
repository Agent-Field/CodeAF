#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
BIN="$PROJECT_ROOT/target/release/furrow"
DEMO_ROOT=${FURROW_DEMO_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/furrow-sync.XXXXXX")}
LAPTOP="$DEMO_ROOT/laptop/project"
DESKTOP="$DEMO_ROOT/desktop/project"
REMOTE="$DEMO_ROOT/encrypted-remote"
LAPTOP_DATA="$DEMO_ROOT/laptop/furrow-data"
DESKTOP_DATA="$DEMO_ROOT/desktop/furrow-data"
export FURROW_NO_DAEMON=1

green='\033[0;32m'
red='\033[0;31m'
bold='\033[1m'
reset='\033[0m'

step() { printf '\n%b%s%b\n' "$bold" "$1" "$reset"; }
ok() { printf '%bPASS%b  %s\n' "$green" "$reset" "$1"; }
fail() { printf '%bFAIL%b  %s\n' "$red" "$reset" "$1"; exit 1; }
laptop() { FURROW_DATA_DIR="$LAPTOP_DATA" "$BIN" --repo "$LAPTOP" "$@"; }
desktop() { FURROW_DATA_DIR="$DESKTOP_DATA" "$BIN" --repo "$DESKTOP" "$@"; }

step "Build furrow"
cargo build --manifest-path "$PROJECT_ROOT/Cargo.toml" --release --quiet

step "Create independent laptop and desktop repositories"
mkdir -p "$LAPTOP"
git -C "$LAPTOP" init -b main --quiet
git -C "$LAPTOP" config user.email demo@furrow.dev
git -C "$LAPTOP" config user.name "furrow demo"
printf '.env\n.cache/\n' > "$LAPTOP/.gitignore"
printf 'export const machine = "base";\n' > "$LAPTOP/app.js"
git -C "$LAPTOP" add app.js .gitignore
git -C "$LAPTOP" commit -m "initial app" --quiet
mkdir -p "$(dirname "$DESKTOP")"
git clone --quiet "$LAPTOP" "$DESKTOP"

printf 'API_TOKEN=never-visible-to-remote\n' > "$LAPTOP/.env"
printf 'unfinished laptop reasoning\n' > "$LAPTOP/notes.txt"
mkdir -p "$LAPTOP/.cache"
dd if=/dev/zero of="$LAPTOP/.cache/warm.bin" bs=1048576 count=8 2>/dev/null
printf 'export const machine = "dirty laptop";\n' > "$LAPTOP/app.js"
laptop watch --no-daemon >/dev/null
desktop watch --no-daemon >/dev/null
ok "both machines have independent stores and timelines"

step "Pair through a dumb developer-owned directory"
PAIR=$(laptop --json pair "$REMOTE" --name viral-demo)
KEY=$(printf '%s' "$PAIR" | sed -n 's/.*"key_hex": "\([^"]*\)".*/\1/p')
[[ ${#KEY} -eq 64 ]] || fail "pairing key was not generated"
desktop pair "$REMOTE" --name viral-demo --key "$KEY" >/dev/null
ok "remote namespace paired with a private 256-bit key"

step "Move the exact dirty workspace to the desktop"
laptop sync --push
desktop sync --pull --bootstrap
cmp "$LAPTOP/app.js" "$DESKTOP/app.js" || fail "dirty tracked file mismatch"
cmp "$LAPTOP/.env" "$DESKTOP/.env" || fail "ignored secret mismatch"
cmp "$LAPTOP/notes.txt" "$DESKTOP/notes.txt" || fail "untracked notes mismatch"
cmp "$LAPTOP/.cache/warm.bin" "$DESKTOP/.cache/warm.bin" || fail "warm cache mismatch"
ok "tracked, ignored, untracked, and warm dependency state arrived exactly"

if grep -R -a -q 'never-visible-to-remote' "$REMOTE"; then
  fail "remote leaked plaintext workspace bytes"
fi
ok "remote contains opaque names and authenticated ciphertext only"

step "Send only the next delta"
printf 'export const machine = "laptop delta";\n' > "$LAPTOP/app.js"
laptop sync --push
desktop sync --pull
cmp "$LAPTOP/app.js" "$DESKTOP/app.js" || fail "delta did not fast-forward"
ok "desktop fast-forwarded without retransmitting the full workspace"

step "Preserve simultaneous offline work instead of overwriting it"
printf 'desktop offline idea\n' > "$DESKTOP/notes.txt"
printf 'export const machine = "new laptop branch";\n' > "$LAPTOP/app.js"
laptop sync --push
if desktop sync --pull; then
  fail "divergent desktop work should not be overwritten"
fi
grep -q 'desktop offline idea' "$DESKTOP/notes.txt" \
  && ok "desktop sibling was preserved byte-for-byte" \
  || fail "desktop work was lost"
grep -q 'new laptop branch' "$LAPTOP/app.js" \
  && ok "laptop sibling remains independently available" \
  || fail "laptop work was lost"

printf '\n%bDemo complete.%b Machines and encrypted remote retained at %s\n' \
  "$green" "$reset" "$DEMO_ROOT"
