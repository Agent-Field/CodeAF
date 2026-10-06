---
kind: changed
title: a recorded failure can carry the later observed success that answered it
pr: 1779
surface: [engine, docs]
invalidates:
  - "Only failures and blocks were ever written from the tool boundary. A success in the same turn and goal now attaches to the failure as its observed successful alternative, and no other success is retained."
  - "A fresh turn shown a prior failure had no note of a path already known to work, so it could repeat dead work to rediscover it. An applicable observed alternative is now rendered beside its failure with its own receipt and source circumstances, as observed history rather than a cause or a ban."
---

One canonical event journal and one projection, still append-only. The
alternative is an `AttemptSucceeded` row whose `AlternativeOf` names the failed
attempt's source key, so an explicit forget of either source retires the pair
through the existing suppression join and the read side never shows a success on
its own. A source snapshot is labelled as a snapshot, never as the environment.
