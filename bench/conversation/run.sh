#!/usr/bin/env bash
# Run the conversation battery: every scenario crossed with every arm that has
# an honest door for it, one isolated workspace and state root per cell, and a
# receipt kept for everything that is claimed afterwards.
#
# THIS SPENDS REAL MONEY AND IS ON DEMAND. Nothing in `make check` reaches it.
#
# Read bench/conversation/README.md before quoting a number from it. Three
# things in particular are not conventions but the point of the suite:
#
#   the doors     a `--print` row and a terminal row are different measurements
#                 and are never mixed. An arm with no interactive door here is
#                 recorded `unsupported`, not quietly run through the other one.
#   the pin       every arm is handed the same exact catalog id, and every model
#                 in a cell's own receipts is checked against the allowlist
#                 afterwards. Open models only.
#   the outcomes  pass, fail, timeout, crash, unsupported and skipped are six
#                 different words. None of the last four is a pass.
#
# Usage:
#   bench/conversation/run.sh --dry-run                compose everything, run nothing
#   bench/conversation/run.sh --scenarios data-tally   one cell
#   bench/conversation/run.sh --arms aforge,pi         two arms
#   bench/conversation/run.sh --door print             only the non-interactive door
set -uo pipefail

CONV_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONV_LIB="$CONV_ROOT/lib"
CONV_REPO_ROOT="$(cd "$CONV_ROOT/../.." && pwd)"
export CONV_ROOT CONV_LIB CONV_REPO_ROOT

# shellcheck source=lib/common.sh
source "$CONV_LIB/common.sh"
# shellcheck source=lib/allowlist.sh
source "$CONV_LIB/allowlist.sh"
# shellcheck source=lib/verdict.sh
source "$CONV_LIB/verdict.sh"
# shellcheck source=lib/adapters.sh
source "$CONV_LIB/adapters.sh"
# shellcheck source=lib/door_tmux.sh
source "$CONV_LIB/door_tmux.sh"
# shellcheck source=lib/guarded.sh
source "$CONV_LIB/guarded.sh"

ALL_SCENARIOS="data-tally research-brief writing-memo code-fix followup-while-working revision-midwork work-result-recalled"

ARMS="${CONV_ARMS:-aforge omp pi}"
SCENARIOS="$ALL_SCENARIOS"
DOOR_FILTER=""
# The effort rung asked of every arm. `low` is the default because it is the
# only rung all three of aforge, omp and pi actually have (pi has no `medium`),
# and DeepSeek V4 Flash's catalog offers low/high/max. An arm without the rung
# runs anyway and is marked not-comparable — never silently moved to another one.
EFFORT="${CONV_EFFORT:-low}"
DRY_RUN=0
KEEP_STATE="${CONV_KEEP:-0}"
# A cap in seconds that overrides every scenario's own. It exists for the
# deterministic tests, which need a cell to hit the cap in seconds rather than
# minutes, and for anybody who wants a cheaper sweep than the scenarios ask for.
CAP_OVERRIDE="${CONV_CAP:-}"
# Every live call goes through the forwarding guard (lib/guard.py), which holds
# the real key and refuses a model off the allowlist before opening any socket
# upstream. An arm that cannot be routed through it is unsupported here.
#
# UNGUARDED IS NOT A WORKAROUND. It exists so the deterministic tests can drive
# fake binaries that talk to nobody, and it refuses to run unless the caller has
# declared that with CONV_FAKE_HARNESS=1.
UNGUARDED=0
# Evidence is never overwritten by accident: a run whose cell directory already
# exists refuses rather than deleting whatever a previous run left there.
OVERWRITE="${CONV_OVERWRITE:-0}"

