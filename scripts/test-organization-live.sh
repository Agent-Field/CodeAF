#!/usr/bin/env bash
# Explicit live runs must not report a skipped suite as successful verification.
set -euo pipefail
if [[ -z "${OPENROUTER_API_KEY:-}" ]]; then
  echo 'OPENROUTER_API_KEY is required for the live organization journeys.' >&2
  exit 2
fi
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
fixture="$(mktemp -d "${TMPDIR:-/tmp}/aforge-organization-profile.XXXXXX")"
trap 'rm -rf "$fixture"' EXIT
printf '{}\n' > "$fixture/config.json"
log="${ORGANIZATION_TEST_LOG:-${TMPDIR:-/tmp}/aforge-organization-$(date +%Y%m%d-%H%M%S)-$$.jsonl}"
echo "Live test receipts: $log"
AFORGE_HOME="$fixture" AFORGE_PROFILE_DIR='' AFORGE_E2E_REQUIRE_LIVE=1 go test -tags e2e -count=1 -timeout 40m -json -run "${ORGANIZATION_TEST_RUN:-^TestOrganizationE2E$}" ./internal/e2e/ | tee "$log"
# The Go JSON stream uses a stable encoding. Requiring the top-level pass also
# rejects an empty test selection or a skipped credential prerequisite.
if ! rg -q '"Action":"pass".*"Test":"TestOrganizationE2E"' "$log" || ! rg -q '"Action":"pass".*"Test":"TestOrganizationE2E/' "$log"; then
  echo 'The live organization suite did not run to completion; inspect the receipts.' >&2
  exit 1
fi
