#!/usr/bin/env bash
# Exercises the Railway scripts offline from the repository root or any other directory.
set -eu
cd "$(dirname "$0")/../../.."
root=$PWD
template=$root/deploy/org/railway/make-template.sh
operator=$root/deploy/org/railway/operator-link.sh
upload=$root/deploy/org/gate/upload-check.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
unset ORG_REGISTRY ORG_TAG TEMPLATE_PROJECT RAILWAY_PROJECT RAILWAY_ENVIRONMENT RAILWAY_SERVICE OPERATOR_LINK_LINES FAKE_LOG_MODE

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

plan=$work/plan
dry_run() {
  local calls
  "$template" >"$plan" && calls=$(grep -c '^+ railway ' "$plan") &&
    tail -n 1 "$plan" | grep -qE "^# $calls calls\. .*Nothing was run\.$"
}
row 'make-template 1: dry run reports its planned call count' dry_run

mkdir "$work/no-railway"
for tool in bash grep sed; do
  ln -s "$(command -v "$tool")" "$work/no-railway/$tool"
done
without_cli() {
  PATH="$work/no-railway" "$template" >"$work/no-railway-result" && cmp -s "$plan" "$work/no-railway-result"
}
row 'make-template 1: dry run works without the Railway CLI' without_cli

codeaf=$(grep -F '+ railway add --service codeaf ' "$plan" || true)
sessionhost=$(grep -F '+ railway add --service sessionhost ' "$plan" || true)
same_image() {
  [[ $codeaf == *'--image agentfield/codeaf-org:dev '* && $sessionhost == *'--image agentfield/codeaf-org:dev '* ]]
}
row 'make-template 2: sessionhost uses the codeaf image' same_image

sessionhost_variables() {
  local pair
  for pair in 'CODEAF_ROLE=sessionhost' 'CODEAF_SESSIONHOST_DATA=/chats' \
    "'CODEAF_SESSIONHOST_SECRET=\${{codeaf.CODEAF_HOSTED_SECRET}}'" \
    'CODEAF_SESSIONHOST_MODEL=deepseek/deepseek-v4-flash-0731' \
    'CODEAF_SESSIONHOST_MAX_RUNNING=4' 'CODEAF_SESSIONHOST_MAX_PER_PERSON=2'; do
    [[ $sessionhost == *"--variables $pair "* ]] || return 1
  done
}
row 'make-template 2: sessionhost gets its role, data, shared secret, model and limits' sessionhost_variables
row 'make-template 2: sessionhost mounts /chats' \
  grep -qxF "+ railway volume --service '<sessionhost service id>' add --mount-path /chats --json" "$plan"
private_sessionhost() {
  [ -n "$sessionhost" ] && ! grep -Eq '^\+ railway domain .*--service sessionhost( |$)' "$plan"
}
row 'make-template 2: sessionhost has no public domain' private_sessionhost
row 'make-template 2: sessionhost is limited to 1 vCPU and 1 GB' \
  grep -qE '^\+ railway environment edit --service-config sessionhost deploy\.limitOverride\.containers\.cpu 1 --service-config sessionhost deploy\.limitOverride\.containers\.memoryBytes 1000000000( |$)' "$plan"

hosted_variables() {
  [[ $codeaf == *"--variables 'CODEAF_HOSTED_SECRET=\${{secret(64,"* &&
     $codeaf == *"--variables 'CODEAF_HOSTED_MODEL_URL=http://\${{RAILWAY_PRIVATE_DOMAIN}}:8787/v1/model' "* &&
     $codeaf == *"--variables 'CODEAF_HOSTED_MODEL_KEY=\${{OPENROUTER_API_KEY}}' "* ]]
}
row 'make-template 3: codeaf gets a generated secret and the model endpoint and input reference' hosted_variables
hosted_url_after_service() {
  local line
  line=$(grep -nFx "+ railway variable set 'CODEAF_HOSTED_URL=http://\${{sessionhost.RAILWAY_PRIVATE_DOMAIN}}:8790' --service codeaf --skip-deploys" "$plan") &&
    grep -qF '+ railway add --service sessionhost ' < <(head -n "$(( ${line%%:*} - 1 ))" "$plan") &&
    [[ $codeaf != *CODEAF_HOSTED_URL=* ]]
}
row 'make-template 3: codeaf gets the hosted URL after sessionhost exists' hosted_url_after_service
row 'make-template 4: codeaf health check uses /v1/about' \
  grep -qxF "+ railway environment edit --service-config codeaf deploy.healthcheckPath /v1/about --message 'codeaf health check'" "$plan"
