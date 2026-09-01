---
kind: fixed
title: steering a task with nobody inside is refused with the reason instead of swallowed
pr: 275
surface: [engine, chat]
invalidates:
  - >
    It was believed that a line steered into a running task always reached its
    worker — SteerTask answering nil meant delivered. From the worker's last
    read until retire (the whole check, and every stopped or errored exit) that
    was false: the worker agent was still open but done reading forever, the
    line was taken onto its queue, echoed by the room as said, and closed over
    unread. Now runTaskChild withdraws the room's speaker on every road out (a
    defer, plus an earlier withdrawal at the tail loop's last read that asks
    the queue once more and puts the speaker back for the answering turn if a
    line raced in), the nil-check and the enqueue sit under one hold of the
    room's lock (taskRoom.steerIn), and SteerTask refuses during the check with
    "task N is being checked — nobody is in there to read your line until the
    check lands", read off the node's graph-locked life word, not the beat
    file.
  - >
    The steer guard used to offer "[r] revive and send" unconditionally. On a
    task that is STILL RUNNING — refused mid-check, or while its work lands —
    the guard now opens "<title> cannot read this right now — " and offers only
    m and esc, because reviving live work manufactures a duplicate task. Which
    of the two guards rises is the ENGINE's word, not the page's: the two
    "nobody is in there" refusals carry session.ErrNobodyToRead and the surface
    matches it with errors.Is. A page's own `done` is its record of a close it
    may not have been told about yet, and reading "still running" off it
    withheld revive from a node the engine had just called finished.
  - >
    TaskNode.spend() used to read the worker's unfolded cost off the room's
    speaker. The speaker now leaves before the money is folded, so the price
    comes off a separate bill (taskRoom.bill) that stands from worker start
    until retire folds it — a card's figure no longer drops and rebounds
    across the check.
---

The swallow was observed live on 2026-09-01 (#273): a person asked a checking
node a question, the room drew it as said, and the words reached no journal and
no model. The state is honest about the node, never about who is inside it —
the refusal now comes from the node's own phase word and the room's lock, said
from both sides so they cannot disagree.
