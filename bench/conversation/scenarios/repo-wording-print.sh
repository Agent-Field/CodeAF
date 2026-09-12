#!/usr/bin/env bash
# repo-wording-print — the ask through the print door.
#
# One message in, one reply out, nobody watching. It is a real door and it is
# NOT the one the request of 2026-09-11 came through, so no row from here may
# be read as a conversation: there is no steer, because there is nobody there
# to type one. What it measures is the turn with the person taken out of it,
# which is the cleanest reading of what a build costs when left alone.

# shellcheck source=repo-wording.ask.sh
source "$CONV_ROOT/scenarios/repo-wording.ask.sh"

SCENARIO_DOOR="print"
SCENARIO_CAP_S="${SCENARIO_CAP_S:-1800}"

scenario_prompt() { repo_wording_ask; }
scenario_check() { repo_wording_check "$@"; }
