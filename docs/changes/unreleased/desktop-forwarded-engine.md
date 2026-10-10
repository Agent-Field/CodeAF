---
kind: fixed
title: Desktop forwarded engines use the permitted loopback origin
surface: [desktop, chat]
invalidates:
  - "Forwarded localhost engine URLs reached the renderer unchanged and were blocked by the app CSP. They now become 127.0.0.1 before caching, preserving the engine token, model and port."
  - "The native capability test omitted the existing maximize permission. Its exact allow-list now includes the title-bar double-click permission."
---

Forwarded connections reject remote authorities, URL credentials, non-root paths,
queries and fragments. Dialog and notification plugins, editor commands and
engine-reported file roots remain wired through the existing native bridge.
