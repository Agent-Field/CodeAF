#!/usr/bin/env bash
# marathon/rescore.sh — an honest, out-of-band score of a LIVE cell's workspace.
#
# Usage: rescore.sh <seed> <n>            e.g.  rescore.sh s6 3
#        ARM=aforge-crew TASK=rust-java-lsp rescore.sh s6 3
#
# WHAT IT IS FOR. The hourly curve is written by snapshot.sh from inside the
# cell's own loop. When a cell is already running — or when its curve was taken
# with the wrong compiler, which is what happened to the seeds launched before
# the toolchain fix — a reading has to be taken from outside it. `<n>` is just
# the sequence number of the reading: it names the output directory and labels
# the row, so a series of them reads as a curve.
#
# THE TWO THINGS THAT MAKE IT HONEST.
#
# 1. THE DROPPINGS ARE STRIPPED. The workspace is copied WITHOUT target/ (build
#    output the verifier rebuilds anyway, and gigabytes of it), .git, and
#    .aforge-v3 — the harness's own store, which the agent happens to have
#    written inside the working directory and which is no part of the work being
#    judged. Everything else travels, including the corpus and any symlink the
#    agent arranged: the scorer addresses test points as
#    file:///workspace/test-files/<rel>, so an arrangement one level above the
#    crate is part of the answer.
#
# 2. THE AGENT'S COMPILER IS ADOPTED. The agent may legally have upgraded its
#    toolchain, and the real verifier — running in the agent's own container —
#    inherits that upgrade. A rescore that used the image's 1.86.0 would report
#    0.0 for a workspace the benchmark scores properly. See toolchain.sh.
#
# It scores in a SHORT-LIVED CONTAINER OF ITS OWN, on two CPUs, so it never
# takes the running agent's four. It never touches the agent's container beyond
# reading it, and it can be run while the cell is live.
set -uo pipefail

SEED="${1:?usage: rescore.sh <seed> <n>}"
N="${2:?usage: rescore.sh <seed> <n>}"
ARM="${ARM:-aforge-crew}"
TASK="${TASK:-rust-java-lsp}"
IMAGE="${IMAGE:-swe-marathon/$TASK:v1.1}"
CAP="${RESCORE_TIMEOUT:-1800}"
CPUS="${RESCORE_CPUS:-2}"
MEM="${RESCORE_MEM:-8g}"

MAR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MARATHON_REPO="${MARATHON_REPO:-/tmp/claude-1001/-home-santosh/36adab24-74be-4ae9-946a-ab932634b759/scratchpad/marathon/repo}"
TASKDIR="$MARATHON_REPO/tasks/$TASK"
MAR_OUT="${MAR_OUT:-/home/santosh/af-bench/marathon}"
CELL="$MAR_OUT/$ARM-$TASK-$SEED"
CONTAINER="${CONTAINER:-oneroad-mar-$ARM-$TASK-$SEED}"
HOST_UID="$(id -u)"; HOST_GID="$(id -g)"

# The caches are per-SEED and are deliberately NOT removed: a rescore is a thing
# you run again, and re-downloading a toolchain and a dependency tree every time
# is the only slow part of it. `docker volume rm oneroad-mar-{cargo,rustup}-rescore-<seed>`
# when the seed is finished with.
VOL="oneroad-mar-cargo-rescore-$SEED"
TCVOL="oneroad-mar-rustup-rescore-$SEED"

# THE OUTPUT IS A SIBLING OF THE CELL, NOT A CHILD OF IT. A live cell's directory
# is read by record.py, road.py and the settle detector while the cell runs, and
# a rescore is an OUTSIDE reading — it has no business adding directories to the
# thing it is measuring. RESCORE_OUT overrides it.
OUT="${RESCORE_OUT:-$MAR_OUT/rescore/$ARM-$TASK-$SEED}/$N"
CURVE="$(dirname "$OUT")/curve.txt"

docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null | grep -q true \
  || { echo "rescore: container $CONTAINER is not running"; exit 2; }
[ -d "$TASKDIR" ] || { echo "rescore: no such task dir $TASKDIR"; exit 2; }
rm -rf "$OUT"; mkdir -p "$OUT/logs"

WORKDIR="$(docker image inspect -f '{{.Config.WorkingDir}}' "$IMAGE" 2>/dev/null)"
[ -n "$WORKDIR" ] || { echo "rescore: image $IMAGE declares no WORKDIR"; exit 2; }
PARENT="$(dirname "$WORKDIR")"; BASE="$(basename "$PARENT")"

