---
kind: changed
title: The built-in default model for every role is DeepSeek V4.1 Flash
pr: 1800
surface: [engine]
invalidates:
  - "A fresh profile talked to the floating ~deepseek/deepseek-v4-flash-latest alias, and the low tier (titles and other cheap calls) and the quorum verifier used older V4 Flash builds."
---

DefaultModel and DefaultLowModel are now the exact id deepseek/deepseek-v4.1-flash,
and the quorum verifier uses the low-tier default instead of a hard-coded
deepseek/deepseek-v4-flash. A profile that already names a model keeps it.
