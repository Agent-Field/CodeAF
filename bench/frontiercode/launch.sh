#!/usr/bin/env bash
# Validate or explicitly launch one host shard of a FrontierCode campaign.
#
#   bench/frontiercode/launch.sh --check-local             gates that run on the Mac, no host, no launch
#   bench/frontiercode/launch.sh --check    <shard>        every gate but the launch, on the staged host
#   bench/frontiercode/launch.sh --execute  <shard>        run the shard's tasks as sequential waves
#   bench/frontiercode/launch.sh --replace  <shard> <task> the single preregistered re-run path
#
# One host per shard, one attempt per task, one pinned binary. A shard runs as
# sequential waves of at most `wave_capacity` concurrent containers, each wave
# its own iteration label; an existing label or an existing run directory is a
# refusal. `--replace` is the only re-run path: same gates, a different label,
# and the original attempt stays where it is.
#
# Gates: a priced model; the pinned binary's hash; the frozen population;
# native Linux/x86_64 (measured runs may not run under emulation); a clean,
# committed rig worktree; host memory at least wave_capacity times the
# per-container memory; and no active experiment container sharing the host.
set -euo pipefail
FC_SCRIPT=launch.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/gcp-lib.sh"

MODE="${1:-}"; SHARD="${2:-}"; REPLACE_TASK="${3:-}"
case "$MODE" in --check-local|--check|--execute|--replace) ;; *) sed -n '2,17p' "$0" >&2; exit 2 ;; esac
if [ "$MODE" != --check-local ]; then
  case "$SHARD" in ''|*[!0-9]*) fc_die "shard must be a number: $SHARD" ;; esac
fi
if [ "$MODE" = --replace ] && [ -z "$REPLACE_TASK" ]; then
  fc_die "--replace needs the task id: $0 --replace <shard> <task>"
fi

fc_load_manifest
fc_require_keys .campaign_name .label_prefix .model .arm .seed_id .preregistration \
  .host_shards .wave_capacity .per_container.cpus .per_container.memory_gb .max_cost_usd \
  .max_hours .hard_timeout_seconds .codeaf_commit .codeaf_sha256 .corpus .corpus_sha256 \
  .hypothesis .reasoning_effort

MODEL="$(fc_get '.model')"
PREFIX="$(fc_get '.label_prefix')"
ARM="$(fc_get '.arm')"
SEED="$(fc_get '.seed_id')"
PREREG="$(fc_get '.preregistration')"
WAVE_CAP="$(fc_get '.wave_capacity')"
MEMGB="$(fc_get '.per_container.memory_gb')"
READY=0

# ── gates that run anywhere ─────────────────────────────────────────────────
model_gate() { fc_model_priced; }

prereg_gate() {
  [ -f "$FC_REPO_ROOT/$PREREG" ] || fc_die "missing preregistration: $PREREG"
}

# The pinned binary: the Mac's copy for --check-local, the staged host's for
# every other mode.
local_bin_gate() {
  local bin="$1" want got
  [ -f "$bin" ] || fc_die "pinned binary is missing: $bin"
  want="$(fc_get '.codeaf_sha256')"
  got="$(fc_sha256 "$bin")"
  [ "$got" = "$want" ] || fc_die "binary $bin sha256 $got != manifest $want"
}

# No iteration label may already exist: a label is an attempt's identity, and
# a second attempt under the first's name is not a re-run.
label_collision_gate() {
  local shard="$1" lbl
  while IFS= read -r lbl; do
    [ -e "$FC_RIG_DIR/results/_iterations/$lbl/meta.txt" ] && fc_die "iteration already exists: $lbl"
  done < <(fc_wave_labels "$shard")
  if [ "$MODE" = --replace ]; then
    [ -e "$FC_RIG_DIR/results/_iterations/$PREFIX-shard$shard-replacement1/meta.txt" ] \
      && fc_die "replacement already exists: $PREFIX-shard$shard-replacement1"
  fi
}

if [ "$MODE" = --check-local ]; then
  model_gate
  prereg_gate
  local_bin_gate "$FC_REPO_ROOT/$(fc_get '.codeaf_local_binary')"
  for s in $(jq -r '.shard_files | keys[]?' "$FC_MANIFEST_PATH"); do label_collision_gate "$s"; done
  echo "ok  local: model priced, preregistration $PREREG present, binary matches $(fc_get '.codeaf_sha256' | cut -c1-12)..., no iteration label exists yet"
  echo "CHECK ONLY (local): no host contacted, no attempt launched"
  exit 0
