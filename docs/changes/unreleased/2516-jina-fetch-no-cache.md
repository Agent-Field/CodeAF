---
kind: fixed
title: web fetch returns the live page, not the reader's hour-old cached snapshot
pr: 2516
surface: [engine]
invalidates:
  - "`web_fetch` (and `codeaf web fetch`) went through r.jina.ai with no cache control, so a page the reader had rendered in the last hour came back as its stored snapshot - sometimes someone else's cache of that address, carrying Jina's own cached-snapshot warning. The fetch now sends `x-no-cache: true`, so what comes back is the page at that address now."
---
