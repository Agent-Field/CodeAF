#!/usr/bin/env bash
# Exercises operator-link.sh offline, against a stand-in for `docker compose` that answers from files.
set -eu
cd "$(dirname "$0")"
script=$PWD/operator-link.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
unset COMPOSE FAKE_LOG FAKE_STATE FAKE_EXEC_FAILS

# The stand-in: `logs` prints the log file; `exec -T codeaf curl ...` keeps what came in on stdin and answers
# with a state, or fails the way a container that is down does.
cat >"$work/dc" <<'FAKE'
#!/usr/bin/env bash
case "$1" in
  logs) cat "$FAKE_LOG" ;;
  exec)
    cat >"$FAKE_STDIN"
    [ -z "${FAKE_EXEC_FAILS:-}" ] || exit 1
    printf '{"state":"%s","factory":{"name":"x"}}\n' "$FAKE_STATE" ;;
  *) exit 64 ;;
esac
FAKE
chmod +x "$work/dc"
export COMPOSE=$work/dc FAKE_LOG=$work/log FAKE_STDIN=$work/stdin

old='codeaf://factory/join?operator=true&url=http%3A%2F%2F%5B%3A%3A%5D%3A8787#token=aaaa'
new='codeaf://factory/join?operator=true&url=https%3A%2F%2Forg.example.com#token=bbbb'
printf 'starting\noperator access link: %s\r\nserving\noperator access link: %s\r\n' "$old" "$new" >"$work/log"

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
run() {
  status=0
  "$script" >"$work/out" 2>"$work/err" </dev/null || status=$?
}

unspent() {
  FAKE_STATE=ready run
  [ "$status" = 0 ] && [ "$(cat "$work/out")" = "$new" ] && [ ! -s "$work/err" ]
}
row 'an unused link is printed, the newest one in the log, without its carriage return' unspent

asked_about_the_one_it_prints() {
  FAKE_STATE=ready run
  grep -qF "\"link\":\"$new\"" "$work/stdin" && grep -qF '"verb":"check"' "$work/stdin"
}
row 'the server is asked to check, not to claim, the link it is about to print' asked_about_the_one_it_prints

refused() {
  FAKE_STATE=$1 run
  [ "$status" = 1 ] && [ ! -s "$work/out" ] && ! grep -qF "$new" "$work/err" && ! grep -qF bbbb "$work/err" && grep -qi 'no longer good' "$work/err"
}
for state in used_up expired revoked no_access; do
  row "a $state link is reported, never printed" refused "$state"
done

unreachable() {
  FAKE_EXEC_FAILS=1 FAKE_STATE=ready run
  [ "$status" = 0 ] && [ "$(cat "$work/out")" = "$new" ]
}
row 'a server that cannot be asked leaves the log as the answer' unreachable

no_link() {
  printf 'starting\nserving\n' >"$work/empty-log"
  FAKE_LOG=$work/empty-log FAKE_STATE=ready run
  [ "$status" = 1 ] && [ ! -s "$work/out" ] && grep -qi 'No operator link' "$work/err"
}
row 'a log with no link says so and exits 1' no_link

exit "$failed"
