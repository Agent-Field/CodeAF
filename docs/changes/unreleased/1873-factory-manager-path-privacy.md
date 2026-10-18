---
kind: fixed
title: Keep manager session paths on the worker
pr: 1873
surface: [engine, remote]
invalidates:
  - "Reader job and queue responses included the manager conversation's session file path in desk.talk; reader responses now omit it, while authenticated worker pull retains the session reference for resuming the manager."
---
