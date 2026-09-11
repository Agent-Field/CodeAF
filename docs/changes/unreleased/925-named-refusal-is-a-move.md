---
kind: fixed
title: a named refusal is a move, an account's ceiling waits once, and a person's pick outranks the chain
pr: 925
surface: [chat, engine, docs]
invalidates:
  - "A 429 relayed from one machine (`(via Wafer: … temporarily rate-limited upstream)`) was read from its STATUS and rode the account-limiter road: wait, double, send the same order again. It is read from WHO REFUSED now — `taxonomy.Evidence.Named` is asked before any status in `classOf` — so a 400, a 502 and a 429 from one pool are one fact and earn one answer: another machine, no wait."
  - "The dispatcher's `AN EXCLUSION THAT DID NOT TAKE IS NOWHERE ELSE TO GO` law keyed on `relayed` — a 4xx that would not otherwise have been retried — and explicitly exempted 429, which is the commonest named refusal in the log. It keys on the veto the body actually carried now, snapshotted at the encode, so a machine that answers a request which named it in `provider.ignore` ends the walk for every status. The measured chain was eight sends to one pool over ninety seconds with six other machines answering."
  - "An open serving set had another machine in it forever, on the premise that each body carries a longer exclusion list than the last. An account-wide pace breaks that premise — it names no machine, so nothing comes off the next body — and used to be answered with a machine move every time, behind a doubling wait, for the whole of the role's give-up. `control.Plan.AccountRefused` is the fact the generator could not see for itself (`Plan.ShapeRefused`'s sibling): the comeback the refusal named, once, and then the model. A base that has never published a pool is NOT this and keeps the walk it has."
  - "A 429 that named nobody reached a person as `the model could not be reached` (`taxonomy.ReasonUnserved`). It has its own shape now, `taxonomy.ReasonPaced`, spelled `we are being asked to slow down` / `we kept being asked to slow down`."
  - "A task node whose model ran out of transport budget moved to the next name in the adapter's fallback chain even when a person had chosen a model in that node's own room — so the work could finish on a model nobody named while the room showed the one they picked. `TaskNode.standingModel` is what a person said out loud, and `nextNodeModel` goes there first. A model the PLANNER wrote into `propose_task` is not a standing pick and the chain still walks past it."
  - "Choosing a model in a task's room said `model · <id> · its next turn takes it`. A turn can be twenty minutes on a long step and the change lands on the next REQUEST, which is usually seconds; it says `the next request takes it`."
  - "`lanestub` could not stage either half of the measured chain. It has `Lane.Unvetoable` — a machine the router serves however loudly the request vetoes it, which is what a relayed upstream label does — and `Server.PacesTheKey`, the account's own ceiling: 429 with `retry_after` in the body and no pool named, while the endpoints page still publishes the pool."
---

Both halves were replicated as `lanestub` scenarios before anything moved
(`internal/provider/namedrefusal_test.go`). On `dev` the first stages the live
chain exactly — eight sends to one machine and 1 m 30 s of waits, every body
after the first carrying `ignore: [Wafer]` and being served by Wafer anyway —
and the second stages seven sends of identical bytes to an account ceiling. The
control case, a router that honours the veto, moves to a healthy machine with no
wait at all and must stay that way.

Not in this change: issue #891 §3, the chooser demanding `provider.only` rather
than advising with `provider.order`. It was built and taken back out — a demand
narrowed to its last machine re-sends to the machine that just refused, and the
plan that bounds the call cannot see a demand that a non-streamed call's encoder
draws for itself. The seam is written up on #891.
