#!/usr/bin/env bash
# Makes a DRAFT Railway template of the org stack from the PUBLISHED IMAGES (README.md "Published images").
#
#     ./make-template.sh                 dry run: prints every Railway call it would make, changes nothing (the default)
#     ./make-template.sh --apply         makes a fresh source project, adds the seven services from their images, and turns
#                                        the project into an UNPUBLISHED template draft in your workspace
#
#     ORG_REGISTRY=agentfield ORG_TAG=dev    which images (the same two variables as images.env); a 12-character commit id
#                                            as ORG_TAG names one exact build, which is what a template you share should use
#     RAILWAY_WORKSPACE=<id or name>         the workspace the project and the draft go in (needed with --apply)
#     TEMPLATE_PROJECT=codeaf-org-template   the name of the source project
#
# WHAT THE TEMPLATE IS, AND IS NOT
#   * Every service is an IMAGE SOURCE. No GitHub repository, no GitHub App, no deployment trigger: the call that stopped
#     the first attempt ("Cannot create deployment trigger ... no one in the project has access to it") is not made.
#   * No real value is ever in it. The AgentField API key, the server key that seals people's own keys, and the hosted-chat
#     secret are Railway template functions, `${{secret(64, ...)}}`, which Railway fills with a NEW random value
#     for every deploy. The other services reference the AgentField key as `${{agentfield.AGENTFIELD_API_KEY}}`. The model
#     key and GitHub token are references to empty inputs on the `codeaf` service. This script contains no secret and
#     refuses (see check_var()) to carry any variable named like a secret whose value is not empty or a `${{...}}` expression.
#   * The smoke-test switches (RUNNER_SELFTEST, SMOKE_FIXTURE_REPO, the model stub and git stub) are not in it.
#   * It is never published. This script has no `railway templates publish` call and never will; the draft opens in the
#     dashboard's template editor, where the person who owns it decides.
#   * Hosted chats are included through the private sessionhost service. It uses the user switch and Landlock on Railway
#     (`codeaf sessionhost probe` in that service reports `uid+fence`). The person using the template still needs to supply
#     the model key input.
#
# RUN AGAINST RAILWAY: --apply ran end to end on 2026-11-25 against Railway CLI 5.71.0, and two calls had to change for
# the current CLI: `railway volume` takes the service as `railway volume --service <service ID> add ...` (the ID, from the
# JSON `railway add` printed, never the name; `volume add --service` is an error and a name panics the CLI), and every
# `railway environment edit` needs `--message` (without it a terminal run stops at "Apply changes now? (Y/n)" and waits).
# railway_scripts_test.sh pins both calls against a stub CLI that records its arguments. Run --apply from a terminal:
# `railway environment edit --service-config ...` reads its patch from the command line only when stdin is a terminal. With
# non-terminal stdin, such as in CI or an agent, it reads a JSON patch from stdin and ignores those flags.
# Two things the CLI cannot do: it has no volume SIZE option (every volume comes out at Railway's default, 50 GB; resize each
# one in the dashboard, see README.md "On Railway"), and `railway templates create` makes a draft the owner still has to
# finish by hand.
# shellcheck disable=SC2016  # the ${{...}} in single quotes are Railway expressions, not shell expansions
set -euo pipefail

APPLY=0
case "${1:-}" in
  --apply) APPLY=1 ;;
  ""|--dry-run) ;;
  *) echo "usage: $0 [--dry-run|--apply]" >&2; exit 2 ;;
esac

REGISTRY="${ORG_REGISTRY:-agentfield}"
TAG="${ORG_TAG:-dev}"
PROJECT="${TEMPLATE_PROJECT:-codeaf-org-template}"
MODEL=deepseek/deepseek-v4-flash-0731
CODEAF_IMAGE="${REGISTRY}/codeaf-org:${TAG}"
AGENTFIELD_IMAGE="${REGISTRY}/codeaf-org-agentfield:${TAG}"
SWE_AF_IMAGE="${REGISTRY}/codeaf-org-swe-af:${TAG}"
PR_AF_IMAGE="${REGISTRY}/codeaf-org-pr-af:${TAG}"

