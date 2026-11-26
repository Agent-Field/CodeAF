#!/usr/bin/env bash
# Checks, offline, what the organization's relay is started with (scripts/relay.sh) and what the organization server is told about it
# (scripts/serve.sh). A stand-in `codeaf` writes down its arguments instead of running, so nothing here starts a server or needs Docker.
set -eu
cd "$(dirname "$0")"
here=$PWD
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
cat >"$work/bin/codeaf" <<'STUB'
#!/bin/sh
# `codeaf relay --help` is how relay.sh asks whether this build has the --storage switch (STUB_OLD=1 is a build from before it).
if [ "$*" = "relay --help" ]; then
  echo "usage: codeaf relay [--listen host:port] [--store path]"
  [ "${STUB_OLD:-}" = 1 ] || echo "  --storage on|off    off keeps the device list and refuses chat uploads (default on)"
  exit 0
fi
printf '%s\n' "$*" >"$STUB_OUT"
STUB
chmod +x "$work/bin/codeaf"

failed=0
row() {
  local name=$1
  shift
  if "$@"; then
    printf 'PASS: %s\n' "$name"
  else
    printf 'FAIL: %s\n' "$name"
    failed=1
  fi
}

# relay_args VAR=value...: the arguments relay.sh gave `codeaf`, with every variable it reads cleared first.
relay_args() {
  rm -f "$work/out"
  env -u PORT -u CODEAF_RELAY_LISTEN -u CODEAF_RELAY_STORE -u CODEAF_RELAY_STORAGE -u CODEAF_RELAY_STATUS -u CODEAF_RELAY_TRUST_PROXY \
    PATH="$work/bin:$PATH" STUB_OUT="$work/out" CODEAF_RELAY_STORE="$work/store" "$@" sh "$here/scripts/relay.sh" 2>"$work/err" || return 1
  cat "$work/out"
}
has() { [[ " $1 " == *" $2 "* ]]; }
lacks() { ! has "$1" "$2"; }

default=$(relay_args)
row 'relay: with nothing set it keeps the device list, not the chats, and publishes no status' \
  bash -c '[ "$1" = "relay --listen 0.0.0.0:8788 --store $2 --storage off --status=false" ]' _ "$default" "$work/store"
row 'relay: its store folder is made' test -d "$work/store"

on=$(relay_args CODEAF_RELAY_STORAGE=on CODEAF_RELAY_STATUS=on CODEAF_RELAY_TRUST_PROXY=on)
row 'relay: storage on drops --storage off' lacks "$on" '--storage'
row 'relay: status on drops --status=false' lacks "$on" '--status=false'
row 'relay: trust-proxy on passes --trust-proxy' has "$on" '--trust-proxy'
row 'relay: trust-proxy is off by default' lacks "$default" '--trust-proxy'

row 'relay: a platform that names the port (Railway sets PORT) is listened on' \
  has "$(relay_args PORT=9100)" '--listen 0.0.0.0:9100'
row 'relay: CODEAF_RELAY_LISTEN wins over PORT' \
  has "$(relay_args PORT=9100 CODEAF_RELAY_LISTEN=127.0.0.1:7000)" '--listen 127.0.0.1:7000'
row 'relay: the words are not case sensitive' \
  lacks "$(relay_args CODEAF_RELAY_STORAGE=ON)" '--storage'

# A typo stops the service with a sentence; it never starts with the opposite setting.
refuses() {
  local out
  if out=$(relay_args "$1=maybe" 2>/dev/null); then return 1; fi
  grep -q "$1 is on or off" "$work/err" && [ ! -e "$work/out" ]
}
for var in CODEAF_RELAY_STORAGE CODEAF_RELAY_STATUS CODEAF_RELAY_TRUST_PROXY; do
  row "relay: $var=maybe is refused in words and nothing starts" refuses "$var"
done

# A codeaf from before --storage cannot leave chats out, so a relay asked to leave them out stops in words; one asked to keep them does not need it.
old_build_refuses() {
  local out
  if out=$(relay_args STUB_OLD=1 2>/dev/null); then return 1; fi
  grep -q 'older than the relay' "$work/err" && grep -q 'CODEAF_BINARY_FROM=source' "$work/err" && [ ! -e "$work/out" ]
}
row 'relay: a codeaf with no --storage switch stops in words when the relay is to keep no chats' old_build_refuses
row 'relay: a codeaf with no --storage switch still starts a relay that keeps the chats' \
  lacks "$(relay_args STUB_OLD=1 CODEAF_RELAY_STORAGE=on)" '--storage'

# serve.sh: the organization server writes the relay it advertises into its config, and only when there is one.
serve_config() {
  rm -f "$work/out"
  env -u CODEAF_ORG_RELAY_URL -u CODEAF_FACTORY_TOKEN PATH="$work/bin:$PATH" STUB_OUT="$work/out" \
    CODEAF_HOME="$work/home" CODEAF_ORG_NAME=Acme "$@" sh "$here/scripts/serve.sh" || return 1
  cat "$work/home/factory/config.yaml"
}
told=$(serve_config CODEAF_ORG_RELAY_URL=https://relay.example.com)
row 'serve: an advertised relay is written to the server config' \
  bash -c 'printf "%s\n" "$1" | grep -qxF "relay_url: \"https://relay.example.com\""' _ "$told"
none=$(serve_config)
row 'serve: with none named the config says nothing about one' bash -c '! printf "%s" "$1" | grep -q relay' _ "$none"

exit "$failed"
