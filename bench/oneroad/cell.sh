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
# WAVE 1B'S BINARY, AND IT IS A SNAPSHOT RATHER THAN THE LIVE PATH ON PURPOSE.
# bin/aforge is rebuilt in place by whoever is working the tree, and wave 1's
# batch cells landed two minutes before one such rebuild — which is luck, not a
# protocol. A wave measured against "whatever bin/aforge was at the moment each
# cell happened to launch" is a wave whose arm has no single identity, so the
# bytes are copied once, sha'd into results/BINARIES, and every cell of the arm
# runs that copy.
ESC_BIN="${ESC_BIN:-$ONEROAD/bin/aforge-esc-b70de31c}"
# Wave 1c's binary, snapshotted for the same reason ESC_BIN is.
PRE_BIN="${PRE_BIN:-$ONEROAD/bin/aforge-pre-9b9a4c92}"
# Wave 1d: the raced screen, the ski-rental checkpoints and the ceiling, together.
CKPT_BIN="${CKPT_BIN:-$ONEROAD/bin/aforge-ckpt-6b9809e9}"
# Wave 1e: the finalist. Everything at once.
FINAL_BIN="${FINAL_BIN:-$ONEROAD/bin/aforge-final-c0e4f5a7}"
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
  local started elapsed stable=0 quiet=0 last="" now screen
  started=$(date +%s)
  local sessroot="$profile/v3/projects"
  while :; do
    sleep "$POLL"
    elapsed=$(( $(date +%s) - started ))
    if [ "$elapsed" -ge "$CELL_SECONDS" ]; then
      say "$ARM/$TASK: DNF at ${elapsed}s (wall)"
      tmux capture-pane -t "$SESSION_NAME" -p > "$CELL/tmux-final.txt"
      tmux capture-pane -t "$SESSION_NAME" -p -S -4000 > "$CELL/tmux-scrollback.txt"
      tmux kill-session -t "$SESSION_NAME" 2>/dev/null
      echo DNF > "$CELL/outcome"
      echo wall > "$CELL/settle_reason"
      return 124
    fi
    now="$(find "$sessroot" -type f ! -name presence.json -printf '%s %T@ %p\n' 2>/dev/null | sort | md5sum)"
    screen="$(tmux capture-pane -t "$SESSION_NAME" -p 2>/dev/null)"
    printf '%s\n' "$screen" > "$CELL/tmux-live.txt"
    # THE STORE IS THE SIGNAL AND THE SCREEN IS ONLY A VETO ON "working".
    # `esc interrupt` was in this guard and it cost a cell: the v3 footer can go
    # on drawing that hint long after the turn has ended, so a cell whose store
    # had been silent for seven minutes was still being called busy and would
    # have run to the wall and been recorded DNF. A liveness check must key on
    # something that STOPS when the work stops; the footer's hint does not.
    if [ "$now" = "$last" ]; then
      quiet=$((quiet + POLL))
    else
      quiet=0
    fi
    # THE STORE IS AUTHORITATIVE PAST A HARD MULTIPLE OF THE WINDOW, and this is
    # the second half of a lesson that cost two cells. The footer's hint was
    # wrong in one direction ('esc interrupt' outliving the turn) and the spinner
    # is wrong in the other: a cell whose store had been silent for sixteen
    # minutes was still drawing 'working' and would have run to the wall. A pane
    # is a rendering, not a fact. So the screen may DELAY a settle but it may not
    # prevent one — past 3x the silence window the store's own silence decides.
    # A LIVE TASK VETOES THE SETTLE, AND NO AMOUNT OF SILENCE OVERRIDES IT.
    #
    # This is the rule that was missing, and its absence destroyed six wave-1f
    # cells. The quiet window is evidence about the CONVERSATION and says nothing
    # whatever about a task: a worker that has just typed `pytest` writes nothing
    # anywhere for minutes. The old rule read that silence as "finished", reaped
    # the tmux session, and killed the worker in the middle of its test run —
    # eighteen minutes of real work, nought files landed, judges scoring the
    # emptiness. THE STORE GOING QUIET IS NOT A TASK FINISHING.
    #
    # lib/tasklive.py counts any state it does not recognise as LIVE, so the way
    # this rule fails is by waiting — bounded by the hard wall above — rather
    # than by killing, which is bounded by nothing and loses the work.
    sess="$(find "$CELL/profile/v3/projects" -mindepth 2 -maxdepth 2 -type d 2>/dev/null | head -1)"
    live=0
    if [ -n "$sess" ]; then
      live="$(python3 "$ONEROAD/lib/tasklive.py" "$sess" 2>/dev/null \
              | python3 -c 'import json,sys
