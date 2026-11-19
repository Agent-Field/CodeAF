---
kind: fixed
title: web fetch asks the reader for a fresh copy and rejects its cached snapshot
pr: 1771
surface: [chat, engine]
invalidates:
  - "`codeaf web fetch https://example.com` returned r.jina.ai's stale cached snapshot of the page — Jina's own 'cached snapshot' page — because the reader serves its 3600s page cache by default. It now sends the reader's no-cache header and, when the reader still marks its answer a cache hit, fetches the target directly, so what comes back is the page at that address now."
---

The Jina reader r.jina.ai serves a 3600s page cache by default, and codeaf's
fetch passed its answer through verbatim — so a model asked to read a page
could be handed someone else's cache of it without knowing. The fetch now
asks for a fresh copy on every request, and discards an answer the reader
still marks as cached (X-Cache: HIT), fetching the target itself instead.
