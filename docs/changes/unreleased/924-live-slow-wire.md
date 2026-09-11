---
kind: fixed
title: a wire below its promised pace is rescued out of the call's own budget, and every attempt is watched
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
  - "`TestTheScreenshotScenarioAnswersThroughTheFourthMachine` was red on dev: the walk it stages died on the first machine's 404, because the walk was bounded by that rolling allowance. It now answers through the fourth machine, and its phase assertion is corrected — a walk between MACHINES says `switching` and names the one it is going to, which is the better sentence; the ordinal belongs to the retry loop, which is not what moved."
---

Replicated first as failing tests from the evidence, then fixed. `internal/lane/control`
stages the substituted machine (604 tokens at 137 ms each against a question sent
expecting sixty a second) and the two cases that must NOT fire; `internal/provider`
stages the same wire through `lanestub` twice — once where the call's budget affords the
rescue and the fast machine answers, once where it may spend nothing and the row, the
move log and the person's own line all have to carry it; `internal/lane` replays the
09:33 series and the 14:29 sighting and asserts the chain the controller waits against
learns the collapse in one sighting.

What is deliberately NOT here, measured and written down at `lane.stepTo` rather than
patched: the flat `Belief.Rate` the CHOOSER ranks on is a separate, slower account of the
same number, and it still lags. A run of consistent sightings makes that single filter
confident — in this very replay its variance fell to 0.011 against an observation noise
of 0.36, a gain of three per cent — and its one escape hatch is the chain's change point,
which on a collapse like this one never fires: the CUSUM peaks at 3.4 against an alarm of
4.0 and then decays, because a chain that absorbs nine tenths of a surprise in one
observation leaves no run of surprises to accumulate. Lowering the alarm buys false steps
on ordinary noise and giving the flat belief its own change point is a third account of
one number, so the fix belongs where the two accounts are reconciled — and that moves how
every lane is ranked, which is a question for `bench/lanelab` and not for one afternoon.
The textbook repair is measured and refused: an offline replay over ten days of the call
log put a floored `P` through the same decisions and moved watched regret 0.9% (inside the
estimator's own 24% median error) while making unattended regret 17% worse and switching
machines 9–10% more. `lane.stepTo` says so, so the next reader does not re-derive it.
