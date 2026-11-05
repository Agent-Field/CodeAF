---
kind: fixed
title: Keep automatic git maintenance out of codeaf operations
pr: 0
surface: [engine, factory, resident, docs]
invalidates:
  - Only the branch land path used to disable automatic git maintenance.
---
codeaf's own commits no longer trigger automatic maintenance anywhere in its
session, Factory or craft git helpers. Git 2.56 can otherwise leave maintenance
running in the background, changing the object store after codeaf moves on.

The settings apply to each command, including operations in task copies and
linked worktrees that share the real repository's objects. Repository settings
stay unchanged: the user's own git still maintains their real repo as usual.
Private copies and the craft history may retain loose objects. No scheduled or
explicit garbage collection or maintenance is added.
