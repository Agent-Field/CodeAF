---
kind: added
title: work.graph reads the tasks of an agent, a task or a crew as general, bounded, paged facts
pr: 0
surface: [engine]
invalidates:
  - "A task could only wait on another task of its own conversation (depends_on). A queued task can now also wait on a task of another conversation through Agent.WaitOnTask: stored in the record as waits_external and enforced by the same frontier, which reads the other conversation outside every lock."
  - "task checkpoints carried no admission time and no time of the present state. They now carry admittedAt and stateAt, stamped where the transition happens; a record written before them carries neither, and the graph says so rather than dating old work with the clock that read it."
  - "The app had no read of what waits on what across a crew. work.graph {scope, view, cluster, focus, cursor, limit, if_revision} answers it from the conversations' own files, without asking any live agent, and the graph holds every task row tasks.list shows except unapproved proposals."
---

New optional wire method `work.graph` (feature `work.graph`; `unknown_method` from an engine
without it). Pages are bounded in nodes, edges and words, every total is true, and an edge exists
only where a wait was stored and enforced. Conversations are read from `tasks.json` and the plan
stores through read-only connections and cached by file stamp, so an unchanged conversation is
never reparsed. Everything it holds or has in flight is bounded to 128 MiB in bytes (snapshots by a
conservative estimate), a snapshot too big to keep is served whole and not kept, and a build beyond the
pending limit is `busy`. Plan rows carry the store's own `state_detail`, and an accepted route carries its
published `price` (absent = unknown). `docs/design/app-v3/PROTOCOL.md` ("Work graph") has the contract.
