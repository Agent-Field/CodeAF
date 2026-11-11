---
kind: added
title: list deliverables held by the teams without including loose conversations
pr: 0
surface: [engine]
invalidates:
  - "Was true: deliverables.list and deliverables.count could read an agent, a crew or the whole index, but no scope represented the open team hierarchy without loose conversations. Is true now: scope teams reads the actual open hierarchy, returns each owner's recorded team and top-level membership, and binds pagination to that membership revision. Inconsistent, oversized or unfinished reads are refused rather than reported as an empty or complete union."
---
