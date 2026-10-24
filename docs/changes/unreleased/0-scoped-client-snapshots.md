---
kind: added
title: Paired clients receive sealed desktop method snapshots
surface: [engine, app, chat]
invalidates:
  - "Scoped clients had relay slots but no binary publishing their contents. The app backend now runs locally paired clients' subscribed reads through the desktop method table, filters chats by lease, and seals scoped result envelopes to each client's independent key."
  - "The relay manual said the hosted Worker lacked scoped clients. Both relay implementations now carry the protocol."
---

The runner stays dormant with no active clients. Publish and app events request
coalesced refreshes; a bounded timer retries failures. Unchanged snapshots skip
writes until renewal ahead of relay expiry. Pairing state is local to each
executor; the current directory does not distribute encryption keys.
