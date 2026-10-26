---
kind: fixed
title: Keep existing chats reachable while a new conversation opens
pr: 1947
surface: [chat, docs]
invalidates: []
---

Pending first messages keep the window's real tabs and navigation visible.
Selecting a chat or page acts on the first click and cancels opening; canceled
Home drafts and attachments remain available on return. Body clicks keep opening
active, and the status says `opening chat` until conversation setup succeeds.
