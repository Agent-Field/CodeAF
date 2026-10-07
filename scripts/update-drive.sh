#!/usr/bin/env bash
# Capture the update offer's deterministic fixtures from THIS worktree's binary.
#
#   scripts/update-drive.sh                  # the whole matrix
#   scripts/update-drive.sh offer 80         # one case at one width
#
# Every fixture disables the updater's hooks (internal/tui3/updatedemo.go), and
# this drive adds to that: a home and workspace made fresh under mktemp, a
# private tmux server, an EMPTY inherited environment, and `chat --no-host` so
# no session host is left behind. Nothing here reaches a release host, installs
# anything, or writes outside the temporary directory, which is removed on exit.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${UPDATE_BIN:-$ROOT/bin/codeaf}"
OUT="${UPDATE_OUT:-$ROOT/docs/design/background-updates-frames}"
CASES=(offer downloading ready ready-working manual off failure)
WIDTHS=(80 120)
if [ "${1:-}" = "--all" ]; then
  shift
  CASES=(offer downloading ready ready-working manual off failure)
  WIDTHS=(80 120)
fi

[ -x "$BIN" ] || { echo "no binary at $BIN — run make build" >&2; exit 1; }
command -v tmux >/dev/null || { echo "tmux is required for a screen drive" >&2; exit 1; }

TMP="$(mktemp -d "${TMPDIR:-/tmp}/codeaf-update-drive.XXXXXX")"
SOCK="codeaf-update-$$"
cleanup() {
  tmux -L "$SOCK" kill-server 2>/dev/null || true
  rm -rf "$TMP"
}
trap cleanup EXIT

HOME_DIR="$TMP/home"
WORK="$TMP/work/project"
mkdir -p "$HOME_DIR" "$WORK" "$OUT"
cat >"$HOME_DIR/config.json" <<'JSON'
{
  "setup_seen_at": "2026-10-07T00:00:00Z",
  "api_key": "demo-key-not-used",
  "model.talk": "deepseek/deepseek-v4.1-flash",
  "models.tiers.high": "deepseek/deepseek-v4.1-flash",
  "models.tiers.worker": "deepseek/deepseek-v4.1-flash",
  "models.tiers.reflex": "deepseek/deepseek-v4.1-flash",
  "budget.daily_usd": "20",
  "ui.mouse": "on"
}
JSON

# capture <case> <cols>
capture() {
  local case="$1" cols="$2"
  local session="update-$case-$cols" file="$OUT/$case-${cols}c.txt"
  tmux -L "$SOCK" kill-session -t "$session" 2>/dev/null || true
  tmux -L "$SOCK" new-session -d -s "$session" -x "$cols" -y 40 \
    "cd $(printf '%q' "$WORK") && env -i \
      HOME=$(printf '%q' "$HOME_DIR") \
      CODEAF_HOME=$(printf '%q' "$HOME_DIR") \
      CODEAF_PROFILE_DIR=$(printf '%q' "$HOME_DIR") \
      CODEAF_NO_UPDATE_CHECK=1 \
      TERM=xterm-256color \
      LANG=C.UTF-8 \
      PATH=/usr/bin:/bin \
      CODEAF_UPDATE_DEMO=$(printf '%q' "$case") \
      $(printf '%q' "$BIN") chat --no-host"
  sleep 3
  tmux -L "$SOCK" capture-pane -p -t "$session" >"$file"
  tmux -L "$SOCK" capture-pane -p -e -t "$session" >"$OUT/$case-${cols}c.ansi"
  tmux -L "$SOCK" kill-session -t "$session" 2>/dev/null || true
  # A CAPTURE THAT DID NOT DRAW THE FIXTURE IS A FAILURE, not a file. Every
  # fixture writes its own name last, so this checks the real surface ran.
  grep -q "CODEAF_UPDATE_DEMO=$case" "$file" || {
    echo "capture $case at ${cols}c did not draw the fixture:" >&2
    cat "$file" >&2
    exit 1
  }
  echo "$file"
}

if [ $# -gt 0 ]; then
  capture "$1" "${2:-80}"
  exit 0
fi

for case in "${CASES[@]}"; do
  for cols in "${WIDTHS[@]}"; do
    capture "$case" "$cols"
  done
done