fi

# ── gates that run on the staged host ───────────────────────────────────────
fc_host_arch_gate
model_gate
prereg_gate "$SHARD"
fc_frozen_check
local_bin_gate "$FC_RIG_DIR/bin/codeaf"

[ -z "$(git -C "$FC_REPO_ROOT" status --porcelain)" ] \
  || fc_die "measured launch requires a clean, committed rig worktree"

shard_rel="$(jq -r --arg s "$SHARD" '.shard_files[$s] // empty' "$FC_MANIFEST_PATH")"
[ -n "$shard_rel" ] || fc_die "manifest names no shard file for shard $SHARD"
mapfile -t tasks < <(fc_shard_tasks "$FC_REPO_ROOT/$shard_rel")
[ "${#tasks[@]}" -gt 0 ] || fc_die "shard $SHARD carries no tasks"
[ "${#tasks[@]}" -le "$WAVE_CAP" ] || true

# Every wave's containers must still find the host memory they may claim.
need_gb=$(( WAVE_CAP * MEMGB ))
have_gb="$(fc_host_mem_gb)"
[ "$have_gb" -ge "$need_gb" ] || fc_die "host has ${have_gb}G, a wave may claim ${need_gb}G"
label_collision_gate "$SHARD"

# The images the shard's tasks need must already be cached by the warm loop;
# building them inside a measured wave would spend the host's wall, not the
# agent's. This also proves no container is running yet.
source "$FC_RIG_DIR/lib.sh"
missing=""
for t in "${tasks[@]}"; do
  load_task "$t" || fc_die "no such task: $t"
  docker image inspect "$TASK_IMAGE" >/dev/null 2>&1 || missing="$missing $TASK_IMAGE"
  docker image inspect "$TASK_VERIFY_IMAGE" >/dev/null 2>&1 || missing="$missing $TASK_VERIFY_IMAGE"
done
[ -z "$missing" ] || fc_die "task image(s) not cached yet (is the warm loop still running?):$missing"

active="$(docker ps --format '{{.Names}}' | grep -E '^fc-' || true)"
[ -z "$active" ] || { echo "refusing to share host with active experiment containers:" >&2; echo "$active" >&2; exit 3; }

# The campaign's key must be on the host before a run; launch.sh sources it
# and passes it to run.sh, which passes it to the credential guard. The value
# is never printed.
if [ -f "$HOME/.codeaf-key" ]; then
  . "$HOME/.codeaf-key"
fi
[ -n "${OPENROUTER_API_KEY:-}" ] || fc_die "no model key on the host; install it with gcp-key.sh --install"

