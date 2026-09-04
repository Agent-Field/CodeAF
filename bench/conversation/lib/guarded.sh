#!/usr/bin/env bash
# guarded.sh — start the forwarding guard, and wire an arm through it.
#
# The guard (lib/guard.py) holds the real key; a harness gets a sentinel and a
# base URL on loopback. An arm is live-runnable here only if every call it makes
# can be pointed at that base URL through a mechanism the CLI actually
# documents or implements. An arm that cannot is unsupported, with the reason
# recorded — not run in the hope that its flags hold.

GUARD_PORT=""
GUARD_URL=""
GUARD_SENTINEL=""
GUARD_PID=""
GUARD_AUDIT=""
GUARD_USAGE=""

# guard_start brings the proxy up and waits for the port it chose. The real key
# is handed to the guard's own environment and to nothing else.
#
# ONE GUARD PER CELL. The guard is also the run's meter, and a meter shared
# between cells cannot say which cell a late call belonged to: a session host
# that keeps working after a cell ends would smear its spend onto the next one.
# A cell's own guard closes with the cell, so a straggler reaches a closed port
# and is visible as a failure rather than counted as somebody else's cost.
#
#   guard_start <audit-path> <usage-path> <scope>
guard_start() {
  local audit="$1" usage="${2:-}" scope="${3:-}" out
  GUARD_USAGE="$usage"
  GUARD_AUDIT="$audit"
  GUARD_SENTINEL="afconv-sentinel-$$-$RANDOM"
  out="$(mktemp)"
  local -a allow=()
  local entry
  for entry in $CONV_ALLOWLIST; do allow+=(--allow "$entry"); done

  GUARD_UPSTREAM_KEY="${OPENROUTER_API_KEY:-}" \
    python3 "$CONV_LIB/guard.py" "${allow[@]}" --audit "$audit" \
      ${usage:+--usage "$usage"} ${scope:+--scope "$scope"} \
      --sentinel "$GUARD_SENTINEL" > "$out" 2>>"$audit.err" &
  GUARD_PID=$!

  local waited=0
  while [ "$waited" -lt 100 ]; do
    GUARD_PORT="$(awk '/^PORT /{print $2; exit}' "$out" 2>/dev/null)"
    [ -n "$GUARD_PORT" ] && break
    kill -0 "$GUARD_PID" 2>/dev/null || break
    sleep 0.1
    waited=$((waited + 1))
  done
  rm -f "$out"
  if [ -z "$GUARD_PORT" ]; then
    conv_warn "the guard did not come up — see $audit.err"
    return 1
  fi
  GUARD_URL="http://127.0.0.1:$GUARD_PORT/v1"
  return 0
}

# guard_check_key asks the upstream whether the key it was given is live. This
# costs nothing (no model call) and turns a stale key into one clear refusal
# instead of a grid of cells that ran, billed nothing and read as failures.
guard_check_key() {
  local status
  status="$(CONV_KEY="${OPENROUTER_API_KEY:-}" python3 -c '
import os, urllib.error, urllib.request
request = urllib.request.Request("https://openrouter.ai/api/v1/key",
                                 headers={"Authorization": "Bearer " + os.environ["CONV_KEY"]})
try:
    with urllib.request.urlopen(request, timeout=20) as response:
        print(response.status)
except urllib.error.HTTPError as error:
    print(error.code)
except Exception:
    print("unreachable")
')"
  if [ "$status" = "200" ]; then
    return 0
  fi
  conv_warn "the upstream rejected this shell's OPENROUTER_API_KEY (HTTP $status)."
  conv_warn "Nothing was spent. The guard needs a live key in the environment;"
  conv_warn "this suite deliberately does not read credentials out of a CLI's own store."
  return 1
}

guard_stop() {
  [ -n "$GUARD_PID" ] || return 0
  kill "$GUARD_PID" 2>/dev/null
  wait "$GUARD_PID" 2>/dev/null
  GUARD_PID=""
}

# arm_guard_wire points one arm at the guard, writing whatever provider config
# that CLI reads. It appends to ARM_ENV, so it must run after arm_isolate.
#
#   aforge  AFORGE_BASE_URL (internal/config/config.go: firstNonEmpty of it and
#           the default) — an env var this repository implements.
#   pi      $PI_CODING_AGENT_DIR/models.json, the custom-provider file
#           core/model-runtime.js loads from the agent dir.
#   omp     <profile>/agent/models.yml, omp's custom-provider file. Written
#           here and then VALIDATED by asking omp's own catalog whether the
#           provider took effect; an arm whose config did not load is
#           unsupported rather than run.
#   opencode  not wired: no custom-provider mechanism was verified for it.
ARM_GUARD=""
ARM_GUARD_NOTE=""
arm_guard_wire() {
  local arm="$1" cell="$2"
  ARM_GUARD="no"; ARM_GUARD_NOTE=""
  [ -n "$GUARD_URL" ] || { ARM_GUARD_NOTE="no guard is running"; return 1; }

  case "$arm" in
    aforge)
      ARM_ENV+=("AFORGE_BASE_URL=$GUARD_URL" "OPENROUTER_API_KEY=$GUARD_SENTINEL")
      ARM_GUARD="yes"
      ARM_GUARD_NOTE="AFORGE_BASE_URL to the guard; the real key is not in this process"
      return 0
      ;;
    pi)
      local home="$ARM_STATE_DIR/pi-home"
      mkdir -p "$home"
      CONV_URL="$GUARD_URL" CONV_KEY="$GUARD_SENTINEL" CONV_ID="$CONV_MODEL" \
        python3 -c '
