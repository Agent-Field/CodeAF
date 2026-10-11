---
kind: added
title: Desktop draft place guesses and Home views
surface: [desktop, chat]
invalidates:
  - "The desktop composer had no live place suggestion, and Home did not expose organiser views or steering. Before the first reply the composer now requests a read-only guess; Home reads views, marks shortcut chats, and posts scoped steer sentences. These controls require the organiser bridge routes."
---

Draft guesses debounce for 300 milliseconds and disappear on an edit or a
20-millisecond request budget breach. A hand selection is kept locally until
the first settled reply. Views filter real loaded Home rows and do not create
places. Unrecognised steering retains the sentence and explains what to try.
