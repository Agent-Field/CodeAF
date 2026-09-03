#!/usr/bin/env bash
# cell.sh runs ONE cell: one issue, one door, one clone, one home, one grade.
#
# It is a process of its own rather than a function so that run.sh can hold
# several cells in flight with nothing more than xargs, and so that a cell that
# dies takes nothing but itself. Everything it learns lands in <out>/cell.json;
# everything it saw lands beside that file.
#
# usage: cell.sh <entry.json> <out-dir>
# env:   CANARY_BIN CANARY_MODEL CANARY_CAP CANARY_WALL CANARY_CACHE
#        CANARY_REGRADE=1  grade an already-run cell again without running it
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# The system interpreter, by path, captured before the venv leads PATH: the
# grade deletes the venv and bash would still hold its hashed python3.
PY="$(command -v python3)"
# shellcheck source=home.sh
source "$HERE/home.sh"
# shellcheck source=chat.sh
source "$HERE/chat.sh"

ENTRY="$1"
OUT="$2"
mkdir -p "$OUT"
WORK="$OUT/work"
HOME_DIR="$OUT/home"
# EVERY CELL HAS ITS OWN TEMP: a base temp shared with strangers is one their
# cleanup can delete from under a grade. This was measured when concurrent
# pytest cleanup left its garbage directory nonempty and the grader exited
# without a summary line.
export TMPDIR="$OUT/tmp"
mkdir -p "$TMPDIR"
PROMPT="$OUT/prompt.txt"
LOAD_START="$(cut -d" " -f1 /proc/loadavg)"

# field reads one string out of the entry; lines reads a list, one per line.
field() { "$PY" -c 'import json,sys; print(json.load(open(sys.argv[1])).get(sys.argv[2], ""))' "$ENTRY" "$1"; }
lines() { "$PY" -c 'import json,sys; print("\n".join(json.load(open(sys.argv[1])).get(sys.argv[2], [])))' "$ENTRY" "$1"; }
gate_rounds_of() { awk '/^ *gate: (fail|refused)/ { rounds++ } END { print rounds + 0 }' "$1"; }
DOOR="$(field door)"
REPO="$(field repo)"
BASE="$(field base)"
MERGE="$(field merge)"
INSTALL="$(field install)"
field prompt > "$PROMPT"
mapfile -t TESTS < <(lines test_files)
BASE_SUITE="$("$PY" -c 'import json,sys; b=json.load(open(sys.argv[1])).get("base_suite"); print(json.dumps(b) if b else "")' "$ENTRY")"
MIRROR="$CANARY_CACHE/repos/${REPO//\//__}.git"

# THE ENVIRONMENT A CELL BUILDS IS THE ONE THE BASE WAS MEASURED IN. The entry
# names the frozen resolution as `lib/constraints/<slug>.txt`; run.sh copies
# lib/ into <run>/rig, so from in here it is beside this script under $HERE.
# An entry that names none is a pool written before the resolution was frozen.
CONSTRAINTS="$(field constraints)"
PIN=""
[ -n "$CONSTRAINTS" ] && [ -f "$HERE/constraints/${CONSTRAINTS##*/}" ] && PIN="$HERE/constraints/${CONSTRAINTS##*/}"
# Whether the suite installed under those pins, as cell.json will report it:
# empty means the entry named no constraints, so there was nothing to honour
# and nothing to report. An entry that names a file this rig does not carry is
# NOT that case — its environment is not the measured one either, and it says
# so exactly as an unconstrained fallback does.
INSTALL_CONSTRAINED=""
[ -n "$CONSTRAINTS" ] && INSTALL_CONSTRAINED=false

