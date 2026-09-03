# The share of the wall a turn may spend inline: the experiment (2026-09-03)

This folder is the record of the run that settles `turnWallShare`
(internal/session/turnwall.go), the fraction of an unattended run's wall that ONE
turn may spend working inline before what is left of it moves onto a task
([#546](https://github.com/Agent-Field/aforge-v2/issues/546), landed as the
constant at 3). The law and the mechanism are on the issue and in the code; this
is the evidence for the number.

## The question

Under a steward with a wall, a turn hands over once its inline stretch passes
`Wall / turnWallShare`. Everything else about the seam is fixed — the road, the
line, the once-per-turn claim, the boundaries it is asked at — so the only free
parameter is the divisor, and it trades one failure against the other:

- **Too large a share** (the divisor too small) is the measured failure that
  opened #546: the tox-4031 cell under `--yolo` on a 900 s wall spent five sixths
  of it reading and running tests inline, moved the work with 147 s left, spent 60
  of those opening the task's working copy, and hit the wall with three changed
  files, no commit, no check and no landing.
- **Too small a share** (the divisor too large) converts turns that were about to
  finish. The road can decline — the running model and the mark's own reader have
  to agree nothing is left — but each ask is two model calls, and a seam that
  fires early on every run pays for them on every run.

## The two arms

| Arm | `turnWallShare` | Inline stretch allowed on a 900 s wall |
| --- | --- | --- |
| A | 3 (shipped) | 300 s |
| B | 2 | 450 s |

Arm A is the branch as merged, byte for byte. Arm B is a one-line scratch commit
on top of it changing the constant and nothing else; the note's wording is not a
factor and is left as it is in both.

## The cells

`attrs`, `tox` and `reef` from the canary pool, each run through the chat door
under `--yolo` with a wall, at both arms. The rig, the pool and the driver are the
gatekeeper's (issue [#407](https://github.com/Agent-Field/aforge-v2/issues/407));
`tox` is the cell the defect was measured on and is the one that must move.

## The method

**Pareto on ends-done-before-wall, then cost.** An arm that ends more cells at
done before the wall wins outright; arms that tie on that are separated by cost.
Wall time is recorded but is not a tiebreak — a run that ends early because it
gave up is not a better run — and `files` is recorded because the failure this
seam exists to prevent shows up there as changed files with nothing landed.

## Results

Filled from the rig by this lane's coordinator; the constant moves only if arm B
wins on the method above, and a change to it is a change to this table in the same
commit.

| share | cell | ends done before wall | wall s | cost | files |
| --- | --- | --- | --- | --- | --- |
| | | | | | |
