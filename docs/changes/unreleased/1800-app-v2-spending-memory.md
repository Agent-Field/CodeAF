---
kind: added
title: App spending execution caps and memory shelves with five-second undo
pr: 1800
surface: [engine, remote, docs]
invalidates:
  - "A11b was only HTTP handlers: daily agent admission, temporary cap expiry and memory-add enforcement were absent. Native sessions now check $20 day, $5 agent/day and $2 task defaults before model calls, read restart-safe local-midnight raises, and honor the memory-add switch."
  - "The app bound its agent budget to a lifetime conversation rail. SetAppBudget now binds a daily allowance including child work, while SetSpendRail retains lifetime conversation and crew-member limits."
  - "Memory shelf moves refused, and app expectations did not serialize with terminal edits. The canonical store now compares and journals conditional text/shelf mutations and the five-second undo receipt in one SQLite transaction, with owner-isolated crew shelves."
  - "Native spending receipts did not publish app events or create budget questions. A home observer now publishes measured changes at most once per second and maintains one saved budget question per reached scope."
---

Task caps keep their root agent with their local task number. Temporary raises
expire on the next local calendar boundary, including daylight-saving changes;
permanent defaults are preserved. An intervening terminal restore/forget
invalidates an old memory Undo. Explicit person memory saves remain available
with automatic additions off. Member and crew cap writes retain their owning
resource backend; the spending resource does not rewrite their native documents.
