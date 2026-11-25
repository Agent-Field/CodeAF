#!/bin/sh
# The keyless smoke test of this Compose stack. It spends nothing: every model call goes to the
# scripted stub (modelstub), whatever keys .env holds. It runs:
#   1. everything up and healthy;
#   2. AgentField, SWE-AF, PR-AF and the factory registered; PR-AF answers a call;
#   3. the operator link claims the organization; a second person joins by invite;
#   4. a factory job reaches SWE-AF through AgentField, with strict run-owner proofs on; its plan
#      question reaches the owner, and the answer resumes the run;
#   5. a restart of every service, then: still registered, still holding the runs and the people.
# Afterwards the stack keeps running against the stub. `docker compose up -d` puts your own keys back.
#
#     ./smoke.sh
#
# COMPOSE='docker compose -p other' picks another project; SMOKE_ENV='--env-file x' adds an env file;
# SMOKE_BUILD=--no-build uses the images that are already there (the default builds what changed).
#
# SMOKE_HOSTED_ONLY=1 runs only the hosted-chats part, on the three services it needs (codeaf, sessionhost,
# modelstub): no AgentField, no nodes, no runner. It implies SMOKE_HOSTED=1.
#
# SMOKE_HOSTED=1 adds hosted chats (the `hosted` profile, .env.stub.hosted): two people run chats on the
# sessionhost through `codeaf chat --org`, and the test checks that the chats are kept apart, hold no
# secrets, cannot reach the other services, can only be attached by their owner, survive a closed
# client, are cleaned up on a stop, and survive a restart of the sessionhost; then (P1) the metered model door, people's own
# keys and git through the organization, and (P2) private-by-default chats, a guest watching a shared chat through a wire that
# refuses what the guest's rights do not allow, bots, and the hosting policy. It needs a codeaf with
# `codeaf sessionhost` in it. A plain run builds the image from this checkout (compose.yaml), which has it; to skip
# that compile, build the image from a binary you made yourself (README.md "Hosted chats"):
#
#     cp ../../bin/codeaf codeaf-local && SMOKE_HOSTED=1 CODEAF_BINARY_FROM=local ./smoke.sh
set -eu
cd "$(dirname "$0")"
# Idempotent: makes .env once, and adds a secret a newer version needs to an .env that predates it.
./setup.sh
HOSTED="${SMOKE_HOSTED:-0}"
SERVICES=
FULL=1
if [ "${SMOKE_HOSTED_ONLY:-0}" = 1 ]; then HOSTED=1; FULL=0; SERVICES="codeaf sessionhost modelstub gitstub"; fi
HOSTED_FLAGS=
if [ "$HOSTED" = 1 ]; then
  HOSTED_FLAGS="--env-file .env.stub.hosted --profile hosted --profile gitstub"
  # The stub git host wants this as the person's GitHub access; it is made for this run and is worth nothing.
  GITSTUB_TOKEN="${GITSTUB_TOKEN:-$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')}"
  export GITSTUB_TOKEN
fi
# shellcheck disable=SC2086
DC="${COMPOSE:-docker compose} --env-file .env --env-file .env.stub $HOSTED_FLAGS ${SMOKE_ENV:-} --profile stub"
SMOKE=/opt/org/smoke.py
bad=0
run() { # service exec-flags section [args]: run one section, remember a failure, go on
  svc=$1; flags=$2; shift 2
  # shellcheck disable=SC2086
  $DC exec -T -u codeaf $flags "$svc" python3 $SMOKE "$@" || bad=1
}

echo '== bringing the stack up (the first time this builds the images: several minutes)'
# shellcheck disable=SC2086
$DC up -d ${SMOKE_BUILD:---build} --wait --wait-timeout 600 $SERVICES

echo '== operator link'
link=$(COMPOSE="$DC" ./operator-link.sh)   # a secret: it is passed on, never printed here

