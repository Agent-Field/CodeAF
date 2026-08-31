#!/usr/bin/env bash
#
# ── THIS SCRIPT HAS NEVER BEEN RUN ──────────────────────────────────────────
#
# It is the blind live A/B that bench/lanelab/REPORT.md says is worth doing
# ONLY AFTER the three defects in that report are fixed. As written it defaults
# to --dry-run and prints what it would do; running it for real needs --go, and
# --go spends money.
#
# WHAT IT COSTS WHEN IT IS RUN. Two arms x $PROMPTS prompts x $REPS replicates,
# through the real aforge CLI on the real model. At the default 40 prompts and
# 3 replicates that is 240 sessions. bench/ab-routing measured this shape of
# experiment at roughly $0.02-$0.06 a session on ds-v4-flash, so budget
# $6-$15 and about two hours of wall clock at CONCURRENCY=6. Arm B additionally
# pays for hedges: REPORT.md finding 3 measured the hedge firing on up to 95% of
# long-generation requests, so if that defect is not fixed first, arm B's bill
# can be TWICE arm A's and the experiment will have measured the bug.
#
#   bench/lanelab/live.sh                      # dry run, prints the plan
#   ARM=a bench/lanelab/live.sh --go           # arm A, for real
#   ARM=b bench/lanelab/live.sh --go           # arm B, for real
#   bench/lanelab/live.sh --compare            # the diff, arms relabelled X/Y
#
# ── THE DESIGN, IN THE SPIRIT OF bench/ab-routing/DESIGN.md SECTION 1 ───────
#
# TWO ARMS OVER THE SAME PROMPT SET. Arm A is today's shipped configuration,
# untouched: no environment set at all. The point of a baseline is that it is
# the thing already running, not a reconstruction of it, so the arm-A branch of
# the case block below is deliberately empty and must stay that way. Arm B sets
# one variable, which is the entire difference between the arms.
#
# THE SAME PRICE TABLE FOR BOTH ARMS. Cost is read from the usage ledger's own
# `usd`, which both arms compute from the same catalog, and the per-lane
# tariffs come from the endpoint sheet both arms fetch. Neither arm is allowed
# to price itself. If arm B ever needs a price table arm A does not have, the
# experiment is broken and this script should refuse rather than paper over it.
#
# THE METRIC IS THE DIFF OF THE TWO LEDGERS, NEVER A COUNT OF WINS. Both arms
# write ~/.aforge/v3/usage.jsonl. The comparison is the distribution of
# `ttft_ms`, `tps`, and `usd` and the totals of `hedged` and `hedge_waste_usd`,
# per arm, side by side. A count of which arm was faster on more prompts is not
# evidence: it answers a different question from the one anybody has, which is
# what the p90 wait and the bill are.
#
# ONE SESSION AT A TIME, ON PURPOSE. bench/ab-routing runs its cells in
# parallel because its metric is a grade and latency is only a cost. Here
# latency IS the metric: six concurrent sessions would queue against the same
# endpoints and the arms would be measuring their own contention. Sequential is
# slower and it is the only way these numbers mean anything.
#
# THE ARMS ARE BLIND TO THE ANALYSIS. Each arm writes arm-$ARM.jsonl. The
# compare step reads both files, computes every number, and only then attaches
# the labels -- and it attaches X and Y, not A and B, so that whoever reads the
# output has to look up which was which deliberately rather than seeing the
# answer they expected. The mapping is written to arm-map.txt at compare time.
#
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"

ARM="${ARM:-a}"
PROMPTS="${PROMPTS:-$HERE/prompts.txt}"
REPS="${REPS:-3}"
AFORGE_BIN="${AFORGE_BIN:-$ROOT/bin/aforge}"
OUT="${OUT:-$HERE/live}"
DRY=1
MODE="run"

for a in "$@"; do
  case "$a" in
    --go)      DRY=0 ;;
    --dry-run) DRY=1 ;;
    --compare) MODE="compare" ;;
    *) echo "unknown flag: $a" >&2; exit 2 ;;
  esac
done

# ── the one thing that differs between the arms ─────────────────────────────
#
# If the lane router lands under a different variable name than this, this case
# block is the only edit the experiment needs. bench/ab-routing guessed the name
# wrong once and that was the only line that had to change, which is the point
# of keeping the difference to two lines.
case "$ARM" in
  a) ARM_LABEL="shipped configuration, untouched"
     ARM_ENV=() ;;
  b) ARM_LABEL="lane router on"
     ARM_ENV=("AFORGE_LANES=on") ;;
  *) echo "ARM must be a or b" >&2; exit 2 ;;
esac

# ── budgets and backstops ───────────────────────────────────────────────────
# Neither is a work limit. Both exist so one wedged session cannot turn a $10
# experiment into an unbounded bill.
SESSION_TIMEOUT="${SESSION_TIMEOUT:-8m}"
TOKEN_BUDGET="${TOKEN_BUDGET:-200000}"
SPEND_CAP_USD="${SPEND_CAP_USD:-20}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "missing: $1" >&2; exit 1; }; }

plan() {
  local n="0"
  [ -f "$PROMPTS" ] && n="$(wc -l < "$PROMPTS")"
  cat <<EOF
arm:          $ARM  ($ARM_LABEL)
env:          ${ARM_ENV[*]:-<none: this is the shipped default and must stay empty>}
binary:       $AFORGE_BIN
prompts:      $PROMPTS  ($n prompts)$([ "$n" = "0" ] && echo "  <-- WRITE THIS FIRST: one prompt per line")
replicates:   $REPS      (sequential; see the header on why not parallel)
cells:        $(( n * REPS ))
results:      $OUT/arm-$ARM.jsonl
ledger:       \$HOME/.aforge/v3/usage.jsonl  (copied per cell, diffed at compare)
timeouts:     $SESSION_TIMEOUT per session, $TOKEN_BUDGET tokens, \$$SPEND_CAP_USD cap
EOF
}

