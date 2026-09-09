#!/usr/bin/env bash
# Run a guarded conversation inside a corpus image, using the existing terminal
# adapters. The grader and its tests stay outside the candidate container.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
arm="$1"; out="$2"; image="$3"; verifier="$4"; cap="$5"; mode="${6:-score}"
[[ "$arm" == aforge || "$arm" == pi ]] || exit 2
[[ ! -e "$out" ]] || { echo 'Refusing to overwrite evidence.' >&2; exit 2; }
mkdir -p "$out"
out="$(cd "$out" && pwd)"
CONV_REPO_ROOT="$ROOT"; CONV_LIB="$ROOT/bench/conversation/lib"
CONV_MODEL=deepseek/deepseek-v4-flash-0731
CONV_ALLOWLIST="$CONV_MODEL"; CONV_RUN_ID="$(basename "$out")"
CONV_MAX_COST=5; CONV_READY_WAIT=300; CONV_QUIET=30
source "$CONV_LIB/common.sh"
source "$CONV_LIB/allowlist.sh"
source "$CONV_LIB/adapters.sh"
source "$CONV_LIB/guarded.sh"
source "$CONV_LIB/door_tmux.sh"
source "$ROOT/bench/deepswe/lib.sh"
load_task returns-validated-error-accumulation
emu_args
name="af653-complex-$(basename "$out")"
guard_check_key
guard_start "$out/guard-audit.jsonl" "$out/guard-usage.jsonl" "$out"
cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  guard_stop || true
}
trap cleanup EXIT
# Host networking reaches only the guard's loopback address. No host directories,
# credentials, Docker socket, hidden tests, or reference implementation are mounted.
docker run -d --name "$name" --platform linux/amd64 --network host \
  --cpus "$TASK_CPUS" --memory "${TASK_MEM}m" "${EMU_ARGS[@]}" \
  --entrypoint /bin/bash "$image" -c 'sleep 7200' > "$out/container-id.txt"
docker exec "$name" mkdir -p /bench/state/aforge-home /bench/state/pi-home /bench/state/pi-sessions /bench/user
docker exec -w /app "$name" git config user.name Benchmark
docker exec -w /app "$name" git config user.email benchmark@example.invalid
docker exec -w /app "$name" git branch main "$TASK_BASE"
docker exec -w /app "$name" git status --porcelain > "$out/base-status.txt"
docker exec -w /app "$name" git rev-parse HEAD > "$out/base-head.txt"
[[ "$(cat "$out/base-head.txt")" == "$TASK_BASE" && ! -s "$out/base-status.txt" ]]
docker exec -w /app "$name" git show-ref > "$out/base-refs.txt"
docker exec "$name" bash -c 'test ! -e /tests && test ! -e /solution && test ! -e /var/run/docker.sock'
docker cp "$ROOT/bin/aforge" "$name:/usr/local/bin/aforge" >/dev/null
if [[ "$mode" == preflight ]]; then
  printf '%s\n' 'Read the first line of README.md and report it. Do not modify any file.' > "$out/prompt.txt"
else
  cp "$TASK_DIR/instruction.md" "$out/prompt.txt"
fi
python3 - "$out" "$GUARD_URL" "$GUARD_SENTINEL" "$CONV_MODEL" <<'PY'
import json, pathlib, sys
out, url, sentinel, model = sys.argv[1:]
p = pathlib.Path(out)
(p / 'models.json').write_text(json.dumps({'providers': {'guard': {
    'name': 'guard', 'baseUrl': url, 'apiKey': sentinel,
    'api': 'openai-completions', 'models': [{'id': model, 'reasoning': True,
        'contextWindow': 1310720}],
}}}))
(p / 'config.json').write_text(json.dumps({'api_key': sentinel, 'tools.approvalMode': 'allow'}))
PY
docker cp "$out/models.json" "$name:/bench/state/pi-home/models.json" >/dev/null
docker cp "$out/config.json" "$name:/bench/state/aforge-home/config.json" >/dev/null
# All arguments into the container are sentinel credentials or non-secret settings.
# The existing adapter owns the product flags; this wrapper only crosses Docker.
for bin in aforge pi; do
  {
    echo '#!/usr/bin/env bash'
    printf 'exec docker exec -it -w /app'
    for setting in "${EMU_ARGS[@]}"; do printf ' %q' "$setting"; done
    for setting in HOME=/bench/user TERM=xterm-256color AFORGE_HOME=/bench/state/aforge-home \
      PI_CODING_AGENT_DIR=/bench/state/pi-home "AFORGE_BASE_URL=$GUARD_URL" "OPENROUTER_API_KEY=$GUARD_SENTINEL"; do
      printf ' -e %q' "$setting"
    done
    printf ' %q /usr/bin/env %s "$@"\n' "$name" "$bin"
  } > "$out/$bin-wrapper"
  chmod 700 "$out/$bin-wrapper"
