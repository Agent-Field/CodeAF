#!/usr/bin/env bash
# THE SCREEN DRIVE FOR THE QUESTION VIEWS (lane P, tracking issue #830).
#
# It opens THIS worktree's own binary in a tmux session of its own, on a home of
# its own, with one of questiondemo.go's fixtures named in the environment, and
# saves what the terminal actually drew. Nothing here talks to a model, writes
# outside its throwaway home, or touches a session anybody else owns.
#
#   scripts/qv-drive.sh                       # the whole matrix
#   scripts/qv-drive.sh permission 120 dark rich
#
set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/bin/aforge"
OUT="${QV_OUT:-$HOME/af-qv-reports/P/screens}"
SESSION="af-qv-P-drive"
HOMEDIR="${QV_HOME:-/tmp/af-qv-P-drive-home}"

[ -x "$BIN" ] || { echo "no binary at $BIN — run make build" >&2; exit 1; }
mkdir -p "$OUT"

# A THROWAWAY HOME, built once and reused. The first-run setup is answered
# already so the fixture is what opens, and the crew rows name a model nothing
# will ever call.
setup_home() {
  rm -rf "$HOMEDIR"
  mkdir -p "$HOMEDIR"
  cat >"$HOMEDIR/config.json" <<'JSON'
{
  "setup_seen_at": "2026-09-11T00:00:00Z",
  "model.talk": "openai/gpt-4o-mini",
  "models.tiers.low": "openai/gpt-4o-mini",
  "models.tiers.high": "openai/gpt-4o-mini",
  "budget.daily_usd": "20",
  "ui.mouse": "on"
}
JSON
}

# capture <fixture> <cols> <theme> <tier> [keys...]
capture() {
  local fixture="$1" cols="$2" theme="$3" tier="$4"; shift 4
  local name="$fixture-${cols}c-$theme-$tier"
  [ $# -gt 0 ] && name="$name-$(echo "$*" | tr ' ' '-')"

  tmux kill-session -t "$SESSION" 2>/dev/null
  local env=(
    "HOME=$HOMEDIR" "AFORGE_HOME=$HOMEDIR" "AFORGE_PROFILE_DIR="
    "TERM=xterm-256color" "AFORGE_QUESTION_DEMO=$fixture"
    "OPENROUTER_API_KEY=demo-key-not-used"
  )
  case "$theme" in
    light) env+=("COLORFGBG=0;15") ;;
    dark)  env+=("COLORFGBG=15;0") ;;
  esac
  case "$tier" in
    # The ASCII floor is reached the way a real terminal reaches it: a locale
    # that is not UTF-8 (styles.go's detectASCII may only veto, and there is
    # deliberately no environment pin for it).
    ascii) env+=("LANG=C" "LC_ALL=C") ;;
    plain) env+=("LANG=ja_JP.UTF-8") ;;
    *)     env+=("LANG=en_US.UTF-8") ;;
  esac

  tmux new-session -d -s "$SESSION" -x "$cols" -y 40 \
    "env $(printf '%q ' "${env[@]}") $(printf '%q' "$BIN")"
  sleep 3
  for key in "$@"; do
    tmux send-keys -t "$SESSION" "$key"
    sleep 0.8
  done
  sleep 0.5
  tmux capture-pane -p -t "$SESSION" >"$OUT/$name.txt"
  tmux capture-pane -p -e -t "$SESSION" >"$OUT/$name.ansi"
  tmux kill-session -t "$SESSION" 2>/dev/null
  echo "$name"
}

setup_home

if [ $# -gt 0 ]; then
  capture "$@"
  exit 0
fi

# The matrix the brief asks for: every view the chooser can reach, at the three
# widths, in both themes and both tiers.
for fixture in permission irreversible weighed standing harness-offer design connect connect-key; do
  for cols in 120 96 56; do
    for theme in dark light; do
      for tier in rich ascii; do
        capture "$fixture" "$cols" "$theme" "$tier"
      done
    done
  done
done

# And the shapes that only exist after a key is pressed: the fold, the receipt,
# `something else…` open as a box, and the full page.
capture weighed 120 dark rich Escape
capture weighed 120 dark rich 1
capture weighed 120 dark rich Down Down Down
capture reading 120 dark rich
capture reading 120 dark rich x
capture blanks 120 dark rich
capture checklist 120 dark rich
capture dial 120 dark rich
capture pairs 120 dark rich
capture layout 120 dark rich
