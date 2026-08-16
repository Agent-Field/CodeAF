#!/usr/bin/env bash
# Run the end-to-end regression battery: one scratch directory per cell, one
# harness invocation, then an autopsy of the journal the run left behind and a
# set of assertions that the run does not get to grade.
#
# This spends real money and is ON DEMAND. It is deliberately not wired into
# `make check` and must never be: `make check` is a thing you run before every
# commit, and this is a thing you run when you have changed the tasker and want
# to know what it cost you.
#
# Read bench/e2e/README.md before trusting a row, in particular the model pin —
# the aforge and peer arms name the same model two different ways, and the
# runner refuses to compare them unless it can show they are the same model.
#
# Usage:
#   bench/e2e/run.sh                        every cell, aforge arm
#   bench/e2e/run.sh --cells lookup         one cell
#   bench/e2e/run.sh --arm pi               the same tasks through pi
#   bench/e2e/run.sh --dry-run              compose every invocation, run none
#   bench/e2e/run.sh compare                latest vs previous, per cell
set -uo pipefail

E2E_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export E2E_ROOT
REPO_ROOT="$(cd "$E2E_ROOT/../.." && pwd)"

# ── parameters ──────────────────────────────────────────────────────────────

# The model pin, in two spellings of one model.
#
# E2E_MODEL is what aforge is given. The leading `~` is aforge's own alias
# syntax for a floating tag and is not an OpenRouter model id; it is also
# aforge's shipped default (internal/config/config.go, DefaultModel), which is
# the point — the battery measures the tasker as configured, not as specially
# tuned for a benchmark.
#
# E2E_PEER_MODEL is what pi and opencode are given, because neither understands
# aforge's alias syntax: passing the `~` form to pi returns
# `400 ... is not a valid model ID`. It is the concrete slug that alias serves,
# recorded in bench/probelab/REPORT.md.
#
# Changing either without changing the other silently compares two models and
# makes every historical row in e2e.csv incomparable. The `model` column exists
# so that can be seen rather than assumed, and the peer arms refuse to run when
# the pin cannot be verified — see require_peer_model.
E2E_MODEL="${E2E_MODEL:-~deepseek/deepseek-v4-flash-latest}"
E2E_PEER_MODEL="${E2E_PEER_MODEL:-deepseek/deepseek-v4-flash-0731}"

# The binary under test. bin/ is gitignored, so a git worktree has no build of
# its own and the checkout's binary is the one to use; falling back to PATH
# covers an installed aforge. It is never built here — which build is being
# measured is the caller's decision, and `make check` is where building belongs.
if [ -z "${AFORGE_BIN:-}" ]; then
  if [ -x "$REPO_ROOT/bin/aforge" ]; then
    AFORGE_BIN="$REPO_ROOT/bin/aforge"
  else
    AFORGE_BIN="$(command -v aforge || echo "$REPO_ROOT/bin/aforge")"
  fi
fi
PI_BIN="${PI_BIN:-pi}"
OPENCODE_BIN="${OPENCODE_BIN:-opencode}"

ALL_CELLS="lookup compare3 fanin6 bughunt feature bundle3 dynamic"

RESULTS_DIR="${RESULTS_DIR:-$REPO_ROOT/bench-results}"
CSV="${CSV:-$RESULTS_DIR/e2e.csv}"
RUN_DIR="${RUN_DIR:-$RESULTS_DIR/e2e/$(date +%Y%m%d-%H%M%S)}"

ARM="aforge"
CELLS="$ALL_CELLS"
DRY_RUN=0

# ── arguments ───────────────────────────────────────────────────────────────
SUBCOMMAND=""
case "${1:-}" in
  compare) SUBCOMMAND="compare"; shift ;;
esac

while [ $# -gt 0 ]; do
  case "$1" in
    --arm)      ARM="${2:?--arm needs a value}"; shift 2 ;;
    --arm=*)    ARM="${1#*=}"; shift ;;
    --cells)    CELLS="$(echo "${2:?--cells needs a value}" | tr ',' ' ')"; shift 2 ;;
    --cells=*)  CELLS="$(echo "${1#*=}" | tr ',' ' ')"; shift ;;
    --dry-run)  DRY_RUN=1; shift ;;
    -h|--help)  sed -n '1,30p' "$0"; exit 0 ;;
    *)          echo "unknown argument: $1" >&2; exit 1 ;;
  esac
