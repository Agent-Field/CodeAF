#!/usr/bin/env bash
# aforge-fake — a stand-in for `aforge chat --once`, so the other half of the
# receipt reader is tested too.
#
# aforge does not stream its usage to stdout: the reply goes to stdout and the
# accounting goes into the home. So this fake writes the two files the reader
# actually opens — v3/usage.jsonl and a transcript with a non-aux `usage` seal —
# and the modes are the same failures as pi-fake's:
#
#   ok           reply, one turn call and one auxiliary call, both on the pin
#   exit3        the same, with a non-zero exit
#   badoutput    a wrong answer
#   hang         never finishes
#   nocost       a reply and NO usage rows: cost unknown, not zero
#   wrongmodel   the auxiliary call is billed to a model off the allowlist,
#                which is the failure --one-model exists to prevent and the one
#                a flag alone can never rule out
set -uo pipefail

MODE="${FAKE_MODE:-ok}"

MODEL="unset"
PROMPT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --version)                     echo "aforge 0.0.0-fake built today"; exit 0 ;;
    --model)                       MODEL="${2:-}"; shift 2 ;;
    --once)                        PROMPT="${2:-}"; shift 2 ;;
    --reasoning|--max-cost|--max-hours|--session) shift 2 ;;
    chat|--one-model|--no-host|--yolo)            shift ;;
    *)                             shift ;;
  esac
done

# Written only for a real cell run, never for the version query above.
[ -n "${FAKE_MARKER:-}" ] && date +%s >> "$FAKE_MARKER"

if [ "$MODE" = "hang" ]; then sleep 3600; exit 0; fi

home="${AFORGE_HOME:?aforge-fake needs AFORGE_HOME, exactly as the real one does}"
mkdir -p "$home/v3/projects/fake/session" "$home/logs"

if [ "$MODE" = "badoutput" ]; then
  echo "Net revenue is \$1.00, the leading region is atlantis, and 99 rows were refunded."
elif key="$(ls "${FAKE_ANSWER_ROOT:-/nonexistent}"/*/judge/expected.json 2>/dev/null | head -1)"; [ -n "$key" ]; then
  FAKE_KEY="$key" python3 -c '
import json, os
key = json.load(open(os.environ["FAKE_KEY"]))
print("Net revenue is $%.2f. The top region is %s with $%.2f. %d rows were refunded."
      % (key["net_revenue_usd"], key["top_region"], key["top_region_revenue_usd"], key["refunded_rows"]))
'
else
  echo "no answer key was reachable"
fi

if [ "$MODE" != "nocost" ]; then
  aux_model="$MODEL"
  [ "$MODE" = "wrongmodel" ] && aux_model="anthropic/claude-sonnet-4"
  FAKE_MODEL="$MODEL" FAKE_AUX="$aux_model" FAKE_HOME="$home" python3 -c '
import json, os
home, model, aux = os.environ["FAKE_HOME"], os.environ["FAKE_MODEL"], os.environ["FAKE_AUX"]
with open(home + "/v3/usage.jsonl", "w") as handle:
    handle.write(json.dumps({"at": "2026-01-01T00:00:00Z", "model": model, "calls": 1,
                             "in": 15663, "out": 44, "usd": 0.00078, "ttft_ms": 900}) + "\n")
    handle.write(json.dumps({"at": "2026-01-01T00:00:01Z", "model": aux, "role": "title",
                             "calls": 1, "in": 122, "out": 102, "usd": 0.000016}) + "\n")
with open(home + "/v3/projects/fake/session/transcript.jsonl", "w") as handle:
    handle.write(json.dumps({"type": "usage", "usage": {"model": model, "input": 15663,
                                                        "output": 44, "costUsd": 0.00078,
                                                        "calls": 1}}) + "\n")
    handle.write(json.dumps({"type": "usage", "usage": {"model": aux, "input": 122,
                                                        "output": 102, "costUsd": 0.000016,
                                                        "calls": 1, "aux": True,
                                                        "role": "title"}}) + "\n")
'
fi

case "$MODE" in
  exit3) exit 3 ;;
  *)     exit 0 ;;
esac
