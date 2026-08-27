#!/usr/bin/env bash
# marathon/snapshot.sh — the progress curve: partial_score against elapsed time.
#
# Usage: snapshot.sh <cell> <container> <image> <taskdir> <workdir> <started-epoch>
#
# WHY IT SCORES SOMEWHERE ELSE. The obvious thing — score in the agent's own
# container — would hand the agent's four CPUs to a `cargo build --release` and a
# 68,000-point scoring run for half an hour, several times over a ten-hour cell,
# and every curve point would be paid for out of the result it is measuring. So a
# snapshot is a tarball of the workspace (WITHOUT target/, which is build output
# and can be gigabytes) scored in a SHORT-LIVED container of its own, from the
# same image, on two CPUs, with a hard kill.
#
# THIS LOOP MAY NEVER TAKE THE CELL DOWN. A snapshot that fails to build, fails
# to score, or runs out of its half hour records a row saying so and goes back to
# sleep. Nothing in here is allowed to be fatal, which is why every step is
# guarded and the whole script runs with errors non-fatal.
#
# IT COMPILES WITH THE AGENT'S COMPILER, NOT THE IMAGE'S. The agent may upgrade
# its toolchain — legally, officially, and today almost unavoidably, since
# lsp-types -> url 2.5.8 -> idna -> icu 2.3 needs rustc 1.88 and the image ships
# 1.86.0 — and the real verifier inherits that upgrade because it runs in the
# agent's own container. A scorer that always used the image's 1.86.0 would
# report 0.0 for a workspace the benchmark scores properly: a curve that
# UNDER-reads, which is the one failure mode a curve must not have. So every
# snapshot asks the agent's container what its default toolchain is
# (toolchain.sh detect) and installs it here first. The toolchains live in a
# per-cell docker volume, so the first snapshot of a cell pays for the download
# and no later one does, and the rustc actually used is written into the row.
#
# The curve is INDICATIVE, NOT THE VERDICT. It scores a fresh container's
# pristine /workspace/java and /workspace/golden.jsonl, so a snapshot cannot show
# the integrity or cached-golden failures that only the real verifier — which
# looks at the agent's own container — can see.
set -uo pipefail

CELL="${1:?snapshot.sh <cell> <container> <image> <taskdir> <workdir> <started>}"
CONTAINER="${2:?}"; IMAGE="${3:?}"; TASKDIR="${4:?}"; WORKDIR="${5:?}"; STARTED="${6:?}"
EVERY="${SNAPSHOT_EVERY:-3600}"
CAP="${SNAPSHOT_TIMEOUT:-1800}"
SNAP_CPUS="${SNAPSHOT_CPUS:-2}"
SNAP_MEM="${SNAPSHOT_MEM:-8g}"

HOST_UID="$(id -u)"; HOST_GID="$(id -g)"
PARENT="$(dirname "$WORKDIR")"; LEAF="$(basename "$WORKDIR")"
CURVE="$CELL/curve.csv"
# A cargo registry that survives between snapshots. Every snapshot build starts
# from an empty target/ and would otherwise re-download the whole dependency tree
# each hour; the cache is per-cell so two cells' snapshot builds never contend on
# the same registry lock. It is the SNAPSHOT containers' cache only — the agent's
# own container never sees it, so nothing is warmed for the run under test.
VOL="oneroad-mar-cargo-$(basename "$CELL")"
# The rustup store is a SECOND per-cell volume, mounted at the image's own
# /root/.rustup. A named volume is populated from the image on first use, so the
# scorer starts with the shipped 1.86.0 already there and only ever downloads the
# delta the agent chose — once for the cell, not once an hour. Per-cell rather
# than shared because two cells calling `rustup toolchain install` on one store
# at the same second is a race nobody needs.
TCVOL="oneroad-mar-rustup-$(basename "$CELL")"
MAR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

log() { printf '[%s] snapshot: %s\n' "$(date +%H:%M:%S)" "$*"; }

row() {  # row <elapsed> <partial> <passed> <total> <reward> <note>
  ELAPSED="$1" PARTIAL="$2" PASSED="$3" TOTAL="$4" REWARD="$5" NOTE="$6" CURVE="$CURVE" \
  python3 - <<'PY'
import csv, os
path = os.environ["CURVE"]
new = not os.path.exists(path)
with open(path, "a", newline="") as fh:
    w = csv.writer(fh, quoting=csv.QUOTE_MINIMAL)
    if new:
        w.writerow(["elapsed_s", "partial_score", "passed", "total", "reward", "note"])
    w.writerow([os.environ[k] for k in ("ELAPSED", "PARTIAL", "PASSED", "TOTAL", "REWARD", "NOTE")])
PY
}

cleanup() { docker volume rm "$VOL" "$TCVOL" >/dev/null 2>&1; }
trap cleanup EXIT