try:  print(json.load(sys.stdin)["live"])
except Exception: print(0)' 2>/dev/null)"
      live="${live:-0}"
    fi

    if [ "$now" = "$last" ] && [ "$live" = "0" ] \
       && { ! printf '%s' "$screen" | grep -qE 'working' \
         || [ "$quiet" -ge $((SILENCE_SECONDS * 3)) ]; }; then
      stable=$((stable + POLL))
      if [ "$stable" -ge "$SILENCE_SECONDS" ]; then
        say "$ARM/$TASK: settled at ${elapsed}s (idle, and every task landed)"
        echo idle-and-landed > "$CELL/settle_reason"
        break
      fi
    else
      stable=0
      if [ "$live" != "0" ] && [ $((elapsed % 300)) -lt "$POLL" ]; then
        say "$ARM/$TASK: ${elapsed}s — $live task(s) still running; not settling"
      fi
    fi
    last="$now"
  done
  tmux capture-pane -t "$SESSION_NAME" -p > "$CELL/tmux-final.txt"
  tmux capture-pane -t "$SESSION_NAME" -p -S -4000 > "$CELL/tmux-scrollback.txt"
  # THE SESSION IS REAPED ONCE ITS EVIDENCE IS CAPTURED, and forgetting this cost
  # wave 1 real money in contention: `aforge chat` is an interactive surface that
  # never exits on its own, so every settled cell left a live process and a live
  # tmux session behind it. Fourteen of them were still resident when wave 1b
  # fired, competing for the same CPU as the cells being measured — which makes
  # every wall clock after the first wave quietly worse than the harness it is
  # supposed to be measuring. Capture first, then reap: the panes above are the
  # only thing in the session worth keeping, and the store is on disk already.
  tmux kill-session -t "$SESSION_NAME" 2>/dev/null
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
  # Wave 1b. Same two configurations as the new-* arms, same protocol, different
  # bytes: the question is whether the mid-turn escalation teaching moves the
  # road column, so everything except the binary has to be held still.
  aforge-esc-flash) run_aforge "$ESC_BIN" 1; CODE=$? ;;
  aforge-esc-crew)  run_aforge "$ESC_BIN" 0; CODE=$? ;;
  # Wave 1c. The pre-turn route judge resolves the screen and the confirm
  # through the TIER ROWS, so the all-flash arm runs both readings on flash and
  # the crew arm runs the screen on the shipped low tier and the confirm on the
  # kimi mastermind. That difference is not noise to be controlled away — it is
  # what the two arms are FOR, and it is recorded as part of the arm.
  aforge-pre-flash) run_aforge "$PRE_BIN" 1; CODE=$? ;;
  aforge-pre-crew)  run_aforge "$PRE_BIN" 0; CODE=$? ;;
  aforge-ckpt-flash) run_aforge "$CKPT_BIN" 1; CODE=$? ;;
  aforge-ckpt-crew)  run_aforge "$CKPT_BIN" 0; CODE=$? ;;
  # Wave 1e. crew is the intended default — the mark-reader sidecar and the
  # confirm are mastermind calls and are simply ABSENT on the all-flash pin,
  # which is why that arm is the cost ablation and not a second opinion.
  #
  # NEITHER ARM WRITES A `routing` ROW. The new default is latency under a price
  # ceiling for the turn and price for background calls, and pinning the row
  # would measure the pin instead of the default. The profile writer below has
  # never written that key; this comment is here so nobody adds it.
  aforge-final-crew)  run_aforge "$FINAL_BIN" 0; CODE=$? ;;
  aforge-1f-crew)     run_aforge "$ONEROAD/bin/aforge-1f-0fabf058" 0; CODE=$? ;;
  aforge-1f-flash)    run_aforge "$ONEROAD/bin/aforge-1f-0fabf058" 1; CODE=$? ;;
  aforge-final-flash) run_aforge "$FINAL_BIN" 1; CODE=$? ;;
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
ESCALATED=""; ESC_SEEN=""; ROUTE=""; GRIND=""; PRE_LINE=""; MARKS=""
CALLS=""; COST_CALLS=""; ENDPOINTS=""; MARKS_J=""; FORKS=""; COSTROLE=""; CEIL=""
case "$ARM" in
  aforge-*)
    # THE ESCALATION NOTE, WHICH IS ITS OWN COLUMN AND NOT A READING OF THE ROAD.
    # `taskEscalationNote` (internal/session/task.go) is the one line a person
    # sees when work leaves an answer that had already begun it, and it is looked
    # for BY ITS EXACT BYTES rather than by a paraphrase, because a benchmark
    # that matched loosely would eventually report a near-miss as a hit.
    #
    # It is recorded separately from `road` because the two can disagree in the
    # way that matters most: a handoff that fired and then produced a task which
    # never divided is a DIFFERENT result from a turn that never escalated at
    # all, and a road column reading `task` cannot tell them apart. The screen is
    # searched as well as the store — the note is written for a person, so the
    # pane is where it is guaranteed to appear.
    # The pane is CORROBORATION ONLY and never the answer. The note is a
    # transient hub event that is never written down, and the v3 surface is an
    # alt-screen app with no scrollback, so a frame between two polls is a frame
    # gone for good — "not seen" on the pane is not evidence of "did not happen".
    # road.py derives the real column from the transcript; this only records
    # whether a poll happened to catch the sentence.
    if grep -qF 'this one wants more hands · handing it over with everything found so far' \
         "$CELL"/tmux-*.txt 2>/dev/null; then
      ESC_SEEN="yes"
    else
      ESC_SEEN="no"
    fi
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
                  ("ESCALATED","escalated"),("ROUTE","route"),
                  ("GRIND","chat_tool_calls"),("PRE_LINE","pre_turn_line"),
                  ("MARKS","checkpoint_marks"),("CALLS","calls"),
                  ("COST_CALLS","cost_usd_calls"),("ENDPOINTS","endpoint_mix"),
                  ("MARKS_J","marks"),("FORKS","forks"),("COSTROLE","cost_by_role"),
                  ("CEIL","ceiling_decision"),
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
    ESCALATED="n/a"; ESC_SEEN="n/a"; ROUTE="n/a"; GRIND="n/a"; PRE_LINE=""; MARKS="n/a"
    CALLS="n/a"; COST_CALLS=""; ENDPOINTS="n/a"; MARKS_J="n/a"; FORKS="n/a"; COSTROLE="n/a"; CEIL="n/a"
    COST=""; COST_SRC="not-self-reported"
    ;;
