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
# Local proofs use Miniflare's built-in request metadata instead of fetching
# Cloudflare's default Request.cf fixture during development startup.
export CLOUDFLARE_CF_FETCH_ENABLED=false
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
# Only local proof runs opt into the proxy policy. Each Wrangler gets a
# private package copy, so simultaneous runs never edit an installed asset.
if [ -n "${CODEAF_RELAYCONF_RETRIES:-}" ]; then
  case "$1" in
    wrangler|*/wrangler)
      local_binary=$(command -v "$1")
      local_proxy=$(node "$(dirname "$0")/local-proxy.js" "$local_state" "$local_binary")
      shift
      set -- node "$local_proxy" "$@"
      ;;
  esac
fi
"$@" &
child=$!
status=0
wait "$child" || status=$?
child=
exit "$status"
