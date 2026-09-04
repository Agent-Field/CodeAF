#!/usr/bin/env bash
# selftest.sh — the rig testing itself, deterministically, offline, for free.
#
# WHAT THIS IS FOR. A benchmark harness fails quietly. A flag that moved between
# versions, an exit code nobody read, a cost parser that saw no usage and wrote
# zero — each of those produces a full CSV of plausible rows, and none of them
# announces itself. So every one of those failures is staged here against fake
# binaries whose behaviour is known, and the rig has to catch it. If it does
# not, this file fails and says which lie got through.
#
# Nothing here calls a model, spends anything, or reaches the network. It is
# meant to be run before and after every change to this suite:
#
#   bench/conversation/test/selftest.sh
#
# Cases, and the lie each one stages:
#
#   dry-run        composing an invocation must not execute one
#   ok             a healthy cell passes, and its cost is what one message cost
#                  and not three times it (all three arms repeat the message)
#   exit3          a non-zero exit with a perfect reply is a FAILURE
#   badoutput      a fluent wrong answer is a FAILURE
#   hang           a harness that never returns is a TIMEOUT, never a pass
#   nocost         no usage reported is cost UNKNOWN, never 0.00
#   wrongmodel     a model off the allowlist FAILS, print door and aux role both
#   nopin          an arm whose catalog cannot pin the id is SKIPPED, not run
#   unsupported    a scenario an arm has no door for is UNSUPPORTED, not passed
#   interactive    the tmux door delivers a followup typed while work is running
#   neverbusy      no window of work to interrupt is a FAILURE, not a pass
#   deaf           a followup that is ignored is a FAILURE
set -uo pipefail

TEST_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONV_ROOT="$(cd "$TEST_ROOT/.." && pwd)"
RUN="$CONV_ROOT/run.sh"
FAKE="$TEST_ROOT/fake"
WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/afconv-selftest.XXXXXX")"
LOGDIR="${CONV_SELFTEST_LOGS:-$WORKDIR/logs}"
mkdir -p "$LOGDIR"

# The fakes are configured through the environment, and a harness process only
# inherits what this suite carries on purpose (lib/common.sh). Naming the FAKE_*
# variables here is what lets them through — and the credential scrubbing test
# below still holds, because nothing in this list is a credential.
export CONV_PASS_ENV="FAKE_MODE FAKE_MARKER FAKE_ENV_REPORT FAKE_TUI_MODE FAKE_TUI_BUSY FAKE_CATALOG_ID"

PASSED=0
FAILED=0
SKIPPED=0

say() { printf '%s\n' "$*"; }
ok()   { PASSED=$((PASSED + 1)); printf '  ✓ %s\n' "$1"; }
bad()  { FAILED=$((FAILED + 1)); printf '  ✗ %s\n' "$1"; }
# A skipped check is neither of those and is counted on its own. The exit code
# below is non-zero when anything was skipped unless the caller said that was
# expected: "skipped" quietly reading as "passed" is the failure this whole
# suite is built to prevent, and its own tests must not commit it.
skip() { SKIPPED=$((SKIPPED + 1)); printf '  ⊘ %s (skipped, NOT passed)\n' "$1"; }

# field reads one value out of a run's results.jsonl.
field() {
  local results="$1" key="$2"
  CONV_RESULTS="$results" CONV_KEY="$key" python3 -c '
import json, os
rows = [json.loads(line) for line in open(os.environ["CONV_RESULTS"]) if line.strip()]
if not rows:
    print("NO-ROWS")
else:
    value = rows[-1].get(os.environ["CONV_KEY"])
    print("null" if value is None else value)
'
}

# case_run drives one whole run of the battery against the fakes and prints
# where its results landed. Every case gets its own output directory, its own
# CSV and its own marker file.
case_run() {
  local name="$1"; shift
  local out="$WORKDIR/$name"
  mkdir -p "$out"
  CONV_OUT="$out/evidence" CONV_CSV="$out/results.csv" CONV_RUN_ID="$name" \
  FAKE_MARKER="$out/marker" \
    "$RUN" "$@" > "$LOGDIR/$name.log" 2>&1
  CASE_EXIT=$?
  CASE_OUT="$out"
  CASE_RESULTS="$out/evidence/results.jsonl"
  return 0
}

# The fakes stand in for arms whose auxiliary calls this suite cannot pin in
# advance, so the cases that are about something else turn the role-pin gate
# off explicitly. The gate itself is tested on its own, below.
print_case() {
  local name="$1"; shift
  case_run "$name" --arms pi --scenarios data-tally --cap 60 --role-pin off "$@"
}

