---
kind: fixed
title: The tasks tool counts another window's families, not its parts
pr: 944
surface: [engine]
invalidates:
  - "The `tasks` tool listed another window's work one row per PART, and took its search cap over those rows. A window running one quick task with three parts drew four rows and spent four of the cap, so it could push a different window's whole task out of the answer entirely. It now folds the same way the `<elsewhere>` block already did — one row per family, with its parts counted on the family's own row — and the cap and the overflow line both count families."
  - "The two readings the model gets in one turn used to be able to disagree: `<elsewhere>` said `3 quick parts running` while the tool said four unrelated jobs, and a model deciding whether to start the same work believed whichever it saw last. Both now come from `foldElsewhere`, so there is one writer and one sentence."
---

The fold is reused rather than restated. A second implementation of the same
reading is the drift this repository has laws against, and it is exactly what
made the two readings disagree in the first place.
