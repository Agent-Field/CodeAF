---
kind: fixed
title: A factory step ends by reporting, and says plainly when it did not
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: a factory stage conversation had a goal owner (it was unattended under a two-hour wall), which took stock, read its ending and could end it as `finishing here · what was asked is done` without `stage_result` ever being called; the runner then recorded a stage that never reported. Is true now: a stage conversation has no goal owner, no marks, no ceiling and no handover; it works until it calls `stage_result`. One that stops without reporting is told once, as codeaf (`stopped without reporting · asked once for the report`), and the runner stops a round at two hours (`ran out of time without reporting`)."
  - "Was true: a stage's reads could be handed to a quick task on another model, and a stage's team handle was chosen by its title model, so the team heard `@plan is now @dispute` mid-run. Is true now: a stage's reads are its own work, and its handle is its step name (`@review`, `@review2` for a second round) and is never renamed. The brief no longer says `Ignore team notices about renames.`"
  - "Was true: a step that fell short asked `<step> is not <until> after 1 round: no result · try once more, send it back, or cancel the job?` and the job read `Out of rounds`. Is true now: rounds are named only for a step that loops; a step that never reported says `plan stopped without reporting`, a one-try step `plan did not finish: …` or `test is not proven: …`; `send it back` is offered only when a finished step before it can take the work back, and the choices read `try again, … or stop the job?`. The presentation title is `Stuck`; the wire kind stays `out_of_rounds` and the answers keep their tokens."
---

On 2026-10-18 a plan step on a real issue handed its reading to a quick task,
was renamed by its title model, filled its context, and at round 16 its goal
owner read the ending and declared it done; it never called `stage_result`, and
the job asked whether to try a plan that had "used its rounds" once more. Each
of those came from machinery an attended conversation needs and a stage does
not: the stage already has a judge, which is code reading its report.
