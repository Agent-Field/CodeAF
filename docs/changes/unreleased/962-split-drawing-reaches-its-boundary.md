---
kind: fixed
title: a split drawing reaches the boundary its own cut opened, and never says it carried on
pr: 962
surface: [engine]
invalidates:
  - "A mark's drawing that said the turn had independent parts in it was believed to be spent at the boundary its own cut opened. It was not always: `readBeside` published the answer only after `act` had returned, and `sidecar.take` is non-blocking, so the loop arriving at that very boundary read the reading as still in flight and let it pass. The answer is published BEFORE the interruption is delivered now. `sidecar.spent` gates the interruption — a taker stores it, and the interruption is delivered only if it claims first — so an answer already spent at a boundary raises nothing into whatever the turn does next. It is a gate and not a fence: for the one instruction between the settle closing and the claim, an interruption can still win while a taker takes anyway, and `Agent.dropOwedCut` is the other side of that — a cut owed for a drawing the turn has since taken is withdrawn at the boundary that took it."
  - "`Agent.cutGeneration` returning false for a generation that has not started was believed harmless for every reading, because 'the transcript it will be assembled from already carries the change'. That is true of the recall and false of a drawing: a drawing rides no transcript and exists only to be spent at a boundary. A cut whose cause is a `boundaryCut` — the mark's is the only one today — is now OWED rather than dropped, and `beginGeneration` spends it on the next request the SAME turn makes (by `turnSeq`, so a cut one turn never spent cannot reach the next person's message). A recall's cut still reports false and still changes nothing."
  - "A mark row whose sketch had independent parts in it and whose decision read `continue` was a reader that decided to carry on. It was usually the opposite: a drawing that landed past the turn's last step was journaled through `carryOnDecision` on the turn's exit road. That road now writes `late` — a new decision word, spelled apart from `split` because nothing was handed anywhere and apart from `continue` because the reader decided otherwise. Every other journal site (the ceiling's own in-line read, the looping ending) still spells a carry-on and is unchanged."
  - "`readBeside`'s doc stated that `act` running before the answer could be taken was load-bearing, because an answer taken before the cut landed would be spent against a step the interruption was still about to cut. The order is reversed and the paragraph is gone: the taker is at a boundary by construction, and the claim is what orders the two."
---

Found by reading, out of #952 §5, and filed as #956 with a replication a stranger can run.
The turn paid for a whole extra model step it had just bought its way out of, and where that
step was the turn's last, a turn with independent parts left in it ended in words with the
file recording the reader as having agreed to that. The two windows are one missing ordering
seen from both ends, so they take one fix in the two places that already owned it — the
sidecar's own goroutine, and the lock that already decides whether there is anything to cut.

This makes one road busier than it has been measured at, and that road is short a journal
row: a split drawing taken on a turn the person has since stopped is written down as
nothing at all (`checkpointSettle`'s `ctx.Err()` arm). It is filed as #984 rather than fixed
here, because giving it a word means a new decision word beside this one and not widening
`abandoned`, which the bench reads.
