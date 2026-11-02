---
kind: added
title: The app can run a stopped factory job again, and a drop is no longer called final
pr: 1899
surface: [engine, chat, docs]
invalidates:
  - "Was true: a person's Stop that cut a step left the job `needs_you` with `failure` `{code: stopped}`, `can.run` false and `can.run_again` true, and the app had no action for it: `POST /v1/jobs/{id}/run_again` was not on the app wire. Is true now: `factory.job_run_again {factory, job, expect}` (`expect.version` required) sends Run again under `factory.job_actions`, answered `FactoryJobActed` with action `run_again`. The factory clears the failure and starts a new run at the first step, in the worktree and branch the job's runner kept; it still refuses a job with no failure whose pull request was not closed unmerged."
  - "Was true: starting a dropped job was refused as `#12 was dropped for good`, a sentence agents relayed as the job being impossible to restart, although Bring back already returned a dropped job to Backlog. Is true now: the refusal is `#12 was dropped · Bring back returns it to Backlog`, still a `409` conflict, and the manual no longer says a drop finishes a job for good."
---

Run again was a factory door with nothing in the app to press, so a job a
person had stopped mid-step could only be undone within five seconds or left.
The drop refusal is spelled once, beside the check that makes it, and the
factory's HTTP door reads that one spelling to answer it as a conflict.
