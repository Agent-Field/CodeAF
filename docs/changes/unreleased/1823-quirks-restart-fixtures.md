---
kind: internal
title: provider restart fixtures drain learned-memo writers before resetting memory
pr: 1823
surface: [build]
invalidates:
  - "The restart fixtures forced a synchronous save but left a scheduled save running while they discarded process memory. They now drain the owned writer before resetting memory and reloading the memo, preserving the original startup and request-count assertions."
---
