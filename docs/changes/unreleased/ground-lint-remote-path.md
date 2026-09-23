---
kind: fixed
title: a deliverable path on another machine no longer refuses the task
surface: [engine]
invalidates:
  - "`groundLint` refused a task whose deliverable named any absolute path outside its ground, whether or not that path was a directory on this machine. A path is now a place only when a directory along it exists here and is not the filesystem root, so a path on another host is left alone while a real folder outside the ground is still refused by name."
  - "`groundLint` carried a row in `complexityDebt` at 18. The reading of where a path stands moved into `standsOutside`, `placeOnThisMachine` and `repositoryHolding`, the function is under the ceiling, and the row is gone."
---

Work handed to a host reached over ssh writes its deliverable as an absolute
path on that host. The lint read every such path as a folder the task was
trying to stand in, refused it, and the proposer stopped handing the work out.
The guard, not the lint, is what stops a write into a folder that is not there.