# Variable names that mean secret; a value for one must be empty or a Railway expression, never a literal.
SECRETISH='(KEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|AUTH)'
# Smoke-test switches: never in a template.
SMOKE_ONLY='^(RUNNER_SELFTEST|SMOKE_FIXTURE_REPO|SWE_SMOKE_FIXTURE_REPO|CODEAF_BASE_URL|STUB_DELAY|STUB_COST_USD|GITSTUB_TOKEN)$'
KEY64='${{secret(64, "abcdef0123456789")}}'   # openssl rand -hex 32: a new value for every deploy

calls=0
# quote ARG: ARG as one shell word, in single quotes when it needs them, so a dry run reads as the command it stands for.
quote() {
  case "$1" in
    ""|*[!A-Za-z0-9_@%+=:,./-]*) printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")" ;;
    *) printf '%s' "$1" ;;
  esac
}
rail() {
  calls=$((calls + 1))
  printf '+ railway'
  local arg
  for arg in "$@"; do printf ' '; quote "$arg"; done
  printf '\n'
  if [ "$APPLY" = 1 ]; then
    if [ "$capture" = 1 ]; then
      # Keep the JSON Railway prints on stdout (its prompts and progress stay on the terminal) to read an ID from it.
      railway_out=$(railway "$@")
      printf '%s\n' "$railway_out"
    else
      railway "$@"
    fi
  fi
}
capture=0
railway_out=""

# service_id is the ID Railway gave the service added last. `railway volume` needs the ID, not the name.
service_id=""

# volume_add NAME MOUNT: a volume on the service added last. The current CLI wants the service ID before `add`
# (`railway volume --service <ID> add`); an older one took `railway volume add --service <name>`, and is used when its help
# still lists that option. A dry run always prints the current form, so the plan does not depend on which CLI is installed.
volume_style=""
volume_add() {
  local name="$1" mount="$2" help
  if [ "$APPLY" = 1 ] && [ -z "$volume_style" ]; then
    help=$(railway volume add --help 2>&1 || true)
    case "$help" in *--service*) volume_style=old ;; *) volume_style=current ;; esac
  fi
  if [ "$volume_style" = old ]; then
    rail volume add --service "$name" --mount-path "$mount" --json
  else
    rail volume --service "${service_id:-<$name service id>}" add --mount-path "$mount" --json
  fi
}

# check_var NAME VALUE: refuses anything that would put a real value, or a smoke-test switch, in the template.
check_var() {
  local name="$1" value="$2"
  if printf '%s' "$name" | grep -Eq "$SMOKE_ONLY"; then
    echo "refusing smoke-only variable $name" >&2; exit 3
  fi
  if printf '%s' "$name" | grep -Eiq "(^|_)${SECRETISH}(_|$)"; then
    case "$value" in
      ""|'${{'*) ;;
      *) echo "refusing a literal value for $name: a secret in a template must be empty or a \${{...}} expression" >&2; exit 3 ;;
    esac
  fi
}

# service NAME IMAGE VAR=value... : adds one image-sourced service with its variables (railway add --variables takes one
# pair each). Every pair goes through check_var in this shell, so a refusal stops the script before anything is created.
service() {
  local name="$1" image="$2"; shift 2
  local args=() pair
  for pair in "$@"; do
    check_var "${pair%%=*}" "${pair#*=}"
    args+=(--variables "$pair")
  done
  capture=1
  rail add --service "$name" --image "$image" "${args[@]}" --json
  capture=0
  if [ "$APPLY" = 1 ]; then
    service_id=$(printf '%s\n' "$railway_out" | sed -n 's/.*"id" *: *"\([^"]*\)".*/\1/p' | head -n 1)
    [ -n "$service_id" ] || { echo "could not read the ID of service $name from the output of 'railway add --json'" >&2; exit 4; }
  else
    service_id=""
  fi
}

if [ "$APPLY" = 1 ]; then
  : "${RAILWAY_WORKSPACE:?set RAILWAY_WORKSPACE to the workspace id or name the project and the draft go in}"
  command -v railway >/dev/null || { echo "the railway CLI is not installed" >&2; exit 2; }
  railway whoami >/dev/null
  # The template names images, so they must exist: a draft that points at nothing would only fail on its first deploy.
  for image in "$CODEAF_IMAGE" "$AGENTFIELD_IMAGE" "$SWE_AF_IMAGE" "$PR_AF_IMAGE"; do
    docker buildx imagetools inspect "$image" >/dev/null 2>&1 || { echo "image $image is not published (or not readable): stopping before anything is created" >&2; exit 2; }
  done
  # `railway init` links the current directory to the new project; keep that out of any checkout.
  cd "$(mktemp -d)"
