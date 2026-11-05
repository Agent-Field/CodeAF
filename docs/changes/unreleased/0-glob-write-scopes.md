---
kind: fixed
title: Workers accept glob write scopes without allowing workspace escapes
pr: 0
surface: [engine]
invalidates:
  - "Write scopes matched only exact paths or directory prefixes."
---

Crew and adaptive workers can write under segment globs such as `crew-0-*.md`.
`*`, `?` and `[...]` match within one segment; matched directories cover their
subtrees. Planning refuses traversal, absolute patterns, malformed patterns and
unsupported `**`, with a repairable reason. The write guard refuses symlink
escapes, including dangling links. Worker guards, amendment collision and retry
checks, and quick-task claims share matching rules. Potentially overlapping glob
claims serialize conservatively.
