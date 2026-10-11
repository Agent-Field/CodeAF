---
kind: changed
title: A failed step, each round, a refused link and who may act are on /v1
pr: 1800
surface: [chat, remote, docs]
invalidates:
  - "job.failure was step, code, detail, next and at, and a failed step was not a record event. The job and that step now also carry attempts and machine, and the record says step_failed."
  - "A loop step kept only its latest result. It now keeps one rounds line per finished try, bounded by the step's max and never more than 32."
  - "A refused access token was a plain 401, and revoke deleted the row. A presented token now answers expired, revoked or unknown, with invited_by and created_at when the row remains, and revoke keeps a hash so a later use can say revoked."
  - "Who may act was only the needs-you answer. Every job now carries can for this caller, and approve, run again, cancel job and retry publishing answer 403 not_job_person when the caller is not the job's person."
  - "A conflict named a sentence, by and at. It now also names who, when, and the machine or the commit."
---

`/v1` stays contract 1. `can` is computed for the caller and is not stored on
the job. A plain question and reply stay open to anyone who can see the
repository. Take of a job someone else holds stays 409. A request that sent
no token still says how to sign in.
