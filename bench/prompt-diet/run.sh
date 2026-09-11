#!/usr/bin/env bash
# run.sh — one branch's whole parity measurement, in four layers.
#
# THE QUESTION THIS ANSWERS is the owner's acceptance for the prompt-diet wave,
# in their own words: "real e2e with dev and our branch, same other changes,
# just this diff alone, and see if we are on parity but efficient". Parity is an
# OUTCOME claim — every subtest and every cell ends the same way or better — and
# efficiency is a TOKEN claim: fewer prompt tokens per turn for that same
# outcome. A byte count alone proves neither, which is why this exists at all
# (docs/design/prompt-diet/DESIGN.md §6, "Proof is the bench, not the byte
# count").
#
# THE RIG IS THIS CHECKOUT; THE SUBJECT IS THE BRANCH. The bench scripts, the
# scenario texts and the assertions all come from the checkout this file lives
# in, and only `bin/aforge` and the Go sources under test come from <branch>.
# That is deliberate and it is the only arrangement that can compare two
# branches at all: `dev` at 6aa6a946e has never heard of bench/prompt-diet, so a
# run that used the subject's own rig would have nothing to run on the baseline
# side — and a rig that moved with the subject would be measuring two harnesses
# rather than two prompts.
#
# IT RUNS ON THE SPARK. Layer B alone is seventeen minutes of a real binary in a
# real terminal against a real model, and the owner's standing order is that no
# full suite runs on the laptop. Nothing here refuses to run elsewhere — a
# refusal keyed off a hostname would be a lie on the next machine — but every
# recipe in docs/design/prompt-diet/BENCH.md is spelled for `ssh spark`.
#
# IT SPENDS REAL MONEY. Layers B and C call a real model. Layer A does not, and
# is worth running on its own while editing.
#
# Usage:
#   bench/prompt-diet/run.sh <branch-or-revision> <label> [options]
#
#   bench/prompt-diet/run.sh 6aa6a946e dev --layers a          # free, seconds
#   bench/prompt-diet/run.sh 6aa6a946e dev                     # the default sweep
#   bench/prompt-diet/run.sh prompt-diet/integrate diet        # the other side
#
# Options:
#   --layers a,b,c,d   which layers to run (default: a,c,d)
#   --model <id>       the catalog id every cell is pinned to
#   --scenarios <list> conversation scenarios (comma-separated, or "none")
#   --cells <list>     bench/e2e cells (comma-separated, or "none")
#   --suites <list>    tagged Go suites for layer B (comma-separated, or "none")
#   --out <dir>        where evidence lands (default ~/bench-diet-out/<label>, and
#                      it must be outside every checkout — see the note below)
#   --allowlist <ids>  extra catalog ids the run may legitimately have billed
#   --keep-worktree    leave the built worktree behind for a follow-up run
#   --reuse-worktree   build into an existing worktree instead of creating one
#   --dry-run          compose everything, spend nothing
set -uo pipefail

DIET_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RIG_ROOT="$(cd "$DIET_ROOT/../.." && pwd)"

# ── the layers ──────────────────────────────────────────────────────────────
#
# A  prefix   the static bill: `prompt + tools` bytes from the budget test.
#             Free, offline, and the only layer that is exact rather than
#             sampled — the wire's own prompt_tokens carry the transcript too.
# B  suites   the tagged Go suites that drive the real binary against a real
#             model. This is where PARITY is decided: a subtest that passed on
#             dev and fails on the diet is the whole answer, whatever the token
#             column says.
# C  cells    the existing bench batteries — bench/conversation (a conversation
#             turn, a handoff, work recalled) and bench/e2e (a task shape end to
#             end). These carry their own verdicts, costs and token counts.
# D  wire     the per-request ledger, rolled up from the guard's own usage file
#             (bench/conversation/lib/guard.py, which every cell already stands
#             in front of OpenRouter) and from aforge's built-in call log.
LAYERS="a,c,d"

