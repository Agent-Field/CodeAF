---
kind: added
title: Follow PR explains investigations and verification in the job conversation
pr: 1899
surface: [engine, chat, docs]
invalidates:
  - "Was true: a running Follow PR exposed tool captions in its log while the job conversation mostly showed start/end receipts. Is true now: it posts its observed trigger and relays brief committed model progress into the existing job conversation, without another UI element or additional model calls."
---

Progress prose is not verification or authority to push. Existing service receipts,
budget limits and Auto/Manual gates remain authoritative. No running jobs are
changed until the updated engine is built and launched normally.
