#!/usr/bin/env bash
# J13 · Second window
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J13 Second window — the window that is not the resident says so, same thread, promotion when the first closes'

expect_nodes 0

# The residency words, per surface. v1 writes "second window" (and "second
# window · resident is pid N") into the header — internal/tui/residency.go:56.
# v2 has no header (13.4) and puts the same fact in the footer's health column
# as "visitor · pid N" (chat/panes.go:292-312, 13.4 J13), which is the same
# promise in fewer cells: this window is a guest of a brain that lives
# elsewhere. Either way the law is 5.20's predictability rule — a window must
# never pretend to be the resident — and this journey asserts the words the
# surface under test actually owes.
if is_v2; then
  RESIDENCY_RE='visitor'
  RESIDENCY_WHAT='"visitor · pid N" in B'"'"'s footer'
else
  RESIDENCY_RE='second window'
  RESIDENCY_WHAT='"second window" in B'"'"'s header'
fi
ALIVE_RE="$(alive_re)"

snap a-before
assert_screen "$ALIVE_RE" 'window A is alive' 30

# B opens onto the same brain. It must say so, not pretend to be the resident.
ux_launch "$UX_SESSION_B"
snap b-opened "$UX_SESSION_B"

if wait_screen "$RESIDENCY_RE" 30 "$UX_SESSION_B"; then
  _check yes 'window B names itself a visiting window, not the resident' \
    "$RESIDENCY_WHAT" "$(pane "$UX_SESSION_B" | grep -Eio "$RESIDENCY_RE.*" | head -1)"
else
  _check no 'window B names itself a visiting window, not the resident' \
    "$RESIDENCY_WHAT" "$(pane "$UX_SESSION_B" | sed -n '1p;$p' | tr '\n' ' ')"
fi

if pane "$UX_SESSION_B" | grep -Eqi 'heron|haiku|river|water|stretch|learned'; then
  _check yes 'B follows the same thread — one conversation, two windows' \
    'the prior conversation visible in B' 'found'
else
  _check no 'B follows the same thread — one conversation, two windows' \
    'the prior conversation visible in B' "$(pane "$UX_SESSION_B" | sed -n '3,6p' | tr '\n' ' ')"
fi

refute_screen "$RESIDENCY_RE" 'window A does not call itself a visiting window'

# Close A. B must promote itself, quietly and without dying.
ux_kill "$UX_SESSION"
sleep 40
snap b-after-promotion "$UX_SESSION_B"

if pane "$UX_SESSION_B" | grep -q 'AFORGE-EXITED'; then
  _check no 'B survives A closing' 'B still running' 'B exited'
else
  _check yes 'B survives A closing' 'B still running' 'B alive'
fi

# Promotion clears the segment: v1 rewrites its header row, v2's footer health
# column returns nil and the whole "visitor · pid N" cell disappears
# (panes.go:292-312 — a resident says nothing about residency at all).
if wait_screen "$ALIVE_RE" 20 "$UX_SESSION_B"; then
  if is_v2; then
    chrome="$(pane "$UX_SESSION_B" | tail -1)"
  else
    chrome="$(pane "$UX_SESSION_B" | head -1)"
  fi
  record 'B chrome after A closed' "$chrome"
  if printf '%s' "$chrome" | grep -Eq "$RESIDENCY_RE"; then
    _check no 'B promoted — the residency segment cleared' \
      "no $RESIDENCY_RE segment once B is the resident" "$chrome"
  else
    _check yes 'B promoted — the residency segment cleared' \
      "no $RESIDENCY_RE segment once B is the resident" "$chrome"
  fi
else
  _check no 'B still draws a frame after promotion' 'a frame of its own' 'no frame'
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
