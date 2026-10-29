---
kind: fixed
title: Landing a task branch works on Apple Git 2.39 with git-lfs installed
pr: 2035
surface: [engine, chat]
invalidates:
  - "#1995 said a Git without the attribute queries refuses a land whenever a filter program is configured. A Git that predates them (Apple Git 2.39 on macOS) now reads each tree's attributes through a temporary index and finds its global and system attribute files itself; only a non-Apple Git older than 2.42 still refuses."
---

With a filter program configured (for example by `git lfs install`), a land on
macOS's own Git refused every time with "this land cannot determine which files
run a filter program", because Apple Git 2.39 has neither `check-attr --source`
(Git 2.40) nor `git var GIT_ATTR_GLOBAL`/`GIT_ATTR_SYSTEM` (Git 2.42).

The land now notices, once per Git executable and version, that a query is
unsupported (only the exact reply a Git that predates it gives) and falls back.
Each tree is read into a private temporary index outside the repository, with
split index and hooks off, and removed before the plan returns. The global
attribute file is found as Git finds it, and Apple Git's two system files are
bound, honouring `GIT_ATTR_NOSYSTEM`. A capable Git keeps the native queries and
the same plan digest. A land still refuses only when its paths use a filter
whose driver has a program, and asks nothing about attributes when no program
is configured.
