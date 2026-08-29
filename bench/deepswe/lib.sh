#!/usr/bin/env bash
# Shared plumbing for the DeepSWE rig: corpus lookup, the task.toml scan and the
# grade step. Sourced by run.sh and gold.sh, which differ only in who writes
# model.patch — the harness under test, or the task's own reference solution.
set -uo pipefail

RIG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORPUS="${CORPUS:-$HOME/src/swe-pro/tools/deepswe-bench/tasks}"
RESULTS="${RESULTS:-$RIG_DIR/results}"

# The task images are published for linux/amd64 only. On an arm64 host every
# docker call therefore has to name the platform explicitly and the host needs
# the qemu-x86_64 binfmt handler registered:
#   docker run --privileged --rm tonistiigi/binfmt --install amd64
PLATFORM="${PLATFORM:-linux/amd64}"

# Emulation guard. The task images are amd64 only, so on an arm64 host every
# container runs under qemu-x86_64 — and there a multi-threaded Go program dies
# with "runtime: lfstack.push invalid packing". Go's lock-free stack packs a
# pointer into 48 bits and sign-extends it back, so it requires every address to
# sit below 2^47; this host's kernel hands qemu mmap results above that line.
# The crash is raised from the garbage collector (gcStart -> finishsweep_m ->
# spanSet.reset), so a process that never collects never reaches it. esbuild is
# a Go binary and sits under vitest, which is how a whole TypeScript suite came
# back as "missing from report (test did not run)" and graded a clean 0 against
# the task's OWN reference solution.
#
# GOGC=off with a GOMEMLIMIT backstop is the fix: collection is disabled until
# the heap approaches the container's own memory cap, which for a test suite or
# a bundler never happens, and if it ever did the container was going to be
# killed anyway. It changes nothing a suite asserts. GOMAXPROCS=1 alone was
# tried first and only lowered the odds — ofetch's larger suite still crashed.
# Measured on the ofetch reference solution: GOMAXPROCS=1 scored 0 twice,
# GOGC=off scored 1 three times out of three.
case "$(uname -m)" in
  x86_64|amd64) EMULATED=0 ;;
  *) case "$PLATFORM" in *amd64*) EMULATED=1 ;; *) EMULATED=0 ;; esac ;;
esac
emu_args() { # echoes the docker env flags for the emulation guard
  EMU_ARGS=()
  [ "$EMULATED" = 1 ] || return 0
  EMU_ARGS=(-e "GOGC=${EMU_GOGC:-off}" -e "GOMEMLIMIT=$(( TASK_MEM * 3 / 4 ))MiB")
}

log() { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }

# Section-aware scan of the fixed task.toml schema. Keys come back as
# "section.key": timeout_sec appears under both [agent] and [verifier], and
# cpus/memory_mb under both [environment] and [verifier.environment].
toml_get() { # <file> <section.key>
  python3 - "$1" "$2" <<'PY'
import re, sys
raw = open(sys.argv[1], errors="replace").read()
want = sys.argv[2]
section = ""
for line in raw.split("\n"):
    t = line.strip()
    if not t or t.startswith("#"):
        continue
    if t.startswith("[") and t.endswith("]"):
        section = t[1:-1]
        continue
    if "=" not in t:
        continue
    k, _, v = t.partition("=")
    v = v.strip()
    if len(v) >= 2 and v[0] == '"' and v[-1] == '"':
        v = v[1:-1]
    if f"{section}.{k.strip()}" == want:
        print(v)
        break
PY
}

load_task() { # <task-id> -> exports TASK_*
  TASK_ID="$1"
  TASK_DIR="$CORPUS/$TASK_ID"
  [ -f "$TASK_DIR/task.toml" ] || { log "no such task: $TASK_ID (looked in $CORPUS)"; return 1; }
  TASK_IMAGE="$(toml_get "$TASK_DIR/task.toml" environment.docker_image)"
  TASK_BASE="$(toml_get "$TASK_DIR/task.toml" metadata.base_commit_hash)"
  TASK_LANG="$(toml_get "$TASK_DIR/task.toml" metadata.language)"
  TASK_CPUS="${CPUS:-$(toml_get "$TASK_DIR/task.toml" environment.cpus)}"
  TASK_MEM="${MEMORY_MB:-$(toml_get "$TASK_DIR/task.toml" environment.memory_mb)}"
  TASK_SECS="${AGENT_SECONDS:-$(toml_get "$TASK_DIR/task.toml" agent.timeout_sec)}"
  TASK_VSECS="$(toml_get "$TASK_DIR/task.toml" verifier.timeout_sec)"
  TASK_CPUS="${TASK_CPUS:-2}"; TASK_MEM="${TASK_MEM:-8192}"
  TASK_SECS="${TASK_SECS%%.*}"; TASK_SECS="${TASK_SECS:-5400}"
  TASK_VSECS="${TASK_VSECS%%.*}"; TASK_VSECS="${TASK_VSECS:-1800}"
  [ -n "$TASK_IMAGE" ] || { log "$TASK_ID: task.toml names no docker image"; return 1; }
}

# Build the verifier image (the pinned task image plus the hidden tests) and run
# it over whatever model.patch is sitting in the result directory. The verifier
# gets no network, so a misbehaving suite cannot reach out mid-grade.
grade_patch() { # <result-dir>
  local out="$1" tag="deepswe-verify:$TASK_ID"
  [ -s "$out/model.patch" ] || { log "$TASK_ID: no model.patch to grade"; return 1; }

  log "$TASK_ID: building verifier image"
  if ! docker build --platform "$PLATFORM" -t "$tag" "$TASK_DIR/tests" > "$out/verifier-build.log" 2>&1; then
    log "$TASK_ID: verifier build failed — see $out/verifier-build.log"
    return 1
  fi

  # grader.py reads $ARTIFACTS_DIR/model.patch, default /logs/artifacts.
  rm -rf "$out/logs"; mkdir -p "$out/logs/artifacts" "$out/logs/verifier"
  cp "$out/model.patch" "$out/logs/artifacts/model.patch"

  emu_args
  log "$TASK_ID: grading (limit ${TASK_VSECS}s)"
  timeout "$((TASK_VSECS + 120))" docker run --rm --platform "$PLATFORM" --network none \
    --cpus "$TASK_CPUS" --memory "${TASK_MEM}m" "${EMU_ARGS[@]}" \
    -v "$out/logs:/logs" "$tag" /tests/test.sh > "$out/verifier.log" 2>&1
  local code=$?

  if [ -f "$out/logs/verifier/reward.json" ]; then
    cp "$out/logs/verifier/reward.json" "$out/reward.json"
    return 0
  fi
  # test.sh's EXIT trap writes reward.txt=-1 when it crashes before producing
  # reward.json. That sentinel is an infrastructure failure, NOT a score of
  # zero, and must never be recorded as a legitimate 0.
  if [ "$(cat "$out/logs/verifier/reward.txt" 2>/dev/null | tr -d '[:space:]')" = "-1" ]; then
    log "$TASK_ID: verifier crashed (reward.txt sentinel -1) — see $out/verifier.log"
  else
    log "$TASK_ID: verifier wrote no reward.json (exit $code) — see $out/verifier.log"
  fi
  return 1
}
