#!/usr/bin/env bash
# demo-local-work.sh — ongoing work over LOCAL FILES, end to end, with a real
# model, in a disposable aforge home. No connectors, no accounts, nothing sent
# anywhere but the model provider.
#
# What it shows, each step through the shipped binary's own doors:
#   folders + a folder rule -> ongoing work placed in that folder ->
#   a file change starts a run -> the run's report is published ->
#   an edit reaches the next run -> pause holds work back -> resume catches up ->
#   a product spec change starts a marketing review (second work item) ->
#   stop is final -> `aforge standing show` prints cause and effect for every run.
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
#   go test -tags e2e -run '^TestLocalWorkJourney$' ./internal/e2e/
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
bin="$root/bin/aforge"
if [[ ! -x "$bin" ]]; then
  echo "bin/aforge is missing: run make build first." >&2
  exit 2
fi
if [[ -z "${OPENROUTER_API_KEY:-}" && -z "${SETUP_ONLY:-}" ]]; then
  echo "OPENROUTER_API_KEY is required for the live run (SETUP_ONLY=1 seeds without a model)." >&2
  echo "Key-free, scripted-model version: go test -tags e2e -run '^TestLocalWorkJourney\$' ./internal/e2e/" >&2
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
check() { run standing check; }

say "LIVE MODEL DEMO — local files only, model $model, at most \$$per_run a run, \$$AFORGE_DAILY_BUDGET a day"
echo "home:    $AFORGE_HOME"
echo "project: $project"

say "Folders, and a rule placed on each"
launch="$("$bin" collections create Launch --json | sed -E 's/.*"id":"([^"]+)".*/\1/')"
marketing="$("$bin" collections create Marketing --json | sed -E 's/.*"id":"([^"]+)".*/\1/')"
echo "Launch=$launch Marketing=$marketing"
run standing add --hold --scope "$launch" --workspace "$project" \
  --words "Inbox reports for Launch never quote email addresses or phone numbers; write [redacted]."
run standing add --hold --scope "$marketing" --workspace "$project" \
  --words "Marketing review notes cite the spec line behind every finding and never edit the copy itself."

say "Ongoing work: keep an inbox report current (placed in Launch)"
inbox="$("$bin" standing add --json --workspace "$project" --place "$launch" \
  --words "Keep an eye on the inbox folder and keep reports/inbox-report.md current." \
  --watch 'inbox/*' --report reports/inbox-report.md --per-run-usd "$per_run" \
  --instructions "Read the files under inbox/ that changed (use the read tool; do not run shell commands) and write a short Markdown report: new decisions, open requests with their owner, and anything that needs the person. Carry forward items from the previous report that are still open." \
  | head -1 | sed -E 's/.*"id":"([^"]+)".*/\1/')"
echo "inbox work: $inbox"

say "Ongoing work: review launch copy when the product spec changes (placed in Marketing)"
review="$("$bin" standing add --json --workspace "$project" --place "$marketing" \
  --words "When the product spec changes, review the launch copy against it." \
  --watch 'product/*' --report marketing/review-notes.md --per-run-usd "$per_run" \
  --instructions "Compare marketing/launch-copy.md with product/spec.md (use the read tool; do not run shell commands). List every claim in the copy the spec does not support, quoting the spec line. Do not change any file." \
  | head -1 | sed -E 's/.*"id":"([^"]+)".*/\1/')"
echo "review work: $review"

say "First pass: every watch takes its baseline; nothing runs"
check

if [[ -n "${SETUP_ONLY:-}" ]]; then
  say "Seeded. Drive it yourself:"
  echo "  export AFORGE_HOME=$AFORGE_HOME"
  echo "  echo 'Decision: ship Friday' > $project/inbox/today.md"
  echo "  $bin standing check && $bin standing show $inbox"
  exit 0
fi

say "A new inbox note starts a run; the report is published"
cat > "$project/inbox/2026-09-10-standup.md" <<'EOF'
Standup notes
- Decision: launch moves to Friday.
- Request: Priya to confirm the venue (priya@example.com, +1 555 0100).
- Request: someone needs to own the press release.
EOF
check
echo "--- reports/inbox-report.md"; cat "$project/reports/inbox-report.md"

say "Edit the work: the NEXT run uses the new brief; the last run keeps what it had"
run standing edit "$inbox" --instructions "Read the files under inbox/ that changed (use the read tool; do not run shell commands). Write a Markdown table of open requests with columns Request, Owner, Due; unknown owners are 'unassigned'. Then list decisions."
cat >> "$project/inbox/2026-09-10-standup.md" <<'EOF'
- Request: Bob owns the press release, due Wednesday.
EOF
check
echo "--- reports/inbox-report.md"; cat "$project/reports/inbox-report.md"

say "Pause: a new note waits; nothing runs"
run standing pause "$inbox"
echo "- Decision: budget capped at 5k." > "$project/inbox/2026-09-11-budget.md"
check
say "Resume: the change made while paused is picked up, not lost"
run standing resume "$inbox"
check

say "Product changes: the marketing review runs, and the copy is untouched"
copy_before="$(sha256sum "$project/marketing/launch-copy.md" | cut -d' ' -f1)"
cat > "$project/product/spec.md" <<'EOF'
# Product spec
- Offline support: removed in this release.
- Export: CSV only.
EOF
check
echo "--- marketing/review-notes.md"; cat "$project/marketing/review-notes.md"
copy_after="$(sha256sum "$project/marketing/launch-copy.md" | cut -d' ' -f1)"
[[ "$copy_before" == "$copy_after" ]] && echo "launch-copy.md unchanged" || echo "WARNING: launch-copy.md changed"

say "Stop: final"
run standing stop "$inbox"
echo "- Decision: nothing after stop." > "$project/inbox/2026-09-12-late.md"
check

say "Cause and effect, per run"
run standing show "$inbox"
run standing show "$review"

say "Spend in this home (every model call, from its usage ledger)"
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
print(f"model calls: {calls}  spend: ${total:.4f}")
EOF
echo
echo "Kept at $dir — export AFORGE_HOME=$AFORGE_HOME to keep exploring with $bin standing ..."