while [ $# -gt 0 ]; do
  case "$1" in
    --arms)        ARMS="$(echo "${2:?--arms needs a value}" | tr ',' ' ')"; shift 2 ;;
    --arms=*)      ARMS="$(echo "${1#*=}" | tr ',' ' ')"; shift ;;
    --scenarios)   SCENARIOS="$(echo "${2:?--scenarios needs a value}" | tr ',' ' ')"; shift 2 ;;
    --scenarios=*) SCENARIOS="$(echo "${1#*=}" | tr ',' ' ')"; shift ;;
    --door)        DOOR_FILTER="${2:?--door needs a value}"; shift 2 ;;
    --door=*)      DOOR_FILTER="${1#*=}"; shift ;;
    --cap)         CAP_OVERRIDE="${2:?--cap needs a value}"; shift 2 ;;
    --cap=*)       CAP_OVERRIDE="${1#*=}"; shift ;;
    --effort)      EFFORT="${2:?--effort needs a value}"; shift 2 ;;
    --effort=*)    EFFORT="${1#*=}"; shift ;;
    --model)       CONV_MODEL="${2:?--model needs a value}"; shift 2 ;;
    --model=*)     CONV_MODEL="${1#*=}"; shift ;;
    --allowlist)   CONV_ALLOWLIST="${2:?--allowlist needs a value}"; CONV_ALLOWLIST_EXPLICIT=yes; shift 2 ;;
    --allowlist=*) CONV_ALLOWLIST="${1#*=}"; CONV_ALLOWLIST_EXPLICIT=yes; shift ;;
    --unguarded)   UNGUARDED=1; shift ;;
    --overwrite)   OVERWRITE=1; shift ;;
    --out)         CONV_OUT="${2:?--out needs a value}"; shift 2 ;;
    --out=*)       CONV_OUT="${1#*=}"; shift ;;
    --keep)        KEEP_STATE=1; shift ;;
    --dry-run)     DRY_RUN=1; shift ;;
    -h|--help)     sed -n '1,30p' "$0"; exit 0 ;;
    *)             conv_warn "unknown argument: $1"; exit 1 ;;
  esac
done

allowlist_follow_model

if [ "$UNGUARDED" = "1" ] && [ "${CONV_FAKE_HARNESS:-0}" != "1" ]; then
  conv_warn "--unguarded runs harnesses with the real key and no model gate; it is for fake"
  conv_warn "binaries only and needs CONV_FAKE_HARNESS=1. Refusing."
  exit 1
fi

# The run id carries the process id as well as the clock so that two runs
# started in the same second do not share an evidence directory.
CONV_RUN_ID="${CONV_RUN_ID:-$(date +%Y%m%d-%H%M%S)-$$}"
CONV_OUT="${CONV_OUT:-$CONV_REPO_ROOT/bench-results/conversation/$CONV_RUN_ID}"
CSV="${CONV_CSV:-$CONV_REPO_ROOT/bench-results/conversation.csv}"
export CONV_RUN_ID

require_tools python3 awk || exit 1
if [ "$DRY_RUN" != "1" ]; then
  [ -n "$TIMEOUT_BIN" ] || { conv_warn "need timeout(1) — brew install coreutils"; exit 1; }
fi

GIT_SHA="$(cd "$CONV_REPO_ROOT" && git rev-parse --short HEAD 2>/dev/null || echo unknown)"
TODAY="$(date +%Y-%m-%d)"
mkdir -p "$CONV_OUT" "$(dirname "$CSV")"

# The header is written once, ever, and columns are appended rather than
# inserted: a reader that indexes the first columns by position keeps reading
# the same things after a column is added.
[ -f "$CSV" ] || echo "date,run_id,git_sha,scenario,workload,door,arm,arm_version,model,effort_requested,effort_sent,wall_s,exit,cost_usd,cost_source,tokens_in,tokens_out,turns,verdict,comparable,notes" > "$CSV"

RESULTS_JSONL="$CONV_OUT/results.jsonl"
# A run's own summary is authoritative evidence and is refused before it is
# opened. Per-cell refusal is not enough on its own: truncating this file first
# and only then declining to overwrite the cells leaves the cell directories
# intact and destroys the record that says what they were.
if [ -s "$RESULTS_JSONL" ] && [ "$OVERWRITE" != "1" ]; then
  conv_warn "refusing to write over an existing run summary at $RESULTS_JSONL"
  conv_warn "(it holds $(grep -c "" "$RESULTS_JSONL") row(s)); pass --overwrite to replace this run,"
  conv_warn "or give --out a directory of its own."
  exit 1
fi
: > "$RESULTS_JSONL"

echo "battery:    bench/conversation"
echo "run:        $CONV_RUN_ID"
echo "git:        $GIT_SHA"
allowlist_banner
echo "effort:     $EFFORT (asked of every arm; an arm without the rung is marked not-comparable)"
echo "arms:       $ARMS"
echo "scenarios:  $SCENARIOS"
echo "evidence:   $CONV_OUT"
echo "history:    $CSV"
[ "$DRY_RUN" = "1" ] && echo "dry run:    composing fixtures, argv and turn plans only — no model call, no spend"
echo

