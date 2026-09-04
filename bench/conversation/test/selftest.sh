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
#                  AND the answer is seen before the work's own finish marker
#   blocking       the counterexample: a harness that queues the followup,
#                  finishes the work, then answers correctly is a FAILURE. Its
#                  final transcript is indistinguishable from a pass, which is
#                  why the cell judges timestamps and not text
#   neverbusy      no window of work to steer is a FAILURE, not a pass
#   deaf           a followup that is ignored is a FAILURE
#   smudged        a near miss is a miss: RRABANNIC is not RABANNIC
#   noready        a screen this rig cannot read is UNSUPPORTED (its own
#                  calibration gap), never a crash blamed on the harness
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
export CONV_PASS_ENV="FAKE_MODE FAKE_MARKER FAKE_ENV_REPORT FAKE_TUI_MODE FAKE_TUI_BUSY FAKE_CATALOG_ID FAKE_ANSWER_ROOT"

# These cases drive fake binaries that reach no network, so they run without the
# forwarding guard — and say so the only way run.sh accepts, which is what keeps
# --unguarded from being usable as a workaround for a real run.
export CONV_FAKE_HARNESS=1

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

# door_witness prints which witness a cell's interactive door used.
door_witness() {
  CONV_DOOR="$1" python3 -c '
import json, os
print(json.load(open(os.environ["CONV_DOOR"])).get("witness") or "")
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
  FAKE_MARKER="$out/marker" FAKE_ANSWER_ROOT="$out/evidence" \
    "$RUN" "$@" > "$LOGDIR/$name.log" 2>&1
  CASE_EXIT=$?
  CASE_OUT="$out"
  CASE_RESULTS="$out/evidence/results.jsonl"
  return 0
}

# Fake binaries reach no network, so these cases run without the guard.
print_case() {
  local name="$1"; shift
  case_run "$name" --arms pi --scenarios data-tally --cap 60 --unguarded "$@"
}

say "bench/conversation selftest — fake binaries, no model, no spend"
say "workdir: $WORKDIR"
say

# ── the dry run must not run anything ───────────────────────────────────────
say "dry-run:"
PI_BIN="$FAKE/pi-fake.sh" AFORGE_BIN="$FAKE/aforge-fake.sh" \
  case_run dryrun --unguarded --arms pi,aforge --scenarios data-tally --dry-run
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

# ── the answer key is not in the model's workspace ──────────────────────────
say "judge (the workspace holds the question, not the answer):"
[ -f "$CASE_OUT/evidence/data-tally-pi/work/ledger.csv" ] \
  && ok "the fixture the model reads is in the workspace" || bad "the fixture is missing"
[ ! -e "$CASE_OUT/evidence/data-tally-pi/work/expected.json" ] \
  && ok "and the answer key is not" || bad "the answer key is sitting in the model's workspace"
[ -s "$CASE_OUT/evidence/data-tally-pi/judge/expected.json" ] \
  && ok "it is in the judge's directory beside the cell" || bad "the judge has no answer key"
say

# ── a price table of zeroes is not a free call ──────────────────────────────
say "zeroprice (positive usage, zero self-reported price):"
FAKE_MODE=zeroprice PI_BIN="$FAKE/pi-fake.sh" print_case zeroprice
[ "$(field "$CASE_RESULTS" cost_usd)" = "null" ] \
  && ok "a zero price against real tokens is not recorded as \$0" \
  || bad "a zero-priced paid call was recorded as $(field "$CASE_RESULTS" cost_usd)"
[ "$(field "$CASE_RESULTS" comparable)" = "no" ] \
  && ok "and the cell is not comparable" || bad "a fabricated-zero cell stayed comparable"
grep -q 'zero price table' "$CASE_RESULTS" \
  && ok "and the row says why" || bad "the row does not explain the zero"
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
  case_run hang --unguarded --arms pi --scenarios data-tally --cap 5
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
  case_run auxmodel --unguarded --arms aforge --scenarios data-tally --cap 60
