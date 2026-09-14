---
kind: changed
title: A closed tab leaves the ctrl+k list at the same moment it leaves the row
pr: 1040
surface: [chat, docs]
invalidates:
  - "The switcher's open list held EVERY conversation the keeper was holding, so a tab dismissed with the `×`, with `ctrl+w`, or with `ctrl+w` on the card itself vanished from the strip and stayed on the card looking open. The list above the fold is now exactly the tab row: `hopReading` splits its rows on `app.hopTabbed` (which asks `app.tabShut`, set the instant a tab goes and cleared in `app.rememberOpen`), and a conversation with no tab is drawn BEHIND THE FOLD — still held, still running, still openable, its tab coming back with it. `TestADismissedConversationIsStillOnTheSwitcher` still holds; it is on the card's fold now rather than in its open list."
  - "`app.hopOpenRows` was a walk of the rows counting `open`. It is now the `tabs` figure `app.hopReading` returned, kept on `hopCard` — the rows gain the rest of the machine when the fold opens, and a walk taken then said the window had twelve conversations open. The head's `3 of 12` is how many TABS are on the row, of how many conversations the machine has."
  - "A second `ctrl+w` on the card used to re-close the row already closed and merely re-say its receipt. Each press now closes the next row, because the closed one has left the list — the same thing the key does on the strip. `TestDismissingARunningTabNeverStopsItsWork` asserts two presses closing two tabs."
  - "`internal/manual/chat/keys.md` said a row below the fold means `this terminal is not holding it`. That is no longer the only case: a conversation whose tab you closed is held, running, and down there. Both that page and `screen.md` now say so."
---

Reported by the owner: the tab disappears and the entry does not. These are two
readings of one set of conversations drawn one line apart, so the card claiming a
conversation was open while the row it is glued to said otherwise was the card
being wrong.

The test the change had to keep is `TestADismissedConversationIsStillOnTheSwitcher`
— a closed tab is how you get a conversation back, so it may not fall off the card
entirely. The fold is where both facts fit: off the list that mirrors the row, and
one `→` `enter` from being in front of you again.

`hopTabbed` asks `tabShut` rather than the drawn strip on purpose. The strip is a
frame behind the keystroke, so a card rebuilt from it after its own `ctrl+w` kept
the row it had just closed until something else redrew the row.
