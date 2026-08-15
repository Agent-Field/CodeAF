#!/usr/bin/env bash
# dynamic — a perf triage with no failing test to point the way.
#
# Three pipelines over one dataset. Nothing is broken, the suite is green, and
# no line of code says "slow"; dedupe simply rescans its accumulated output for
# every input row. Measured on the generated fixture: ingest 7ms, rollup 1ms,
# dedupe 8161ms. The only evidence is the clock, so the run has to go and take a
# measurement before it can know what to fix — which is why this cell also
# checks that the plan was STAGED, diagnose before fix, rather than one lunge.
#
# It carries a second, unrelated sentinel. The revise.go rootID bug had a
# finished report written correctly to disk and never returned to the caller:
# the file was there, the run said it was done, and stdout carried nothing but
# ceremony. Every file-based assertion in this battery would have passed that
# run. So this cell asserts the report exists AND that its content reached
# stdout, because those turned out to be two different things.

CELL_GUARDS="quadratic stage found and fixed by measurement; report on disk AND on stdout; staged plan"
CELL_BUDGET=1800

CELL_TASK='This Go module has three data pipelines: ingest, dedupe, and rollup. One of them is far slower than the other two. Use `go run ./cmd/pipebench -pipeline=all` to measure them, work out which stage is slow and why, and fix it so it runs in time comparable to the others. The behaviour must not change: `go test ./...` must still pass, and the pipeline must still return the same records in the same order. Then write a short report to perf-report.md covering which stage was slow, the measured before and after timings, the root cause, and what you changed. Print the full report as your final answer as well as writing it to the file.'

cell_fixture() {
  local dir="$1"
  "$E2E_ROOT/fixtures/pipelines.sh" "$dir" >/dev/null

  # The baseline is MEASURED, not assumed. The assertion afterwards is a ratio
  # against this number, so the cell stays honest on a slower or faster machine
  # and does not encode one laptop's milliseconds as a constant.
  local baseline
  baseline="$( (cd "$dir" && go run ./cmd/pipebench -pipeline=dedupe) 2>/dev/null \
    | grep -aoE 'ms=[0-9]+' | cut -d= -f2 )"
  echo "${baseline:-0}" > "$dir/.e2e-dedupe-baseline-ms"

  # The dedupe output count, so a fix that is fast because it drops records is
  # caught even if the unit tests are too small to notice.
  local out
  out="$( (cd "$dir" && go run ./cmd/pipebench -pipeline=dedupe) 2>/dev/null \
    | grep -aoE 'out=[0-9]+' | cut -d= -f2 )"
  echo "${out:-0}" > "$dir/.e2e-dedupe-out"
}

cell_check() {
  local dir="$1" stdout="$2" stderr="$3" code="$4"
  local report="$dir/perf-report.md"

  check_eq "run exited cleanly" 0 "$code"
  go_suite_green "go test ./... is green" "$dir" "$dir/.e2e-gotest.log"

  # ── the fix actually made it fast ────────────────────────────────────────
  local baseline want_out after after_out
  baseline="$(cat "$dir/.e2e-dedupe-baseline-ms" 2>/dev/null || echo 0)"
  want_out="$(cat "$dir/.e2e-dedupe-out" 2>/dev/null || echo 0)"

  local measured
  measured="$( (cd "$dir" && go run ./cmd/pipebench -pipeline=dedupe) 2>/dev/null )"
  after="$(echo "$measured" | grep -aoE 'ms=[0-9]+' | cut -d= -f2)"
  after_out="$(echo "$measured" | grep -aoE 'out=[0-9]+' | cut -d= -f2)"
  after="${after:-999999}"
  after_out="${after_out:-0}"

  record "dedupe_before_ms" "$baseline"
  record "dedupe_after_ms" "$after"
  record "dedupe_out_before" "$want_out"
  record "dedupe_out_after" "$after_out"

  # A tenfold floor. The real improvement measured on a correct fix is about
  # 1600x (8161ms to 5ms), so ten is not a demanding bar — it is set low on
  # purpose so that a loaded machine cannot fail a genuinely fixed run.
  if [ "$baseline" -gt 0 ] 2>/dev/null; then
    local ceiling=$((baseline / 10))
    check_lt "dedupe is at least 10x faster than baseline" "$ceiling" "$after"
    record "speedup_x" "$(awk -v b="$baseline" -v a="$after" 'BEGIN{if(a>0) printf "%.1f", b/a; else print "inf"}')"
  else
    fail "no usable baseline was measured for dedupe"
  fi

  # Fast and wrong is not fixed.
  check_eq "dedupe still returns the same record count" "$want_out" "$after_out"

  # ── the report exists ────────────────────────────────────────────────────
  check_file "perf-report.md written" "$report"
  check_grep "report names the slow stage" "dedupe" "$report"
  check_grep "report gives before/after timings" "[0-9]+ *(ms|milli|s\b)" "$report"

  # ── and its content reached stdout (the revise.go rootID sentinel) ───────
  #
  # Three separate signals, because the failure mode was subtle: the run
  # completed, the file was correct, and stdout had only the summary line.
  check_grep "stdout names the stage that was fixed" "dedupe" "$stdout"

  local stdout_chars
  stdout_chars="$(wc -c < "$stdout" 2>/dev/null | tr -d ' ')"
  record "stdout_chars" "${stdout_chars:-0}"
  check_ge "stdout carries more than a summary line" 300 "${stdout_chars:-0}"

  # The strongest form: distinctive lines from the file, found verbatim in
  # stdout. Substantial lines only — a shared heading like "## Root cause"
  # would match between two unrelated documents.
  local overlap=0 line
  while IFS= read -r line; do
    [ ${#line} -ge 40 ] || continue
    if grep -aqF -- "$line" "$stdout" 2>/dev/null; then
      overlap=$((overlap + 1))
    fi
  done < <(awk '{ print length"\t"$0 }' "$report" 2>/dev/null | sort -rn | cut -f2- | head -10)
  record "report_lines_echoed" "$overlap"
  check_ge "report content itself reached stdout" 1 "$overlap"
}

cell_shape() {
  local db="$1"

  local stages questions
  stages="$(plan_graph_stages "$db")"
  questions="$(plan_has_questions "$db")"

  # Diagnose, then fix: two stages at minimum. A single-stage plan for this ask
  # is a run that decided what was wrong before it measured anything.
  check_ge "plan was staged (diagnose before fix)" 2 "$stages"
  check_eq "no failed nodes" 0 "$(failed_count "$db")"

  record "plan_stages" "$stages"
  record "open_questions" "$questions"
  record "route" "$(scale_route "$db")"
  record "nodes" "$(node_count "$db")"
  record "plan_nodes" "$(plan_graph_nodes "$db")"
  record "growth_rounds" "$(growth_rounds "$db")"
  record "concurrent_starts_2s" "$(concurrent_starts "$db" 2)"
  record "max_fan_in" "$(max_fan_in "$db")"
  record "delivery_gate" "$(delivery_pass "$db")"
}

CELL_WALL_CEILING=1800
