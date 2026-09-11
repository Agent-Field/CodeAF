---
kind: fixed
title: a live wire below its promised pace is rescued out of the call's own budget, and every attempt is watched
pr: 924
surface: [chat, engine, docs]
invalidates:
  - "Rescues were bounded by a rolling PROCESS-WIDE allowance — at most two in any twenty requests and at most a tenth of the last hour's bill (`lane.Budget`, `provider.SetHedgeBudget`). That whole mechanism is deleted. A count spread over twenty requests cannot tell the one that needs rescuing from the nineteen that do not, so it refused by arrival order: on 2026-09-11 the controller reached its ceiling, asked for a second machine, and was told no on behalf of requests that had already finished — while one endpoint wrote 604 tokens in 86 seconds with a person watching. What a call may spend rescuing itself is now its own: `control.Plan.SpendUSD`, derived as `Role.GiveUp() / λ` (the longest wait it can still have, converted at what a second of that wait is worth), asked through the same `control.Purse`. How many arms one question may run at once is still `maxArms = 4` and is now the ONLY thing that counts arms."
  - "`SetLaneGuard(false)` switched rescues off by installing a budget with a zero allowance. There is no allowance to zero; it is now a purse that refuses everything (`lane.NoSpending`), written onto the plan where every arm reads it. `provider.SetHedgeBudget` and `lane.NewBudget` / `lane.DefaultBudget` no longer exist."
  - "A row whose rescue never left said `refused: budget`, which named a rolling allowance and therefore said \"some other request spent this one's rescue\". It now says `refused: plan cannot pay`, which names the only rail there is. `make census` reads the field for the first time and reports `a rescue the controller called for that never left, and says why`, with the words underneath."
  - "The aggregate credit test judged every visible gap against the SERVING lane's belief, which `Controller.Serving` re-points the moment the stream says who is answering. So a machine that had been collapsing all afternoon was judged against its own collapse, and the more this process learned about it the less any stream it served could read as slow: 604 tokens in 86 seconds and 5,793 in 363 were both \"keeping up\" against a belief of six a second. A stream is now held to the pace its question was SENT EXPECTING — the gap belief the plan was built with, which is the machine the choice named — so a router substituting a ten-times-slower machine is noticed and one answering at the rate it was asked for is never touched."
  - "A `control.Report` was the end of the story: nothing could be started, so nothing happened and the question went on being answered at the pace that earned the report. A report is now also a move — the serving machine is written onto the plan's move log (asked of `control.Next`, so a question with nowhere else to go keeps the only machine it has) and the person is told."
  - "There was no word for a stream that is writing too slowly to read. `writing` was false about the wait and `all lanes slow · still waiting` was false about the silence, so the surface said nothing: the measured call showed one nudge and then eighty-six seconds. `provider.PhaseBelowPace` is the third word and `internal/tui3` draws it as `answering slowly · nowhere faster · 1m 26s`."
  - "A watch belonged to an ARM and an arm can send many times, so every attempt after the first ran inside the first one's once-only flags: `streamWatch.acted` keeps the FIRST act, so a later attempt's row could never be written, and the controller's own report and purse refusal are once per controller. The 363-second second attempt of 2026-09-11 carried no action, no reason and no silence at all. Every attempt now gets its own controller, dated from the moment its own bytes leave (`streamWatch.attempt`, called from `Client.send`); what belongs to the QUESTION — the deadline and the move log — is untouched."
  - "The flat `Belief.TTFT` / `Belief.Rate` the chooser ranks on was a SECOND Kalman filter over the same number the four-level chain holds, fed the same observation with the same noise and reaching a different answer — the chain absorbs about four fifths of a surprise where the flat belief absorbs a seventh. By the end of that afternoon the chain said six tokens a second and the belief the chooser reads still said eleven. The flat belief is now the chain read flat (the `flatten` the chooser already used for cold pairs), for the quantity a sighting actually taught; `stepTo` is deleted with the second filter it existed to repair."
  - "`TestTheScreenshotScenarioAnswersThroughTheFourthMachine` was red on dev: the walk it stages died on the first machine's 404, because the walk was bounded by that rolling allowance. It now answers through the fourth machine, and its phase assertion is corrected — a walk between MACHINES says `switching` and names the one it is going to, which is the better sentence; the ordinal belongs to the retry loop, which is not what moved."
---

Replicated first as failing tests from the evidence, then fixed. `internal/lane/control`
stages the substituted machine (604 tokens at 137 ms each against a question sent
expecting sixty a second) and the two cases that must NOT fire; `internal/provider`
stages the same wire through `lanestub` twice — once where the call's budget affords the
rescue and the fast machine answers, once where it may spend nothing and the row, the
move log and the person's own line all have to carry it; `internal/lane` replays the
09:33 series and the 14:29 sighting and asserts that the two accounts of one rate agree
and that both land on what the answer measured.

What is deliberately NOT here: the change-point alarm still cannot fire on a step the
chain absorbs in one observation (its CUSUM peaked at 3.4 against an alarm of 4.0 and
then decayed, which is what `rate/drift` shows in the real belief file). It no longer
matters for this defect — there is no second filter left for it to repair — and re-sizing
it is a fit for the simulator rather than a change to make from one afternoon.