say "bench/conversation selftest — fake binaries, no model, no spend"
say "workdir: $WORKDIR"
say

# ── the dry run must not run anything ───────────────────────────────────────
say "dry-run:"
PI_BIN="$FAKE/pi-fake.sh" AFORGE_BIN="$FAKE/aforge-fake.sh" \
  case_run dryrun --role-pin off --arms pi,aforge --scenarios data-tally --dry-run
[ "$CASE_EXIT" -eq 0 ] && ok "a dry run exits 0" || bad "a dry run should exit 0 (got $CASE_EXIT)"
if [ -e "$CASE_OUT/marker" ]; then
  bad "the dry run EXECUTED a harness — the marker file exists"
else
  ok "the dry run executed nothing"
fi
grep -q 'deepseek/deepseek-v4-flash-0731' "$LOGDIR/dryrun.log" \
  && ok "the composed argv carries the pinned model" \
  || bad "the composed argv does not name the pin"
grep -q 'no model call, no spend' "$LOGDIR/dryrun.log" \
  && ok "the dry run says what it did not do" \
  || bad "the dry run does not say it spent nothing"
say

# ── a healthy cell, and the cost of one message ─────────────────────────────
say "ok (print door, pi-shaped receipts):"
FAKE_MODE=ok PI_BIN="$FAKE/pi-fake.sh" print_case ok
[ "$(field "$CASE_RESULTS" verdict)" = "pass" ] \
  && ok "a healthy cell passes" || bad "a healthy cell did not pass: $(field "$CASE_RESULTS" verdict)"
[ "$(field "$CASE_RESULTS" comparable)" = "yes" ] \
  && ok "a healthy cell is comparable" || bad "a healthy cell was marked not-comparable"
# The fake repeats its message on message_end, turn_end and agent_end, exactly
# as the real ones do. One message cost 6.34e-05; three would be 1.902e-04.
COST="$(field "$CASE_RESULTS" cost_usd)"
python3 -c "import sys; sys.exit(0 if abs(float('$COST') - 6.34e-05) < 1e-9 else 1)" \
  && ok "the same message repeated three times is counted once ($COST)" \
  || bad "the receipt reader double-counted repeated messages (got $COST)"
[ "$(field "$CASE_RESULTS" cost_source)" = "self-reported" ] \
  && ok "cost is recorded as self-reported" || bad "cost source is wrong"
say

# ── a dropped exit code ─────────────────────────────────────────────────────
say "exit3 (a perfect reply and a non-zero exit):"
FAKE_MODE=exit3 PI_BIN="$FAKE/pi-fake.sh" print_case exit3
[ "$(field "$CASE_RESULTS" exit)" = "3" ] \
  && ok "the exit code is recorded" || bad "the exit code was lost: $(field "$CASE_RESULTS" exit)"
[ "$(field "$CASE_RESULTS" verdict)" = "fail" ] \
  && ok "a non-zero exit fails the cell" || bad "a non-zero exit did not fail the cell"
[ "$CASE_EXIT" -ne 0 ] \
  && ok "the runner's own exit code reflects the failure" || bad "the runner exited 0 with a failed cell"
say

# ── a fluent wrong answer ───────────────────────────────────────────────────
say "badoutput (confident, wrong):"
FAKE_MODE=badoutput PI_BIN="$FAKE/pi-fake.sh" print_case badoutput
[ "$(field "$CASE_RESULTS" verdict)" = "fail" ] \
  && ok "a wrong answer fails on the numbers" || bad "a wrong answer passed"
[ "$(field "$CASE_RESULTS" exit)" = "0" ] \
  && ok "and it failed on quality, not on the exit code" || bad "the fake did not exit 0"
say

# ── a harness that never returns ────────────────────────────────────────────
say "hang (the cap has to stop it):"
FAKE_MODE=hang PI_BIN="$FAKE/pi-fake.sh" \
  case_run hang --role-pin off --arms pi --scenarios data-tally --cap 5
[ "$(field "$CASE_RESULTS" verdict)" = "timeout" ] \
  && ok "a hung harness is a timeout" || bad "a hung harness was recorded as $(field "$CASE_RESULTS" verdict)"
[ "$(field "$CASE_RESULTS" verdict)" != "pass" ] \
  && ok "and a timeout is not a pass" || bad "a timeout was counted as a pass"
say

