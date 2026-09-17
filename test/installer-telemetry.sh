#!/usr/bin/env bash
# THE INSTALLER'S TELEMETRY DUTIES, PROVED WITHOUT A NETWORK.
#
# The notice text lives once, byte for byte, in docs/TELEMETRY.md and is
# quoted in three places — the binary, this installer and the README — and
# only a test notices when one of them drifts. The installer's main body
# downloads a release, so this test never sources it whole: it lifts out the
# three telemetry functions and runs them against a temporary state root.
# Nothing here opens a socket.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

script=scripts/install.sh
doc=docs/TELEMETRY.md
test -f "$doc" || { echo "docs/TELEMETRY.md is missing; this test reads the notice from it"; exit 1; }

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

# ok() evals these assertion strings, so they stay single-quoted and resolve
# every variable at run time; nothing below expands its quotes early.
# shellcheck disable=SC2016,SC2034 # ok() evals these strings; quoting is deliberate
{

# --- install.json -----------------------------------------------------------

CHANNEL=stable write_install_marker "$tmp/state"
f="$tmp/state/telemetry/install.json"
ok "marker exists" '[ -f "$f" ]'
mode_of() { stat -f '%Lp' "$1" 2>/dev/null || stat -c '%a' "$1"; }
json_valid() {
	if command -v python3 >/dev/null 2>&1; then
		python3 -m json.tool "$f" >/dev/null 2>&1
	elif command -v jq >/dev/null 2>&1; then
		jq -e . "$f" >/dev/null 2>&1
	else
		grep -Eq '"channel":"(stable|rc|staging|dev|unknown)"' "$f"
	fi
}
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
# The channel lands in JSON verbatim and CHANNEL can come from the environment:
# a known channel is recorded as-is, anything else becomes "unknown", and the
# marker parses either way.
ok "valid channel recorded" 'grep -q "\"channel\":\"rc\"" "$f"'
ok "valid channel marker is valid JSON" 'json_valid'
CHANNEL='we"ird' write_install_marker "$tmp/state"
ok "unexpected channel written as unknown" 'grep -q "\"channel\":\"unknown\"" "$f"'
ok "unexpected-channel marker is valid JSON" 'json_valid'
ok "unwritable state root does not fail the install" 'write_install_marker /proc/nonexistent-root'

# --- the notice -------------------------------------------------------------

notice="$tmp/notice.txt"
print_telemetry_notice 2> "$notice"
expected=$(awk '
	/^## The notice$/ {f=1; next}
	f && /^```$/ {f++; next}
	f == 2 {print}
' "$doc")
body=$(sed 1d "$notice")
ok "one blank line before the notice" '[ -z "$(head -n 1 "$notice")" ]'
ok "notice matches docs/TELEMETRY.md verbatim" '[ "$body" = "$expected" ]'
readme_block=$(awk '
	/^```text$/ {f = 1; buf = ""; next}
	/^```$/     {if (f && buf ~ /codeaf sends anonymous usage counts/) {print buf; exit} f = 0; next}
	f           {buf = buf $0 "\n"}
' README.md)
ok "README quotes the notice verbatim" '[ -n "$readme_block" ] && [ "$(printf "%s\n" "$expected")" = "$readme_block" ]'
ok "notice goes to stderr, nothing to stdout" '[ -z "$(print_telemetry_notice 2>/dev/null)" ]'

for v in off 0 false OFF False; do
	export CODEAF_TELEMETRY="$v"
	out=$( print_telemetry_notice 2>&1 )
	ok "CODEAF_TELEMETRY=$v opts out" 'case "$out" in *"off"*) true;; *) false;; esac'
	ok "opt-out prints no notice body" 'case "$out" in *"anonymous usage counts to AgentField"*) false;; *) true;; esac'
	unset CODEAF_TELEMETRY
done
for v in 1 true TRUE; do
	export DO_NOT_TRACK="$v"
	out=$( print_telemetry_notice 2>&1 )
	ok "DO_NOT_TRACK=$v opts out" 'case "$out" in *"off"*) true;; *) false;; esac'
	unset DO_NOT_TRACK
done
out=$( print_telemetry_notice 2>&1 )
ok "unset prints the notice" 'case "$out" in *"anonymous usage counts to AgentField"*) true;; *) false;; esac'
export CODEAF_TELEMETRY=1
out=$( print_telemetry_notice 2>&1 )
ok "CODEAF_TELEMETRY=1 prints the notice" 'case "$out" in *"anonymous usage counts to AgentField"*) true;; *) false;; esac'
unset CODEAF_TELEMETRY

# --- nothing new on the wire --------------------------------------------------

ok "no telemetry request anywhere in the installer" '! grep -qE "agentfield\.ai/api|POST" "$script"'
}

printf 'installer telemetry: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
