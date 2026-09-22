---
kind: added
title: Report total session token usage in anonymous telemetry
pr: 1387
surface: [chat, build, docs]
invalidates:
  - "Session telemetry previously sent only banded model and tool counts. It now also sends the numeric total of provider-reported input and output tokens per completed session."
---
