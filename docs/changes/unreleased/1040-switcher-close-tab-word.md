---
kind: fixed
title: The switcher's close key says close tab, because put away is home's word for archiving
pr: 1040
surface: [chat, docs]
invalidates:
  - "The `ctrl+k` switcher's legend read `ctrl+w put away`. It now reads `ctrl+w close tab`. The key never archived anything: `hopAway` takes the row off this window's tab row and the conversation keeps running and stays on the list, which is what the card's own receipt (`tab closed · <title>`) already said."
  - "`put away` belongs to ONE act and it is home's: `ctrl+e` on a home row archives a conversation and says `put away · type its name to find it again`. Nothing on the switcher puts a conversation away. `internal/manual/chat/keys.md` used the phrase for the switcher's `ctrl+w` in four places and no longer does; its section is now headed *Close a tab from the switcher*, not *Put a conversation away from the switcher*."
---

Reported by the owner from the card itself: the legend said one act and the
receipt two lines below said another. The receipt was the honest one — its
comment in `hop.go` states the law it was written under, that the surface must
not claim an act it did not perform — so the legend moved to meet it.

The switcher's section in `keys.md` now carries the distinction in its own
paragraph rather than leaving a reader to infer it, because the two keys are one
keystroke apart in a person's hands and only one of them is durable.
