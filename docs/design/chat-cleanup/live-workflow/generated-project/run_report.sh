#!/bin/sh
# Safe export for finance imports.
#
# Runs report.py into a temporary file next to TARGET and moves it onto TARGET
# ONLY when the report fully succeeds (exit 0). A failed run -- for example a
# --strict run where a row was skipped -- leaves an existing TARGET byte-for-byte
# intact, so a partial report can never overwrite a file finance already uses.
#
# Usage: run_report.sh TARGET.csv CSV [report.py args...]
# Example:
#   ./run_report.sh october-food-summary.csv october-export.csv \
#       --month 2026-10 --category FOOD --csv --strict

set -eu

if [ "$#" -lt 2 ]; then
    echo "usage: $0 TARGET.csv CSV [report.py args...]" >&2
    exit 64
fi

target=$1
shift

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
tmp=$target.tmp.$$

# Remove the temporary file on any exit (success, error, or argument failure).
trap 'rm -f -- "$tmp"' 0

python3 "$script_dir/report.py" "$@" >"$tmp"

# Reached only if report.py exited 0; same-directory move is atomic.
mv -- "$tmp" "$target"
