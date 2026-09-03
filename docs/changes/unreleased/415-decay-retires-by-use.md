---
kind: fixed
title: an observation is retired when it stops being needed, and a re-read of retired bytes gets the bytes
pr: 415
surface: [engine]
invalidates:
  - "The observation decay pass in `internal/exec` retired by age: it walked the transcript newest-first and stubbed the oldest raw tool output until the live window fitted the low-water mark, protecting only the newest assistant turn's results. It retires by use. `decayer.retire` now walks three times into one accumulator — this turn's own results against the full budget, then history nothing has acted past, then history that has been — so a spent result goes before an older one the model has not acted on, and unconsumed material is reached for only when retiring every spent result did not bring the window under the mark."
  - "Nothing recorded whether a result had been used. Two signals do, and they are both signals the loop already had: the artifact count the no-progress guard reads before and after a turn's tool calls (a turn that changed the workspace acted on everything the transcript held when it asked for them, recorded by `decayer.actedPast`), and a pointer emitted at an earlier copy, which spends that copy because its bytes are reachable from a durable address whatever happens to the copy above."
  - "A repeated body always became a pointer, and a re-read of a copy decay had already stubbed was answered with `[identical to the result of … whose bytes are in <spill> — read the part you need with sh]`. That sentence no longer exists. `observations.admit` carries the bytes whole when the earlier copy has been retired, reuses the address the stub already names rather than writing a second file, and makes the new copy canonical — so a pointer only ever stands for a copy still quoted in the transcript and still says `read it above, or from <path>`."
  - "`internal/exec`'s `TestAPointerNamesTheSpillFileOnceTheOriginalHasDecayed` pinned the pointer-at-a-stub answer as correct. It is gone; `TestARereadOfRetiredBytesIsAnsweredWithTheBytes` pins the opposite, and `TestRetirementFollowsUseRatherThanAge` and `TestAPointerSpendsTheCopyItPointsAt` are the ordering's table tests."
---

A leaf that must read more material than its observation window holds before it can
act was losing the reads it had not used yet, oldest first — and when it re-read
them it was handed a description of the bytes instead of the bytes, which it could
only follow in slices, each slice fresh material that pushed the window over again.
Measured on #252: 200 shell calls for 186 distinct commands, two files read six
times each, 46 turns and two full budgets for zero writes.

THE LAW, and the two halves are one mechanism: an observation is retired in order of
how long ago it was last needed, not how long ago it arrived, and a re-read of
retired bytes is answered with the bytes. The window is a hard bound on request
size, so "never" is not available to material the model has not used; "last" is.

The low-water mark, the hysteresis, spill-before-stub, the preserved-path invariant
that keeps a pointer from being stranded, tool-call pairing, and the `prefix
rewritten — N observation(s) retired, M turn(s) folded; this turn re-reads cold`
trace line are all unchanged. No new thresholds and no second mutation detector.
