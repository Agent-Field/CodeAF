#!/usr/bin/env bash
# run.sh — drive the real aforge, as a person, and write down which journeys
# are true today.
#
# The doctrine is docs/JOURNEY.md: a journey passes only when the screen showed
# it, the journal durably recorded it, and nothing else happened. So this
# builds the real binary, gives it a disposable brain that shares nothing with
# the user's, opens it in a real terminal, and types.
#
#   test/ux/run.sh                     everything
#   test/ux/run.sh --only 01,02,05     these journeys
#   test/ux/run.sh --suite quality     the quality suite only
#   test/ux/run.sh --keep              leave the tmux session and home behind
#
# Money: every model call lands in the disposable store's usage table, and the
# cumulative total is checked between journeys against UX_CAP (default $3).
# Over the cap the run aborts — a test suite must not be able to spend a
# surprise.

set -uo pipefail

UX_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$UX_ROOT/../.." && pwd)"

: "${UX_CAP:=3.00}"
: "${UX_WIDTH:=200}"
: "${UX_HEIGHT:=50}"
: "${UX_REAL_HOME:=$HOME}"

SUITES=(journeys quality)
ONLY=""
KEEP=0

while [ $# -gt 0 ]; do
  case "$1" in
    --only) ONLY="$2"; shift 2 ;;
    --suite) SUITES=("$2"); shift 2 ;;
    --keep) KEEP=1; shift ;;
    --cap) UX_CAP="$2"; shift 2 ;;
    -h|--help) sed -n '2,25p' "$0"; exit 0 ;;
    *) echo "unknown flag $1" >&2; exit 2 ;;
  esac
done

for tool in tmux sqlite3 python3; do
  command -v "$tool" >/dev/null || { echo "run.sh needs $tool on PATH" >&2; exit 2; }
done

# ------------------------------------------------------------------ the home
#
# Disposable, fresh per run, and named nothing like the user's. HOME is moved
# as well as AFORGE_HOME: the state root override covers everything aforge
# writes on purpose, and moving HOME too means that anything which does not yet
# honour it — a launchd plist, a stray dotfile — lands here rather than in the
# user's actual home. Whatever shows up under $UX_HOME/.aforge is a leak, and
# the report says so.

RUN_ID="$(date +%Y%m%d-%H%M%S)"
UX_RUN="${UX_RUN:-$(mktemp -d "${TMPDIR:-/tmp}/aforge-ux-$RUN_ID-XXXX")}"
UX_HOME="$UX_RUN/home"
UX_STATE="$UX_HOME/state"
UX_DB="$UX_STATE/graph.db"
UX_EVIDENCE="$UX_ROOT/evidence"
UX_REPORT="$UX_ROOT/report.md"
UX_SESSION="aforge-ux-$$"
UX_SESSION_B="aforge-ux-$$-b"
UX_BIN="$REPO/bin/aforge"

mkdir -p "$UX_STATE" "$UX_HOME/Library/LaunchAgents"
rm -rf "$UX_EVIDENCE"; mkdir -p "$UX_EVIDENCE"

cleanup() {
  local code=$?
  tmux kill-session -t "$UX_SESSION" 2>/dev/null
  tmux kill-session -t "$UX_SESSION_B" 2>/dev/null
  if [ "$KEEP" = "1" ]; then
    echo "kept: $UX_RUN"
  else
    rm -rf "$UX_RUN"
  fi
  exit $code
}
trap cleanup EXIT INT TERM

# ------------------------------------------------------- provider + settings
#
# The one and only read of the user's real store: the API key, and the model
# pins as a baseline. Nothing is ever written back there.

if [ ! -f "$UX_REAL_HOME/.aforge/config.json" ]; then
  echo "no $UX_REAL_HOME/.aforge/config.json — the suite needs a provider key" >&2
  exit 2
fi

python3 - "$UX_REAL_HOME/.aforge" "$UX_STATE" <<'PY'
import json, os, sys
real, disposable = sys.argv[1], sys.argv[2]
config = json.load(open(os.path.join(real, "config.json")))
key = config.get("api_key", "")
if not key:
    raise SystemExit("the real config.json has no api_key")
json.dump({"api_key": key}, open(os.path.join(disposable, "config.json"), "w"))
os.chmod(os.path.join(disposable, "config.json"), 0o600)

