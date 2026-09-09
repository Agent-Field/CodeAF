---
kind: changed
title: a task tries the check on another model, one merge round and one rerun before it asks you
pr: 673
surface: [engine]
invalidates:
  - "A check that answered neither word used to be asked once more on the SAME model, and the node then landed saying nobody could check it. The retry now rides the adapter's own fallback chain (provider.Client.FallbackModels — the same door the turn loop hops along and a node's run walks), so the second checker is a different model where this install has one to move to. --one-model and an install with no chain keep the fresh checker on the model it already had."
  - "A branch that would not merge used to go straight to the person as needs-your-look with the clashing files named. It now gets ONE resolver round first: the person's branch is merged into the TASK's branch inside the task's own working copy, a worker brings the two versions together with the brief and both sides in front of it, the check runs again on the result and the landing is retried. Only a round that fails reaches the card. The tip the task branch stood on is kept as `<task branch>-before-merge`, so nothing is rewritten in place."
  - "There is a new engine door, `func (a *Agent) ResolveConflict(id uint64) error`, which spends one more merge round on demand — the surface's `[a] resolve it`. It returns before the round does and refuses in one line when the task has no working copy to resolve in, or when a round is already running."
  - "A wire, upstream or stale ending used to land the node incomplete at once, offering the person a rerun. The engine now buys the node ONE rerun from its branch first, through task_continue.go's own re-arming; a second such ending lands incomplete. Exactly one — a provider that is down stays down for the second attempt."
  - "The provider fallback chain is walked once per NODE, not once per attempt. A rerun inherits the model move the last attempt made, so a node admitted on one model and rerun on another still lands on a card that names where it started."
---

Each of the three is one automatic attempt, bounded at one, with one plain line in
the task's journal. They exist because the cards they replace asked a person to
press a key that means "try that again", which is an errand rather than a decision.
