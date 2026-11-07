---
kind: fixed
title: a step edit keeps a running job running, managers see PR decisions, and the foreman opens
pr: 2488
surface: [factory, engine, app]
invalidates:
  - "Was true: a step edit to a running job (factory_run, the step edit door, the factory_item card) released the worker's lease, the worker ran a person's Stop, and the job fell back to new in Backlog and ran plan again on the next Start. Is true now: the lease is withdrawn, the job is queued again at the step it was on with its finished steps kept, and it runs the edited steps after it."
  - "Was true: factory_status carried no post-PR decision, no autonomy, no pull request facts and its own step count, so the manager said nothing was waiting beside a Merge card, claimed gates held Auto runs and said it could not read the pull request. Is true now: it carries the autonomy, `Steps: N of M done` from factory.StepCount, the pull request's number, link, change, checks and reviews, and `Waiting on the person:` lines for Mark ready, Merge and publication approval."
  - "Was true: no tool on any belt merged or marked a pull request ready. Is true now: factory_answer with action=merge, mark_ready or approve presses the job page's own POST through the app's signed-in local factory connection, only when the person asks; a terminal chat refuses in words."
  - "Was true: the foreman existed in code but nothing opened it, and its brief spoke of a key L. Is true now: factory.foreman_open {factory} answers its agent id (hello feature factory.foreman_open); factory_floor reads running, queue and shipped, and starts or prioritises one job when the person asks."
  - "Was true: a window on a moved CODEAF_HOME never told the model which home it was on, so it read the manual's ~/.codeaf. Is true now: the page names the home in use (Config.StateHome)."
---
