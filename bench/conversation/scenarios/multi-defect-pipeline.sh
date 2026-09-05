#!/usr/bin/env bash
# multi-defect-pipeline — substantial independent work, and the integration of
# it, through the print door.
#
# One small Python project with four independent defects, one each in parsing,
# validation, aggregation and rendering, plus the command-line pipeline that
# joins them. Each defect is found and fixed on its own; the pipeline's output
# is only right when all four are. That is the whole shape of this cell: real
# separable work whose parts have to come back together.
#
# IT ASSERTS NOTHING ABOUT SHAPE. Not how many agents ran, not whether a task
# was spawned, not whether the work was done in parallel or in one long turn. A
# harness that repairs all four modules serially passes exactly as a harness
# that fans out does, and should: the person asked for working software.
#
# The judge is external, and it is behavioural: it imports the repaired project
# from a copy, runs it on inputs the workspace never contained, and runs the CLI
# as a subprocess. It never reads the source text, so a rewrite passes and a
# lookup table keyed on the visible test data does not. The workspace's own
# suite is the map, not the mark: it is checksummed before the run, and it is
# the judge outside that decides.

SCENARIO_WORKLOAD="coding"
SCENARIO_DOOR="print"
SCENARIO_ARMS="aforge omp pi opencode"
# Four modules of work, not one boundary: this cap is a spend backstop, and a
# harness still running at it has produced nothing.
SCENARIO_CAP_S="${SCENARIO_CAP_S:-1200}"
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

check_same_sum() {
  local description="$1" path="$2" want="$3" got
  got="$(shasum -a 256 "$path" 2>/dev/null | cut -d' ' -f1)"
  if [ -z "$got" ]; then
    fail "$description — the file is gone: $(basename "$path")"
  elif [ "$got" = "$want" ]; then
    pass "$description"
  else
    fail "$description — it was modified: $(basename "$path")"
  fi
}

scenario_check() {
  local work="$1" reply="$2" cell="$3" judge="$4"
  local verdict="$judge/judge-verdict.json"
  local copy="$cell/judged-copy"

  # ── the marker, before it is used ───────────────────────────────────────
  #
  # The judge and the guard manifest live beside the cell rather than in the
  # workspace, and nothing the harness was asked to do goes near them. Checking
  # is detection, not prevention: nothing here sandboxes a filesystem.
  if [ -z "$SCENARIO_JUDGE_SUM" ] || [ -z "$SCENARIO_GUARD_SUM" ]; then
    fail "no checksums were taken for the judge — the tamper checks did not run"
    return
  fi
  check_same_sum "the judge was not modified during the run" \
    "$judge/judge_dutylog.py" "$SCENARIO_JUDGE_SUM"
  check_same_sum "the guard manifest was not modified during the run" \
    "$judge/workspace-guard.json" "$SCENARIO_GUARD_SUM"

  # ── the question is still the question ──────────────────────────────────
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
  if python3 "$judge/judge_dutylog.py" --project "$copy" --out "$verdict" \
       > "$cell/judge.log" 2>&1 && [ -s "$verdict" ]; then
    pass "the judge ran to completion"
  else
    fail "the judge did not complete — see judge.log"
    return
  fi
  check_same_sum "the judge was not rewritten while it judged" \
    "$judge/judge_dutylog.py" "$SCENARIO_JUDGE_SUM"

  # The code under judgement runs in the judge's own process, so a verdict is
  # believed only when it is the shape this fixture ships: every case accounted
  # for, and the project it names is the copy that was handed to it. This is a
  # sanity check on the artifact, not a sandbox — see the README.
  check "the verdict accounts for the whole fixture and names the copy it judged" \
    "$(CONV_VERDICT="$verdict" CONV_COPY="$copy" python3 -c '
import json, os
try:
    got = json.load(open(os.environ["CONV_VERDICT"]))
except Exception:
    print(0); raise SystemExit(0)
print(1 if got.get("cases", 0) >= 30
      and os.path.realpath(got.get("project", "")) == os.path.realpath(os.environ["CONV_COPY"])
      and set(got.get("groups", {})) == {"parsing", "validation", "aggregate", "report", "integration"}
      else 0)
' 2>/dev/null)"

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
