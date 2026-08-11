#!/usr/bin/env bash
# J18 · Take things out (hint-only variant)
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J18 Take things out — y/Y copy, /open, clickable paths: deliverables exit the terminal without friction'

expect_nodes 0

# The clipboard itself is the machine's, not aforge's — pbcopy on a headless CI
# box is a different journey. What is aforge's, and what this asserts, is that
# the way out is discoverable and that the paths are real.
note 'variant: the clipboard is environment-dependent, so this asserts the affordance renders and the paths are real files'

# The door to the help sheet. v1 takes a bare `?` from anywhere. v2's `?` is
# bound only while the scope map holds the keyboard (app.go's key ladder: "it
# can be a bare `?` only where the composer does not hold every printable
# key"), so the user's route is ctrl+o then `?` — 13.4 G1/J19 record the same
# thing, and note that v2's footer advertises `? help` unconditionally while
# the key only answers with rail focus. The journey follows the route the
# surface actually has; that the advertised one does nothing is a finding, not
# a reason to skip the sheet.
if is_v2; then
  press C-o
  sleep 1
fi
press '?'
sleep 3
snap help

assert_screen 'help' 'the help overlay opened on ?' 20
if is_v2; then
  # 5.22 discoverability: the copy verb has to be readable somewhere the user
  # can find it. v2's registry spells the same row "copy answer / copy the
  # focused answer to the clipboard   y" (registry/catalog.go:125-126) instead
  # of v1's "copy the answer". Same promise, v2's words.
  assert_screen 'copy .*answer' 'the y/Y copy hint is spelled out in help' 20
  record 'copy hint' "$(pane | grep -Ei 'copy .*answer' | head -1 | sed 's/^ *//')"
else
  assert_screen 'copy the answer' 'the y/Y copy hint is spelled out in help' 20
  record 'copy hint' "$(pane | grep -i 'copy the answer' | head -1 | sed 's/^ *//')"
fi
assert_screen '/open' 'the /open route out is named' 20

press Escape
sleep 2
snap help-closed
# Closing the sheet leaves the keyboard where it was — on v2 that is the scope
# map, where Enter opens a rail row instead of sending a draft. Hand it back to
# the composer, or every journey after this one types into a window that will
# not listen.
if is_v2; then
  press C-o
  sleep 1
fi

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
