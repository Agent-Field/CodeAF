#!/usr/bin/env bash
# summary.sh — read one or more runs and print the per-workload comparison.
#
# It is a thin wrapper so that the reading of a run is one command with no
# arguments to remember; everything it does is in lib/pareto.py, including the
# rules about what may be compared with what.
set -uo pipefail

CONV_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ $# -eq 0 ]; then
  # With no argument, the newest run under the default evidence directory.
  latest="$(ls -1d "$CONV_ROOT/../../bench-results/conversation"/*/results.jsonl 2>/dev/null | tail -1)"
  if [ -z "$latest" ]; then
    echo "usage: bench/conversation/summary.sh <results.jsonl> [more...]" >&2
    exit 1
  fi
  set -- "$latest"
fi

exec python3 "$CONV_ROOT/lib/pareto.py" "$@"
