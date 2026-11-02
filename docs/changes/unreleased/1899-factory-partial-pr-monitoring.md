---
kind: fixed
title: Keep current PR facts when readiness policy is unavailable
pr: 1899
surface: [engine, chat]
invalidates:
  - "A readiness API error previously discarded successfully read CI and review facts and left healthy drafts watching forever."
---

Follow PR retains independent current-head CI, reviews and feedback reads,
records typed incomplete-section diagnostics, and observes terminal PR states
without depending on optional policy APIs. The exact GitHub plan-restriction
response makes only that unavailable protection feature inapplicable; known
requirements from other policy APIs remain enforced. Permission errors, rate
limits and incomplete material remain distinct and block readiness. Existing
watching jobs can enter final review on the next complete snapshot. No live job
data migration or restart action is required.
