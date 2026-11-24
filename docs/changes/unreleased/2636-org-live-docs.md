---
kind: added
title: Two people can type into one organization page at once, relayed by its server as opaque Yjs updates
pr: 2636
surface: [factory, engine, app]
invalidates:
  - "Two people cannot edit one organization page at the same time; a page is held by one editor (E3a's page hand). Now each page is a live Yjs document relayed by the organization's server at /v1/docs/ws/<id>/page/<pid>, and E3a's page hand is gone."
  - "Every organization change reaches the desk on GET /v1/events. Live document updates and carets travel on a per-document stream, GET /v1/docs/{doc}/events, which the engine follows with factory.org_doc_follow."
---

The server keeps each live document as an append-only log of updates it never parses, stamped with seq, by and at, plus the latest snapshot a client made. The parent item's access decides who may use it: `view` reads and follows, `edit` writes. Clients compact: past 500 updates or 512 KiB the server asks for a snapshot, and past 5000 or 2 MiB it refuses new updates until one arrives. The engine says `org_docs` in hello and carries `factory.org_doc_read`, `_send`, `_snapshot`, `_aware`, `_follow` and `_unfollow`, relaying `factory.org_doc_update`, `factory.org_doc_awareness` and `factory.org_doc_closed`.
