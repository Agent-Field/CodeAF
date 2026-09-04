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

SCENARIO_WORKLOAD="conversation"
SCENARIO_DOOR="interactive"
SCENARIO_ARMS="aforge omp pi"
SCENARIO_CAP_S="${SCENARIO_CAP_S:-420}"
SCENARIO_GUARDS="a requirement changed while the work was in flight"

# shellcheck source=../fixtures/slowwork.sh
source "$CONV_ROOT/fixtures/slowwork.sh"

scenario_fixture() { fixture_slowwork "$1"; }

scenario_turns() {
  local plan="$2"
  {
    printf 'ready\tFirst run ./slow-build.sh here — it takes about a minute and I want it done. While it runs, start writing report.md: one markdown bullet per service in services.txt, each naming the service and its port.\n'
    printf 'busy\tChange of plan: I need that as CSV, not markdown. Write report.csv with a header line service,port and one row per service, and make sure report.md does not exist at the end.\n'
  } > "$plan"
}

scenario_check() {
  local work="$1" transcript="$2"
  check_file "the revised deliverable exists (report.csv)" "$work/report.csv"
  if [ -s "$work/report.csv" ]; then
    check_grep "report.csv has the asked-for header" "^ *service *, *port" "$work/report.csv"
    check_grep_all "report.csv carries every service and port" "$work/report.csv" \
      "kestrel *, *8431" "gasket *, *9002" "flange *, *7710"
    local rows; rows="$(grep -acE '^[a-z]+ *, *[0-9]+' "$work/report.csv" 2>/dev/null || echo 0)"
    check_eq "report.csv has one row per service" 3 "$rows"
  fi
  # The revision was to REPLACE the deliverable. A run that leaves both files
  # has answered "yes" to both instructions and resolved nothing.
  if [ -e "$work/report.md" ]; then
    fail "the superseded report.md was left behind"
  else
    pass "the superseded report.md is gone"
  fi
  # The revision must not have cost the job that was already running.
  check_file "the slow job still finished" "$work/build.log"
  record "revision_acknowledged_on_screen" \
    "$(grep -acE '(csv|CSV)' "$transcript" 2>/dev/null || echo 0)"
}
