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

# The commit every cell starts from. Empty means the clone's HEAD, which is
# only honest while the issues are still open there — the repository has since
# merged fixes for #23 (2026-08-06) and #21 (2026-08-08), so a HEAD clone
# passes the suite before any harness touches it and the row measures nothing.
# For the recorded issue set the pin is 6c978ffa1c49ba600c85eb893958409e37dbedd2
# (2026-08-02, merge of PR #18): the last commit with all four issues open.
BASE_COMMIT="${BASE_COMMIT:-}"

# aforge shape. Four values, against the same recorded pi and opencode rows:
#
#   node     one leaf, one graph. The executor measured alone, and the drift
#            control: byte for byte the invocation the recorded aforge numbers
#            came from, so a re-run that moves says the harness moved.
#   do       `aforge do "<issue text>"` with no graph written for it. The
#            shipping claim: the compiler decides how the work is shaped and
#            that shape is what gets measured. It used to be called `select`,
#            for the worker it also chose; there is one worker now, so what it
#            still decides is the shape and nothing else.
#   pipeline plan the graph first, then run it. The parallel shape, and what the
#            PR-review comparison used.
#   chat     `aforge chat --once` — the chat surface's brain, one turn, nobody
#            watching. NOT a fourth way to run an errand: it compiles no graph,
#            so there is no delivery gate, no replan and no done.json. It is
#            here to answer a different question than the other three — what a
#            person typing into chat would have got.
AFORGE_MODE="${AFORGE_MODE:-node}"
AFORGE_BIN="${AFORGE_BIN:-aforge}"

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
# Columns are appended, never inserted, so a reader that indexes the first nine
# by position still reads the first nine and a CSV written before a column
# arrived is still a CSV. `subharness_chosen` was dropped in #227, when the last
# thing that could have chosen anything went: every leaf runs the one worker.
echo "harness,issue,seconds,exit,changed_files,passed,failed,cost_usd,cost_source,aforge_mode,nodes_failed" > "$CSV"

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
  if [ -n "$BASE_COMMIT" ]; then
    # A pinned start needs history; --depth 1 would not contain it.
    git clone --quiet "$REPO" "$dir" || return 1
    (cd "$dir" && git checkout --quiet "$BASE_COMMIT") || return 1
  else
    git clone --depth 1 --quiet "$REPO" "$dir" || return 1
  fi
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
# What it writes is byte-identical to what it wrote when the recorded aforge
# numbers were taken, which is the only way this shape can still be the drift
# control.
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

# seconds_of turns a timeout(1) duration into the plain seconds `aforge do`
# wants. The two walls have to be the same wall: a `do` cell held to do's
# fifteen-minute default while the graph shapes get forty is not the same cell.
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
      AFORGE_PRE_ARGV=("$TIMEOUT_BIN" "$CELL_TIMEOUT" "$AFORGE_BIN" plan "$prompt" --brief -model "$MODEL" -o "$cell/graph.json")
      AFORGE_ARGV=("$TIMEOUT_BIN" "$CELL_TIMEOUT" "$AFORGE_BIN" run "$cell/graph.json" -w "$dir" -model "$MODEL" -o "$cell/done.json")
      ;;
    do)
      # No graph written and nothing pinned: the compiler decides the shape,
      # which is the thing being measured. -w is what keeps the writes in the
      # clone, so the diff afterwards is this run's diff; --keep leaves the
      # private store behind, which is where the model audit reads what actually
      # served each node.
      AFORGE_ARGV=("$TIMEOUT_BIN" "$CELL_TIMEOUT" "$AFORGE_BIN" "do" "$prompt" \
        -w "$dir" -keep -model "$MODEL" -timeout "$(seconds_of "$CELL_TIMEOUT")")
      ;;
    chat)
      # The chat surface's brain, one turn, nobody watching (docs/HEADLESS.md
      # section 3). Three things differ from every other shape here and all
      # three are the command's, not this script's:
      #
      #   no -w        the workspace is the process's directory, so run_harness
      #                cd's into the clone for this mode alone.
      #   --yolo       nobody is there to approve a tool call, and the honest
      #                posture for that is said in advance rather than defaulted.
      #   --one-model  a chat session resolves its auxiliary calls through the
      #                tier rows and role pins, so --model alone measures the
      #                machine's /settings as much as the model. Without this
      #                the cell is not the single-model cell the row claims.
      AFORGE_ARGV=("$TIMEOUT_BIN" "$CELL_TIMEOUT" "$AFORGE_BIN" chat \
        --once "$prompt" --yolo --one-model -model "$MODEL")
      ;;
    *)
      AFORGE_ARGV=("$TIMEOUT_BIN" "$CELL_TIMEOUT" "$AFORGE_BIN" run "$cell/graph.json" -w "$dir" -model "$MODEL" -o "$cell/done.json")
      ;;
  esac
}