[ "$(field "$CASE_RESULTS" verdict)" = "fail" ] \
  && ok "a role call off the allowlist fails the cell" || bad "an auxiliary call escaped the allowlist"

say "ok (aforge, home-shaped receipts):"
FAKE_MODE=ok AFORGE_BIN="$FAKE/aforge-fake.sh" \
  case_run aforgeok --unguarded --arms aforge --scenarios data-tally --cap 60
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
  case_run unsupported --unguarded --arms pi --scenarios work-result-recalled --cap 30
[ "$(field "$CASE_RESULTS" verdict)" = "unsupported" ] \
  && ok "an undefined scenario is unsupported" || bad "an undefined scenario was $(field "$CASE_RESULTS" verdict)"
grep -q 'unsupported 1' "$LOGDIR/unsupported.log" \
  && ok "and it is counted apart from the passes" || bad "the summary hides the unsupported cell"
say

# ── the interactive door ────────────────────────────────────────────────────
say "interactive (a followup ANSWERED WHILE the work ran):"
if ! command -v tmux >/dev/null 2>&1; then
  skip "the tmux door tests need tmux(1)"
else
  # One knob for every interactive case, so the only difference between a pass
  # and the counterexample below is what the harness does with what is typed.
  # The busy window outlasts the build on purpose: the blocking mode has to be
  # still holding the turn when the work ends, or it would not be a
  # counterexample at all.
  tui_case() {
    local name="$1" mode="$2"
    FAKE_TUI_MODE="$mode" FAKE_TUI_BUSY=14 CONV_SLOW_SECONDS=5 \
    CONV_POLL=1 CONV_QUIET=3 CONV_READY_WAIT=25 CONV_BUSY_WAIT=25 \
    PI_BIN="$FAKE/tui-fake.sh" \
      case_run "$name" --unguarded --arms pi --scenarios followup-while-working --cap 120
    CASE_DOOR="$CASE_OUT/evidence/followup-while-working-pi/door.json"
  }

  # ordered_stamps exits 0 when both fields are present in door.json and the
  # first is strictly earlier. The selftest checks the door's own numbers, not
  # only the verdict derived from them.
  ordered_stamps() {
    CONV_DOOR="$1" CONV_A="$2" CONV_B="$3" python3 -c '
import json, os, sys
got = json.load(open(os.environ["CONV_DOOR"]))
a, b = got.get(os.environ["CONV_A"]), got.get(os.environ["CONV_B"])
sys.exit(0 if a and b and float(a) < float(b) else 1)
'
  }

  tui_case door ok
  [ "$(field "$CASE_RESULTS" door)" = "interactive" ] \
    && ok "the cell is recorded as the interactive door" || bad "the door was not recorded as interactive"
  grep -q '"outcome":"pass","check":"and answered BEFORE the build finished"' "$CASE_RESULTS" \
    && ok "a harness that answers during the build satisfies the ordering check" \
    || bad "the ordering check did not pass for a responsive harness"
  [ "$(field "$CASE_RESULTS" verdict)" = "pass" ] \
    && ok "and the whole interactive cell passes" || bad "the interactive cell did not pass: $(field "$CASE_RESULTS" verdict)"
  ordered_stamps "$CASE_DOOR" work_started_at midwork_sent_at \
    && ok "the door recorded the followup going in after the work began" \
    || bad "door.json does not show the followup landing inside the work"
  ordered_stamps "$CASE_DOOR" answer_first_seen_at work_finished_at \
    && ok "and the answer appearing before the work ended" \
    || bad "door.json does not show the answer preceding the build's finish marker"
  [ "$(door_witness "$CASE_DOOR")" = "work-markers" ] \
    && ok "the witness is the fixture's own phase markers, not a spinner" \
    || bad "the cell used a weaker witness than it claims: $(door_witness "$CASE_DOOR")"

  # THE COUNTEREXAMPLE. This run is what used to pass: the harness reads
  # nothing until the build is over, then answers correctly, with the right
  # derived token and the right build marker, in a final transcript that looks
  # perfect. Steering that arrives after the work is not steering.
  say "blocking (the right answer, given only after the build finished):"
  tui_case blocking blocking
  grep -q 'RABANNIC' "$CASE_OUT/evidence/followup-while-working-pi/scrollback.txt" \
    && ok "the transcript does end with the correct answer" \
    || bad "the counterexample never produced the answer at all"
  grep -q '"outcome":"fail","check":"and answered BEFORE the build finished"' "$CASE_RESULTS" \
    && ok "and the cell FAILS it anyway, on the ordering" \
    || bad "the false green is back: a blocking harness was not caught by the ordering check"
  [ "$(field "$CASE_RESULTS" verdict)" = "fail" ] \
    && ok "a blocking harness fails the mid-work cell" \
    || bad "a blocking harness was recorded $(field "$CASE_RESULTS" verdict)"

  say "neverbusy (no window of work ever opened):"
  FAKE_TUI_MODE=neverbusy CONV_SLOW_SECONDS=4 \
  CONV_POLL=1 CONV_QUIET=3 CONV_READY_WAIT=25 CONV_BUSY_WAIT=8 \
  PI_BIN="$FAKE/tui-fake.sh" \
    case_run neverbusy --unguarded --arms pi --scenarios followup-while-working --cap 60
  [ "$(field "$CASE_RESULTS" verdict)" != "pass" ] \
    && ok "a scenario that did not happen is not a pass" || bad "a cell with no mid-work window passed"
  grep -q 'no-midwork-window' "$CASE_RESULTS" \
    && ok "and the reason is on the row" || bad "no-midwork-window is not recorded"

  # A near miss is a miss. A live omp pane rendered the answer with a duplicated
  # first letter and a substring match took it as correct.
  say "smudged (the right answer with an extra letter):"
  tui_case smudged smudged
  grep -q 'RRABANNIC' "$CASE_OUT/evidence/followup-while-working-pi/scrollback.txt" \
    && ok "the near-miss word is what reached the screen" \
    || bad "the counterexample did not produce the smudged word"
  [ "$(field "$CASE_RESULTS" verdict)" = "fail" ] \
    && ok "RRABANNIC does not pass as RABANNIC" \
    || bad "a near-miss answer was accepted: $(field "$CASE_RESULTS" verdict)"

  # A screen this rig cannot read is this rig's gap, not a dead product.
  say "noready (a composer the driver was never calibrated against):"
  FAKE_TUI_MODE=noready CONV_SLOW_SECONDS=4 \
  CONV_POLL=1 CONV_QUIET=3 CONV_READY_WAIT=6 CONV_BUSY_WAIT=6 \
  PI_BIN="$FAKE/tui-fake.sh" \
    case_run noready --unguarded --arms pi --scenarios followup-while-working --cap 60
  [ "$(field "$CASE_RESULTS" verdict)" = "unsupported" ] \
    && ok "an unrecognised composer is unsupported, not a crash" \
    || bad "an unrecognised composer was recorded $(field "$CASE_RESULTS" verdict)"
  grep -q 'noready' "$CASE_RESULTS" \
    && ok "and the row says the screen was drawn but not matched" \
    || bad "the row does not distinguish noready from noframe"
  grep -q 'the session died\|composer never drew' "$CASE_RESULTS" \
    && bad "the row still blames the harness for a calibration gap" \
    || ok "nothing on the row blames the harness"
  grep -q 'the answer is the derived token' "$CASE_RESULTS" \
    && bad "the scenario's checks ran against a cell that never started" \
    || ok "and the scenario's own checks did not run"
  say

  say "deaf (the followup is ignored):"
  tui_case deaf deaf
  [ "$(field "$CASE_RESULTS" verdict)" = "fail" ] \
    && ok "a dropped followup fails the cell" || bad "a dropped followup was $(field "$CASE_RESULTS" verdict)"