# THE PIN IS THE ONE THE BATTERIES WERE CALIBRATED ON, and it is not the one
# this wave started with.
#
# MEASURED on the Spark, 2026-09-10. Pinned to `deepseek/deepseek-v4.1-flash`,
# cells died mid-turn with the provider's own sentence: "0 endpoints out of 1
# requested are available matching your guardrail restrictions and data policy
# … Paid model training violation (account settings): 1 endpoint excluded". It
# is intermittent and it is not aforge's fault twice over — the id is real and a
# bare curl to it answered three times out of three, from GMICloud and DeepInfra
# — but aforge asks for ONE endpoint per request and takes no fallback, so an
# endpoint this account's privacy settings exclude is a dead turn rather than a
# hop. It failed `research-brief`, which had passed twice, and `code-fix`, on
# the same afternoon, on the same build. A parity ruling cannot be made through
# that: the noise is bigger than the effect being measured.
#
# `deepseek/deepseek-v4-flash-0731` is the same family, is what every historical
# row in `bench/conversation` and `bench/e2e` was measured on, and its endpoints
# are ones this account allows. Comparability with that history is worth having
# anyway. DIET_MODEL or --model moves it; BENCH.md §1 carries the finding.
MODEL="${DIET_MODEL:-deepseek/deepseek-v4-flash-0731}"
# The scenarios are picked for what they exercise rather than for coverage:
# research-brief is a plain conversation turn through the print door, code-fix
# is the turn that becomes work, followup-while-working is a person typing while
# work is out, and work-result-recalled is finished work coming back into the
# conversation. Between them they touch every delivery class the diet moves.
SCENARIOS="${DIET_SCENARIOS:-research-brief,code-fix,followup-while-working,work-result-recalled}"
# `lookup` is the cheap machinery check (one node, ~$0.002); `bundle3` is the
# handoff shape — three parts, three leaves and a sink — which is what the
# routing table has to still get right after the page loses its handoff prose.
CELLS="${DIET_CELLS:-lookup,bundle3}"
SUITES="${DIET_SUITES:-TestTUIE2E,TestQuestionsE2E,TestStandingE2E}"
OUT=""
EXTRA_ALLOW="${DIET_ALLOWLIST:-}"
KEEP_WORKTREE=0
REUSE_WORKTREE=0
DRY_RUN=0

warn() { printf '  !  %s\n' "$*" >&2; }
say()  { printf '%s\n' "$*"; }
rule() { printf '── %s %s\n' "$1" "$(printf '─%.0s' $(seq 1 $((72 - ${#1})))) "; }

BRANCH="${1:-}"
LABEL="${2:-}"
if [ -z "$BRANCH" ] || [ -z "$LABEL" ]; then
  sed -n '1,50p' "$0"
  exit 1
fi
shift 2

while [ $# -gt 0 ]; do
  case "$1" in
    --layers)          LAYERS="${2:?--layers needs a value}"; shift 2 ;;
    --layers=*)        LAYERS="${1#*=}"; shift ;;
    --model)           MODEL="${2:?--model needs a value}"; shift 2 ;;
    --model=*)         MODEL="${1#*=}"; shift ;;
    --scenarios)       SCENARIOS="${2:?--scenarios needs a value}"; shift 2 ;;
    --scenarios=*)     SCENARIOS="${1#*=}"; shift ;;
    --cells)           CELLS="${2:?--cells needs a value}"; shift 2 ;;
    --cells=*)         CELLS="${1#*=}"; shift ;;
    --suites)          SUITES="${2:?--suites needs a value}"; shift 2 ;;
    --suites=*)        SUITES="${1#*=}"; shift ;;
    --out)             OUT="${2:?--out needs a value}"; shift 2 ;;
    --out=*)           OUT="${1#*=}"; shift ;;
    --allowlist)       EXTRA_ALLOW="${2:?--allowlist needs a value}"; shift 2 ;;
    --allowlist=*)     EXTRA_ALLOW="${1#*=}"; shift ;;
    --keep-worktree)   KEEP_WORKTREE=1; shift ;;
    --reuse-worktree)  REUSE_WORKTREE=1; KEEP_WORKTREE=1; shift ;;
    --dry-run)         DRY_RUN=1; shift ;;
    -h|--help)         sed -n '1,50p' "$0"; exit 0 ;;
    *)                 warn "unknown argument: $1"; exit 1 ;;
  esac