# The organization's relay: a seventh service, the same image in its relay role, with a public domain of its own, and the address
# the organization server tells its people.
relay=$(grep -F '+ railway add --service relay ' "$plan" || true)
relay_variables() {
  local pair
  [[ $relay == *'--image agentfield/codeaf-org:dev '* ]] || return 1
  for pair in 'CODEAF_ROLE=relay' 'PORT=8788' 'CODEAF_RELAY_STORAGE=off' 'CODEAF_RELAY_STATUS=off' 'CODEAF_RELAY_TRUST_PROXY=on'; do
    [[ $relay == *"--variables $pair "* ]] || return 1
  done
}
row 'make-template 7: relay is the codeaf image in its relay role, storage and status off, trusting the proxy' relay_variables
row 'make-template 7: relay keeps its device list on a /data volume' \
  grep -qxF "+ railway volume --service '<relay service id>' add --mount-path /data --json" "$plan"
row 'make-template 7: relay health check is /p/health, which names nobody' \
  grep -qxF "+ railway environment edit --service-config relay deploy.healthcheckPath /p/health --message 'relay health check'" "$plan"
row 'make-template 7: relay has a public domain on its own port' \
  grep -qxF '+ railway domain --service relay --port 8788 --json' "$plan"
org_relay_url_after_domain() {
  local line
  line=$(grep -nFx "+ railway variable set 'CODEAF_ORG_RELAY_URL=https://\${{relay.RAILWAY_PUBLIC_DOMAIN}}' --service codeaf --skip-deploys" "$plan") &&
    grep -qxF '+ railway domain --service relay --port 8788 --json' < <(head -n "$(( ${line%%:*} - 1 ))" "$plan") &&
    [[ $codeaf != *CODEAF_ORG_RELAY_URL=* ]]
}
row 'make-template 7: codeaf is told the relay address after the relay has its domain' org_relay_url_after_domain
template_excludes_smoke_switches() {
  ! grep -Eq -- '--variables (RUNNER_SELFTEST|SMOKE_FIXTURE_REPO|SWE_SMOKE_FIXTURE_REPO|CODEAF_BASE_URL|STUB_DELAY|STUB_COST_USD|GITSTUB_TOKEN)=' "$plan"
}
row 'make-template 5: no smoke-only switches enter the plan' template_excludes_smoke_switches

# --apply against a stub of the Railway CLI (5.7x) that records every call's arguments, one line per call with each argument
# in brackets, and refuses what that CLI refuses: `volume add --service` is an unknown option, `volume --service <name>` panics
# (only an ID works), and `environment edit` without --message stops at "Apply changes now? (Y/n)", where a terminal run waits
# forever. FAKE_RAILWAY_STYLE=old makes `volume add` take --service itself, as an older CLI did; FAKE_RAILWAY_NO_ID=1 makes
# `add` print no ID.
mkdir "$work/apply-bin"
cat >"$work/apply-bin/railway" <<'STUB'
#!/usr/bin/env bash
set -u
line=""
for a in "$@"; do line+="[$a]"; done
printf '%s\n' "$line" >>"$FAKE_RAILWAY_LOG"
case "${1:-}" in
  whoami) exit 0 ;;
  init) echo '{"id":"project-id","name":"stub"}' ;;
  add)
    name="" prev=""
    for a in "$@"; do [ "$prev" = --service ] && name=$a; prev=$a; done
    echo '> What do you need? Docker Image' >&2
    if [ "${FAKE_RAILWAY_NO_ID:-}" = 1 ]; then echo '{}'; else printf '{"id":"id-%s","name":"%s"}\n' "$name" "$name"; fi ;;
  volume)
    shift
    parent=""
    if [ "${1:-}" = --service ]; then parent=${2:-}; shift 2; fi
    [ "${1:-}" = add ] || exit 64
    shift
    for a in "$@"; do
      if [ "$a" = --help ]; then
        if [ "${FAKE_RAILWAY_STYLE:-current}" = old ]; then echo '  -s, --service <SERVICE>  Service'; fi
        echo '  -m, --mount-path <MOUNT_PATH>  The mount path of the volume'
        exit 0
      fi
    done
    if [ "${FAKE_RAILWAY_STYLE:-current}" != old ]; then
      for a in "$@"; do
        [ "$a" = --service ] && { echo "error: unexpected argument '--service' found" >&2; exit 2; }
      done
      case "$parent" in id-*) ;; *) echo "thread 'main' panicked: called Option::unwrap() on a None value" >&2; exit 101 ;; esac
    fi
    echo '{"id":"volume"}' ;;
  environment)
    shift
    [ "${1:-}" = edit ] || exit 64
    shift
    seen=0
    for a in "$@"; do if [ "$a" = --message ] || [ "$a" = -m ]; then seen=1; fi; done
    [ "$seen" = 1 ] || { echo '? Apply changes now? (Y/n)' >&2; exit 99; } ;;
  domain) echo '{"domain":"https://stub.example"}' ;;
  *) echo '{}' ;;