fi
say

# ── spending only where the model is pinned all the way down ────────────────
say "role pin (recorded as configuration, not trusted as enforcement):"
FAKE_MODE=ok PI_BIN="$FAKE/pi-fake.sh" print_case rolepin
grep -q 'role_pin=unverified' "$CASE_RESULTS" \
  && ok "an arm whose auxiliary roles cannot be pinned says so on its row" \
  || bad "the row does not record the role-pin state"
[ "$(field "$CASE_RESULTS" verdict)" = "pass" ] \
  && ok "and that alone does not fail the cell — the guard is the enforcement" \
  || bad "role-pin state changed the verdict: $(field "$CASE_RESULTS" verdict)"
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
    case_run ompprofile --unguarded --arms omp --scenarios data-tally --cap 60
  if [ -f "$PROFILE_DIR/agent/sentinel.txt" ]; then
    ok "an existing omp profile survives the run"
  else
    bad "the run DELETED a profile it did not create"
  fi
  [ "$(field "$CASE_RESULTS" verdict)" = "skipped" ] \
    && ok "and the cell is skipped rather than run inside settings it did not write" \
    || bad "the run used a profile it did not create: $(field "$CASE_RESULTS" verdict)"
  rm -rf "$PROFILE_DIR"

  # A profile this run creates is its own to clean up, and the name is checked
  # before it is ever joined into a path.
  FAKE_MODE=ok OMP_BIN="$FAKE/pi-fake.sh" CONV_OMP_PROFILE="../escape" \
    case_run ompescape --unguarded --arms omp --scenarios data-tally --cap 60
  [ "$(field "$CASE_RESULTS" verdict)" = "skipped" ] \
    && ok "a profile name with a path escape is refused" \
    || bad "a profile name containing .. was accepted"
