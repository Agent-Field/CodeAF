---
kind: changed
title: home's rescan defers while a conversation door is in flight
pr: 1743
surface: [chat, tui]
invalidates:
  - "Home's periodic rescan and the rescan at its doors ran on the update loop even while a conversation door was opening, adding rescan cost to the transition on busy profiles. The rescan defers until the door settles now, so opening a conversation no longer carries home's bookkeeping with it."
---

Home's world is rebuilt after the door answers, not during it. The beat keeps
its cadence and the doors keep their pins; the rescan simply waits for the
conversation that is opening to finish opening, which keeps the transition's
cost to the transition itself.
