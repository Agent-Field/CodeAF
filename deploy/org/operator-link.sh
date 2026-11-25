#!/bin/sh
# Prints this organization's operator access link from the server's log. The link is a secret and it works
# once: whoever opens it first becomes the operator. The server names itself by the address it listens on, so
# the link carries `http://[::]:8787` unless CODEAF_PUBLIC_DOMAIN names the public host (README.md, "Claim the
# organization"); the link is only accepted with exactly the address the server gave it, so it is printed as is.
#
# The log keeps the link of every start, a spent one included, so the newest link in it is not proof that the
# organization can still be claimed. The server is asked about it before it is printed, and a spent, expired or
# retired one is reported instead of handed over. If the server cannot be asked, the link is printed as the log has it.
set -eu
cd "$(dirname "$0")"
DC="${COMPOSE:-docker compose}"
link=$($DC logs --no-log-prefix codeaf 2>/dev/null | grep -o 'codeaf://factory/join?operator=true[^ ]*' | tail -1 | tr -d '\r' || true)
if [ -z "$link" ]; then
  echo "No operator link in the server's log. Either this organization was already claimed (its operator signs in" >&2
  echo "instead), or the server is not up yet: docker compose ps, docker compose logs codeaf." >&2
  exit 1
fi
# The check only reads: it never claims, mints a session or uses the link up. It runs inside the container, where
# the server answers on its own port, and the link goes in on stdin so it is not on a command line.
state=$(printf '{"verb":"check","link":"%s"}' "$link" |
  $DC exec -T codeaf curl -s -H 'Content-Type: application/json' --data-binary @- http://127.0.0.1:8787/v1/factory-access 2>/dev/null |
  sed -n 's/.*"state":"\([a-z_]*\)".*/\1/p' | head -n 1 || true)
case "$state" in
  ready | "") printf '%s\n' "$link" ;;
  *)
    echo "The operator link in the server's log is no longer good (the server says: $state). Either this organization" >&2
    echo "was already claimed (its operator signs in instead), or the link ran out: one lasts 24 hours." >&2
    echo "If nobody has claimed it yet, docker compose restart codeaf prints a fresh one." >&2
    exit 1
    ;;
esac
