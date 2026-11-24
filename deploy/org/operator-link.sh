#!/bin/sh
# Prints this organization's operator access link from the server's log. The link is a secret and it works
# once: whoever opens it first becomes the operator. The server names itself by the address it listens on, so
# the link carries `http://[::]:8787` unless CODEAF_PUBLIC_DOMAIN names the public host (README.md, "Claim the
# organization"); the link is only accepted with exactly the address the server gave it, so it is printed as is.
set -eu
cd "$(dirname "$0")"
DC="${COMPOSE:-docker compose}"
link=$($DC logs --no-log-prefix codeaf 2>/dev/null | grep -o 'codeaf://factory/join?operator=true[^ ]*' | tail -1 | tr -d '\r' || true)
if [ -z "$link" ]; then
  echo "No operator link in the server's log. Either this organization was already claimed (its operator signs in" >&2
  echo "instead), or the server is not up yet: docker compose ps, docker compose logs codeaf." >&2
  exit 1
fi
printf '%s\n' "$link"
