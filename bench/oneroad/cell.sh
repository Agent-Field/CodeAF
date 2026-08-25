#!/usr/bin/env bash
# cell.sh — ONE cell of the one-road benchmark: one arm, one task, one clone.
#
# Usage: cell.sh <arm> <task>
#   arm   aforge-new-flash | aforge-new-crew | aforge-old-flash | pi | opencode
#   task  20 | 21 | 22 | 23 | batch
#
# THE PROTOCOL POINT (bench/oneroad/README.md): the aforge arms are driven
# through the REAL TUI over tmux. The issue text is pasted into the composer and
# Enter is pressed, exactly as a person would, and what the chat surface decides
# to do with it IS the measurement. `aforge do` and `chat --once` are headless
# doors that are handed the shape instead of choosing it, and using either here
# would answer a different question.
#
# pi and opencode get the same text through their own front door, which is their
# one-shot print mode. They have no task concept and no road to choose, which is
# exactly why they are the reference point.
set -uo pipefail

ARM="${1:?usage: cell.sh <arm> <task>}"
TASK="${2:?usage: cell.sh <arm> <task>}"
SEED="${SEED:-s1}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ONEROAD="$ROOT"
source "$ONEROAD/lib/corpus.sh"

ISSUES="${ISSUES:-20 21 22 23}"
MODEL="${MODEL:-deepseek/deepseek-v4-flash}"
NEW_BIN="${NEW_BIN:-/home/santosh/af-oneroad/bin/aforge}"
OLD_BIN="${OLD_BIN:-/home/santosh/af-oldbase/bin/aforge}"
PI_BIN="${PI_BIN:-pi}"
OPENCODE_BIN="${OPENCODE_BIN:-$HOME/.opencode/bin/opencode}"

# The wall. bench/run.sh's forty minutes, in seconds, because a cell past it is
# recorded as DNF rather than left to spend.
CELL_SECONDS="${CELL_SECONDS:-2400}"
# How long the surface must be completely silent before the turn is called
# settled. A task thinking between tool calls is silent for tens of seconds, so
# the window has to be longer than a model call and shorter than the wall.
SILENCE_SECONDS="${SILENCE_SECONDS:-180}"
POLL="${POLL:-10}"

CELL="$ONEROAD/results/$ARM-$TASK-$SEED"
rm -rf "$CELL"; mkdir -p "$CELL"
WORK="$CELL/work"; mkdir -p "$WORK"
DIR="$WORK/$SLUG"
SESSION_NAME="oneroad-$ARM-$TASK-$SEED"

say() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" | tee -a "$CELL/cell.log"; }

# ── the prompt ──────────────────────────────────────────────────────────────
say "$ARM/$TASK: fetching issue text"
if [ "$TASK" = "batch" ]; then
  PROMPT="$(batch_prompt)" || { say "could not read the issues"; exit 1; }
else
  PROMPT="$(issue_prompt "$TASK")" || { say "could not read issue #$TASK"; exit 1; }
fi
printf '%s' "$PROMPT" > "$CELL/prompt.txt"

# ── the clone, and the suite before anybody touched it ──────────────────────
say "$ARM/$TASK: cloning at $BASE_COMMIT"
COMMIT="$(fresh_clone "$DIR")" || { say "clone failed"; exit 1; }
echo "$COMMIT" > "$CELL/base-commit"
setup_python "$DIR"
BEFORE="$(run_suite "$DIR" "$CELL/pytest-before.log")"
say "$ARM/$TASK: suite before = $BEFORE (passed/failed)"

# The load this cell started under. Arms share one machine by design (the
# same-wave rule is what keeps provider weather fair), so the contention has to
# be on the record rather than argued about afterwards.
cut -d' ' -f1-3 /proc/loadavg > "$CELL/loadavg-before"

