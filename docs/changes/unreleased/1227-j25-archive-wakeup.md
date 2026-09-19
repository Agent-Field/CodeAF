---
kind: fixed
title: archive suppresses Bind/Resume automatic wakeups; history remains
pr: 1227
surface: [chat, engine]
invalidates:
  - "J25 archive did not suppress Bind. Conversation put-away was session.SetArchived only, Bind/Resume always flushed pending, and no production door wrote ParticipantArchived. ArchiveCoordination now writes that bit; Bind, Resume, tick, and host spawn leave pending pending; pause still Resume-flushes already-waiting lines."
  - "Putting a discussion away left collab participants active, so reopen looked like an automatic wakeup. Put-away now maps onto ParticipantArchived; the roster and deliveries stay readable."
---

Closing a view still does not pause. Live tmux of archive is a later integrated pass.
