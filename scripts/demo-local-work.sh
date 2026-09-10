#!/usr/bin/env bash
# demo-local-work.sh — ongoing work over LOCAL FILES, end to end, with a real
# model, in a disposable aforge home. No connectors, no accounts, nothing sent
# anywhere but the model provider.
#
# What it shows, each step through the shipped binary's own doors:
#   folders + a folder rule -> ongoing work placed in that folder ->
#   a file change starts a run -> the report is checked against the folder's
#   rule and published -> an edit reaches the next run -> pause holds work
#   back -> resume catches up -> a product spec change starts a marketing
#   review (second work item) -> stop is final -> an idle pass runs nothing ->
#   `aforge standing show` prints cause and effect for every run.
#
# IT IS STRICT. Every step asserts what it expects — the check's exit status,
# the run count, the occurrence's outcome, version and change list, the
# publication receipt, and that the known fixture's contact details are not in
# the published report — and the first thing that does not hold stops the
# demo with "DEMO FAILED", the item's own account of why, and exit status 1.
# A report that was held back or a run that was cut off is a failed demo, not
# a warning. The last good report is left where it was either way.
#
# Usage:
#   make build
#   OPENROUTER_API_KEY=... scripts/demo-local-work.sh              # fresh temp home
#   DEMO_DIR=/tmp/aforge-local-work scripts/demo-local-work.sh      # named place
#   DEMO_MODEL=deepseek/deepseek-v4-flash PER_RUN_USD=0.25 ...      # the defaults
#   SETUP_ONLY=1 scripts/demo-local-work.sh                         # seed, then drive it yourself
#
# It never touches ~/.aforge, never installs the background timer, and never
# changes HOME. The deterministic, key-free version of the same journey is
#   make test-local-work
set -uo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
bin="$root/bin/aforge"
if [[ ! -x "$bin" ]]; then
  echo "bin/aforge is missing: run make build first." >&2
  exit 2
fi
if [[ -z "${OPENROUTER_API_KEY:-}" && -z "${SETUP_ONLY:-}" ]]; then
  echo "OPENROUTER_API_KEY is required for the live run (SETUP_ONLY=1 seeds without a model)." >&2
  echo "Key-free, scripted-model version: make test-local-work" >&2
  exit 2
fi

dir="${DEMO_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/aforge-local-work.XXXXXX")}"
mkdir -p "$dir"
export AFORGE_HOME="$dir/home"
project="$dir/project"
model="${DEMO_MODEL:-deepseek/deepseek-v4-flash}"
per_run="${PER_RUN_USD:-0.25}"
export AFORGE_MODEL="$model"
# The whole day's standing spend in this home, which the pass enforces.
export AFORGE_DAILY_BUDGET="${DAILY_USD:-2}"
unset AFORGE_PROFILE_DIR OPENAI_API_KEY || true

rm -rf "$AFORGE_HOME" "$project"
mkdir -p "$AFORGE_HOME" "$project/inbox" "$project/product" "$project/marketing"
# Memory off and background checks off: this home is for watching one journey,
# and the machine's timer belongs to the person's real install.
printf '{"memory.enabled":"off","standing.background":"off"}\n' > "$AFORGE_HOME/config.json"
cat > "$project/product/spec.md" <<'EOF'
# Product spec
- Offline support: supported on desktop.
- Export: CSV and PDF.
EOF
cat > "$project/marketing/launch-copy.md" <<'EOF'
# Launch copy
Works offline, anywhere. Export to CSV, PDF and Excel in one click.
EOF

say() { printf '\n\033[1m== %s\033[0m\n' "$*"; }
run() { printf '$ aforge %s\n' "$*"; "$bin" "$@"; }

inbox="" review=""
fail() {
  printf '\n\033[1;31mDEMO FAILED: %s\033[0m\n' "$*"
  for id in $inbox $review; do
    echo "--- aforge standing show $id"
    "$bin" standing show "$id" --runs 3 || true
  done
  echo
  echo "The last good reports were left as they were. Home: $AFORGE_HOME"
  echo "Look further: export AFORGE_HOME=$AFORGE_HOME; $bin standing show <id>"
  exit 1
}

# check runs one pass and fails the demo on any exit status but 0: the pass
# itself says which run did not finish (aforge standing check exits 2 for a run
# that did not finish and 4 for one waiting on the person).
check() {
  printf '$ aforge standing check\n'
  local out rc
  out="$("$bin" standing check 2>&1)"; rc=$?
  printf '%s\n' "$out"
  [[ $rc -eq 0 ]] || fail "$1: aforge standing check exited $rc"
}

# expect <step> <id> <runs> [<spec> <change>] asserts the item's run count and,
# with a version, that the newest run finished, landed, ran on that version,
# was woken by that change and published a report whose bytes match the receipt.
expect_py="$(cat <<'EOF'
import hashlib, json, os, sys
step, want, spec, change, project = sys.argv[1:6]
record = json.load(sys.stdin)
runs = record.get("occurrences") or []
def no(why):
    print(f"{step}: {why}")
    sys.exit(1)
