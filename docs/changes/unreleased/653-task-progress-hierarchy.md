---
kind: fixed
title: Keep intermediate task narration distinct from the answer
pr: 653
surface: [chat]
invalidates:
  - "A paragraph ending a settled task phase could retain answer styling even after more tools ran beneath it. Later work now makes it progress narration and eligible for the next step caption."
---

The final reply keeps its answer styling. User messages and corrections still end
the comparison, so later work does not reclassify the reply to an earlier request.
The original transcript, phase folding, and disclosure choices remain intact.
