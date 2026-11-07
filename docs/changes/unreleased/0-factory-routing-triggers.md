---
kind: fixed
title: factory routing says what it did, and outside work can be given a repository
pr: 0
surface: [engine]
invalidates:
  - "Start on a job with no repository answered `uncertain: say which repository the work is on · the action may have happened…`, which the desk dropped. It is now a `conflict` in the words \"say which repository the work is on\", and the waiting job reads state_presentation title \"Waiting for a repository\" with \"Say which repository the work is on, or add a routing rule that sends it to one.\""
  - "Nothing on the wire could give a job a repository except a second delivery. `factory.job_set_repo {factory, job, repo, expect?}` (hello `factory.job_set_repo`, `POST /v1/jobs/{id}/repo`) does, for a job that never ran: it stamps that repository's steps and routes the job again; the operator or the job's person only. A repository set this way wins over a rule's `repo`."
  - "A routing rule naming a pipeline its repository's file does not list failed the whole intake pass, which made the GitHub and Jira polls back off up to ten minutes, and said so only in stderr. It now never fails the pass: the job stays unrouted with one `routed` activity row, `routing rule <name>: <repo> has no pipeline <p>`. `factory.routes_test` answers `refused` with that reason instead of `matched`, and `factory.routes_set` refuses such a rule when the file can be read. A failed GitHub poll is now logged."
  - "Turning Pick up issues off on a factory on your own machine switched routing off entirely. Routing rules (auto, ask, backlog) now apply whatever the switch says; it governs only labels starting work no rule matched."
  - "`factory.job_start {pipeline}` on a queued job that never ran was refused with \"This job has already run, so it keeps its steps.\" A queued job not yet taken now takes the pick, its run laid out again; only a job that ran is refused."
  - "A rule that filed a job in Backlog wrote no activity row; it now writes `routing rule <name> · Backlog`. A held routed job no longer carries `autonomy_why`, so the desk stops reading \"Auto stopped: Approval needed: start · …\" when nothing ran."
  - "An edited ticket re-routed a Backlog job and silently reset its pipeline to the default. A routed job now keeps its route and steps unless a different rule matches; then that rule's pipeline applies and the activity says `routing rule <new> · in place of routing rule <old> · steps …`."
---

Found by the live audit of factory triggers (2026-11-06). The router stays a
pure function; every fix is in how intake and the doors act on its answer.
