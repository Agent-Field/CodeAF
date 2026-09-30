#!/usr/bin/env bash
set -euo pipefail

payload="$1"
message="$(jq -r .text "$payload")"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  printf '%s\n\n' "$message" >> "$GITHUB_STEP_SUMMARY"
fi
if [ -z "${SLACK_RELEASE_WEBHOOK:-}" ]; then
  echo '::warning::SLACK_RELEASE_WEBHOOK is not configured; the message is in the run summary.'
  exit 0
fi
if ! curl -fsS --max-time 20 --retry 2 -H 'Content-Type: application/json' --data-binary "@$payload" "$SLACK_RELEASE_WEBHOOK" > /dev/null; then
  echo '::warning::Slack webhook POST failed; the message is in the run summary.'
fi
