---
kind: changed
title: A cheap factory step runs on the small work model and a strong one on the checks model
pr: 2148
surface: [engine, chat, docs]
invalidates:
  - "Was true: a factory step's effort (`cheap`, `strong`) reached the step only as a caption in its brief (`thinking cheap`); every step without its own model ran on the job's model or the factory's default. Is true now: a step marked cheap opens on the small work model (models.tiers.low, the task models' auxiliary pin) and a step marked strong on the checks model (models.tiers.high, the checker pin), unless the step or the job has its own model; with one model for every task nothing changes. The app announces it with the hello word `factory.effort_models` and reads and sets the two models through settings.models.get and settings.models.set, as it already does."
---

Spending's Models rows can say which model cheap and strong factory steps
run on, and change them where Settings already does.
