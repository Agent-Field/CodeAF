---
kind: added
title: codeaf app answers a bounded, read-only file preview
pr: 0
surface: [engine]
invalidates:
  - "The app could find and stat files but not show one, so a file chip opened nothing. files.read {agent, path, offset, limit} now answers the file's canonical path, whether it lies outside the workspace, its facts, and one page of at most 64 KiB of UTF-8 text from files up to 1 MiB."
---

New method `files.read {agent, path, offset, limit}` → `{file}`. Binary,
non-UTF-8, over 1 MiB, folders, fifos and missing paths answer their kind and
facts with no bytes. Links are followed; `outside` is judged on where the file
really is. Nothing is ever written. An engine that does not read answers
`unknown_method`.
