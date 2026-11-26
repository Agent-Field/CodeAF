#!/usr/bin/env bash
# Checks which services each Compose profile turns on, from the file alone (`docker compose config` starts nothing).
# The scripted model belongs to `stub`; the scripted git host, which will not start without the token the smoke test
# makes, belongs to `gitstub`, so turning the scripted model on by hand cannot leave a container crash-looping.
set -eu
cd "$(dirname "$0")"
export AGENTFIELD_API_KEY=offline-test
DC="${COMPOSE:-docker compose}"

services() {
  # --env-file names .env.example, so a .env of the operator's, with profiles of its own in it, is not read.
  # shellcheck disable=SC2086
  $DC --env-file .env.example "$@" config --services | sort | tr '\n' ' '
}
has() { [[ " $1" == *" $2 "* ]]; }

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

plain=$(services)
stub=$(services --profile stub)
hosted_stub=$(services --profile hosted --profile stub)
gitstub=$(services --profile gitstub)

row 'no profile: the six services (the relay among them), no stub and no sessionhost' \
  bash -c '[ "$1" = "agentfield codeaf pr-af relay runner swe-af " ]' _ "$plain"
row 'stub turns the scripted model on and not the git host' \
  bash -c 'case " $1" in *" modelstub "*) ;; *) exit 1 ;; esac; case " $1" in *" gitstub "*) exit 1 ;; esac' _ "$stub"
row 'stub with hosted adds the sessionhost and still not the git host' \
  bash -c 'case " $1" in *" sessionhost "*" "*) ;; *) exit 1 ;; esac; case " $1" in *" gitstub "*) exit 1 ;; esac' _ "$hosted_stub"
row 'gitstub is its own profile' has "$gitstub" gitstub
row 'the smoke test asks for the gitstub profile when it runs the hosted part' \
  grep -q -- '--profile hosted --profile gitstub' smoke.sh

# The relay is bound the way the organization server is: on this machine alone unless the operator says otherwise, with a port
# of its own that the operator can change, and it keeps no chat content unless asked to.
service_json() { # SERVICE EXPRESSION: a Python expression over that service's resolved settings `r`
  # shellcheck disable=SC2086
  $DC --env-file .env.example config --format json |
    python3 -c 'import json, sys; r = json.load(sys.stdin)["services"][sys.argv[1]]; p = r["ports"][0] if r.get("ports") else {}; print(eval(sys.argv[2]))' "$1" "$2"
}
relay_words='" ".join([p["host_ip"], p["published"], str(p["target"]), r["environment"]["CODEAF_RELAY_STORAGE"], r["command"][0]])'
row 'the relay listens on 127.0.0.1:8788 and keeps no chats by default' \
  bash -c '[ "$1" = "127.0.0.1 8788 8788 off relay" ]' _ "$(service_json relay "$relay_words")"
row 'its bind, its port and its storage are the operator'"'"'s to change' \
  bash -c '[ "$1" = "0.0.0.0 9788 8788 on relay" ]' _ \
  "$(CODEAF_RELAY_BIND=0.0.0.0 CODEAF_RELAY_PORT=9788 CODEAF_RELAY_STORAGE=on service_json relay "$relay_words")"
row 'the organization server is told the relay address the operator names' \
  bash -c '[ "$1" = "https://relay.example.com" ]' _ \
  "$(CODEAF_ORG_RELAY_URL=https://relay.example.com service_json codeaf 'r["environment"]["CODEAF_ORG_RELAY_URL"]')"

exit "$failed"