done

# ── compare subcommand ──────────────────────────────────────────────────────
if [ "$SUBCOMMAND" = "compare" ]; then
  exec python3 "$E2E_ROOT/lib/compare.py" "$CSV"
fi

# ── prerequisites ───────────────────────────────────────────────────────────
TIMEOUT_BIN="$(command -v timeout || command -v gtimeout || true)"
if [ -z "$TIMEOUT_BIN" ]; then
  echo "need timeout(1) — brew install coreutils" >&2
  exit 1
fi
for tool in sqlite3 python3 awk; do
  command -v "$tool" >/dev/null || { echo "need $tool" >&2; exit 1; }
done
# A missing binary must be said here, not discovered as exit 127 inside a cell:
# that failure writes a row indistinguishable from a run that produced nothing,
# and on the peer arms it would do so after the fixtures had been built.
if [ "$ARM" = "aforge" ] && [ ! -x "$AFORGE_BIN" ]; then
  echo "no aforge binary at $AFORGE_BIN — build one (make build) or set AFORGE_BIN" >&2
  echo "note: bin/ is gitignored, so a fresh worktree has no build of its own" >&2
  exit 1
fi
# go is only needed by the code cells, but every one of those needs it, so a
# missing toolchain should be said now rather than after twenty minutes of spend.
case " $CELLS " in
  *" bughunt "*|*" feature "*|*" bundle3 "*|*" dynamic "*)
    command -v go >/dev/null || { echo "need go — the code cells run its test suite" >&2; exit 1; }
    ;;
esac

# shellcheck source=lib/autopsy.sh
source "$E2E_ROOT/lib/autopsy.sh"
# shellcheck source=lib/assert.sh
source "$E2E_ROOT/lib/assert.sh"

# ── the model pin, enforced ─────────────────────────────────────────────────

# pi_knows_model asks pi's own catalog whether it can pin a slug exactly. The
# catalog prints one row per model with the provider first and the id second,
# so an exact field match is the question — a substring match would accept
# deepseek-v4-flash when deepseek-v4-flash-0731 was asked for.
pi_knows_model() {
  local model="$1"
  "$PI_BIN" --list-models "$model" 2>/dev/null \
    | awk -v want="$model" 'NF >= 2 && $2 == want { found = 1 } END { exit !found }'
}

# require_peer_model is the guard against the quietest way this battery could
# lie: running pi on a different model than aforge and putting both rows in one
# CSV. If the peer arm cannot pin the exact slug, the arm is skipped with the
# reason recorded, and no row is written that could be read as a comparison.
SKIP_REASON=""
require_peer_model() {
  case "$ARM" in
    pi)
      if [ "$DRY_RUN" = "1" ]; then return 0; fi
      if ! command -v "$PI_BIN" >/dev/null; then
        SKIP_REASON="pi not installed"
        return 1
      fi
      if ! pi_knows_model "$E2E_PEER_MODEL"; then
        SKIP_REASON="pi cannot pin $E2E_PEER_MODEL — refusing to compare a different model"
        return 1
      fi
      ;;
    opencode)
      if [ "$DRY_RUN" = "1" ]; then return 0; fi
      if ! command -v "$OPENCODE_BIN" >/dev/null; then
        SKIP_REASON="opencode not installed"
        return 1
      fi
      ;;
  esac
  return 0
}

# model_for_arm is the slug this arm is handed, and the value that lands in the
# CSV's model column.
model_for_arm() {
  case "$ARM" in
    aforge) echo "$E2E_MODEL" ;;
    *)      echo "$E2E_PEER_MODEL" ;;
  esac
}

# ── invocation ──────────────────────────────────────────────────────────────

# compose_argv builds the argv for one cell and executes nothing, so --dry-run
# can print exactly what the real run would launch rather than an approximation
# that drifts the first time a flag moves. Same discipline as bench/run.sh.
#
# aforge's `do` takes --timeout in whole seconds, not a duration string.
compose_argv() {
  local dir="$1" task="$2" budget="$3"
  case "$ARM" in
    aforge)
      ARGV=("$TIMEOUT_BIN" "$((budget + 120))" "$AFORGE_BIN" do "$task"
            -w "$dir" -keep -yes-spend -model "$E2E_MODEL" -timeout "$budget")
      ;;
    pi)
      ARGV=("$TIMEOUT_BIN" "$((budget + 120))" "$PI_BIN" -p
            --provider openrouter --model "$E2E_PEER_MODEL" "$task")
      ;;
    opencode)
      ARGV=("$TIMEOUT_BIN" "$((budget + 120))" "$OPENCODE_BIN" run
            -m "openrouter/$E2E_PEER_MODEL" "$task")
      ;;
    *)
      echo "unknown arm: $ARM" >&2
      return 1
      ;;
  esac
}