import json, os, sys
json.dump({"providers": {"guard": {
    "name": "guard",
    "baseUrl": os.environ["CONV_URL"],
    "apiKey": os.environ["CONV_KEY"],
    "api": "openai-completions",
    "models": [{"id": os.environ["CONV_ID"], "reasoning": True}],
}}}, open(sys.argv[1], "w"), indent=1)
' "$home/models.json"
      ARM_ENV+=("OPENROUTER_API_KEY=$GUARD_SENTINEL")
      ARM_GUARD="yes"
      ARM_GUARD_NOTE="models.json custom provider in the cell's agent dir"
      return 0
      ;;
    omp)
      local agent="$HOME/.omp/profiles/${CONV_OMP_PROFILE_ACTIVE:-}/agent"
      [ -n "${CONV_OMP_PROFILE_ACTIVE:-}" ] && [ -d "$agent" ] || {
        ARM_GUARD_NOTE="no profile of this run's own to write models.yml into"
        return 1
      }
      cat > "$agent/models.yml" <<YAML
providers:
  guard:
    baseUrl: $GUARD_URL
    apiKey: $GUARD_SENTINEL
    api: openai-completions
    models:
      - id: $CONV_MODEL
        reasoning: true
YAML
      # Written is not loaded. omp disables custom providers wholesale when
      # models.yml fails validation ("custom providers disabled"), so its own
      # catalog is asked whether the provider exists before anything is spent.
      if ! "${CHILD_ENV[@]}" OMP_PROFILE="$CONV_OMP_PROFILE_ACTIVE" \
            "$OMP_BIN" models find "${CONV_MODEL##*/}" --json 2>/dev/null |
            CONV_WANT="guard/$CONV_MODEL" python3 -c '
import json, os, sys
want = os.environ["CONV_WANT"]
try:
    blob = json.load(sys.stdin)
except Exception:
    sys.exit(1)
for model in (blob.get("models") if isinstance(blob, dict) else blob) or []:
    if not isinstance(model, dict):
        continue
    selector = model.get("selector") or "/".join(
        x for x in (model.get("provider"), model.get("id")) if x)
    if selector == want:
        sys.exit(0)
sys.exit(1)
'; then
        ARM_GUARD_NOTE="omp did not load the models.yml guard provider"
        return 1
      fi
      ARM_ENV+=("OPENROUTER_API_KEY=$GUARD_SENTINEL")
      ARM_GUARD="yes"
      ARM_GUARD_NOTE="models.yml custom provider, verified present in omp's catalog"
      return 0
      ;;
    *)
      ARM_GUARD_NOTE="no verified custom-provider mechanism for $arm"
      return 1
      ;;
  esac
}

# arm_guard_model is the model string an arm is given once it is wired: the
# guard is a provider of its own to the peers, and unchanged to aforge.
arm_guard_model() {
  case "$1" in
    omp) printf 'guard/%s' "$CONV_MODEL" ;;
    pi)  printf '%s' "$CONV_MODEL" ;;
    *)   printf '%s' "$CONV_MODEL" ;;
  esac
}

# arm_host_stop ends whatever this cell's own conversation left running, using
# the product's own door and the cell's own state root.
#
# aforge conversations are hosted by default, so a cell that just detaches from
# tmux leaves a session host behind — a benchmark must not litter the machine
# with daemons, and it must not stop anybody else's either. `aforge engine
# --stop` is scoped to one workspace, and the cell's AFORGE_HOME scopes it
# again, so this can only reach the host this cell started. Nothing is killed
# by pattern and no global process is touched.
#
# Order matters: the host is stopped BEFORE the cell's guard, so a session that
# is still finishing cannot outlive the thing that keeps it on the allowlist.
arm_host_stop() {
  local arm="$1" cell="$2" work="$3"
  [ "$arm" = "aforge" ] || return 0
  local bin; bin="$(arm_bin aforge)"
  [ -n "$bin" ] || return 0
  [ -d "$cell/state/aforge-home" ] || return 0
  "${CHILD_ENV[@]}" "$bin" engine --workspace "$work" --stop \
    > "$cell/host-stop.log" 2>&1
  printf 'exit:%s\n' "$?" >> "$cell/host-stop.log"
}
