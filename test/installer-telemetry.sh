#!/usr/bin/env bash
# THE INSTALLER'S TELEMETRY DUTIES, PROVED WITHOUT A NETWORK.
#
# The wire contract (.telemetry-contract.md) is one text in three places — the
# binary, this installer and the README — and only a test notices when one of
# them drifts. The installer's main body downloads a release, so this test
# never sources it whole: it lifts out the three telemetry functions and runs
# them against a temporary state root. Nothing here opens a socket.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

script=scripts/install.sh
contract=.telemetry-contract.md
test -f "$contract" || { echo "telemetry contract is missing; this test reads it"; exit 1; }

eval "$(awk '/^TELEMETRY_NOTICE=/{f=1} /^VERBOSE=/{f=0} f' "$script")"
eval "$(sed -n '/^telemetry_off()/,/^}/p; /^write_install_marker()/,/^}/p; /^print_telemetry_notice()/,/^}/p' "$script")"
type telemetry_off >/dev/null
type write_install_marker >/dev/null
type print_telemetry_notice >/dev/null

pass=0
fail=0
ok() {
	if eval "$2"; then
		pass=$((pass + 1))
	else
		fail=$((fail + 1))
		printf 'FAIL %s\n' "$1"
	fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
unset CODEAF_TELEMETRY DO_NOT_TRACK || true

# --- install.json -----------------------------------------------------------

CHANNEL=stable write_install_marker "$tmp/state"
f="$tmp/state/telemetry/install.json"
ok "marker exists" '[ -f "$f" ]'
mode_of() { stat -f '%Lp' "$1" 2>/dev/null || stat -c '%a' "$1"; }
ok "telemetry dir is 0700" '[ "$(mode_of "$tmp/state/telemetry")" = 700 ]'
ok "install.json is 0600" '[ "$(mode_of "$f")" = 600 ]'
ok "install_method is script" 'grep -q "\"install_method\":\"script\"" "$f"'
ok "channel recorded" 'grep -q "\"channel\":\"stable\"" "$f"'
ok "installed_at is UTC RFC3339" 'grep -Eq "\"installed_at\":\"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z\"" "$f"'
ok "exactly the three contract keys" '[ "$(tr "," "\n" < "$f" | grep -c "\":")" = 3 ]'
CHANNEL=dev write_install_marker "$tmp/state"
ok "marker follows the channel" 'grep -q "\"channel\":\"dev\"" "$f"'
ok "installer creates no install_id" '[ ! -e "$tmp/state/telemetry/install_id" ]'
: > "$tmp/state/telemetry/install_id"
CHANNEL=rc write_install_marker "$tmp/state"
ok "existing install_id left untouched" '[ ! -s "$tmp/state/telemetry/install_id" ]'
ok "unwritable state root does not fail the install" 'write_install_marker /proc/nonexistent-root'

# --- the notice -------------------------------------------------------------

notice="$tmp/notice.txt"
print_telemetry_notice 2> "$notice"
expected=$(awk '/^Notice, printed once/{f=1;next} f' "$contract")
body=$(sed 1d "$notice")
ok "one blank line before the notice" '[ -z "$(head -n 1 "$notice")" ]'
ok "notice matches the contract verbatim" '[ "$body" = "$expected" ]'
ok "notice goes to stderr, nothing to stdout" '[ -z "$(print_telemetry_notice 2>/dev/null)" ]'

for v in off 0 false OFF False; do
	out=$( ( CODEAF_TELEMETRY="$v"; print_telemetry_notice ) 2>&1 )
	ok "CODEAF_TELEMETRY=$v opts out" 'case "$out" in *"off"*) true;; *) false;; esac'
	ok "opt-out prints no notice body" 'case "$out" in *"anonymous usage counts to AgentField"*) false;; *) true;; esac'
done
for v in 1 true TRUE; do
	out=$( ( DO_NOT_TRACK="$v"; print_telemetry_notice ) 2>&1 )
	ok "DO_NOT_TRACK=$v opts out" 'case "$out" in *"off"*) true;; *) false;; esac'
done
out=$( print_telemetry_notice 2>&1 )
ok "unset prints the notice" 'case "$out" in *"anonymous usage counts to AgentField"*) true;; *) false;; esac'
out=$( ( CODEAF_TELEMETRY=1; print_telemetry_notice ) 2>&1 )
ok "CODEAF_TELEMETRY=1 prints the notice" 'case "$out" in *"anonymous usage counts to AgentField"*) true;; *) false;; esac'

# --- nothing new on the wire --------------------------------------------------

ok "no telemetry request anywhere in the installer" '! grep -qE "agentfield\.ai/api|POST" "$script"'

printf 'installer telemetry: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
