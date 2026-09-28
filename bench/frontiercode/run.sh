#!/usr/bin/env bash
# Run one frontiercode cell: the senior-dev harness under test, inside the
# task's own container, over one task, with internet on through the rig's
# open-and-logged egress proxy, and the provider key held OUTSIDE the
# container by the credential guard. Then grade it.
#
#   bench/frontiercode/run.sh <task-id> <model-id> <seed-tag>
#
# Env:
#   CODEAF_BIN    path to a linux binary for the container's architecture
#                 (required; bin/codeaf-linux-arm64 by default when present)
#   MODEL         the model pin (default: deepseek/deepseek-v4-flash-0731 —
#                 the model the DeepSWE campaigns pinned, so results can be
#                 cross-checked)
#   VARIANT       reasoning effort per call (default: high, recorded)
#   MAX_COST      the run's dollar ceiling (default 5)
#   MAX_HOURS     the run's wall in hours (default 2)
#   RESULTS       output root (default bench/frontiercode/results)
#   KEEP          1 keeps the containers for post-mortem
#   SKIP_GRADE    1 stops once the patch is collected — a rig dry run
#
# Writes results/<task>-<model>-<seed>/{run.log,records.jsonl,home/,homeledger/,
# model.patch,grade.json,scan.json,record.jsonl,egress-proxy.log,guard-*.jsonl}.
# Every phase is recorded even when it fails — the question a failed run has to
# answer is which layer broke.
#
# THE NETWORK LAYOUT. Three containers on two docker networks:
#
#   internal net (no route out)          bridge net (internet)
#   ┌────────────────────────┐           ┌─────────────────────┐
#   │ agent (senior-dev)     │──guard──▶ │ guard ──▶ openrouter│   model plane
#   │        via egress:3128 │──proxy──▶ │ egress proxy        │   agent egress
#   └────────────────────────┘           └─────────────────────┘
#
# The agent's model calls go to the guard (its base URL names the guard; the
# guard holds the real key and meters every call); everything else the
# container asks for goes through the open-and-logged proxy, which permits
# everything and writes every connection down. The scanner reads both the
# proxy log and the harness's own transcript afterwards and flags a run that
# visited the task's upstream. The container holds no credential at all.
set -uo pipefail

