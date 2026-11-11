---
kind: fixed
title: a finished program stops its host watcher before returning
pr: 0
surface: [engine]
invalidates:
  - "Was true: a program could return while its parent-process watcher was still running, allowing that invocation's watcher or grace callback to outlive it. Is true now: finishing the invocation cancels and joins its existing watcher before returning. A missing parent's stop and grace behavior are unchanged."
---
