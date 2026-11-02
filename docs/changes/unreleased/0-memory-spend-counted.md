---
kind: fixed
title: codeaf counts memory spending and keeps job learning inside its turn
pr: 0
surface: [engine]
invalidates:
  - "Memory compiler costs were kept only in its private receipts, so Spending and job headers understated provider spending. Memory calls now enter the usage ledger with the memory role, and owned calls charge the job's existing meter."
  - "The process worker could import job conversations and continue compiling them after Pause or completion. Job sources now belong to the job turn and are excluded from independent background compilation. Idle polling no longer launches refinement without pending sources or retries the same failed input without new sources or an explicit Build."
---

Saved knowledge, citations, search and page organization remain available.
A finished job turn waits at most two seconds for optional memory. Unfinished
compilation continues with its job across steps. Pause, Stop and job end cancel
it, while paid receipts still charge that job. Later turns do not stack compilers.
Quit leaves unfinished sources pending for the next finished owned turn. Compilation can require multiple dependent model requests
for extraction, organization and evidence review. Unchanged input reuses validated
receipts. Large finished turns drain their finite captured snapshot before
publication. Older job sources are reclassified from their persisted ownership.
Existing historical ledger gaps are not reconstructed.

Old job chats with factory-owner.json remain owned even after their roster
entry disappears, so background memory does not spend for them.

Delegated task reports compile with their owning job turn, keep their citation
identities, and charge the same job instead of remaining pending indefinitely.

Memory spending cannot revoke a successful stage result. Admission to the next
step still sees the full job spend, including late memory receipts.

Pending provider receipt recovery still has the ordinary-chat quit limit: it is
not durable, and quitting before the receipt arrives can leave a charge unpriced.
