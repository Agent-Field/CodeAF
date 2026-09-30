---
kind: fixed
title: a stopped answer can still be opened after reopening its conversation
pr: 1681
surface: [chat, docs]
invalidates:
  - "Reopening a conversation whose last answer you stopped showed only your message: no `▸ stopped by you` chip, and ctrl+e opened nothing, though the words were in the journal (dev only, since #1627). The reopened turn now draws the same stopped chip, and ctrl+e shows the words that arrived before the stop, as unfinished work."
---
