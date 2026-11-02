---
kind: fixed
title: Job Brief follows proof approval and publication
pr: 0
surface: [engine]
invalidates:
  - "Was true: a Brief could keep saying proof was waiting after the job reached Publish or opened its pull request. Is true now: proof approval and publication force a refresh with the current approval, PR and outcome facts; ordinary step updates keep their cost guard."
  - "Was true: a failed refresh left an older verdict with no freshness signal. Is true now: the engine returns verdict_behind when the kept verdict's inputs differ from the current job, so the desk can show it as out of date."
---

Brief updates now follow proof approval, publication, merged or closed PRs,
shipping, and stopped or failed runs, including a stop before any step starts. Auto avoids an intermediate write while
routine proof approval proceeds. A normal manual job gets two extra writes,
charged to its existing spend. Failed updates keep the old verdict and mark it
out of date until a fresh version is kept. PR numbers still require evidence.
