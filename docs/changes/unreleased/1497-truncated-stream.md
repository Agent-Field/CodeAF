---
kind: changed
title: a reply cut off mid-stream is dropped and asked again
pr: 1497
surface: [chat, engine]
invalidates:
  - A streamed reply ending without a finish reason or [DONE] marker was
    returned as complete, so its partial text could enter the conversation. It
    is discarded and retried now; either explicit completion signal is enough.
---
