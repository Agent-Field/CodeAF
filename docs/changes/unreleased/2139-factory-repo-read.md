---
kind: added
title: The app reads a watched repository's checkout through factory.repo_read, bounded and read-only
pr: 2139
surface: [engine]
invalidates:
  - "Was true: the app could not read a watched repository's files through its factory; an Ask answer could cite jobs and steps only. Is true now: an engine whose hello says `factory.repo_read` serves `factory.repo_read {factory, repo, op, path?, pattern?, from?, to?, job?}` over GET /v1/repos/{owner}/{repo}/read: tree (at most 500 entries), grep (RE2, at most 50 hits) or read (at most 400 lines and 32 KiB), from git's object store at the default branch tip or a job's head commit, with credential-looking paths denied, binaries refused, `not_cloned` when the factory has no checkout, and every answer marked untrusted."
---
