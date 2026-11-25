#!/usr/bin/env bash
# run.sh — the script-tool benchmark: read-heavy questions about one pinned
# repository, each asked of a pair of arms in the same wave, each answer scored
# by check.py against the source itself.
#
# Usage: NEW_BIN=<codeaf> bench/scriptbench/run.sh [task ...]
#   tasks default to every tasks/*.md; ARMS, SEEDS and MODEL below can be set.
#
# EVERY CELL IS bench/oneroad/cell.sh, so it is driven through the real chat
# over tmux, from an empty environment, with the same counters and the same
# settling rules; this script only hands it a prompt file and a repository and
# leaves the test suite out (SKIP_SUITE=1), because check.py is the judge here.
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ONEROAD="$HERE/../oneroad"

: "${NEW_BIN:?set NEW_BIN to the codeaf binary under test}"
export NEW_BIN
export MODEL="${MODEL:-anthropic/claude-sonnet-5.5}"
ARMS="${ARMS:-codeaf-new-crew codeaf-chatscript-crew}"
SEEDS="${SEEDS:-s1}"
TASKS="${*:-$(cd "$HERE/tasks" && ls *.md | sed 's/\.md$//')}"

# The corpus: click 8.1.7, sixteen modules and about ten thousand lines, pinned
# so every answer check.py computes is the same answer next month.
export REPO="https://github.com/pallets/click"
export BASE_COMMIT="874ca2bc1c30d93a4ac6e36a15ed685eafe89097"
export RESULTS="$HERE/results"
export SKIP_SUITE=1
# A question answered in words settles fast; an hour would only be paid by a
# cell that has gone wrong.
export CELL_SECONDS="${CELL_SECONDS:-1200}"
export SILENCE_SECONDS="${SILENCE_SECONDS:-60}"
mkdir -p "$RESULTS"

# THE MANUAL QUESTIONS ARE CHECKED AGAINST THE BINARY'S OWN CORPUS. The truth is
# read through internal/manual from this checkout, so the checkout's chat pages
# must be the ones the binary was built from; a mismatch would score a correct
# reply as wrong, and the run refuses rather than measure that.
REPO_ROOT="$(git -C "$HERE" rev-parse --show-toplevel)"
BIN_REV="$("$NEW_BIN" --version 2>/dev/null | awk '{print $2}')"
if ! git -C "$REPO_ROOT" rev-parse --verify --quiet "$BIN_REV^{commit}" >/dev/null; then
  echo "cannot tell which commit $NEW_BIN was built from (its --version names \"$BIN_REV\"); build it with make build from this checkout" >&2
  exit 1
fi
if ! git -C "$REPO_ROOT" diff --quiet "$BIN_REV" -- internal/manual/chat; then
  echo "the manual in this checkout differs from the one $NEW_BIN ($BIN_REV) ships; build it from here" >&2
  exit 1
fi
export MANUAL_TRUTH="$RESULTS/manual-truth.json"
(cd "$REPO_ROOT" && go run ./bench/scriptbench/manualtruth) > "$MANUAL_TRUTH" || exit 1

# NO CODEAF ON THE CELL'S PATH. `codeaf manual` reads a different corpus from
# the `manual` tool, and a model that shelled out to an installed codeaf would
# be answering from the wrong pages through a road this benchmark is not about.
# Every folder holding a codeaf program leaves the path; the binary under test
# is run by its own path.
clean_path=""
IFS=: read -ra path_dirs <<< "$PATH"
for dir in "${path_dirs[@]}"; do
  [ -x "$dir/codeaf" ] && continue
  clean_path="${clean_path:+$clean_path:}$dir"
done
export PATH="$clean_path"

for seed in $SEEDS; do
  for task in $TASKS; do
    # THE SAME-WAVE RULE (bench/oneroad/wave.sh): both arms of a question start
    # together, so provider weather falls on the pair or on neither.
    pids=()
    for arm in $ARMS; do
      SEED="$seed" PROMPT_FILE="$HERE/tasks/$task.md" \
        bash "$ONEROAD/cell.sh" "$arm" "$task" > "$RESULTS/.$arm-$task-$seed.out" 2>&1 &
      pids+=($!)
      sleep 2
    done
    for pid in "${pids[@]}"; do wait "$pid"; done
  done
done
python3 "$HERE/score.py" "$RESULTS"
