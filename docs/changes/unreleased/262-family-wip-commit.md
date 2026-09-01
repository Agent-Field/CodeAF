---
kind: changed
title: a parent commits its work to the family branch before it hands out the parts
pr: 262
surface: [engine]
invalidates:
  - "Parts of a division were cut from a HEAD holding none of the parent's mid-run work — workers never commit and the harness only committed at the landing, so a repro or a failing test the parent had built was on nobody's disk. The harness now stages the parent's ledger and commits it onto the FAMILY BRANCH before the first part is admitted (`the work so far on <title>, before its parts were handed out`), and the parts branch from that commit."
  - "Each part sealed the parent's tree independently when its worktree was cut, so siblings prepared minutes apart got different worlds. The family's world is frozen ONCE at the division, written onto every part at admission and carried on the checkpoint, and `snapshotRung` carves from it without sealing. Work the parent does after the split reaches no part."
  - "`sealGroundWork` staged every seal through one shared `.git/aforge-ground-index`, so concurrent siblings raced on the file. Each seal now gets an index of its own."
  - "`sealGroundWork` answered the empty string for a clean tree AND for every git failure, so a locked index or a broken repository silently carved a child from HEAD without its parent's work. It now answers `(string, error)`, and `groundRung.carve` carries an error so a rung that reached a ground and could not make its world stops the ladder in git's own words instead of falling through to a lesser one."
  - "`commitTaskWorkAs` (behind `commitTaskWork`) threw away the `git commit` error and answered only the staged paths, so a commit that never ran was indistinguishable from one that did. It now answers the paths, the SHA and the error. `commitTaskWork` still discards them and `taskTree.comeHome` is unchanged — what a landing owes a failed commit is issue #255's seam."
  - "A checkpoint that ran was taken as a checkpoint that carried. `stageTaskWork` steps over a path git refuses one at a time, which is right for a landing and wrong for a family, so the freeze now asks the tree (`unheldLedgerPaths`) whether every ledger path that exists and is not ignored really went in, and refuses the division when one did not."
  - "The first answer to the freeze stood the universe rung down for every part, which would have taken a `.env`, an installed dependency tree and a dev database away from parts of a family whose parent had them. The fork now honours the freeze instead (`openForkAt`): the branch is opened AT the frozen commit inside the fork and untracked-and-not-ignored files are cleaned, so the tracked world is the freeze exactly and everything git cannot see is still there. THE FREEZE IS OVER WHAT GIT CAN SEE, which is the edge the ledger, the landing and the mirror all already draw."
  - "A division could only be refused for the evidence floor, a full lane, the fan cap, the reviewer, or overlapping scopes. It can now also be refused with `refused:freeze` when the family's own tree will not take the commit its parts have to start from — nothing is handed out rather than handed out onto a world the parent does not have."
---

The parent-stays law makes the parent the coordinator holding the findings, and until
now the parts could not see the findings' artefacts — only whatever prose the parent
restated in their briefs. For code work that is the difference between a part that can
run the failing test and one that has to be told about it.

Committing to the family branch rather than sealing per child is what makes the parent's
work HISTORY instead of scaffolding: the seal moves no ref and is rebased back out at each
part's landing, so a `git log` of the family branch showed none of it and a crash between
the division and the landing left it in an object nothing referenced. Because the world is
now on a branch, a part carries no machine commit to lift out and its landing is an
ordinary merge onto a shared ancestor.

A family standing in the person's own directory — `here`, or a folder with nowhere else to
stand — freezes nothing and commits nothing: its parts go on sharing the directory, which
is what "here" already meant. The guard is structural rather than a `.git` check, because
the person's own repository has one.

The freeze-once plumbing is #246's shape, kept and re-fed with this commit instead of a
dangling `commit-tree` object, and stored per child at admission rather than as one
mutable field on the parent.

The free hands a division holds are now one value with one release (`divisionHands`)
rather than a counter every ending in `divideOnce` had to remember to decrement — the
ending that forgets is a lane the person's cap never gives out again.
