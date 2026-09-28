---
kind: fixed
title: hedge fixtures hold the primary before its first visible word
pr: 1627
surface: [engine]
invalidates:
  - "The shared-choice and losing-arm accounting fixtures assumed a rescue would precede primary progress. A delayed watcher could instead let the primary finish or commit and reach an artificial permanent stall. Both fixtures now order their primary with a signal before its first visible word."
---

Addresses #1630 without changing routing policy. Existing choice, wire order,
winner and accounting assertions remain. Failure diagnostics include request
and phase details to distinguish scheduling from routing defects.
