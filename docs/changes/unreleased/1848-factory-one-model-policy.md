---
kind: fixed
title: Factory workers honor the saved one-model task policy on every launch path
pr: 1848
surface: [engine]
invalidates:
  - "Some factory launch paths ignored the saved one-model task setting. Reads, manager and step conversations now use the selected model and connection, and refuse a mismatch or unavailable choice instead of selecting a fallback."
  - "A refused policy could reach session creation as an empty model and leave an empty session directory. The factory now returns the policy's actionable refusal before opening a conversation."
  - "A factory turn could reuse an interactive manager chat running on a different model. Under the saved one-model setting it uses a governed factory conversation, and refuses while an incompatible open chat holds that conversation."
---

Auto routing and explicit one-model launch pins retain their existing behavior. New admission under the saved policy respects the daily crew cap. A running conversation keeps the model it started with.