fi
say

# ── evidence is not overwritten by accident ─────────────────────────────────
say "evidence (a second run erases nothing the first left):"
SHARED="$WORKDIR/shared-evidence"
FAKE_MODE=ok PI_BIN="$FAKE/pi-fake.sh" CONV_OUT="$SHARED" CONV_CSV="$WORKDIR/shared.csv" \
  FAKE_ANSWER_ROOT="$SHARED" CONV_RUN_ID=first "$RUN" --unguarded --arms pi --scenarios data-tally --cap 60 \
  > "$LOGDIR/evidence-first.log" 2>&1
FIRST_ROWS="$(grep -c "" "$SHARED/results.jsonl" 2>/dev/null || echo 0)"
[ "$FIRST_ROWS" -ge 1 ] \
  && ok "the first run wrote a summary" || bad "the first run wrote no summary"
# A sentinel in both places the second run could clobber: the authoritative
# summary, and an artifact inside the cell.
echo '{"sentinel":"first-run-row-that-must-survive"}' >> "$SHARED/results.jsonl"
echo 'first-run artifact' > "$SHARED/data-tally-pi/sentinel.txt"

FAKE_MODE=ok PI_BIN="$FAKE/pi-fake.sh" CONV_OUT="$SHARED" CONV_CSV="$WORKDIR/shared.csv" \
  FAKE_ANSWER_ROOT="$SHARED" CONV_RUN_ID=second "$RUN" --unguarded --arms pi --scenarios data-tally --cap 60 \
  > "$LOGDIR/evidence-second.log" 2>&1
SECOND_EXIT=$?
[ "$SECOND_EXIT" -ne 0 ] \
  && ok "a second run into the same output refuses" \
  || bad "a second run wrote into an existing run's output (exit $SECOND_EXIT)"
grep -q 'first-run-row-that-must-survive' "$SHARED/results.jsonl" \
  && ok "and the first run's summary rows are still there" \
  || bad "the second run TRUNCATED the first run's summary"
[ -s "$SHARED/data-tally-pi/sentinel.txt" ] \
  && ok "and the first run's cell artifacts are still there" \
  || bad "the second run destroyed a cell artifact"
[ -s "$SHARED/data-tally-pi/receipt.json" ] \
  && ok "including its receipt" || bad "the first run's receipt is gone"