done

has_layer() { case ",$LAYERS," in *",$1,"*) return 0 ;; *) return 1 ;; esac; }

# THE EVIDENCE ROOT LIVES OUTSIDE EVERY AFORGE CHECKOUT, and this is a
# correctness rule rather than tidiness.
#
# MEASURED, 2026-09-10, the first baseline run: with the evidence under
# `bench/prompt-diet/out/`, a cell's scratch workspace sat inside the rig's own
# git checkout. The `code-fix` cell handed the model a small Go module to fix;
# the model walked up out of it, found the aforge repository around it, and ran
# `cd <rig> && go test ./...` — a full-tree build of aforge, on a shared box,
# inside a cell whose wall clock was supposed to be measuring a two-file fix.
# The cell was thrown away and the run started again from here.
#
# The bench batteries have always had this exposure — `bench-results/` is inside
# the repository too — and it has never bitten because nothing else pointed a
# model at a workspace nested that deep. This bench does, so this bench moves
# out. DIET_OUT_ROOT or --out can put it anywhere; anywhere inside a checkout is
# a mistake, and the run says so rather than only recording the consequences.
OUT="${OUT:-${DIET_OUT_ROOT:-$HOME/bench-diet-out}/$LABEL}"
mkdir -p "$OUT" || { warn "cannot write $OUT"; exit 1; }
if git -C "$OUT" rev-parse --show-toplevel >/dev/null 2>&1; then
  warn "$OUT is inside a git checkout, so a cell's workspace will be too — and a"
  warn "model that walks up out of its fixture finds the repository instead."
  warn "Set DIET_OUT_ROOT or --out to somewhere outside every checkout."
  exit 1
fi

# GO IS NOT ON THE SPARK'S NON-INTERACTIVE PATH. Finding it here rather than
# asking every caller to export it is what keeps the ssh recipes in BENCH.md to
# one line, and a missing toolchain says so once instead of failing four layers
# deep with "go: command not found".
GO="${GO:-$(command -v go || true)}"
[ -n "$GO" ] && [ -x "$GO" ] || GO="$HOME/.local/bin/go"
if [ ! -x "$GO" ]; then
  warn "no go toolchain: set GO=<path> (the Spark's lives at ~/.local/bin/go)"
  exit 1
fi

# ── the subject: one worktree, one build ────────────────────────────────────
#
# The worktree is DETACHED at the revision, so the run records a sha rather than
# a branch name that will have moved by the time anybody reads the table.
# THE BUILD IS NOT UNDER THE EVIDENCE, for the same reason the evidence is not
# under a checkout: a cell's workspace sits inside the evidence tree, and a
# model that walks up out of its fixture must not arrive in an aforge checkout.
# Two roots, and the only thing between them is the label.
WORKTREE="${DIET_WORKTREE:-${DIET_BUILD_ROOT:-$HOME/bench-diet-build}/$LABEL}"
if [ "$REUSE_WORKTREE" = "1" ] && [ -e "$WORKTREE/.git" ]; then
  say "reusing worktree $WORKTREE"
