#!/bin/sh
# Prints the latest operator access link from Railway logs. Run from a linked project or set RAILWAY_PROJECT
# and RAILWAY_ENVIRONMENT. RAILWAY_SERVICE defaults to codeaf and OPERATOR_LINK_LINES defaults to 300.
# A restart of an unclaimed server prints a fresh link and invalidates the older one. Always read the latest.
# The link is a secret that works once. Anybody who can read the project's logs can read it.
set -eu

if ! command -v railway >/dev/null 2>&1; then
  echo 'The Railway CLI is not installed.' >&2
  exit 2
fi

set -- logs --service "${RAILWAY_SERVICE:-codeaf}" --lines "${OPERATOR_LINK_LINES:-300}"
if [ -n "${RAILWAY_PROJECT:-}" ]; then set -- "$@" --project "$RAILWAY_PROJECT"; fi
if [ -n "${RAILWAY_ENVIRONMENT:-}" ]; then set -- "$@" --environment "$RAILWAY_ENVIRONMENT"; fi
if ! logs=$(railway "$@" </dev/null 2>/dev/null); then
  echo 'Fetching the service log with railway logs failed.' >&2
  exit 2
fi

link=$(printf '%s\n' "$logs" | grep -o 'codeaf://factory/join?operator=true[^[:space:]]*' | tail -n 1)
if [ -z "$link" ]; then
  echo 'The organization may already be claimed, the server may still be starting, or the link may have scrolled out of the log tail and raising OPERATOR_LINK_LINES may find it.' >&2
  exit 1
fi
printf '%s\n' "$link"
