---
kind: added
title: App resource lanes have typed contracts and guarded route reservations
pr: 1800
surface: [engine, build, docs]
invalidates:
  - "Remaining app routes existed only in the design and returned 404. Their request, response and event types now have generated JSON Schemas, and authenticated reserved routes return 501 not built yet until their owning lane lands."
  - "App resource work needed shared router edits. Each reserved resource now owns a handler file and route table, so parallel lanes can implement their own doors without editing the transport registration."
---

The route law reads explicit app method/path pairs from API.md and exercises the
registered server. Populated and zero-value round-trips cover every wire type;
populated values must also satisfy their committed schemas. Generation drift is
a test failure. Existing transport, keys and agent lifecycle behaviour stays live;
the full agent snapshot is reserved for A4b and no engine behaviour is added here.
