#!/bin/bash
# durability-hosted.sh proves that nothing a chat completed is lost when the
# machine holding it dies, against a hosted relay: the real binary is the
# holding machine and runs a scripted model, so every run makes the same calls
# and knows to the millisecond when each finished; the other machine then takes
# the chat over and the run counts what arrived.
#
#   scripts/durability-hosted.sh [name of one test, for example KillAfterLoneCall]
#
# It needs tmux, a built bin/codeaf (`make build`) and the network. It makes NO
# model calls (the model is a script on loopback) and costs the relay a few
# hundred requests. The relay is $CODEAF_RELAY and has no default: unset, the script stops.
# Every run makes a fresh identity of its own, so it never touches another
# run's chats on the relay.
#
# Each scenario prints one line that begins DURABILITY and says PASS, FAIL (a
# completed call's work is missing on the other machine) or FAIL-RECORD (the
# work arrived and the transcript's result line for it did not), with the count
# of calls completed on the holder against the calls present on the other
# machine. The script exits 1 when any scenario is not PASS. The whole suite
# takes about twenty minutes, most of it the three minutes the lid stays shut.
set -u
cd "$(dirname "$0")/.." || exit 2
: "${CODEAF_RELAY:?it is the relay under test, for example https://relay.example.com (docs/testing-anywhere.md)}"
only="${1:-}"
log=".lane/durability-hosted.log"
mkdir -p .lane
go test -tags e2e -count=1 -v -timeout 40m -run "TestDurability${only}" ./internal/e2e/ >"$log" 2>&1
code=$?
grep -E "DURABILITY|^(--- |ok|FAIL)" "$log" | sed 's/^ *durability_hosted_e2e_test.go:[0-9]*: //'
echo "full log: $log"
exit "$code"
