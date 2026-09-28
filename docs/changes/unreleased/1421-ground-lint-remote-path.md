---
kind: fixed
title: a deliverable path on another machine no longer refuses the task
pr: 1421
surface: [engine]
invalidates:
  - "`groundLint` refused a task whose deliverable named any absolute path outside its ground, whether or not that path was a directory on this machine. A path is a place only when the directory it names exists here and is not the filesystem root. A directory merely somewhere along the path does not count: on macOS `/home` is a symlink to a directory that is there, so a path from another host that begins `/home` was still read as a place and still refused."
  - "`groundLint` carried a row in `complexityDebt` at 18. The reading of where a path stands moved into `standsOutside`, `placeOnThisMachine` and `repositoryHolding`, the function is under the ceiling, and the row is gone."
---

Work handed to a host reached over ssh writes its deliverable as an absolute
path on that host. The lint read every such path as a folder the task was
trying to stand in, refused it, and the proposer stopped handing the work out.
The guard, not the lint, is what stops a write into a folder that is not there.
