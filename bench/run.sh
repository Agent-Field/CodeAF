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

# aforge shape. Four values, and the first three are the comparison this file
# exists for — the same issue taken three ways, against the same recorded pi and
# opencode rows:
#
#   node     one leaf on the default worker. The executor measured alone, and
#            the drift control: byte for byte the invocation the recorded aforge
#            numbers came from, so a re-run that moves says the harness moved.
#   swe      the same one-node graph with the leaf handed to the swe worker.
#            The specialist forced, to measure it rather than to trust it.
#   select   `aforge do` with nothing forced. The shipping claim: whatever the
#            compiler picks is what gets measured, including picking linear.
#   pipeline plan the graph first, then run it. The parallel shape, and what the
#            PR-review comparison used.
AFORGE_MODE="${AFORGE_MODE:-node}"
AFORGE_BIN="${AFORGE_BIN:-aforge}"

# The worker AFORGE_MODE=swe forces. It is a variable because the point of the
# mode is measuring one named worker against the default, and the second
# specialist will want the same cell with a different name in it.
AFORGE_SUBHARNESS="${AFORGE_SUBHARNESS:-swe}"

# BENCH_DRY_RUN composes every invocation and runs none of them. It clones
# nothing, builds no venv, calls no model, and prints the exact argv each mode
# would execute — the wiring check that costs nothing, for the failure mode
# where a flag moved between versions and the first evidence is a $40 grid of
# zero-file cells.
BENCH_DRY_RUN="${BENCH_DRY_RUN:-0}"
case "${1:-}" in
  --dry-run) BENCH_DRY_RUN=1 ;;
esac

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

# sqlite3 is optional and only the select mode wants it: it is how the store
# says which worker the compiler chose. Without it that one column reads
# "unknown" and nothing else in the run changes.
SQLITE_BIN="$(command -v sqlite3 || true)"

# The engine needs the same key the rest of aforge runs on, and a cell that
# starts without it burns a clone and a venv before finding out. Nothing is
# invented here — this only makes sure what the shell already has reaches the
# child processes, and says so early when it has nothing.
case " $HARNESSES " in
  *" aforge "*)
    if [ -n "${OPENROUTER_API_KEY:-}" ]; then
      export OPENROUTER_API_KEY
    elif [ "$BENCH_DRY_RUN" != "1" ]; then
      echo "OPENROUTER_API_KEY is unset — aforge cells will fail" >&2
    fi
    ;;
esac

mkdir -p "$RESULTS"
CSV="$RESULTS/results.csv"
# The two new columns are appended, never inserted. A reader that indexes the
# old nine by position still reads the old nine; a reader that goes by header
# gets the new two; and a CSV written before this change is still a CSV.
echo "harness,issue,seconds,exit,changed_files,passed,failed,cost_usd,cost_source,aforge_mode,subharness_chosen" > "$CSV"

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
#
# The fourth argument is the worker this node is handed to, and it is the whole
# difference between the node and swe cells: same template, same invocation,
# same one leaf, one field. An empty name writes no field at all, so the graph
# the node mode renders is byte-identical to the one it rendered before this
# mode existed — which is the only way it can still be the drift control.
render_graph() {
  local prompt="$1" title="$2" out="$3" subharness="${4:-}"
  TEMPLATE="$TEMPLATE" PROMPT="$prompt" TITLE="$title" OUT="$out" SUBHARNESS="$subharness" python3 - <<'PY'
import json, os
raw = json.load(open(os.environ["TEMPLATE"]))
prompt, title = os.environ["PROMPT"], os.environ["TITLE"]
subharness = os.environ.get("SUBHARNESS", "").strip()
raw["goal"] = title
for node in raw["nodes"]:
    node["title"] = title[:60]
    node["summary"] = title
    node["brief"] = prompt
    if subharness:
        node["subharness"] = subharness
json.dump(raw, open(os.environ["OUT"], "w"), indent=2)
PY
}