# finish writes cell.json and is the only exit. A cell that could not be set up
# is recorded as such, not as a run that produced nothing.
finish() {
  local ended="$1"
  # A regrade measured no run, so it carries no load figure: the box at
  # grading time says nothing about the box the door ran on.
  local load="$LOAD_START $(cut -d" " -f1 /proc/loadavg)"
  [ "${CANARY_REGRADE:-}" = "1" ] && load=""
  CANARY_ENTRY="$ENTRY" CANARY_OUT_DIR="$OUT" CANARY_SETUP="$ended" \
  CANARY_CAP="$CANARY_CAP" CANARY_WALL="$CANARY_WALL" CANARY_LOAD="$load" \
  CANARY_CONSTRAINED="$INSTALL_CONSTRAINED" "$PY" - <<'PYX'
import glob, json, os, re
out = os.environ["CANARY_OUT_DIR"]
def load(name):
    try:
        return json.load(open(os.path.join(out, name)))
    except (OSError, ValueError):
        return {}
entry, door, judge = json.load(open(os.environ["CANARY_ENTRY"])), load("door.json"), load("judge.json")
cap, wall = float(os.environ["CANARY_CAP"]), int(os.environ["CANARY_WALL"])
setup = os.environ["CANARY_SETUP"]
# True, false, or null when the entry named no constraints to install under.
constrained = {"true": True, "false": False}.get(os.environ.get("CANARY_CONSTRAINED", ""))

def chat_transcript_counts():
    """Count loop outcomes when this chat cell has a v3 transcript.

    Older event payloads are Python repr strings; newer ones are JSON objects.
    """
    if entry.get("door") != "chat":
        return None, None, None
    paths = glob.glob(os.path.join(out, "home", "v3", "projects", "*", "*", "transcript.jsonl"))
    if not paths:
        return None, None, None
    mark_fails = carry_ons = 0
    steward_last = None
    with open(paths[0]) as transcript:
        for line in transcript:
            if not line.strip():
                continue
            event = json.loads(line)
            if event.get("type") == "mark":
                payload = event.get("mark")
                decision = payload.get("decision") if isinstance(payload, dict) else None
                if isinstance(payload, str):
                    match = re.search(r"decision': '([\w ]+)'", payload)
                    decision = match.group(1) if match else None
                if decision in {"failed", "no reader"}:
                    mark_fails += 1
            if event.get("type") == "principal":
                payload = event.get("principal")
                if isinstance(payload, dict):
                    who, principal_event, decision = payload.get("who"), payload.get("event"), payload.get("decision")
                else:
                    text = payload if isinstance(payload, str) else ""
                    who = "steward" if re.search(r"who': 'steward'", text) else None
                    principal_event = "decided" if re.search(r"event': 'decided'", text) else None
                    match = re.search(r"decision': '([\w ]+)'", text)
                    decision = match.group(1) if match else None
                if who == "steward" and principal_event == "decided" and decision in {"carry on", "done", "stop"}:
                    steward_last = decision
                    if decision == "carry on":
                        carry_ons += 1
    return mark_fails, carry_ons, steward_last

mark_fails, carry_ons, steward_last = chat_transcript_counts()
f2p = judge.get("f2p") or {}
tests = "tests green" if f2p.get("pass") else ("tests: %d failed, %d errors, %d passed" % (
    f2p.get("failed", 0), f2p.get("errors", 0), f2p.get("passed", 0)) if f2p else "no grade")

# TWO VERDICTS, KEPT APART. How the door ended is one finding and what the
# pull request's tests said about the tree it left is another: a door that
# refuses work the tests call green is a defect of this product, and a single
# pass/fail is exactly where such a defect would hide. `pass` below is their
# conjunction, and stays the thing a regression is measured on.
# THE VERDICT COLUMN READS THE DOOR'S OWN LADDER. `aforge do` ends on five
# exit codes and each one is a different finding, so the rig reads the code
# rather than inferring the finding from the record the door left behind:
#   0  done — it finished the work and said so
#   1  could not run — nothing was attempted
#   2  ran and did not finish — whatever it managed is on the tree
#   3  a limit the person set stopped it: the wall, a budget, the turn cap
#   4  it needed a person, and stopped to ask
# Rung 3 splits in two, because a clock running out and a budget refusing to
# spend are different defects: it reads `wall` when the rig's own wall is what
# fired and `limit` otherwise. `chat` has no such ladder and is read from its
# record exactly as it always was, and so is any code that is not on the ladder
# — which is how timeout(1)'s 124 still reads `wall`.
DO_LADDER = {0: "ok", 1: "could-not-run", 2: "partial", 3: "limit", 4: "asked"}


def door_verdict():
    """One word for how the door ended: for `do`, the rung it exited on; for
    `chat`, the order a reader would ask in — did it stop by itself, did it
    leave a question, did it exit clean."""
    if not door:
        return ""
    ended = door.get("ended", "")
    if door.get("blocked_on"):
        return "asked"
    if ended != "self":
        return ended.replace(" (killed)", "").replace(" ", "")
    if entry.get("door") == "do":
        rung = DO_LADDER.get(door.get("exit"))
        if rung == "limit" and (door.get("wall_s") or 0) >= wall:
            return "wall"
        if rung:
            return rung
    if door.get("exit", 0) != 0:
        return "partial"
    return "ok"

def tests_verdict():
    """One word for what the pull request's own tests said about the tree the
    door left, and whether the rest of the suite survived it."""
    if not f2p:
        return "no grade"
    if not f2p.get("pass"):
        return "red"
    return "regressed" if judge.get("regressed") else "green"

# The first reason that applies is the one recorded, in the order a reader
# would want to hear them: could it run, did it end, did it work, what did it
# cost — and the test grade rides beside a door that ended badly, because a
# door calling correct work "partial" is a different finding from wrong work.
reason = ""
if setup != "ok":
    reason = setup
elif door.get("blocked_on"):
    reason = "asked: " + door["blocked_on"][:120]
elif door.get("ended") != "self":
    reason = "%s · %s" % (door.get("ended", "no door record"), tests)
elif door.get("exit", 0) != 0:
    reason = "exit %s · %s" % (door["exit"], tests)
elif not f2p:
    reason = "no grade"
elif not f2p["pass"]:
    reason = tests
elif judge.get("regressed"):
    reason = "suite regressed: %d failed" % judge["suite"]["failed"]
elif door.get("cost_usd", 0) > cap:
    reason = "over cap $%.2f" % door["cost_usd"]
elif door.get("wall_s", 0) > wall:
    reason = "over wall %ss" % door["wall_s"]

cell = {
    "id": entry["id"], "repo": entry["repo"], "issue": entry["issue"], "anchor": entry.get("anchor", False),
    "door": entry["door"], "pass": reason == "", "reason": reason,
    # The two verdicts, and the door's own facts kept beside them so that a
    # reader can check the word against what was measured.
    "door_verdict": door_verdict(), "tests_verdict": tests_verdict(),
    "ended": door.get("ended"), "exit": door.get("exit"),
    # Where the issue came from and how large a change it asks for, carried
    # from the pool entry: an issue drawn from a published list is one the
    # model may have read before, and a medium tier is a harder ask than a
    # small one. A pool written before these existed means neither.
    "source": entry.get("source") or "fresh", "tier": entry.get("tier") or "small",
    "wall_s": door.get("wall_s"), "cost_usd": door.get("cost_usd"), "ttft_ms": door.get("ttft_ms"),
    "task_done_s": door.get("task_done_s"), "asked_s": door.get("asked_s"),
    "done_to_wall_s": door.get("done_to_wall_s"),
    "mark_fails": mark_fails, "carry_ons": carry_ons, "steward_last": steward_last,
    "calls": door.get("calls"), "changed_files": judge.get("changed_files"), "commits": judge.get("commits"),
    "f2p": judge.get("f2p"), "suite": judge.get("suite"), "regressed": judge.get("regressed"),
    "subharness": door.get("subharness"), "nodes": door.get("nodes"),
    "gate_rounds": door.get("gate_rounds"),
    # Whether the suite was installed under the pool's frozen resolution. A
    # cell that fell back to an unconstrained install ran in an environment
    # nobody measured, and the scoreboard says `unpinned` so a reader is not
    # told a base's counts apply to a base this cell never built.
    "install_constrained": constrained,
    # The box's one-minute load average when the cell began and when it ended:
    # a wall is only comparable to another wall measured under a similar load.
    "load": os.environ["CANARY_LOAD"],
}
json.dump(cell, open(os.path.join(out, "cell.json"), "w"), indent=1)
print("%-5s %-38s %-4s %5ss $%.3f  %s" % ("PASS" if cell["pass"] else "FAIL", cell["id"], cell["door"],
      cell["wall_s"] or 0, cell["cost_usd"] or 0, reason))
PYX
  exit 0
}

