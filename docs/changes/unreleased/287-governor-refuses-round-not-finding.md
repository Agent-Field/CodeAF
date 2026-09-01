---
kind: fixed
title: a growth governor may refuse a round, never a review's finding
pr: 287
surface: [engine, resident]
invalidates:
  - "A coverage refusal used to ACQUIT the review's finding: `revision.Extension` carried an `Overturned` field, a `goal-already-covered` refusal set it, and the delivery said \"the job's own reading of what it is judged on found nothing left uncovered, so the review's finding is what was wrong\" and exited 0. That field is gone and that sentence is unreachable; a coverage refusal now leaves the gap Unclosed and the run partial."
  - "The delivery gate's coverage finding used to be counted per LINE OF THE REQUEST, so `store.DeliveryGate.Unexercised` held one entry per line and the person was told \"1 behaviour the request states has no check\" over six of them. It is now one entry per acceptance POINT, and the line grouping is only how the repair brief writes the list out."
  - "The gap prose used to open \"The request asks for behaviours that no check exercises.\" It now opens with the count — \"2 behaviours the request states have no check that exercises them.\" — because that first line is what the closing line of a partial run quotes back."
  - "`resident.GovernorStanding` used to answer nothing for a `goal-already-covered` refusal, so a run stopped by the coverage reading said nothing about it on its last line. It now answers \"a reading of what this job is judged on found nothing left to add\"."
---

The growth governor's coverage question asks a model whether the plan's own Done is covered
by what landed, and it is shown the plan and the workers' own summaries — the account being
judged. On a headless run against a real issue it answered "nothing is left" over a feature
that was dead in the delivered tree, and because a `goal-already-covered` refusal set
`Overturned`, that answer was recorded as the REVIEW having been wrong. Only the exit floor
kept it off exit 0.

Four things were wrong at once and all four are closed. A refusal to fund a round no longer
settles anything about the finding; the governor reads the finding held in its hand before
the growth journal, which the delivery gate writes one event later than it reads; the
acceptance finding counts behaviours rather than lines of the request; and a coverage
refusal now reaches the person's last line instead of vanishing into the journal.
