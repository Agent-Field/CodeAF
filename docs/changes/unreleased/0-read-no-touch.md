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

## Read-path audit

| Read and source | Former write and correction |
| --- | --- |
| `factory.job_chats`, `cmd/codeaf/appfactory_chats.go:28` | Called Talk and persistent SetEffort. Returns stored chats and applies effort only while opening the conversation. |
| Every HTTP snapshot, `internal/factory/serve/serve.go:413` | Reaped leases and settled jobs before routing GET. Startup, deadlines, worker pulls and explicit writes now own maintenance. Writes settle after version checks. |
| Presence and desk reads, `internal/factory/serve/presence.go:24`, `internal/factory/serve/runners_v2.go:46`, `internal/factory/serve/conflict.go:242` | Called reaping Desk.Live. Snapshot now observes holds without changing jobs. This covers queue, job, step-edit, me, person, runners and people reads. |
| Worker checks, `internal/factory/serve/workers.go:744`, `internal/factory/serve/stop_after_step.go:49` | Publication checking called Store.Update without changing a field. Stop-choice reads renewed the lease. Both now read without writing jobs or leases. |
| Job and queue storage, `internal/factory/store/store.go:202`, `internal/factory/store/store.go:222` | Get and List persisted repository identity normalization and bumped revisions. They now normalize returned snapshots. Actual writes persist normalization. |

Brief, spot, hand-off, repository, usage and transcript reads use stored snapshots
or files. `internal/factory/serve/thread.go:9` reads the replicated thread.
`internal/factory/serve/job_presentation.go:27` and `:45` copy presentation data
before withholding chat paths. None calls Talk.

## Operations that are real writes

`factory.read` at `cmd/codeaf/appfactory_scout.go:52` and
`internal/factory/serve/read.go:17` is an explicit POST assessment action. It runs
triage and saves its result. `factory.read_on_arrival` at
`cmd/codeaf/appfactory_read_on_arrival.go:20` writes settings only when `on` is
supplied. Worker and runner pull GETs at `internal/factory/serve/workers.go:374`
acquire leases and start work. These retain their write behavior.

`factory.list` at `cmd/codeaf/appfactory.go:835` and `factory.local_status` at
`cmd/codeaf/appfactory_setup.go:337` resume the saved local service and runner.
That startup is a lifecycle action: its independent workers and deadlines can
advance jobs. Ordinary job browsing no longer performs that maintenance.