# seconds_of turns a timeout(1) duration into the plain seconds `aforge do`
# wants. The two walls have to be the same wall: a select cell held to do's
# fifteen-minute default while node and swe get forty is not the same cell.
seconds_of() {
  local spec="$1" count="${1%[smh]}"
  case "$spec" in
    *h) echo $(( count * 3600 )) ;;
    *m) echo $(( count * 60 )) ;;
    *s) echo "$count" ;;
    *)  echo "$spec" ;;
  esac
}

# compose_aforge builds the argv this cell would execute, and executes nothing.
# It is a separate step from running it so that --dry-run can print exactly what
# the real run would launch, rather than a hand-written approximation of it that
# drifts the first time a flag moves.
#
# AFORGE_PRE_ARGV is the planning call, and only the pipeline shape has one —
# AFORGE_HAS_PRE says whether it is there, because an empty array is not
# something every bash this script may meet will let `set -u` look at.
# AFORGE_ARGV is the single invocation every shape ends with.
compose_aforge() {
  local dir="$1" prompt="$2" cell="$3"
  AFORGE_PRE_ARGV=()
  AFORGE_HAS_PRE=""
  case "$AFORGE_MODE" in
    pipeline)
      AFORGE_HAS_PRE="1"
      AFORGE_PRE_ARGV=("$TIMEOUT_BIN" "$CELL_TIMEOUT" "$AFORGE_BIN" plan "$prompt" --brief -o "$cell/graph.json")
      AFORGE_ARGV=("$TIMEOUT_BIN" "$CELL_TIMEOUT" "$AFORGE_BIN" run "$cell/graph.json" -w "$dir" -o "$cell/done.json")
      ;;
    select)
      # No graph and no forcing: the compiler decides both the shape and the
      # worker, which is the thing being measured. -w is what keeps the writes
      # in the clone, so the diff afterwards is this run's diff; --keep leaves
      # the private store behind, which is where the choice is legible.
      AFORGE_ARGV=("$TIMEOUT_BIN" "$CELL_TIMEOUT" "$AFORGE_BIN" "do" "$prompt" \
        -w "$dir" -keep -timeout "$(seconds_of "$CELL_TIMEOUT")")
      ;;
    *)
      AFORGE_ARGV=("$TIMEOUT_BIN" "$CELL_TIMEOUT" "$AFORGE_BIN" run "$cell/graph.json" -w "$dir" -o "$cell/done.json")
      ;;
  esac
}

# render_cell_graph writes the graph the node and swe shapes execute. It costs
# nothing and calls no model, so the dry run does it too — a rendered graph with
# the right worker in it is most of what there is to check.
render_cell_graph() {
  local prompt="$1" cell="$2"
  case "$AFORGE_MODE" in
    pipeline|select) return 0 ;;
    swe) render_graph "$prompt" "$(echo "$prompt" | head -1)" "$cell/graph.json" "$AFORGE_SUBHARNESS" ;;
    *)   render_graph "$prompt" "$(echo "$prompt" | head -1)" "$cell/graph.json" ;;
  esac
}

# run_harness invokes one harness against one clone and returns its exit code.
# stdout and stderr are kept whole: the summary lines are parsed out of them
# afterwards, and when a cell is a DNF the log is the only evidence of why.
run_harness() {
  local harness="$1" dir="$2" prompt="$3" log="$4" cell="$5"
  case "$harness" in
    aforge)
      compose_aforge "$dir" "$prompt" "$cell"
      if [ -n "$AFORGE_HAS_PRE" ]; then
        "${AFORGE_PRE_ARGV[@]}" >>"$log" 2>&1 || return $?
      else
        render_cell_graph "$prompt" "$cell" || return 1
      fi
      "${AFORGE_ARGV[@]}" >>"$log" 2>&1
      ;;
    pi)
      (cd "$dir" && "$TIMEOUT_BIN" "$CELL_TIMEOUT" "$PI_BIN" -p --provider openrouter --model "$MODEL" "$prompt") >>"$log" 2>&1
      ;;
    opencode)
      (cd "$dir" && "$TIMEOUT_BIN" "$CELL_TIMEOUT" "$OPENCODE_BIN" run -m "openrouter/$MODEL" "$prompt") >>"$log" 2>&1
      ;;
    *)
      echo "unknown harness $harness" >&2
      return 1
      ;;
  esac
}

