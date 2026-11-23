---
kind: fixed
title: The key vault carries secrets, not files of any size
pr: 2576
surface: [engine, app, chat]
invalidates:
  - "Every file the seal guard withheld as looking like a secret travelled through the key vault whatever its size. A chat whose folder was the codeaf home carried graph.db, a git pack and binaries into a 3.3 GB vault.enc, and every chat flush then read that whole file once per slot: 22 s of CPU and a 34 GB peak per flush on the owner's machine. One vault slot now holds at most 1 MiB (keys.MaxSlotValue). A larger withheld file is still kept out of every save but stays on its computer: it is never read for the vault, never overwritten by another computer's copy and never deleted by its removal. A vault that already held such slots drops them, without a tombstone, the first time it is opened."
---
