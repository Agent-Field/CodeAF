#!/usr/bin/env bash
# THE INSTALLER'S TELEMETRY DUTIES, PROVED WITHOUT A NETWORK.
#
# The installer writes the local install marker and prints no telemetry notice:
# the binary shows the notice, quoted byte for byte in docs/TELEMETRY.md and the
# README, before the first session's events are sent. The installer's main body
# downloads a release, so this test never sources it whole: it lifts out the
# marker and PATH functions and runs them against a temporary state root.
# Nothing here opens a socket.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

script=scripts/install.sh
doc=docs/TELEMETRY.md
test -f "$doc" || { echo "docs/TELEMETRY.md is missing; this test reads the notice from it"; exit 1; }

eval "$(sed -n '/^write_install_marker()/,/^}/p; /^print_path_hint()/,/^}/p' "$script")"
type write_install_marker >/dev/null
type print_path_hint >/dev/null

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
# GNU stat first: on Linux `stat -f` succeeds too, but reports the filesystem.
mode_of() { stat -c '%a' "$1" 2>/dev/null || stat -f '%Lp' "$1"; }
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
ok "an unwritable state root says nothing" '[ -z "$(write_install_marker /proc/nonexistent-root 2>&1)" ]'

# --- no notice from the installer --------------------------------------------

# The binary shows the notice at the first session; the installer shows none.
ok "installer prints no telemetry notice" '! grep -qE "anonymous (performance data|usage counts)|TELEMETRY_NOTICE|print_telemetry_notice" "$script"'
ok "no printed line in the installer mentions telemetry" '! grep -E "printf|echo" "$script" | grep -qi telemetry'
expected=$(awk '
	/^## The notice$/ {f=1; next}
	f && /^```$/ {f++; next}
	f == 2 {print}
' "$doc")
readme_block=$(awk '
	/^```text$/ {f = 1; buf = ""; next}
	/^```$/     {if (f && buf ~ /codeaf sends anonymous usage counts/) {print buf; exit} f = 0; next}
	f           {buf = buf $0 "\n"}
' README.md)
ok "README quotes the notice verbatim" '[ -n "$readme_block" ] && [ "$(printf "%s\n" "$expected")" = "$readme_block" ]'

# --- the PATH line comes last -------------------------------------------------
# The line a person has to paste is the installer's final word: bare, after a
# blank line, and never prefixed with "codeaf: add it to this shell with:",
# which made it a sentence to trim rather than a line to select.

hint='export PATH="/x/bin:$PATH"'
has_escape() { printf '%s' "$1" | grep -q "$(printf '\033')"; }
ok "an empty hint prints nothing" '[ -z "$(print_path_hint "" 2>&1)" ]'
out=$(print_path_hint "$hint"; printf x); out=${out%x}
expected_hint=$(printf '\n%s\n\nx' "$hint"); expected_hint=${expected_hint%x}
ok "the hint is one blank line, the bare export, one blank line" '[ "$out" = "$expected_hint" ]'
ok "channel and path announcements go to stderr, verbose only" '[ -z "$(grep -E "printf .codeaf: (installed|%s %s for|%s for)" "$script" | grep -v ">&2")" ]'
ok "the receipt is the installed binary naming itself" 'grep -q "printf .installed %s" "$script"'
ok "no colour when stdout is not a terminal" '! has_escape "$out"'
export NO_COLOR=1
out=$(print_path_hint "$hint" 2>&1)
ok "NO_COLOR is respected" '! has_escape "$out"'
unset NO_COLOR
last_line=$(grep -v '^[[:space:]]*#' "$script" | grep -v '^[[:space:]]*$' | tail -n 1)
ok "the hint is the installer's last line" '[ "$last_line" = "print_path_hint \"\$PATH_HINT\"" ]'
ok "the old prefixed sentence is gone" '! grep -q "add it to this shell with" "$script"'

# --- nothing new on the wire --------------------------------------------------

ok "no telemetry request anywhere in the installer" '! grep -qE "agentfield\.ai/api|POST" "$script"'
}

printf 'installer telemetry: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
