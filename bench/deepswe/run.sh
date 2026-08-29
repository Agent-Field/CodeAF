#!/usr/bin/env bash
# Run the aforge harness headless over one DeepSWE task, inside the task's own
# pinned container, and grade what it left on disk.
#
#   bench/deepswe/run.sh <task-id> <model-id> <seed-tag>
#
# Env:
#   AFORGE_BIN    path to a linux/amd64 aforge binary (required)
#   API_KEY       provider key; falls back to $OPENROUTER_API_KEY, then to the
#                 api_key in ~/.aforge/config.json (read only — never written)
#   CORPUS        tasks directory (default ~/src/swe-pro/tools/deepswe-bench/tasks)
#   RESULTS       output root (default bench/deepswe/results)
#   AGENT_SECONDS override the task's own agent budget
#   CPUS / MEMORY_MB   override the task's own resource budget
#   KEEP          1 keeps the agent container for post-mortem
#
# Writes results/<task>-<model>-<seed>/{run.log,graph.db,model.patch,reward.json,
# cost.json,meta.json}. Every phase is recorded even when it fails, because the
# question a failed run has to answer is *which layer* broke.
#
# The harness runs INSIDE the task container (the binary is copied in), matching
# how the reference runner drives its own CLI. Two deliberate deviations from
# task.toml, both recorded because they affect comparability: the agent
# container has network access (an API-driven agent cannot honour
# `network_mode = "no-network"`), and the container is emulated when the host is
# not amd64. The verifier still runs with --network none, so grading is
# unaffected.
set -uo pipefail
source "$(cd "$(dirname "$0")" && pwd)/lib.sh"

