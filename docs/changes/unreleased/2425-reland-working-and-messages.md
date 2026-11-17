---
kind: fixed
title: working elsewhere, messages to a held chat and the destination chip are back
pr: 2425
surface: [engine, chat]
invalidates:
  - "Was true: #2422 reverted #2375, #2408 and #2420, so app/v3 had no Working lease flag, no chats.send and no notes on device records. Is true now: all three are re-landed as their own entries describe, with the note goroutines run through guard.Go."
  - "Was true: the held-chat drive test counted every non-GET request on the held cell as another computer writing. Is true now: the holder's own /heartbeat renewal is excluded, because a second window re-dialing the shared watch socket makes the holder beat its lease."
---
