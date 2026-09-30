#!/usr/bin/env bash
# Regression test for fetch-results.sh's completeness handling: a run carrying
# the full artifact contract passes --check, and one missing any required
# artifact exits non-zero. It uses FC_EVIDENCE_DIR so no real evidence, host or
# key is touched.
#
#   bash bench/frontiercode/tests/fetch-results-check.sh
set -euo pipefail
RIG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

RUN="$TMP/evidence/run-codeaf-senior-dev-x"
mkdir -p "$RUN/logs/grade"
for f in record.jsonl DONE artifacts.sha256 model.patch scan.json cost.json \
         logs/grade/grade.json egress-proxy.log; do
  printf 'evidence\n' > "$RUN/$f"
done

if ! FC_EVIDENCE_DIR="$TMP/evidence" bash "$RIG_DIR/fetch-results.sh" --check; then
  echo "FAIL: --check refused a complete run directory" >&2
  exit 1
fi
echo "ok  complete run directory accepted"

# The egress proxy log is the scanner's raw input: dropping it must refuse.
rm "$RUN/egress-proxy.log"
if FC_EVIDENCE_DIR="$TMP/evidence" bash "$RIG_DIR/fetch-results.sh" --check >/dev/null 2>&1; then
  echo "FAIL: --check accepted a run missing egress-proxy.log" >&2
  exit 1
fi
echo "ok  missing egress-proxy.log refused"

# Any required artifact missing is a refusal, not a silent partial fetch.
printf 'evidence\n' > "$RUN/egress-proxy.log"
rm "$RUN/model.patch"
if FC_EVIDENCE_DIR="$TMP/evidence" bash "$RIG_DIR/fetch-results.sh" --check >/dev/null 2>&1; then
  echo "FAIL: --check accepted a run missing model.patch" >&2
  exit 1
fi
echo "ok  missing model.patch refused"

# An empty required artifact is as incomplete as a missing one.
printf 'evidence\n' > "$RUN/model.patch"
: > "$RUN/logs/grade/grade.json"
if FC_EVIDENCE_DIR="$TMP/evidence" bash "$RIG_DIR/fetch-results.sh" --check >/dev/null 2>&1; then
  echo "FAIL: --check accepted an empty grade.json" >&2
  exit 1
fi
echo "ok  empty required artifact refused"

echo "fetch-results completeness regression: PASS"