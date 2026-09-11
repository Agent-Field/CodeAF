---
kind: fixed
title: A conversation's identity is written under a lock of its own, so two windows lose nothing
pr: 695
surface: [chat, engine]
invalidates:
  - "A conversation folder's meta.json was serialized on a mutex belonging to one Agent, so only one window's stamps were ordered against each other. Every read-modify-write of that file — the name, the total, the rung, the folders, the working copies, the workspace anchor and home's put-away — now runs under a meta.lock flocked beside it, so a second Agent, a second aforge and an engine on shared disk queue instead of renaming a complete file over each other's field."
  - "Two writers on one folder lost a field in 200 rounds out of 200, and home putting a conversation away while its own window sealed a turn could leave the conversation un-archived with nothing said. Both now keep every field."
  - "A conversation folder held transcript.jsonl, meta.json, state.json, tasks.json, the task transcripts and work/. It also holds meta.lock — an empty file that is only ever flocked, and the manual page that lists the folder's contents names it."
  - "Agent.metaMu is gone. Nothing in internal/session serializes metadata on an Agent any more; withMetaLock in metalock.go is the one door, and it keeps the old law that a stamp is never refused — a filesystem that cannot flock, a lock file that cannot be created and a holder that outlasts the five-second wait all write exactly as they did before the lock existed."
---

meta.json is replaced atomically, so a reader always sees a whole identity — but
the read before that rename was never part of the same act, and two writers each
holding half an identity is how a name or a total disappeared. The lock is the
folder's rather than the session's, because the writers that meet there are on
different Agents, different processes and sometimes different machines.