# venv installs the suite the door will run, by the rung the pick was
# validated on (pick.py's ladder) and spelled the same way, so a cell builds
# the environment its grade was measured in: an extra, a dependency group,
# every requirements file, or nothing but pytest. Given a constraints file,
# EVERY install in the rung passes it: with the whole graph pinned the
# resolver has nothing left to search, which is what makes pip's
# `resolution-too-deep` impossible on a rung that resolved when it was picked.
venv() {
  local pin="${1-}"
  local -a c=()
  [ -n "$pin" ] && c=(-c "$pin")
  (
    cd "$WORK" || exit 1
    "$PY" -m venv .venv >/dev/null 2>&1 || exit 1
    .venv/bin/pip install -q --upgrade pip >/dev/null 2>&1
    case "$INSTALL" in
      pytest|"")         : ;;
      requirements*.txt) for r in requirements*.txt; do timeout 300 .venv/bin/pip install -q ${c[@]+"${c[@]}"} -r "$r" >/dev/null 2>>"$OUT/pip.err" || exit 1; done ;;
      "--group "*)       timeout 300 .venv/bin/pip install -q ${c[@]+"${c[@]}"} -e . --group "${INSTALL#--group }" >/dev/null 2>"$OUT/pip.err" ;;
      *)                 timeout 300 .venv/bin/pip install -q ${c[@]+"${c[@]}"} -e "$INSTALL" >/dev/null 2>"$OUT/pip.err" ;;
    esac || exit 1
    .venv/bin/pip install -q ${c[@]+"${c[@]}"} pytest >/dev/null 2>&1
  )
}

