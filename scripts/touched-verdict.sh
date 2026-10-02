#!/usr/bin/env bash
# The classifier consumes Go's event stream, rather than guessing test names
# from assertion prose or hiding the first failure behind a successful retry.
set -euo pipefail
exec python3 "$(dirname "${BASH_SOURCE[0]}")/touched-verdict.py" "$@"