else
  echo "# DRY RUN: nothing is changed. Run with --apply (and RAILWAY_WORKSPACE) to make the draft."
fi

rail init --name "$PROJECT" --workspace "${RAILWAY_WORKSPACE:-<workspace>}" --json

# --- agentfield: the control plane. One volume, a health check, a generated API key. -----------------------------------------------
service agentfield "$AGENTFIELD_IMAGE" \
  "AGENTFIELD_API_KEY=$KEY64" \
  AGENTFIELD_HOME=/data \
  AGENTFIELD_CONFIG_FILE=/dev/null \
  AGENTFIELD_PORT=8080 \
  PORT=8080 \
  AGENTFIELD_STORAGE_MODE=local \
  AGENTFIELD_STORAGE_LOCAL_DATABASE_PATH=/data/agentfield.db \
  AGENTFIELD_STORAGE_LOCAL_KV_STORE_PATH=/data/agentfield.bolt \
  AGENTFIELD_RUN_OWNER_REQUIRE_PROOF=true \
  AGENTFIELD_TELEMETRY_ENABLED=false
volume_add agentfield /data
rail environment edit --service-config agentfield deploy.healthcheckPath /api/v1/health --message "agentfield health check"

# --- codeaf: the organization server, the one service with a public domain. Holds the template's two inputs. --------------------------
# CODEAF_TRUSTED_PROXIES is Railway's edge range: without it password sign-in over the public https name is refused as plain http.
service codeaf "$CODEAF_IMAGE" \
  CODEAF_ROLE=server \
  PORT=8787 \
  "CODEAF_ORG_NAME=codeaf organization" \
  'CODEAF_PUBLIC_DOMAIN=${{RAILWAY_PUBLIC_DOMAIN}}' \
  CODEAF_TRUSTED_PROXIES=100.64.0.0/10 \
  "CODEAF_SERVER_KEY=$KEY64" \
  "CODEAF_HOSTED_SECRET=$KEY64" \
  'CODEAF_HOSTED_MODEL_URL=http://${{RAILWAY_PRIVATE_DOMAIN}}:8787/v1/model' \
  'CODEAF_HOSTED_MODEL_KEY=${{OPENROUTER_API_KEY}}' \
  OPENROUTER_API_KEY= \
  GH_TOKEN=
volume_add codeaf /data
rail environment edit --service-config codeaf deploy.healthcheckPath /v1/about --message "codeaf health check"
rail domain --service codeaf --port 8787 --json

# --- sessionhost: hosted chats, with private networking and their own volume. ------------------------------------------------------
service sessionhost "$CODEAF_IMAGE" \
  CODEAF_ROLE=sessionhost \
  CODEAF_SESSIONHOST_DATA=/chats \
  'CODEAF_SESSIONHOST_SECRET=${{codeaf.CODEAF_HOSTED_SECRET}}' \
  "CODEAF_SESSIONHOST_MODEL=$MODEL" \
  CODEAF_SESSIONHOST_MAX_RUNNING=4 \
  CODEAF_SESSIONHOST_MAX_PER_PERSON=2
volume_add sessionhost /chats
rail environment edit --service-config sessionhost deploy.limitOverride.containers.cpu 1 \
  --service-config sessionhost deploy.limitOverride.containers.memoryBytes 1000000000 --message "sessionhost limits"

# Railway can resolve this reference once the sessionhost service exists.
hosted_url='http://${{sessionhost.RAILWAY_PRIVATE_DOMAIN}}:8790'
check_var CODEAF_HOSTED_URL "$hosted_url"
rail variable set "CODEAF_HOSTED_URL=$hosted_url" --service codeaf --skip-deploys