# install_suite is the only caller of venv(). Under the pool's pins first, and
# if THAT fails ONCE more unconstrained: a cell that ran in a resolution nobody
# measured still says more than a cell that never ran, and the fallback is
# recorded rather than hidden, so the table can tell a reader that this cell's
# environment was not the one the base was measured in.
install_suite() {
  if [ -z "$PIN" ]; then
    venv
    return $?
  fi
  if venv "$PIN"; then
    INSTALL_CONSTRAINED=true
    return 0
  fi
  # Whatever the constrained attempt half-installed must not colour the
  # fallback: it starts from nothing, the same as a first attempt.
  rm -rf "$WORK/.venv"
  INSTALL_CONSTRAINED=false
  venv
}

# grade lays the fix pull request's tests over what the door left and runs
# them; the venv goes afterwards, because it is the biggest thing in the cell
# and says nothing a log does not.
grade() {
  [ -x "$WORK/.venv/bin/python" ] || install_suite || finish "venv: pip install $INSTALL failed"
  "$PY" "$HERE/judge.py" --work "$WORK" --mirror "$MIRROR" --merge "$MERGE" --base "$BASE" --tests "${TESTS[@]}" \
    --base-suite "$BASE_SUITE" --out "$OUT" 2>"$OUT/judge.err"
  rm -rf "$WORK/.venv"
  finish ok
}

# A cell whose door has already spoken can be graded again without being run
# again: the door's record and its working tree are the evidence, and a grader
# that changed, or a grade that never ran, should not cost another model run.
if [ "${CANARY_REGRADE:-}" = "1" ] && [ -f "$OUT/door.json" ] && [ -d "$WORK" ]; then
  if [ -f "$OUT/do.err" ]; then
    # Regrading backfills the count because reading a log the run already wrote is a measurement, not a guess.
    GATE_ROUNDS="$(gate_rounds_of "$OUT/do.err")" "$PY" -c \
      'import json,os,sys; path=sys.argv[1]; door=json.load(open(path)); door["gate_rounds"]=int(os.environ["GATE_ROUNDS"]); json.dump(door, open(path, "w"), indent=1)' \
      "$OUT/door.json"
  fi
  grade
fi

# ── the repository, with the fix kept out of reach ──────────────────────────
#
# One mirror per repository is cloned once and refreshed per cell under a lock.
# The working tree is NOT a clone of it: it is an empty repository that fetches
# exactly the base commit, so the history the door can read stops where the
# issue was open and the merge that fixed it is nowhere in the tree.
mkdir -p "$CANARY_CACHE/repos"
(
  flock 9
  if [ ! -d "$MIRROR" ]; then
    git clone -q --mirror "https://github.com/$REPO" "$MIRROR" || exit 1
    git -C "$MIRROR" config uploadpack.allowAnySHA1InWant true
  fi
  git -C "$MIRROR" fetch -q 2>/dev/null || true
  git -C "$MIRROR" cat-file -e "$MERGE^{commit}" && git -C "$MIRROR" cat-file -e "$BASE^{commit}"
) 9>"$MIRROR.lock" || finish "mirror: cannot reach $REPO at $BASE and $MERGE"

