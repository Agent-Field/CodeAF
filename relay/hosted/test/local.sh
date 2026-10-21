#!/bin/sh
# Local tools keep credentials, logs and metrics away from the caller's home.
set -eu
local_state=$(mktemp -d "${TMPDIR:-/tmp}/codeaf-relay-local.XXXXXX")
export XDG_CONFIG_HOME="$local_state/config"
export XDG_CACHE_HOME="$local_state/cache"
export XDG_STATE_HOME="$local_state/state"
export WRANGLER_LOG_PATH="$local_state/logs/wrangler.log"
export WRANGLER_SEND_METRICS=false
export WRANGLER_HIDE_BANNER=true
export WRANGLER_NO_SKILLS_UPDATE_PROMPTS=true
export WRANGLER_REGISTRY_PATH="$local_state/registry"
export CI=1
mkdir -p "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME" "$XDG_STATE_HOME" "$local_state/logs"
child=
cleanup() {
  if [ -n "$child" ]; then kill "$child" 2>/dev/null || true; fi
  rm -rf "$local_state"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
"$@" &
child=$!
status=0
wait "$child" || status=$?
child=
exit "$status"
