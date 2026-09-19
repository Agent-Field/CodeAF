---
kind: fixed
title: a long declared check says why it was refused
pr: 1283
surface: [chat, engine]
invalidates:
  - "A declared check longer than the admission limit was refused as though it were not one rerunnable command, even when it was one real command. The refusal now names length as the cause and says how long the check actually is, so the person shortens that check instead of guessing which one was too long."
  - "The audit door silently dropped a long declared check even after the task had recorded it. It now keeps that command in the checker contract, so the checker is not later told its own declared check was absent from the door."
---

The proposal limit still keeps an invalid check short enough to quote readably in a
refusal. Length limits admission of a new check, not whether an already recorded
check belongs to the task.
