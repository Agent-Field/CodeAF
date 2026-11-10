---
kind: fixed
title: A chat whose upload keeps failing is no longer sealed again on every retry
pr: 2544
surface: [engine]
invalidates:
  - "Every sync export re-sealed and rewrote every frame the ledger lacked, even when the same frames were already waiting in the chat's outbox, so a relay refusing every put (403 \"storage_off\") made each one-minute retry re-encrypt and rewrite the whole chat (470 MB per chat on 2026-11-09, 400% CPU). An export now leaves kept-export.json beside its frames and is answered from it while the head, keys, ledger size, frame size and frame files are unchanged."
  - "Frames of an export for an older head stayed in the outbox for ever when they were never published. The next export for a new head now deletes the frames only the older export named."
---

The relay's `storage_off` refusal itself is still unknown to the client and is
still retried every minute; each retry now reads the waiting frames instead of
sealing them.
