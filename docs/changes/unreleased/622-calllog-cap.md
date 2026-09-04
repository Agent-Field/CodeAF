---
kind: changed
title: a body-on call log keeps a long debug session instead of rotating at 32 MB
pr: 622
surface: [engine, docs]
invalidates:
  - "AFORGE_CALL_LOG_BODIES=1 still rotated the live file at 32 MB — about eighty body-bearing calls — and kept one predecessor, so a moderate debug session lost its start. The ordinary cap is still 32 MB; with bodies on the live file is allowed 256 MB."
---

F24: one afternoon with bodies on blew past the 32 MB cap in under thirty minutes.
