---
kind: fixed
title: a watch that fires wakes the conversation, and only the firing does
pr: 102
surface: [chat, engine]
invalidates:
  - "A watch's news waited for the next thing you say, and #95's manual said so in as many words. No longer true: the tick that ENDS a watch — `until` matched, the output went quiet, three failures in a row — now starts a reply by itself, the way a background job's exit does. Its ordinary ticks still wait for a boundary."
  - "A conversation waiting on a watch went quiet until the person typed, which #95 described as a gap in the wake lane. The gap is closed; nothing about carrying on changed."
  - "The `watch` tool's description said its updates are batched at the turn boundary and stopped there. It now also says the ENDING comes back on its own, and `jobs output` moved into the `jobs` clause rather than getting its own sentence."
---

A watch on `gh pr checks` saw the pending count go to zero, and the session that
had learned it said nothing until somebody came back and typed. The delta lane
was right for a delta — a log that grew by two lines is telemetry with a complete
log behind it — and wrong for the one note that is not a delta: a watch that has
fired has answered the question it was started for and will never speak again.

`watchTick` already knew which was which; it returned "this was the last tick" to
break its own loop and the loop threw the bool away. It now reaches the lane's
door, and the firing is queued as an owed note that starts a turn.
