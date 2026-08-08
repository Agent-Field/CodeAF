#!/usr/bin/env bash
# J13 · Second window
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J13 Second window — named "second window" in the header, same thread, promotion when the first closes'

expect_nodes 0

snap a-before
assert_screen 'aforge' 'window A is alive' 30

# B opens onto the same brain. It must say so, not pretend to be the resident.
ux_launch "$UX_SESSION_B"
snap b-opened "$UX_SESSION_B"

if wait_screen 'second window' 30 "$UX_SESSION_B"; then
  _check yes 'window B names itself a second window in the header' \
    '"second window" in B'"'"'s header' "$(pane "$UX_SESSION_B" | head -1 | grep -o 'second window.*' | head -1)"
else
  _check no 'window B names itself a second window in the header' \
    '"second window" in B'"'"'s header' "$(pane "$UX_SESSION_B" | head -1)"
fi

if pane "$UX_SESSION_B" | grep -Eqi 'heron|haiku|river|water|stretch|learned'; then
  _check yes 'B follows the same thread — one conversation, two windows' \
    'the prior conversation visible in B' 'found'
else
  _check no 'B follows the same thread — one conversation, two windows' \
    'the prior conversation visible in B' "$(pane "$UX_SESSION_B" | sed -n '3,6p' | tr '\n' ' ')"
fi

refute_screen 'second window' 'window A does not call itself a second window'

# Close A. B must promote itself, quietly and without dying.
ux_kill "$UX_SESSION"
sleep 40
snap b-after-promotion "$UX_SESSION_B"

if pane "$UX_SESSION_B" | grep -q 'AFORGE-EXITED'; then
  _check no 'B survives A closing' 'B still running' 'B exited'
else
  _check yes 'B survives A closing' 'B still running' 'B alive'
fi

if wait_screen 'aforge' 20 "$UX_SESSION_B"; then
  header="$(pane "$UX_SESSION_B" | head -1)"
  record 'B header after A closed' "$header"
  if printf '%s' "$header" | grep -q 'second window'; then
    _check no 'B promoted — the header segment cleared' \
      'no "second window" segment once B is the resident' "$header"
  else
    _check yes 'B promoted — the header segment cleared' \
      'no "second window" segment once B is the resident' "$header"
  fi
else
  _check no 'B still draws a frame after promotion' 'a header' 'no frame'
fi

# The promoted brain must still be a brain.
since="$(mark)"
say "what is 7 times 6?" "$UX_SESSION_B"
if wait_screen '42' 120 "$UX_SESSION_B"; then
  _check yes 'the promoted window answers — the brain moved, not just the frame' '42 on B' 'found'
else
  _check no 'the promoted window answers — the brain moved, not just the frame' '42 on B' "$(pane "$UX_SESSION_B" | tail -12 | tr '\n' ' ')"
fi
snap b-answered "$UX_SESSION_B"

# Hand the suite back to a single window named the way the runner expects.
ux_kill "$UX_SESSION_B"
sleep 3
ux_launch "$UX_SESSION"

dump_turn "$since"
finish