# quote_argv prints an argv a person could paste back into a shell. The task is
# a paragraph and would drown the line, so it is shown as a head and a length.
quote_argv() {
  local part
  for part in "$@"; do
    case "$part" in
      *[!A-Za-z0-9@%+=:,./_-]*)
        if [ "${#part}" -gt 60 ]; then
          printf " '%s… (%d chars)'" "$(echo "$part" | head -1 | cut -c1-56)" "${#part}"
        else
          printf " '%s'" "$part"
        fi
        ;;
      *) printf ' %s' "$part" ;;
    esac
  done
  printf '\n'
}

# csv_field makes a value safe for a CSV cell: no commas, no newlines.
csv_field() {
  echo "$1" | tr '\n' ' ' | tr ',' ';' | sed 's/  */ /g; s/^ *//; s/ *$//'
}

# ── the grid ────────────────────────────────────────────────────────────────
GIT_SHA="$(cd "$REPO_ROOT" && git rev-parse --short HEAD 2>/dev/null || echo unknown)"
TODAY="$(date +%Y-%m-%d)"

echo "battery:  bench/e2e"
echo "arm:      $ARM"
echo "model:    $(model_for_arm)"
echo "cells:    $CELLS"
echo "binary:   $([ "$ARM" = "aforge" ] && echo "$AFORGE_BIN" || echo "$ARM")"
echo "git:      $GIT_SHA"
echo "results:  $CSV"
[ "$DRY_RUN" = "1" ] && echo "dry run:  composing fixtures and invocations only — no model call, no spend"
echo

if ! require_peer_model; then
  echo "arm $ARM skipped: $SKIP_REASON" >&2
  mkdir -p "$RESULTS_DIR"
  [ -f "$CSV" ] || echo "date,git_sha,cell,harness,wall_s,cost_usd,nodes,route,quality_pass,notes,model,exit" > "$CSV"
  for cell in $CELLS; do
    echo "$TODAY,$GIT_SHA,$cell,$ARM,,,,skipped,skipped,$(csv_field "$SKIP_REASON"),$(csv_field "$(model_for_arm)")," >> "$CSV"
  done
  echo "recorded $(echo "$CELLS" | wc -w | tr -d ' ') skipped rows in $CSV"
  exit 0
fi

mkdir -p "$RESULTS_DIR" "$RUN_DIR"
# The header is written once, ever. History is appended to and never rewritten:
# a battery whose baseline file gets recreated has no baselines.
[ -f "$CSV" ] || echo "date,git_sha,cell,harness,wall_s,cost_usd,nodes,route,quality_pass,notes,model,exit" > "$CSV"

TOTAL_FAILED=0
TOTAL_COST=0

