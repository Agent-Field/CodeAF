---
kind: fixed
title: closing chat now stops and joins the pool judge sweep
pr: 1267
surface: [chat]
invalidates:
  - "The pool judge sweep could outlive chat close and resolve an empty profile path against a later CODEAF_HOME, allowing it to write pool files into another launch's home. Each launch's sweep is now context-owned by the profile pool errand set, and close cancels and joins it before returning."
  - "Process guard goroutines had no structural inventory of their close ownership. A class law now rejects a new unclassified process launch and records joined, self-completing, and explicitly known-open goroutines."
---