run_arm() {
  need jq
  [ -x "$AFORGE_BIN" ] || { echo "build it first: make build" >&2; exit 1; }
  [ -f "$PROMPTS" ] || { echo "no prompt set at $PROMPTS" >&2; exit 1; }
  mkdir -p "$OUT"
  : > "$OUT/arm-$ARM.jsonl"

  local rep i=0
  for rep in $(seq 1 "$REPS"); do
    while IFS= read -r prompt; do
      [ -n "$prompt" ] || continue
      i=$((i + 1))
      # Each cell is one session in a fresh workspace with a fresh profile, so
      # cell 20 is a repeat of cell 1 rather than a continuation of it. The lane
      # belief is meant to persist ACROSS sessions, so arm B is run with a
      # shared profile in a second pass -- that is a separate experiment and it
      # is not this one.
      local ws ledger_before
      ws="$(mktemp -d)"
      ledger_before="$(mktemp)"
      cp "$HOME/.aforge/v3/usage.jsonl" "$ledger_before" 2>/dev/null || : > "$ledger_before"

      env "${ARM_ENV[@]}" \
        AFORGE_TOKEN_BUDGET="$TOKEN_BUDGET" \
        timeout "$SESSION_TIMEOUT" \
        "$AFORGE_BIN" ask "$prompt" -w "$ws" >/dev/null 2>&1 || true

      # THE MEASUREMENT IS THE DIFF OF THE LEDGER, not anything the session
      # printed. The new lane fields -- lane, ttft_ms, tps, hedged,
      # hedge_waste_usd -- are written by internal/session/usage_ledger.go.
      diff <(cat "$ledger_before") "$HOME/.aforge/v3/usage.jsonl" \
        | sed -n 's/^> //p' \
        | jq -c --arg arm "$ARM" --argjson cell "$i" --argjson rep "$rep" \
              '{arm:$arm, cell:$cell, rep:$rep,
                lane:(.lane//null), model:(.model//null),
                ttft_ms:(.ttft_ms//null), tps:(.tps//null),
                hedged:(.hedged//false), hedge_waste_usd:(.hedge_waste_usd//0),
                usd:(.usd//0), in:(.in//0), out:(.out//0)}' \
        >> "$OUT/arm-$ARM.jsonl" || true

      rm -rf "$ws" "$ledger_before"

      # The live spend cap. Money already committed is allowed to land; new
      # cells stop.
      local spent
      spent="$(jq -s 'map(.usd)|add // 0' "$OUT/arm-$ARM.jsonl")"
      if awk -v s="$spent" -v c="$SPEND_CAP_USD" 'BEGIN{exit !(s>=c)}'; then
        echo "spend cap \$$SPEND_CAP_USD reached at cell $i; stopping" >&2
        return 0
      fi
    done < "$PROMPTS"
  done
}

compare() {
  need jq
  local A="$OUT/arm-a.jsonl" B="$OUT/arm-b.jsonl"
  [ -s "$A" ] && [ -s "$B" ] || { echo "run both arms first" >&2; exit 1; }

  # BLIND. The two files are relabelled X and Y before a single number is
  # printed, and the mapping is written to a file the reader has to open on
  # purpose. It is not security; it is a way of not letting the expected answer
  # arrive before the numbers do.
  local X Y
  if [ "$(( RANDOM % 2 ))" -eq 0 ]; then X="$A"; Y="$B"; else X="$B"; Y="$A"; fi
  { echo "X = $(basename "$X")"; echo "Y = $(basename "$Y")"; } > "$OUT/arm-map.txt"

  local f
  for f in "$X:X" "$Y:Y"; do
    jq -s --arg tag "${f#*:}" '
      def pctile(p): sort | .[((length-1) * p) | floor];
      { arm: $tag,
        n: length,
        ttft_p50: ([.[]|.ttft_ms|select(.!=null)] | pctile(0.50)),
        ttft_p90: ([.[]|.ttft_ms|select(.!=null)] | pctile(0.90)),
        ttft_p99: ([.[]|.ttft_ms|select(.!=null)] | pctile(0.99)),
        tps_p50:  ([.[]|.tps|select(.!=null)]     | pctile(0.50)),
        usd_per_1k: (([.[]|.usd] | add // 0) / length * 1000),
        hedge_pct: (([.[]|select(.hedged)] | length) / length * 100),
        hedge_waste_usd: ([.[]|.hedge_waste_usd] | add // 0),
        lanes: ([.[]|.lane|select(.!=null)] | group_by(.) | map({(.[0]): length}) | add)
      }' "${f%%:*}"
  done

  echo
  echo "the arm labels are in $OUT/arm-map.txt -- read the numbers first."
}

if [ "$MODE" = "compare" ]; then
  compare
  exit 0
fi

plan
if [ "$DRY" -eq 1 ]; then
  echo
  echo "DRY RUN. Nothing was sent and nothing was spent."
  echo "Read bench/lanelab/REPORT.md before passing --go: the simulator says"
  echo "this design fails its own ship gate on 4 of 6 comparisons, and arm B"
  echo "can cost twice arm A until the hedge budget is fixed."
  exit 0
fi

echo
echo "RUNNING FOR REAL. Ctrl-C stops it; cells already sent are already paid for."
run_arm
echo "wrote $OUT/arm-$ARM.jsonl"
echo "when both arms are done:  bench/lanelab/live.sh --compare"
