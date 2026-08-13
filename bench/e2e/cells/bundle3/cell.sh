#!/usr/bin/env bash
# bundle3 — three unrelated asks in one sentence.
#
# The bundle route is the shape the planner never sees: three independent
# requests laid out side by side, no spine, no synthesis of one into another.
# It is worth its own cell because it is the one wide shape that can be reached
# without planning, and because collapsing it is cheap to do by accident — a
# compiler that reads "and" as one errand turns three parallel packages into one
# sequential leaf and nothing in the output says so.
#
# So the assertions are: the route was taken, three leaves ran together, and all
# three packages exist and compile. The suite is the judge for the last part.
#
# CALIBRATION NOTE. Of the seven cells this is the only one whose *shape*
# assertions have not been checked against a real run of this exact task — see
# the same note in README.md. The route and parallelism assertions follow from
# internal/store/scalegate.go, but whether this particular sentence is read as a
# bundle is a model's judgement. Treat the first real run as calibration: if it
# routes 'planned' with three leaves the work is still correct, and the honest
# fix is to relax the route assertion to a record, not to reword the task until
# it produces the answer the battery wanted.

CELL_GUARDS="three independent asks route as a bundle, run in parallel, and all compile"
CELL_BUDGET=1800

CELL_TASK='In this Go module, write three completely independent packages. First, package slugify with a function Slugify(string) string that lowercases text, replaces any run of non-alphanumeric characters with a single hyphen, and trims leading and trailing hyphens. Second, package humanize with a function Bytes(int64) string that formats a byte count as a human-readable string using B, KB, MB, and GB with one decimal place. Third, package retry with a function Do(attempts int, fn func() error) error that calls fn up to attempts times and returns the last error if every attempt fails. None of the three packages may import either of the others. Give each package its own tests, and make sure `go test ./...` passes.'

cell_fixture() {
  local dir="$1"
  "$E2E_ROOT/fixtures/toolkit.sh" "$dir" >/dev/null
}

cell_check() {
  local dir="$1" stdout="$2" stderr="$3" code="$4"

  check_eq "run exited cleanly" 0 "$code"

  # All three packages present. Located by name rather than by path, since
  # toolkit/slugify and toolkit/pkg/slugify are both reasonable choices.
  local part found_all=1
  for part in slugify humanize retry; do
    if find "$dir" -type d -name "$part" 2>/dev/null | grep -q .; then
      pass "package $part exists"
    else
      fail "package $part is missing"
      found_all=0
    fi
  done

  go_suite_green "go test ./... is green" "$dir" "$dir/.e2e-gotest.log"

  # Each package must carry its own tests.
  for part in slugify humanize retry; do
    local dirpath
    dirpath="$(find "$dir" -type d -name "$part" 2>/dev/null | head -1)"
    if [ -n "$dirpath" ] && ls "$dirpath"/*_test.go >/dev/null 2>&1; then
      pass "package $part has tests"
    else
      fail "package $part has no _test.go"
    fi
  done

  # Independence, as the task required: none may import another.
  local leak=0
  for part in slugify humanize retry; do
    local dirpath
    dirpath="$(find "$dir" -type d -name "$part" 2>/dev/null | head -1)"
    [ -n "$dirpath" ] || continue
    local other
    for other in slugify humanize retry; do
      [ "$other" = "$part" ] && continue
      if grep -rqaE "\"[^\"]*/$other\"" "$dirpath" --include='*.go' 2>/dev/null; then
        leak=1
        note "leak:$part-imports-$other"
      fi
    done
  done
  check "the three packages are independent" "$([ "$leak" = "0" ] && echo 1 || echo 0)"

  record "packages_found" "$found_all"
}

cell_shape() {
  local db="$1"

  local route parts concurrent
  route="$(scale_route "$db")"
  parts="$(scale_field "$db" parts)"
  concurrent="$(concurrent_starts "$db" 2)"

  check_eq "scale_gate route is bundle" "bundle" "$route"
  check_ge "gate declared at least three parts" 3 "${parts:-0}"
  check_ge "three leaves started within 2s of each other" 3 "$concurrent"
  check_ge "a synthesis node is gated on three edges" 3 "$(max_fan_in "$db")"
  check_eq "no failed nodes" 0 "$(failed_count "$db")"

  record "route" "$route"
  record "parts" "${parts:-none}"
  record "nodes" "$(node_count "$db")"
  record "concurrent_starts_2s" "$concurrent"
  record "max_fan_in" "$(max_fan_in "$db")"
  record "edges" "$(edge_count "$db")"
  record "delivery_gate" "$(delivery_pass "$db")"
}

CELL_WALL_CEILING=1800
