---
kind: fixed
title: Single-model chat defers independent Model Pool judging
pr: 1610
surface: [chat]
invalidates:
  - "The Model Pool could call another model after a task landed under --one-model. Single-model launches now defer independent judging and leave pending judgments for an ordinary launch."
---

The task-landing hook and startup sweep stay absent during a single-model launch.
The chosen worker is never substituted as its own independent pool judge.
