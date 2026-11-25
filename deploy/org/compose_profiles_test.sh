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

row 'no profile: the five services, no stub and no sessionhost' \
  bash -c '[ "$1" = "agentfield codeaf pr-af runner swe-af " ]' _ "$plain"
row 'stub turns the scripted model on and not the git host' \
  bash -c 'case " $1" in *" modelstub "*) ;; *) exit 1 ;; esac; case " $1" in *" gitstub "*) exit 1 ;; esac' _ "$stub"
row 'stub with hosted adds the sessionhost and still not the git host' \
  bash -c 'case " $1" in *" sessionhost "*" "*) ;; *) exit 1 ;; esac; case " $1" in *" gitstub "*) exit 1 ;; esac' _ "$hosted_stub"
row 'gitstub is its own profile' has "$gitstub" gitstub
row 'the smoke test asks for the gitstub profile when it runs the hosted part' \
  grep -q -- '--profile hosted --profile gitstub' smoke.sh

exit "$failed"
