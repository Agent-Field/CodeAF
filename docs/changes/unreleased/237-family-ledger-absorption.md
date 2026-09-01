---
kind: fixed
title: A task that hands parts out lands what the whole family wrote, not the parent's slice of it
pr: 237
surface: [engine, chat]
invalidates:
  - "A task on a plain folder landed only what its OWN worker wrote. Its parts wrote into the same copy and their files stayed there: the mirror held all of them, the person's folder got one, and the task settled as done with the check passing against the mirror. A task's ledger now absorbs every landed part's paths before it lands, so the whole family's product comes home."
  - "`taskTree.comeHome` and `taskTree.landMirror` were said to ship `wrote` — the node's own list. They still read exactly one list and neither has changed; what changed is that the list handed to them is the family's (`internal/session/task_ledger.go`'s `absorbedLedger`, called on the three landing roads in `task_run.go`)."
  - "What a node settles with (`TaskNode.changed`, `leavings()`, the row's file citations, the checkpoint) was one worker's files. It is now the subtree's, on every road that settles a node — merged, kept, stopped, turned back at the gate. That is what makes the fold compose (what a part settles holding is what its parent absorbs) and what makes it outlive the run: an accept or a re-audit hours later has nothing else to read."
  - "`taskTree.comeHome` and `keptWork` were called from a dozen places, each holding its own list. Every road now goes through `landHome` or `keepHome` (`internal/session/task_ledger.go`), which finalize the ledger and then land or keep it — the ordinary finishing line, the threshold's, the gate's three refusals, a ground that moved, a person's accept, and a late verdict."
  - "`TaskNode.workingCopy` rebuilt a tree for a node with no branch as `{dir, in place}` — no ground, no mode. A MIRROR has no branch either, so `comeHome` read the rebuilt tree as an in-place task and returned having done nothing: an accepted or re-audited folder family laid NOTHING back over the person's folder, and the check on one restored an empty world. The rebuilt tree carries the recorded ground and mode, so a folder family lands the same way whenever it is settled. Every other mode still lands in place, unchanged."
  - "`landingFilesFor` built a fresh dedup map per part and scanned the node's own list once per part path. It is one set and one pass — and it now reads the parts from the graph and the node's own half as what is left, which makes it safe to call on a ledger that has already absorbed them. A path both a node and a part wrote is filed under the parts rather than under the node, because the second reading of a folded ledger cannot tell the two apart and an attribution that drifts with every re-audit is worse than one that is stable; the path is named in the checker's packet, staged and restored either way."
  - "`Agent.groundShift` was asked about whatever list its caller happened to be holding, which for a divider was its own worker's slice. It folds the family's ledger itself, so the question — has anything else landed in these files while this ran — is asked about what would actually ship."
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

And it is written onto the node rather than computed at the merge, because a
landing is not a moment — a family that lands needing a look is settled by a
person in the morning, and that road has nothing to read but what the first
landing wrote down.