if len(runs) != int(want):
    no(f"{len(runs)} run(s), expected {want}")
if not spec:
    print(f"ok: {len(runs)} run(s)")
    sys.exit(0)
o = runs[0]
changes = [c["kind"] + " " + c["path"] for c in o.get("changes") or []]
if o.get("phase") != "finished" or o.get("outcome") != "landed":
    no(f"the newest run is {o.get('phase')} · {o.get('outcome')}: {o.get('outcomeText', '')}")
if str(o.get("spec")) != spec:
    no(f"the newest run ran on instructions v{o.get('spec')}, expected v{spec}")
if change not in changes:
    no(f"the newest run was woken by {changes}, expected {change!r}")
published = o.get("published")
if not published:
    no("the newest run published no report")
body = open(os.path.join(project, published["path"]), "rb").read()
if hashlib.sha256(body).hexdigest() != published["sha256"]:
    no(f"{published['path']} on disk does not match the run's receipt")
checked = o.get("ruleCheck")
line = f"ok: run {os.path.basename(o['runDir'])} landed on v{spec}, woken by {change}, published {published['path']}"
if checked:
    line += f"; checked against {len(checked['rules'])} rule(s): {checked['verdict']}"
    if checked.get("rewrote"):
        line += " after one correction (" + checked.get("first", "") + ")"
print(line)
EOF
)"
expect() {
  local step="$1" id="$2" want="$3" spec="${4:-}" change="${5:-}"
  "$bin" standing show "$id" --json --runs 50 | python3 -c "$expect_py" "$step" "$want" "$spec" "$change" "$project" || fail "$step"
}

# no_contact fails the demo if the fixture's own contact details are in a
# published report. It is keyed to THIS fixture on purpose: it is the demo's
# assertion about its own inputs, not aforge's check, which reads the rule's
# words with a model and has no pattern in it.
no_contact() {
  if grep -nE '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}|555[ -]?0100' "$1"; then
    fail "$1 still carries a raw contact detail the Launch rule forbids"
  fi
  echo "ok: no raw contact detail in $(basename "$1")"
}

sha() { sha256sum "$1" | cut -d' ' -f1; }

say "LIVE MODEL DEMO — local files only, model $model, at most \$$per_run a run, \$$AFORGE_DAILY_BUDGET a day"
echo "aforge:  $("$bin" version 2>&1 | head -1)"
echo "home:    $AFORGE_HOME"
echo "project: $project"

