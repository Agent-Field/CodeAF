---
kind: fixed
title: A resumed conversation keeps its late receipts and never saves a lower spend
pr: 2414
surface: [chat, engine, app]
invalidates:
  - Replay restored a conversation's spend from usage seals only, so a restart forgot late receipts and the next turn could save a lower total.
  - Journal call lines were never read back as spend; a call line marked as a late receipt now restores one call and its charge.
---

Ordinary call lines remain evidence only. A meta row rebuilt from the
transcript counts the same late receipts.