# Versions are recorded before anything runs, because "which build produced this
# row" is the first question anybody asks of a comparison and the peers move
# their flags between releases.
# A plain table rather than an associative array: /bin/bash on macOS is 3.2 and
# has none, and a rig that only runs under the shell its author happened to have
# is not portable.
ARM_VERSION_TABLE=""
version_of() {
  printf '%s' "$ARM_VERSION_TABLE" | awk -v arm="$1" -F'\t' '$1 == arm { print $2; found = 1 }
    END { if (!found) print "unknown" }'
}
for arm in $ARMS; do
  if arm_known "$arm"; then
    ARM_VERSION_TABLE="$ARM_VERSION_TABLE$arm	$(arm_version "$arm" | tr -d '\t')
"
    printf 'version:    %-9s %s\n' "$arm" "$(version_of "$arm")"
  else
    conv_warn "unknown arm: $arm"
  fi
done
echo

TOTAL_PASS=0; TOTAL_FAIL=0; TOTAL_SKIP=0; TOTAL_UNSUP=0; TOTAL_TIMEOUT=0; TOTAL_CRASH=0
CLEANUP_PATHS=()

# The key is checked once for the run; the guard itself is started per cell, so
# that what it meters belongs to exactly one cell.
if [ "$DRY_RUN" != "1" ] && [ "$UNGUARDED" != "1" ]; then
  # The guard is the only process that gets the real key, so it has to be in
  # this shell's environment. Nothing here reads a credential out of a config
  # file: whose key this is stays the operator's decision, made explicitly.
  if [ -z "${OPENROUTER_API_KEY:-}" ]; then
    conv_warn "OPENROUTER_API_KEY is not set in this shell, so the guard has no upstream key."
    conv_warn "Run from a shell that has it (a login shell, or export it for this command)."
    exit 1
  fi
  if ! guard_check_key; then
    exit 1
  fi
  echo "guard:      one per cell, upstream openrouter, allowlist enforced before forwarding"
  echo "audit:      <cell>/guard-audit.jsonl and <cell>/guard-usage.jsonl"
  echo
fi
trap 'guard_stop' EXIT

# emit_row writes one cell's outcome to both files. Every caller goes through
# it, including the ones that never ran a harness: a scenario an arm cannot do
# leaves a row saying so, because a missing row is indistinguishable from a
# scenario nobody thought of.
emit_row() {
  local scenario="$1" workload="$2" door="$3" arm="$4" wall="$5" code="$6" \
        cost="$7" cost_source="$8" tokens_in="$9" tokens_out="${10}" turns="${11}" \
        verdict="${12}" comparable="${13}" reason="${14}"
  local version; version="$(version_of "$arm")"
  echo "$TODAY,$CONV_RUN_ID,$GIT_SHA,$scenario,$workload,$door,$arm,$(csv_field "$version"),$(csv_field "$CONV_MODEL"),$EFFORT,${ARM_EFFORT_SENT:-n/a},$wall,$code,$cost,$cost_source,$tokens_in,$tokens_out,$turns,$verdict,$comparable,$(csv_field "${CELL_NOTES:-}${reason:+;$reason}")" >> "$CSV"
  cat >> "$RESULTS_JSONL" <<JSON
{"date":$(json_str "$TODAY"),"run_id":$(json_str "$CONV_RUN_ID"),"git_sha":$(json_str "$GIT_SHA"),"scenario":$(json_str "$scenario"),"workload":$(json_str "$workload"),"door":$(json_str "$door"),"arm":$(json_str "$arm"),"arm_version":$(json_str "$version"),"model_pin":$(json_str "$CONV_MODEL"),"effort_requested":$(json_str "$EFFORT"),"effort_sent":$(json_str "${ARM_EFFORT_SENT:-n/a}"),"effort_supported":$(json_str "${ARM_EFFORT_SUPPORTED:-n/a}"),"wall_s":$(json_str "$wall"),"exit":$(json_str "$code"),"cost_usd":$([ "$cost" = "unknown" ] && echo null || echo "$cost"),"cost_source":$(json_str "$cost_source"),"tokens_in":$([ "$tokens_in" = "unknown" ] && echo null || echo "${tokens_in:-null}"),"tokens_out":$([ "$tokens_out" = "unknown" ] && echo null || echo "${tokens_out:-null}"),"turns":$(json_str "$turns"),"verdict":$(json_str "$verdict"),"comparable":$(json_str "$comparable"),"reason":$(json_str "$reason"),"notes":$(json_str "${CELL_NOTES:-}"),"checks":[${CELL_CHECK_JSON:-}]}
JSON
  case "$verdict" in
    pass)        TOTAL_PASS=$((TOTAL_PASS + 1)) ;;
    fail)        TOTAL_FAIL=$((TOTAL_FAIL + 1)) ;;
    timeout)     TOTAL_TIMEOUT=$((TOTAL_TIMEOUT + 1)) ;;
    crash)       TOTAL_CRASH=$((TOTAL_CRASH + 1)) ;;
    skipped)     TOTAL_SKIP=$((TOTAL_SKIP + 1)) ;;
    unsupported) TOTAL_UNSUP=$((TOTAL_UNSUP + 1)) ;;
  esac
}

