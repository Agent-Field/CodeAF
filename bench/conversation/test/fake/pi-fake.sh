#!/usr/bin/env bash
# pi-fake — a stand-in for the pi/omp print door, so the rig can be tested
# without a model.
#
# It speaks the parts of the real interface this suite actually uses: a version
# string, an exact-match model catalog, and the JSON Lines event stream that
# `--mode json` produces (verified against pi 0.84.2 and omp 18.1.2). What it
# says is controlled by FAKE_MODE, and each mode is a failure the rig has to
# catch rather than a feature:
#
#   ok           a correct answer, correct model, real usage
#   exit3        the same correct answer, and a non-zero exit code. A rig that
#                reads the reply and forgets the exit code passes this and
#                should not.
#   badoutput    a fluent, confident, wrong answer
#   hang         never finishes — the cap has to stop it
#   nocost       a correct answer with no usage block at all: cost is unknown,
#                which is not the same as zero
#   wrongmodel   a correct answer billed to a model outside the allowlist
#
# Running it leaves FAKE_MARKER behind, which is how the dry-run test proves
# that composing an invocation did not execute one.
set -uo pipefail

MODE="${FAKE_MODE:-ok}"

# Reports which credentials reached this process, by name only. The rig claims
# to hand a harness the OpenRouter key and nothing else; this is how that claim
# is checked rather than believed.
if [ -n "${FAKE_ENV_REPORT:-}" ]; then
  {
    printf 'openrouter=%s\n' "${OPENROUTER_API_KEY:+present}"
    printf 'anthropic=%s\n' "${ANTHROPIC_API_KEY:+present}"
    printf 'openai=%s\n' "${OPENAI_API_KEY:+present}"
  } > "$FAKE_ENV_REPORT"
fi

MODEL="unset"
PROMPT=""
LIST_PATTERN=""
WANT_LIST=0
WANT_VERSION=0
SUBCOMMAND=""

while [ $# -gt 0 ]; do
  case "$1" in
    --version|-v)   WANT_VERSION=1; shift ;;
    --list-models)  WANT_LIST=1; LIST_PATTERN="${2:-}"; shift 2 ;;
    models)         SUBCOMMAND="models"; shift ;;
    find)           [ "$SUBCOMMAND" = "models" ] && { LIST_PATTERN="${2:-}"; shift 2; } || shift ;;
    --model)        MODEL="${2:-}"; shift 2 ;;
    --json)         shift ;;
    -p|--print|--auto-approve|--no-tools|--no-session) shift ;;
    --mode|--provider|--thinking|--session-dir|--cwd|--profile|--max-time|--variant|--dir|--smol|--slow|--plan)
                    shift 2 ;;
    -*)             shift ;;
    *)              PROMPT="$1"; shift ;;
  esac
done

if [ "$WANT_VERSION" = "1" ]; then
  echo "0.0.0-fake"
  exit 0
fi

# The catalog. `nopin` makes the exact id unfindable, which must skip the arm
# rather than run it on a neighbouring model.
if [ "$WANT_LIST" = "1" ] || [ "$SUBCOMMAND" = "models" ]; then
  if [ "$MODE" = "nopin" ]; then
    exit 0
  fi
  # A catalog answers with the id it would accept, not with the pattern it was
  # searched by — that difference is the bug the real pi found in this rig.
  catalog_id="${FAKE_CATALOG_ID:-deepseek/deepseek-v4-flash-0731}"
  if [ "$SUBCOMMAND" = "models" ]; then
    printf '{"models":[{"provider":"openrouter","id":"%s","selector":"openrouter/%s"}]}\n' \
      "$catalog_id" "$catalog_id"
  else
    printf 'provider     model                                             context\n'
    printf 'openrouter   %s   1M\n' "$catalog_id"
  fi
  exit 0
fi

# Only a real cell run marks the marker: the rig asks every binary its version
# and asks its catalog about the pin before it opens any door, and those are not
# runs.
[ -n "${FAKE_MARKER:-}" ] && date +%s >> "$FAKE_MARKER"

if [ "$MODE" = "hang" ]; then
  # Longer than any cap this suite sets, so the cap is what ends it.
  sleep 3600
  exit 0
fi

# The reply. The answer key is in the workspace the fixture built, so a correct
# answer is a matter of reading it — this is a test of the rig, not of anything
# that has to reason.
answer() {
  if [ "$MODE" = "badoutput" ]; then
    printf 'The net revenue is $1.00, the leading region is atlantis with $0.50, and 99 rows were refunded.'
    return
  fi
  if [ -f expected.json ]; then
    python3 -c '
import json
key = json.load(open("expected.json"))
print("Net revenue is $%.2f. The top region is %s with $%.2f. %d rows were refunded."
      % (key["net_revenue_usd"], key["top_region"], key["top_region_revenue_usd"], key["refunded_rows"]))
'
  else
    printf 'There is no expected.json here, so this fake has nothing to answer with.'
  fi
}

BILLED="$MODEL"
[ "$MODE" = "wrongmodel" ] && BILLED="openai/gpt-4o-mini"

FAKE_TEXT="$(answer)" FAKE_MODEL="$BILLED" FAKE_MODE="$MODE" python3 -c '
import json, os
text = os.environ["FAKE_TEXT"]
model = os.environ["FAKE_MODEL"].removeprefix("openrouter/")
usage = {"input": 412, "output": 44, "cacheRead": 0, "cacheWrite": 0,
         "totalTokens": 456,
         "cost": {"input": 5.5e-05, "output": 8.4e-06, "total": 6.34e-05}}
message = {"role": "assistant", "content": [{"type": "text", "text": text}],
           "provider": "openrouter", "model": model, "responseId": "fake-1",
           "stopReason": "stop"}
if os.environ["FAKE_MODE"] != "nocost":
    message["usage"] = usage
print(json.dumps({"type": "session", "version": 3, "id": "fake"}))
print(json.dumps({"type": "turn_start"}))
# The real harnesses repeat the same message on message_end, turn_end and
# agent_end. The fake repeats it too, because a reader that sums all three
# trebles every cost and this is where that must be caught.
print(json.dumps({"type": "message_end", "message": message}))
print(json.dumps({"type": "turn_end", "message": message}))
print(json.dumps({"type": "agent_end", "messages": [message]}))
'

case "$MODE" in
  exit3) exit 3 ;;
  *)     exit 0 ;;
esac
