#!/usr/bin/env bash
# frame.sh — capture text frames from the real aforge binary in a real terminal.
#
#   DEMO_HOME=… scripts/frame.sh <name> <cols> <rows> [key ...]
#
# Runs bin/aforge against a demo home on a PRIVATE tmux socket (never the shared
# server other sessions are using), in a window pinned to exactly <cols>x<rows>,
# sends each key with a settle pause, and writes the visible pane to
# docs/design/polish/frames/<name>.<cols>x<rows>.txt (plain) and .ans (colour).
set -uo pipefail

ROOT="${ROOT:-/home/santosh/af-polish}"
HOME_DIR="${DEMO_HOME:?set DEMO_HOME}"
OUT="${OUT:-$ROOT/docs/design/polish/frames}"
BIN="${BIN:-$ROOT/bin/aforge}"
SOCK="${SOCK:-polish}"
SETTLE="${SETTLE:-0.7}"
BOOT="${BOOT:-3.0}"

name="$1"; cols="$2"; rows="$3"; shift 3
sess="f$$"
T=(tmux -L "$SOCK")
mkdir -p "$OUT"

"${T[@]}" kill-session -t "$sess" 2>/dev/null
"${T[@]}" new-session -d -s "$sess" -x "$cols" -y "$rows" \
  "cd '$HOME_DIR/${SUBDIR:-aforge-v2}' && HOME='$HOME_DIR' TERM=xterm-256color COLORTERM=truecolor '$BIN' ${AFORGE_ARGS:-}"
"${T[@]}" set-option -t "$sess" -w window-size manual >/dev/null 2>&1
"${T[@]}" resize-window -t "$sess" -x "$cols" -y "$rows" >/dev/null 2>&1
sleep "$BOOT"

for k in "$@"; do
  "${T[@]}" send-keys -t "$sess" "$k"
  sleep "$SETTLE"
done
sleep "$SETTLE"

"${T[@]}" capture-pane -p    -t "$sess" > "$OUT/$name.${cols}x${rows}.txt"
"${T[@]}" capture-pane -p -e -t "$sess" > "$OUT/$name.${cols}x${rows}.ans"
"${T[@]}" kill-session -t "$sess" 2>/dev/null
echo "$OUT/$name.${cols}x${rows}.txt"