grep -q 'refusing to write over an existing run summary' "$LOGDIR/evidence-second.log" \
  && ok "and it says which file stopped it" || bad "the refusal does not name the summary"

# --overwrite is the deliberate way, and it is the only way.
FAKE_MODE=ok PI_BIN="$FAKE/pi-fake.sh" CONV_OUT="$SHARED" CONV_CSV="$WORKDIR/shared.csv" \
  FAKE_ANSWER_ROOT="$SHARED" CONV_RUN_ID=third "$RUN" --unguarded --overwrite --arms pi --scenarios data-tally --cap 60 \
  > "$LOGDIR/evidence-third.log" 2>&1
[ $? -eq 0 ] && ok "--overwrite replaces a run deliberately" || bad "--overwrite did not run"
say

# ── the guard: prevention, not detection ────────────────────────────────────
say "guard (a disallowed model must never reach an upstream):"
GUARD_DIR="$WORKDIR/guard"
mkdir -p "$GUARD_DIR"
HITS="$GUARD_DIR/upstream-hits.jsonl"
UP_OUT="$GUARD_DIR/upstream.out"
python3 "$FAKE/upstream.py" --hits "$HITS" > "$UP_OUT" 2>&1 &
UP_PID=$!
for _ in $(seq 1 50); do
  UP_PORT="$(awk '/^PORT /{print $2; exit}' "$UP_OUT" 2>/dev/null)"
  [ -n "${UP_PORT:-}" ] && break
  sleep 0.1
done

G_OUT="$GUARD_DIR/guard.out"
GUARD_TEST=1 GUARD_UPSTREAM_KEY="test-upstream-key-not-real" \
  python3 "$CONV_ROOT/lib/guard.py" --allow deepseek/deepseek-v4-flash-0731 \
    --audit "$GUARD_DIR/audit.jsonl" --usage "$GUARD_DIR/usage.jsonl" \
    --scope selftest --sentinel "test-sentinel" \
    --upstream "http://127.0.0.1:${UP_PORT:-0}/v1" > "$G_OUT" 2>&1 &
G_PID=$!
for _ in $(seq 1 50); do
  G_PORT="$(awk '/^PORT /{print $2; exit}' "$G_OUT" 2>/dev/null)"
  [ -n "${G_PORT:-}" ] && break
  sleep 0.1
done

if [ -z "${G_PORT:-}" ] || [ -z "${UP_PORT:-}" ]; then
  bad "the guard or its stand-in upstream did not come up"