# ── the aforge arms: the real TUI, over tmux ────────────────────────────────
run_aforge() {
  local binary="$1" all_flash="$2"
  local profile="$CELL/profile" cellhome="$CELL/home"
  mkdir -p "$profile" "$cellhome"

  # The disposable brain. Same shape test/ux/run.sh builds: HOME moves as well as
  # the profile, so anything that does not yet honour the profile override lands
  # here rather than in the user's actual home, and two cells cannot contaminate
  # each other through one store.
  #
  # setup_seen_at is written BEFORE the first launch on purpose. The v3 chat
  # opens a two-step first-run setup on a profile with nothing in it
  # (internal/config/firstrun.go), and a benchmark that answered it with
  # keystrokes would be racing a wizard for the composer. The marker says the
  # setup was met; the rows it would have asked about are written here directly,
  # which is the same end state and none of the race.
  ALL_FLASH="$all_flash" MODEL="$MODEL" PROFILE="$profile" python3 - <<'PY'
import datetime, json, os
profile = os.environ["PROFILE"]
config = {
    "api_key": os.environ["OPENROUTER_API_KEY"],
    "setup_seen_at": datetime.datetime.now(datetime.timezone.utc)
                     .strftime("%Y-%m-%dT%H:%M:%S.%fZ"),
}
# THE ONE DIFFERENCE BETWEEN THE TWO NEW ARMS. all-flash pins every tier at the
# cheap model, so the crew row reads `custom` and nothing on the machine reaches
# for a thinking model. The crew arm writes NO tier row at all, which is not the
# same as writing the balanced values: an unset row is the shipped default, and
# a benchmark that copied the defaults in would stop measuring them the day they
# moved (internal/config/crew.go: the preset is derived, never stored).
if os.environ["ALL_FLASH"] == "1":
    for tier in ("reflex", "low", "high", "mastermind"):
        config["models.tiers." + tier] = os.environ["MODEL"]
json.dump(config, open(os.path.join(profile, "config.json"), "w"), indent=2)
os.chmod(os.path.join(profile, "config.json"), 0o600)
PY

  tmux kill-session -t "$SESSION_NAME" 2>/dev/null
  # --yolo is the unattended posture said out loud rather than defaulted: there
  # is nobody at this terminal to approve a tool call, and it is the same posture
  # `pi -p` and `opencode run --auto` take on the other arms.
  # --model pins the conversation's own model; the tier rows above (or their
  # absence) decide everything the machine calls on its own behalf.
  tmux new-session -d -s "$SESSION_NAME" -x 200 -y 50 \
    "cd '$DIR' && env HOME='$cellhome' AFORGE_HOME='$profile' AFORGE_PROFILE_DIR='$profile' \
      OPENROUTER_API_KEY='$OPENROUTER_API_KEY' TERM=xterm-256color \
      '$binary' chat --yolo --model '$MODEL'; echo AFORGE-EXITED; sleep 60"

  # The first frame. A surface that never drew one has nothing to type into, and
  # the pane is kept so the reason is legible rather than inferred.
  local waited=0 drew=""
  while [ "$waited" -lt 60 ]; do
    sleep 3; waited=$((waited + 3))
    if tmux capture-pane -t "$SESSION_NAME" -p 2>/dev/null | grep -q 'AFORGE-EXITED'; then
      tmux capture-pane -t "$SESSION_NAME" -p > "$CELL/tmux-firstframe.txt"
      say "aforge exited instead of drawing a frame"; return 90
    fi
    if tmux capture-pane -t "$SESSION_NAME" -p 2>/dev/null | grep -Eq '›|try "what is in this folder"'; then
      drew=yes; break
    fi
  done
  tmux capture-pane -t "$SESSION_NAME" -p > "$CELL/tmux-firstframe.txt"
  [ -n "$drew" ] || { say "no first frame within 60s"; return 91; }

  # THE MESSAGE, TYPED AS A PERSON TYPES IT. A whole issue is many lines, and
  # send-keys would submit at the first newline; a bracketed paste arrives as one
  # tea.PasteMsg with its newlines intact (internal/tui3/app.go), which is what a
  # person's terminal does when they paste. Enter is a separate keystroke
  # afterwards, exactly as it is for them.
  tmux load-buffer -b "$SESSION_NAME" "$CELL/prompt.txt"
  tmux paste-buffer -p -b "$SESSION_NAME" -t "$SESSION_NAME"
  sleep 3
  tmux capture-pane -t "$SESSION_NAME" -p > "$CELL/tmux-composed.txt"
  tmux send-keys -t "$SESSION_NAME" Enter
  say "$ARM/$TASK: sent (${#PROMPT} chars), waiting up to ${CELL_SECONDS}s"

  # ── settlement ────────────────────────────────────────────────────────────
  #
  # Silence is the signal, and it is measured on the STORE rather than on the
  # screen: a task working in a worktree draws nothing at all for minutes while
  # writing its journal the whole time. The fingerprint is every byte the session
  # folder holds, so a node journal growing counts as alive even when the
  # conversation is idle. The screen is consulted only as a second opinion —
  # "working" on the footer overrides silence.
  #
  # presence.json IS EXCLUDED, AND LEAVING IT IN COST THIS BENCHMARK ITS FIRST
  # NINE CELLS. It is a heartbeat: the session rewrites it every few seconds for
  # as long as the process is alive, whether or not anything is happening. A
  # fingerprint that includes it therefore NEVER stabilises, and nine cells that
  # had finished their work sat in this loop until the wall would have recorded
  # them DNF. The rule the mistake teaches is general — a settle detector must
  # fingerprint only what WORK writes, never what mere liveness writes.
  local started elapsed stable=0 last="" now screen
  started=$(date +%s)
  local sessroot="$profile/v3/projects"
  while :; do
    sleep "$POLL"
    elapsed=$(( $(date +%s) - started ))
    if [ "$elapsed" -ge "$CELL_SECONDS" ]; then
      say "$ARM/$TASK: DNF at ${elapsed}s"
      tmux capture-pane -t "$SESSION_NAME" -p > "$CELL/tmux-final.txt"
      echo DNF > "$CELL/outcome"
      return 124
    fi
    now="$(find "$sessroot" -type f ! -name presence.json -printf '%s %T@ %p\n' 2>/dev/null | sort | md5sum)"
    screen="$(tmux capture-pane -t "$SESSION_NAME" -p 2>/dev/null)"
    printf '%s\n' "$screen" > "$CELL/tmux-live.txt"
    if [ "$now" = "$last" ] && ! printf '%s' "$screen" | grep -qE 'working|esc interrupt'; then
      stable=$((stable + POLL))
      if [ "$stable" -ge "$SILENCE_SECONDS" ]; then
        say "$ARM/$TASK: settled at ${elapsed}s (${SILENCE_SECONDS}s silent)"
        break
      fi
    else
      stable=0
    fi
    last="$now"
  done
  tmux capture-pane -t "$SESSION_NAME" -p > "$CELL/tmux-final.txt"
  tmux capture-pane -t "$SESSION_NAME" -p -S -4000 > "$CELL/tmux-scrollback.txt"
  echo OK > "$CELL/outcome"
  return 0
}