[ "$FULL" = 1 ] && run runner '' control
run codeaf '-e ORG_URL=http://127.0.0.1:8787' org --operator-link "$link"
if [ "$HOSTED" = 1 ]; then
  echo '== hosted chats'
  run codeaf '-e ORG_URL=http://127.0.0.1:8787' hosted-prepare
  # The test chats are let run commands with nobody at a keyboard (smoke.py says why); it is written on the
  # chats' machine, as root, into each chat's own settings.
  $DC exec -T codeaf cat /data/smoke-hosted-ids.json | $DC exec -T sessionhost python3 $SMOKE hosted-seed || bad=1
  run codeaf '-e ORG_URL=http://127.0.0.1:8787' hosted
  # What only the machine itself can show: the state the client side just wrote goes in on stdin.
  $DC exec -T codeaf cat /data/smoke-hosted.json | $DC exec -T sessionhost python3 $SMOKE hosted-host || bad=1
  # P1: the real model door (metered, capped), people's own keys, git through the organization, and what the machine holds.
  run codeaf '-e ORG_URL=http://127.0.0.1:8787 -e GITSTUB_TOKEN' hosted-p1
  $DC exec -T codeaf cat /data/smoke-p1.json | $DC exec -T sessionhost python3 $SMOKE hosted-p1-host || bad=1
  # The organization server alone restarts: a running chat's access must keep working, and stop at once when its chat is stopped.
  # shellcheck disable=SC2086
  $DC restart codeaf
  # shellcheck disable=SC2086
  $DC up -d --wait --wait-timeout 600 codeaf
  run codeaf '-e ORG_URL=http://127.0.0.1:8787' hosted-p1-restart
  $DC exec -T codeaf cat /data/smoke-p1.json | $DC exec -T sessionhost python3 $SMOKE hosted-p1-bearer || bad=1
  mode=$($DC logs sessionhost 2>&1 | grep -m1 'sessionhost: mode' || true)
  if [ -n "$mode" ]; then echo "PASS  C4 the machine started in a mode that keeps chats apart   ${mode##*: }"; else echo 'FAIL  C4 the machine said no mode at start' >&2; bad=1; fi
  # Fail closed: the same machine without the right to switch users must refuse, not run chats as root.
  # shellcheck disable=SC2086
  if out=$($DC run --rm -T --no-deps --entrypoint codeaf --cap-drop SETUID sessionhost sessionhost probe 2>&1); then
    echo "FAIL  C4 a machine that cannot switch users still says it is ready   $out" >&2; bad=1
  elif printf '%s' "$out" | grep -q '"ok": false'; then
    echo 'PASS  C4 a machine that cannot switch users refuses to run chats'
  else
    echo "FAIL  C4 the refusal was not a clean one   $out" >&2; bad=1
  fi
  # P2: a chat is private until it is shared, a shared chat is watched through a wire that checks every call against the guest's
  # rights, a bot is its creator and no more, and the policy says where chats may run. Needs a third person, invited by the section.
  run codeaf '-e ORG_URL=http://127.0.0.1:8787' hosted-p2-prepare
  $DC exec -T codeaf cat /data/smoke-p2-ids.json | $DC exec -T sessionhost python3 $SMOKE hosted-seed || bad=1
  run codeaf '-e ORG_URL=http://127.0.0.1:8787 -e GITSTUB_TOKEN' hosted-p2
fi
[ "$FULL" = 1 ] && run runner '' job

echo '== restarting every service'
# shellcheck disable=SC2086
$DC restart $SERVICES
# shellcheck disable=SC2086
$DC up -d --wait --wait-timeout 600 $SERVICES
[ "$FULL" = 1 ] && run runner '' after
run codeaf '-e ORG_URL=http://127.0.0.1:8787' after-org
if [ "$HOSTED" = 1 ]; then
  $DC exec -T sessionhost python3 $SMOKE hosted-leftovers || bad=1
  run codeaf '-e ORG_URL=http://127.0.0.1:8787' hosted-after
  run codeaf '-e ORG_URL=http://127.0.0.1:8787 -e GITSTUB_TOKEN' hosted-p1-after
  run codeaf '-e ORG_URL=http://127.0.0.1:8787' hosted-p2-after
fi

if [ "$bad" = 0 ]; then echo 'SMOKE PASSED'; else echo 'SMOKE FAILED' >&2; exit 1; fi