rm -rf "$WORK"
git init -q -b main "$WORK"
git -C "$WORK" fetch -q --depth 1 "$MIRROR" "$BASE" || finish "fetch: $BASE"
git -C "$WORK" reset -q --hard FETCH_HEAD
echo ".venv/" >> "$WORK/.git/info/exclude"

install_suite || finish "venv: pip install $INSTALL failed"
canary_home "$HOME_DIR" "$CANARY_MODEL" "$CANARY_CAP"
export PATH="$WORK/.venv/bin:$PATH"

# ── the door ────────────────────────────────────────────────────────────────
case "$DOOR" in
  do)
    started=$(date +%s)
    AFORGE_HOME="$HOME_DIR" timeout $((CANARY_WALL + 90)) "$CANARY_BIN" do "$(cat "$PROMPT")" \
      -w "$WORK" -json -keep -yes-spend -model "$CANARY_MODEL" -plan-model "$CANARY_MODEL" -timeout "$CANARY_WALL" \
      >"$OUT/do.json" 2>"$OUT/do.err"
    code=$?
    # The kept store is moved beside the cell before anything reads it: left
    # in the system temp directory it is swept before anyone looks, and it is
    # the only place the run's own events — acceptance, gate, growth — live.
    store="$(grep -aoE '^store kept at .*' "$OUT/do.err" | tail -1 | sed -E 's#^store kept at ##')"
    if [ -n "$store" ] && [ -d "$store" ]; then
      mv "$store" "$OUT/store" 2>/dev/null || cp -R "$store" "$OUT/store" 2>/dev/null
    fi
    CANARY_CODE="$code" CANARY_WALL_S="$(( $(date +%s) - started ))" CANARY_OUT_DIR="$OUT" \
    CANARY_GATE_ROUNDS="$(gate_rounds_of "$OUT/do.err")" \
    CANARY_HOME_DIR="$HOME_DIR" CANARY_LIB="$HERE" CANARY_LIMIT="$CANARY_WALL" CANARY_PY="$PY" "$PY" - <<'PYX'
import json, os, subprocess
out, home, lib = os.environ["CANARY_OUT_DIR"], os.environ["CANARY_HOME_DIR"], os.environ["CANARY_LIB"]
code, wall = int(os.environ["CANARY_CODE"]), int(os.environ["CANARY_WALL_S"])
try:
    said = json.load(open(os.path.join(out, "do.json")))
except (OSError, ValueError):
    said = {}
def ask(word):
    return subprocess.run([os.environ["CANARY_PY"], os.path.join(lib, "journal.py"), word, home],
                          capture_output=True, text=True).stdout.strip()
# timeout(1) exits 124 when it had to kill the door; the door's own wall is a
# settled run that says so in seconds. Both are "wall", one is also a crash of
# the door's own stopping.
ended = "self"
if code == 124:
    ended = "wall (killed)"
elif not said:
    ended = "crash" if code != 0 else "no record"
elif said.get("seconds", 0) >= int(os.environ["CANARY_LIMIT"]):
    ended = "wall"
json.dump({
    "door": "do", "ended": ended, "exit": code, "wall_s": wall,
    # The bill is the run's own sum over its private store; the call log is the
    # cross-check and is recorded beside it rather than instead of it.
    "cost_usd": float(said.get("spend") or 0) or float(ask("cost") or 0),
    "calllog_cost_usd": float(ask("cost") or 0),
    "ttft_ms": int(ask("ttft") or 0) or None, "calls": int(ask("calls") or 0),
    "settled": said.get("settled"), "blocked_on": said.get("blocked_on", ""),
    "nodes": said.get("nodes"), "subharness": said.get("subharness"),
    # Each refusal line represents one completed delivery-gate round, so the
    # log remains the source of truth even when the final record is incomplete.
    "gate_rounds": int(os.environ["CANARY_GATE_ROUNDS"]),
}, open(os.path.join(out, "door.json"), "w"), indent=1)
PYX
    ;;
  chat)
    canary_chat "$CANARY_BIN" "$HOME_DIR" "$WORK" "$PROMPT" "$CANARY_MODEL" "$CANARY_CAP" "$CANARY_WALL" "$OUT"
    ;;
  *) finish "unknown door $DOOR" ;;
esac

grade
