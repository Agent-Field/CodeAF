#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
BIN="$ROOT/target/release/furrow"
WORK=$(mktemp -d "${TMPDIR:-/tmp}/furrow-mission-control.XXXXXX")
REPO="$WORK/project"
AUTH="$WORK/auth-hardening"
PASSKEY="$WORK/passkey-rollout"
DOCS="$WORK/docs-refresh"
export FURROW_DATA_DIR="$WORK/data"
export FURROW_NO_DAEMON=1

cargo build --release --quiet --manifest-path "$ROOT/Cargo.toml"
mkdir -p "$REPO/src"
git -C "$REPO" init -q -b main
git -C "$REPO" config user.email demo@furrow.local
git -C "$REPO" config user.name "Furrow Demo"
printf 'export function authenticate(user: string) {\n  return user.length > 0;\n}\n' >"$REPO/src/auth.ts"
printf 'export const port = 4173;\n' >"$REPO/src/config.ts"
printf '# Parallel agent demo\n' >"$REPO/README.md"
printf '.env\n' >"$REPO/.gitignore"
git -C "$REPO" add .
git -C "$REPO" commit -q -m "initial workspace"
printf 'SESSION_KEY=local-demo-only\n' >"$REPO/.env"

"$BIN" --repo "$REPO" watch --no-daemon >/dev/null
"$BIN" --repo "$REPO" snap -m "baseline before parallel work" >/dev/null
printf 'Mission Control demo: %s\n' "$WORK"
printf 'Watch real furrow commands create three universes and open a collision.\n'
printf 'Press Ctrl-C to stop the local UI server. The demo workspaces remain on disk.\n'

scenario() {
  sleep 2
  printf '\n[furrow] creating a clear documentation universe\n'
  "$BIN" --repo "$REPO" fork docs-refresh --destination "$DOCS" >/dev/null
  printf '\nOperational notes for the next release.\n' >>"$DOCS/README.md"
  "$BIN" --repo "$DOCS" snap -m "documentation agent updated release notes" >/dev/null

  sleep 3
  printf '[furrow] security agent claims and edits src/auth.ts\n'
  "$BIN" --repo "$REPO" fork auth-hardening --destination "$AUTH" >/dev/null
  "$BIN" --repo "$AUTH" claim src/auth.ts --owner security-agent >/dev/null
  printf 'export function authenticate(user: string) {\n  return user.length >= 8; // hardened policy\n}\n' >"$AUTH/src/auth.ts"
  "$BIN" --repo "$AUTH" snap -m "security agent hardened authentication" >/dev/null

  sleep 3
  printf '[furrow] identity agent edits the same path; conflict radar opens\n'
  "$BIN" --repo "$REPO" fork passkey-rollout --destination "$PASSKEY" >/dev/null
  printf 'export function authenticate(user: string) {\n  return user.startsWith("passkey:");\n}\n' >"$PASSKEY/src/auth.ts"
  "$BIN" --repo "$PASSKEY" snap -m "identity agent added passkeys" >/dev/null
  printf '[furrow] live scenario ready: inspect the diffs and merge previews in Mission Control\n'
}

scenario &
if [[ "${FURROW_DEMO_NO_OPEN:-0}" == "1" ]]; then
  exec "$BIN" --repo "$REPO" ui --no-open --merge-check "git diff --check"
fi
exec "$BIN" --repo "$REPO" ui --merge-check "git diff --check"
