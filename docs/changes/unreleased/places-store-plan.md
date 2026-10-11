---
kind: added
title: Places store accepts an organizing plan in one atomic commit
surface: [desktop, chat]
invalidates:
  - "An organizing run needed separate store mutations and receipts. A plan now validates every operation before one commit and one receipt."
  - "Memberships did not distinguish homes from shortcuts. The optional link field now records shortcuts while old files load unchanged."
---

Plan proposals use the same atomic store door on acceptance and count every
created place in the AI ledger. Organizing defaults are add-on, 150 calls,
five US cents per run, and live guess on. These policy rows do not enable the
bridge or desktop organizing controls by themselves. Plan Undo restores the exact prior file, including its revision and visit records;
ordinary single-action Undo keeps its existing behavior.
