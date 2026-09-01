---
kind: changed
title: a repository task inherits the whole world, its .env included, and still comes home as a merge
pr: 183
surface: [engine, chat]
invalidates:
  - "#162 wrote that a repository ground takes the snapshot rung and that a furrow universe of one would turn every branch landing into a copy landing. Neither holds: a repository task is grounded in a universe whenever furrow can fork its folder, and it still lands as a merge on its own `task/…` branch — the branch is cut inside the fork and fetched home at the landing."
  - "A repository task could not inherit anything `.gitignore` covers — a `.env`, an installed `node_modules`, a dev database — and both `groundladder.go`'s header and the manual said so outright. It inherits all of them on the universe rung; the manual's new section *Does my task see my .env* says when it does, when it does not, and how to tell which happened."
  - "`groundladder.go`'s law was THE LADDER CHOOSES THE WORLD; IT NEVER CHANGES THE PROMISE, and its consequence was that one rung was closed to one promise. The law is now that the ladder chooses the world and THE LANDING keeps the promise: any rung may reach any ground, and what a rung may never do is change how the work comes home."
  - "`taskTree.comeHome` merged the branch and then unregistered the worktree, deleted the branch and dropped the fork inline. Those moves are `taskTree.releaseLanded` now, and the merge is preceded by `taskTree.carryBranchHomeLocked` — a `git fetch` of one branch from the node's own copy, and nothing at all for a node working in a worktree."
  - "A task's checkpoint recorded its ground and its mode. It records four more, additively: `groundRung`, `groundSeal`, `groundBase` and `groundUniverse`. A landing outlives the run that made the world, so a task accepted an hour later or resumed after a crash now lands by the road that actually made its world."
  - "Attaching a folder to furrow left a `.furrow/` directory sitting in the person's `git status`, and the snapshot rung's machine commit could carry it. The name goes into that repository's `.git/info/exclude` — local to their checkout, never committed — and the machine commit excludes it."
  - "`restoreFromBranch` cut the check's clean copy from the ground repository. It cuts from whichever repository holds the branch (`taskTree.branchHolder`), which until the landing is the fork itself for a universe-grounded node."
  - "Any folder furrow would take could be forked. A ground whose `.git` is a FILE — a linked worktree — never is: a byte-exact copy carries that file's absolute path, so the fork's commits, branch and HEAD would all be written into the repository it names and would move a checkout somebody is standing in. Such a ground takes the snapshot rung."
---

The ground law's last rung. #142 said a task inherits its parent's world as it
is; #162 built the ladder and could hand a repository everything except the part
git was told not to look at, which is the part that decides whether a worker can
run the tests it was asked to run.

What made that seem unavoidable was reading the promise as a property of the
GROUND — a repository must be a worktree — rather than of the LANDING. A
universe of a repository is that repository, `.git` and all, so the branch can be
cut inside the fork and one fetch at the end puts it where the merge has always
happened. The person sees the same branch, the same merge, the same diff.
