#!/usr/bin/env bash
# Run the issue-implementation benchmark: one fresh clone per (harness, issue)
# cell, one harness invocation, then the repository's own test suite as the
# judge. Results land in $RESULTS/results.csv and the full transcript of every
# cell in $RESULTS/<harness>-<issue>/.
#
# Read bench/README.md before trusting any number this prints, in particular
# the cost column: only aforge self-reports usage.
set -uo pipefail

# ── parameters ──────────────────────────────────────────────────────────────
REPO="${REPO:-https://github.com/MALIBA-AI/bambara-text-normalization}"
MODEL="${MODEL:-deepseek/deepseek-v4-flash-0731}"
ISSUES="${ISSUES:-20 21 22 23}"
HARNESSES="${HARNESSES:-aforge pi opencode}"

# Wall-clock cap per cell. A harness that has not produced a diff by here is
# recorded as DNF rather than left to spend: opencode hit this on issue #22 and
# kept spending after the diff never arrived.
CELL_TIMEOUT="${CELL_TIMEOUT:-40m}"

# aforge shape: "node" is one leaf, which is the executor measured alone and
# the number quoted for the issue matrix; "pipeline" plans the graph first and
# is what the PR-review comparison used.
AFORGE_MODE="${AFORGE_MODE:-node}"
AFORGE_BIN="${AFORGE_BIN:-aforge}"

# pi and opencode flags move between versions. These are the invocations the
# recorded runs used; check them against your installed version before
# concluding anything from a zero-file result.
PI_BIN="${PI_BIN:-pi}"
OPENCODE_BIN="${OPENCODE_BIN:-opencode}"

RESULTS="${RESULTS:-$(pwd)/bench-results/$(date +%Y%m%d-%H%M%S)}"
TEMPLATE="${TEMPLATE:-$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/graphs/issue.json}"

# ── prerequisites ───────────────────────────────────────────────────────────
TIMEOUT_BIN="$(command -v timeout || command -v gtimeout || true)"
if [ -z "$TIMEOUT_BIN" ]; then
  echo "need timeout(1) — brew install coreutils" >&2
  exit 1
fi
for tool in git python3 gh; do
  command -v "$tool" >/dev/null || { echo "need $tool" >&2; exit 1; }
done

mkdir -p "$RESULTS"
CSV="$RESULTS/results.csv"
echo "harness,issue,seconds,exit,changed_files,passed,failed,cost_usd,cost_source" > "$CSV"

SLUG="$(basename "$REPO" .git)"
OWNER_REPO="$(echo "$REPO" | sed -E 's#^.*github.com[:/]##; s#\.git$##')"

# ── helpers ─────────────────────────────────────────────────────────────────

# issue_prompt writes the issue as the instruction every harness receives. All
# three get the same text; the only difference between cells is who executes it.
issue_prompt() {
  local number="$1"
  gh issue view "$number" --repo "$OWNER_REPO" --json title,body \
    --template 'Implement issue #'"$number"': {{.title}}

{{.body}}

Work in this repository. Implement the change and make the existing test suite pass. Do not weaken or delete tests to make them pass.'
}

# fresh_clone gives each cell a clone of its own at the same starting commit.
# Reusing one checkout across cells leaks the previous harness's diff into the
# next one's starting state, which silently flatters whoever runs second.
fresh_clone() {
  local dir="$1"
  rm -rf "$dir"
  git clone --depth 1 --quiet "$REPO" "$dir" || return 1
  (cd "$dir" && git rev-parse HEAD)
}

# setup_python builds the venv once per cell. The suite is the judge, so it is
# installed before the harness runs and never touched afterwards.
setup_python() {
  local dir="$1"
  (
    cd "$dir" || exit 1
    python3 -m venv .venv >/dev/null 2>&1 || exit 1
    .venv/bin/pip install -q --upgrade pip >/dev/null 2>&1
    .venv/bin/pip install -q -e ".[dev]" >/dev/null 2>&1 ||
      .venv/bin/pip install -q -e . >/dev/null 2>&1 ||
      .venv/bin/pip install -q -r requirements.txt >/dev/null 2>&1
    .venv/bin/pip install -q pytest >/dev/null 2>&1
  )
}

# render_graph turns the one-node template into a graph carrying this issue.
# Substitution goes through python rather than sed because an issue body
# contains quotes and newlines that would otherwise produce invalid JSON.
render_graph() {
  local prompt="$1" title="$2" out="$3"
  TEMPLATE="$TEMPLATE" PROMPT="$prompt" TITLE="$title" OUT="$out" python3 - <<'PY'
import json, os
raw = json.load(open(os.environ["TEMPLATE"]))
prompt, title = os.environ["PROMPT"], os.environ["TITLE"]
raw["goal"] = title
for node in raw["nodes"]:
    node["title"] = title[:60]
    node["summary"] = title
    node["brief"] = prompt
json.dump(raw, open(os.environ["OUT"], "w"), indent=2)
PY
}