[ $# -eq 3 ] || { echo "usage: run.sh <task-id> <model-id> <seed-tag>" >&2; exit 2; }
TASK="$1"; MODEL="$2"; SEED="$3"
SLUG="$(printf '%s' "$MODEL" | tr '/:' '--')"

BIN="${AFORGE_BIN:-}"
[ -n "$BIN" ] && [ -x "$BIN" ] || { echo "run.sh: set AFORGE_BIN to a linux/amd64 aforge binary" >&2; exit 2; }

KEY="${API_KEY:-${OPENROUTER_API_KEY:-}}"
if [ -z "$KEY" ] && [ -f "$HOME/.aforge/config.json" ]; then
  KEY="$(python3 -c "import json,os;print(json.load(open(os.path.expanduser('~/.aforge/config.json'))).get('api_key',''))" 2>/dev/null)"
fi
[ -n "$KEY" ] || { echo "run.sh: no provider key (API_KEY / OPENROUTER_API_KEY)" >&2; exit 2; }

load_task "$TASK" || exit 1
OUT="$RESULTS/$TASK-$SLUG-$SEED"
rm -rf "$OUT"; mkdir -p "$OUT"
NAME="deepswe-af-$TASK-$SEED"

# An isolated profile, so the run can never read or write the owner's ~/.aforge.
# Every model knob is pinned to the one model: the point of the measurement is
# a single model's behaviour, and any unpinned role silently escalates.
PROFILE="$OUT/profile"; mkdir -p "$PROFILE"
python3 - "$PROFILE/config.json" "$MODEL" "$KEY" <<'PY'
import json, os, sys
path, model, key = sys.argv[1], sys.argv[2], sys.argv[3]
roles = ["auditor", "careful", "compaction", "consolidate", "designer", "division",
         "guardian", "handoff", "imagegen", "intake", "markreader", "planner",
         "reflex", "router", "routerconfirm", "shaper", "speech", "taskname",
         "title", "video", "vision", "worker"]
json.dump({
    "api_key": key,
    "tools.approvalMode": "allow",
    # The roster. `aforge do`'s own path is what this rig measures, so the
    # specialist workers are not installed at all: a run one of them took over
    # would be a measurement of the specialist wearing the harness's name.
    "work.workers": os.environ.get("BENCH_WORKERS", "bare"),
    "models.roles": "\n".join(f"{r}:{model}" for r in roles),
    "model.talk": model,
    "models.tiers.low": model,
    "models.tiers.high": model,
    "models.tiers.reflex": model,
    "models.tiers.mastermind": model,
    "models.fallbacks": model,
    "task.model": model,
    "vision_model": model,
}, open(path, "w"), indent=2)
PY

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
meta "task=$TASK" "model=$MODEL" "seed=$SEED" "language=$TASK_LANG" "image=$TASK_IMAGE" \
     "base_commit=$TASK_BASE" "platform=$PLATFORM" "cpus=$TASK_CPUS" "memory_mb=$TASK_MEM" \
     "agent_seconds_budget=$TASK_SECS" "emulated=$EMULATED" "stage=start"

cleanup() { [ "${KEEP:-0}" = 1 ] || docker rm -f "$NAME" >/dev/null 2>&1; }
trap cleanup EXIT

docker rm -f "$NAME" >/dev/null 2>&1
# Five runs starting together is five pulls together, and the registry answers
# the fifth with "toomanyrequests: Rate exceeded". An image already on the host
# needs no pull at all, and a pull that is refused is worth retrying rather than
# losing the task over.
if docker image inspect "$TASK_IMAGE" >/dev/null 2>&1; then
  log "$TASK: image already present"
else
  pulled=0
  for attempt in 1 2 3 4 5; do
    log "$TASK: pulling $TASK_IMAGE (attempt $attempt)"
    if docker pull --platform "$PLATFORM" "$TASK_IMAGE" >> "$OUT/docker.log" 2>&1; then
      pulled=1; break
    fi
    sleep $((attempt * 20))
  done
  if [ "$pulled" != 1 ]; then
    meta "stage=pull-failed"; log "$TASK: docker pull failed"; exit 1
  fi
fi

log "$TASK: starting agent container ($TASK_CPUS cpu, ${TASK_MEM}m)"
if ! docker run -d --platform "$PLATFORM" --name "$NAME" \
     --cpus "$TASK_CPUS" --memory "${TASK_MEM}m" \
     "$TASK_IMAGE" sleep infinity >> "$OUT/docker.log" 2>&1; then
  meta "stage=start-failed"; log "$TASK: docker run failed"; exit 1
fi

docker cp "$BIN" "$NAME:/usr/local/bin/aforge" >> "$OUT/docker.log" 2>&1
docker exec "$NAME" chmod +x /usr/local/bin/aforge >> "$OUT/docker.log" 2>&1
docker exec "$NAME" mkdir -p /bench/home >> "$OUT/docker.log" 2>&1
docker cp "$PROFILE/config.json" "$NAME:/bench/home/config.json" >> "$OUT/docker.log" 2>&1
docker cp "$TASK_DIR/instruction.md" "$NAME:/bench/instruction.md" >> "$OUT/docker.log" 2>&1

# The brief is handed over on disk and fed in on STDIN. Two reasons, both
# learned the hard way. Multi-line prose full of quotes and backticks routed
# through two layers of shell quoting is how a rig silently truncates the task
# it thinks it asked for. And `aforge do` takes its goal as a positional
# argument, so a brief whose first character is "-" — a bullet, which is how
# several DeepSWE instructions open — is parsed as an unknown flag and the run
# dies at argument parsing with a usage dump. `do` reads the goal from stdin
# when no positional argument is given, and that path has no such hazard.
docker exec -i "$NAME" tee /bench/drive.sh > /dev/null <<'INNER'
#!/bin/sh
set -u
exec /usr/local/bin/aforge do \
  -w /app \
  -model "$BENCH_MODEL" \
  -plan-model "$BENCH_MODEL" \
  -db /bench/graph.db \
  -keep \
  -timeout "$BENCH_TIMEOUT" \
  -yes-spend \
  -json < /bench/instruction.md
INNER
docker exec "$NAME" chmod +x /bench/drive.sh >> "$OUT/docker.log" 2>&1

emu_args
log "$TASK: aforge do — model $MODEL, wall limit ${TASK_SECS}s"
meta "stage=agent"
t0=$(date +%s)
timeout "$((TASK_SECS + 300))" docker exec "${EMU_ARGS[@]}" \
  -e "OPENROUTER_API_KEY=$KEY" \
  -e "AFORGE_HOME=/bench/home" \
  -e "HOME=/root" \
  -e "BENCH_MODEL=$MODEL" \
  -e "BENCH_TIMEOUT=$TASK_SECS" \
  -w /app "$NAME" /bench/drive.sh > "$OUT/run.log" 2>&1
CODE=$?
WALL=$(( $(date +%s) - t0 ))
# 0 = delivered whole, 2 = partial, 124 = the rig's own wall fired first.
log "$TASK: aforge exited $CODE after ${WALL}s"
meta "exit_code=$CODE" "agent_seconds=$WALL" "stage=extract"

# Stage first so files the harness created land in the diff; diffing --cached
# against the base commit then captures committed and uncommitted work alike.
docker exec "$NAME" git config --global --add safe.directory /app >/dev/null 2>&1
docker exec "$NAME" git -C /app add -A >> "$OUT/docker.log" 2>&1
docker exec "$NAME" git -C /app diff --cached "$TASK_BASE" > "$OUT/model.patch" 2>> "$OUT/docker.log"
docker cp "$NAME:/bench/graph.db" "$OUT/graph.db" >> "$OUT/docker.log" 2>&1

# Spend is read from the store's own usage ledger, not from the stream: the
# ledger is what the harness actually billed.
python3 - "$OUT/graph.db" "$OUT/cost.json" <<'PY'
import json, sqlite3, sys
out = {"cost_usd": 0.0, "prompt_tokens": 0, "completion_tokens": 0, "models": "", "calls": 0}
try:
    c = sqlite3.connect(sys.argv[1])
    r = c.execute("select coalesce(sum(cost),0), coalesce(sum(prompt_tokens),0), "
                  "coalesce(sum(completion_tokens),0), coalesce(group_concat(distinct model),''), "
                  "count(*) from usage").fetchone()
    out = {"cost_usd": r[0], "prompt_tokens": r[1], "completion_tokens": r[2],
           "models": r[3], "calls": r[4]}
except Exception as e:
    out["error"] = str(e)
json.dump(out, open(sys.argv[2], "w"), indent=2)
PY

# Which worker actually did the work. `aforge do` is meant to be measured on
# its own path: a run a specialist worker took over is measuring the specialist,
# not the harness, so it is recorded and flagged rather than quietly averaged
# in. nodes.subharness is the worker a node RAN on; splice_subharness is only
# the worker its children would have been spliced onto, which is an intention
# and not a fact.
python3 - "$OUT/graph.db" "$OUT/meta.json" <<'WORKERS'
import json, os, sqlite3, sys
db, metapath = sys.argv[1], sys.argv[2]
meta = json.load(open(metapath)) if os.path.exists(metapath) else {}
ran, planned = [], []
try:
    c = sqlite3.connect(db)
    ran = sorted({r[0] for r in c.execute(
        "select subharness from nodes where coalesce(subharness,'') != ''")})
    planned = sorted({r[0] for r in c.execute(
        "select splice_subharness from nodes where coalesce(splice_subharness,'') != ''")})
except Exception as err:
    meta["workers_error"] = str(err)
meta["workers_ran"] = ran
meta["workers_planned"] = planned
meta["void"] = "swe" in ran
json.dump(meta, open(metapath, "w"), indent=2)
WORKERS

PATCH_BYTES=$(wc -c < "$OUT/model.patch" | tr -d ' ')
meta "patch_bytes=$PATCH_BYTES" "stage=grade"
log "$TASK: patch is ${PATCH_BYTES} bytes"

cleanup; trap - EXIT

t1=$(date +%s)
if grade_patch "$OUT"; then
  meta "grade_seconds=$(( $(date +%s) - t1 ))" "stage=done"
else
  meta "grade_seconds=$(( $(date +%s) - t1 ))" "stage=grade-failed"
fi
python3 "$RIG_DIR/report.py" "$OUT"