# ── snapshot the rig, then run from the copy ────────────────────────────────
# Bash reads a script by byte offset as it goes, so editing the rig while a
# run is in flight makes the running shell resume in the middle of the new
# text — the failure bench/deepswe lost two ninety-minute runs to. Every run
# copies the rig into its own result directory first and re-execs from that
# copy, which nothing later edits.
__RIG_SRC="$(cd "$(dirname "$0")" && pwd)"
if [ -z "${BENCH_SNAPSHOT:-}" ]; then
  [ $# -eq 3 ] || { echo "usage: run.sh <task-id> <model-id> <seed-tag>" >&2; exit 2; }
  __slug="$(printf '%s' "$2" | tr '/:' '--')"
  export RESULTS="${RESULTS:-$__RIG_SRC/results}"
  __out="$RESULTS/$1-$__slug-$3"
  rm -rf "$__out"; mkdir -p "$__out/rig"
  cp -R "$__RIG_SRC/lib.sh" "$__RIG_SRC/run.sh" "$__RIG_SRC/grade.sh" "$__RIG_SRC/gold.sh" \
        "$__RIG_SRC/negative.sh" "$__RIG_SRC/seal.sh" "$__RIG_SRC/report.py" "$__out/rig/"
  cp -R "$__RIG_SRC/grade" "$__out/rig/grade"
  cp -R "$__RIG_SRC/bin" "$__out/rig/bin" 2>/dev/null || true
  # The credential guard is the conversation battery's; a run's frozen copy
  # carries its own, like journal.py in the DeepSWE rig.
  cp "$__RIG_SRC/../conversation/lib/guard.py" "$__out/rig/guard.py"
  export BENCH_SNAPSHOT="$__out/rig"
  export BENCH_RIG_REV="$(git -C "$__RIG_SRC" rev-parse --short HEAD 2>/dev/null || echo unknown)"
  exec bash "$__out/rig/run.sh" "$@"
fi

source "$(cd "$(dirname "$0")" && pwd)/lib.sh"

[ $# -eq 3 ] || { echo "usage: run.sh <task-id> <model-id> <seed-tag>" >&2; exit 2; }
TASK="$1"; MODEL="${2:-${MODEL:-deepseek/deepseek-v4-flash-0731}}"; SEED="$3"
SLUG="$(printf '%s' "$MODEL" | tr '/:' '--')"
VARIANT="${VARIANT:-high}"
MAX_COST="${MAX_COST:-5}"
MAX_HOURS="${MAX_HOURS:-2}"
CHAT_CAP="${MAX_COST}"

BIN="${CODEAF_BIN:-}"
[ -n "$BIN" ] && [ -x "$BIN" ] || {
  for candidate in "$RIG_DIR/bin/codeaf-linux-arm64" "$RIG_DIR/bin/codeaf-linux-amd64"; do
    if [ -x "$candidate" ]; then BIN="$candidate"; break; fi
  done
}
[ -n "$BIN" ] && [ -x "$BIN" ] || { echo "run.sh: no linux codeaf binary (CODEAF_BIN or rig/bin/codeaf-linux-<arch>)" >&2; exit 2; }

load_key || { echo "run.sh: no provider key (API_KEY / OPENROUTER_API_KEY / keychain)" >&2; exit 2; }

load_task "$TASK" || exit 1
OUT="$RESULTS/$TASK-$SLUG-$SEED"
mkdir -p "$OUT"
NAME="fc-$TASK-$SEED"
SENTINEL="bench-sentinel-$RANDOM$RANDOM"

# Build the environment image before anything else: the run is dead without it.
mkdir -p "$RESULTS"
ensure_env_image || { log "$TASK: environment image build failed — see $RESULTS/.build-$TASK-env.log"; exit 1; }

meta() { python3 - "$OUT/meta.json" "$@" <<'PY'
import json, os, sys
path = sys.argv[1]
d = json.load(open(path)) if os.path.exists(path) else {}
for kv in sys.argv[2:]:
    k, _, v = kv.partition("=")
    try:
        v = json.loads(v)
    except Exception:
        pass
    d[k] = v
json.dump(d, open(path, "w"), indent=2)
PY
}
BIN_SHA="$(shasum -a 256 "$BIN" 2>/dev/null | cut -c1-16)"
meta "task=$TASK" "arm=codeaf-senior-dev" "model=$MODEL" "variant=$VARIANT" "seed=$SEED" \
     "language=$TASK_LANG" "image=$TASK_IMAGE" "base_commit=$TASK_BASE" "reference_commit=$TASK_REFERENCE" \
     "platform=$PLATFORM" "cpus=$TASK_CPUS" "memory_mb=$TASK_MEM" "agent_seconds_budget=$TASK_SECS" \
     "emulated=$EMULATED" "rig_rev=${BENCH_RIG_REV:-unknown}" "bin_sha256_16=$BIN_SHA" \
     "max_cost_usd=$MAX_COST" "max_hours=$MAX_HOURS" "stage=start"
python3 "$RIG_DIR/grade/record.py" start --run-dir "$OUT" \
  --json "{\"task\":\"$TASK\",\"arm\":\"codeaf-senior-dev\",\"model\":\"$MODEL\",\"variant\":\"$VARIANT\",\"seed\":\"$SEED\",\"base_commit\":\"$TASK_BASE\"}"

cleanup() {
  for c in "$NAME" fc-egress-$SEED fc-guard-$SEED; do
    docker rm -f "$c" >/dev/null 2>&1
  done
  docker network rm "fc-in-$SEED" >/dev/null 2>&1
  return 0
}
trap cleanup EXIT

# ── the network: an internal net with two exits, guard and proxy ────────────
docker network create --internal "fc-in-$SEED" > /dev/null 2>&1

log "$TASK: starting the credential guard (holds the key, meters every call)"
# The guard binds loopback by design; the run's frozen copy of it is widened
# to the container's own interfaces so the internal network can reach it. The
# copy is transformed, never the shared file.
sed 's/("127.0.0.1", args.port)/("0.0.0.0", args.port)/' \
  "$RIG_DIR/guard.py" > "$OUT/rig/guard-container.py"
docker run -d --name "fc-guard-$SEED" --network "fc-in-$SEED" \
  -e "GUARD_UPSTREAM_KEY=$KEY" \
  -v "$OUT:/audit" \
  --entrypoint python3 python:3.12-slim \
  /audit/rig/guard-container.py --allow "$MODEL" \
  --audit /audit/guard-audit.jsonl --usage /audit/guard-usage.jsonl \
  --sentinel "$SENTINEL" --scope "fc-$TASK-$SEED" > "$OUT/guard-start.log" 2>&1 || {
  log "$TASK: guard failed to start — see $OUT/guard-start.log"; meta "stage=guard-failed"; exit 1; }
docker network connect bridge "fc-guard-$SEED" > /dev/null 2>&1
GUARD_PORT=""
for _ in $(seq 1 60); do
  GUARD_PORT="$(docker logs "fc-guard-$SEED" 2>/dev/null | sed -n 's/^PORT //p' | tail -1)"
  [ -n "$GUARD_PORT" ] && break
  sleep 1
done
[ -n "$GUARD_PORT" ] || { log "$TASK: the guard never reported its port — see $OUT/guard-start.log"; meta "stage=guard-no-port"; exit 1; }
log "$TASK: guard listening on port $GUARD_PORT"

log "$TASK: starting the egress proxy (open and logged)"
docker run -d --name "fc-egress-$SEED" --network "fc-in-$SEED" \
  -v "$SNAPSHOT_DIR/bin:/rigbin:ro" -v "$OUT:/logs" \
  --entrypoint /rigbin/egress-proxy-$([ "$PLATFORM" = linux/amd64 ] && echo amd64 || echo arm64) \
  alpine:3.20 -addr :3128 -log /logs/egress-proxy.log > "$OUT/egress-start.log" 2>&1 || {
  log "$TASK: egress proxy failed to start — see $OUT/egress-start.log"; meta "stage=egress-failed"; exit 1; }
docker network connect bridge "fc-egress-$SEED" > /dev/null 2>&1
sleep 2

log "$TASK: starting agent container"
emu_args
if ! docker run -d --platform "$PLATFORM" --name "$NAME" \
     --network "fc-in-$SEED" \
     --cpus "$TASK_CPUS" --memory "${TASK_MEM}m" \
     "$TASK_IMAGE" sleep infinity >> "$OUT/docker.log" 2>&1; then
  meta "stage=start-failed"; log "$TASK: docker run failed"; exit 1
fi

# An isolated home, so the run can never read or write the owner's profile.
# The profile's api_key is the guard's SENTINEL — the container holds no real
# credential — and the model plane's base URL names the guard.
PROFILE="$OUT/profile"; mkdir -p "$PROFILE"; chmod 700 "$PROFILE"
python3 - "$PROFILE/config.json" "$SENTINEL" "$MAX_COST" <<'PY'
import datetime, json, sys
path, sentinel, cap = sys.argv[1], sys.argv[2], float(sys.argv[3])
out = {
    "api_key": sentinel,
    "tools.approvalMode": "allow",
    "daily_budget_usd": cap,
    "setup_seen_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
}
json.dump(out, open(path, "w"), indent=2)
PY

docker cp "$BIN" "$NAME:/usr/local/bin/codeaf" >> "$OUT/docker.log" 2>&1
docker exec "$NAME" chmod +x /usr/local/bin/codeaf >> "$OUT/docker.log" 2>&1
docker exec "$NAME" mkdir -p /bench/home >> "$OUT/docker.log" 2>&1
docker cp "$PROFILE/config.json" "$NAME:/bench/home/config.json" >> "$OUT/docker.log" 2>&1
docker cp "$TASK_DIR/instruction.md" "$NAME:/bench/instruction.md" >> "$OUT/docker.log" 2>&1
cp "$TASK_DIR/instruction.md" "$OUT/prompt.md"
# The repo must be clean before a run can start (senior-dev refuses a dirty
# tree — the folder is readied, not worked over) and the base asserted.
docker exec -w /root/repos/jsonschema "$NAME" sh -c \
  'git config --global --add safe.directory /root/repos/jsonschema; git status --porcelain | wc -l' \
  > "$OUT/base-status.txt" 2>> "$OUT/docker.log"
docker exec -w /root/repos/jsonschema "$NAME" git rev-parse HEAD > "$OUT/base-head.txt" 2>> "$OUT/docker.log"
if [ "$(cat "$OUT/base-head.txt")" != "$TASK_BASE" ] || [ -s "$OUT/base-status.txt" ]; then
  meta "stage=base-not-pristine"
  log "$TASK: the task tree is not pristine at the base commit — refusing to run"
  exit 1
fi

# The drive line: senior-dev's run command, the model pinned on its pool
# (--asked, so a model the catalog cannot size fails the run rather than being
# quietly swapped), the variant recorded, the ceilings the rig owns.
docker exec -i "$NAME" tee /bench/drive.sh > /dev/null <<INNER
#!/bin/sh
set -u
exec /usr/local/bin/codeaf senior-dev run \\
  --json \\
  --dir /root/repos/jsonschema \\
  --max-cost "$MAX_COST" \\
  --max-hours "$MAX_HOURS" \\
  --variant "$VARIANT" \\
  --high "openrouter/$MODEL" \\
  --asked \\
  -- "\$(cat /bench/instruction.md)"
INNER
docker exec "$NAME" chmod +x /bench/drive.sh >> "$OUT/docker.log" 2>&1

log "$TASK: codeaf senior-dev — model $MODEL, wall ${MAX_HOURS}h, cap \$$MAX_COST"
meta "stage=agent"
t0=$(date +%s)
timeout "$((TASK_SECS + 300))" docker exec "${EMU_ARGS[@]}" \
  -e "CODEAF_HOME=/bench/home" \
  -e "HOME=/root" \
  -e "CODEAF_BASE_URL=http://guard:$GUARD_PORT/api/v1" \
  -e "HTTPS_PROXY=http://egress:3128" \
  -e "HTTP_PROXY=http://egress:3128" \
  -e "ALL_PROXY=http://egress:3128" \
  -e "NO_PROXY=localhost,127.0.0.1,::1,guard,egress" \
  -w /root "$NAME" /bench/drive.sh > "$OUT/records.jsonl" 2> "$OUT/run.log"
CODE=$?
WALL=$(( $(date +%s) - t0 ))
# senior-dev's exit ladder: 0 done · 2 incomplete · 3 a limit · 4 asked · 124 the rig's own wall.
case "$CODE" in 0) ENDED=self ;; 124) ENDED="wall (killed)" ;; 3) ENDED=limit ;; 4) ENDED=asked ;; *) ENDED=partial ;; esac
log "$TASK: codeaf exited $CODE after ${WALL}s"
meta "exit_code=$CODE" "ended=$ENDED" "agent_seconds=$WALL" "stage=extract"

