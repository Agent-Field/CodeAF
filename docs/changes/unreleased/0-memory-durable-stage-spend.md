---
kind: fixed
title: codeaf retains memory spending after a stage closes
pr: 0
surface: [engine]
invalidates:
  - "A closed stage could lose paid memory from its saved conversation total."
---

Memory that finishes after a stage closes now appends its paid usage to the
existing conversation journal. Reopening restores that spending along with the
stage's other calls, so the next spending update retains the paid memory.
