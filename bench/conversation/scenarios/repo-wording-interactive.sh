#!/usr/bin/env bash
# repo-wording-interactive — the ask through the door the request actually came
# through: a real terminal, a real paste, and one correction typed thirty
# seconds later while the harness is still working.
#
# THE STEER IS ON THE CLOCK AND NOT ON A MARKER. A person does not wait for a
# spinner to reach a particular state before changing their mind; they read the
# first thing they see and type. A marker-shaped wait would also hand the two
# arms different treatments — a build that reaches the marker sooner gets
# steered sooner — and the one thing this grid may not do is give its arms
# different questions.

# shellcheck source=repo-wording.ask.sh
source "$CONV_ROOT/scenarios/repo-wording.ask.sh"

SCENARIO_DOOR="interactive"
SCENARIO_CAP_S="${SCENARIO_CAP_S:-1800}"

scenario_turns() {
  local plan="$2"
  {
    printf 'ready\t%s\n' "$(repo_wording_ask | tr '\n' ' ')"
    printf 'after:%s\t%s\n' "${CONV_STEER_AFTER_S:-30}" "$(repo_wording_steer | tr '\n' ' ')"
  } > "$plan"
}

scenario_check() { repo_wording_check "$@"; }