for scenario in $SCENARIOS; do
  scenario_file="$CONV_ROOT/scenarios/$scenario.sh"
  if [ ! -f "$scenario_file" ]; then
    conv_warn "no such scenario: $scenario"
    continue
  fi

  for arm in $ARMS; do
    arm_known "$arm" || continue

    # The contract is reset before every load, so a scenario that forgets to
    # define one of these inherits nothing from the one before it.
    unset SCENARIO_WORKLOAD SCENARIO_DOOR SCENARIO_ARMS SCENARIO_CAP_S SCENARIO_GUARDS
    scenario_fixture() { :; }
    scenario_prompt() { :; }
    scenario_turns() { :; }
    scenario_check() { :; }
    # shellcheck source=/dev/null
    source "$scenario_file"
    : "${SCENARIO_WORKLOAD:=unknown}"
    : "${SCENARIO_DOOR:=print}"
    : "${SCENARIO_ARMS:=$CONV_ALL_ARMS}"
    : "${SCENARIO_CAP_S:=300}"
    [ -z "$CAP_OVERRIDE" ] || SCENARIO_CAP_S="$CAP_OVERRIDE"

    [ -z "$DOOR_FILTER" ] || [ "$DOOR_FILTER" = "$SCENARIO_DOOR" ] || continue

    assert_begin
    ARM_EFFORT_SENT="n/a"; ARM_EFFORT_SUPPORTED="n/a"
    printf '── %-26s %-8s %-12s %s\n' "$scenario" "$arm" "$SCENARIO_DOOR" "${SCENARIO_GUARDS:-}"

    # ── is this cell honest to run at all ─────────────────────────────────
    case " $SCENARIO_ARMS " in
      *" $arm "*) ;;
      *)
        printf '    ⊘  unsupported: this suite defines no %s door for %s\n' "$SCENARIO_DOOR" "$arm"
        emit_row "$scenario" "$SCENARIO_WORKLOAD" "$SCENARIO_DOOR" "$arm" "" "" unknown none unknown unknown "" \
                 unsupported no "scenario not defined for $arm"
        echo
        continue
        ;;
    esac

    if [ -z "$(arm_bin "$arm")" ]; then
      printf '    ⊘  skipped: %s is not installed\n' "$arm"
      emit_row "$scenario" "$SCENARIO_WORKLOAD" "$SCENARIO_DOOR" "$arm" "" "" unknown none unknown unknown "" \
               skipped no "$arm not installed"
      echo
      continue
    fi

    comparable="yes"; comparable_reason=""
    if ! arm_effort "$arm" "$EFFORT"; then
      comparable="no"; comparable_reason="$ARM_EFFORT_NOTE"
      printf '    !  effort mismatch: %s\n' "$ARM_EFFORT_NOTE"
    elif [ "$ARM_EFFORT_SUPPORTED" = "unverified" ]; then
      comparable="no"; comparable_reason="$ARM_EFFORT_NOTE"
      printf '    !  effort unverifiable: %s\n' "$ARM_EFFORT_NOTE"
    fi

    # Recorded, not relied on: role pins are configuration and the guard is the
    # enforcement. Both belong on the row.
    arm_role_pin "$arm"
    record "role_pin" "$ARM_ROLE_PIN"

    # What ambient configuration could be turned off with a documented flag, and
    # what could not. A peer that loads the operator's skills and MCP servers is
    # running a different system prompt and a different tool set from its
    # neighbours, and that belongs on the row rather than in a footnote.
    arm_baseline "$arm"
    record "ambient" "$ARM_BASELINE_NOTE"

    cell="$CONV_OUT/$scenario-$arm"
    work="$cell/work"
    # Evidence is not overwritten by accident. A cell directory that already
    # exists belongs to an earlier run, and deleting it would erase the only
    # record of what that run did.
    if [ -e "$cell" ] && [ "$OVERWRITE" != "1" ]; then
      conv_warn "refusing to overwrite existing evidence at $cell (pass --overwrite to replace it)"
      emit_row "$scenario" "$SCENARIO_WORKLOAD" "$SCENARIO_DOOR" "$arm" "" "" unknown none unknown unknown "" \
               skipped no "evidence directory already exists"
      echo
      continue
    fi
    [ "$OVERWRITE" = "1" ] && rm -rf "$cell"
    mkdir -p "$cell" "$work"
    CONV_OMP_PROFILE_ACTIVE=""
    if ! arm_isolate "$arm" "$cell"; then
      printf '    ⊘  skipped: %s\n' "$ARM_ISOLATION"
      emit_row "$scenario" "$SCENARIO_WORKLOAD" "$SCENARIO_DOOR" "$arm" "" "" unknown none unknown unknown "" \
               skipped no "$ARM_ISOLATION"
      echo
      continue
    fi
    [ -n "$ARM_CLEANUP_PATH" ] && CLEANUP_PATHS+=("$ARM_CLEANUP_PATH")
    child_env_array "${ARM_ENV[@]}"

    if [ "$DRY_RUN" != "1" ] && [ "$UNGUARDED" != "1" ]; then
      if ! guard_start "$cell/guard-audit.jsonl" "$cell/guard-usage.jsonl" "$scenario-$arm"; then
        printf '    ✗  the cell'"'"'s guard did not start\n'
        emit_row "$scenario" "$SCENARIO_WORKLOAD" "$SCENARIO_DOOR" "$arm" "" "" unknown none unknown unknown "" \
                 fail no "guard did not start"
        echo
        continue
      fi
    fi

    # Route this arm through the guard before anything else touches the network.
    # An arm that cannot be routed is unsupported for a live run: its calls
    # would leave with the real key and no model gate in front of them.
    if [ "$DRY_RUN" != "1" ] && [ "$UNGUARDED" != "1" ]; then
      if ! arm_guard_wire "$arm" "$cell"; then
        printf '    ⊘  unsupported: not routable through the guard (%s)\n' "$ARM_GUARD_NOTE"
        emit_row "$scenario" "$SCENARIO_WORKLOAD" "$SCENARIO_DOOR" "$arm" "" "" unknown none unknown unknown "" \
                 unsupported no "no guard route: $ARM_GUARD_NOTE"
        echo
        continue
      fi
      record "guard" "$ARM_GUARD_NOTE"
    fi

    # The child environment is built now because the pin check below has to ask
    # the catalog in the same state root the cell will run in. Asked in the
    # operator's environment it answers a different question: pi's default
    # profile on this machine lists no openrouter models, while a fresh
    # PI_CODING_AGENT_DIR does.
    child_env_array "${ARM_ENV[@]}"
    if ! arm_pin_check "$arm"; then
      printf '    ⊘  skipped: %s\n' "$ARM_PIN_NOTE"
      emit_row "$scenario" "$SCENARIO_WORKLOAD" "$SCENARIO_DOOR" "$arm" "" "" unknown none unknown unknown "" \
               skipped no "$ARM_PIN_NOTE"
      echo
      continue
    fi
    record "model_pin" "$ARM_PIN_NOTE"

    # The answer key is built where the model cannot read it. A checker file in
    # the workspace is a benchmark that hands out its own solutions.
    judge="$cell/judge"
    mkdir -p "$judge"
    if ! scenario_fixture "$work" "$judge"; then
      printf '    ✗  the fixture did not generate\n'
      emit_row "$scenario" "$SCENARIO_WORKLOAD" "$SCENARIO_DOOR" "$arm" "" "" unknown none unknown unknown "" \
               fail no "fixture failed"
      echo
      continue
    fi

    # ── compose ───────────────────────────────────────────────────────────
    prompt=""; plan="$cell/turns.tsv"
    if [ "$SCENARIO_DOOR" = "print" ]; then
      prompt="$(scenario_prompt "$work")"
      printf '%s' "$prompt" > "$cell/prompt.txt"
      arm_print_argv "$arm" "$work" "$prompt" || continue
    else
      scenario_turns "$work" "$plan"
      if ! arm_tui_argv "$arm" "$work" "$SCENARIO_CAP_S"; then
        printf '    ⊘  unsupported: %s\n' "$ARM_DOOR_NOTE"
        emit_row "$scenario" "$SCENARIO_WORKLOAD" "$SCENARIO_DOOR" "$arm" "" "" unknown none unknown unknown "" \
                 unsupported no "$ARM_DOOR_NOTE"
        echo
        continue
      fi
    fi
    quote_argv "${ARGV[@]}" > "$cell/argv.txt"

    if [ "$DRY_RUN" = "1" ]; then
      printf '   '; quote_argv "${ARGV[@]}"
      printf '    env:     %s\n' "${ARM_ENV[*]:-none}"
      printf '    door:    %s (%s)\n' "$SCENARIO_DOOR" "${ARM_DOOR_NOTE:-print door: one message, no interaction}"
      printf '    fixture: %s file(s)\n' "$(find "$work" -type f 2>/dev/null | wc -l | tr -d ' ')"
      [ -f "$plan" ] && printf '    turns:   %s\n' "$(wc -l < "$plan" | tr -d ' ')"
      echo
      continue
    fi

    record_config "$cell/config.txt" \
      "run_id:$CONV_RUN_ID" "scenario:$scenario" "workload:$SCENARIO_WORKLOAD" \
      "door:$SCENARIO_DOOR" "arm:$arm" "binary:$(arm_bin "$arm")" \
      "version:$(version_of "$arm")" "model_pin:$CONV_MODEL" \
      "model_arg:$(arm_model_arg "$arm")" "allowlist:$CONV_ALLOWLIST" \
      "effort_requested:$EFFORT" "effort_sent:$ARM_EFFORT_SENT" \
      "role_pin:$ARM_ROLE_PIN ($ARM_ROLE_NOTE)" "guard:${ARM_GUARD:-off} ${ARM_GUARD_NOTE:-}" \
      "isolation:$ARM_ISOLATION" \
      "arm_env:${ARM_ENV[*]:-none}" "carried_env:$CONV_CARRIED" \
      "cap_s:$SCENARIO_CAP_S" "workspace:$work"

    # ── run ───────────────────────────────────────────────────────────────
    #
    # CHILD_ENV was built above: only PATH, HOME, TERM, LANG, TMPDIR, the
    # OpenRouter key and the arm's own variables survive. A peer that never sees
    # another provider's key cannot bill one.
    started="$(python3 -c 'import time; print(time.monotonic())')"; code=""; timed_out=0
    if [ "$SCENARIO_DOOR" = "print" ]; then
      # The cap is a spend backstop, not a work limit: a harness still running
      # at the cap has produced nothing and is recorded as a timeout, and on a
      # metered key leaving it running is how a benchmark becomes a bill.
      ( cd "$(arm_print_cwd "$arm" "$work")" && "${CHILD_ENV[@]}" \
          "$TIMEOUT_BIN" "$SCENARIO_CAP_S" "${ARGV[@]}" ) \
          > "$cell/stdout.log" 2> "$cell/stderr.log"
      code=$?
      # timeout(1) reports 124 when it had to kill, and a shell reports 128+n
      # for a signalled child. Both are the cap, not a verdict about the work.
      case "$code" in 124|137|143) timed_out=1 ;; esac
    else
      tmux_door_run "$arm" "$work" "$SCENARIO_CAP_S" "$cell" "$plan"
      code="n/a"
      [ "$DOOR_ENDED" = "cap" ] && timed_out=1
      : > "$cell/stdout.log"
    fi
    wall="$(python3 -c 'import sys,time; print(round(time.monotonic()-float(sys.argv[1]),3))' "$started")"

    # The conversation is hosted by default, so a cell that ends without
    # stopping its host leaves a daemon behind. This stops that host and only
    # that one, and it happens before the guard closes so nothing outlives the
    # thing that keeps it on the allowlist.
    if [ "$DRY_RUN" != "1" ] && [ "$UNGUARDED" != "1" ]; then
      arm_host_stop "$arm" "$cell" "$work"
    fi

    # Did the cell's only route upstream survive the cell? A guard that exits
    # mid-run leaves the harness dialling a closed port, and everything after
    # that — no reply, no file, no receipt — is about the rig and not about the
    # harness. It has happened once in this lane (notes/pilot-02-03.md), and it
    # arrived looking exactly like a product failure.
    guard_died=0
    if [ "$DRY_RUN" != "1" ] && [ "$UNGUARDED" != "1" ] && ! guard_alive; then
      guard_died=1
    fi

    # ── receipts ──────────────────────────────────────────────────────────
    receipt="$cell/receipt.json"
    # Stop the owned host above and close the ledger before reading it. Billing
    # recovery runs after the measured user interaction and cannot change it.
    guard_stop
    usage_ledger="$cell/guard-usage.jsonl"
    if [ "$UNGUARDED" != "1" ] && [ -s "$usage_ledger" ]; then
      if python3 "$CONV_LIB/reconcile.py" "$usage_ledger" "$cell/guard-reconciled.jsonl" 2>> "$cell/stderr.log"; then
        usage_ledger="$cell/guard-reconciled.jsonl"
      fi
    fi
    python3 "$CONV_LIB/receipts.py" --kind "$(arm_receipt_kind "$arm")" \
      --path "$(arm_receipt_path "$arm" "$cell" "$SCENARIO_DOOR")" --stdout "$cell/stdout.log" \
      --guard-usage "$usage_ledger" \
      --out "$receipt" --reply "$cell/reply.txt" 2>> "$cell/stderr.log"

    read -r cost cost_source tokens_in tokens_out turns models <<EOF
