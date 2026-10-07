#!/usr/bin/env bash
# Capture the update offer's deterministic fixtures AND the real settings row
# from THIS worktree's binary.
#
#   scripts/update-drive.sh                       # the whole matrix
#   scripts/update-drive.sh offer 80              # one offer fixture at one width
#   scripts/update-drive.sh settings-off 120      # the real /settings page, row off
#
# The offer fixtures raise the REAL state machine with every updater hook
# disabled (internal/tui3/updatedemo.go), so no capture can make a request or
# replace a file. The settings captures are the OTHER kind and are labelled as
# such: they open the REAL settings page through /settings and type into its
# search box, so the row drawn is the registry's own row reading the profile's
# own value \u2014 there is no fixture state in them at all.
#
# This drive adds to both: a home and workspace made fresh under mktemp, a
# private tmux server, an EMPTY inherited environment, `chat --no-host` so no
# session host is left behind, CODEAF_NO_UPDATE_CHECK=1 so the launch check makes
# no request, and CODEAF_BASE_URL pointed at a loopback address so ANY provider
# or catalog call a surface might start lands on this machine and nowhere else.
# No real key, profile or engine is read or written and nothing outside the
# temporary directory is touched.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${UPDATE_BIN:-$ROOT/bin/codeaf}"
OUT="${UPDATE_OUT:-$ROOT/docs/design/background-updates-frames}"
CASES=(offer downloading ready ready-working manual off failure)
WIDTHS=(80 120)

[ -x "$BIN" ] || { echo "no binary at $BIN \u2014 run make build" >&2; exit 1; }
command -v tmux >/dev/null || { echo "tmux is required for a screen drive" >&2; exit 1; }

TMP_PREFIX="${TMPDIR:-/tmp}/codeaf-update-drive."
TMP="$(mktemp -d "$TMP_PREFIX"XXXXXX)"
SOCK="codeaf-update-$$"
# CLEANUP REMOVES ONLY WHAT THIS SCRIPT MADE. The home, the workspace and the
# profile all live under $TMP, which mktemp just created under that prefix; a
# user-supplied UPDATE_OUT is written into and never deleted.
cleanup() {
  tmux -L "$SOCK" kill-server 2>/dev/null || true
  case "$TMP" in
    "$TMP_PREFIX"*) rm -rf "$TMP" ;;
    *) echo "refusing to remove $TMP" >&2 ;;
  esac
}
trap cleanup EXIT

WORK="$TMP/work/project"
mkdir -p "$WORK" "$OUT"

# profile <dir> <auto> — write the demo profile. `auto` is left UNSET for the
# default-on capture (so the page shows the default, not a value this script
# wrote) and is the JSON word false for the off one. Every model tier the
# surface can resolve is named exactly, so no catalog lookup is ever needed.
profile() {
  local dir="$1" auto="${2:-}" tail='  "ui.mouse": "on"'
  mkdir -p "$dir"
  if [ -n "$auto" ]; then
    tail="  \"ui.mouse\": \"on\",
  \"update.auto\": $auto"
  fi
  cat >"$dir/config.json" <<JSON
{
  "setup_seen_at": "2026-10-07T00:00:00Z",
  "api_key": "demo-key-not-used",
  "model.talk": "deepseek/deepseek-v4.1-flash",
  "models.tiers.high": "deepseek/deepseek-v4.1-flash",
  "models.tiers.low": "deepseek/deepseek-v4.1-flash",
  "models.tiers.worker": "deepseek/deepseek-v4.1-flash",
  "models.tiers.reflex": "deepseek/deepseek-v4.1-flash",
  "models.tiers.mastermind": "deepseek/deepseek-v4.1-flash",
$tail
}
JSON
}

# launch <session> <cols> <home> <demo-case|-> \u2014 start the surface in a private
# tmux server with an empty environment.
launch() {
  local session="$1" cols="$2" home_dir="$3" demo="$4" demo_env=""
  [ "$demo" != "-" ] && demo_env="CODEAF_UPDATE_DEMO=$(printf '%q' "$demo")"
  tmux -L "$SOCK" kill-session -t "$session" 2>/dev/null || true
  tmux -L "$SOCK" new-session -d -s "$session" -x "$cols" -y 40 \
    "cd $(printf '%q' "$WORK") && env -i \
      HOME=$(printf '%q' "$home_dir") \
      CODEAF_HOME=$(printf '%q' "$home_dir") \
      CODEAF_PROFILE_DIR=$(printf '%q' "$home_dir") \
      CODEAF_NO_UPDATE_CHECK=1 \
      CODEAF_BASE_URL=http://127.0.0.1:9 \
      TERM=xterm-256color \
      LANG=C.UTF-8 \
      PATH=/usr/bin:/bin \
      $demo_env \
      $(printf '%q' "$BIN") chat --no-host"
  sleep 3
}

