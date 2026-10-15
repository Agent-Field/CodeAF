---
kind: added
title: codeaf app answers a bounded, read-only diff of one file
pr: 0
surface: [engine]
invalidates:
  - "The app could read a file but not compare it, so a changed file had no Changes view. files.diff {agent, path, base?} now answers one file's working tree (staged and unstaged) against HEAD or a named commit, with at most 256 KiB and 2,000 lines of hunks."
---

New method `files.diff {agent, path, base?}` → `{diff}`. The base is HEAD
unless a commit or ref is named; one that names no commit falls back to HEAD
and says so (`requested_unavailable`), and one that does says whether it is
in HEAD's history. Untracked files answer as wholly added with no base;
ignored, outside git, no commit yet, missing and git refusals each answer
their state. Git runs read-only under one 3 s deadline, never runs a
program from repository configuration (external diff, textconv, fsmonitor,
filters), is killed once its output passes the bound, and at most two diffs
run at once.
