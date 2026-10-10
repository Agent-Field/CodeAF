---
kind: fixed
title: Desktop Home chat rows show available engine digests
surface: [desktop, chat]
invalidates:
  - "Home chat rows discarded the engine's per-chat recap lines. They now show the matching line when available, with a waiting reason taking priority and missing evidence omitted."
---

The Chats section reuses the shared 44px ChatRow in its own PlaceChats module.
Clicking still opens or focuses the conversation in the current place; a missing
saved location now shows the engine-data refusal in the section instead of
escaping the click handler.
