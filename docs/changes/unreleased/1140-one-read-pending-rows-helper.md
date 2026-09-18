---
kind: internal
title: one readPendingRows test helper in cmd/codeaf, not two
pr: 1140
surface: [engine]
invalidates:
  - "#1137 and #1138 each added a `readPendingRows` test helper to `cmd/codeaf`; each was green alone and together they redeclared it, so the package's tests did not compile on `santos/dev`. One copy remains."
---
