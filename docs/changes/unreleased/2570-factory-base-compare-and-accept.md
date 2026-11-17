---
kind: added
title: The factory compares failing Go tests with the base, and a stuck step can be accepted as is
pr: 2570
surface: [factory, app]
invalidates:
  - "Was true: under the Test rule no new failures, a failing check counted against the job unless the step itself had reproduced it in a clean worktree at the base with go test -json. Is true now: when a Test round ends with failing go test checks, the factory reruns each on the job checkout and on a clean worktree at the base and compares the failing test names; a check whose failures are all on the base no longer blocks. Go only; the rule green stays strict."
  - "Was true: a step out of attempts took three answers, try once more, send back, or cancel the job. Is true now: a stuck Test, Review or Proof step that reported, before anything is published, also takes accept (factory.job_accept_step). Its failing checks are kept in its notes as 'Accepted by the owner:', later steps are not blocked by the same failure, and the PR lists them."
  - "Was true: the GitHub issue list read each issue's id but never kept it, so a known job took no later labels, titles or comments, and a start label on a known issue never started it. Is true now: listed issues carry their id."
---
