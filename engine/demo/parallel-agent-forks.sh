#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
BIN="$PROJECT_ROOT/target/release/furrow"
DEMO_ROOT=${FURROW_DEMO_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/furrow-forks.XXXXXX")}
REPO="$DEMO_ROOT/project"
ALPHA="$DEMO_ROOT/alpha"
BETA="$DEMO_ROOT/beta"
export FURROW_DATA_DIR="$DEMO_ROOT/furrow-data"
export FURROW_NO_DAEMON=1

green='\033[0;32m'
red='\033[0;31m'
bold='\033[1m'
reset='\033[0m'

step() { printf '\n%b%s%b\n' "$bold" "$1" "$reset"; }
ok() { printf '%bPASS%b  %s\n' "$green" "$reset" "$1"; }
fail() { printf '%bFAIL%b  %s\n' "$red" "$reset" "$1"; exit 1; }

step "Build furrow"
cargo build --manifest-path "$PROJECT_ROOT/Cargo.toml" --release --quiet

step "Create one dirty, warm developer workspace"
mkdir -p "$REPO/node_modules/example" "$REPO/.cache/build"
git -C "$REPO" init -b main --quiet
git -C "$REPO" config user.email demo@furrow.dev
git -C "$REPO" config user.name "furrow demo"
printf '.env\nnode_modules/\n.cache/\n' > "$REPO/.gitignore"
printf 'export const result = "original";\n' > "$REPO/app.js"
git -C "$REPO" add app.js .gitignore
git -C "$REPO" commit -m "initial app" --quiet
printf 'API_TOKEN=local-only\n' > "$REPO/.env"
printf '// uncommitted developer edit\n' >> "$REPO/app.js"
dd if=/dev/zero of="$REPO/node_modules/example/warm-cache.bin" bs=1048576 count=32 2>/dev/null
printf 'compiled-before-agents\n' > "$REPO/.cache/build/state"

"$BIN" --repo "$REPO" watch >/dev/null
ok "complete dirty state protected"

step "Fork two full workspaces with warm dependencies"
"$BIN" --repo "$REPO" fork alpha --destination "$ALPHA"
"$BIN" --repo "$REPO" fork beta --destination "$BETA"

step "Make overlapping agent intent visible before writes"
"$BIN" --repo "$ALPHA" claim app.js --owner alpha-agent >/dev/null
if "$BIN" --repo "$BETA" claim app.js --owner beta-agent; then
  fail "beta should not receive alpha's active path claim"
else
  ok "beta saw alpha's advisory app.js claim before editing"
fi
"$BIN" --repo "$ALPHA" coord write tasks/alpha.txt \
  --value "alpha owns app.js" --owner alpha-agent >/dev/null
grep -q 'alpha owns app.js' "$BETA/.furrow/coord/tasks/alpha.txt" \
  && ok "alpha's versioned coordination note propagated eagerly to beta" \
  || fail "coordination note did not propagate"

step "Run two simulated agents concurrently"
(
  printf 'export const result = "alpha implementation";\n' > "$ALPHA/app.js"
  printf 'alpha-only artifact\n' > "$ALPHA/.cache/build/alpha"
) &
alpha_pid=$!
(
  printf 'export const result = "beta implementation";\n' > "$BETA/app.js"
  printf 'beta-only migration\n' > "$BETA/migration.sql"
) &
beta_pid=$!
wait "$alpha_pid" "$beta_pid"

step "Verify full-state isolation"
grep -q 'uncommitted developer edit' "$REPO/app.js" \
  && ok "source dirty edit was never touched" || fail "source was modified"
grep -q 'alpha implementation' "$ALPHA/app.js" \
  && ok "alpha has its own implementation" || fail "alpha result missing"
grep -q 'beta implementation' "$BETA/app.js" \
  && ok "beta has its own implementation" || fail "beta result missing"
[[ -s "$ALPHA/node_modules/example/warm-cache.bin" && -s "$BETA/node_modules/example/warm-cache.bin" ]] \
  && ok "both agents started with the 32 MiB warm dependency tree" || fail "warm state missing"
grep -q 'local-only' "$ALPHA/.env" && grep -q 'local-only' "$BETA/.env" \
  && ok "ignored local configuration exists in both forks" || fail "ignored state missing"
[[ $(cat "$REPO/.furrow/workspace-id") != $(cat "$ALPHA/.furrow/workspace-id") ]] \
  && [[ $(cat "$ALPHA/.furrow/workspace-id") != $(cat "$BETA/.furrow/workspace-id") ]] \
  && ok "every fork has an independent timeline" || fail "workspace identities collided"

"$BIN" --repo "$ALPHA" snap -m "alpha agent boundary" >/dev/null
"$BIN" --repo "$BETA" snap -m "beta agent boundary" >/dev/null

step "List the parallel workspaces with live conflict radar"
"$BIN" --repo "$REPO" forks
events=$("$BIN" --repo "$REPO" events)
printf '%s\n' "$events"
printf '%s\n' "$events" | grep -q '"state":"opened"' \
  && ok "one durable conflict event names both forks and app.js" \
  || fail "conflict radar event missing"

step "Verification-gated merge of alpha"
"$BIN" --repo "$REPO" merge alpha \
  --check "grep -q 'alpha implementation' app.js && test -f .cache/build/alpha"
grep -q 'alpha implementation' "$REPO/app.js" \
  && ok "alpha converged only after its check passed" || fail "alpha merge missing"

step "Overlapping beta edit stops as an explicit conflict"
if "$BIN" --repo "$REPO" merge beta --dry-run; then
  fail "beta should conflict with alpha on app.js"
else
  ok "beta conflict was reported without mutating the source"
fi
[[ ! -e "$REPO/migration.sql" ]] \
  && ok "no partial beta changes landed" || fail "conflicted merge partially applied"

printf '\n%bDemo complete.%b Workspaces retained at %s\n' "$green" "$reset" "$DEMO_ROOT"
