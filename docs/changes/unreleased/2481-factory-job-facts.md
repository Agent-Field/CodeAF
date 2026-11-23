---
kind: added
title: Factory jobs say their source, rule, one step count and the model each step runs on
pr: 2481
surface: [engine, factory, app]
invalidates:
  - "With no factory default model, factory steps ran on the runner's launch model, which for a profile that never typed one is config.DefaultModel. They now run on the person's worker model (Settings › Models › worker, pinned or routed) first, and the launch model only after that. The chain is one function, factory.StepModel, used by the stage maker, the queue's model and the wire's runs_on."
  - "The step count had three sources (the Brief's phases, job.step.count and the desktop's rail rows). job.steps_done/steps_total from factory.StepCount is now the one count, and the Brief reads it too; a skipped step counts for neither side."
  - "Desktop clients built a job's repository link from repo. job.repo_url is now sent, read off the forge's own PR or issue address."
  - "A factory step chat's opening brief and its '[carry on] … stage_result' reminders were indistinguishable from the person's messages on the wire. Transcript entries now carry author person|engine|agent (hello agents.entry_author)."
  - "agents.suggest_reply could offer 'Please call stage_result …'. A draft naming an underscored belt tool or opening with [carry on] is now dropped."
  - "A copy of the repository a step left inside the checkout (.proof-prefix-NNNN/ on PR #148) was measured, judged by protected paths and committed. Steps now get a scratch folder under the OS temp dir, and such a copy is left out of the change and the commit."
---
