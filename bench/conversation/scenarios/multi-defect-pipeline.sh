#!/usr/bin/env bash
# multi-defect-pipeline — a multi-module correctness fixture, through the print
# door.
#
# One small Python project with four independent defects, one each in parsing,
# validation, aggregation and rendering, plus the command-line pipeline that
# joins them. Each defect is found and fixed on its own; the pipeline's output
# is only right when all four are. The four repairs are small — a CSV reader, a
# set instead of a variable, an ISO year, a tie-break — so this is a calibration
# fixture for separable work and its integration, not a demonstration that there
# is enough work here to be worth splitting up.
#
# IT ASSERTS NOTHING ABOUT SHAPE. Not how many agents ran, not whether a task
# was spawned, not whether the work was done in parallel or in one long turn. A
# harness that repairs all four modules serially passes exactly as a harness
# that fans out does, and should: the person asked for working software.
#
# The judge is external and black-box: it runs the repaired project's own
# command line as a child process, on inputs the workspace never contained, and
# reads stdout, stderr and the exit status. It never imports the candidate and
# never reads its source, so a rewrite passes, a lookup table keyed on the
# visible test data does not, and candidate code cannot reach into the process
# that holds the expected answers. The workspace's own suite is the map, not the
# mark: it is checksummed before the run, and the judge outside decides.

SCENARIO_WORKLOAD="coding"
SCENARIO_DOOR="print"
SCENARIO_ARMS="aforge omp pi opencode"
# The runtime cap: how long the harness may run before the cell is recorded as a
# timeout. It bounds time, not money — what a run costs depends on what it asked
# the provider for, and the guard's ledger is what says so.
SCENARIO_CAP_S="${SCENARIO_CAP_S:-1200}"
# The judging cap, which is separate and much smaller: this fixture is a few
# hundred lines of standard library and a correct run of the whole judge takes
# under two seconds. Judging happens after the harness has stopped, so nothing
# the runner does bounds it — this does.
SCENARIO_JUDGE_CAP_S="${SCENARIO_JUDGE_CAP_S:-90}"
SCENARIO_GUARDS="four independent defects, and a pipeline that needs all four"

# shellcheck source=../fixtures/dutylog.sh
source "$CONV_ROOT/fixtures/dutylog.sh"

SCENARIO_JUDGE_SUM=""
SCENARIO_GUARD_SUM=""

scenario_fixture() {
  fixture_dutylog "$1" "$2" || return 1
  # Held by the runner for the length of the cell. Nothing the harness can write
  # to holds these, which is what makes them worth checking afterwards.
  SCENARIO_JUDGE_SUM="$(fixture_dutylog_judge_sum "$2")"
  SCENARIO_GUARD_SUM="$(fixture_dutylog_guard_sum "$2")"
  [ -n "$SCENARIO_JUDGE_SUM" ] && [ -n "$SCENARIO_GUARD_SUM" ]
}

scenario_prompt() {
  cat <<'TXT'
This Python project turns a duty-log CSV into a weekly report. Its own test
suite fails:

    python3 -m unittest discover -s tests -t .

Four modules under dutylog/ are wrong — parsing, validation, aggregate and
report — and each of the four failures has its own separate cause. Fix the code
under dutylog/ so that the suite passes and the pipeline is correct end to end:

    python3 -m dutylog.cli --input LOG.csv --from 2026-12-28 --to 2027-01-10

SPEC.md is the contract; read it, and prefer it over any guess about what a
failing assertion meant.

Do not change, weaken or delete anything under tests/ or SPEC.md — they are
correct and the code is not. Your work is judged by behaviour on inputs that are
not in this directory, including the combined output of the pipeline.
TXT
}

# judge_group prints pass or fail for one of the judge's groups, or nothing when
# there is no verdict to read.
judge_group() {
  CONV_VERDICT="$1" CONV_GROUP="$2" python3 -c '
import json, os
try:
    groups = json.load(open(os.environ["CONV_VERDICT"]))["groups"]
except Exception:
    raise SystemExit(0)
state = groups.get(os.environ["CONV_GROUP"])
if state is None:
    raise SystemExit(0)
print(("pass" if state["passed"] else "fail") + " %d/%d"
      % (state["cases"] - len(state["failed"]), state["cases"]))
'
}

judge_detail() {
  CONV_VERDICT="$1" CONV_GROUP="$2" python3 -c '
import json, os
try:
    groups = json.load(open(os.environ["CONV_VERDICT"]))["groups"]
except Exception:
    raise SystemExit(0)
state = groups.get(os.environ["CONV_GROUP"]) or {}
print("; ".join(f["case"] for f in state.get("failed", []))[:160].replace(",", ";"))
'
}

# check_group turns one group of the external judge into one assertion.
check_group() {
  local verdict="$1" group="$2" summary
  summary="$(judge_group "$verdict" "$group")"
  case "$summary" in
    pass*) pass "$group behaves ($summary)" ;;
    fail*) fail "$group behaves — $summary: $(judge_detail "$verdict" "$group")" ;;
    *)     fail "$group behaves — the judge produced no verdict for it" ;;
  esac
}

# same_sum answers, without judging: a missing file is not the same file.
same_sum() {
  local got
  got="$(shasum -a 256 "$1" 2>/dev/null | cut -d' ' -f1)"
  [ -n "$got" ] && [ "$got" = "$2" ]
}