# --- relay: the organization's own relay, `codeaf relay` in the same image. Members' computers pair through it, see each other
# and open each other's chats live, so none of that needs the public relay. It needs a public domain of its own on its own port
# (its requests are signed with their path, so it cannot sit under the organization server's address), and Railway's edge in front
# of it, which is why it trusts X-Forwarded-For. Chat storage is off: the hosted chats are already kept by `codeaf`. It keeps the
# device list, in its volume. /p/health is the page the relay serves for any pairing code, so it answers 200 and says nothing about
# who is connected; GET /status stays off for the same reason (the relay is open to the internet).
service relay "$CODEAF_IMAGE" \
  CODEAF_ROLE=relay \
  PORT=8788 \
  CODEAF_RELAY_STORAGE=off \
  CODEAF_RELAY_STATUS=off \
  CODEAF_RELAY_TRUST_PROXY=on
volume_add relay /data
rail environment edit --service-config relay deploy.healthcheckPath /p/health --message "relay health check"
rail domain --service relay --port 8788 --json

# The address the organization server tells its people to pair through. Railway can resolve this reference once the relay has its
# domain, and it is only an address, never a secret.
relay_url='https://${{relay.RAILWAY_PUBLIC_DOMAIN}}'
check_var CODEAF_ORG_RELAY_URL "$relay_url"
rail variable set "CODEAF_ORG_RELAY_URL=$relay_url" --service codeaf --skip-deploys

# --- runner: the factory runner, the same image in its runner role. --------------------------------------------------------------------
service runner "$CODEAF_IMAGE" \
  CODEAF_ROLE=runner \
  'AGENTFIELD_URL=http://${{agentfield.RAILWAY_PRIVATE_DOMAIN}}:8080' \
  'AGENTFIELD_API_KEY=${{agentfield.AGENTFIELD_API_KEY}}' \
  "ORG_MODEL=$MODEL" \
  'OPENROUTER_API_KEY=${{codeaf.OPENROUTER_API_KEY}}' \
  'GH_TOKEN=${{codeaf.GH_TOKEN}}'
volume_add runner /data

# --- the reasoner nodes ----------------------------------------------------------------------------------------------------------------------
service swe-af "$SWE_AF_IMAGE" \
  NODE_ID=swe-planner \
  'AGENTFIELD_SERVER=http://${{agentfield.RAILWAY_PRIVATE_DOMAIN}}:8080' \
  'AGENTFIELD_API_KEY=${{agentfield.AGENTFIELD_API_KEY}}' \
  SWE_HITL_NATIVE=1 \
  SWE_FENCE=off \
  SWE_PRO_ENGINE=0 \
  SWE_DEFAULT_MODEL=openrouter/deepseek/deepseek-v4-flash-0731 \
  'OPENROUTER_API_KEY=${{codeaf.OPENROUTER_API_KEY}}' \
  'GH_TOKEN=${{codeaf.GH_TOKEN}}'
volume_add swe-af /workspaces

service pr-af "$PR_AF_IMAGE" \
  NODE_ID=pr-af \
  PORT=8007 \
  'AGENTFIELD_SERVER=http://${{agentfield.RAILWAY_PRIVATE_DOMAIN}}:8080' \
  'AGENTFIELD_API_KEY=${{agentfield.AGENTFIELD_API_KEY}}' \
  'AGENT_CALLBACK_URL=http://${{RAILWAY_PRIVATE_DOMAIN}}:8007' \
  PR_AF_MODEL=deepseek/deepseek-v4-flash-0731 \
  'OPENROUTER_API_KEY=${{codeaf.OPENROUTER_API_KEY}}' \
  'GH_TOKEN=${{codeaf.GH_TOKEN}}'
volume_add pr-af /workspaces

# --- the draft. `templates create` clones the project into an UNPUBLISHED template; there is no publish call here. -----------------
rail templates create --json

if [ "$APPLY" = 1 ]; then
  cat <<NOTE

The draft template is in your workspace, unpublished. Open it in the dashboard's template editor and mark OPENROUTER_API_KEY and
GH_TOKEN (on the codeaf service) as inputs people fill in. The source project "$PROJECT" only fed the draft: delete it.
Never press Publish unless you mean to put this in the marketplace.

Every volume came out at Railway's default size, 50 GB, because the CLI has no size option: resize them before first use
(deploy/org/README.md, "Railway", Volumes).
NOTE
else
  echo "# $calls calls. Variables checked: no literal secret, no smoke-only switch. Nothing was run."
fi
