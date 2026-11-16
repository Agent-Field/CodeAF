#!/usr/bin/env bash
# Passkeys end to end against the real binary: scripts/passkey-e2e.mjs says
# what it proves. Run `make build` first.
#
#   PLAYWRIGHT_CORE=<dir of a playwright-core whose browsers are installed> \
#     scripts/passkey-e2e.sh
#
# It starts its own `codeaf factory serve` and `codeaf app` engines in a
# temporary home and never touches ~/.codeaf. SHOTS=<dir> keeps the page's
# screenshots at 320 and 1024 pixels.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bin="${CODEAF_BIN:-$root/bin/codeaf}"
if [[ ! -x "$bin" ]]; then
  echo "passkey-e2e: no $bin; run make build first" >&2
  exit 2
fi
if [[ -z "${PLAYWRIGHT_CORE:-}" ]]; then
  echo "passkey-e2e: set PLAYWRIGHT_CORE to a playwright-core directory" >&2
  exit 2
fi
command -v openssl >/dev/null || { echo "passkey-e2e: needs openssl" >&2; exit 2; }
CODEAF_BIN="$bin" exec node "$root/scripts/passkey-e2e.mjs"