else
  rm -rf "$WORKTREE"
  mkdir -p "$(dirname "$WORKTREE")"
  if [ "$DRY_RUN" != "1" ]; then
    # A branch that only exists on the remote is fetched by name first; a bare
    # revision that is already in the object store needs no fetch and must not
    # fail the run when the fetch of a non-existent branch does.
    git -C "$RIG_ROOT" fetch -q origin "$BRANCH" 2>/dev/null || true
    RESOLVED=""
    for candidate in "origin/$BRANCH" "$BRANCH" FETCH_HEAD; do
      if git -C "$RIG_ROOT" rev-parse --verify -q "$candidate^{commit}" >/dev/null 2>&1; then
        RESOLVED="$candidate"; break
      fi
    done
    [ -n "$RESOLVED" ] || { warn "cannot resolve $BRANCH to a commit"; exit 1; }
    git -C "$RIG_ROOT" worktree add -f --detach "$WORKTREE" "$RESOLVED" >/dev/null 2>&1 \
      || { warn "git worktree add failed for $RESOLVED"; exit 1; }
  fi
fi

REVISION="unknown"
[ "$DRY_RUN" = "1" ] || REVISION="$(git -C "$WORKTREE" rev-parse HEAD 2>/dev/null || echo unknown)"
RIG_SHA="$(git -C "$RIG_ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"

cleanup_worktree() {
  [ "$KEEP_WORKTREE" = "1" ] && return 0
  [ -d "$WORKTREE" ] || return 0
  git -C "$RIG_ROOT" worktree remove --force "$WORKTREE" >/dev/null 2>&1 \
    || rm -rf "$WORKTREE"
}
trap cleanup_worktree EXIT

AFORGE_BIN="$WORKTREE/bin/aforge"

rule "prompt diet · $LABEL"
say "branch:     $BRANCH"
say "revision:   $REVISION"
say "rig:        $RIG_ROOT @ $RIG_SHA"
say "worktree:   $WORKTREE"
say "model:      $MODEL"
say "layers:     $LAYERS"
say "evidence:   $OUT"
say ""

if [ "$DRY_RUN" = "1" ]; then
  say "dry run: nothing was built and no call was made"
  exit 0
fi

# `make build` and never `go build -o` somewhere else: the packed manual is a
# generate step, and a binary built without it answers "what can you do?" from a
# corpus that is not the one the release ships (CLAUDE.md, the owner's standing
# orders).
rule "build"
if ! make -C "$WORKTREE" build > "$OUT/build.log" 2>&1; then
  warn "make build failed — see $OUT/build.log"
  tail -20 "$OUT/build.log" >&2
  exit 1
fi
[ -x "$AFORGE_BIN" ] || { warn "no binary at $AFORGE_BIN"; exit 1; }
say "built $AFORGE_BIN"
say ""

STARTED="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# ── layer A · the static prefix ─────────────────────────────────────────────
PREFIX_BYTES=""; PREFIX_PROMPT=""; PREFIX_TOOLS=""
if has_layer a; then
  rule "A · prefix"
  ( cd "$WORKTREE" && "$GO" test -count=1 -v \
      -run '^TestTheFixedPrefixStaysUnderItsBudget$' ./internal/session/ ) \
      > "$OUT/prefix.log" 2>&1
  # The test logs one line and this reads it rather than recomputing anything:
  # the budget test is the scoreboard the whole wave commits against, and a
  # second arithmetic for the same number is exactly the drift the one-source-of
  # -truth law exists to stop.
  line="$(grep -o 'the fixed prefix is .*' "$OUT/prefix.log" | head -1)"
  if [ -n "$line" ]; then
    PREFIX_BYTES="$(sed -n 's/.*is \([0-9]*\) bytes.*/\1/p' <<< "$line")"
    PREFIX_PROMPT="$(sed -n 's/.*prompt \([0-9]*\) .*/\1/p' <<< "$line")"
    PREFIX_TOOLS="$(sed -n 's/.*tools \([0-9]*\).*/\1/p' <<< "$line")"
    say "$line"
  else
    warn "the budget test printed no prefix line — see $OUT/prefix.log"
  fi
  if grep -q '^FAIL' "$OUT/prefix.log"; then
    warn "the budget test FAILED on this revision (that is a finding, not a rig fault)"
  fi
  say ""
fi

