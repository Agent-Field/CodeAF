---
kind: added
title: ctrl+shift+t reopens the tab you just closed
pr: 653
surface: [chat]
invalidates:
  - "A dismissed tab could only be brought back through `ctrl+k` or home. `ctrl+shift+t` reopens the last one shut, and pressing it again walks back through earlier closures newest first."
  - "`ctrl+t` and `ctrl+w` were the whole of the tab grammar. There is a third key now, and the plain `ctrl+t` is still only ever a new chat — a terminal that cannot send the shifted chord gets that, never a reopen."
---

The window keeps the last 32 closed tabs, reusing the tab row's existing cap.
Held conversations return through the keeper; remembered conversations resume
through the existing open door and their own connection, without ending the chat
currently in front.
The chord does not create a new conversation. A tab already reopened by hand is
skipped, a reclosed chat has one entry, and the synthetic New chat page stays out
of history: its parked first message comes back with `ctrl+t`.

The chord is read at `ctrl+w`'s own rung, under every modal, and once more on home
alone — shutting the last tab lands you there, so the key that undoes that press
has to be reachable from where the press put you.

Failed reopen attempts retain the last closure for retry, and remote addresses are
resolved through the existing connection. Escape from Home exposes the current
conversation; reopening then skips that already-visible tab. Reopening never claims
another conversation's connection and does not restart work that was explicitly stopped.