for cell in $CELLS; do
  cell_file="$E2E_ROOT/cells/$cell/cell.sh"
  if [ ! -f "$cell_file" ]; then
    echo "no such cell: $cell" >&2
    continue
  fi

  # Reset the contract before sourcing, so a cell that forgets to define one of
  # these inherits nothing from the cell before it.
  unset CELL_TASK CELL_BUDGET CELL_GUARDS CELL_WALL_CEILING
  cell_fixture() { :; }
  cell_check() { :; }
  cell_shape() { :; }
  # shellcheck source=/dev/null
  source "$cell_file"

  : "${CELL_BUDGET:=900}"
  : "${CELL_WALL_CEILING:=$CELL_BUDGET}"

  cell_dir="$RUN_DIR/$ARM-$cell"
  work="$cell_dir/work"
  mkdir -p "$cell_dir"
  rm -rf "$work"
  mkdir -p "$work"

  printf '── %s (%s)\n' "$cell" "${CELL_GUARDS:-}"

  # Fixtures are deterministic and call nothing, so the dry run builds them too:
  # a fixture that no longer generates is most of what a wiring check can catch.
  if ! cell_fixture "$work"; then
    echo "    fixture failed" >&2
    continue
  fi

  compose_argv "$work" "$CELL_TASK" "$CELL_BUDGET" || continue

  if [ "$DRY_RUN" = "1" ]; then
    printf '   '
    quote_argv "${ARGV[@]}"
    printf '    fixture: %s file(s) under %s\n' \
      "$(find "$work" -type f 2>/dev/null | wc -l | tr -d ' ')" "${work#$REPO_ROOT/}"
    continue
  fi

  # aforge is told where to work with -w and is launched from here. pi and
  # opencode have no such flag: they edit the directory they are started in, so
  # the peer arms are launched from inside the fixture. Getting this wrong is
  # the failure mode bench/README.md warns about — a zero exit, zero changed
  # files, and a row that looks exactly like a real DNF.
  started=$(date +%s)
  if [ "$ARM" = "aforge" ]; then
    "${ARGV[@]}" >"$cell_dir/stdout.log" 2>"$cell_dir/stderr.log"
    code=$?
  else
    ( cd "$work" && "${ARGV[@]}" ) >"$cell_dir/stdout.log" 2>"$cell_dir/stderr.log"
    code=$?
  fi
  wall=$(( $(date +%s) - started ))

  # ── autopsy ─────────────────────────────────────────────────────────────
  #
  # The store is moved next to the cell's logs BEFORE anything reads it, for
  # two reasons. It is left in the system temp directory otherwise, where it
  # gets swept before anyone looks at it; and reading it where the engine just
  # put it means reading a WAL database whose sidecars may still need recovery,
  # which fails by answering nothing rather than by erroring. Moving it first
  # makes the copy the battery's own, and every number below comes from the
  # same file a person can go and open afterwards.
  cost="n/a"
  nodes=""
  route="n/a"
  db=""
  if [ "$ARM" = "aforge" ]; then
    store_home="$(store_dir_of "$cell_dir/stderr.log")"
    if [ -n "$store_home" ] && [ -d "$store_home" ]; then
      rm -rf "$cell_dir/store"
      mv "$store_home" "$cell_dir/store" 2>/dev/null || cp -R "$store_home" "$cell_dir/store" 2>/dev/null
    fi
    db="$cell_dir/store/graph.db"
    if [ -f "$db" ]; then
      cost="$(total_cost "$db")"
      nodes="$(node_count "$db")"
      route="$(scale_route "$db")"
    fi
  fi

  # ── assertions ──────────────────────────────────────────────────────────
  assert_begin
  check_lt "wall under the cell's ceiling" "$CELL_WALL_CEILING" "$wall"
  cell_check "$work" "$cell_dir/stdout.log" "$cell_dir/stderr.log" "$code"
  if [ "$ARM" = "aforge" ]; then
    if [ -f "$db" ]; then
      cell_shape "$db"
      note "model_billed=$(models_used "$db")"
      note "tokens=$(total_tokens "$db")"
    else
      fail "no journal — the run left no store to autopsy"
    fi
  else
    note "no-journal-arm"
  fi

  verdict="$(cell_verdict)"
  [ "$verdict" = "pass" ] || TOTAL_FAILED=$((TOTAL_FAILED + 1))
  [ "$cost" = "n/a" ] || TOTAL_COST="$(awk -v a="$TOTAL_COST" -v b="$cost" 'BEGIN{printf "%.6f", a+b}')"

  echo "$TODAY,$GIT_SHA,$cell,$ARM,$wall,$cost,$nodes,$route,$verdict,$(csv_field "$CELL_NOTES"),$(csv_field "$(model_for_arm)"),$code" >> "$CSV"

  printf '    %s  %ss  %s  %s nodes  %s  exit %s\n\n' \
    "$([ "$verdict" = "pass" ] && echo "PASS" || echo "FAIL")" \
    "$wall" "\$$cost" "${nodes:-n/a}" "$route" "$code"
done

echo
if [ "$DRY_RUN" = "1" ]; then
  echo "dry run — fixtures generated, nothing was run or spent"
else
  echo "wrote $CSV"
  echo "evidence in $RUN_DIR"
  printf 'cells failed: %s   total spend: $%s\n' "$TOTAL_FAILED" "$TOTAL_COST"
  echo
  echo "compare against the previous run:  bench/e2e/run.sh compare"
fi
