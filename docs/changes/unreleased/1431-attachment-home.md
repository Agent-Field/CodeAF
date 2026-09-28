---
kind: fixed
title: Conversation drafts and attachments stay with their conversation
pr: 1431
surface: [chat, docs]
invalidates:
  - "Unsent conversation attachments remained on the shared tray when Home opened. Home now owns a separate tray and opens with a clean slate. The conversation keeps its unsent prompt, cursor and attachments when you leave and return; starting a conversation from Home carries only what was added there. Files and queued messages are untouched."
  - "Conversation chips could be clicked to remove them but showed no action mark. They now show a remove mark through the shared glyph vocabulary."
---
