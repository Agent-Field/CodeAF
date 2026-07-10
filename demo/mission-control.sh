#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
BIN="$ROOT/target/release/agit"
WORK=$(mktemp -d "${TMPDIR:-/tmp}/agit-mission-control.XXXXXX")
REPO="$WORK/project"
AUTH="$WORK/auth-hardening"
PASSKEY="$WORK/passkey-rollout"
export AGIT_DATA_DIR="$WORK/data"
export AGIT_NO_DAEMON=1

cargo build --release --quiet --manifest-path "$ROOT/Cargo.toml"
mkdir -p "$REPO/src"
git -C "$REPO" init -q -b main
git -C "$REPO" config user.email demo@agit.local
git -C "$REPO" config user.name "Agit Demo"
printf 'export function authenticate(user: string) {\n  return user.length > 0;\n}\n' >"$REPO/src/auth.ts"
printf 'export const port = 4173;\n' >"$REPO/src/config.ts"
printf '.env\n' >"$REPO/.gitignore"
git -C "$REPO" add .
git -C "$REPO" commit -q -m "initial workspace"
printf 'SESSION_KEY=local-demo-only\n' >"$REPO/.env"

"$BIN" --repo "$REPO" watch --no-daemon >/dev/null
"$BIN" --repo "$REPO" snap -m "baseline before parallel work" >/dev/null
"$BIN" --repo "$REPO" fork auth-hardening --destination "$AUTH" >/dev/null
"$BIN" --repo "$REPO" fork passkey-rollout --destination "$PASSKEY" >/dev/null
"$BIN" --repo "$AUTH" claim src/auth.ts --owner security-agent >/dev/null

printf 'export function authenticate(user: string) {\n  return user.length >= 8; // hardened policy\n}\n' >"$AUTH/src/auth.ts"
printf 'export function authenticate(user: string) {\n  return user.startsWith("passkey:");\n}\n' >"$PASSKEY/src/auth.ts"
"$BIN" --repo "$AUTH" snap -m "security agent hardened authentication" >/dev/null
"$BIN" --repo "$PASSKEY" snap -m "identity agent added passkeys" >/dev/null

printf 'Mission Control demo: %s\n' "$WORK"
printf 'Press Ctrl-C to stop the local UI server. The demo workspaces remain on disk.\n'
exec "$BIN" --repo "$REPO" ui