# Settings are the user's pins as a baseline — they already name the cheapest
# capable models — with the rails a disposable run wants on top.
try:
    settings = json.load(open(os.path.join(real, "settings.json")))
except FileNotFoundError:
    settings = {}
settings.setdefault("chat_model", "~deepseek/deepseek-v4-flash-latest")
settings.setdefault("task_model", "~deepseek/deepseek-v4-flash-latest")
json.dump(settings, open(os.path.join(disposable, "settings.json"), "w"), indent=2)
print("talk=%s work=%s plan=%s boost=%s" % (
    settings.get("chat_model"), settings.get("task_model"),
    settings.get("plan_model"), settings.get("boost_model")))
PY
[ $? -eq 0 ] || exit 2

# ------------------------------------------------------------------- binary

echo "building $UX_BIN"
( cd "$REPO" && go build -o bin/aforge ./cmd/aforge ) || exit 2

# UX_ENV is the one description of the disposable brain. Both this runner and
# the journeys that relaunch it (leave-and-return, second window) build their
# tmux command from exactly this string, so a window opened by journey 12 is
# the same brain the suite has been talking to all along.
UX_ENV="HOME=$UX_HOME AFORGE_HOME=$UX_STATE AFORGE_PROFILE_DIR=$UX_STATE"
UX_ENV="$UX_ENV AFORGE_DAILY_BUDGET=$UX_CAP AFORGE_BRIEF_AFTER=0 AFORGE_PRACTICE_BUDGET=0"
UX_ENV="$UX_ENV TERM=xterm-256color"

export UX_BIN UX_HOME UX_STATE UX_DB UX_EVIDENCE UX_CAP UX_ROOT REPO UX_RUN UX_ENV
export UX_SESSION UX_SESSION_B UX_WIDTH UX_HEIGHT

launch() {
  local sess="$1"; shift
  tmux kill-session -t "$sess" 2>/dev/null
  tmux new-session -d -s "$sess" -x "$UX_WIDTH" -y "$UX_HEIGHT" \
    "env $UX_ENV $* '$UX_BIN' chat; echo AFORGE-EXITED; sleep 900"
  sleep 6
}

# --------------------------------------------------------------- the money

total_spend() { sqlite3 -readonly "$UX_DB" "select coalesce(round(sum(cost),6),0) from usage" 2>/dev/null || echo 0; }

over_cap() {
  local spent="$1"
  python3 -c "import sys; sys.exit(0 if float(sys.argv[1]) > float(sys.argv[2]) else 1)" "$spent" "$UX_CAP"
}

# --------------------------------------------------------------- the journeys

