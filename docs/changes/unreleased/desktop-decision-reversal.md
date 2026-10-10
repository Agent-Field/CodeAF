---
kind: added
title: Decision reversal runs undo and lists affected work without cascading
surface: [engine, docs]
invalidates:
  - "Overturning a ledger decision only stamped it. The engine now offers an operation that executes its stored undo through the action owner before recording reversal, and returns direct later dependents without changing them."
---

The operation accepts next=ask or keep. Always ask resets that subject kind to
ask; keep restarts its learning ring. Irreversible or unavailable undo returns
a refusal without changing the ledger. Dependency read failures and undo
failures leave the ledger untouched. Repeated requests do not undo again or
clear new learning. Real action and dependency owners are supplied by callers;
this change does not add desktop bridge delivery.
