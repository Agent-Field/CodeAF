---
kind: fixed
title: A task that hands parts out lands what the whole family wrote, not the parent's slice of it
pr: 237
surface: [engine, chat]
invalidates:
  - "A task on a plain folder landed only what its OWN worker wrote. Its parts wrote into the same copy and their files stayed there: the mirror held all of them, the person's folder got one, and the task settled as done with the check passing against the mirror. A task's ledger now absorbs every landed part's paths before it lands, so the whole family's product comes home."
  - "`taskTree.comeHome` and `taskTree.landMirror` were said to ship `wrote` — the node's own list. They still read exactly one list and neither has changed; what changed is that the list handed to them is the family's (`internal/session/task_ledger.go`'s `absorbedLedger`, called on the three landing roads in `task_run.go`)."
  - "What a node settles with (`TaskNode.changed`, `leavings()`, the row's file citations, the checkpoint) was one worker's files. It is now the subtree's, which is also what makes the fold compose: what a part settles holding is what its parent absorbs."
  - "The manual said a plain folder gets \"the files the task wrote … laid back over it by name\", and *What a finished task brings home* said nothing about a divided task. Both now say the family's files land, that a part's file needs no second naming on a `files:` line, and that a part which did not finish is not on the list."
---

The ledger is the contract of what ships — `stageTaskWork` stages it, `landMirror`
lays it — and a divider's ledger had a hole in it. On a repository ground git
closed the hole underneath, because a part's work arrives as commits on the very
tree the parent merges; on a folder ground there was nothing underneath, and the
loss was silent both ways round: the card said done, and the check was run
against the tree that DID hold everything.

The fold is at the landing rather than in the copier, and it happens AFTER the
check on purpose: the checker's packet has to keep "Files it wrote" a true claim
about this node, with its parts named in a sentence of their own.