$(CONV_RECEIPT="$receipt" python3 -c '
import json, os
got = json.load(open(os.environ["CONV_RECEIPT"]))
def show(value):
    return "unknown" if value is None else value
print(show(got["cost_usd"]), got["cost_source"], show(got["tokens_in"]),
      show(got["tokens_out"]), got["turns"], ",".join(got["models"]) or "none")
')
EOF

    # For the interactive door the transcript IS the reply: what the person saw.
    reply="$cell/reply.txt"
    [ "$SCENARIO_DOOR" = "interactive" ] && reply="$cell/scrollback.txt"

    # ── judge ─────────────────────────────────────────────────────────────
    verdict=""
    # unjudged names a cell whose outcome says nothing about the harness. Its
    # scenario checks do not run, because running them would produce a column of
    # failures caused by this suite and attributed to the arm.
    unjudged=""
    if [ "$guard_died" = "1" ]; then
      unjudged="the forwarding guard exited during this cell (status ${GUARD_EXIT:-unknown})"
      conv_warn "$unjudged — see $cell/guard-audit.jsonl.err"
      note "$unjudged; nothing here is evidence about $arm"
      verdict="skipped"
      comparable="no"
      comparable_reason="${comparable_reason:+$comparable_reason; }the guard died mid-cell"
    fi
    if [ "$timed_out" = "1" ]; then
      fail "the cell hit its ${SCENARIO_CAP_S}s cap and was stopped"
      verdict="timeout"
    fi

    if [ -n "$unjudged" ]; then
      :
    elif [ "$SCENARIO_DOOR" = "print" ]; then
      # The exit code is a fact the row must carry, and a non-zero exit is a
      # failure even when the reply looks fine: a script downstream branches on
      # it, and a suite that ignores it would never notice it being dropped.
      [ "$timed_out" = "1" ] || check_eq "the harness exited 0" 0 "$code"
      # Exit 0 with an empty reply is what a refused upstream call looks like:
      # the CLI streams its ceremony, gets nothing back, and leaves quietly.
      check_ge "the harness produced a reply" 1 "$(wc -c < "$cell/reply.txt" | tr -d ' ')"
    else
      record "door_ended" "$DOOR_ENDED"
      record "door_markers" "${ARM_DOOR_NOTE:-none}"
      case "$DOOR_ENDED" in
        idle) pass "the conversation settled on its own" ;;
        no-midwork-window)
          # The mid-work turn was never sent, because there was no mid-work to
          # send it into. The cell is unexercised, and an unexercised cell is a
          # failure: the only other option is to call "nothing happened" a pass.
          fail "no window of work to steer was ever observed — the scenario did not happen"
          ;;
        noready)
          # A screen WAS drawn and these markers did not match it. That is a gap
          # in this suite's calibration, not a harness failure — pi's status bar
          # says "(guard)" on a guarded run where the calibration expected
          # "(openrouter)", and reporting that as a crash blames a product for a
          # regex. Nothing was driven, so nothing is claimed: `unsupported` is
          # the word for a cell this suite could not honestly drive, and it is
          # not a pass.
          conv_warn "the driver did not recognise $arm's composer — see $cell/screen.txt"
          note "a screen was drawn that the ready marker ($ARM_READY_RE) does not match"
          unjudged="the driver could not recognise this harness's composer"
          verdict="unsupported"
          comparable="no"
          comparable_reason="${comparable_reason:+$comparable_reason; }composer not recognised (benchmark calibration)"
          ;;
        noframe) fail "the composer never drew anything (see tmux.err)"; verdict="crash" ;;
        crash)   fail "the session died"; verdict="crash" ;;
        cap)     verdict="timeout" ;;
        *)       fail "the driver ended in an unrecognised state: ${DOOR_ENDED:-empty}" ;;
      esac
      [ -n "$unjudged" ] || \
        check_ge "every scripted turn was sent" "$(grep -acE '^[a-z]' "$plan")" "$DOOR_TURNS_SENT"
    fi

    # The open-model law, enforced on what was actually billed rather than on
    # what was asked for. A role, a title call or a fallback that resolved
    # elsewhere lands here.
    if [ "$models" = "none" ]; then
      record "model_verified" "no"
      comparable="no"
      comparable_reason="${comparable_reason:+$comparable_reason; }receipts do not name the billed model"
    else
      offenders="$(models_outside_allowlist $(echo "$models" | tr ',' ' ') | tr '\n' ' ')"
      if [ -n "${offenders// /}" ]; then
        fail "a model outside the allowlist was billed: $offenders"
        comparable="no"
        comparable_reason="${comparable_reason:+$comparable_reason; }off-allowlist model"
      else
        pass "every billed model is on the open-model allowlist ($models)"
      fi
    fi

    if [ "$cost_source" = "none" ]; then
      # The receipt knows WHY there is no figure — no usage at all, or a
      # self-reported zero against real tokens — and a row that only says
      # "unknown" leaves the reader to guess which.
      receipt_note="$(CONV_RECEIPT="$receipt" python3 -c '
