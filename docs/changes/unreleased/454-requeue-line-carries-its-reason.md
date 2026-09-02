---
kind: fixed
title: the headless ↻ line says why a claim went back on the queue, not only how much it picked up
pr: 454
surface: [engine, resident, docs]
invalidates:
  - "The ↻ line on a headless requeue read `picked up again from N recorded turns` and nothing else; the reason lived only on the ⏳ line above it. The line now carries the release's own reason after the count: `picked up again from 9 recorded turns — it was still working when it ran out of its token budget (cost: 199131 of 176834 tokens of billed work)`."
  - "The reason for a claim taken back from a worker that stopped answering ended `— N turns of its work is recorded, and the next one carries on from there`. It ends `and the next attempt carries on from there`, the same clause the out-of-room reason uses, because both endings now compose it from one spelling."
---

A release reason names the bound that fired, its figures and the surviving turns, and the
stream threw all of it away on the arm that hands work on. That read as a bare restart to
anybody who met one ↻ on its own, which is what happens the moment the two events stop
being adjacent. The count is still the release payload's own and the why is still the
release's own words: `resident.ReleaseWhy` takes off the clause about how many turns
survived, because the line has already said that number itself, and the clause is spelled
in one place so the cut and the join cannot drift apart.
