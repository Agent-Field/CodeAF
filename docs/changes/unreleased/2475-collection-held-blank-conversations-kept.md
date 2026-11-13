---
kind: fixed
title: Collection-held blank conversations survive cleanup and resolve as live
pr: 2475
surface: [app, chat]
invalidates:
  - "A collection reference did not protect a never-spoken conversation from cleanup. It now keeps the conversation until its last collection reference is removed."
  - "Collections reported saved blank conversations as deleted because agents.list omits them. Collections now resolve them as live from their saved metadata without opening them."
---

A never-spoken conversation held by a collection survives engine shutdown,
launch cleanup and the background places sweep. It stays out of `agents.list`
and automatic resume or reuse: a launch resumes the last spoken conversation,
or creates a fresh one when there is none.

An unreadable collections database preserves blank conversations and excludes
them from automatic resume or reuse. Collection reads show a held blank's saved
title without opening it; permanently deleted conversations remain deleted.
