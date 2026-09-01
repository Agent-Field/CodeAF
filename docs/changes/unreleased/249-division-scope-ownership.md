---
kind: changed
title: no two parts of one division may own the same path, and it is refused at admission
pr: 249
surface: [engine]
invalidates:
  - "Overlapping scope between the parts of a division was advice a worker could ignore — prompts/divide.md said \"Two parts that edit the same file are not independent\" and the reviewer's brief asked it to fix the boundary. It is now ENFORCED: a division whose parts name the same path is refused before any part exists, and the worker is told which path."
  - "journalDivision's Decision could be refused:arguments, refused:floor, refused:review-unreached, refused:lane, refused:cap, refused:review or refused:nobody. There is now an eighth, refused:scope, and a bench counting refusals has to know it."
---

`dev` landing of the work that first opened as PR #236 against `feat/task-families`.
The check is deliberately dim: only the exact same normalised path collides.