# ── the peer arms: their own front door ─────────────────────────────────────
run_peer() {
  case "$ARM" in
    pi)
      # bench/run.sh's recorded invocation, checked against pi 0.84.1: -p is
      # still non-interactive print mode and --provider/--model still pin.
      (cd "$DIR" && timeout "$CELL_SECONDS" "$PI_BIN" -p \
        --provider openrouter --model "$MODEL" "$PROMPT")
      ;;
    opencode)
      # bench/run.sh's pattern with ONE addition, and it is recorded rather than
      # quiet: opencode 1.18.22 defaults `--auto` to false, so `run` without it
      # stops at the first permission prompt with nobody there to answer and the
      # cell measures a dialog rather than a harness. --auto is this arm's
      # equivalent of aforge's --yolo and pi's -p.
      (cd "$DIR" && timeout "$CELL_SECONDS" "$OPENCODE_BIN" run --auto \
        -m "openrouter/$MODEL" "$PROMPT")
      ;;
  esac
}

# ── the run ─────────────────────────────────────────────────────────────────
STARTED=$(date +%s)
case "$ARM" in
  aforge-new-flash) run_aforge "$NEW_BIN" 1; CODE=$? ;;
  aforge-new-crew)  run_aforge "$NEW_BIN" 0; CODE=$? ;;
  aforge-old-flash) run_aforge "$OLD_BIN" 1; CODE=$? ;;
  pi|opencode)      run_peer >"$CELL/harness.log" 2>&1; CODE=$?
                    [ "$CODE" = "124" ] && echo DNF > "$CELL/outcome" || echo OK > "$CELL/outcome" ;;
  *) say "unknown arm $ARM"; exit 2 ;;
esac
WALL=$(( $(date +%s) - STARTED ))
cut -d' ' -f1-3 /proc/loadavg > "$CELL/loadavg-after"
say "$ARM/$TASK: harness finished, exit $CODE, ${WALL}s"

# ── what it actually did ────────────────────────────────────────────────────
CHANGED="$(changed_files "$DIR")"
(cd "$DIR" && git diff) > "$CELL/diff.patch" 2>/dev/null
(cd "$DIR" && git status --porcelain | grep -vE '\.venv|__pycache__') > "$CELL/status.txt" 2>/dev/null
# An untracked file is part of the work and `git diff` never shows one, so the
# patch is completed by hand rather than left silently short.
(cd "$DIR" && git status --porcelain | grep -E '^\?\?' | grep -vE '\.venv|__pycache__' | \
  sed 's/^?? //' | while read -r new; do
    printf '\n--- /dev/null\n+++ b/%s\n' "$new"
    sed 's/^/+/' "$new" 2>/dev/null
  done) >> "$CELL/diff.patch" 2>/dev/null
