---
kind: added
title: App clients can read a factory's spend by model over the same days as by repository
pr: 2220
surface: [engine]
invalidates:
  - "Was true: a factory's spend reads had no model, and a job kept one spend figure with no record of which model the calls ran on. Is true now: factory.spend_by_model splits that same figure by the model recorded on each new call, and spend with no recorded model is unknown_usd."
---

`factory.spend_by_model {factory, from, to}` (hello feature
`factory.spend_by_model`, capability `spend_by_model`) answers
`{factory, from, to, time_zone, models: [{model, cost_usd, calls, input_tokens, output_tokens, jobs}], unknown_usd}`
for the jobs the caller can read. The days and the dollars are the same as
`factory.spend_by_repo`: each job's whole spend is placed on the day its run
started (a job that never ran: the day it was created), at most 366 days, and
the model lines plus `unknown_usd` equal that read's total. Models are listed
dearest first. A new stage call, job chat call, or brief records its model;
a job from before that record, and any spend that named no model, is
`unknown_usd` and is not guessed. A triage read, a scout, and page AI that
is not booked on a job stay out of both totals. `input_tokens` and
`output_tokens` are omitted when none were measured, and `unknown_usd` is
omitted when every dollar names a model. No manual page changes: this is an
app wire read with no chat or resident surface.
