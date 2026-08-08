#!/usr/bin/env bash
# J18 · Take things out (hint-only variant)
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J18 Take things out — y/Y copy, /open, clickable paths: deliverables exit the terminal without friction'

expect_nodes 0

# The clipboard itself is the machine's, not aforge's — pbcopy on a headless CI
# box is a different journey. What is aforge's, and what this asserts, is that
# the way out is discoverable and that the paths are real.
note 'variant: the clipboard is environment-dependent, so this asserts the affordance renders and the paths are real files'

press '?'
sleep 3
snap help

assert_screen 'help' 'the help overlay opened on ?' 20
assert_screen 'copy the answer' 'the y/Y copy hint is spelled out in help' 20
record 'copy hint' "$(pane | grep -i 'copy the answer' | head -1 | sed 's/^ *//')"
assert_screen '/open' 'the /open route out is named' 20

press Escape
sleep 2
snap help-closed

# The other half of "without friction": the paths the thread printed are real.
paths="$(journal "select body from messages where role in ('agent','system')" | grep -Eo "$UX_STATE/workspace/[^ )\"']*" | sort -u | head -5)"
record 'paths named in the thread' "$(printf '%s' "$paths" | tr '\n' ' ')"
missing=0
while IFS= read -r p; do
  [ -z "$p" ] && continue
  [ -e "$p" ] || { missing=$((missing + 1)); note "MISSING: $p"; }
done <<< "$paths"

if [ -n "$paths" ]; then
  [ "$missing" -eq 0 ] \
    && _check yes 'every workspace path the thread printed is a real file' 'all paths exist' 'all exist' \
    || _check no 'every workspace path the thread printed is a real file' 'all paths exist' "$missing missing"
else
  # Not a failure. An answer-first deliverable is allowed to hand back the
  # content and no path at all — /open and the workspace link are the way out
  # in that case, and grading their absence would punish the better behaviour.
  record 'OBSERVED' 'this run delivered answer-first with no absolute path in the thread; the way out is /open and the workspace link, not a printed path'
fi

dump_turn "$(mark)"
finish
