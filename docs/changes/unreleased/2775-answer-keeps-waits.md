---
kind: fixed
title: an answer no longer starts a workstream task whose waits are not done
pr: 2775
surface: [engine, factory, docs]
invalidates:
  - "Was true: answering the last open Ask on a workstream task set it Running and, for Afi on an organization's server, started Afi at once, even while the task still waited on unfinished tasks (blackmac, 2026-11-17: the launch blog post was written before the launch date was confirmed). Is true now: the answer is kept and its decision recorded, the task goes to Up next with its thread saying \"Up next: waits for <titles> before it starts\", and it starts by itself once that work is done. A task whose waits are all done goes on at once, as before."
---
