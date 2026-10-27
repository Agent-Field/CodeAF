---
kind: fixed
title: A path rule that names a bare folder covers what is under it
pr: 0
surface: [engine, docs]
invalidates:
  - "Was true: only a path rule ending in a slash covered a folder's contents: `touch auth/` asked about auth/keys.go, but `touch auth` matched only a file or folder named auth itself, so a plan or an issue naming auth/keys.go went through. Is true now: an ask-first path or a never line that is a bare name, with no slash at the end and no `*`, `?` or `[`, covers the folder of that name and everything under it as well, at a start and in the approval after a plan. auth does not cover authors/."
---
