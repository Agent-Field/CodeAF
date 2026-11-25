#!/bin/sh
# Run from the repository root. Railway indexes the checkout before uploading it, so a tracked symlink
# whose target is missing prevents the upload.
set -eu

index=$(mktemp)
trap 'rm -f "$index"' 0
# Keep Git from quoting paths so that names with spaces or non-ASCII characters are checked as stored.
git ls-files -s -z >"$index"
# shellcheck disable=SC2016 # The child shell expands these variables.
counts=$(xargs -0 sh -c '
  tab=$(printf "\t")
  count=0
  bad=0
  for entry do
    case "$entry" in
      "120000 "*) path=${entry#*"$tab"} ;;
      *) continue ;;
    esac
    # An unstaged deletion is still in the index but cannot enter the upload.
    [ -L "./$path" ] || continue
    count=$((count + 1))
    if [ ! -e "./$path" ]; then
      printf "upload-check: %s -> %s\n" "$path" "$(readlink "./$path")" >&2
      bad=1
    fi
  done
  printf "%s\n" "$count"
  exit "$bad"
' sh <"$index") || exit 1
printf '%s\n' "$counts" | awk '{count += $1} END {printf "upload-check: %d tracked symlinks, none dangling\n", count}'
