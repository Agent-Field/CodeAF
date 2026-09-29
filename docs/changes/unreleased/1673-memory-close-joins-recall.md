---
kind: fixed
title: a closed conversation has no memory pass still reading or writing its brain
pr: 1673
surface: [engine]
invalidates:
  - "The pre-turn recall (memory.go's startRecallLocked) rode beside the turn and was never joined, so it could still be reading the brain — or writing it, on a router command — after Agent.Close returned. It is now a counted memory pass: Close waits for it on the same memoryJobs join as the post-turn extract/decide pass."
  - "waitForMemory waited closeGrace for the post-turn pass, cancelled whatever was left, and returned at once. It now joins what it cancelled, with a second closeGrace as the limit, and logs a pass that outlives even that."
---
TestANeighbourTurnsTheWriteIntoADecision failed once on #1658's gate with its own
assertion green. The failure was TempDir's cleanup finding the brain's folder not
empty. The store was still being read after the conversation closed: a recall
the scheduler had left behind its own turn was still inside SQLite when the test
closed the store and removed the folder. Close now waits for that reading
the way it already waited for the post-turn pass, and it also waits for a pass
it has just cancelled instead of returning at the cancel. A synctest test holds
each pass on a gate and fails on the old code.
