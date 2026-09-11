# The hand-over press: held for the turn that reads it

2026-09-11. Lane `codex/personal-handover-race` (Claude Code Opus on Spark), base
`e373ab411` on `codex/personal-ai-backend`. Round 1: `42b3fd4d7` (reviewed,
approved, merged by the coordinator). Round 2 (the review's non-blocking findings):
`79cc7bb65`, which adds this file. Round 3 (the second review's stranding and panic):
the commit after it. Not merged by the lane.

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
  `handReader`). Its note carries the ticket (`userMessage.handsOver`). A press
  becomes read only at the drain before a request (`drainSteering`,
  `markHandOversRead`), which stamps the reading turn's number on it. A turn-end or
  abandon drain parks it on `Agent.handsUnsent` for the next turn's first drain; when
  no turn starts it is given back (`giveBackHandOvers`). Late writes compare the ticket.
- **L2, recheck at the effect.** A turn's floor (`handBackUnsettled(seq)`) takes
  back only a press that turn read, checked under the graph lock it writes under. The
  press's state check and its write are one claim (`TaskNode.handOver`).
- **A drain belongs to the live turn.** `drainSteering` refuses unless its hub is
  the session's; the hub is set and cleared in the same hold as `turnSeq`.
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

Mutations ran in a throwaway detached worktree. A disowned turn's clean-up asks
`turnIs(seq)` before its floor, so `Abandon` runs that turn's floor itself (its
goroutine may never unwind). The press emits its card update *before* it enqueues:
every give-back acts on the note, so its update can only follow. Reading `notice`
after `enqueueNote` would not close it, and no deterministic regression exists without
a seam inside `enqueueNote`. `TaskNode.wasHandedOver` is deleted.

## Round 3: the second review

| Finding | Test (failed on `79cc7bb65`) | Fix |
|---|---|---|
| let-go drain strands a press | `TestALetGoTurnsDrainTakesNothingFromTheQueue` (queue emptied) | drain checks `a.hub == hub` |
| let-go floor after `turnIs` / after `Abandon`'s unlock | `TestALetGoTurnsFloorLeavesAPressTheNextTurnRead` (`"person"`) | floor checks `handReader == seq` |
| let-go floor on a policy-held landing | `TestADisownedTurnLeavesAPolicyHeldLandingAlone` (`turnIs` → `true`: FAIL 3/3) | clean-up asks `turnIs(seq)` |
| picture turn abandoned: `close of closed channel` | `TestAPictureAnswerLetGoOfLeavesTheNextTurnAlone` (panic) | picture turns are numbered; clean-up checks it |

Cancelling before `Abandon` unlocks was rejected: a loop can pass its cancel check just
before it and still drain. The picture turn's `done` closes once under the number, the
ordinary clean-up's guard. A node held by policy has no reader stamp, so its windows remain.

## Known residue (not fixed, fail-safe direction noted)

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

## Round 4: the settle-policy road

Lane `codex/personal-settle-race`, base `9f72babb2`. `handToModelOnAuto` now makes the
press's own write (`TaskNode.handsOverLocked`, shared with `handOver`) and answers its
ticket. `reportTaskNode` puts it on the landing note (`postTaskMessage` →
`userMessage.handsOver`) and gives it back when no note carries it. That covers a refused
duplicate, a delivery nobody takes (`releaseNote`), and a node that moved on. A node
already handed over mints nothing, because a second ticket would strand the first.
`handsUnsent`, `markHandOversRead` and `giveBackHandOvers` are unchanged. A parent
worker drains the note with the same `drainSteering`, so its own request marks it read
and its own floor hands it back.

| Test (fails first) | Mutation → result |
|---|---|
| `TestALandingThatMissesATurnIsHeldForTheTurnThatReadsIt` | base product code → FAIL 50/50; policy mints no ticket → FAIL 3/3; note carries no ticket → FAIL 3/3 |
| `TestAPiecesLandingIsHeldByTheWorkerThatReadsIt` | worker's drain does not mark → FAIL 3/3 (passes on base) |
| `TestALandingNobodyTakesStaysWithThePerson` | base → FAIL; drop the give-back → FAIL 3/3 |
| `TestALandingAnnouncedTwiceIsHandedOverOnce` | drop the `handed` guard → FAIL 3/3 |
| `TestALetGoTurnsFloorLeavesALandingTheNextTurnRead` | base → FAIL; floor skips only unread → FAIL 3/3 |

A drain that belongs to the live turn needs no new test: `drainSteering` refuses a
let-go hub before it reads the queue, so the note's kind cannot matter
(`TestALetGoTurnsDrainTakesNothingFromTheQueue`). A policy hold now has a reader stamp.

**Lock discipline.** The ticket write is under `graph.mu` alone, in `handToModelOnAuto`,
reached from `reportTaskNode` with no lock held. `settlePolicy`'s `a.mu` is released
before `graph.mu` is taken. The note field is set on a local value before
`deliverTo`. The read mark is where it was (after `a.mu` in `drainSteering`). The
give-back is `reportTaskNode`'s deferred call, after delivery, with no lock held.

**Behaviour that moved.** `d` on a card the policy already handed over is a second
hand-over: `already handed to aforge`, nothing sent (it used to queue a second note).
`TestHandingOneToTheModelLeavesTheNodeWhereItIs` now takes it back first.

**Residue.** A piece landing after its parent worker's last request is given back at the
worker's turn end, because a task never wakes. The runner's resume turn then reads the
note while the person holds it, which is the same as base. Closing it needs the resume
turn to count as "started" for `orphanedHandsLocked`, plus a give-back at `Close`.
Race: the 25 hand-over and floor tests ran `-race -count=200`: 5000/5000 PASS, no data races.
