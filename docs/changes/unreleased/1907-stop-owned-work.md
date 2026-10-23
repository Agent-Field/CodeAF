---
kind: fixed
title: Stop delegated work and helpers at conversation limits
pr: 1907
surface: [chat, engine]
invalidates: []
---

Conversation Stop and `--max-hours` now cancel owned delegated runs, background
jobs and automatic helpers. Stopped runs cannot begin another checking pass or
revive from a pending start. Page Stop also prevents a pending isolated landing
from continuing; a home merge already in progress retains its actual result.
Requests already sent may still report their final usage.