AFTER="$(run_suite "$DIR" "$CELL/pytest-after.log")"
say "$ARM/$TASK: suite after = $AFTER, $CHANGED changed file(s)"

# ── the road columns, and the parallelism timeline ──────────────────────────
ROAD=""; ARMED=""; PARTS=0; PEAK=""; REFUSED=""; COST=""; COST_SRC=""
case "$ARM" in
  aforge-*)
    # The store is COPIED beside the evidence rather than moved: the process may
    # still be holding it, and a reader that took it away would be reading a
    # database somebody else still has open. The copy is the benchmark's own and
    # is what every number below is taken from — and what a later autopsy opens.
    cp -R "$CELL/profile" "$CELL/store" 2>/dev/null
    SESSION_DIR="$(find "$CELL/store/v3/projects" -mindepth 2 -maxdepth 2 -type d 2>/dev/null | head -1)"
    if [ -n "$SESSION_DIR" ]; then
      COLUMNS="$(python3 "$ONEROAD/lib/road.py" "$SESSION_DIR" --timeline "$CELL/timeline.json" 2>"$CELL/road.err")"
      if [ -n "$COLUMNS" ]; then
        eval "$(COLUMNS="$COLUMNS" python3 -c '
import json, os, shlex
c = json.loads(os.environ["COLUMNS"])
for name, key in (("ROAD","road"),("ARMED","armed"),("PARTS","parts"),
                  ("PEAK","peak_workers"),("REFUSED","refused"),
                  ("COST","cost_usd"),("COST_SRC","cost_source")):
    print("%s=%s" % (name, shlex.quote(str(c.get(key, "")))))')"
      fi
    else
      say "no v3 session folder under the profile — road columns unreadable"
    fi
    ;;
  *)
    # bench/README.md's rule: only aforge self-reports usage, and an account-level
    # delta on a shared key is not a substitute. pi and opencode print no cost, so
    # the column is empty and the SOURCE says why rather than a zero that reads
    # like a free run.
    ROAD="n/a"; ARMED="n/a"; PARTS=0; PEAK="n/a"; REFUSED="n/a"
    COST=""; COST_SRC="not-self-reported"
    ;;
esac

# ── the record ──────────────────────────────────────────────────────────────
OUTCOME="$(cat "$CELL/outcome" 2>/dev/null || echo UNKNOWN)"
ARM="$ARM" TASK="$TASK" SEED="$SEED" WALL="$WALL" CODE="$CODE" CHANGED="$CHANGED" \
BEFORE="$BEFORE" AFTER="$AFTER" ROAD="$ROAD" ARMED="$ARMED" PARTS="$PARTS" \
PEAK="$PEAK" REFUSED="$REFUSED" COST="$COST" COST_SRC="$COST_SRC" MODEL="$MODEL" \
OUTCOME="$OUTCOME" COMMIT="$COMMIT" CELL="$CELL" python3 - <<'PY'
import json, os
env = os.environ
def load(name):
    try:
        return [int(x) for x in env[name].split("/")]
    except Exception:
        return [None, None]
pb, fb = load("BEFORE")
pa, fa = load("AFTER")
meta = {
    "task": env["TASK"], "harness": env["ARM"], "seed": env["SEED"],
    "model": env["MODEL"], "base_commit": env["COMMIT"],
    "outcome": env["OUTCOME"], "exit": int(env["CODE"] or 0),
    "wall_s": int(env["WALL"]),
    "cost_usd": env["COST"], "cost_source": env["COST_SRC"],
    "tests_before": env["BEFORE"], "tests_after": env["AFTER"],
    "passed_before": pb, "failed_before": fb,
    "passed_after": pa, "failed_after": fa,
    "changed_files": int(env["CHANGED"] or 0),
    "road": env["ROAD"], "armed": env["ARMED"],
    "parts": int(env["PARTS"] or 0), "peak_workers": env["PEAK"],
    "refused": env["REFUSED"],
    "loadavg_before": open(os.path.join(env["CELL"], "loadavg-before")).read().strip(),
    "loadavg_after": open(os.path.join(env["CELL"], "loadavg-after")).read().strip(),
}
json.dump(meta, open(os.path.join(env["CELL"], "meta.json"), "w"), indent=2)
print(json.dumps(meta))
PY

# ── the judge's input bundle ────────────────────────────────────────────────
python3 "$ONEROAD/lib/judge_bundle.py" "$CELL"
say "$ARM/$TASK: done — $CELL"
