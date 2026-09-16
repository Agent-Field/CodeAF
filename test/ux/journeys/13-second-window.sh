#!/usr/bin/env bash
# J13 · One brain
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"
journey_is 'J13 One brain — a second window on a conversation this machine already has open is told where the conversation lives, and the first window never blinks'

expect_nodes 0

# One brain means one composer. The watcher machinery (watching.go) exists for
# conversations on FAR machines; a conversation open in a second window on this
# one is refused at the journal with the session-busy word. That refusal is the
# product keeping its law — two windows must not race one composer — and this
# journey holds it open.
snap a-before
assert_screen "$(alive_re)" 'window A is alive' 30

# B opens onto the same brain. It must not become a second front door.
ux_launch "$UX_SESSION_B"
snap b-opened "$UX_SESSION_B"

if wait_screen 'open in another window' 30 "$UX_SESSION_B"; then
  _check yes 'the second window is refused with the session-busy word' \
    '"open in another window — go there, or start a new conversation here"' \
    "$(pane "$UX_SESSION_B" | grep -i 'open in another window' | head -1 | sed 's/^ *//')"
else
  _check no 'the second window is refused with the session-busy word' \
    '"open in another window — go there, or start a new conversation here"' \
    "$(pane "$UX_SESSION_B" | sed -n '1p;$p' | tr '\n' ' ')"
fi

# And A never blinked: still drawing its own frame.
if pane "$UX_SESSION" | grep -q 'CODEAF-EXITED'; then
  _check no 'the first window survived the second opening' 'A still running' 'A exited'
else
  _check yes 'the first window survived the second opening' 'A still running' 'A alive'
fi

# The one brain answers from the window that owns it.
since="$(mark)"
say "what is 7 times 6?" "$UX_SESSION"
if wait_screen '42' 120 "$UX_SESSION"; then
  _check yes 'the first window still answers — one brain, undisturbed' '42 on A' 'found'
else
  _check no 'the first window still answers — one brain, undisturbed' '42 on A' \
    "$(pane "$UX_SESSION" | tail -12 | tr '\n' ' ')"
fi
snap a-answered "$UX_SESSION"

ux_kill "$UX_SESSION_B"
sleep 3

dump_turn "$since"
finish