check_same_sum() {
  local description="$1" path="$2" want="$3"
  if [ ! -f "$path" ]; then
    fail "$description — the file is gone: $(basename "$path")"
  elif same_sum "$path" "$want"; then
    pass "$description"
  else
    fail "$description — it was modified: $(basename "$path")"
  fi
}

scenario_check() {
  local work="$1" reply="$2" cell="$3" judge="$4"
  local verdict="$judge/judge-verdict.json"
  local copy="$cell/judged-copy"

  # ── the instruments, before they are used ───────────────────────────────
  #
  # The judge and the guard manifest live beside the cell rather than inside the
  # directory the harness was pointed at. That is a location, not a sandbox: an
  # agent can reach them, and these checksums say afterwards whether it did. A
  # changed instrument is not run: a corrupted judge that has been detected must
  # not then be executed, and a corrupted manifest cannot verify anything.
  if [ -z "$SCENARIO_JUDGE_SUM" ] || [ -z "$SCENARIO_GUARD_SUM" ]; then
    fail "no checksums were taken for the judge — the tamper checks did not run"
    return
  fi
  if ! same_sum "$judge/judge_dutylog.py" "$SCENARIO_JUDGE_SUM"; then
    fail "the judge was not modified during the run — it was, so it was not run"
    note "the scenario's behavioural checks did not run: the judge changed"
    return
  fi
  pass "the judge was not modified during the run"

  # ── the question is still the question ──────────────────────────────────
  if ! same_sum "$judge/workspace-guard.json" "$SCENARIO_GUARD_SUM"; then
    fail "the guard manifest was not modified during the run — it was, so nothing it says is trusted"
  else
    pass "the guard manifest was not modified during the run"
    if python3 "$CONV_ROOT/fixtures/dutylog/manifest.py" verify \
         "$work" "$judge/workspace-guard.json" > "$cell/workspace-guard.log" 2>&1; then
      pass "tests/ and SPEC.md were not edited or deleted"
    else
      fail "tests/ and SPEC.md were edited or deleted — see workspace-guard.log"
      note "guard=$(grep -aE '^(changed|missing):' "$cell/workspace-guard.log" | tr '\n' ' ' | tr ',' ';')"
    fi
    local added
    added="$(grep -ac '^added:' "$cell/workspace-guard.log" 2>/dev/null | tail -1)"
    record "files_added_under_guard" "${added:-0}"
  fi

  # ── the judge ───────────────────────────────────────────────────────────
  #
  # It runs on a COPY, so the workspace stays exactly as the harness left it and
  # so anything the judged code writes lands in the copy. The judge's own file
  # is checksummed again afterwards: code under judgement runs while it works.
  rm -rf "$copy"
  if ! cp -R "$work" "$copy" 2>/dev/null; then
    fail "the workspace could not be copied for judging"
    return
  fi
  # The judge bounds each of its own child processes; this bounds the judge. A
  # candidate that hangs on import, or one whose CLI spawns something that
  # outlives it, costs this cell SCENARIO_JUDGE_CAP_S and a recorded failure —
  # never the runner's attention. -k follows an ignored TERM with a KILL.
  #
  # This does NOT reach the candidate's own processes: the judge starts each of
  # them in a new session, so they are not in the group timeout(1) signals. The
  # judge ends those groups itself, on its signalled path as well as its
  # ordinary one, which is where that guarantee actually lives.
  "$TIMEOUT_BIN" -k 10 "$SCENARIO_JUDGE_CAP_S" \
    python3 "$judge/judge_dutylog.py" --project "$copy" --out "$verdict" \
    > "$cell/judge.log" 2>&1
  local judge_exit=$?
  if [ "$judge_exit" -eq 0 ] && [ -s "$verdict" ]; then
    pass "the judge ran to completion"
  elif [ "$judge_exit" -eq 124 ] || [ "$judge_exit" -eq 137 ]; then
    fail "the judge ran to completion — it hit the ${SCENARIO_JUDGE_CAP_S}s judging cap and was killed"
    note "judge_timeout=${SCENARIO_JUDGE_CAP_S}s"
    return
  else
    fail "the judge did not complete (exit $judge_exit) — see judge.log"
    return
  fi
  check_same_sum "the judge was not rewritten while it judged" \
    "$judge/judge_dutylog.py" "$SCENARIO_JUDGE_SUM"

  # One assertion per module, so a three-of-four repair says which one is
  # missing, and one for the pipeline that needs all four.
  check_group "$verdict" parsing
  check_group "$verdict" validation
  check_group "$verdict" aggregate
  check_group "$verdict" report
  check_group "$verdict" integration

  record "judge_cases_failed" \
    "$(CONV_VERDICT="$verdict" python3 -c 'import json,os; got=json.load(open(os.environ["CONV_VERDICT"])); print("%d/%d" % (got["failed"], got["cases"]))' 2>/dev/null || echo unknown)"
  record "modules_behaving" \
    "$(CONV_VERDICT="$verdict" python3 -c 'import json,os; groups=json.load(open(os.environ["CONV_VERDICT"]))["groups"]; print(sum(1 for g in ("parsing","validation","aggregate","report") if groups[g]["passed"]))' 2>/dev/null || echo unknown)"
  record "reply_chars" "$(wc -c < "$reply" | tr -d ' ')"
}