declare -a ORDER=()
for suite in "${SUITES[@]}"; do
  for script in "$UX_ROOT/$suite"/*.sh; do
    [ -e "$script" ] || continue
    ORDER+=("$script")
  done
done

wanted() {
  [ -z "$ONLY" ] && return 0
  local name; name="$(basename "$1" .sh)"
  case ",$ONLY," in *",${name%%-*},"*) return 0 ;; *",$name,"*) return 0 ;; esac
  return 1
}

echo
echo "aforge UX suite — $(date)"
echo "  binary   $UX_BIN"
echo "  home     $UX_HOME  (real home untouched: $UX_REAL_HOME)"
echo "  session  $UX_SESSION at ${UX_WIDTH}x${UX_HEIGHT}"
echo "  cap      \$$UX_CAP"
echo

launch "$UX_SESSION"
if ! tmux capture-pane -t "$UX_SESSION" -p | grep -q 'aforge'; then
  echo "aforge did not draw a first frame; aborting" >&2
  tmux capture-pane -t "$UX_SESSION" -p
  exit 1
fi

declare -a RESULTS=()
declare -a GAUGES=()
ABORTED=""
JUDGE_CALLS=0
START_TS="$(date +%s)"

sql() { sqlite3 -readonly "$UX_DB" "$1" 2>/dev/null; }
watermark() { sql "select coalesce(max(seq),0) from events"; }

for script in "${ORDER[@]}"; do
  name="$(basename "$script" .sh)"
  wanted "$script" || continue

  before="$(total_spend)"
  mark_before="$(watermark)"
  started="$(date +%s)"
  UX_JOURNEY="$name" UX_SESSION="$UX_SESSION" bash "$script"
  code=$?
  elapsed=$(( $(date +%s) - started ))
  after="$(total_spend)"
  cost="$(python3 -c "print('%.4f' % (float('$after') - float('$before')))")"

  verdict="$(cat "$UX_EVIDENCE/$name/verdict" 2>/dev/null || echo FAIL)"
  checks="$(cat "$UX_EVIDENCE/$name/checks" 2>/dev/null || echo '0/0 checks')"
  [ "$code" -ne 0 ] && [ "$verdict" = "PASS" ] && verdict=FAIL
  RESULTS+=("$name|$verdict|$checks|${elapsed}s|\$$cost")

  # ------------------------------------------------------------- the gauges
  #
  # Proportionality: how much machinery did the ask actually buy? A haiku is
  # one node. A haiku that became five is a passing journey with a quality
  # problem, and the report has to be able to say so.
  nodes="$(sql "select count(*) from nodes where created_seq > $mark_before")"
  jobs="$(sql "select count(*) from nodes where created_seq > $mark_before and origin='user' and parent_id='root'")"
  users="$(sql "select count(*) from messages where seq > $mark_before and role='user'")"
  agents="$(sql "select count(*) from messages where seq > $mark_before and role='agent'")"
  systems="$(sql "select count(*) from messages where seq > $mark_before and role='system'")"
  expect="$(cat "$UX_EVIDENCE/$name/expect-nodes" 2>/dev/null || echo 1)"

  # Noise: agent turns beyond one reply per thing the user said. Receipts and
  # deliverables are not noise — they are the product working.
  noise="$(python3 -c "print(max(0, ${agents:-0} - ${users:-0}))")"

  flags=""
  if [ "${nodes:-0}" -gt "${expect:-1}" ] && [ "${nodes:-0}" -gt 2 ]; then
    flags="$flags OVER-DECOMPOSED(${nodes}v${expect})"
  fi
  [ "${noise:-0}" -ge 2 ] && flags="$flags CHATTY(+$noise)"
  python3 -c "import sys; sys.exit(0 if float('$cost') > 0.05 else 1)" && flags="$flags EXPENSIVE"
  [ "${elapsed:-0}" -gt 180 ] && flags="$flags SLOW"

  # ------------------------------------------------------------- the judge
  quality="—"; why=""
  if [ -s "$UX_EVIDENCE/$name/judge-deliverable" ]; then
    judged="$(python3 "$UX_ROOT/judge.py" "$UX_STATE" \
      "$UX_EVIDENCE/$name/judge-ask" "$UX_EVIDENCE/$name/judge-deliverable" 2>/dev/null)"
    JUDGE_CALLS=$((JUDGE_CALLS + 1))
    quality="${judged%%|*}"
    why="${judged#*|}"
    {
      echo
      echo "### quality judge"
      echo
      echo "- **score**: $quality/5"
      echo "- **why**: $why"
    } >> "$UX_EVIDENCE/$name/notes.md"
    printf '    · quality %s/5 — %s\n' "$quality" "$why" >&2
    case "$quality" in
      1|2) flags="$flags POOR-RESULT" ;;
      3)   flags="$flags MIDDLING" ;;
    esac
  fi

  GAUGES+=("$name|$verdict|${jobs:-0}/${nodes:-0}|\$$cost|${elapsed}|$quality|${flags:-—}|$why")

  if over_cap "$after"; then
    ABORTED="spend \$$after exceeded the \$$UX_CAP cap after $name"
    echo "ABORT: $ABORTED" >&2
    break
  fi
done

TOTAL="$(total_spend)"
WALL=$(( $(date +%s) - START_TS ))

# --------------------------------------------------------------- leak check

LEAKS="$(find "$UX_HOME" -maxdepth 1 -name '.aforge' 2>/dev/null)"
PLIST="$(ls "$UX_HOME/Library/LaunchAgents" 2>/dev/null | tr '\n' ' ')"

# ------------------------------------------------------------------ report

{
  echo "# aforge UX suite — which journeys are true today"
  echo
  echo "Run $(date -u '+%Y-%m-%d %H:%M UTC') · binary \`$(cd "$REPO" && git rev-parse --short HEAD)\` · wall ${WALL}s"
  echo
  echo "Real binary, real terminal (tmux ${UX_WIDTH}x${UX_HEIGHT}), real provider (OpenRouter),"
  echo "disposable brain at \`\$AFORGE_HOME\`. Models: $(python3 -c "
import json;s=json.load(open('$UX_STATE/settings.json'));print('talk %s · work %s · plan %s · boost %s' % (s.get('chat_model'),s.get('task_model'),s.get('plan_model'),s.get('boost_model')))")"
  echo
  echo "**Total spend: \$$TOTAL** (cap \$$UX_CAP) · plus $JUDGE_CALLS quality-judge calls made outside aforge and not in its journal"
  [ -n "$ABORTED" ] && echo && echo "> **RUN ABORTED** — $ABORTED"
  echo
  echo '## The report card'
  echo
  echo 'Completion is the cheap half. `jobs/nodes` is how much machinery the ask bought'
  echo '(a haiku should be one node); `quality` is a cheap model reading the deliverable'
  echo 'against a fixed rubric — literal ask satisfied, answer-first, competent, 1–5.'
  echo
  echo '| journey | pass | jobs/nodes | $ | seconds | quality/5 | flags |'
  echo '|---|---|---|---|---|---|---|'
  for row in "${GAUGES[@]}"; do
    IFS='|' read -r n v nodes money secs q f _why <<< "$row"
    echo "| $n | $v | $nodes | $money | $secs | $q | $f |"
  done
  echo
  for row in "${GAUGES[@]}"; do
    IFS='|' read -r n v nodes money secs q f why <<< "$row"
    [ -n "$why" ] && echo "- **$n** judged $q/5 — $why"
  done
  echo
  echo '## Journeys'
  echo
  echo '| journey | verdict | checks | time | cost | evidence |'
  echo '|---|---|---|---|---|---|'
  for row in "${RESULTS[@]}"; do
    IFS='|' read -r n v c t m <<< "$row"
    echo "| $n | **$v** | $c | $t | $m | [notes](evidence/$n/notes.md) |"
  done
  echo
  echo '### Not run'
  echo
  echo '| journey | why |'
  echo '|---|---|'
  echo '| J15 practice | idle-time loop: `AFORGE_PRACTICE_IDLE` defaults to 20m of quiet before self-practice, so an honest test costs 20 minutes of nothing happening. Out of scope for a suite that must stay under a few minutes per journey. |'
  echo "| J9 consent (full) | needs a plan estimated over the real \$3 rail. The cheap variant runs instead: \`AFORGE_PLAN_CONSENT\` lowered so the same gate fires on a job costing cents. |"
  echo
  echo '### Housekeeping'
  echo
  if [ -n "$LEAKS" ]; then
    echo "- ⚠️ **state leak**: something wrote to \`\$HOME/.aforge\` despite \`AFORGE_HOME\` — $LEAKS"
  else
    echo '- ✅ no state leak: nothing landed in `$HOME/.aforge`; the whole run lived under `$AFORGE_HOME`.'
  fi
  if [ -n "$PLIST" ]; then
    echo "- ⚠️ the run installed a launch agent in the disposable home: $PLIST"
  else
    echo '- ✅ no launch agent installed (the standing-watch offer is declined by the suite).'
  fi
  echo "- the user's real store at \`$UX_REAL_HOME/.aforge\` was read once (api_key, settings.json) and never written."
  echo
  for row in "${RESULTS[@]}"; do
    IFS='|' read -r n v c t m <<< "$row"
    echo "---"
    echo
    echo "## $n — $v"
    echo
    sed 's/^# .*$//' "$UX_EVIDENCE/$n/notes.md" 2>/dev/null
    echo
  done
} > "$UX_REPORT"

echo
echo "================================================================"
printf '%-26s %-9s %-14s %6s %8s\n' journey verdict checks time cost
for row in "${RESULTS[@]}"; do
  IFS='|' read -r n v c t m <<< "$row"
  printf '%-26s %-9s %-14s %6s %8s\n' "$n" "$v" "$c" "$t" "$m"
done
echo "----------------------------------------------------------------"
echo "total spend \$$TOTAL / cap \$$UX_CAP   ·   report: $UX_REPORT"
echo

FAILED=0
for row in "${RESULTS[@]}"; do
  IFS='|' read -r n v c t m <<< "$row"
  [ "$v" = "FAIL" ] && FAILED=$((FAILED + 1))
done
[ -n "$ABORTED" ] && FAILED=$((FAILED + 1))
[ "$FAILED" -gt 0 ] && exit 1
exit 0