else
  ask() {
    curl -s -o "$2" -w '%{http_code}' -X POST "http://127.0.0.1:$G_PORT/v1/chat/completions" \
      -H "Authorization: Bearer ${3:-test-sentinel}" -H 'Content-Type: application/json' \
      -d "{\"model\":\"$1\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}],\"stream\":true}"
  }

  CODE="$(ask "anthropic/claude-sonnet-4" "$GUARD_DIR/denied.body")"
  [ "$CODE" = "403" ] \
    && ok "a commercial model is refused (403)" || bad "a commercial model got $CODE"
  [ ! -s "$HITS" ] \
    && ok "and the upstream was never contacted" \
    || bad "the refused request REACHED the upstream: $(cat "$HITS")"

  CODE="$(ask "deepseek/deepseek-v4-flash-0731" "$GUARD_DIR/allowed.body")"
  [ "$CODE" = "200" ] \
    && ok "the allowlisted model is forwarded (200)" || bad "the allowed model got $CODE"
  grep -q '"model": "deepseek/deepseek-v4-flash-0731"' "$HITS" \
    && ok "and the upstream saw exactly that model" || bad "the upstream saw something else"
  grep -q 'UPSTREAM-CHUNK-1' "$GUARD_DIR/allowed.body" && grep -q 'UPSTREAM-CHUNK-2' "$GUARD_DIR/allowed.body" \
    && ok "streamed chunks are relayed through untouched" || bad "the stream did not come through"

  CODE="$(ask "deepseek/deepseek-v4-flash-0731" "$GUARD_DIR/nosentinel.body" "wrong-token")"
  [ "$CODE" = "401" ] \
    && ok "a caller without this run's sentinel is refused" || bad "a wrong sentinel got $CODE"

  HITS_BEFORE="$(wc -l < "$HITS")"
  CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:$G_PORT/v1/chat/completions" \
    -H 'Authorization: Bearer test-sentinel' -H 'Content-Type: application/json' -d 'not json')"
  [ "$CODE" = "400" ] \
    && ok "a body whose model cannot be read is refused" || bad "an uncheckable body got $CODE"
  [ "$(wc -l < "$HITS")" -eq "$HITS_BEFORE" ] \
    && ok "and it too reached no upstream" || bad "an uncheckable body was forwarded"

  # A fallback array is a second way to name a model, and a request that lists
  # a commercial id there would have been billed for it the moment the first
  # choice was unavailable. Every id in the request is checked, not the first.
  post() {
    curl -s -o "$2" -w '%{http_code}' -X POST "http://127.0.0.1:$G_PORT/v1/chat/completions" \
      -H 'Authorization: Bearer test-sentinel' -H 'Content-Type: application/json' -d "$1"
  }
  HITS_BEFORE="$(wc -l < "$HITS")"
  CODE="$(post '{"model":"deepseek/deepseek-v4-flash-0731","models":["deepseek/deepseek-v4-flash-0731","openai/gpt-4o"],"messages":[{"role":"user","content":"hi"}]}' "$GUARD_DIR/fallback.body")"
  [ "$CODE" = "403" ] \
    && ok "an off-allowlist id in the fallback array is refused (403)" \
    || bad "a commercial fallback got $CODE"
  [ "$(wc -l < "$HITS")" -eq "$HITS_BEFORE" ] \
    && ok "and that request reached no upstream either" \
    || bad "a request with a commercial fallback was forwarded"
  CODE="$(post '{"model":"deepseek/deepseek-v4-flash-0731","models":["deepseek/deepseek-v4-flash-0731"],"messages":[{"role":"user","content":"hi"}]}' "$GUARD_DIR/fallback-ok.body")"
  [ "$CODE" = "200" ] \
    && ok "a fallback array of allowlisted ids still goes through" \
    || bad "an allowlisted fallback array got $CODE"

  # Relay, not buffer. The upstream holds the second event back for two
  # seconds; a guard that read the response to completion before answering
  # would deliver both at once, and a TUI behind it would sit blank for the
  # whole generation.
  GUARD_PORT="$G_PORT" python3 - "$GUARD_DIR/stream-timing.json" <<'PYCLIENT'
import http.client, json, os, sys, time
body = json.dumps({
    "model": "deepseek/deepseek-v4-flash-0731",
    "messages": [{"role": "user", "content": "hi"}],
    "stream": True,
    "stream_delay": 2,
})
conn = http.client.HTTPConnection("127.0.0.1", int(os.environ["GUARD_PORT"]), timeout=30)
started = time.time()
conn.request("POST", "/v1/chat/completions", body=body,
             headers={"Authorization": "Bearer test-sentinel",
                      "Content-Type": "application/json"})
response = conn.getresponse()
seen = {}
buffered = b""
while True:
    piece = response.read1(4096)
    if not piece:
        break
    buffered += piece
    for n in (1, 2):
        marker = b"UPSTREAM-CHUNK-%d" % n
        if n not in seen and marker in buffered:
            seen[n] = time.time() - started