import json, os
got = json.load(open(os.environ["CONV_RECEIPT"]))
print("; ".join(got.get("notes") or []))
')"
      record "cost" "unknown${receipt_note:+ — $receipt_note}"
      comparable="no"
      comparable_reason="${comparable_reason:+$comparable_reason; }cost not fully accounted"
    else
      record "cost_usd" "$cost"
    fi
    record "tokens" "in=$tokens_in out=$tokens_out"

    if [ -n "$unjudged" ]; then
      note "the scenario's own checks did not run: $unjudged"
    else
      scenario_check "$work" "$reply" "$cell" "$judge"
    fi

    # A cap or a dead session is the most specific thing that can be said about
    # a cell, and it keeps its word: everything downstream of a killed harness
    # fails too, and letting those failures rename the outcome would hide the
    # cause. The checks still ran and are all in the receipt either way.
    [ -n "$verdict" ] || verdict="$(cell_verdict)"

    emit_row "$scenario" "$SCENARIO_WORKLOAD" "$SCENARIO_DOOR" "$arm" "$wall" "$code" \
             "$cost" "$cost_source" "$tokens_in" "$tokens_out" "$turns" \
             "$verdict" "$comparable" "$comparable_reason"

    printf '    %s  %ss  cost=%s  tokens=%s/%s  comparable=%s\n\n' \
      "$(printf '%s' "$verdict" | tr '[:lower:]' '[:upper:]')" "$wall" "$cost" "$tokens_in" "$tokens_out" "$comparable"
  done
