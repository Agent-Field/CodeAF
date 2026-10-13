---
kind: added
title: app Inbox derives source items and shares a five-second answer hold
pr: 1800
surface: [engine, remote, docs]
invalidates:
  - "The app Inbox routes returned authenticated 501 stubs. They now derive the sixteen waiting kinds with stable source ids and forward answers through their owning resolvers."
  - "Agent answers had no server-side Undo hold. App ask and permission answers now share the Inbox's five-second coordinator; Undo cancels an unsent answer."
  - "Factory needs-you items were not merged by the binary. Typed solo and attached producers now merge person-scoped Contract 2 items, follow factory events and preserve the factory approval's own Undo deadline."
---

Source-resource production and factory attachment lifecycle remain in their
own lanes. Inbox adds no Later or Done-today store, and shutdown cancels local
pending answers rather than sending them after a restart.