# ── unknown cost is not zero ────────────────────────────────────────────────
say "nocost (no usage reported):"
FAKE_MODE=nocost PI_BIN="$FAKE/pi-fake.sh" print_case nocost
[ "$(field "$CASE_RESULTS" cost_usd)" = "null" ] \
  && ok "an unreported cost stays unknown" || bad "an unreported cost became $(field "$CASE_RESULTS" cost_usd)"
[ "$(field "$CASE_RESULTS" cost_source)" = "none" ] \
  && ok "the reason is recorded on the row" || bad "cost_source does not say the cost is absent"
[ "$(field "$CASE_RESULTS" comparable)" = "no" ] \
  && ok "a cell with no cost is excluded from comparison" || bad "a costless cell was left comparable"
grep -q 'unknown' "$CASE_OUT/results.csv" \
  && ok "the CSV says unknown rather than 0" || bad "the CSV does not carry 'unknown'"
say

# ── the open-model law ──────────────────────────────────────────────────────
say "wrongmodel (a model outside the allowlist):"
FAKE_MODE=wrongmodel PI_BIN="$FAKE/pi-fake.sh" print_case wrongmodel
[ "$(field "$CASE_RESULTS" verdict)" = "fail" ] \
  && ok "an off-allowlist model fails the cell" || bad "an off-allowlist model was allowed"
grep -q 'allowlist' "$LOGDIR/wrongmodel.log" \
  && ok "and the log names the offender" || bad "the log does not mention the allowlist"

say "wrongmodel (aforge, an auxiliary role billed elsewhere):"
FAKE_MODE=wrongmodel AFORGE_BIN="$FAKE/aforge-fake.sh" \
  case_run auxmodel --arms aforge --scenarios data-tally --cap 60
[ "$(field "$CASE_RESULTS" verdict)" = "fail" ] \
  && ok "a role call off the allowlist fails the cell" || bad "an auxiliary call escaped the allowlist"

say "ok (aforge, home-shaped receipts):"
FAKE_MODE=ok AFORGE_BIN="$FAKE/aforge-fake.sh" \
  case_run aforgeok --arms aforge --scenarios data-tally --cap 60
[ "$(field "$CASE_RESULTS" verdict)" = "pass" ] \
  && ok "the aforge receipt reader works end to end" || bad "a healthy aforge cell did not pass"
python3 -c "import sys; sys.exit(0 if abs(float('$(field "$CASE_RESULTS" cost_usd)') - 0.000796) < 1e-9 else 1)" \
  && ok "the turn and its auxiliary call are both counted" \
  || bad "the aforge cost is not turn+aux (got $(field "$CASE_RESULTS" cost_usd))"
say

# ── an arm that cannot pin the model ────────────────────────────────────────
say "nopin (the catalog cannot pin the exact id):"
FAKE_MODE=nopin PI_BIN="$FAKE/pi-fake.sh" print_case nopin
[ "$(field "$CASE_RESULTS" verdict)" = "skipped" ] \
  && ok "an unpinnable arm is skipped" || bad "an unpinnable arm was $(field "$CASE_RESULTS" verdict)"
[ "$CASE_EXIT" -eq 0 ] \
  && ok "a skip does not fail the run" || bad "a skip moved the runner's exit code"
grep -q 'skipped 1' "$LOGDIR/nopin.log" \
  && ok "and the summary counts it separately from passes" || bad "the summary hides the skip"
say

# ── a scenario an arm has no door for ───────────────────────────────────────
say "unsupported (a scenario this arm has no door for):"
FAKE_MODE=ok PI_BIN="$FAKE/pi-fake.sh" \
  case_run unsupported --role-pin off --arms pi --scenarios task-result-delivered --cap 30
[ "$(field "$CASE_RESULTS" verdict)" = "unsupported" ] \
  && ok "an undefined scenario is unsupported" || bad "an undefined scenario was $(field "$CASE_RESULTS" verdict)"
grep -q 'unsupported 1' "$LOGDIR/unsupported.log" \
  && ok "and it is counted apart from the passes" || bad "the summary hides the unsupported cell"
say

# ── the interactive door ────────────────────────────────────────────────────
say "interactive (a followup typed while work is running):"
if ! command -v tmux >/dev/null 2>&1; then
  skip "the tmux door tests need tmux(1)"