# harness_cost reads the run's own accounting. For aforge that is the $ figure
# on the run summary line — the same line in all three shapes: `run` ends with
# it, and `do` ends with "<elapsed> · <n> nodes · $<spend>". A swe leaf's spend
# arrives from the engine's terminal event and is summed into that figure like
# any other leaf's, so nothing here has to know which worker ran. For pi and
# opencode there is nothing to read, and the account-level delta is not a
# substitute — see bench/README.md.
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

# graph_subharness reads the worker off the graph the run finished with. For
# the node and swe shapes that is the whole answer, because the field was
# written before the run and the completed graph carries it back out.
#
# One thing it cannot see: a build with no swe worker registered degrades the
# leaf to the default one and says nothing on either stream, so a swe cell on
# such a build is a linear cell wearing the name. The check for that is out of
# band — `aforge run --subharness <name>` on a build without it prints "no
# subharness named" — and it is a gap this column inherits, recorded in
# bench/README.md rather than papered over here.
graph_subharness() {
  local finished="$1" requested="$2"
  local found=""
  if [ -f "$finished" ]; then
    found="$(GRAPH="$finished" python3 - <<'PY'
import json, os
try:
    raw = json.load(open(os.environ["GRAPH"]))
except Exception:
    raise SystemExit(0)
names = []
for node in raw.get("nodes", []):
    name = (node.get("subharness") or "").strip()
    if name and name not in names:
        names.append(name)
print("+".join(names))
PY
)"
  fi
  if [ -n "$found" ]; then
    echo "$found"
  elif [ -n "$requested" ]; then
    # Asked for and not in the graph that came back: the run did not get far
    # enough to write one, so what was asked for is the honest record.
    echo "$requested"
  else
    echo "linear"
  fi
}

# store_subharness reads what the compiler chose, which is the only mode where
# the answer is not known in advance. `do --keep` prints where it left its
# private store; the node row in that store carries the settled choice, which is
# the same durable field the graph shapes set by hand.
store_subharness() {
  local log="$1"
  local home database chosen
  home="$(grep -oE '^store kept at .*' "$log" | tail -1 | sed -E 's#^store kept at ##')"
  database="$home/graph.db"
  if [ -z "$home" ] || [ ! -f "$database" ] || [ -z "$SQLITE_BIN" ]; then
    echo "unknown"
    return
  fi
  chosen="$("$SQLITE_BIN" "$database" \
    "SELECT DISTINCT COALESCE(NULLIF(subharness,''), NULLIF(splice_subharness,'')) AS worker
       FROM nodes WHERE worker IS NOT NULL AND worker != '';" 2>/dev/null | paste -sd+ -)"
  echo "${chosen:-linear}"
}

# stow_store moves the kept store next to the rest of the cell's evidence. A
# select cell's store is the record of what was chosen and why there was a
# choice, and leaving it in the system temp directory is how it gets swept.
stow_store() {
  local log="$1" cell="$2"
  local home
  home="$(grep -oE '^store kept at .*' "$log" | tail -1 | sed -E 's#^store kept at ##')"
  if [ -n "$home" ] && [ -d "$home" ]; then
    rm -rf "$cell/store"
    mv "$home" "$cell/store" 2>/dev/null || true
  fi
}

# subharness_chosen is what actually took the leaf. Forced shapes know it before
# they start and it is read back rather than assumed; the select shape is told
# by the store afterwards.
subharness_chosen() {
  local harness="$1" cell="$2" log="$3"
  if [ "$harness" != "aforge" ]; then
    echo "n/a"
    return
  fi
  case "$AFORGE_MODE" in
    select) store_subharness "$log" ;;
    swe)    graph_subharness "$cell/done.json" "$AFORGE_SUBHARNESS" ;;
    *)      graph_subharness "$cell/done.json" "" ;;
  esac
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

