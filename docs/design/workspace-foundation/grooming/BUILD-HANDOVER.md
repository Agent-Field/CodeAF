# The hand-over press: held for the turn that reads it

2026-09-11. Lane `codex/personal-handover-race` (Claude Code Opus on Spark), base
`e373ab411` on `codex/personal-ai-backend`. Round 1: `42b3fd4d7` (reviewed,
approved, merged by the coordinator). Round 2 (the review's non-blocking findings):
the commit that adds this file. Not merged by the lane.

## Outcome

"Let aforge decide" (`HandUnverifiedToModel`, `task_audit.go`) hands one card's
question to the model. It is now held until a request has carried its note, and the
turn that read it is the one whose end hands it back. A press that no turn is going
to read comes back to the person at once: a stopped turn, an abandoned one, a closed
session, a picture answered by another model. A second press while the first is
held answers `already handed to aforge`, and two racing presses cannot both be taken.

## The defect (round 1)

Reported as two flaky tests: `TestHandingTheSameDecisionOverTwiceSaysItIsAlreadyHandedOver`
and `TestHandingAYourCallToTheModelAndItsResolveAreOneRoad`. Both were two races.

**Product race** (lines at `e373ab411`). The landing note wakes a turn. When a press
lands after that turn's last request, its note waits on the steering queue. The
turn's clean-up ran the end-of-turn floor (`handBackUnsettled`, `agent.go:1593`)
outside `a.mu` and before the end drain (`agent.go:1627`). So it handed back a press
it never read. The same clean-up then drained the note as unanswered and woke the
next turn (`agent.go:1658`). That turn was told the person had asked it to decide,
while the node said the person held the decision. A second press was taken as fresh
and sent the model a second copy. With logging added, the floor handed back an
unread press in 503 of 600 runs; the tests hid it by the order of their reads. The
double-press check (`task_audit.go:2410–2427`) was also read, unlock, write.

**Observation race.** The tests read momentary state while the session's own woken
turns legitimately read the press, ended without settling (scripted model), and
handed it back. The failing count was a queue that turn was draining.

| Run | Failures |
|---|---|
| base, `-count=200`, two tests | 6 / 400 |
| base, `-race -count=200`, two tests | 22 / 400, no data races (logic race) |
| round 1, `-race -count=1000`, four tests | 0 / 4000 |
| the round-1 regression on base product code | 50 / 50 |

## The fence

- **L1, fenced writes.** Each press is a ticket on the node (`handPress`,
  `handUnread`). Its note carries the ticket (`userMessage.handsOver`). A press
  becomes read only at the drain before a request (`drainSteering`,
  `markHandOversRead`). A turn-end or abandon drain parks it on `Agent.handsUnsent`
  for the next turn's first drain. When no turn starts, it is given back
  (`orphanedHandsLocked`, `giveBackHandOvers`). Every late write compares the ticket.
- **L2, recheck at the effect.** The floor skips an unread press under the graph lock
  it writes under (`task_run.go:4099`). The press's state check and its write are one
  claim (`TaskNode.handOver`).
- No `a.mu`→`graph.mu` nesting: every ticket write runs after `a.mu` is released.

## Round 2: the review's findings

| Finding | Test (fails first) | Mutation → result |
|---|---|---|
| read-mark ticket check | `TestALateActOnATakenBackPressLeavesTheNextPressAlone` | `if true` → FAIL 3/3 |
| give-back ticket check | same | drop `handPress == ticket` → FAIL 3/3 |
| interrupt give-back | `TestAPressComesBackWhenItsTurnIsStopped` | `_ = orphaned` → FAIL 3/3 |
| abandon give-back | `TestAPressComesBackWhenItsTurnIsAbandoned` | `_ = orphaned` → FAIL 3/3 |
| closed-session give-back | `TestAPressOnAClosedSessionComesStraightBack` | drop call → FAIL 3/3 |
| picture-turn give-back | `TestAPressDuringAPictureAnswerComesBackWhenItEnds` | `_ = orphaned` → FAIL 3/3 |
| disowned turn steals | `TestADisownedTurnLeavesTheNextTurnsDecisionAlone` | failed on `42b3fd4d7`; `if true` → FAIL 3/3 |
| abandon is the turn's end | `TestWhatAnAbandonedTurnWasDecidingComesBackAtTheAbandon` | drop floor → FAIL 3/3 |
| floor skips unread (round 1) | `TestAHandOverThatMissesATurnIsHeldForTheTurnThatReadsIt` | drop skip → FAIL 3/3 |

Mutations ran in a throwaway detached worktree, never this tree.

- **Disowned turn.** Its clean-up now asks `turnIs(seq)` before the floor
  (`agent.go:1602`). Skipping it there would strand whatever the abandoned turn read,
  because its goroutine may never unwind. So `Abandon` runs the floor once it lets go
  (`abandon.go:149`), beside the orphan give-back.
- **Out-of-date card.** Reading `notice` after `enqueueNote` does not close it: a
  give-back can still emit between the press's read and its emit. The press now emits
  *before* it enqueues. Every road that gives this press back acts on its note, so it
  is downstream of the enqueue, and its update can only follow the press's. No
  deterministic regression: the interleaving needs a step inside `enqueueNote`, and
  the package has no seam there.
- `TaskNode.wasHandedOver` is deleted; its rule now sits on the `handed` field.
- Manual: `task-controls.md` names the picture case and quotes `aforge is deciding`.

## Known residue (not fixed, fail-safe direction noted)

- **Check-then-act windows.** A turn abandoned between `turnIs` and its floor, or a
  press read by a new turn in the instant between `Abandon`'s unlock and its floor,
  can still be handed back early. The fail-safe is that the person holds the card.
- **Two writers on one card.** A take-back from another window racing a press can
  still end on the older update. Only a per-node update sequence closes that.
- **An orphaned note stays in the transcript.** The next turn may resolve the node
  while the person holds it: `resolveUnverifiedBy` (`task_audit.go:2345`) never asks
  who holds the decision.
- **Sub-task nodes.** The press's note goes to the conversation, but the floor that
  hands it back is the parent worker's (`readsTheDecisionLocked`, `task_run.go:4175`).
- **A declined wake** (spend rail, work stopped) leaves `aforge is deciding` on an
  idle session until the next turn ends.

## Next lane: the settle-policy road

`handToModelOnAuto` (`task_run.go:4049`) marks the model as the decider by policy
under `task.settle = auto`, and in every run with nobody watching. It carries no
ticket, so round 1's race lives on there. A landing note that arrives after a
running turn's last request is handed back by that turn's floor, which never read it.
The same end drains the note and wakes a turn told to settle the work, while the
card shows its answers again. Suggested shape: have `handToModelOnAuto` issue a ticket, put it
on the landing message in `postTaskMessage` (`task_run.go:3673`), and reuse
`handsUnsent`, `markHandOversRead` and `giveBackHandOvers` unchanged. Two cases need
care. The note can go to a parent worker (`taskNoteReaders`), whose own drain must
mark it read. A delivery nobody takes (`releaseNote`) must give the ticket back.
Regression shape: `TestAHandOverThatMissesATurnIsHeldForTheTurnThatReadsIt` with
`AskConsent` off, and the landing admitted from inside the scripted request.
