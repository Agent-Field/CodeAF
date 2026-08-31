---
kind: fixed
title: a checkpoint that arrives after the close writes nothing
pr: 99
surface: [engine]
---

A woken turn's hand-off can checkpoint the task graph from a goroutine that outlives the
session's close, and what it writes then is a settled file overwritten with a stale
"running" row — measured as a one-in-three failure of the woken-turn metering test, whose
temp directory the write raced. The store now latches closed with the session, after the
turn is waited for and the jobs are cut, so every closing transition still lands and a
later write is dropped whole. Only the session that owns the graph closes its store; a
task worker shares its parent's and leaves it open.
