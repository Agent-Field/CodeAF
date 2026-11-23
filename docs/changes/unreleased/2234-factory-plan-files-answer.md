---
kind: fixed
title: codeaf Auto reads the plan model's structured file list
pr: 2234
surface: [engine]
invalidates:
  - "A plan's file list was read only from a Files: declaration in its prose output. Plan answers now carry files in stage_result, saved and carried with the step."
---

A plan that names README.md in prose can now supply a checkable file list in
its answer, so Auto can continue when that list touches no protected path.
Protected files still require approval. Missing, invalid or conflicting lists
still pause Auto, with words that explain the missing checkable list without
claiming the plan names no files. One existing correction attempt asks the plan
to supply its list; no new retry budget is added. Older saved Files: declarations
remain readable.