# run_harness invokes one harness against one clone and returns its exit code.
# stdout and stderr are kept whole: the summary lines are parsed out of them
# afterwards, and when a cell is a DNF the log is the only evidence of why.
run_harness() {
  local harness="$1" dir="$2" prompt="$3" log="$4" cell="$5"
  case "$harness" in
    aforge)
      if [ "$AFORGE_MODE" = "pipeline" ]; then
        "$TIMEOUT_BIN" "$CELL_TIMEOUT" "$AFORGE_BIN" plan "$prompt" --brief -o "$cell/graph.json" \
          >>"$log" 2>&1 || return $?
      else
        render_graph "$prompt" "$(echo "$prompt" | head -1)" "$cell/graph.json" || return 1
      fi
      "$TIMEOUT_BIN" "$CELL_TIMEOUT" "$AFORGE_BIN" run "$cell/graph.json" -w "$dir" -o "$cell/done.json" \
        >>"$log" 2>&1
      ;;
    pi)
      (cd "$dir" && "$TIMEOUT_BIN" "$CELL_TIMEOUT" "$PI_BIN" -m "$MODEL" "$prompt") >>"$log" 2>&1
      ;;
    opencode)
      (cd "$dir" && "$TIMEOUT_BIN" "$CELL_TIMEOUT" "$OPENCODE_BIN" run -m "$MODEL" "$prompt") >>"$log" 2>&1
      ;;
    *)
      echo "unknown harness $harness" >&2
      return 1
      ;;
  esac
}

# harness_cost reads the run's own accounting. For aforge that is the $ figure
# on the run summary line. For pi and opencode there is nothing to read, and
# the account-level delta is not a substitute — see bench/README.md.
harness_cost() {
  local harness="$1" log="$2"
  if [ "$harness" != "aforge" ]; then
    echo "n/a,not-self-reported"
    return
  fi
  local cost
  cost="$(grep -oE '\$[0-9]+\.[0-9]+' "$log" | tail -1 | tr -d '$')"
  echo "${cost:-0},self-reported"
}

# changed_files counts what the harness actually did to the working tree. A
# harness that times out with zero changed files did nothing, whatever its log
# claims to have been doing.
changed_files() {
  (cd "$1" && git status --porcelain 2>/dev/null | grep -vc '\.venv' || true)
}

# run_suite is the verdict. It runs after the harness is finished and its
# counts, not the harness's own report, are what the matrix records.
run_suite() {
  local dir="$1" log="$2"
  (cd "$dir" && ./.venv/bin/python -m pytest -q) >"$log" 2>&1
  local line
  line="$(grep -E '[0-9]+ (passed|failed)' "$log" | tail -1)"
  local passed failed
  passed="$(echo "$line" | grep -oE '[0-9]+ passed' | grep -oE '[0-9]+')"
  failed="$(echo "$line" | grep -oE '[0-9]+ failed' | grep -oE '[0-9]+')"
  echo "${passed:-0},${failed:-0}"
}

# ── the grid ────────────────────────────────────────────────────────────────
echo "repo:     $REPO"
echo "model:    $MODEL"
echo "issues:   $ISSUES"
echo "harness:  $HARNESSES (aforge mode: $AFORGE_MODE)"
echo "results:  $RESULTS"
echo

for issue in $ISSUES; do
  prompt="$(issue_prompt "$issue")" || { echo "could not read issue #$issue" >&2; continue; }
  for harness in $HARNESSES; do
    cell="$RESULTS/$harness-$issue"
    mkdir -p "$cell"
    dir="$cell/$SLUG"
    printf '%-9s #%-3s ' "$harness" "$issue"

    commit="$(fresh_clone "$dir")" || { echo "clone failed"; continue; }
    echo "$commit" > "$cell/base-commit"
    setup_python "$dir"

    started=$(date +%s)
    run_harness "$harness" "$dir" "$prompt" "$cell/harness.log" "$cell"
    code=$?
    seconds=$(( $(date +%s) - started ))

    changed="$(changed_files "$dir")"
    counts="$(run_suite "$dir" "$cell/pytest.log")"
    cost="$(harness_cost "$harness" "$cell/harness.log")"

    echo "$harness,$issue,$seconds,$code,$changed,$counts,$cost" >> "$CSV"
    printf '%4ss  exit %-3s %2s files  %s passed/failed  %s\n' \
      "$seconds" "$code" "$changed" "${counts/,/ + }" "${cost%%,*}"
  done
done

echo
echo "wrote $CSV"
