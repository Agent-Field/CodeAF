#!/usr/bin/env bash
# revision-midwork — the interactive door: the person changes their mind while
# the work is in flight.
#
# This is the failure mode that costs real money in real use. The harness is
# asked for one deliverable, and halfway through is told the shape was wrong.
# Three outcomes are possible and only one of them is right:
#
#   right    the revised deliverable exists, correct, and the abandoned one is
#            not left lying around
#   stale    the original is delivered anyway — the revision was heard and not
#            acted on, or was queued until after the work finished
#   both     both files exist, which is the answer that looks like compliance
#            and leaves the person to work out which one to trust
#
# The checks below tell those three apart on disk. Nothing is asserted about
# how the harness organised itself to do it.
#
# WHAT THIS CELL DOES NOT ASSERT, deliberately. The revised file is required to
# be written AFTER the revision was sent — otherwise it is not an answer to the
# revision — but NOT before the slow job finishes. The person asked for both the
# build and the report; finishing the build first and then writing the CSV is a
# legitimate way to do what was asked, and failing it would be scoring
# eagerness rather than correctness. The timing is recorded so it can be read.
# The sibling cell, followup-while-working, is the one that asserts an answer
# arrived mid-work, because there the whole point of the question is that it
# should not have to wait.

SCENARIO_WORKLOAD="conversation"
SCENARIO_DOOR="interactive"
SCENARIO_ARMS="aforge omp pi"
SCENARIO_CAP_S="${SCENARIO_CAP_S:-420}"
SCENARIO_GUARDS="a requirement changed while the work was in flight"

# shellcheck source=../fixtures/slowwork.sh
source "$CONV_ROOT/fixtures/slowwork.sh"

scenario_fixture() { fixture_slowwork "$1"; }

scenario_turns() {
  local work="$1" plan="$2"
  SCENARIO_WORK_START="$work/.phases/build-started"
  SCENARIO_WORK_DONE="$work/.phases/build-finished"
  SCENARIO_ANSWER_RE="report\\.csv"
  {
    printf 'ready\tFirst run ./slow-build.sh here — it takes about a minute and I want it done. While it runs, start writing report.md: one markdown bullet per service in services.txt, each naming the service and its port.\n'
    printf 'midwork\tChange of plan: I need that as CSV, not markdown. Write report.csv with a header line service,port and one row per service, and make sure report.md does not exist at the end.\n'
  } > "$plan"
}

scenario_check() {
  local work="$1" transcript="$2" cell="$3"
  local door="$cell/door.json"
  local invocations
  invocations="$(wc -l < "$work/.phases/build-invocations" 2>/dev/null | tr -d '[:space:]')"
  check "the prepared action ran exactly once" "$([ "$invocations" = "1" ] && echo 1 || echo 0)"
  local sent started finished
  sent="$(door_field "$door" midwork_sent_at)"
  started="$(door_field "$door" work_started_at)"
  finished="$(door_field "$door" work_finished_at)"
  record "witness" "$(door_field "$door" witness)"

  # The steer has to have landed inside the work, not before it began.
  check "the work actually started" "$([ -n "$started" ] && echo 1 || echo 0)"
  check "the revision was sent after the work started" "$(stamps_ordered "$started" "$sent")"
  check "and before the work finished" \
    "$([ -z "$finished" ] && echo 1 || stamps_ordered "$sent" "$finished")"

  check_file "the revised deliverable exists (report.csv)" "$work/report.csv"
  if [ -s "$work/report.csv" ]; then
    # The file has to be the answer to the REVISION, so it must not predate it.
    # Whether it also beat the build to the finish is recorded, not judged.
    check "report.csv was written after the revision was sent" \
      "$(stamps_ordered "$sent" "$(file_mtime "$work/report.csv")")"
    record "csv_before_build_finished" \
      "$([ -n "$finished" ] && stamps_ordered "$(file_mtime "$work/report.csv")" "$finished" || echo unknown)"
    check_grep "report.csv has the asked-for header" "^ *service *, *port" "$work/report.csv"
    check_grep_all "report.csv carries every service and port" "$work/report.csv" \
      "kestrel *, *8431" "gasket *, *9002" "flange *, *7710"
    local rows; rows="$(grep -acE '^[a-z]+ *, *[0-9]+' "$work/report.csv" 2>/dev/null || echo 0)"
    check_eq "report.csv has one row per service" 3 "$rows"
  fi

  # A run that leaves both files has answered both instructions and resolved
  # nothing.
  if [ -e "$work/report.md" ]; then
    fail "the superseded report.md was left behind"
  else
    pass "the superseded report.md is gone"
  fi
  check_file "the slow job still finished" "$work/build.log"
}
