#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
BIN="$PROJECT_ROOT/target/release/furrow"
DEMO_ROOT=${FURROW_DEMO_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/furrow-demo.XXXXXX")}
REPO="$DEMO_ROOT/project"
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

step "Create a realistic agent workspace"
mkdir -p "$REPO/node_modules/demo-package" "$REPO/dist"
git -C "$REPO" init -b main --quiet
git -C "$REPO" config user.email demo@furrow.dev
git -C "$REPO" config user.name "furrow demo"
cat > "$REPO/.gitignore" <<'EOF'
.env
dev.sqlite*
node_modules/
dist/
EOF
cat > "$REPO/app.js" <<'EOF'
export function greeting(name) {
  return `hello ${name}`;
}
EOF
git -C "$REPO" add app.js .gitignore
git -C "$REPO" commit -m "initial app" --quiet

# Real state that Git does not protect.
printf 'API_TOKEN=local-development-secret\n' > "$REPO/.env"
printf 'developer reasoning that is not committed\n' > "$REPO/notes.txt"
dd if=/dev/zero of="$REPO/node_modules/demo-package/index.bin" bs=1024 count=512 2>/dev/null
printf 'warm-build-cache\n' > "$REPO/dist/cache.txt"
sqlite3 "$REPO/dev.sqlite" \
  "PRAGMA journal_mode=WAL; CREATE TABLE users(id INTEGER PRIMARY KEY, name TEXT); INSERT INTO users(name) VALUES('Ada');" \
  >/dev/null
printf '// dirty human work\n' >> "$REPO/app.js"

printf 'Workspace: %s\n' "$REPO"
printf 'Logical size: '
du -sh "$REPO" | awk '{print $1}'

step "Protect the complete working state"
SNAPSHOT=$(
  "$BIN" --repo "$REPO" --json watch \
    | sed -n 's/.*"snapshot":"\([0-9a-f]*\)".*/\1/p'
)
[[ ${#SNAPSHOT} -eq 64 ]] || fail "snapshot was not created"
ok "complete snapshot ${SNAPSHOT:0:12} is durable"

step "Simulate a destructive autonomous agent"
git -C "$REPO" clean -fdx >/dev/null
cat > "$REPO/app.js" <<'EOF'
throw new Error("agent destroyed the application");
EOF
printf 'Git can restore app.js, but .env, notes, dependencies, build state, and SQLite are gone.\n'
[[ ! -e "$REPO/.env" ]] || fail ".env should have been deleted by the simulation"
[[ ! -e "$REPO/dev.sqlite" ]] || fail "database should have been deleted by the simulation"

step "Preview the rewind"
"$BIN" --repo "$REPO" rewind "$SNAPSHOT" --dry-run

step "Restore everything, using the consistent SQLite image"
OUTPUT=$("$BIN" --repo "$REPO" rewind "$SNAPSHOT" --sqlite-consistent --yes)
printf '%s\n' "$OUTPUT"

step "Verify independently"
grep -q 'local-development-secret' "$REPO/.env" && ok "ignored .env restored" || fail ".env mismatch"
grep -q 'developer reasoning' "$REPO/notes.txt" && ok "untracked notes restored" || fail "notes mismatch"
grep -q 'dirty human work' "$REPO/app.js" && ok "dirty tracked edit restored" || fail "tracked edit mismatch"
[[ -s "$REPO/node_modules/demo-package/index.bin" ]] && ok "warm dependency state restored" || fail "dependency missing"
grep -q 'warm-build-cache' "$REPO/dist/cache.txt" && ok "build cache restored" || fail "build cache mismatch"
[[ $(sqlite3 "$REPO/dev.sqlite" 'SELECT name FROM users WHERE id=1;') == Ada ]] \
  && ok "SQLite row restored" || fail "SQLite data mismatch"
[[ $(sqlite3 "$REPO/dev.sqlite" 'PRAGMA integrity_check;') == ok ]] \
  && ok "SQLite integrity check passes" || fail "SQLite integrity failed"

step "Timeline proves the rewind is reversible"
"$BIN" --repo "$REPO" timeline

printf '\n%bDemo complete.%b Workspace retained at %s\n' "$green" "$reset" "$DEMO_ROOT"
