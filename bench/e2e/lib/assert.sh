#!/usr/bin/env bash
# assert.sh — the verdict vocabulary every cell speaks.
#
# A cell says PASS or FAIL for each thing it guards, and RECORD for each thing
# it merely wants written down. The distinction is the whole discipline: an
# assertion that fails must mean something regressed, so anything with a
# legitimate range — which shape the compiler picked, what a run cost — is
# recorded rather than asserted. A battery that fails on a healthy run stops
# being read, and a battery nobody reads guards nothing.
#
# Sourced by run.sh. State lives in three globals reset by assert_begin.

# CELL_FAILURES counts failed assertions in the current cell.
# CELL_CHECKS counts assertions made.
# CELL_NOTES accumulates the record, semicolon-separated — never comma, because
# it lands in a CSV field.
CELL_FAILURES=0
CELL_CHECKS=0
CELL_NOTES=""

assert_begin() {
  CELL_FAILURES=0
  CELL_CHECKS=0
  CELL_NOTES=""
}

# note appends to the record without judging it.
note() {
  local text="${1//,/;}"
  if [ -z "$CELL_NOTES" ]; then CELL_NOTES="$text"; else CELL_NOTES="$CELL_NOTES;$text"; fi
}

# record prints a measured value and files it in the notes.
record() {
  local label="$1" value="$2"
  printf '    ·  %-28s %s\n' "$label" "$value"
  note "$label=$value"
}

# pass/fail are the two halves of an assertion's outcome. Callers normally go
# through check_* below rather than calling these directly.
pass() {
  CELL_CHECKS=$((CELL_CHECKS + 1))
  printf '    ✓  %s\n' "$1"
}

fail() {
  CELL_CHECKS=$((CELL_CHECKS + 1))
  CELL_FAILURES=$((CELL_FAILURES + 1))
  printf '    ✗  %s\n' "$1"
  note "FAIL:$1"
}

# check is the general form: a description and an already-evaluated verdict.
check() {
  local description="$1" ok="$2"
  if [ "$ok" = "1" ] || [ "$ok" = "true" ]; then pass "$description"; else fail "$description"; fi
}

# check_eq asserts an exact value and shows what it got when it differs.
check_eq() {
  local description="$1" want="$2" got="$3"
  if [ "$want" = "$got" ]; then pass "$description ($got)"; else fail "$description — want $want, got $got"; fi
}

# check_ge asserts a floor. Most shape assertions are floors rather than exact
# counts: a run that produced more parallelism than required has not regressed.
check_ge() {
  local description="$1" floor="$2" got="$3"
  if [ "${got:-0}" -ge "$floor" ] 2>/dev/null; then
    pass "$description ($got >= $floor)"
  else
    fail "$description — want >= $floor, got ${got:-0}"
  fi
}

# check_lt asserts a ceiling, in float-tolerant form for wall clocks.
check_lt() {
  local description="$1" ceiling="$2" got="$3"
  if awk -v a="${got:-0}" -v b="$ceiling" 'BEGIN{exit !(a<b)}'; then
    pass "$description ($got < $ceiling)"
  else
    fail "$description — want < $ceiling, got ${got:-0}"
  fi
}

# check_file asserts a deliverable exists and is not empty. "The run said it
# wrote the file" is not evidence that the file is there.
check_file() {
  local description="$1" path="$2"
  if [ -s "$path" ]; then pass "$description"; else fail "$description — missing or empty: $path"; fi
}

# check_grep asserts a pattern appears in a file, case-insensitively. This is
# the anchor test: the deliverable has to actually name the thing it was asked
# about, not merely be long.
check_grep() {
  local description="$1" pattern="$2" path="$3"
  if [ -f "$path" ] && grep -aqiE -- "$pattern" "$path"; then
    pass "$description"
  else
    fail "$description — /$pattern/ not found in $(basename "$path")"
  fi
}

# check_grep_all asserts every one of a list of anchors appears in a file, and
# names precisely which ones did not. A comparison that covers five of six
# subjects is a specific failure, not a general one.
check_grep_all() {
  local description="$1" path="$2"; shift 2
  local missing=""
  local anchor
  for anchor in "$@"; do
    if ! { [ -f "$path" ] && grep -aqiE -- "$anchor" "$path"; }; then
      missing="$missing $anchor"
    fi
  done
  if [ -z "$missing" ]; then
    pass "$description (all $# anchors)"
  else
    fail "$description — missing:$missing"
  fi
}

# check_unchanged asserts a file the run was told not to touch was not touched.
# It is how "made the tests pass" is told apart from "made the tests agree".
check_unchanged() {
  local description="$1" path="$2" want_sum="$3"
  local got_sum
  got_sum="$(shasum -a 256 "$path" 2>/dev/null | cut -d' ' -f1)"
  if [ -z "$got_sum" ]; then
    fail "$description — file is gone: $path"
  elif [ "$got_sum" = "$want_sum" ]; then
    pass "$description"
  else
    fail "$description — modified: $path"
  fi
}

# go_suite_green runs the fixture's own suite and asserts it passes. The suite
# is the judge, run after the harness has stopped, exactly as bench/run.sh runs
# pytest: the harness's own account of whether it succeeded is not evidence.
go_suite_green() {
  local description="$1" dir="$2" log="$3"
  if (cd "$dir" && go test ./... ) >"$log" 2>&1; then
    pass "$description"
    return 0
  fi
  fail "$description — see $(basename "$log")"
  note "go-test-tail=$(grep -aE '^(---|FAIL|ok|# )' "$log" | tail -3 | tr '\n' ' ' | tr ',' ';')"
  return 1
}

# cell_verdict turns the tally into the CSV's quality_pass column.
cell_verdict() {
  if [ "$CELL_FAILURES" -eq 0 ] && [ "$CELL_CHECKS" -gt 0 ]; then echo "pass"; else echo "fail"; fi
}
