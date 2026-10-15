---
kind: fixed
title: ask here answers its approvals in the pane, and its card keeps the question guards
pr: 1833
surface: [chat, docs]
invalidates:
  - "A call inside an `ask here` errand that needed your ok had nowhere to be answered, so the errand's row said `working` until the window closed. The pane now asks it under the call, and `1 allow once` or `3 deny` answers it."
  - "asking-from-home.md sent approvals to `continue as a conversation`, which interrupts the waiting call. It now says the approval is answered in the pane, and that continuing ends the waiting call unanswered."
  - "Typing a correction after `o` on the pane's card still let `1`, `3` and `0` answer the card, so `make it 10` said yes. Every key is text while the change is written."
  - "The pane's card took a key the instant it appeared and filed its answer as a receipt above the window's own box. It now keeps the block's quarter-second settle guard and leaves no receipt there."
  - "asking-from-home.md named the card's change key `c`. It is `o`."
  - "The pane's stream reader was spelled `errandWait`, so the tui3 test harness treated it as work that always finishes. It is `waitErrand` and is listed in `blockingCommands`."
---
