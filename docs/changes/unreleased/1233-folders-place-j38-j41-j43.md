---
kind: fixed
title: Folders New chat Esc returns without minting; nest/rename are keys; home composer survives alt+5
pr: 1233
surface: [chat]
invalidates:
  - "Esc from visible New chat on the Folders place landed in the launch conversation (session-header transcript). It now returns to Folders and mints nothing."
  - "Enter on `/folders nest Receipts in Billing` stored that line as a folder name. Nest, rename, and move are keyboard actions; a known slash line is dispatched, not CreateFolder'd."
  - "A sentence typed on home vanished after Folders opened (`› say what you want done`). The composer and folder selection now survive 80-col and wide."
---

J42 (organize restart minting a second pending job) is a different lane.
