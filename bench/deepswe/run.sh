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

# --- snapshot the rig, then run from the copy -------------------------------
# Bash reads a script by byte offset as it goes, so editing the rig while a run
# is in flight makes the running shell resume in the middle of the new text.
# Two ninety-minute runs were lost to exactly that: one resumed mid-heredoc
# (`line 145: is: command not found`), never extracted its patch and never
# graded; the other lost its container to a cleanup trap that fired out of
# sequence. So every run copies the rig into its own result directory before it
# does anything else — before lib.sh is even sourced, because a sourced file is
# read the same incremental way — and re-execs from that copy, which nothing
# later edits.
__RIG_SRC="$(cd "$(dirname "$0")" && pwd)"
if [ -z "${BENCH_SNAPSHOT:-}" ]; then
  [ $# -eq 3 ] || { echo "usage: run.sh <task-id> <model-id> <seed-tag>" >&2; exit 2; }
  __slug="$(printf '%s' "$2" | tr '/:' '--')"
  export RESULTS="${RESULTS:-$__RIG_SRC/results}"
  __out="$RESULTS/$1-$__slug-$3"
  rm -rf "$__out"; mkdir -p "$__out/rig"
  cp "$__RIG_SRC/run.sh" "$__RIG_SRC/lib.sh" "$__RIG_SRC/report.py" "$__out/rig/"
  export BENCH_SNAPSHOT="$__out/rig"
  exec bash "$__out/rig/run.sh" "$@"
fi

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
mkdir -p "$OUT"   # the snapshot step above already cleared it
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
# An emulated process that crashes leaves a core dump in the working directory,
# and `git add -A` stages it like any other new file. ink s3 submitted a 
# qemu core dump of node as part of its answer. It is the emulator's droppings,
# not the run's work, so it never reaches the diff.
docker exec "$NAME" sh -c 'cd /app && rm -f qemu_*.core core.[0-9]* 2>/dev/null; true' >> "$OUT/docker.log" 2>&1
# The graded diff has to be everything the run changed relative to the task's
# base commit, and "the index" is not that. A run that commits its work to a
# branch and leaves the worktree back on the default branch has an index that
# matches base, and the old `git add -A && git diff --cached <base>` graded it
# as though it had written nothing — happy-dom s7 spent its whole 90-minute wall
# and submitted 0 bytes that way.
#
# So every place the work could be is measured and the richest one wins: the
# worktree (with untracked files staged, which is what `add -A` is for), and
# each local branch that moved off base. They are candidates rather than a union
# because the grader applies ONE patch and two overlapping patches do not apply.
# Every candidate's size is written into meta.json, so the choice is auditable
# and a run whose work was split across two of them is visible rather than
# silently halved.
#
# --binary throughout: a run that writes a .joblib, a fixture image or any other
# non-text file otherwise produces "Binary files a/x and b/x differ", which
# `git apply` refuses with "cannot apply binary patch without full index line",
# and the whole patch is rejected for a rig reason. The corpus's own collect
# command in task.toml uses --binary for exactly this.
docker exec -i "$NAME" tee /bench/collect.sh > /dev/null <<'COLLECT'
#!/bin/sh
set -u
base="$1"; out=/bench/candidates
rm -rf "$out"; mkdir -p "$out"
cd /app || exit 1
git add -A >/dev/null 2>&1
git diff --cached --binary "$base" > "$out/worktree.patch" 2>/dev/null
for ref in $(git for-each-ref --format='%(refname:short)' refs/heads 2>/dev/null); do
  safe=$(printf '%s' "$ref" | tr '/' '_')
  git diff --binary "$base" "$ref" > "$out/branch-$safe.patch" 2>/dev/null
done
# HEAD too, which covers a detached checkout no branch points at.
git diff --binary "$base" HEAD > "$out/head.patch" 2>/dev/null

# Directories a tsconfig declares as build output. This is what makes
# lib/*.js + lib/*.d.ts + lib/*.map generated on a project that compiles there,
# while leaving lib/ alone on a project that keeps hand-written source in it.
outdirs=$(find . -maxdepth 3 -name 'tsconfig*.json' -not -path '*/node_modules/*' 2>/dev/null \
  | xargs grep -ho '"outDir"[[:space:]]*:[[:space:]]*"[^"]*"' 2>/dev/null \
  | sed 's/.*:[[:space:]]*"//; s/"$//; s|^\./||; s|/$||' | sort -u)

# A candidate's size is measured over SOURCE only. The chosen patch is still the
# whole diff -- the grader applies it entire -- but generated bytes must not be
# what wins the choice, or a build-output diff beats a clean source one.
for f in "$out"/*.patch; do
  [ -f "$f" ] || continue
  awk '
    /^diff --git / { if (p != "") sizes[p] += n; p = $3; sub(/^a\//, "", p); n = 0 }
    { n += length($0) + 1 }
    END { if (p != "") sizes[p] += n; for (k in sizes) printf "%s\t%d\n", k, sizes[k] }
  ' "$f" > "$out/.files"
  # --no-index so a path the repo tracks is still reported when .gitignore names it.
  cut -f1 "$out/.files" | git check-ignore --no-index --stdin 2>/dev/null | sort -u > "$out/.ignored"
  src=0; gen=0
  while IFS='	' read -r path bytes; do
    [ -n "$path" ] || continue
    g=0
    grep -qxF "$path" "$out/.ignored" 2>/dev/null && g=1
    case "$path" in
      dist/*|build/*|coverage/*|__pycache__/*) g=1 ;;
      */dist/*|*/build/*|*/coverage/*|*/__pycache__/*) g=1 ;;
      *.map|*.pyc) g=1 ;;
    esac
    if [ "$g" = 0 ] && [ -n "$outdirs" ]; then
      for d in $outdirs; do
        case "$path" in "$d"/*|*/"$d"/*) g=1; break ;; esac
      done
    fi
    if [ "$g" = 1 ]; then gen=$((gen + bytes)); else src=$((src + bytes)); fi
  done < "$out/.files"
  printf '%s\t%s\t%s\t%s\n' "$(wc -c < "$f" | tr -d ' ')" "$src" "$gen" "$(basename "$f")"
done
rm -f "$out/.files" "$out/.ignored"
COLLECT
docker exec "$NAME" chmod +x /bench/collect.sh >> "$OUT/docker.log" 2>&1
docker exec "$NAME" /bench/collect.sh "$TASK_BASE" > "$OUT/candidates.tsv" 2>> "$OUT/docker.log"

# Richest by SOURCE bytes, total only as the tie-break.
BEST=$(sort -t"$(printf '\t')" -k2,2nr -k1,1nr "$OUT/candidates.tsv" 2>/dev/null | head -1 | cut -f4)
BEST="${BEST:-worktree.patch}"
docker exec "$NAME" cat "/bench/candidates/$BEST" > "$OUT/model.patch" 2>> "$OUT/docker.log"
log "$TASK: graded diff taken from $BEST"
python3 - "$OUT/meta.json" "$OUT/candidates.tsv" "$BEST" <<'CANDS'
import json, os, sys
metapath, tsv, best = sys.argv[1], sys.argv[2], sys.argv[3]
meta = json.load(open(metapath)) if os.path.exists(metapath) else {}
rows = {}
if os.path.exists(tsv):
    for line in open(tsv):
        parts = line.rstrip("\n").split("\t")
        if len(parts) == 4 and parts[0].isdigit():
            rows[parts[3]] = {"total_bytes": int(parts[0]),
                              "source_bytes": int(parts[1]),
                              "generated_bytes": int(parts[2])}
meta["patch_candidates"] = rows
meta["patch_source"] = best
chosen = rows.get(best, {})
meta["patch_source_bytes"] = chosen.get("source_bytes")
meta["patch_generated_bytes"] = chosen.get("generated_bytes")
warn = [n for n, r in rows.items() if r["generated_bytes"] > r["source_bytes"]]
meta["patch_generated_warning"] = warn
if best in warn:
    print("WARNING: chosen patch %s is mostly generated output "
          "(%d generated vs %d source bytes)"
          % (best, chosen.get("generated_bytes", 0), chosen.get("source_bytes", 0)),
          file=sys.stderr)
json.dump(meta, open(metapath, "w"), indent=2)
CANDS
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
# in.
#
# THE COLUMN IS nodes.ran, AND IT IS THE ONLY ONE THAT ANSWERS THIS QUESTION.
# nodes.subharness is what the compiler ASKED for and is blank on the great
# majority of nodes, because the compiler routes almost nothing — every node of
# the s9 ink and igel stores read blank, which is why that sweep's autopsy took
# a day. splice_subharness is what the subtree was admitted under, an intention
# and not a fact. nodes.ran is written by the dispatch path at the moment it
# builds the worker, and it names the generalist out loud.
python3 - "$OUT/graph.db" "$OUT/meta.json" <<'WORKERS'
import json, os, sqlite3, sys
db, metapath = sys.argv[1], sys.argv[2]
meta = json.load(open(metapath)) if os.path.exists(metapath) else {}
ran, asked, planned = [], [], []
try:
    c = sqlite3.connect(db)
    columns = {r[1] for r in c.execute("pragma table_info(nodes)")}
    if "ran" in columns:
        ran = sorted({r[0] for r in c.execute(
            "select ran from nodes where coalesce(ran,'') != ''")})
    else:
        # A store written by a binary from before the column existed. The ask is
        # all it has, and it is recorded as the ask rather than passed off as
        # the fact — an absence is never a diagnosis.
        meta["workers_from_ask"] = True
        ran = sorted({r[0] for r in c.execute(
            "select subharness from nodes where coalesce(subharness,'') != ''")})
    asked = sorted({r[0] for r in c.execute(
        "select subharness from nodes where coalesce(subharness,'') != ''")})
    planned = sorted({r[0] for r in c.execute(
        "select splice_subharness from nodes where coalesce(splice_subharness,'') != ''")})
except Exception as err:
    meta["workers_error"] = str(err)
meta["workers_ran"] = ran
meta["workers_asked"] = asked
meta["workers_planned"] = planned
meta["void"] = "swe" in ran
json.dump(meta, open(metapath, "w"), indent=2)
WORKERS

# Liveness evidence, counted off the stream the run just wrote: how often a
# call was cut and retried, and whether a leaf that restarted came back to
# banked work rather than a blank page. Recorded rather than judged — the log
# lines are quoted into meta.json so the claim can be checked.
python3 - "$OUT/run.log" "$OUT/meta.json" "$OUT/graph.db" <<'LIVENESS'
import json, os, re, sqlite3, sys
logpath, metapath, db = sys.argv[1], sys.argv[2], sys.argv[3]
meta = json.load(open(metapath)) if os.path.exists(metapath) else {}
lines = open(logpath, errors="replace").read().split("\n") if os.path.exists(logpath) else []
faults = [l.strip() for l in lines if l.lstrip().startswith("\u2717") and "retr" in l.lower()]
resumed = [l.strip() for l in lines if re.search(r"resum|banked|picked up where", l, re.I)]
starts = {}
try:
    c = sqlite3.connect(db)
    for (n,) in c.execute("select node_id from events where kind='node_started'"):
        starts[n] = starts.get(n, 0) + 1
except Exception:
    pass
meta["fault_retries"] = len(faults)
meta["fault_retry_lines"] = faults[:12]
meta["restarted_nodes"] = {n: k for n, k in starts.items() if k > 1}
meta["resumed_with_transcript"] = bool(resumed)
meta["resume_lines"] = resumed[:8]
json.dump(meta, open(metapath, "w"), indent=2)
LIVENESS

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