# grab <session> <file> \u2014 keep both the plain text and the painted capture.
grab() {
  local session="$1" file="$2"
  tmux -L "$SOCK" capture-pane -p -t "$session" >"$file"
  tmux -L "$SOCK" capture-pane -p -e -t "$session" >"${file%.txt}.ansi"
  tmux -L "$SOCK" kill-session -t "$session" 2>/dev/null || true
}

# capture <case> <cols> \u2014 one offer fixture at one width.
capture() {
  local case="$1" cols="$2"
  local session="update-$case-$cols" file="$OUT/$case-${cols}c.txt" home_dir="$TMP/home/$case"
  profile "$home_dir"
  launch "$session" "$cols" "$home_dir" "$case"
  grab "$session" "$file"
  # A CAPTURE THAT DID NOT DRAW THE FIXTURE IS A FAILURE, not a file. Every
  # fixture writes its own name last, so this checks the real surface ran.
  grep -q "CODEAF_UPDATE_DEMO=$case" "$file" || {
    echo "capture $case at ${cols}c did not draw the fixture:" >&2
    cat "$file" >&2
    exit 1
  }
  echo "$file"
}

# capture_settings <state> <cols> \u2014 the REAL /settings page with the real
# `auto update` row, reached the way a person reaches it: the command, the
# arrow keys that walk the tabs to Display, and the down key that walks the
# cursor onto the row. The row is confirmed selected by its OWN hint being on
# screen, not by the label merely existing somewhere in the frame.
capture_settings() {
  local state="$1" cols="$2"
  local session="settings-$state-$cols" file="$OUT/settings-auto-$state-${cols}c.txt"
  local home_dir="$TMP/home/settings-$state" auto=""
  [ "$state" = "off" ] && auto="false"
  profile "$home_dir" "$auto"
  launch "$session" "$cols" "$home_dir" -
  tmux -L "$SOCK" send-keys -t "$session" -l "/settings"
  tmux -L "$SOCK" send-keys -t "$session" Enter
  sleep 2
  # THE DISPLAY TAB, BY THE PANEL'S OWN ARROW KEY: Session, Context, Workspace,
  # Display. The panel's type-to-search is deliberately NOT used here — it ranks
  # fuzzy matches from every tab, so the row under the cursor is usually the
  # wrong one and the sentence drawn under it explains somebody else's setting.
  tmux -L "$SOCK" send-keys -t "$session" Right
  tmux -L "$SOCK" send-keys -t "$session" Right
  tmux -L "$SOCK" send-keys -t "$session" Right
  sleep 2
  local landed=""
  for _ in $(seq 1 24); do
    # THE ROW'S OWN HINT IS THE PROOF THE CURSOR IS ON IT: the sheet draws the
    # hint only for the row under the cursor, and this row's hint opens with
    # these words.
    if tmux -L "$SOCK" capture-pane -p -t "$session" | grep -q "check for a new codeaf at launch"; then
      landed=yes
      break
    fi
    tmux -L "$SOCK" send-keys -t "$session" Down
    sleep 0.4
  done
  if [ -z "$landed" ]; then
    echo "the settings capture at ${cols}c never selected the auto update row:" >&2
    tmux -L "$SOCK" capture-pane -p -t "$session" >&2
    tmux -L "$SOCK" kill-session -t "$session" 2>/dev/null || true
    exit 1
  fi
  grab "$session" "$file"
  # THE ROW ITSELF IS THE CHECK, not the fact that something drew: the registry's
  # label, the sentence the registry wrote for it, and the value its reader
  # resolved. A capture without all three is a failure.
  grep -q "check for a new codeaf at launch" "$file" || {
    echo "the settings capture at ${cols}c did not draw the row's own hint:" >&2
    cat "$file" >&2
    exit 1
  }
  grep -q "auto update" "$file" || {
    echo "the settings capture at ${cols}c did not draw the auto update row:" >&2
    cat "$file" >&2
    exit 1
  }
  grep -q "auto update.*$state" "$file" || {
    echo "the settings capture at ${cols}c did not read $state:" >&2
    cat "$file" >&2
    exit 1
  }
  echo "$file"
}

case "${1:-}" in
  --all) shift
    for case in "${CASES[@]}"; do for cols in "${WIDTHS[@]}"; do capture "$case" "$cols"; done; done
    for state in on off; do for cols in "${WIDTHS[@]}"; do capture_settings "$state" "$cols"; done; done
    ;;
  "") for case in "${CASES[@]}"; do for cols in "${WIDTHS[@]}"; do capture "$case" "$cols"; done; done
      for state in on off; do for cols in "${WIDTHS[@]}"; do capture_settings "$state" "$cols"; done; done
      ;;
  settings) for state in on off; do for cols in "${WIDTHS[@]}"; do capture_settings "$state" "$cols"; done; done
    ;;
  settings-on|settings-off) capture_settings "${1#settings-}" "${2:-80}"
    ;;
  *) capture "$1" "${2:-80}"
    ;;
esac