# mode_of names the shape a cell ran in. Only aforge has one; the column reads
# n/a for the harnesses that are one shape and nothing else.
mode_of() {
  if [ "$1" = "aforge" ]; then echo "$AFORGE_MODE"; else echo "n/a"; fi
}

# quote_argv prints an argv the way a person could paste it back into a shell.
# The issue text is a whole GitHub issue and would drown the line, so the one
# argument that is prose is shown as its first line and a length.
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

# dry_cell is the whole run minus the spending. It composes what would be
# launched and prints it, having cloned nothing and called nothing.
dry_cell() {
  local harness="$1" dir="$2" prompt="$3" cell="$4"
  if [ "$harness" != "aforge" ]; then
    case "$harness" in
      pi)       quote_argv "$TIMEOUT_BIN" "$CELL_TIMEOUT" "$PI_BIN" -p --provider openrouter --model "$MODEL" "$prompt" ;;
      opencode) quote_argv "$TIMEOUT_BIN" "$CELL_TIMEOUT" "$OPENCODE_BIN" run -m "openrouter/$MODEL" "$prompt" ;;
      *)        echo " unknown harness $harness" ;;
    esac
    return
  fi
  compose_aforge "$dir" "$prompt" "$cell"
  render_cell_graph "$prompt" "$cell" || true
  if [ -n "$AFORGE_HAS_PRE" ]; then
    quote_argv "${AFORGE_PRE_ARGV[@]}"
    printf '%-9s #%-3s ' "$harness" "$issue"
  fi
  quote_argv "${AFORGE_ARGV[@]}"
  case "$AFORGE_MODE" in
    pipeline|select) ;;
    *)
      [ -f "$cell/graph.json" ] && printf '          graph:   %s (worker: %s)\n' \
        "$cell/graph.json" "$(graph_subharness "$cell/graph.json" "")"
      ;;
  esac
}

# ── the grid ────────────────────────────────────────────────────────────────
echo "repo:     $REPO"
echo "model:    $MODEL"
echo "issues:   $ISSUES"
echo "harness:  $HARNESSES (aforge mode: $AFORGE_MODE)"
echo "results:  $RESULTS"
[ "$BENCH_DRY_RUN" = "1" ] && echo "dry run:  composing invocations only — no clone, no venv, no model call"
echo

for issue in $ISSUES; do
  prompt="$(issue_prompt "$issue")" || {
    if [ "$BENCH_DRY_RUN" = "1" ]; then
      # A dry run is a wiring check and must work without a GitHub round trip.
      # The stand-in is the same shape as the real thing — multi-line prose with
      # the issue number in the first line — because that shape is what the
      # composition has to survive.
      echo "could not read issue #$issue — composing with a stand-in prompt" >&2
      prompt="Implement issue #$issue: <title unavailable, dry run>

<body unavailable, dry run>

Work in this repository. Implement the change and make the existing test suite pass. Do not weaken or delete tests to make them pass."
    else
      echo "could not read issue #$issue" >&2
      continue
    fi
  }
  for harness in $HARNESSES; do
    cell="$RESULTS/$harness-$issue"
    mkdir -p "$cell"
    dir="$cell/$SLUG"
    printf '%-9s #%-3s ' "$harness" "$issue"

    if [ "$BENCH_DRY_RUN" = "1" ]; then
      dry_cell "$harness" "$dir" "$prompt" "$cell"
      continue
    fi

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
    worker="$(subharness_chosen "$harness" "$cell" "$cell/harness.log")"
    stow_store "$cell/harness.log" "$cell"

    echo "$harness,$issue,$seconds,$code,$changed,$counts,$cost,$(mode_of "$harness"),$worker" >> "$CSV"
    printf '%4ss  exit %-3s %2s files  %s passed/failed  %s  %s\n' \
      "$seconds" "$code" "$changed" "${counts/,/ + }" "${cost%%,*}" "$worker"
  done
done

echo
if [ "$BENCH_DRY_RUN" = "1" ]; then
  echo "dry run — nothing was cloned, run, or spent"
else
  echo "wrote $CSV"
fi
