---
kind: fixed
title: "a review conclusion is judged by the commands the worker declared and the checker ran"
pr: 1220
surface: [chat]
invalidates:
  - "A review backfilled its contract by guessing which recorded command was a runner, which named specific tools and missed others. The contract is now only what the worker declares, judged by properties any project shares."
---

The review round's contract is the command or commands the worker declares as the
proof of its work. The engine no longer guesses a runner from the recorded
trajectory; it judges a declared command only by properties it can observe in any
project: the checker ran it in this check task, its exit was recorded, and it
passed the shipped read-only audit law. A `holds:` conclusion requires every
declared command to have a recorded zero-exit audited run. A `does not hold:`
conclusion is never gated, so a reading checker can still report a defect on an
empty or partial contract. A leaf that declared nothing is judged by reading, and
reading can hold. Each verdict records its basis on the leaf, by run with the
commands and their exit codes, or by reading, so a later reader and a grid see how
it was earned.

The check seat remains read-only. This does not change `verifyOnlyBash` or
`refuseOutsideDoor`.