done
AFORGE_BIN="$out/aforge-wrapper"; PI_BIN="$out/pi-wrapper"
ARM_GUARD=yes; ARM_ENV=("OPENROUTER_API_KEY=$GUARD_SENTINEL")
ARM_STATE_DIR=/bench/state
arm_baseline "$arm"
arm_effort "$arm" low
arm_tui_argv "$arm" /app "$cap"
# A quiet composer between requests is not completion. Extend the existing
# screen witness with this cell's admission ledger, without inspecting outputs
# for whether the implementation looks correct.
door_is_busy() {
  if [[ -n "$ARM_BUSY_RE" ]] && screen_matches "$ARM_BUSY_RE" "$1"; then return 0; fi
  python3 - "$out/guard-usage.jsonl" <<'PY'
import json, pathlib, sys
admitted, settled = set(), set()
p = pathlib.Path(sys.argv[1])
for line in p.read_text().splitlines() if p.exists() else []:
    try:
        row = json.loads(line)
    except ValueError:
        continue
    if row.get('phase') == 'admitted': admitted.add(row['request_id'])
    if row.get('phase') == 'settled': settled.add(row['request_id'])
sys.exit(0 if admitted - settled else 1)
PY
}
python3 - "$out/prompt.txt" "$out/plan.tsv" <<'PY'
import pathlib, sys
# The existing driver takes one physical line per message. Whitespace is the
# only normalization; the original corpus prompt remains beside the plan.
text = pathlib.Path(sys.argv[1]).read_text()
pathlib.Path(sys.argv[2]).write_text('ready\t' + ' '.join(text.split()) + '\n')
PY
sha256sum "$ROOT/bin/aforge" "$out/prompt.txt" "$out/plan.tsv" > "$out/inputs.sha256"
printf '%s\n' "$image" > "$out/runtime-image.txt"
printf '%s\n' "$verifier" > "$out/verifier-image.txt"
docker exec "$name" bash -c 'node --version; npm --version; pi --version' > "$out/toolchain.txt" 2>&1
set +e
tmux_door_run "$arm" "$out" "$cap" "$out" "$out/plan.tsv" > "$out/driver.log" 2>&1
door_code=$?
set -e
printf '%s\n' "$door_code" > "$out/driver-exit.txt"
# Stop this container's host before extracting files; no worker can race grading.
if [[ "$arm" == aforge ]]; then
  docker exec -w /app -e AFORGE_HOME=/bench/state/aforge-home "${EMU_ARGS[@]}" "$name" \
    /usr/local/bin/aforge engine --workspace /app --stop > "$out/host-stop.log" 2>&1 || true
fi
docker cp "$name:/bench/state" "$out/state" >/dev/null
docker exec -w /app "$name" git add -N -- .
docker exec -w /app "$name" git diff --binary "$TASK_BASE" > "$out/model.patch"
docker exec -w /app "$name" git status --short > "$out/final-status.txt"
docker exec -w /app "$name" git log -5 --oneline > "$out/final-commits.txt"
docker exec -w /app "$name" git worktree list --porcelain > "$out/worktrees.txt"
docker cp "$name:/app" "$out/workspace" >/dev/null
docker rm -f "$name" >/dev/null
guard_stop || true
if [[ "$mode" != preflight ]]; then
  mkdir -p "$out/logs/artifacts" "$out/logs/verifier"
  cp "$out/model.patch" "$out/logs/artifacts/model.patch"
  # The actual image ID was frozen after base/reference controls. Never rebuild
  # from a mutable tag halfway through a comparison.
  timeout "$((TASK_VSECS + 120))" docker run --rm --platform linux/amd64 --network none \
    --cpus "$TASK_CPUS" --memory "${TASK_MEM}m" "${EMU_ARGS[@]}" \
    -v "$out/logs:/logs" "$verifier" /tests/test.sh > "$out/verifier.log" 2>&1 || true
  if [[ -f "$out/logs/verifier/reward.json" ]]; then
    cp "$out/logs/verifier/reward.json" "$out/reward.json"
  else
    printf '%s\n' 'Grader produced no reward: infrastructure failure, not quality zero.' > "$out/grader-error.txt"
  fi
fi
echo "Completed $arm $mode: $out"