wave_count=$(( (${#tasks[@]} + WAVE_CAP - 1) / WAVE_CAP ))
if [ "$MODE" = --check ]; then
  echo "ok  shard=$SHARD tasks=${#tasks[@]} waves=$wave_count labels=$(fc_wave_labels "$SHARD" | tr '\n' ' ')"
  echo "    model=$MODEL arm=$ARM cap=\$$(fc_get '.max_cost_usd') hours=$(fc_get '.max_hours') timeout=$(fc_get '.hard_timeout_seconds')s"
  echo "CHECK ONLY: no attempt launched"
  exit 0
fi

# run.sh writes its result under the task, the arm, the model and the seed. The
# expected directory is derived here so a launch can refuse to overwrite an
# attempt that already exists -- an immutable record is the point.
slug="$(printf '%s' "$MODEL" | tr '/:' '--')"
run_dir_for() { printf '%s/results/%s-codeaf-senior-dev-%s-%s\n' "$FC_RIG_DIR" "$1" "$slug" "$2"; }

launch_one() { # <task> <seed>
  local task="$1" seed="$2" out
  out="$(run_dir_for "$task" "$seed")"
  [ ! -e "$out" ] || fc_die "refusing to overwrite an existing attempt: $out"
  CODEAF_BIN="$FC_RIG_DIR/bin/codeaf" MAX_COST="$(fc_get '.max_cost_usd')" MAX_HOURS="$(fc_get '.max_hours')" \
    VARIANT="$(fc_get '.reasoning_effort')" AGENT_SECONDS="$(fc_get '.hard_timeout_seconds')" \
    bash "$FC_RIG_DIR/run.sh" "$task" "$MODEL" "$seed"
}

write_iteration_meta() { # <iter dir> <shard> <wave> <label> <tasks...>
  local iter="$1" shard="$2" wave="$3" label="$4"; shift 4
  mkdir -p "$iter"
  {
    echo "label=$label"
    echo "campaign=$(fc_get '.campaign_name')"
    echo "shard=$shard"
    echo "wave=$wave"
    echo "wave_capacity=$WAVE_CAP"
    echo "arm=$ARM"
    echo "seed_id=$SEED"
    echo "model=$MODEL"
    echo "codeaf_commit=$(fc_get '.codeaf_commit')"
    echo "codeaf_sha256=$(fc_get '.codeaf_sha256')"
    echo "rig_commit=$(git -C "$FC_REPO_ROOT" rev-parse HEAD)"
    echo "prereg=$PREREG"
    echo "hypothesis=$(fc_get '.hypothesis')"
    echo "recovery_note=$(jq -c '.recovery // {}' "$FC_MANIFEST_PATH")"
    echo "tasks=$*"
    echo "started=$(date -Iseconds)"
  } > "$iter/meta.txt"
}

record_wave() { # <iter> <tasks...>
  local iter="$1"; shift
  local t out score exitf
  echo -e "task\trun_dir\tscore" > "$iter/scoreboard.tsv"
  for t in "$@"; do
    out="$(run_dir_for "$t" "$SEED")"
    score="$(python3 -c "import json;print(json.load(open('$out/logs/grade/grade.json')).get('score'))" 2>/dev/null || echo rig)"
    printf '%s\t%s\t%s\n' "$t" "${out#$FC_RIG_DIR/}" "$score" >> "$iter/scoreboard.tsv"
  done
}

if [ "$MODE" = --replace ]; then
  idx=-1
  for i in "${!tasks[@]}"; do [ "${tasks[$i]}" = "$REPLACE_TASK" ] && idx="$i"; done
  [ "$idx" -ge 0 ] || fc_die "$REPLACE_TASK is not in shard $SHARD"
  label="$PREFIX-shard$SHARD-replacement1"
  seed="$SEED-replacement1"
  iter="$FC_RIG_DIR/results/_iterations/$label"
  [ ! -e "$iter/meta.txt" ] || fc_die "replacement already exists: $label"
  write_iteration_meta "$iter" "$SHARD" 0 "$label" "$REPLACE_TASK"
  echo "launching $label: $REPLACE_TASK (original attempt preserved)"
  launch_one "$REPLACE_TASK" "$seed"
  record_wave "$iter" "$REPLACE_TASK"
  echo "replacement $label complete"
  exit 0
fi

for ((w = 0; w < wave_count; w++)); do
  start=$((w * WAVE_CAP))
  wave_tasks=("${tasks[@]:start:WAVE_CAP}")
  label="$PREFIX-shard$SHARD-wave$((w + 1))"
  iter="$FC_RIG_DIR/results/_iterations/$label"
  [ ! -e "$iter/meta.txt" ] || fc_die "iteration already exists: $label"
  active="$(docker ps --format '{{.Names}}' | grep -E '^fc-' || true)"
  [ -z "$active" ] || { echo "refusing to share host with active experiment containers:" >&2; echo "$active" >&2; exit 3; }
  write_iteration_meta "$iter" "$SHARD" "$((w + 1))" "$label" "${wave_tasks[@]}"
  echo "launching $label ($(date -u +%H:%M:%SZ)): ${wave_tasks[*]}"
  pids=()
  for t in "${wave_tasks[@]}"; do
    launch_one "$t" "$SEED" > "$iter/$t.runner.log" 2>&1 &
    pids+=($!)
    sleep 3
  done
  for i in "${!wave_tasks[@]}"; do
    if wait "${pids[$i]}"; then
      echo "[$label] ${wave_tasks[$i]} runner exited 0"
    else
      echo "[$label] ${wave_tasks[$i]} runner exited non-zero (its experiment record says why)" >&2
    fi
  done
  echo "finished=$(date -Iseconds)" >> "$iter/meta.txt"
  record_wave "$iter" "${wave_tasks[@]}"
done
echo "shard $SHARD complete ($(date -u +%H:%M:%SZ))"
