#!/bin/sh
# Prints the operator access link of this organization, with an address that works from this
# machine. The server prints the address it listens on ("[::]:8787") unless CODEAF_PUBLIC_DOMAIN
# names the host people reach it on; this swaps the first for localhost and your published port.
# The link is a secret, and it works once: whoever opens it first becomes the operator.
set -eu
cd "$(dirname "$0")"
DC="${COMPOSE:-docker compose}"
port=$(sed -n 's/^CODEAF_PORT=//p' .env 2>/dev/null | tail -1)
link=$($DC logs --no-log-prefix codeaf 2>/dev/null | grep -o 'codeaf://factory/join?operator=true[^ ]*' | tail -1 | tr -d '\r' || true)
if [ -z "$link" ]; then
  echo "No operator link in the server's log. Either this organization was already claimed (its operator signs in" >&2
  echo "instead), or the server is not up yet: docker compose ps, docker compose logs codeaf." >&2
  exit 1
fi
printf '%s\n' "$link" | sed "s|%5B%3A%3A%5D%3A8787|localhost%3A${port:-8787}|"
