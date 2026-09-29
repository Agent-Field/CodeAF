#!/usr/bin/env bash
# Shared plumbing for the FrontierCode rig: task lookup, image builds, the
# emulation guard and the key. Sourced by run.sh, gold.sh, negative.sh and the
# grade drivers, which differ only in who writes the patch being graded — the
# harness under test, a control patch, or a sealed fixture.
#
# The rig reuses bench/deepswe's lessons (the emulation guard, the pull-retry,
# the platform pinning) and its own task images are built for the HOST's
# architecture by default: on this arm64 workstation a native arm64 image
# needs no emulation and grades the same tree the agent worked in. The
# PLATFORM override keeps the amd64 path for cloud amd64 hosts.
set -uo pipefail

RIG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TASKS="${TASKS:-$RIG_DIR/tasks}"
RESULTS="${RESULTS:-$RIG_DIR/results}"

# The platform the task images run as. Default: the host's own — images are
# built, not pulled, so they can match. bench/deepswe pins linux/amd64 because
# its corpus publishes amd64-only images; this rig builds its own.
case "$(uname -m)" in
  x86_64|amd64) DEFAULT_PLATFORM=linux/amd64 ;;
  *) DEFAULT_PLATFORM=linux/arm64 ;;
esac
PLATFORM="${PLATFORM:-$DEFAULT_PLATFORM}"

# Emulation guard, carried over from bench/deepswe/lib.sh unchanged in effect:
# an amd64 image on an arm64 host runs under qemu, where a multi-threaded Go
# program dies with "runtime: lfstack.push invalid packing" — Go's lock-free
# stack packs a pointer into 48 bits and sign-extends it back, so it needs
# every address below 2^47, and this host's kernel hands qemu mmap results
# above that line. The crash is raised from the garbage collector, so a
# process that never collects never reaches it; GOGC=off with a GOMEMLIMIT
# backstop is the fix, and it changes nothing a test suite asserts. This rig's
# own native-arch images never take this path; it is here for amd64 images.
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

# coreutils' timeout is `gtimeout` on a Mac that installed it with brew, and
# this host has neither: the shim that answers instead is a python wrapper
# with the same exit code (124 on the wall), so callers and logs read the same.
if ! command -v timeout >/dev/null 2>&1 && command -v gtimeout >/dev/null 2>&1; then
  timeout() { gtimeout "$@"; }
elif ! command -v timeout >/dev/null 2>&1; then
  timeout() { python3 "$RIG_DIR/grade/checks/timeout.py" "$@"; }
fi

# Section-aware scan of the task.toml schema, as bench/deepswe/lib.sh does.
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
  TASK_DIR="$TASKS/$TASK_ID"
  [ -f "$TASK_DIR/task.toml" ] || { log "no such task: $TASK_ID (looked in $TASKS)"; return 1; }
  TASK_IMAGE="$(toml_get "$TASK_DIR/task.toml" environment.docker_image)"
  TASK_VERIFY_IMAGE="$(toml_get "$TASK_DIR/task.toml" verifier.docker_image)"
  TASK_BASE="$(toml_get "$TASK_DIR/task.toml" metadata.base_commit_hash)"
  TASK_REFERENCE="$(toml_get "$TASK_DIR/task.toml" metadata.reference_commit_hash)"
  TASK_REPO_URL="$(toml_get "$TASK_DIR/task.toml" metadata.repository_url)"
  TASK_LANG="$(toml_get "$TASK_DIR/task.toml" metadata.language)"
  TASK_CPUS="${CPUS:-$(toml_get "$TASK_DIR/task.toml" environment.cpus)}"
  TASK_MEM="${MEMORY_MB:-$(toml_get "$TASK_DIR/task.toml" environment.memory_mb)}"
  TASK_SECS="${AGENT_SECONDS:-$(toml_get "$TASK_DIR/task.toml" agent.timeout_sec)}"
  TASK_VSECS="$(toml_get "$TASK_DIR/task.toml" verifier.timeout_sec)"
  TASK_CPUS="${TASK_CPUS:-4}"; TASK_MEM="${TASK_MEM:-8192}"
  TASK_SECS="${TASK_SECS%%.*}"; TASK_SECS="${TASK_SECS:-7200}"
  TASK_VSECS="${TASK_VSECS%%.*}"; TASK_VSECS="${TASK_VSECS:-5400}"
  [ -n "$TASK_IMAGE" ] || { log "$TASK_ID: task.toml names no docker image"; return 1; }
}

# The task's environment image is built, never pulled: the Dockerfile clones
# the task's own repository at the pinned base commit at build time, so the
# rig commits no upstream source bytes and the image is reproducible from the
# committed files alone. An image already on the host is trusted; FORCE_IMAGE=1
# rebuilds.
ensure_env_image() {
  mkdir -p "$RESULTS"
  if docker image inspect "$TASK_IMAGE" >/dev/null 2>&1 && [ "${FORCE_IMAGE:-0}" != 1 ]; then
    log "$TASK_ID: environment image already present"
    return 0
  fi
  log "$TASK_ID: building environment image $TASK_IMAGE ($PLATFORM)"
  docker build --platform "$PLATFORM" \
    --build-arg BASE_COMMIT="$TASK_BASE" \
    --build-arg REFERENCE_COMMIT="$TASK_REFERENCE" \
    -t "$TASK_IMAGE" "$TASK_DIR/environment" > "$RESULTS/.build-$TASK_ID-env.log" 2>&1
}

ensure_verify_image() {
  mkdir -p "$RESULTS"
  if docker image inspect "$TASK_VERIFY_IMAGE" >/dev/null 2>&1 && [ "${FORCE_IMAGE:-0}" != 1 ]; then
    return 0
  fi
  log "$TASK_ID: building verifier image $TASK_VERIFY_IMAGE ($PLATFORM)"
  docker build --platform "$PLATFORM" \
    --build-arg BASE_IMAGE="$TASK_IMAGE" \
    --build-arg BASE_COMMIT="$TASK_BASE" \
    --build-arg REFERENCE_COMMIT="$TASK_REFERENCE" \
    -t "$TASK_VERIFY_IMAGE" "$TASK_DIR/tests" > "$RESULTS/.build-$TASK_ID-verify.log" 2>&1
}

# The provider key, read the way this machine keeps it: the environment, then
# the macOS Keychain service the DeepSWE rigs use. It is read only — never
# echoed, never written anywhere but the one process that holds it.
load_key() {
  KEY="${API_KEY:-${OPENROUTER_API_KEY:-}}"
  if [ -z "$KEY" ] && command -v security >/dev/null 2>&1; then
    KEY="$(security find-generic-password -a "$USER" -s delta-openrouter -w 2>/dev/null || true)"
  fi
  if [ -z "$KEY" ] && [ -f "$HOME/.config/openrouter/key" ]; then
    KEY="$(tr -d '[:space:]' < "$HOME/.config/openrouter/key")"
  fi
  [ -n "$KEY" ]
}

# scrub_key replaces the api_key in a copied profile config, for results that
# are kept for years.
scrub_profile() { # <path>
  python3 - "$1" <<'PY' 2>/dev/null
import json, sys
try:
    p = sys.argv[1]; d = json.load(open(p)); d["api_key"] = "<scrubbed>"; json.dump(d, open(p, "w"), indent=1)
except Exception:
    pass
PY
  return 0
}