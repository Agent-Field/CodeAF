#!/usr/bin/env bash
# verdict.sh — the vocabulary a cell speaks about its own outcome.
#
# The distinction bench/e2e/lib/assert.sh draws is kept here, because it is the
# whole discipline: a cell PASSES or FAILS the things it guards, and RECORDS the
# things it merely wants written down. Anything with a legitimate range — what a
# run cost, how many calls it took, which shape the work took — is recorded, so
# that a change in it is visible without being a failure. A battery that fails on
# a healthy run stops being read, and a battery nobody reads guards nothing.
#
# What this file adds over the e2e one is the outcomes that are neither pass nor
# fail, and that a conversation suite cannot do without:
#
#   unsupported  the arm has no door for this scenario that this suite can
#                honestly drive. Nothing ran. It is excluded from every claim
#                and it is NOT a pass.
#   skipped      something outside the harness stopped the cell — no binary, no
#                key, a model that could not be pinned. The reason travels with
#                the row.
#   mismatch     the cell ran, but a setting could not be matched across arms
#                (an effort rung one arm does not have). The numbers are real
#                and are excluded from comparison, because they were not
#                measured under the same conditions as the arms beside them.
#
# NO OUTCOME EXCEPT `pass` IS EVER COUNTED AS ONE. The summary prints each of
# them separately and the runner's exit code reflects failures only, which is
# what makes "skipped" impossible to read as "passed".

CELL_FAILURES=0
CELL_CHECKS=0
CELL_NOTES=""
CELL_CHECK_JSON=""

assert_begin() {
  CELL_FAILURES=0
  CELL_CHECKS=0
  CELL_NOTES=""
  CELL_CHECK_JSON=""
}

# note appends to the record without judging it.
note() {
  local text="${1//,/;}"
  if [ -z "$CELL_NOTES" ]; then CELL_NOTES="$text"; else CELL_NOTES="$CELL_NOTES;$text"; fi
}

record() {
  local label="$1" value="$2"
  printf '    ·  %-30s %s\n' "$label" "$value"
  note "$label=$value"
}

# check_json accumulates one JSON object per assertion, so the cell's receipt
# carries what was checked and not only how many checks there were. An evidence
# directory whose verdict cannot be re-read line by line is an opinion.
check_json() {
  local outcome="$1" description="$2"
  local row
  row="{\"outcome\":$(json_str "$outcome"),\"check\":$(json_str "$description")}"
  if [ -z "$CELL_CHECK_JSON" ]; then CELL_CHECK_JSON="$row"; else CELL_CHECK_JSON="$CELL_CHECK_JSON,$row"; fi
}

pass() {
  CELL_CHECKS=$((CELL_CHECKS + 1))
  printf '    ✓  %s\n' "$1"
  check_json pass "$1"
}

fail() {
  CELL_CHECKS=$((CELL_CHECKS + 1))
  CELL_FAILURES=$((CELL_FAILURES + 1))
  printf '    ✗  %s\n' "$1"
  note "FAIL:$1"
  check_json fail "$1"
}

check() {
  local description="$1" ok="$2"
  if [ "$ok" = "1" ] || [ "$ok" = "true" ]; then pass "$description"; else fail "$description"; fi
}

check_eq() {
  local description="$1" want="$2" got="$3"
  if [ "$want" = "$got" ]; then pass "$description ($got)"; else fail "$description — want $want, got $got"; fi
}

check_ge() {
  local description="$1" floor="$2" got="$3"
  if [ "${got:-0}" -ge "$floor" ] 2>/dev/null; then
    pass "$description ($got >= $floor)"
  else
    fail "$description — want >= $floor, got ${got:-0}"
  fi
}

check_lt() {
  local description="$1" ceiling="$2" got="$3"
  if awk -v a="${got:-0}" -v b="$ceiling" 'BEGIN{exit !(a<b)}'; then
    pass "$description ($got < $ceiling)"
  else
    fail "$description — want < $ceiling, got ${got:-0}"
  fi
}

# check_file asserts a deliverable exists and is not empty. "The reply said it
# wrote the file" is not evidence that the file is there.
check_file() {
  local description="$1" path="$2"
  if [ -s "$path" ]; then pass "$description"; else fail "$description — missing or empty: $path"; fi
}

check_grep() {
  local description="$1" pattern="$2" path="$3"
  if [ -f "$path" ] && grep -aqiE -- "$pattern" "$path"; then
    pass "$description"
  else
    fail "$description — /$pattern/ not found in $(basename "$path")"
  fi
}

# check_not_grep is how a revision is told from an addition: the answer must no
# longer carry the thing the user changed their mind about.
check_not_grep() {
  local description="$1" pattern="$2" path="$3"
  if [ -f "$path" ] && grep -aqiE -- "$pattern" "$path"; then
    fail "$description — /$pattern/ is still there in $(basename "$path")"
  else
    pass "$description"
  fi
}

# check_grep_all names precisely which anchors were missing. A comparison that
# covers five of six subjects is a specific failure, not a general one.
check_grep_all() {
  local description="$1" path="$2"; shift 2
  local missing="" anchor total=$#
  for anchor in "$@"; do
    if ! { [ -f "$path" ] && grep -aqiE -- "$anchor" "$path"; }; then
      missing="$missing $anchor"
    fi
  done
  if [ -z "$missing" ]; then
    pass "$description (all $total anchors)"
  else
    fail "$description — missing:$missing"
  fi
}

# go_suite_green runs the fixture's own suite after the harness has stopped. The
# suite is the judge; the harness's account of whether it succeeded is not
# evidence.
go_suite_green() {
  local description="$1" dir="$2" log="$3"
  if (cd "$dir" && go test ./... ) >"$log" 2>&1; then
    pass "$description"
    return 0
  fi
  fail "$description — see $(basename "$log")"
  note "go-test-tail=$(grep -aE '^(---|FAIL|ok|# )' "$log" 2>/dev/null | tail -3 | tr '\n' ' ' | tr ',' ';')"
  return 1
}

cell_verdict() {
  if [ "$CELL_FAILURES" -eq 0 ] && [ "$CELL_CHECKS" -gt 0 ]; then echo "pass"; else echo "fail"; fi
}

# ── reading the interactive door's own record ───────────────────────────────

# door_field prints one value from a cell's door.json, or nothing.
door_field() {
  CONV_DOOR="$1" CONV_FIELD="$2" python3 -c '
import json, os
try:
    got = json.load(open(os.environ["CONV_DOOR"]))
except Exception:
    raise SystemExit(0)
value = got.get(os.environ["CONV_FIELD"])
print("" if value is None else value)
'
}

# stamps_ordered prints 1 when both stamps exist and the first is not after the
# second. A missing stamp is 0: an ordering that cannot be shown is not one that
# happened, which is the whole lesson of the cell this was written for.
stamps_ordered() {
  [ -n "${1:-}" ] && [ -n "${2:-}" ] || { echo 0; return; }
  awk -v a="$1" -v b="$2" 'BEGIN { print (a <= b) ? 1 : 0 }'
}

# stamp_gap prints the seconds between two stamps, or "unknown".
stamp_gap() {
  [ -n "${1:-}" ] && [ -n "${2:-}" ] || { echo unknown; return; }
  awk -v a="$1" -v b="$2" 'BEGIN { printf "%.1f", b - a }'
}