esac
STUB
cat >"$work/apply-bin/docker" <<'STUB'
#!/bin/sh
exit 0
STUB
chmod +x "$work/apply-bin/railway" "$work/apply-bin/docker"
mkdir "$work/tmp"

apply_with() {
  FAKE_RAILWAY_LOG=$work/calls
  : >"$FAKE_RAILWAY_LOG"
  status=0
  env "$@" FAKE_RAILWAY_LOG="$FAKE_RAILWAY_LOG" TMPDIR="$work/tmp" RAILWAY_WORKSPACE=stub-workspace \
    PATH="$work/apply-bin:$PATH" "$template" --apply >"$work/apply-out" 2>"$work/apply-err" || status=$?
}
apply_ok() { apply_with FAKE_RAILWAY_STYLE=current; [ "$status" = 0 ] && ! grep -q "ID of service" "$work/apply-err"; }
row 'make-template 6: --apply runs to the end against the current CLI' apply_ok

volumes_by_id() {
  apply_ok || return 1
  local want
  for want in agentfield:/data codeaf:/data sessionhost:/chats relay:/data runner:/data swe-af:/workspaces pr-af:/workspaces; do
    local name=${want%%:*} mount=${want#*:} add vol
    add=$(grep -nF "[add][--service][$name][--image]" "$FAKE_RAILWAY_LOG" | head -n 1 | cut -d: -f1) &&
      vol=$(grep -nxF "[volume][--service][id-$name][add][--mount-path][$mount][--json]" "$FAKE_RAILWAY_LOG" | head -n 1 | cut -d: -f1) &&
      [ "$add" -lt "$vol" ] || return 1
  done
  [ "$(grep '^\[volume\]' "$FAKE_RAILWAY_LOG" | grep -vc '\[--help\]')" = 7 ]
}
row 'make-template 6: each volume is added with its service ID, after the service exists, and never as volume add --service' volumes_by_id

limits_have_a_message() {
  apply_ok &&
    grep -qxF '[environment][edit][--service-config][sessionhost][deploy.limitOverride.containers.cpu][1][--service-config][sessionhost][deploy.limitOverride.containers.memoryBytes][1000000000][--message][sessionhost limits]' "$FAKE_RAILWAY_LOG"
}
row 'make-template 6: the sessionhost limits edit carries --message, so a terminal run does not wait for a key' limits_have_a_message

every_edit_has_a_message() {
  apply_ok || return 1
  local edits messages
  edits=$(grep -c '^\[environment\]\[edit\]' "$FAKE_RAILWAY_LOG")
  messages=$(grep '^\[environment\]\[edit\]' "$FAKE_RAILWAY_LOG" | grep -c '\[--message\]')
  [ "$edits" = 4 ] && [ "$messages" = 4 ]
}
row 'make-template 6: all four environment edits carry --message' every_edit_has_a_message

older_cli_keeps_working() {
  apply_with FAKE_RAILWAY_STYLE=old
  [ "$status" = 0 ] &&
    [ "$(grep -c '^\[volume\]\[add\]\[--service\]' "$FAKE_RAILWAY_LOG")" = 7 ] &&
    grep -qxF '[volume][add][--service][sessionhost][--mount-path][/chats][--json]' "$FAKE_RAILWAY_LOG" &&
    ! grep -q '^\[volume\]\[--service\]' "$FAKE_RAILWAY_LOG"
}
row 'make-template 6: an older CLI whose volume add takes --service still gets that form' older_cli_keeps_working

stops_without_an_id() {
  apply_with FAKE_RAILWAY_NO_ID=1
  [ "$status" = 4 ] && grep -qF 'could not read the ID of service agentfield' "$work/apply-err" &&
    ! grep -q '^\[volume\]' "$FAKE_RAILWAY_LOG"
}
row 'make-template 6: an add that prints no service ID stops the script before any volume' stops_without_an_id

mkdir "$work/bin"
cat >"$work/bin/railway" <<'FAKE'
#!/bin/sh
set -eu
[ "$(printf '%s\n' "$@")" = "$FAKE_EXPECT_ARGS" ] || exit 90
if IFS= read -r input; then exit 91; fi
printf '%s\n' 'decoy codeaf://factory/join?operator=false&token=decoy'
case "${FAKE_LOG_MODE:-links}" in
  empty) exit 0 ;;
  error)
    printf '%s\n' 'operator access link: codeaf://factory/join?operator=true&token=partial'
    printf '%s\n' 'operator access link: codeaf://factory/join?operator=true&token=failed' >&2
    exit 42 ;;
