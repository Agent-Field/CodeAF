---
kind: fixed
title: Collections survive a second aforge, and a read never takes the writer lock
pr: 685
surface: [engine, chat, docs]
invalidates:
  - "Collection writes from several processes at once were said to queue. They were dropped: eighty concurrent `aforge collections add` commands kept 72 to 79 memberships, because the lock wait was one second. It is now ten, the same bound `internal/store` waits, and all eighty land."
  - "Every open — `list` and `show` included — took a write transaction, so a plain read failed while any peer held the writer. The schema is now built by the first write that needs it, and a read reaches the file through a read-only transaction that costs a shared lock."
  - "A collections database file that already existed at nought bytes was initialized by a read. Only `create` initializes storage now; a refused edit against a blank store answers `collection not found` and leaves the file alone. The claim in #661's entry that a cold read does not initialize storage is true of an existing empty file as well."
  - "A lock this store could not take reported SQLite's own code — `database is locked (5) (SQLITE_BUSY)`. It now answers `the collections database is busy being written by something else (waited 10s)`, and the bound in that sentence is read from the same constant the pragma is."
  - "A `--db` path that would not open reported `unable to open database file: out of memory (14)`, `file is not a database (26)` or `attempt to write a readonly database (8)`. Nothing had run out of memory; the operating system is asked instead and the answer names the file — `is not a regular database file`, `permission denied`, `is not a collections database`."
---

`TestConcurrentFirstOpensAgreeOnOneSchema` was the unit shape of the same
defect and was red under a full run; it now races thirty-two handles rather
than eight and passes under `-race -count=3`.

A reader arriving while a peer initializes is also new. Asked as loose
statements the two pragmas could straddle another process's commit, and our own
schema version over an application id that had not landed yet reads exactly
like a database belonging to another feature — which this store refuses rather
than touches. One read-only transaction answers all three questions off one
snapshot, so that half-seen commit cannot be observed at all.

Journal mode is unchanged: WAL is persistent on-disk state and was measured not
to help this defect.
