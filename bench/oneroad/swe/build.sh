#!/usr/bin/env bash
# build.sh — build the wave-2 images this machine could not build, from the
# patched arm64 Dockerfiles in envs/. Detached, parallel, one log per task.
set -uo pipefail
SWE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOGS="${LOGS:-/tmp/claude-1001/-home-santosh/36adab24-74be-4ae9-946a-ab932634b759/scratchpad/swe-builds}"
mkdir -p "$LOGS"
TASKS="${*:-turborepo-fix-prune-missing-sources gitea-fix-force-push-timeline gitea-refactor-auth-middleware gitea-fix-codeql-code-scanning}"
for t in $TASKS; do
  [ -f "$SWE/envs/$t/Dockerfile" ] || { echo "no patched Dockerfile for $t"; continue; }
  (
    started=$(date +%s)
    # The context is the patched directory itself; these Dockerfiles COPY
    # nothing, so the context only has to hold the Dockerfile.
    docker build --progress=plain -t "$t:latest" -f "$SWE/envs/$t/Dockerfile" "$SWE/envs/$t" \
      > "$LOGS/$t.arm64.log" 2>&1
    rc=$?
    printf '%s %s rc=%s %ss\n' "$(date -Is)" "$t" "$rc" "$(( $(date +%s) - started ))" \
      >> "$LOGS/arm64-builds.status"
  ) &
  echo "launched $t (pid $!)"
done
wait
echo "all builds finished — see $LOGS/arm64-builds.status"
