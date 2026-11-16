---
kind: added
title: the desktop can read a chat another computer holds without moving it or its project folder
pr: 2650
surface: [engine, app, docs]
invalidates:
  - "Was true: the only way to see inside a chat another computer held was agents.take, which leased the chat away and downloaded its whole snapshot, project folder included. Is true now: chats.read {cell, before?, limit?} answers {entries, more, before?, head, sealed_at_ms?, fetched_bytes} — one page of the transcript as last sealed there, newest page first — fetching only the transcript's own objects and the chunks under the page. No lease, no materialize, no agent."
  - "Was true: cellsync's want/import walked the whole head. Is true now: cellsync.Reader and Pager want one path (and a byte range of it) through new furrow verbs want --path [--range], peek and read; they need a furrow whose `furrow --json capabilities` lists read, and chats.read is absent from hello until then."
  - "Was true: objects a device fetched for a chat came only from a take. Is true now: a read imports into the same store a take uses, so a take after reading fetches only what the read did not."
---

Measured on a real furrow with a 16.8 MB chat (12 MiB project, 4 MiB transcript):
the first page fetched 644 KB, one more turn 294 KB, and a take afterwards
refetched none of the read's objects. docs/design/remote-read/DESIGN.md has the
design and says why it must not be simplified into a full fetch.