# Read to the end rather than walking away: an unfinished read is a client
# disconnect, and the ledger test below wants this call to have settled.
json.dump({"first": seen.get(1), "second": seen.get(2)}, open(sys.argv[1], "w"))
PYCLIENT
  FIRST="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["first"] or -1)' "$GUARD_DIR/stream-timing.json")"
  SECOND="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["second"] or -1)' "$GUARD_DIR/stream-timing.json")"
  awk -v a="$FIRST" 'BEGIN{exit !(a >= 0 && a < 1.5)}' \
    && ok "the first event reaches the client before the second is even sent (${FIRST}s)" \
    || bad "the first event was held until the stream ended (${FIRST}s) — the guard buffers"
  awk -v a="$FIRST" -v b="$SECOND" 'BEGIN{exit !(b >= 1.5 && b > a)}' \
    && ok "and the second arrives when the upstream sends it (${SECOND}s)" \
    || bad "the second event did not arrive after its upstream delay (${SECOND}s)"

  # Accounting. A call that is admitted has been paid for whatever happens
  # next, so the ledger must close every admission — and where it cannot, the
  # cell's cost has to go unknown rather than be totalled from the calls that
  # happened to finish.
  cost_of() {
    python3 "$CONV_ROOT/lib/receipts.py" --kind pi-events --path /dev/null \
      --guard-usage "$1" --out "$GUARD_DIR/receipt.json" >/dev/null 2>&1
    python3 -c '
import json, sys
got = json.load(open(sys.argv[1]))
print(got["cost_source"], "null" if got["cost_usd"] is None else got["cost_usd"])
' "$GUARD_DIR/receipt.json"
  }
  read -r ACC_SOURCE ACC_COST <<< "$(cost_of "$GUARD_DIR/usage.jsonl")"
  [ "$ACC_SOURCE" = "guard-upstream" ] \
    && ok "the calls so far are priced from the provider's own usage ($ACC_COST)" \
    || bad "the guard did not price completed calls: $ACC_SOURCE"
  grep -q '"phase": "admitted"' "$GUARD_DIR/usage.jsonl" \
    && ok "and each was booked before it was forwarded, not only after" \
    || bad "no admission row was written before forwarding"

  # The client hangs up mid-stream. The call was made, the provider may bill
  # it, and its usage block never arrives — the guard must record that quietly
  # and not answer a request whose headers it has already sent.
  GUARD_PORT="$G_PORT" python3 - <<'PYDROP'
import http.client, json, os
body = json.dumps({"model": "deepseek/deepseek-v4-flash-0731",
                   "messages": [{"role": "user", "content": "hi"}],
                   "stream": True, "stream_delay": 3})
conn = http.client.HTTPConnection("127.0.0.1", int(os.environ["GUARD_PORT"]), timeout=30)
conn.request("POST", "/v1/chat/completions", body=body,
             headers={"Authorization": "Bearer test-sentinel",
                      "Content-Type": "application/json"})
response = conn.getresponse()
response.read1(64)      # take the first event, then walk away
conn.close()
PYDROP
  for _ in $(seq 1 60); do
    grep -q 'client disconnected mid-stream' "$GUARD_DIR/usage.jsonl" && break
    sleep 0.2
  done
  python3 -c '
import json, sys
rows = [json.loads(line) for line in open(sys.argv[1]) if line.strip()]
last = [row for row in rows if row.get("phase") == "settled"][-1]
sys.exit(0 if last["cost_usd"] is None and "disconnect" in (last.get("note") or "") else 1)
' "$GUARD_DIR/usage.jsonl" \
    && ok "a dropped call is settled as unknown, not left out of the ledger" \
    || bad "the dropped call has no unpriced settlement"
  read -r ACC_SOURCE ACC_COST <<< "$(cost_of "$GUARD_DIR/usage.jsonl")"
  [ "$ACC_COST" = "null" ] \
    && ok "and one priced call beside it is NOT reported as the total" \
    || bad "a partial total was reported as known: $ACC_COST"
  kill -0 "$G_PID" 2>/dev/null \
    && ok "the guard survived the disconnect" || bad "the guard died on a client disconnect"

  # And the abrupt case: the guard is killed with a call in flight, so an
  # admission exists that nothing will ever close.
  grep '"phase": "admitted"' "$GUARD_DIR/usage.jsonl" | head -1 > "$GUARD_DIR/orphan.jsonl"
  read -r ACC_SOURCE ACC_COST <<< "$(cost_of "$GUARD_DIR/orphan.jsonl")"
  [ "$ACC_COST" = "null" ] \
    && ok "an admission nothing settled reads as unknown, not as nothing" \
    || bad "an unsettled admission was priced: $ACC_COST"
  grep -q 'never settled' "$GUARD_DIR/receipt.json" \
    && ok "and the receipt says which calls went unaccounted" \
    || bad "the receipt does not name the unsettled calls"

  grep -q '"decision": "deny"' "$GUARD_DIR/audit.jsonl" \
    && ok "the audit log records the refusals" || bad "the audit log has no denial"
  if grep -qE 'test-upstream-key-not-real|test-sentinel|Authorization' "$GUARD_DIR/audit.jsonl"; then
    bad "the audit log contains a credential"
  else
    ok "and it contains no credential"
  fi
