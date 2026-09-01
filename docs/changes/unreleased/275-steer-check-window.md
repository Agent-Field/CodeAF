---
kind: fixed
title: steering a task under check is refused with the reason instead of swallowed
pr: 275
surface: [engine, chat]
invalidates:
  - >
    It was believed that a line steered into a running task always reached its
    worker — SteerTask answering nil meant delivered. During the whole
    "checking what it left" phase that was false: the worker agent was still
    open but done reading forever, the line was taken onto its queue, echoed by
    the room as said, and closed over unread when the check ended. Now the
    runner withdraws the room's speaker at its loop's last read and SteerTask
    refuses during the check with "task N is being checked — nobody is in there
    to read your line until the check lands", which the steer guard renders
    with the words kept in the box.
  - >
    taskRoom.speaker() was cleared only at room close. It is now also withdrawn
    by runTaskChild the moment its tail loop decides it is done, and the queue
    is asked once more on the way out so a line that races the withdrawal is
    answered by one more turn.
---

The swallow was observed live on 2026-09-01 (#273): a person asked a checking
node a question, the room drew it as said, and the words reached no journal and
no model. The state is honest about the node, never about who is inside it —
the refusal now comes from the phase, and the door and the runner say it from
both sides so they cannot disagree.