say "Folders, and a rule placed on each"
launch="$("$bin" collections create Launch --json | sed -E 's/.*"id":"([^"]+)".*/\1/')"
marketing="$("$bin" collections create Marketing --json | sed -E 's/.*"id":"([^"]+)".*/\1/')"
echo "Launch=$launch Marketing=$marketing"
run standing add --hold --scope "$launch" --workspace "$project" \
  --words "Inbox reports for Launch never quote email addresses or phone numbers; write [redacted]." || fail "adding the Launch rule"
run standing add --hold --scope "$marketing" --workspace "$project" \
  --words "Marketing review notes cite the spec line behind every finding and never edit the copy itself." || fail "adding the Marketing rule"

say "Ongoing work: keep an inbox report current (placed in Launch)"
inbox="$("$bin" standing add --json --workspace "$project" --place "$launch" \
  --words "Keep an eye on the inbox folder and keep reports/inbox-report.md current." \
  --watch 'inbox/*' --report reports/inbox-report.md --per-run-usd "$per_run" \
  --instructions "Read the files under inbox/ that changed (use the read tool; do not run shell commands) and write a short Markdown report: new decisions, open requests with their owner, and anything that needs the person. Carry forward items from the previous report that are still open." \
  | head -1 | sed -E 's/.*"id":"([^"]+)".*/\1/')"
[[ -n "$inbox" ]] || fail "setting up the inbox work"
echo "inbox work: $inbox"

say "Ongoing work: review launch copy when the product spec changes (placed in Marketing)"
review="$("$bin" standing add --json --workspace "$project" --place "$marketing" \
  --words "When the product spec changes, review the launch copy against it." \
  --watch 'product/*' --report marketing/review-notes.md --per-run-usd "$per_run" \
  --instructions "Compare marketing/launch-copy.md with product/spec.md (use the read tool; do not run shell commands). List every claim in the copy the spec does not support, quoting the spec line. Do not change any file." \
  | head -1 | sed -E 's/.*"id":"([^"]+)".*/\1/')"
[[ -n "$review" ]] || fail "setting up the review work"
echo "review work: $review"

say "First pass: every watch takes its baseline; nothing runs"
check "the baseline"
expect "the baseline" "$inbox" 0
expect "the baseline" "$review" 0

if [[ -n "${SETUP_ONLY:-}" ]]; then
  say "Seeded. Drive it yourself:"
  echo "  export AFORGE_HOME=$AFORGE_HOME"
  echo "  echo 'Decision: ship Friday' > $project/inbox/today.md"
  echo "  $bin standing check && $bin standing show $inbox"
  exit 0
fi

say "A new inbox note starts a run; the report is checked against the Launch rule, then published"
cat > "$project/inbox/2026-09-10-standup.md" <<'EOF'
Standup notes
- Decision: launch moves to Friday.
- Request: Priya to confirm the venue (priya@example.com, +1 555 0100).
- Request: someone needs to own the press release.
EOF
check "the first inbox note"
expect "the first inbox note" "$inbox" 1 1 "added inbox/2026-09-10-standup.md"
echo "--- reports/inbox-report.md"; cat "$project/reports/inbox-report.md"
no_contact "$project/reports/inbox-report.md"

say "Edit the work: the NEXT run uses the new instructions; the last run keeps what it had"
run standing edit "$inbox" --instructions "Read the files under inbox/ that changed (use the read tool; do not run shell commands). Write a Markdown table of open requests with columns Request, Owner, Due; unknown owners are 'unassigned'. Then list decisions." || fail "editing the inbox work"
cat >> "$project/inbox/2026-09-10-standup.md" <<'EOF'
- Request: Bob owns the press release, due Wednesday.
EOF
check "the edited work"
expect "the edited work" "$inbox" 2 2 "modified inbox/2026-09-10-standup.md"
echo "--- reports/inbox-report.md"; cat "$project/reports/inbox-report.md"
no_contact "$project/reports/inbox-report.md"
grep -q '|' "$project/reports/inbox-report.md" || fail "the edited instructions asked for a table and the report has none"
echo "ok: the report is now a table"

say "Pause: a new note waits; nothing runs"
run standing pause "$inbox" || fail "pausing"
echo "- Decision: budget capped at 5k." > "$project/inbox/2026-09-11-budget.md"
check "the paused pass"
expect "the paused pass" "$inbox" 2
say "Resume: the change made while paused is picked up, not lost"
run standing resume "$inbox" || fail "resuming"
check "the resumed pass"
expect "the resumed pass" "$inbox" 3 2 "added inbox/2026-09-11-budget.md"
no_contact "$project/reports/inbox-report.md"

say "Product changes: the marketing review runs, and the copy is untouched"
copy_before="$(sha "$project/marketing/launch-copy.md")"
cat > "$project/product/spec.md" <<'EOF'
# Product spec
- Offline support: removed in this release.
- Export: CSV only.
EOF
check "the spec change"
expect "the spec change" "$review" 1 1 "modified product/spec.md"
expect "the spec change" "$inbox" 3
echo "--- marketing/review-notes.md"; cat "$project/marketing/review-notes.md"
[[ "$copy_before" == "$(sha "$project/marketing/launch-copy.md")" ]] || fail "the review changed launch-copy.md, which it was only asked to review"
echo "ok: launch-copy.md unchanged"

say "Stop: final"
run standing stop "$inbox" || fail "stopping"
echo "- Decision: nothing after stop." > "$project/inbox/2026-09-12-late.md"
check "the pass after stop"
expect "the pass after stop" "$inbox" 3
if "$bin" standing resume "$inbox" >/dev/null 2>&1; then fail "a stopped item resumed"; fi
echo "ok: stopped work does not resume"

say "An idle pass runs nothing and publishes nothing twice"
report_before="$(sha "$project/reports/inbox-report.md")" notes_before="$(sha "$project/marketing/review-notes.md")"
check "the idle pass"
expect "the idle pass" "$inbox" 3
expect "the idle pass" "$review" 1
[[ "$report_before" == "$(sha "$project/reports/inbox-report.md")" && "$notes_before" == "$(sha "$project/marketing/review-notes.md")" ]] || fail "an idle pass rewrote a report"
echo "ok: both reports unchanged"

say "Cause and effect, per run"
run standing show "$inbox"
run standing show "$review"

say "Spend in this home"
python3 - "$AFORGE_HOME/v3/usage.jsonl" <<'EOF'
import json, sys
total, calls = 0.0, 0
try:
    for line in open(sys.argv[1]):
        row = json.loads(line)
        total += row.get("usd", 0) or 0
        calls += row.get("calls", 0) or 0
except FileNotFoundError:
    pass
print(f"model calls: {calls}  spend: ${total:.4f} (every model call in this home, from its usage ledger)")
EOF
printf '\n\033[1;32mDEMO PASSED\033[0m — every step held, on %s, model %s.\n' "$("$bin" version 2>&1 | head -1)" "$model"
echo "Kept at $dir — export AFORGE_HOME=$AFORGE_HOME to keep exploring with $bin standing ..."