else
  FAKE_TUI_MODE=ok FAKE_TUI_BUSY=10 CONV_SLOW_SECONDS=4 \
  CONV_POLL=1 CONV_QUIET=3 CONV_READY_WAIT=25 CONV_BUSY_WAIT=25 \
  PI_BIN="$FAKE/tui-fake.sh" \
    case_run door --role-pin off --arms pi --scenarios followup-while-working --cap 90
  [ "$(field "$CASE_RESULTS" door)" = "interactive" ] \
    && ok "the cell is recorded as the interactive door" || bad "the door was not recorded as interactive"
  if grep -q '"outcome":"pass","check":"the followup was answered while the build ran"' "$CASE_RESULTS"; then
    ok "a followup typed during work reached the harness and was answered"
  else
    bad "the followup did not arrive while work was in flight"
  fi
  [ "$(field "$CASE_RESULTS" verdict)" = "pass" ] \
    && ok "and the whole interactive cell passes" || bad "the interactive cell did not pass: $(field "$CASE_RESULTS" verdict)"

  say "neverbusy (nothing was ever in flight to interrupt):"
  FAKE_TUI_MODE=neverbusy CONV_SLOW_SECONDS=4 \
  CONV_POLL=1 CONV_QUIET=3 CONV_READY_WAIT=25 CONV_BUSY_WAIT=8 \
  PI_BIN="$FAKE/tui-fake.sh" \
    case_run neverbusy --role-pin off --arms pi --scenarios followup-while-working --cap 60
  [ "$(field "$CASE_RESULTS" verdict)" != "pass" ] \
    && ok "a scenario that did not happen is not a pass" || bad "a cell with no busy window passed"
  grep -q 'no-busy-window' "$CASE_RESULTS" \
    && ok "and the reason is on the row" || bad "no-busy-window is not recorded"

  say "deaf (the followup is ignored):"
  FAKE_TUI_MODE=deaf FAKE_TUI_BUSY=10 CONV_SLOW_SECONDS=4 \
  CONV_POLL=1 CONV_QUIET=3 CONV_READY_WAIT=25 CONV_BUSY_WAIT=25 \
  PI_BIN="$FAKE/tui-fake.sh" \
    case_run deaf --role-pin off --arms pi --scenarios followup-while-working --cap 90
  [ "$(field "$CASE_RESULTS" verdict)" = "fail" ] \
    && ok "a dropped followup fails the cell" || bad "a dropped followup was $(field "$CASE_RESULTS" verdict)"
fi
say

# ── spending only where the model is pinned all the way down ────────────────
say "role-pin gate (an arm whose auxiliary calls cannot be pinned):"
FAKE_MODE=ok PI_BIN="$FAKE/pi-fake.sh" \
  case_run rolepin --arms pi --scenarios data-tally --cap 60
[ "$(field "$CASE_RESULTS" verdict)" = "skipped" ] \
  && ok "an unpinnable arm is skipped before it is paid for" \
  || bad "an unpinnable arm was $(field "$CASE_RESULTS" verdict)"
if [ -e "$CASE_OUT/marker" ]; then
  bad "the gate skipped the cell but the harness ran anyway"
else
  ok "and no harness process was started"
fi
say

# ── the child environment carries one credential ────────────────────────────
say "environment (only the OpenRouter key reaches a harness):"
FAKE_MODE=ok PI_BIN="$FAKE/pi-fake.sh" \
FAKE_ENV_REPORT="$WORKDIR/envreport.txt" \
OPENROUTER_API_KEY="test-openrouter-not-a-real-key" \
ANTHROPIC_API_KEY="test-anthropic-not-a-real-key" \
OPENAI_API_KEY="test-openai-not-a-real-key" \
  print_case envscrub
if [ -f "$WORKDIR/envreport.txt" ]; then
  grep -q '^openrouter=present' "$WORKDIR/envreport.txt" \
    && ok "the OpenRouter key reaches the harness" || bad "the OpenRouter key did not reach the harness"
  grep -q '^anthropic=$' "$WORKDIR/envreport.txt" \
    && ok "an Anthropic key in this shell does not" || bad "an Anthropic key leaked into the harness"
  grep -q '^openai=$' "$WORKDIR/envreport.txt" \
    && ok "an OpenAI key in this shell does not" || bad "an OpenAI key leaked into the harness"
else
  bad "the fake wrote no environment report"
fi
grep -q 'credentials_present' "$CASE_OUT/evidence"/data-tally-pi/config.txt \
  && ok "the evidence records credential names, not an environment dump" \
  || bad "the cell's config record is missing"
grep -q 'test-openrouter-not-a-real-key' "$CASE_OUT/evidence"/data-tally-pi/config.txt \
  && bad "a key VALUE was written into the evidence" \
  || ok "and no key value is written into the evidence"
