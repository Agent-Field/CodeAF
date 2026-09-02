---
kind: fixed
title: generated manual packs are ignored again, and a build refuses to track them
pr: 299
surface: [build]
invalidates:
  - "`internal/manual/chat.pack.gz` had been force-added after #113 removed it, so every manual merge conflicted on the binary again. It is untracked, and `make build` now refuses any generated manual pack present in the Git index."
---

The Markdown folders remain the source of truth. Release builds still generate
and embed the ignored archives before compiling, and the packed-manual test still
proves that generated path.