# THE ARMS ARE STAGGERED SO THEIR SCORERS NEVER PILE UP. Three cells launched
# arm-fair reach every hour mark together, and three release builds plus three
# 68,000-point scoring runs starting at the same second would take six CPUs off
# a machine that has already reserved twelve to the cells themselves. The offset
# shifts the phase only: each arm still gets one point an hour, and every row
# carries the elapsed time it was actually taken at.
[ "${SNAPSHOT_OFFSET:-0}" -gt 0 ] && sleep "$SNAPSHOT_OFFSET"

n=0
while :; do
  # Sleep in small steps so the loop notices the cell ending rather than holding a
  # snapshot container alive past it.
  slept=0
  while [ "$slept" -lt "$EVERY" ]; do
    [ -f "$CELL/agent-done" ] && exit 0
    docker inspect -f '{{.State.Running}}' "$CONTAINER" 2>/dev/null | grep -q true || exit 0
    sleep 10; slept=$((slept + 10))
  done
  [ -f "$CELL/agent-done" ] && exit 0

  n=$((n + 1))
  TAG="$(printf 't%02d' "$n")"
  DIR="$CELL/snapshots/$TAG"
  mkdir -p "$DIR/logs"
  ELAPSED=$(( $(date +%s) - STARTED ))
  log "$TAG at ${ELAPSED}s — copying workspace"

  # THE WHOLE OF /workspace TRAVELS, NOT THE CRATE, AND THE FIRST THREE s1 CURVE
  # POINTS ARE WHAT THAT MISTAKE LOOKS LIKE.
  #
  # This copied only the crate leaf, on the reasoning that the crate is the work.
  # It is not what the benchmark scores. The scorer addresses every test point as
  # `file:///workspace/test-files/<rel>`, and the s1 worker met that by creating
  # `/workspace/test-files -> /workspace/java` — one symlink, one level ABOVE the
  # crate, therefore outside the copy. Three hourly points came back 0/68186 from
  # a server that was answering correctly for 132 of them, because in the scoring
  # container the URIs resolved to nothing. A curve that under-reads is worse than
  # no curve: it reads as an agent making no progress.
  #
  # So the copy is now the whole of /workspace, minus only what the verifier
  # rebuilds for itself (target/) and what is not part of the state being judged
  # (.git and the usual vendored trees). The corpus travels with it — java/ and
  # golden.jsonl — which is also the more faithful mirror: the benchmark hashes
  # the AGENT's copy of those against its manifest, so a snapshot that scored the
  # image's pristine ones could never show an integrity failure the real verifier
  # would find.
  #
  # NO `-h`. tar stores a symlink as a symlink unless told to follow it, and
  # following it here would replace the one-byte link with a second copy of the
  # 1,007-file corpus — and, worse, would hide from the scorer whether the agent's
  # arrangement actually works.
  if ! docker exec "$CONTAINER" tar czf - -C "$(dirname "$PARENT")" \
        --exclude="$(basename "$PARENT")/*/target" \
        --exclude="$(basename "$PARENT")/*/.git" \
        --exclude="$(basename "$PARENT")/*/node_modules" \
        --exclude="$(basename "$PARENT")/*/__pycache__" \
        --exclude="$(basename "$PARENT")/*/.venv" \
        "$(basename "$PARENT")" > "$DIR/workspace.tgz" 2>"$DIR/copy.err"; then
    log "$TAG copy failed"; row "$ELAPSED" 0 0 0 0 "workspace copy failed"; continue
  fi

  SC="oneroad-mar-snap-$(basename "$CELL")-$TAG"
  docker rm -f "$SC" >/dev/null 2>&1
  if ! docker run -d --name "$SC" --cpus "$SNAP_CPUS" --memory "$SNAP_MEM" \
        -v "$DIR/logs:/logs" -v "$VOL:/root/.cargo/registry" -v "$TCVOL:/root/.rustup" \
        --entrypoint sleep "$IMAGE" infinity >/dev/null 2>"$DIR/run.err"; then
    log "$TAG scorer container failed to start"; row "$ELAPSED" 0 0 0 0 "scorer container failed to start"; continue
  fi

  # /opt, not /tmp: test.sh's cached-golden scan walks /tmp, /workspace, /root and
  # /home looking for large json files, and a staging directory in any of those is
  # a way for this loop to invent a cheat that never happened.
  docker cp "$DIR/workspace.tgz" "$SC:/opt/snap.tgz" >/dev/null 2>&1
  docker cp "$TASKDIR/tests" "$SC:/tests" >/dev/null 2>&1
  # The image's own /workspace is REMOVED before the agent's is unpacked over the
  # same place. Extracting on top would leave the image's files wherever the
  # agent had deleted or replaced one, so the scorer would be judging a blend of
  # two workspaces that never existed. `tar x` restores symlinks as symlinks, so
  # the agent's arrangement arrives intact and is tested rather than assumed.
  docker exec "$SC" sh -c \
    "rm -rf '$PARENT' && tar xzf /opt/snap.tgz -C '$(dirname "$PARENT")' && rm -f /opt/snap.tgz" >/dev/null 2>&1

  # THE AGENT'S COMPILER, ADOPTED. Detection is done fresh every hour because the
  # agent can upgrade at any point in ten hours, and the curve has to follow it.
  # A detection or install that fails is NOT fatal — the scorer falls back to
  # whatever the volume already has, and the row says which rustc that was, so a
  # reader can see the fallback rather than being silently told a lower number.
  SNAP_TC="$(bash "$MAR/toolchain.sh" detect "$CONTAINER" 2>/dev/null | awk '{print $1}')"
  if [ -n "$SNAP_TC" ]; then
    SCORER_RUSTC="$(bash "$MAR/toolchain.sh" adopt "$SC" "$SNAP_TC" 2>>"$DIR/toolchain.log")"
  else
    SCORER_RUSTC=""
  fi
  if [ -z "$SCORER_RUSTC" ]; then
    SCORER_RUSTC="$(docker exec "$SC" sh -c 'PATH=/root/.cargo/bin:$PATH rustc --version' 2>/dev/null)"
    log "$TAG could not adopt '${SNAP_TC:-<undetected>}' — scoring with ${SCORER_RUSTC:-unknown}"
  fi

  timeout "$CAP" docker exec "$SC" bash /tests/test.sh > "$DIR/verify.log" 2>&1
  VC=$?
  docker exec "$SC" chown -R "$HOST_UID:$HOST_GID" /logs >/dev/null 2>&1
  docker rm -f "$SC" >/dev/null 2>&1

  NOTE=""; [ "$VC" = "124" ] && NOTE="killed at ${CAP}s"
  DIR="$DIR" python3 - > "$DIR/metrics-row.txt" 2>/dev/null <<'PY'
