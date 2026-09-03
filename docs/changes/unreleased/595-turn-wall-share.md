---
kind: fixed
title: under a steward with a wall, inline head work is bounded by a share of the wall
pr: 595
surface: [chat]
invalidates:
  - "Inline work under a wall moved onto a task only on a ROUND count (the marks at 10, 20 and 40) or a WRITE count (two files or five write calls), so a turn that read and ran tests could spend five sixths of the run's wall inline and cross neither. It no longer can: an unattended session whose budget names a wall also bounds ONE turn's inline stretch at `Wall / turnWallShare` (internal/session/turnwall.go, the share is 3), and a turn past it takes the same handover road the marks and the write seam take, with its own line."
  - "The lines a person reads when their work is moved onto a task were four — the mark's `this has parts · …`, the write seam's `this is changing more than a quick edit · …`, the ceiling's `this is running long · …`, and the carry-on's. There is a fifth: `this has taken a third of the time · moving it to a task that can be checked before the wall`, and it is said only in a session with a wall."
  - "`internal/manual/chat/starting-aforge.md` said a long turn's work is moved when a second reader sees parts, when it has changed enough files, or when it has run past its own price — three ways, all of them counts. It now names the fourth and says what it needs: a budget with hours in it. `--max-cost` on its own states no clock and never triggers it, and a session somebody is sitting in front of has no wall to take a share of."
---

**Nothing decided wrongly, which is why nothing caught it.** The measured cell —
the `tox-4031` chat cell under `--yolo` on a 900 s wall — had its fix working in
the person's live checkout at about five minutes. The turn kept reading and
running tests inline; the write seam fired at round 32, with 147 seconds of the
wall left; the task it started spent 60 of those opening a worktree, and was
still running when the wall came down. Three changed files, no commit, no check,
no landing. Both governors that could have moved it earlier answer questions
about counts, and reading is free in any number by a rule that is right for the
turn it was written about.

So the clock is a third governor, and it exists only where there is a wall to
take a share of. **A third**, because the other two thirds are what the work
needs after it moves: a working copy opened (measured at 60 s), the job run, and
somebody who is not the model that did it reading the result. It is asked only at
a boundary the mark ladder passed over and never on a round spent watching work
already handed out, it opens its door once in a turn, and it is read on the
steward's own clock so the share and the wall cannot disagree. The handover road
is the existing one, so the principal's reading of the ending is unchanged: done
seals, stop seals with its reason, carry-on hands over, moving work holds.

**And the stretch is bounded by what is left as well as by the share.** The
run that sized the share (below) found the share alone was read off the whole
wall from the turn's own start, so a turn that began with 310 s of a 900 s wall
left was given its full 300 s and handed over with six seconds to go — the
measured failure again, done by the seam written to prevent it. The bound is
now the smaller of the share and what remained when the turn began less
`taskAllowance` (`gitRootPatience + auditDeadline`: a working copy opened and
one verdict, the two limits a task is already held to, not a third number), so
a turn that begins with less than that in front of it is moved at its first
boundary while there is still something to move into.

The number itself is settled by measurement rather than by the one reading:
`docs/design/turn-wall-share-doe/` is the record of the run, and a change to the
constant is a change to that table in the same commit.
