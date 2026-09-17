---
kind: fixed
title: a finishing task can read the evidence its context has archived
pr: 808
surface: [chat, engine]
invalidates:
  - "The finishing turn kept only tools that save a deliverable and removed read. It now retains read alongside the existing saving tools: compacted results name files, so withdrawing their retrieval tool made the retained evidence unreachable exactly when the worker had to finish."
  - "The landing instruction prohibited all further observation. It now permits reading existing evidence while still forbidding new exploration. Reading does not count as saving or reset the progress counter; savingTools and the exclusions for asynchronous generation are unchanged."
---

The combined regression archives a tool result, retrieves its full text, applies the
same tool narrowing as the finishing turn, and retrieves the same evidence again.
It needs no live model or benchmark repository. The fix adds no new state, provider
choice, budget or task-specific exception.