# ── layer B · the tagged suites ─────────────────────────────────────────────
#
# THE SCREENS ARE FAR TOO WIDE TO READ THROUGH A PIPE, so every suite's output
# is redirected whole to its own file and only the counts are printed here.
if has_layer b && [ "$SUITES" != "none" ]; then
  rule "B · suites"
  mkdir -p "$OUT/suites"
  if [ -z "${OPENROUTER_API_KEY:-}" ]; then
    warn "no OPENROUTER_API_KEY: every tagged suite will SKIP and layer B proves nothing"
  fi
  if ! command -v tmux >/dev/null 2>&1; then
    warn "no tmux: the TUI suites will SKIP"
  fi
  for suite in ${SUITES//,/ }; do
    log="$OUT/suites/$suite.log"
    say "running $suite (output → $log)"
    ( cd "$WORKTREE" && AFORGE_CALL_LOG="$OUT/suites/$suite.calllog.jsonl" \
        "$GO" test -tags e2e -count=1 -timeout 40m -v \
        -run "^$suite$" ./internal/e2e/ ) > "$log" 2>&1
    printf '   %s: %s pass, %s fail, %s skip\n' "$suite" \
      "$(grep -c '^\s*--- PASS: ' "$log" 2>/dev/null || echo 0)" \
      "$(grep -c '^\s*--- FAIL: ' "$log" 2>/dev/null || echo 0)" \
      "$(grep -c '^\s*--- SKIP: ' "$log" 2>/dev/null || echo 0)"
  done
  python3 "$DIET_ROOT/lib/outcomes.py" "$OUT/suites" > "$OUT/suites/outcomes.json" \
    || warn "could not summarise the suite logs"
  say ""
fi

# ── layer C · the existing bench cells ──────────────────────────────────────
#
# Nothing here invents a cell format. bench/conversation and bench/e2e each have
# a runner, a README that says which numbers are honest to quote, and an
# append-only history; this passes them a binary and a pin and keeps their
# evidence where the rest of the run's evidence is.
if has_layer c; then
  rule "C · cells"
  mkdir -p "$OUT/cells"
  if [ -z "${OPENROUTER_API_KEY:-}" ]; then
    warn "no OPENROUTER_API_KEY: layer C cannot run"
  else
    ALLOWLIST="$MODEL${EXTRA_ALLOW:+ $EXTRA_ALLOW}"
    if [ "$SCENARIOS" != "none" ]; then
      say "conversation: $SCENARIOS"
      # CONV_PASS_ENV is how the suite is asked to carry a variable it does not
      # know about; without it the harness inherits nothing but the key, which
      # is the open-model policy working as designed. The call log is what layer
      # D reads for the tool block's own bytes.
      #
      # AND THE CALLER'S OWN LIST IS ADDED TO IT, NEVER REPLACED BY IT. This line
      # used to assign the two names flat, which silently dropped whatever the
      # caller had asked to carry — and the one recipe in this tree that needs
      # that (BENCH.md §4a's lean cell, `CONV_PASS_ENV=AFORGE_PROMPT_PROFILE`)
      # therefore measured the FULL profile and read as a lean arm that saved
      # nothing. The e2e cells below never had the problem because they inherit
      # the whole environment; only bench/conversation filters, which is what
      # made the failure invisible: half the run was lean and half was not.
      CONV_PASS_ENV="AFORGE_CALL_LOG AFORGE_CALL_LOG_BODIES${CONV_PASS_ENV:+ $CONV_PASS_ENV}" \
      AFORGE_CALL_LOG="$OUT/cells/conversation.calllog.jsonl" \
      AFORGE_CALL_LOG_BODIES=1 \
      AFORGE_BIN="$AFORGE_BIN" \
        "$RIG_ROOT/bench/conversation/run.sh" \
          --arms aforge --model "$MODEL" --allowlist "$ALLOWLIST" \
          --scenarios "${SCENARIOS//,/ }" \
          --out "$OUT/cells/conversation" \
          > "$OUT/cells/conversation.log" 2>&1
      say "   → $OUT/cells/conversation/results.jsonl"
    fi
    if [ "$CELLS" != "none" ]; then
      say "e2e cells: $CELLS"
      AFORGE_BIN="$AFORGE_BIN" E2E_MODEL="$MODEL" \
      CSV="$OUT/cells/e2e.csv" RUN_DIR="$OUT/cells/e2e" \
        "$RIG_ROOT/bench/e2e/run.sh" --cells "$CELLS" \
          > "$OUT/cells/e2e.log" 2>&1
      say "   → $OUT/cells/e2e.csv"
    fi
  fi
  say ""
fi

# ── layer D · the wire ──────────────────────────────────────────────────────
if has_layer d; then
  rule "D · wire"
  python3 "$DIET_ROOT/lib/wire.py" "$OUT" > "$OUT/wire.jsonl" 2> "$OUT/wire.err" \
    || warn "could not roll up the wire ledger — see $OUT/wire.err"
  say "   → $OUT/wire.jsonl ($(wc -l < "$OUT/wire.jsonl" | tr -d ' ') requests)"
  say ""
fi

FINISHED="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# The run's own identity, written through the environment rather than
# interpolated into the script: a label or a branch name with a quote in it
# would otherwise end the run with a Python syntax error and no evidence at all.
DIET_LABEL="$LABEL" DIET_BRANCH="$BRANCH" DIET_REVISION="$REVISION" \
DIET_RIG_ROOT="$RIG_ROOT" DIET_RIG_SHA="$RIG_SHA" DIET_MODEL_PIN="$MODEL" \
DIET_LAYERS="$LAYERS" DIET_SCENARIOS="$SCENARIOS" DIET_CELLS="$CELLS" \
DIET_SUITES="$SUITES" DIET_PREFIX_BYTES="${PREFIX_BYTES:-}" \
DIET_PREFIX_PROMPT="${PREFIX_PROMPT:-}" DIET_PREFIX_TOOLS="${PREFIX_TOOLS:-}" \
DIET_STARTED="$STARTED" DIET_FINISHED="$FINISHED" \
python3 - "$OUT" <<'PY'
import json, os, sys

def number(name):
    """An unmeasured layer leaves the field ABSENT rather than zero: a prefix of
    zero bytes is a claim nobody made, and the emptiness law this repository
    runs on says unknown renders as nothing."""
    raw = os.environ.get(name, "").strip()
    return int(raw) if raw.isdigit() else None

json.dump({
    "label":         os.environ["DIET_LABEL"],
    "branch":        os.environ["DIET_BRANCH"],
    "revision":      os.environ["DIET_REVISION"],
    "rig_root":      os.environ["DIET_RIG_ROOT"],
    "rig_sha":       os.environ["DIET_RIG_SHA"],
    "model":         os.environ["DIET_MODEL_PIN"],
    "layers":        os.environ["DIET_LAYERS"],
    "scenarios":     os.environ["DIET_SCENARIOS"],
    "cells":         os.environ["DIET_CELLS"],
    "suites":        os.environ["DIET_SUITES"],
    "prefix_bytes":  number("DIET_PREFIX_BYTES"),
    "prefix_prompt": number("DIET_PREFIX_PROMPT"),
    "prefix_tools":  number("DIET_PREFIX_TOOLS"),
    "started":       os.environ["DIET_STARTED"],
    "finished":      os.environ["DIET_FINISHED"],
}, open(sys.argv[1] + "/meta.json", "w"), indent=2)
PY

python3 "$DIET_ROOT/lib/summary.py" "$OUT" > "$OUT/summary.md" 2>/dev/null \
  || warn "could not write the one-page summary"

rule "done"
say "evidence:  $OUT"
say "summary:   $OUT/summary.md"
say "compare:   bench/prompt-diet/compare.py dev $LABEL"
