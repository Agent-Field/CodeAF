---
kind: fixed
title: A turn delivering a result this conversation owns finishes it here, not in a second task
pr: 653
surface: [engine, chat]
invalidates:
  - "The write seam was believed to fire on any turn that changed more than two files under the workspace. It no longer fires on one shape: a turn whose only arrivals are results from tasks THIS conversation admitted — the wake a landing starts — writes as much as the integration needs and stays here. Every other writing turn is moved exactly as before."
  - "Custody was believed to cover only pieces still OUT (`checkpoint_custody.go`, `dropped:work-already-out`). It now has a settled half: the delivery of a result that has come BACK is also the conversation's own, journalled `dropped:delivering-own-result` with `reason: delivering task N`. That row admits nothing, so its `taskId` is absent and a bench counting tasks started off that field still counts only tasks."
  - "A person's own sentence was believed to be irrelevant to the write seam. It is now what closes this one exemption: a turn owing anything the person typed — a new request, or a correction steered into the delivery while it runs — is protected by the count exactly as it was. The door is HELD, never spent, so the next sentence meets it with the allowance already crossed."
  - "Ownership is read from the graph and never from words. A node with no admitter and no owner — one rehydrated from a checkpoint after a restart — cannot be proven owned, and its delivery is moved like any other writing turn."
---

The measured trial: a four-module repair asked for on a branch with a final
commit. The task did the work — 29 independent checks passed, the protected files
were untouched, the branch was there — and the turn woken by its report began the
cherry-pick that the request still owed. That is several files under the
workspace, so the write seam fired and handed the INTEGRATION to a second task in
a fresh worktree, with none of the staged index and a brief written from a turn
that was integrating rather than working. It ended in a cancelled stream, and the
commit that was asked for never happened, on work that was finished and correct.

The seam's own premise is that unreviewed edits are being made with nothing to
open and nobody watching. Neither half is true of a delivery: the work ran as a
task, it was audited, it reported, and the thing to open is the task whose result
is being delivered. What the promotion buys there is not supervision — it is a
second working copy that cannot see the first one's index.

So only the write-breadth trigger stands down, and only on that shape. The round
ceiling and the wall share still govern the same woken turn on the same ladder;
the reversal that put a woken turn on the meter at all is untouched.
