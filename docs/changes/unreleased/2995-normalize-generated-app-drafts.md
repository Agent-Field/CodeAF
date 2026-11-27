---
kind: fixed
title: Apply mechanical draft fixes before app Check
pr: 2995
surface: [app]
invalidates:
  - "Check used to send unplaced commands and duplicate record declarations back for another model pass. The builder now applies a deterministic correction when the draft already contains the information it needs."
---

Build activity records automatically placed controls, shared field bindings, sample trigger examples, and repaired visibility policies. Choosing which shared records to use still requires consent in Desk.