say

# ── other people's state is not this suite's to delete ──────────────────────
say "omp profile (an existing profile is used, never removed):"
EXISTING="afconv-selftest-existing-$$"
PROFILE_DIR="$HOME/.omp/profiles/$EXISTING"
if [ -e "$PROFILE_DIR" ]; then
  skip "a profile named $EXISTING already exists — refusing to touch it"
else
  mkdir -p "$PROFILE_DIR/agent"
  printf 'this file belongs to somebody else\n' > "$PROFILE_DIR/agent/sentinel.txt"
  FAKE_MODE=ok OMP_BIN="$FAKE/pi-fake.sh" CONV_OMP_PROFILE="$EXISTING" \
    case_run ompprofile --role-pin off --arms omp --scenarios data-tally --cap 60
  if [ -f "$PROFILE_DIR/agent/sentinel.txt" ]; then
    ok "an existing omp profile survives the run"
  else
    bad "the run DELETED a profile it did not create"
  fi
  grep -q 'reused as-is' "$CASE_OUT/evidence"/data-tally-omp/config.txt 2>/dev/null \
    && ok "and the evidence says it was reused rather than created" \
    || bad "the isolation record does not say the profile was pre-existing"
  rm -rf "$PROFILE_DIR"

  # A profile this run creates is its own to clean up, and the name is checked
  # before it is ever joined into a path.
  FAKE_MODE=ok OMP_BIN="$FAKE/pi-fake.sh" CONV_OMP_PROFILE="../escape" \
    case_run ompescape --role-pin off --arms omp --scenarios data-tally --cap 60
  [ "$(field "$CASE_RESULTS" verdict)" = "skipped" ] \
    && ok "a profile name with a path escape is refused" \
    || bad "a profile name containing .. was accepted"
fi
say

# ── evidence is not overwritten by accident ─────────────────────────────────
say "evidence (a second run does not erase the first):"
SHARED="$WORKDIR/shared-evidence"
FAKE_MODE=ok PI_BIN="$FAKE/pi-fake.sh" CONV_OUT="$SHARED" CONV_CSV="$WORKDIR/shared.csv" \
  CONV_RUN_ID=first "$RUN" --role-pin off --arms pi --scenarios data-tally --cap 60 \
  > "$LOGDIR/evidence-first.log" 2>&1
cp "$SHARED/results.jsonl" "$WORKDIR/first-results.jsonl" 2>/dev/null
FAKE_MODE=ok PI_BIN="$FAKE/pi-fake.sh" CONV_OUT="$SHARED" CONV_CSV="$WORKDIR/shared.csv" \
  CONV_RUN_ID=second "$RUN" --role-pin off --arms pi --scenarios data-tally --cap 60 \
  > "$LOGDIR/evidence-second.log" 2>&1
SECOND_EXIT=$?
[ "$(field "$SHARED/results.jsonl" verdict)" = "skipped" ] \
  && ok "a second run refuses to write over the first's cell" \
  || bad "a second run overwrote existing evidence"
[ -s "$SHARED/data-tally-pi/receipt.json" ] \
  && ok "and the first run's receipt is still there" || bad "the first run's receipt is gone"
[ "$SECOND_EXIT" -eq 0 ] \
  && ok "the refusal is a skip, not a failure" || bad "the refusal moved the exit code"
say

# ── the summary tool ────────────────────────────────────────────────────────
say "summary (per workload, and what it refuses to say):"
SUMMARY="$WORKDIR/summary.txt"
python3 "$CONV_ROOT/lib/pareto.py" "$WORKDIR"/*/evidence/results.jsonl > "$SUMMARY" 2>&1
grep -q 'excluded from every figure' "$SUMMARY" \
  && ok "excluded cells are listed with their reasons" || bad "the summary does not list exclusions"
grep -qE 'not dominated' "$SUMMARY" \
  && ok "the frontier is reported per workload" || bad "no frontier line in the summary"
say

printf 'selftest: %d passed, %d failed, %d skipped\n' "$PASSED" "$FAILED" "$SKIPPED"
if [ "$FAILED" -eq 0 ] && [ "$SKIPPED" -eq 0 ]; then
  printf 'evidence kept in %s\n' "$WORKDIR"
  exit 0
fi
printf 'logs in %s\n' "$LOGDIR"
# Skipped checks make this exit 2 rather than 0: a dependency that was missing
# is a thing somebody has to decide about, not a green run.
[ "$FAILED" -gt 0 ] && exit 1
exit 2