# nodes_failed reads the run's own verdict out of done.json. The smoke run
# proved why the exit code is not enough: the engine crashed inside its leaf,
# `run` exited 0, and the row read like a pass with a suite that was green
# before the harness arrived. Only aforge's graph shapes have a done.json;
# everyone else is n/a, not 0 — absence of evidence, recorded as absence.
nodes_failed() {
  local harness="$1" cell="$2"
  [ "$harness" = "aforge" ] || { echo "n/a"; return; }
  [ -f "$cell/done.json" ] || { echo "n/a"; return; }
  python3 - "$cell/done.json" <<'PY' 2>/dev/null || echo "n/a"
import json, sys
nodes = json.load(open(sys.argv[1])).get("nodes", [])
print(sum(1 for n in nodes if n.get("state") == "failed"))
PY
}

# render_cell_graph writes the graph the node shape executes. It costs nothing
# and calls no model, so the dry run does it too — a rendered graph is most of
# what there is to check.
render_cell_graph() {
  local prompt="$1" cell="$2"
  case "$AFORGE_MODE" in
    # None of these executes a file. Rendering one anyway would leave a
    # graph.json beside the evidence that nothing in the cell ever read, which
    # is worse than no file: the next person to open the directory reads it as
    # what ran.
    pipeline|do|chat) return 0 ;;
    *) render_graph "$prompt" "$(echo "$prompt" | head -1)" "$cell/graph.json" ;;
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
      # `chat` has no -w: its workspace is wherever the process is standing, so
      # this is the one shape the harness has to walk into the clone for. Every
      # other shape is told the directory and must NOT be cd'd, because their
      # -w is what the diff is measured against.
      if [ "$AFORGE_MODE" = "chat" ]; then
        (cd "$dir" && AFORGE_HOME="$cell/home" "${AFORGE_ARGV[@]}") >>"$log" 2>&1
      else
        "${AFORGE_ARGV[@]}" >>"$log" 2>&1
      fi
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
# on the run summary line, which every graph shape ends with. For pi and
# opencode there is nothing to read, and the account-level delta is not a
# substitute — see bench/README.md.
harness_cost() {
  local harness="$1" log="$2" cell="${3:-}"
  if [ "$harness" != "aforge" ]; then
    echo "n/a,not-self-reported"
    return
  fi
  # `chat --once` ends with the reply, not with a summary line, so there is no
  # $ figure to grep. Its accounting is in the session transcript instead: one
  # usage record per model, each with its own costUsd, and the ones made BESIDE
  # the turn marked aux. Every record counts — reading only the un-aux one is
  # how a cell under-reports the exact spend --one-model exists to make legible.
  if [ "$AFORGE_MODE" = "chat" ]; then
    local total
    total="$(CELL="$cell" python3 - <<'PY' 2>/dev/null
import glob, json, os
total = 0.0
seen = False
for path in glob.glob(os.path.join(os.environ["CELL"], "home/v3/projects/*/*/transcript.jsonl")):
    for line in open(path, errors="replace"):
        try:
            used = json.loads(line).get("usage")
        except Exception:
            continue
        if used and used.get("costUsd") is not None:
            total += float(used["costUsd"])
            seen = True
print(f"{total:.4f}" if seen else "")
PY
)"
    if [ -n "$total" ]; then
      echo "$total,self-reported"
    else
      # No transcript is not $0.00. A cell that died before its first turn
      # spent nothing measurable, and recording zero would read as a cheap run.
      echo "n/a,not-self-reported"
    fi
    return
  fi
  local cost
  cost="$(grep -oE '\$[0-9]+\.[0-9]+' "$log" | tail -1 | tr -d '$')"
  echo "${cost:-0},self-reported"
}

# stow_store moves the kept store next to the rest of the cell's evidence. It is
# the cell's record of which models actually served it, and leaving it in the
# system temp directory is how it gets swept.
stow_store() {
  local log="$1" cell="$2"
  local home
  home="$(grep -oE '^store kept at .*' "$log" | tail -1 | sed -E 's#^store kept at ##')"
  if [ -n "$home" ] && [ -d "$home" ]; then
    rm -rf "$cell/store"
    mv "$home" "$cell/store" 2>/dev/null || true
  fi
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
    pipeline|do|chat) ;;
    *)
      [ -f "$cell/graph.json" ] && printf '          graph:   %s\n' "$cell/graph.json"
      ;;
  esac
}

# ── the grid ────────────────────────────────────────────────────────────────
echo "repo:     $REPO"
echo "model:    $MODEL"
[ -n "$BASE_COMMIT" ] && echo "pinned:   $BASE_COMMIT" || echo "pinned:   (none — clone HEAD; see BASE_COMMIT in this file before trusting rows)"
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
    cost="$(harness_cost "$harness" "$cell/harness.log" "$cell")"
    failed_nodes="$(nodes_failed "$harness" "$cell")"
    stow_store "$cell/harness.log" "$cell"

    echo "$harness,$issue,$seconds,$code,$changed,$counts,$cost,$(mode_of "$harness"),$failed_nodes" >> "$CSV"
    printf '%4ss  exit %-3s %2s files  %s passed/failed  %s%s\n' \
      "$seconds" "$code" "$changed" "${counts/,/ + }" "${cost%%,*}" \
      "$([ "$failed_nodes" != "n/a" ] && [ "$failed_nodes" != "0" ] && echo "  ⚠ $failed_nodes node(s) failed")"
  done
done

echo
if [ "$BENCH_DRY_RUN" = "1" ]; then
  echo "dry run — nothing was cloned, run, or spent"
else
  echo "wrote $CSV"
fi