ELAPSED=0
if [ -f "$CELL/started-at" ]; then
  STARTED="$(date -d "$(cat "$CELL/started-at")" +%s 2>/dev/null || echo 0)"
  [ "$STARTED" != "0" ] && ELAPSED=$(( $(date +%s) - STARTED ))
fi

say() { printf '[%s] rescore %s/%s: %s\n' "$(date +%H:%M:%S)" "$SEED" "$N" "$*"; }

say "copying $PARENT out of $CONTAINER (no target/, .git, .aforge-v3)"
if ! docker exec "$CONTAINER" tar czf - -C "$(dirname "$PARENT")" \
      --exclude="$BASE/*/target" --exclude="$BASE/*/.git" \
      --exclude="$BASE/*/.aforge-v3" --exclude="$BASE/*/node_modules" \
      --exclude="$BASE/*/__pycache__" --exclude="$BASE/*/.venv" \
      "$BASE" > "$OUT/workspace.tgz" 2>"$OUT/copy.err"; then
  say "workspace copy FAILED"; sed 's/^/  /' "$OUT/copy.err"; exit 1
fi

SC="oneroad-mar-rescore-$SEED-$N"
docker rm -f "$SC" >/dev/null 2>&1
trap 'docker rm -f "$SC" >/dev/null 2>&1' EXIT INT TERM
docker run -d --name "$SC" --cpus "$CPUS" --memory "$MEM" \
  -v "$OUT/logs:/logs" -v "$VOL:/root/.cargo/registry" -v "$TCVOL:/root/.rustup" \
  --entrypoint sleep "$IMAGE" infinity >/dev/null || { say "scorer container failed to start"; exit 1; }

# /opt, not /tmp: tests/test.sh's cached-golden scan walks /tmp, /workspace,
# /root and /home for large json, and a staging file in any of those is a way to
# invent a cheat that never happened.
docker cp "$OUT/workspace.tgz" "$SC:/opt/snap.tgz" >/dev/null
docker cp "$TASKDIR/tests" "$SC:/tests" >/dev/null
docker exec "$SC" sh -c \
  "rm -rf '$PARENT' && tar xzf /opt/snap.tgz -C '$(dirname "$PARENT")' && rm -f /opt/snap.tgz" >/dev/null

TC="$(bash "$MAR/toolchain.sh" detect "$CONTAINER" 2>/dev/null | awk '{print $1}')"
RUSTC=""
[ -n "$TC" ] && RUSTC="$(bash "$MAR/toolchain.sh" adopt "$SC" "$TC" 2>>"$OUT/toolchain.log")"
[ -n "$RUSTC" ] || RUSTC="$(docker exec "$SC" sh -c 'PATH=/root/.cargo/bin:$PATH rustc --version' 2>/dev/null)"
say "scoring with ${RUSTC:-unknown} (agent default: ${TC:-undetected})"

timeout "$CAP" docker exec "$SC" bash /tests/test.sh > "$OUT/verify.log" 2>&1
VC=$?
docker exec "$SC" chown -R "$HOST_UID:$HOST_GID" /logs >/dev/null 2>&1
docker rm -f "$SC" >/dev/null 2>&1; trap - EXIT INT TERM
rm -f "$OUT/workspace.tgz"

ROW="$(OUT="$OUT" VC="$VC" python3 - <<'PY'
import json, os, re
out, vc = os.environ["OUT"], os.environ["VC"]
d = {}
for name in ("metrics.json", "metrics_main.json"):
    try:
        d = json.load(open(os.path.join(out, "logs", "verifier", name))); break
    except Exception:
        continue
if d:
    main = d.get("main") or d
    hold = d.get("holdout") or {}
    print("partial=%.4f main=%s/%s holdout=%.4f reward=%s" % (
        float(d.get("partial_score") or 0.0), main.get("passed"), main.get("total"),
        float(hold.get("partial_score") or 0.0), d.get("reward")))
else:
    log = ""
    try:
        log = open(os.path.join(out, "verify.log")).read()
    except OSError:
        pass
    why = ("CHEAT DETECTED" if "CHEAT DETECTED" in log else
           "did not compile" if re.search(r"could not compile|error\[E", log) else
           "killed at timeout" if vc == "124" else
           "no metrics.json")
    print("no-score (%s)" % why)
PY
)"

printf '%s n=%s elapsed_s=%s %s [%s]\n' "$(date +%H:%M)" "$N" "$ELAPSED" "$ROW" "${RUSTC:-rustc unknown}" \
  | tee -a "$CURVE"
say "artifacts in $OUT"
