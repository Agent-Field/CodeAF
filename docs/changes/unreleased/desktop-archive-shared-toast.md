---
kind: changed
title: Desktop archive notifications use the shared toast
surface: [chat]
invalidates:
  - "Archive notifications owned their drawing and restarted a separate timer on hover or focus changes; they now use the shared toast host and preserve the remaining six-second clock."
---

Review, Restore all and Escape retain their archive behavior. The shared host
owns geometry, hover and focus holds, expiry and notification replacement.
The history-toast tokens are no longer consumed by ArchiveToast; their removal
is reserved for the token cleanup wave.