rm -rf "$OUT/home"; docker cp "$NAME:/bench/home" "$OUT/home" >> "$OUT/docker.log" 2>&1
scrub_profile "$OUT/home/config.json"

# The patch, from every place the work could have landed: the worktree with
# untracked files staged, every local branch, HEAD. Richest source diff wins.
docker exec -i "$NAME" tee /bench/collect.sh > /dev/null <<'COLLECT'
#!/bin/sh
# Collect the graded diff. The same discipline bench/deepswe learned: "the
# index" is not the work — a run that committed to its own branch and left the
# worktree elsewhere grades as empty unless every candidate is measured.
set -u
base="$1"; out=/bench/candidates
rm -rf "$out"; mkdir -p "$out"
cd /root/repos/jsonschema || exit 1
git add -A >/dev/null 2>&1
git diff --cached --binary "$base" > "$out/worktree.patch" 2>/dev/null
for ref in $(git for-each-ref --format='%(refname:short)' refs/heads 2>/dev/null); do
  safe=$(printf '%s' "$ref" | tr '/' '_')
  git diff --binary "$base" "$ref" > "$out/branch-$safe.patch" 2>/dev/null
done
git diff --binary "$base" HEAD > "$out/head.patch" 2>/dev/null
for f in "$out"/*.patch; do
  [ -f "$f" ] || continue
  printf '%s\t%s\n' "$(wc -c < "$f" | tr -d ' ')" "$(basename "$f")"
done
COLLECT
docker exec "$NAME" chmod +x /bench/collect.sh >> "$OUT/docker.log" 2>&1
docker exec "$NAME" /bench/collect.sh "$TASK_BASE" > "$OUT/candidates.tsv" 2>> "$OUT/docker.log"
BEST=$(sort -t"$(printf '\t')" -k1,1nr "$OUT/candidates.tsv" 2>/dev/null | head -1 | cut -f2)
BEST="${BEST:-worktree.patch}"
docker exec "$NAME" cat "/bench/candidates/$BEST" > "$OUT/model.patch" 2>> "$OUT/docker.log"
rm -rf "$OUT/candidates"; docker cp "$NAME:/bench/candidates" "$OUT/candidates" >> "$OUT/docker.log" 2>&1
log "$TASK: graded diff taken from $BEST"
meta "patch_source=$BEST" "patch_bytes=$(wc -c < "$OUT/model.patch" | tr -d ' ')" "stage=cost"

# Cost, read twice: the harness's own ledger (rows named for this run) and the
# guard's meter (every admitted call, priced by upstream's own usage). The
# guard's figure is the honest one; the harness's is the cross-check.
python3 - "$OUT" <<'PY'
import json, os, sys
out = sys.argv[1]
home = os.path.join(out, "home")
ledger = os.path.join(home, "v3", "usage.jsonl")
rec = {"cost_usd": 0.0, "prompt_tokens": 0, "completion_tokens": 0, "calls": 0, "models": ""}
try:
    # The carried host files its rows under the run's record subject; the run
    # is this result's only resident, so every row in this isolated home is
    # this run's.
    for line in open(ledger, errors="replace"):
        if not line.strip():
            continue
        r = json.loads(line)
        rec["cost_usd"] += r.get("usd") or 0.0
        rec["prompt_tokens"] += r.get("input") or 0
        rec["completion_tokens"] += r.get("output") or 0
        rec["calls"] += r.get("calls") or 0
        rec["models"] = r.get("model", rec["models"])
except FileNotFoundError:
    rec["ledger_error"] = "no usage.jsonl in the run's home"
except Exception as e:
    rec["ledger_error"] = str(e)
# The guard's meter: every admitted call with upstream's own usage row.
gcost = gtin = gtout = gcalls = 0
try:
    for line in open(os.path.join(out, "guard-usage.jsonl"), errors="replace"):
        if not line.strip():
            continue
        r = json.loads(line)
        if r.get("phase") != "settled":
            continue
        gcost += r.get("cost_usd") or 0.0
        usage = r.get("usage") or {}
        gtin += usage.get("prompt_tokens") or 0
        gtout += usage.get("completion_tokens") or 0
        gcalls += 1
    rec["cost_usd_guard"] = gcost
    rec["guard_prompt_tokens"] = gtin
    rec["guard_completion_tokens"] = gtout
    rec["guard_calls"] = gcalls
except FileNotFoundError:
    rec["guard_error"] = "no guard-usage.jsonl — the guard did not meter"
except Exception as e:
    rec["guard_error"] = str(e)
# The cost figure the table carries: the guard's meter when it metered, else
# the harness's own ledger.
rec["cost_usd_final"] = rec.get("cost_usd_guard") if rec.get("cost_usd_guard") is not None else rec["cost_usd"]
rec["tokens_final"] = {"in": rec.get("guard_prompt_tokens") or rec["prompt_tokens"],
                       "out": rec.get("guard_completion_tokens") or rec["completion_tokens"]}
json.dump(rec, open(os.path.join(out, "cost.json"), "w"), indent=2)
print("cost:", json.dumps(rec))
PY

cleanup; trap - EXIT

if [ "${SKIP_GRADE:-0}" = 1 ]; then
  meta "stage=not-graded"; log "$TASK: SKIP_GRADE set — not graded"; exit 0
fi

t1=$(date +%s)
EGRESS_SCAN=1 TASK_ID="$TASK" RESULTS="$RESULTS" bash "$SNAPSHOT_DIR/grade.sh" "$OUT" || \
  log "$TASK: grading failed"
meta "grade_seconds=$(( $(date +%s) - t1 ))" "stage=done"
python3 "$SNAPSHOT_DIR/report.py" "$OUT"