---
kind: internal
title: Factory test homes join their usage writers before directory cleanup
pr: 2384
surface: [build]
invalidates: []
---

Factory test backends release their agents and join the shared usage writer
before the test folders are removed.
