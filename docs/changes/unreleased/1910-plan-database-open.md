---
kind: fixed
title: Open plans beside writers and preserve literal database paths
pr: 1910
surface: [engine]
invalidates:
  - "Opening an existing plan previously acquired the writer lock, so a worker read could fail after five seconds behind another writer. Current-schema inspection and adoption now use a read-only snapshot."
  - "Seven plan and ledger openers treated filename characters as SQLite URI syntax. They now encode the literal absolute or relative filesystem path."
---

Creation and migration retain the serialized writer path. Writes keep their
existing bounded retry budget; exhaustion names the contention and tells the
caller to retry after the other writer finishes, without claiming success.
