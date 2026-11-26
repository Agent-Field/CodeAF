#!/bin/sh
# The organization's relay (role `relay`): `codeaf relay`, the service its members' computers pair through, see each other
# through and open each other's chats live through, so none of that needs the public one. It is a role of the codeaf image,
# not an image of its own, and it speaks plain HTTP: the platform (Railway, Caddy, a load balancer) serves the HTTPS in front of it.
#
# WHAT IT KEEPS. The device list and the chats' places, which are small, in $CODEAF_RELAY_STORE (a volume: without it a restart
# makes every member's computers forget each other). It does NOT keep chat content unless CODEAF_RELAY_STORAGE=on, because the
# organization's hosted chats are already kept by the organization server and a second copy of them here would be a duplicate.
# Every setting arrives as a variable; nothing here is secret.
set -eu

port="${PORT:-8788}"
listen="${CODEAF_RELAY_LISTEN:-0.0.0.0:$port}"
store="${CODEAF_RELAY_STORE:-/data/relay}"
mkdir -p "$store"

# One word each, so a typo stops the service with a sentence instead of starting it with the opposite setting.
word() { # NAME DEFAULT: the variable's value, lower case, or the default when it is empty
  eval "v=\${$1:-}"
  [ -n "$v" ] || v="$2"
  printf '%s' "$v" | tr 'A-Z' 'a-z'
}
storage=$(word CODEAF_RELAY_STORAGE off)
status=$(word CODEAF_RELAY_STATUS off)
proxy=$(word CODEAF_RELAY_TRUST_PROXY off)

set -- --listen "$listen" --store "$store"
case "$storage" in
  off)
    # A codeaf from before the switch would stop at "flag provided but not defined", which says nothing of why; and starting it
    # without the switch would keep chats the operator said to leave out. So it stops here, in words.
    if ! codeaf relay --help 2>&1 | grep -q -- '--storage'; then
      echo "relay: this codeaf is older than the relay's --storage switch, so it cannot leave chats out. Build the image from this checkout (CODEAF_BINARY_FROM=source), or set CODEAF_RELAY_STORAGE=on to keep them here." >&2
      exit 2
    fi
    set -- "$@" --storage off ;;
  on) ;;
  *) echo "relay: CODEAF_RELAY_STORAGE is on or off, not '$storage'" >&2; exit 2 ;;
esac
case "$status" in
  on|1|true|yes) ;;
  off|0|false|no) set -- "$@" --status=false ;;
  *) echo "relay: CODEAF_RELAY_STATUS is on or off, not '$status'" >&2; exit 2 ;;
esac
case "$proxy" in
  on|1|true|yes) set -- "$@" --trust-proxy ;;
  off|0|false|no) ;;
  *) echo "relay: CODEAF_RELAY_TRUST_PROXY is on or off, not '$proxy'" >&2; exit 2 ;;
esac

echo "relay: listening on $listen; keeping its device list in $store; chat storage $storage" >&2
exec codeaf relay "$@"
