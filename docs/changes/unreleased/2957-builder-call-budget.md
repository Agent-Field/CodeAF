---
kind: changed
title: App Builder keeps draft changes when it runs out of model calls
pr: 2957
surface: [app, engine]
invalidates:
  - "The Builder previously used one call limit for every writing turn. Writing and repair now budget for the record types, screens and runs in the plan."
  - "A model call limit previously dropped in-progress edits. Completed definition changes now stay in the draft so you can continue building."
  - "Reverting an app previously rewrote the saved manifest version. It now restores the original files and assigns a new publication number in the app metadata."
---

The scoped Builder can fetch several kit descriptors together and reuse
unchanged definition replies. It receives Check findings together so a
repair can address all reported locations in the same turn.
