---
kind: fixed
title: the shipped third-party notice no longer says Windows builds leave out the furrow engine
pr: 1917
surface: [build, docs]
invalidates:
  - "THIRD-PARTY-NOTICES.md said native Windows builds omit the embedded furrow engine. Since #1888 pinned furrow v0.2.0, every release platform embeds it, native Windows included."
---
