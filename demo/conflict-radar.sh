#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
BIN="$ROOT/target/release/agit"
WORK=$(mktemp -d "${TMPDIR:-/tmp}/agit-radar.XXXXXX")
REPO="$WORK/project"
ALPHA="$WORK/alpha"
BETA="$WORK/beta"
export AGIT_DATA_DIR="$WORK/data"
export AGIT_NO_DAEMON=1

bold='\033[1m'
green='\033[0;32m'
reset='\033[0m'

step() { printf '\n%b%s%b\n' "$bold" "$1" "$reset"; }
ok() { printf '%bPASS%b  %s\n' "$green" "$reset" "$1"; }
fail() { printf 'FAIL  %s\n' "$1" >&2; exit 1; }

step "Build agit"
cargo build --release --quiet --manifest-path "$ROOT/Cargo.toml"

step "Create one complete dirty workspace"
mkdir -p "$REPO"
git -C "$REPO" init -q -b main
git -C "$REPO" config user.email demo@example.com
git -C "$REPO" config user.name Demo
printf 'export const auth = "base";\n' >"$REPO/auth.ts"
printf '.env\n' >"$REPO/.gitignore"
git -C "$REPO" add auth.ts .gitignore
git -C "$REPO" commit -q -m initial
printf 'TOKEN=local-only\n' >"$REPO/.env"
"$BIN" --repo "$REPO" watch --no-daemon >/dev/null

step "Fork two warm universes and publish intent"
"$BIN" --repo "$REPO" fork alpha --destination "$ALPHA" >/dev/null
"$BIN" --repo "$REPO" fork beta --destination "$BETA" >/dev/null
"$BIN" --repo "$ALPHA" claim auth.ts --owner alpha-agent >/dev/null

step "Two agents independently edit the same path"
printf 'export const auth = "alpha refresh tokens";\n' >"$ALPHA/auth.ts"
printf 'export const auth = "beta passkeys";\n' >"$BETA/auth.ts"
"$BIN" --repo "$ALPHA" snap -m "alpha tool boundary" >/dev/null
"$BIN" --repo "$BETA" snap -m "beta tool boundary" >/dev/null

step "Conflict radar groups the collision without blocking either agent"
forks=$("$BIN" --repo "$REPO" forks)
printf '%s\n' "$forks"
test "$(printf '%s\n' "$forks" | grep -c '1 conflict')" = 2 \
  || fail "both universes should report one conflict"
events=$("$BIN" --repo "$REPO" events)
printf '%s\n' "$events"
test "$(printf '%s\n' "$events" | grep -c '"state":"opened"')" = 1 \
  || fail "the path should produce exactly one grouped open event"
printf '%s\n' "$events" | grep -q '"claim_state":"covered"' \
  || fail "the event should carry alpha's advisory claim state"
ok "agents can route around one durable, cursor-addressable conflict event"

step "Beta reverts; the same conflict closes"
printf 'export const auth = "base";\n' >"$BETA/auth.ts"
"$BIN" --repo "$BETA" snap -m "beta rerouted" >/dev/null
forks=$("$BIN" --repo "$REPO" forks)
printf '%s\n' "$forks"
test "$(printf '%s\n' "$forks" | grep -c 'clear')" = 2 \
  || fail "both universes should be clear after rerouting"
events=$("$BIN" --repo "$REPO" events)
test "$(printf '%s\n' "$events" | grep -c '"state":"resolved"')" = 1 \
  || fail "the radar should emit one durable resolve event"
ok "the collision resolved before merge work began"

printf '\n%bDemo complete.%b Workspaces retained at %s\n' "$green" "$reset" "$WORK"
