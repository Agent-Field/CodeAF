---
kind: fixed
title: Opening a factory job leaves it untouched
pr: 0
surface: [factory]
invalidates: []
---

`factory.job_chats` returns existing manager, step and round conversations without
creating a manager or changing the job's touch marker, Changed time or version.
A job without a manager returns `agent: ""` and its existing steps (an empty array
when none exist). `factory.job_send` creates the manager when a person first sends;
subsequent messages may also use `agents.send`. Sending still counts as work.
Existing wrongly marked jobs are not repaired: their history cannot reliably
separate viewing from intentional work.

Read-path audit: the former write was `FactoryJobChats` in
`cmd/codeaf/appfactory_chats.go` (Talk creation and the persistent SetEffort call).
Job, queue/list, brief/spot, step-edit source, hand-off, repository, usage and
transcript reads use stored snapshots or files, not Talk. Desk presentation reads
copy their data before withholding chat paths. `factory.read`
(`cmd/codeaf/appfactory_scout.go`, `internal/factory/serve/read.go`) is an explicit
POST assessment action: it invokes triage and saves the result, so it remains a
real write. `factory.read_on_arrival` reads settings with no `on` argument and
writes settings when `on` is supplied. Worker/runner pull GETs
(`internal/factory/serve/workers.go`) acquire leases and start work; they remain
real writes, not job browsing.
