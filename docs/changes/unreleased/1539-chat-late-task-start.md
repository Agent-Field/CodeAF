---
kind: fixed
title: a late task start is an uncertain receipt
pr: 1539
surface: [chat, remote]
invalidates:
  - "A task start that did not answer within the connection's wait was reported as a refusal. It is now an uncertain receipt; the task may already be running, stays visible through its task updates, and is not retried automatically."
---