import json, os
d = {}
for name in ("metrics.json", "metrics_main.json"):
    try:
        d = json.load(open(os.path.join(os.environ["DIR"], "logs", "verifier", name)))
        break
    except Exception:
        continue
main = d.get("main") or d
print(d.get("partial_score", 0.0) or 0.0,
      main.get("passed", 0) or 0, main.get("total", 0) or 0,
      d.get("reward", 0.0) or 0.0)
PY
  P=""; PA=""; TO=""; RW=""
  read -r P PA TO RW < "$DIR/metrics-row.txt" 2>/dev/null
  [ -n "$P" ] || { P=0; PA=0; TO=0; RW=0; NOTE="${NOTE:-no metrics.json produced}"; }
  if [ "$TO" = "0" ] && [ -z "$NOTE" ]; then
    # A FAILING BUILD LEAVES NO metrics.json AT ALL, and that is the benchmark's
    # own behaviour rather than a fault here: tests/test.sh runs under
    # `set -euo pipefail`, so `cargo build --release 2>&1 | tail -20` failing
    # exits the script before it can reach write_zero_metrics. reward.txt (0.0,
    # written at the top) is all that survives. The row says which of the two
    # zero shapes this was, because "did not compile" and "compiled but scored
    # nothing" are different facts about the attempt.
    if [ ! -f "$DIR/logs/verifier/metrics.json" ] && grep -q "could not compile\|error\[E" "$DIR/verify.log" 2>/dev/null; then
      NOTE="cargo build failed — test.sh exited on its own pipefail before scoring"
    elif [ ! -f "$DIR/logs/verifier/metrics.json" ]; then
      NOTE="no metrics.json — test.sh stopped before scoring"
    else
      NOTE="verifier scored nothing (no binary at the expected path)"
    fi
  fi
  # The compiler is part of the reading, not a footnote to it: two rows with the
  # same elapsed time and different rustc are two different measurements.
  row "$ELAPSED" "$P" "$PA" "$TO" "$RW" "${NOTE:+$NOTE — }${SCORER_RUSTC:-rustc unknown}"
  log "$TAG partial=$P passed=$PA/$TO reward=$RW [${SCORER_RUSTC:-rustc unknown}] ${NOTE:+($NOTE)}"
  # The tarball is the bulky part and the agent's own files are what a reader
  # wants, so it is unpacked beside its own score and dropped. The corpus is left
  # out of the unpacked copy — java/ and golden.jsonl are thirty megabytes of the
  # dataset's own bytes, identical in every snapshot of every arm, and keeping ten
  # copies per cell of files that live in the image would be the only thing on
  # this disk that grows for no reason. They were in the tar that was SCORED,
  # which is where they mattered.
  # --strip-components=1 drops the archive's own leading `workspace/`, so the
  # kept copy reads as the workspace itself rather than as workspace/workspace.
  mkdir -p "$DIR/workspace" \
    && tar xzf "$DIR/workspace.tgz" -C "$DIR/workspace" --strip-components=1 \
         --exclude="*/java" --exclude="*/java/*" --exclude="*/golden.jsonl" 2>/dev/null \
    && rm -f "$DIR/workspace.tgz"
done
