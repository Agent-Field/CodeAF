---
kind: fixed
title: Stop delegated work and helpers at conversation limits
pr: 1907
surface: [chat, engine]
invalidates:
  - "Conversation Stop and time limits previously left delegated runs or helper retries active. Owned work now stops before admission can reopen."
  - "History could show a stale record beside the live task, directing users away from its Stop control. The live task now appears once."
---

Conversation Stop and `--max-hours` now cancel owned delegated runs, background
jobs and automatic helpers. Stopped runs cannot begin another checking pass or
revive from a pending start. Page Stop also prevents a pending isolated landing
from continuing; a home merge already in progress retains its actual result.
Requests already sent may still report their final usage.

Task history keeps the live plan row instead of re-adding its saved record, so
opening the task reaches its current room and Stop control.
