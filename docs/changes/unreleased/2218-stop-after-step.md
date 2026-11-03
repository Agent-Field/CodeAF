---
kind: added
title: codeaf jobs can stop spending after the current step
pr: 2218
surface: [engine]
invalidates:
  - "A job's Spend controls could only change its dollar budget. The owner can now let the current step finish and hold before the next step or landing."
---

"Stop spending after this step" saves a reversible choice on running and queued
jobs. At the boundary, yes carries on without rerunning completed work; no
stops the job and keeps its work. A step's own question or failure comes first.
The app wire and HTTP door carry the choice, including to a leased runner, and
the record names the person who changed it.

If a leased worker cannot read the choice at a step boundary, it keeps the last
saved choice and tries again at the next boundary. A temporary read failure no
longer fails the job; an already armed spending hold still applies.

The desk can now answer this spending hold through `factory.job_spend_answer`:
carry on continues at the next step, and no stops as your own stop. The door
refuses other questions and stale reads without recording an answer.
