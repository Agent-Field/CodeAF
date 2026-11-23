---
kind: internal
title: Disposable task test homes join their shared usage writer before cleanup
pr: 2383
surface: [build]
invalidates: []
---

Task test homes close the shared usage writer in cleanup, so removing the test
folder no longer races a writer that is still flushing.