fi
kill "$G_PID" "$UP_PID" 2>/dev/null; wait "$G_PID" "$UP_PID" 2>/dev/null
say

say "guard (a fixed upstream, and a sentinel where the key would be):"
GUARD_TEST=0 python3 "$CONV_ROOT/lib/guard.py" --allow deepseek/deepseek-v4-flash-0731 \
  --upstream "http://127.0.0.1:1/v1" > "$WORKDIR/fixedupstream.log" 2>&1
[ $? -ne 0 ] \
  && ok "the upstream cannot be moved outside the test flag" \
  || bad "the guard accepted an arbitrary upstream"
grep -q '127.0.0.1' "$CONV_ROOT/lib/guard.py" \
  && ok "the guard binds loopback in source" || bad "the guard does not bind loopback"

# The wiring is checked directly, because a guarded run needs a live key and
# this suite stays offline: what matters is which key reaches a harness.
WIRE="$(CONV_ROOT="$CONV_ROOT" CONV_LIB="$CONV_ROOT/lib" \
  REAL_KEY="real-key-must-not-leak" bash -c '
    set -u
    OPENROUTER_API_KEY="$REAL_KEY"
    source "$CONV_LIB/common.sh"; source "$CONV_LIB/allowlist.sh"
    source "$CONV_LIB/verdict.sh"; source "$CONV_LIB/adapters.sh"
    source "$CONV_LIB/guarded.sh"
    GUARD_URL="http://127.0.0.1:9/v1"; GUARD_SENTINEL="sentinel-token"
    ARM_STATE_DIR="$(mktemp -d)"; ARM_ENV=()
    arm_guard_wire pi "$ARM_STATE_DIR" >/dev/null 2>&1
    child_env "${ARM_ENV[@]}"
    printf "\n---models.json---\n"
    cat "$ARM_STATE_DIR/pi-home/models.json"
  ')"
printf '%s' "$WIRE" | grep -q 'OPENROUTER_API_KEY=sentinel-token' \
  && ok "a wired harness is handed the sentinel" || bad "the harness was not handed the sentinel"
printf '%s' "$WIRE" | grep -q 'real-key-must-not-leak' \
  && bad "the real key reached the harness environment" \
  || ok "and never the real key — only the guard has that"
printf '%s' "$WIRE" | grep -q '"baseUrl": "http://127.0.0.1:9/v1"' \
  && ok "and its provider points at the guard on loopback" \
  || bad "the provider config does not point at the guard"
say

say "guard gate (unguarded is not a workaround):"
CONV_FAKE_HARNESS=0 CONV_OUT="$WORKDIR/gate/evidence" CONV_CSV="$WORKDIR/gate.csv" \
  PI_BIN="$FAKE/pi-fake.sh" "$RUN" --unguarded --arms pi --scenarios data-tally \
  > "$LOGDIR/gate.log" 2>&1
[ $? -ne 0 ] \
  && ok "--unguarded refuses to run without a declared fake harness" \
  || bad "--unguarded ran with real binaries allowed"
grep -q 'CONV_FAKE_HARNESS=1' "$LOGDIR/gate.log" \
  && ok "and says what it would need" || bad "the refusal does not say why"
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