esac

# ── the record ──────────────────────────────────────────────────────────────
OUTCOME="$(cat "$CELL/outcome" 2>/dev/null || echo UNKNOWN)"
SETTLE_REASON="$(cat "$CELL/settle_reason" 2>/dev/null || echo n/a)"
ARM="$ARM" TASK="$TASK" SEED="$SEED" WALL="$WALL" CODE="$CODE" CHANGED="$CHANGED" \
BEFORE="$BEFORE" AFTER="$AFTER" ROAD="$ROAD" ARMED="$ARMED" PARTS="$PARTS" \
PEAK="$PEAK" REFUSED="$REFUSED" ESCALATED="$ESCALATED" ESC_SEEN="$ESC_SEEN" ROUTE="$ROUTE" GRIND="$GRIND" \
PRE_LINE="$PRE_LINE" MARKS="$MARKS" CALLS="$CALLS" COST_CALLS="$COST_CALLS" \
ENDPOINTS="$ENDPOINTS" MARKS_J="$MARKS_J" FORKS="$FORKS" COSTROLE="$COSTROLE" \
CEIL="$CEIL" COST="$COST" COST_SRC="$COST_SRC" MODEL="$MODEL" \
OUTCOME="$OUTCOME" SETTLE_REASON="$SETTLE_REASON" COMMIT="$COMMIT" CELL="$CELL" python3 - <<'PY'
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
    "outcome": env["OUTCOME"], "settle_reason": env.get("SETTLE_REASON", "n/a"),
    "exit": int(env["CODE"] or 0),
    "wall_s": int(env["WALL"]),
    "cost_usd": env["COST"], "cost_source": env["COST_SRC"],
    "tests_before": env["BEFORE"], "tests_after": env["AFTER"],
    "passed_before": pb, "failed_before": fb,
    "passed_after": pa, "failed_after": fa,
    "changed_files": int(env["CHANGED"] or 0),
    "road": env["ROAD"], "armed": env["ARMED"],
    "parts": int(env["PARTS"] or 0), "peak_workers": env["PEAK"],
    "refused": env["REFUSED"], "escalated": env["ESCALATED"],
    "escalation_seen_on_pane": env["ESC_SEEN"],
    "route": env["ROUTE"], "chat_tool_calls": env["GRIND"],
    "pre_turn_line": env["PRE_LINE"], "checkpoint_marks": env["MARKS"],
    "calls": env["CALLS"], "cost_usd_calls": env["COST_CALLS"],
    "endpoint_mix": env["ENDPOINTS"], "marks": env["MARKS_J"],
    "forks": env["FORKS"], "cost_by_role": env["COSTROLE"],
    "ceiling_decision": env["CEIL"],
    "loadavg_before": open(os.path.join(env["CELL"], "loadavg-before")).read().strip(),
    "loadavg_after": open(os.path.join(env["CELL"], "loadavg-after")).read().strip(),
}
json.dump(meta, open(os.path.join(env["CELL"], "meta.json"), "w"), indent=2)
print(json.dumps(meta))
PY

