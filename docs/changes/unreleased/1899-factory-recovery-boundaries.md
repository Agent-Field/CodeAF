---
kind: fixed
title: Factory status reads and recovery commits preserve unrelated work and decisions
pr: 1899
surface: [engine, chat, remote]
invalidates:
  - "A busy manager could treat a status question as paid steering, and an idle status reply could drain old follow-ups. Common status reads now bypass the turn lifecycle and preserve active work, model identity and queued messages."
  - "A manager replaced after lookup could still answer a saved question. The locked mutation now rechecks the exact current manager binding."
  - "A worker signal failure could report nothing changed after saving an answer. A durable decision receipt now confirms the saved outcome and supports exact, idempotent redelivery without consuming a newer question."
  - "Local readiness completion could erase a critical decision arriving during its checks. Both initial and locked revalidation now refuse completion and preserve that decision, for draft and already-ready PRs."
---

The desktop keeps free status exchanges outside provider transcripts and does not
emit a false foreground turn ending. The existing snapshot, head, CI, publication
and bounded retry checks remain required.

The durable status tool follows the existing Auto read policy without overriding
explicit denials. The manager tool belt and fixed prefix are checked with that
optional capability enabled.
