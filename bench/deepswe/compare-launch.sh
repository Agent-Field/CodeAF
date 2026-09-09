#!/usr/bin/env bash
# Credentials live outside the synchronized tree and never enter a candidate.
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
out="$HOME/bench-artifacts/af653-complex-20260905"
export OPENROUTER_API_KEY="$(cat "$HOME/.local/state/af653-complex/guard-key")"
image="$(cat "$out/runtime-image.txt")"
verifier=sha256:9f0582e886813e7a6dcc3892238db52e1a4f6362d07f6fa465f2b457271063f1
mode="${1:-preflight}"
if [[ "$mode" == preflight-pi ]]; then
  bash "$root/bench/deepswe/compare-cell.sh" pi "$out/preflight-pi-v2" "$image" "$verifier" 180 preflight
elif [[ "$mode" == preflight ]]; then
  for arm in aforge pi; do
    bash "$root/bench/deepswe/compare-cell.sh" "$arm" "$out/preflight-$arm" "$image" "$verifier" 180 preflight
  done
else
  [[ -f "$out/preflight-approved.json" ]] || { echo 'Both runtime preflights must pass first.' >&2; exit 2; }
  # This is one predeclared paired pilot, not enough repeats for a ranking.
  for arm in pi aforge; do
    bash "$root/bench/deepswe/compare-cell.sh" "$arm" "$out/score-$arm" "$image" "$verifier" 1800 score
  done
fi