esac
printf '%s\n' 'operator access link: codeaf://factory/join?operator=true&token=older'
printf '%s\r\n' 'operator access link: codeaf://factory/join?operator=true&token=latest'
FAKE
chmod +x "$work/bin/railway"
export FAKE_EXPECT_ARGS
FAKE_EXPECT_ARGS=$(printf '%s\n' logs --service codeaf --lines 300)
printf '%s\n' 'codeaf://factory/join?operator=true&token=latest' >"$work/expected-link"

run_operator() {
  status=0
  PATH="$work/bin:$PATH" "$operator" >"$work/link" 2>"$work/operator-errors" <<< 'caller input' || status=$?
}
latest_link() {
  run_operator
  [ "$status" = 0 ] && cmp -s "$work/expected-link" "$work/link" && [ ! -s "$work/operator-errors" ]
}
row 'operator-link 1: prints exactly the last link, with the carriage return removed' latest_link

explicit_context() (
  export RAILWAY_PROJECT='org fixture' RAILWAY_ENVIRONMENT='test environment' RAILWAY_SERVICE='org server' OPERATOR_LINK_LINES=700
  FAKE_EXPECT_ARGS=$(printf '%s\n' logs --service "$RAILWAY_SERVICE" --lines "$OPERATOR_LINK_LINES" \
    --project "$RAILWAY_PROJECT" --environment "$RAILWAY_ENVIRONMENT")
  latest_link
)
row 'operator-link 1: accepts project, environment, service and log tail overrides' explicit_context

missing_link() {
  FAKE_LOG_MODE=empty run_operator
  [ "$status" = 1 ] && [ ! -s "$work/link" ] && grep -qF OPERATOR_LINK_LINES "$work/operator-errors"
}
row 'operator-link 2: missing link exits 1 and suggests a longer tail' missing_link

missing_cli() {
  status=0
  PATH="$work/no-railway" "$operator" >"$work/link" 2>"$work/operator-errors" || status=$?
  [ "$status" = 2 ] && [ ! -s "$work/link" ] && grep -qi 'railway.*not installed' "$work/operator-errors"
}
row 'operator-link 3: missing Railway CLI exits 2 with a diagnostic' missing_cli

failed_logs() {
  FAKE_LOG_MODE=error run_operator
  [ "$status" = 2 ] && [ ! -s "$work/link" ] && grep -qi 'railway logs.*failed' "$work/operator-errors" &&
    ! grep -qF 'codeaf://factory/join?' "$work/operator-errors"
}
row 'operator-link 3: failed Railway logs exits 2 without leaking its output' failed_logs

clean_checkout() {
  "$upload" >"$work/upload-clean" 2>&1 &&
    grep -qxE 'upload-check: [0-9]+ tracked symlinks, none dangling' "$work/upload-clean"
}
row 'upload-check 3: the checkout has no dangling tracked symlinks' clean_checkout

fixture=$work/repo
git init -q "$fixture"
mkdir "$fixture/links"
touch "$fixture/present"
ln -s ../present "$fixture/links/good"
dangling='links/dangling café'
ln -s ../missing "$fixture/$dangling"
git -C "$fixture" update-index --add -- present links/good "$dangling"
only_dangling() (
  cd "$fixture" || exit 1
  status=0
  "$upload" >"$work/upload-fixture" 2>&1 || status=$?
  [ "$status" = 1 ] && [ "$(wc -l <"$work/upload-fixture")" -eq 1 ] &&
    grep -F "$dangling" "$work/upload-fixture" | grep -qF '../missing'
)
row 'upload-check 3: reports only the dangling link and its target' only_dangling

exit "$failed"