# ── the competitor's own bill ───────────────────────────────────────────────
#
# pi and opencode print no cost, but both KEEP one — pi in its session jsonl,
# opencode in its sqlite — and lib/competitor_cost.py finds this cell's sessions
# by the cell's own work directory and prices the tokens at OpenRouter list. That
# is the comparable figure: the same arithmetic every arm's cost is now quoted
# under, rather than a self-report from one harness and a blank from the others.
#
# IT RUNS AFTER meta.json IS WRITTEN, because meta.json is what it updates, and
# BEFORE the judge bundle, which reads meta.json.
#
# IT MAY NOT FAIL THE CELL. The script also rewrites results.csv, and two things
# make that unsafe to depend on here: the file does not exist yet for the first
# cell of a grid, and cells run concurrently, so two of them rewriting it at once
# could drop a row. Neither matters — meta.json is the durable record and
# collect.py rebuilds results.csv from every meta.json at the end of the grid, so
# a lost or missing CSV row is repaired by the next collect. The `|| true` is
# therefore not papering over a failure; it is saying that this cell's evidence
# does not depend on the shared file.
case "$ARM" in
  pi|opencode)
    python3 "$ONEROAD/lib/competitor_cost.py" "$ONEROAD/results" "$(basename "$CELL")" \
      >> "$CELL/cell.log" 2>&1 || say "$ARM/$TASK: competitor cost unreadable (meta.json keeps its blank)"
    ;;
esac

# ── the judge's input bundle ────────────────────────────────────────────────
python3 "$ONEROAD/lib/judge_bundle.py" "$CELL"
say "$ARM/$TASK: done — $CELL"
