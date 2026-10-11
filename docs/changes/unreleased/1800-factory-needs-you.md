---
kind: changed
title: Needs-you has six kinds, and a failed step is not a question
pr: 1800
surface: [chat, remote, docs]
invalidates:
  - "Needs-you was only the gate plan, proof, question or cap, and the queue did not group waiting jobs. A job now carries needs_you with one of six kinds, and the queue groups by that kind."
  - "A step that used its rounds asked to go on as is, and a step that errored asked to skip or stop. Out of attempts is a question with try once more, send back or cancel, and an error is job.failure."
  - "A job typed on the floor had no ask on the wire, and the pull request body did not carry it. The ask is on the job, and the pull request body starts with it."
---

`/v1` stays contract 1. `needs_you.kind` is `approve_plan`, `approve_proof`,
`question`, `spending_limit`, `out_of_attempts` or `publish_failed`. A free
question may be answered by anyone who can see the repository. The other five
are the job's person, and someone else gets HTTP 403. Continuing a spending
limit raises that one job to codeaf's next cap and records an answer. A publish
that failed retries on the approval already recorded.