done

# omp's isolation is a named profile under the operator's own home rather than a
# directory under the cell, so the run takes its own profile away with it unless
# it was asked to keep the evidence.
if [ "$KEEP_STATE" != "1" ] && [ "$DRY_RUN" != "1" ]; then
  for path in "${CLEANUP_PATHS[@]:-}"; do
    [ -n "$path" ] && [ -d "$path" ] && rm -rf "$path"
  done
fi

echo
if [ "$DRY_RUN" = "1" ]; then
  echo "dry run — fixtures, argv and turn plans composed; nothing ran and nothing was spent"
  exit 0
fi

printf 'pass %s   fail %s   timeout %s   crash %s   skipped %s   unsupported %s\n' \
  "$TOTAL_PASS" "$TOTAL_FAIL" "$TOTAL_TIMEOUT" "$TOTAL_CRASH" "$TOTAL_SKIP" "$TOTAL_UNSUP"
echo "evidence: $CONV_OUT"
echo "history:  $CSV"
echo
echo "per-workload comparison:  bench/conversation/summary.sh $CONV_OUT/results.jsonl"

# Skipped and unsupported cells are NOT failures and are NOT successes; they are
# counted above and excluded from every claim. Only what actually ran badly
# moves the exit code.
[ $((TOTAL_FAIL + TOTAL_TIMEOUT + TOTAL_CRASH)) -eq 0 ]
