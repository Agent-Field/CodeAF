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

## What the first pass found: the rig gave the run no wall

The first pass of both arms produced no `ran long` row in any cell, and reading
the cells' launch lines said why. The canary's chat driver starts
`aforge chat -yolo -one-model -model … -max-cost <cap>` and passes no
`-max-hours`, so `Budget().Wall` was zero in every chat cell and the share seam,
which is asked only under a wall, never fired. Every "wall" the canary had recorded
for a chat cell so far was the tmux clock killing the pane from outside, never the
run's own wall — which also means the tox-4031 stretch that opened #546 was
measured against a clock the run could not see.

The pass was rerun through a wrapper binary that appends `-max-hours 0.25` when
the first argument is `chat` (a 900 s wall, matching the table above), with the
rig itself untouched. Those are the rows below. The first, wall-less pass is kept
beside them on the rig as the control: with no wall, both arms are the shipped
seam with the share switched off.

## Results

Both arms, three cells each, chat door under `--yolo`, a 900 s wall (2026-09-03,
runs `20260903T204602Z-b07c3dd51-doe546-wall-share3` and
`20260903T211119Z-15a2b6ef2-doe546-wall-share2` on the canary rig, tables posted
on #407). `seam` says which door moved the work out of the turn, if any.

| share | cell | ends done before wall | wall s | cost | files | seam |
| --- | --- | --- | --- | --- | --- | --- |
| 3 | reef | no | 903 | $0.085 | 2 | split at 230 s; task back at 589 s; **share at 894 s** on a turn begun at 593 s |
| 3 | attrs | no | 903 | $0.130 | 0 | parts at 239 s, three tasks; one still running at the wall |
| 3 | tox | no | 902 | $0.096 | 2 | split at 199 s; task done 462 s later; its check still running at the wall |
| 2 | reef | yes | 249 | $0.012 | 1 | none; inline, done at 202 s |
| 2 | attrs | no | 903 | $0.091 | 1 | split at 287 s; task done 360 s later; its second check cut by the wall |
| 2 | tox | yes | 202 | $0.010 | 1 | none; inline, done at 181 s |

The control pass with no wall (same binaries, same cells, the rig as it stands):
arm A ended 0 of 3 before the tmux clock, arm B 1 of 3 (reef, 114 s), and the
share seam fired in none of the six.

## The read: the number was not what this run measured

Counted by the method, arm B wins 2–0 on ends-done-before-wall. But neither of
those two cells reached EITHER arm's share: they ended inline at 181 s and 202 s,
under the 300 s that arm A allows, and the difference between the arms is the
running model's variance on a cell, not the constant. **`turnWallShare` stays at
3.** The run is a null result on the number and a measured result on three other
things:

1. **The split seam gets there first, and that is the rig's limit.** Every cell
   that ran long moved its work at 199–287 s through writeseam.go's door, before a
   third of a 900 s wall. On a short wall the share is close to unreachable; the
   wall it was written for is the long one, where a turn can read and run tests
   for an hour without crossing the write seam's file count. The canary's cells
   run on a fifteen-minute wall, so the share's own case is outside what this rig
   can measure, and the null result on the number is a null result at that wall.
2. **The share is taken off the whole wall from the turn's own start, so a turn
   that begins late gets a third the wall no longer has.** The one firing in six
   cells was reef: a turn that began with 310 s left ran its full 300 s and handed
   over at 894 s, six seconds before the wall — the exact shape #546 opened with.
   The seam now hands over only what can still be checked before the wall: past
   the share it fires only while at least a task's setup and check (`taskAllowance`
   in turnwall.go, the two limits a task is already held to) is left, and a turn
   with less runs to the wall inline with nothing spent asking. Landed in the same
   pull request with reef's 894 s as the before in
   `TestATurnThatCannotBeCheckedBeforeTheWallIsNotMoved`.
3. **What actually ran to the wall was the task's check.** In three of the six
   wall cells the task had finished and the check that reads it — the full suite,
   run once by the task and again by each check, 150 s a time on these projects —
   was still running when the wall came down. The share buys a task time to be
   checked; on a 900 s wall the check itself is what there is not time for.
