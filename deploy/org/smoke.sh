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
# COMPOSE='docker compose -p other' picks another project; SMOKE_ENV='--env-file x' adds an env file.
set -eu
cd "$(dirname "$0")"
[ -f .env ] || ./setup.sh
DC="${COMPOSE:-docker compose} --env-file .env --env-file .env.stub ${SMOKE_ENV:-} --profile stub"
SMOKE=/opt/org/smoke.py
bad=0
run() { # service exec-flags section [args]: run one section, remember a failure, go on
  svc=$1; flags=$2; shift 2
  # shellcheck disable=SC2086
  $DC exec -T -u codeaf $flags "$svc" python3 $SMOKE "$@" || bad=1
}

echo '== bringing the stack up (the first time this builds the images: several minutes)'
$DC up -d --build --wait --wait-timeout 600

echo '== operator link'
link=$(COMPOSE="$DC" ./operator-link.sh)   # a secret: it is passed on, never printed here

run runner '' control
run codeaf '-e ORG_URL=http://127.0.0.1:8787' org --operator-link "$link"
run runner '' job

echo '== restarting every service'
$DC restart
$DC up -d --wait --wait-timeout 600
run runner '' after
run codeaf '-e ORG_URL=http://127.0.0.1:8787' after-org

if [ "$bad" = 0 ]; then echo 'SMOKE PASSED'; else echo 'SMOKE FAILED' >&2; exit 1; fi
