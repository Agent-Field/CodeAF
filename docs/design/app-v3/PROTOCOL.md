# codeaf app protocol

`codeaf app` serves the desktop engine over stdin and stdout. Contract 1 is
additive: clients ignore unknown fields and event names. This is the tracked
index of the current wire, including optional engine capabilities.

## Framing and lifetime

Each line is one UTF-8 JSON object. Requests carry an unsigned integer `id`,
a `method` and optional `params`. Each answer carries that `id` and either
`result` or `error: {code, message, detail?}`. Concurrent answers may arrive
in any order. Events carry `event`, a process-wide increasing `seq`, and `data`.
Logs go to stderr.

The first output is `hello` with sequence 1, contract, version, process id,
home and features. Clients check `hello.features` before using optional
capabilities; an unavailable method answers `unknown_method`. End of input
or `shutdown` closes the engine. `codeaf app --home <dir>` chooses its state root.

An `uncertain` mutation may already have happened. Read its receipt or current
state before taking further action; never blindly repeat an uncertain write.
Use the operation's client/request id where it supports replay.

An `unreadable` error refuses a read of a store this engine cannot read: one a
newer build wrote, or a file that is not the store. `crews.overview` answers it
for the crews store. Trying again does not help, so the app says so instead of
offering a retry. It is additive: an app that does not know the code treats it
as any other failure.

## Schema and method index

`codeaf app --schema` prints the complete JSON Schema directly from the Go
dispatch table, event shapes and struct tags in `internal/appwire`. The tables
below are a checked snapshot of that index. `in.*` references describe input
definitions under `$defs`; other references describe result and event types.
For anonymous objects the index names their fields; the schema provides their
full types, optional fields, enums and bounds. Method-specific validation still
decides which combinations are accepted.

## agents methods

| method | params | result |
|---|---|---|
| `agents.archive` | `in.tidyIDs` | `Empty` |
| `agents.budget.get` | `in.IDParams` | `AgentBudgetResult` |
| `agents.budget.set` | `in.AgentBudgetSetWire` | `AgentBudgetResult` |
| `agents.close` | `in.IDParams` | `Empty` |
| `agents.compact` | `in.crewID` | `Empty` |
| `agents.create` | `in.CreateAsk` | `agentResult` |
| `agents.debug` | `in.IDParams` | `ConversationDebug` |
| `agents.delete` | `{confirm, id}` | `Empty` |
| `agents.entries` | `in.EntriesParams` | `EntriesResult` |
| `agents.errand` | `in.ErrandParams` | `Errand` |
| `agents.errand.models` | `in.ErrandModelsParams` | `ErrandModels` |
| `agents.idle` | `{days}` | `IDsResult` |
| `agents.labels` | `{ids}` | `AgentLabels` |
| `agents.list` | `in.AgentsListParams` | `AgentsResult` |
| `agents.manual` | `{client_id, id, question}` | `TurnResult` |
| `agents.mark_seen` | `in.IDParams` | `Empty` |
| `agents.move_to_closed` | `in.tidyIDs` | `Empty` |
| `agents.move_to_task` | `{id, turn}` | `MoveResult` |
| `agents.take` | `in.TakeParams` | `TakeResult` |
| `agents.take_cancel` | `in.TakeCancelParams` | `TakeCancelResult` |
| `agents.open` | `{id, warm}` | `agentResult` |
| `agents.queue` | `in.sayTo` | `queueResult` |
| `agents.rename` | `{id, title}` | `agentResult` |
| `agents.reopen` | `in.crewID` | `agentResult` |
| `agents.retry` | `in.IDParams` | `TurnResult` |
| `agents.rewind` | `{id, point}` | `RewindToken` |
| `agents.rewind.points` | `in.IDParams` | `ConversationPoints` |
| `agents.rewind.undo` | `{id, token}` | `Empty` |
| `agents.run_harness` | `in.RunHarnessParams` | `TurnResult` |
| `agents.send` | `{attachments, client_id, from, id, reads, said, submission, text}` | `TurnResult` |
| `agents.set_approval_posture` | `{id, posture}` | `agentResult` |
| `agents.set_effort` | `{effort, id}` | `agentResult` |
| `agents.set_model` | `{automatic, id, model}` | `agentResult` |
| `agents.set_models` | `{fallback, id, main}` | `agentResult` |
| `agents.since` | `{after, id}` | `Since` |
| `agents.steer` | `in.sayTo` | `Empty` |
| `agents.stop` | `in.IDParams` | `Empty` |
| `agents.suggest_reply` | `in.ReplySuggestionParams` | `ReplySuggestion` |

## asks methods

| method | params | result |
|---|---|---|
| `asks.answer` | `{agent, batch_item, id, later, option, scope, text, why, you_decide}` | `Empty` |
| `asks.defer` | `in.askAt` | `AskResult` |
| `asks.hold` | `{agent, id}` | `Empty` |
| `asks.list` | `in.AgentParams` | `AsksResult` |
| `asks.shown` | `in.askAt` | `AskResult` |

## attachments methods

| method | params | result |
|---|---|---|
| `attachments.add` | `{agent, kind, path, text, url}` | `AttachmentResult` |
| `attachments.remove` | `{agent, id}` | `Empty` |

## autonomy methods

| method | params | result |
|---|---|---|
| `autonomy.get` | `in.AgentParams` | `AutonomyResult` |
| `autonomy.set` | `in.AutonomySetParams` | `AutonomyResult` |

## branches methods

| method | params | result |
|---|---|---|
| `branches.land` | `in.BranchLandQuery` | `BranchLanded` |
| `branches.land.plan` | `in.BranchLandPlanQuery` | `BranchLandPlan` |
| `branches.list` | `in.BranchesQuery` | `BranchesResult` |

## cache methods

| method | params | result |
|---|---|---|
| `cache.clean` | `in.CacheCleanParams` | `CacheCleanResult` |
| `cache.status` | `{}` | `CacheStatus` |

## caps methods

| method | params | result |
|---|---|---|
| `caps.raise` | `in.CapRaise` | `Empty` |

## checkout methods

| method | params | result |
|---|---|---|
| `checkout.diff` | `in.CheckoutPathQuery` | `CheckoutDiffResult` |
| `checkout.locate` | `in.CheckoutPathQuery` | `CheckoutLocation` |
| `checkout.status` | `in.CheckoutQuery` | `CheckoutStatus` |

## collections methods

| method | params | result |
|---|---|---|
| `collections.add` | `{id, refs}` | `CollectionAddResult` |
| `collections.create` | `{name}` | `CollectionResult` |
| `collections.delete` | `in.IDParams` | `Empty` |
| `collections.duplicate` | `in.collectionNameParams` | `CollectionResult` |
| `collections.expand` | `in.collectionBoundParams` | `CollectionExpandResult` |
| `collections.list` | `{after, holds, limit}` | `CollectionsResult` |
| `collections.open` | `in.collectionOpenParams` | `CollectionOpenResult` |
| `collections.relink` | `{from, id, to}` | `CollectionRelinkResult` |
| `collections.remove` | `{id, refs}` | `CollectionRemoveResult` |
| `collections.rename` | `in.collectionNameParams` | `CollectionResult` |

## competence methods

| method | params | result |
|---|---|---|
| `competence.evidence` | `in.CompetenceEvidenceParams` | `CompetenceEvidence` |
| `competence.map` | `in.CompetenceMapParams` | `CompetenceMap` |

## crews methods

| method | params | result |
|---|---|---|
| `crews.archive` | `in.crewID` | `crewResult` |
| `crews.archived` | `{}` | `CrewsResult` |
| `crews.close` | `in.crewID` | `Empty` |
| `crews.create` | `in.CrewCreate` | `crewResult` |
| `crews.decide` | `{option, packet, reason, text}` | `DecisionResult` |
| `crews.decisions` | `{all, id}` | `DecisionsResult` |
| `crews.defaults` | `{}` | `DefaultsResult` |
| `crews.delete` | `in.crewID` | `IDsResult` |
| `crews.get` | `in.crewID` | `crewResult` |
| `crews.items` | `in.crewID` | `ItemsResult` |
| `crews.link` | `{id, kind, ref, title}` | `ItemResult` |
| `crews.list` | `{closed}` | `CrewsResult` |
| `crews.log` | `{after, id, limit}` | `LinesCursorResult` |
| `crews.log.at` | `in.LogAnchorAsk` | `{anchor}` |
| `crews.manager` | `{agent, id}` | `crewResult` |
| `crews.message` | `in.CrewMessage` | `EntryResult` |
| `crews.move` | `{ids, parent}` | `Empty` |
| `crews.organize` | `in.OrganizeAsk` | `OrganizeReply` |
| `crews.overview` | `in.OverviewAsk` | `Overview` |
| `crews.propose` | `{agent}` | `Empty` |
| `crews.reopen` | `in.crewID` | `crewResult` |
| `crews.root.start` | `in.RootStartAsk` | `RootStartResult` |
| `crews.set_default` | `{key, value}` | `DefaultsResult` |
| `crews.unlink` | `{id, item}` | `Empty` |
| `crews.update` | `in.CrewUpdate` | `crewResult` |
| `crews.wrap_up` | `{id, text}` | `Empty` |

## deliverables methods

| method | params | result |
|---|---|---|
| `deliverables.count` | `in.DeliverablesQuery` | `DeliverablesCount` |
| `deliverables.get` | `{agent, id}` | `Deliverable` |
| `deliverables.keep` | `{agent, dir, expect_version, id, name}` | `Deliverable` |
| `deliverables.list` | `in.DeliverablesQuery` | `DeliverablesPage` |
| `deliverables.read` | `{agent, id, max_bytes, version}` | `DeliverableText` |
| `deliverables.remove` | `{agent, id}` | `Empty` |
| `deliverables.snapshot` | `{agent, path, process, url}` | `Deliverable` |

## chats methods

| method | params | result |
|---|---|---|
| `chats.list` | `{}` | `ChatsResult` |

## pair methods

| method | params | result |
|---|---|---|
| `pair.start` | `in.PairStartParams` | `PairResult` |
| `pair.get` | `in.PairGetParams` | `PairGetResult` |
| `pair.answer` | `in.PairAnswerParams` | `PairResult` |
| `pair.cancel` | `in.IDParams` | `PairResult` |

## devices methods

| method | params | result |
|---|---|---|
| `devices.list` | `in.DeviceListParams` | `DevicesResult` |
| `devices.remove` | `in.DeviceRemoveParams` | `DeviceRemoveResult` |
| `devices.rename` | `in.DeviceRenameParams` | `DeviceRenameResult` |

## factory methods

| method | params | result |
|---|---|---|
| `factory.access_cancel` | `in.FactoryAccessParams` | `FactoryAccess` |
| `factory.access_check` | `in.FactoryAccessParams` | `FactoryAccess` |
| `factory.access_sign_in` | `in.FactoryAccessParams` | `FactoryAccess` |
| `factory.activity` | `in.FactoryActivityParams` | `FactoryActivity` |
| `factory.attach` | `in.FactoryAttachParams` | `FactoryConnection` |
| `factory.capabilities` | `in.Empty` | `FactoryCapabilities` |
| `factory.detach` | `in.FactoryRef` | `Empty` |
| `factory.job` | `in.FactoryJobParams` | `Job` |
| `factory.job_approve` | `in.FactoryJobApproveParams` | `FactoryJobActed` |
| `factory.job_assign_to_me` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_brief_seen` | `in.FactoryJobBriefSeenParams` | `FactoryJobActed` |
| `factory.job_change_assignee` | `in.FactoryJobAssigneeParams` | `FactoryJobActed` |
| `factory.job_brief` | `in.FactoryJobBriefParams` | `FactoryJobBrief` |
| `factory.job_brief_spot` | `in.FactoryJobBriefSpotParams` | `FactoryJobBriefSpot` |
| `factory.job_brief_write` | `in.FactoryJobBriefWriteParams` | `FactoryJobBriefWritten` |
| `factory.job_chats` | `in.FactoryJobChatsParams` | `FactoryJobChats` |
| `factory.job_send` | `in.FactoryJobSendParams` | `FactoryJobSent` |
| `factory.job_close` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_control` | `in.FactoryJobControlParams` | `FactoryJobActed` |
| `factory.job_create` | `in.FactoryJobCreateParams` | `FactoryJobActed` |
| `factory.job_dismiss` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_drop` | `in.FactoryJobDropParams` | `FactoryJobActed` |
| `factory.job_follow` | `in.FactoryJobFollowParams` | `FactoryJobActed` |
| `factory.job_hold` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_pr_ready` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_merge` | `in.FactoryJobMergeParams` | `FactoryJobActed` |
| `factory.job_reject_plan` | `in.FactoryJobRejectPlanParams` | `FactoryJobActed` |
| `factory.job_reopen` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_request_changes` | `in.FactoryJobChangesParams` | `FactoryJobActed` |
| `factory.job_restore` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_resume` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_publish_anyway` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_retry_publish` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_run_as_new` | `in.FactoryJobRunAsNewParams` | `FactoryJobActed` |
| `factory.job_run_again` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_stop_after_step` | `in.FactoryJobStopAfterStepParams` | `FactoryJobActed` |
| `factory.job_spend_answer` | `in.FactoryJobSpendAnswerParams` | `FactoryJobActed` |
| `factory.job_set_budget` | `in.FactoryJobBudgetParams` | `FactoryJobActed` |
| `factory.job_set_model` | `in.FactoryJobSetModelParams` | `FactoryJobActed` |
| `factory.job_set_priority` | `in.FactoryJobPriorityParams` | `FactoryJobActed` |
| `factory.job_start` | `in.FactoryJobStartParams` | `FactoryJobActed` |
| `factory.job_step_edit` | `in.FactoryJobStepEditParams` | `FactoryJobStepEdited` |
| `factory.job_step_edits_read` | `in.FactoryJobStepEditsParams` | `FactoryJobStepEdits` |
| `factory.job_step_rerun` | `in.FactoryJobStepParams` | `FactoryJobActed` |
| `factory.job_step_stop` | `in.FactoryJobStepParams` | `FactoryJobActed` |
| `factory.job_stop` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_unassign` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.issue_comment` | `in.FactoryIssueCommentParams` | `FactoryIssueCommented` |
| `factory.job_undo` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.link_copy` | `in.FactoryLinkCopyParams` | `FactoryLinkMade` |
| `factory.link_create` | `in.FactoryLinkCreateParams` | `FactoryLinkMade` |
| `factory.link_revoke` | `in.FactoryLinkRevokeParams` | `FactoryLinkRevoked` |
| `factory.links` | `in.FactoryLinkParams` | `FactoryLinkList` |
| `factory.list` | `in.Empty` | `FactoryList` |
| `factory.local_adopt` | `in.FactoryLocalParams` | `FactoryLocalStatus` |
| `factory.local_run` | `in.FactoryLocalParams` | `FactoryLocalStatus` |
| `factory.local_start` | `in.FactoryLocalParams` | `FactoryLocalStatus` |
| `factory.local_status` | `in.FactoryLocalParams` | `FactoryLocalStatus` |
| `factory.me` | `in.FactoryRef` | `FactoryMe` |
| `factory.people` | `in.FactoryRef` | `People` |
| `factory.pipeline` | `in.FactoryRepoParams` | `FactoryPipeline` |
| `factory.pipeline_copy_clear` | `in.FactoryPipelineCopyClearParams` | `FactoryPipelineCopyDone` |
| `factory.pipeline_copy_set` | `in.FactoryPipelineCopySetParams` | `FactoryPipelineCopyDone` |
| `factory.pipeline_pr` | `in.FactoryPipelinePRParams` | `FactoryPipelinePRDone` |
| `factory.pipeline_prs` | `in.FactoryPipelinePRsParams` | `FactoryPipelinePRs` |
| `factory.pipeline_preview` | `in.FactoryPipelinePreviewParams` | `FactoryPipelinePreview` |
| `factory.pipeline_source` | `in.FactoryPipelineSourceParams` | `FactoryPipelineSource` |
| `factory.queue` | `in.FactoryRef` | `Queue` |
| `factory.refresh` | `in.FactoryRefreshParams` | `Empty` |
| `factory.estimate_new` | `in.FactoryEstimateNewParams` | `NewEstimate` |
| `factory.read` | `in.FactoryReadParams` | `ReadResult` |
| `factory.read_on_arrival` | `in.FactoryReadOnArrivalParams` | `FactoryReadOnArrival` |
| `factory.rename` | `in.FactoryRenameParams` | `Empty` |
| `factory.repo_facts` | `in.FactoryRepoFactsParams` | `FactoryRepoFacts` |
| `factory.repo_read` | `in.FactoryRepoReadParams` | `FactoryRepoRead` |
| `factory.repo_set` | `in.FactoryRepoSetParams` | `FactoryRepoSetDone` |
| `factory.repos` | `in.FactoryRef` | `FactoryRepos` |
| `factory.revoke_sessions` | `in.FactoryAdminParams` | `FactoryAdminDone` |
| `factory.rules` | `in.FactoryRef` | `FactoryRules` |
| `factory.rules_set` | `in.FactoryRulesSetParams` | `FactoryRules` |
| `factory.runner_slots` | `in.FactoryRunnerSlotsParams` | `FactoryRunnerSlotsDone` |
| `factory.runners` | `in.FactoryRef` | `Runners` |
| `factory.scout` | `in.FactoryScoutParams` | `ScoutLook` |
| `factory.session_restore` | `in.FactorySessionRestoreParams` | `FactorySessionRestored` |
| `factory.sessions_take` | `in.Empty` | `FactorySessionsTaken` |
| `factory.setup` | `in.FactoryRef` | `FactorySetup` |
| `factory.setup_back` | `in.FactoryLocalParams` | `FactorySetupView` |
| `factory.setup_clone` | `in.FactorySetupFolderParams` | `FactorySetupView` |
| `factory.setup_connect` | `in.FactorySetupConnectParams` | `FactorySetupView` |
| `factory.setup_continue` | `in.FactoryLocalParams` | `FactorySetupView` |
| `factory.setup_discover` | `in.FactoryLocalParams` | `FactorySetupView` |
| `factory.setup_done` | `in.FactoryLocalParams` | `FactorySetupView` |
| `factory.setup_folder_check` | `in.FactorySetupFolderCheckParams` | `FactorySetupFolder` |
| `factory.setup_read` | `in.FactoryLocalParams` | `FactorySetupView` |
| `factory.setup_retry_read` | `in.FactorySetupRepoParams` | `FactorySetupView` |
| `factory.setup_select` | `in.FactorySetupSelectParams` | `FactorySetupView` |
| `factory.setup_skip` | `in.FactorySetupRepoParams` | `FactorySetupView` |
| `factory.setup_use_folder` | `in.FactorySetupFolderParams` | `FactorySetupView` |
| `factory.sign_in_cancel` | `in.FactorySignInFlowParams` | `Empty` |
| `factory.sign_in_poll` | `in.FactorySignInFlowParams` | `FactorySignInState` |
| `factory.sign_in_start` | `in.FactorySignInStartParams` | `FactorySignIn` |
| `factory.sign_out` | `in.FactoryAdminParams` | `FactoryAdminDone` |
| `factory.spend_by_model` | `in.FactorySpendByModelParams` | `FactorySpendByModel` |
| `factory.usage_by_model` | `in.FactoryUsageParams` | `FactoryUsage` |
| `factory.spend_by_repo` | `in.FactorySpendByRepoParams` | `FactorySpendByRepo` |
| `factory.spending` | `in.FactoryRef` | `FactorySpending` |
| `factory.spending_set` | `in.FactorySpendingSetParams` | `FactorySpending` |

An engine whose hello carries the features `factory.default_budget`,
`factory.auto_start_cap` and `factory.default_model` serves
`factory.spending {factory}` and `factory.spending_set {factory,
default_budget_usd?, auto_start_max_usd?, default_model?, expect?}`. Both answer
`{factory, default_budget_usd, auto_start_max_usd, default_model}`, the values the
factory keeps for every repository it watches (`GET` and `PATCH /v1/spending`).
`default_budget_usd` is the budget a job gets when it has none and its
repository's file names no `default_budget`: a positive amount, or `null` for the
built-in $5. `auto_start_max_usd` is the most a job may be budgeted and still
start on its own: zero or more, or `null` for the built-in $5; a repository's
stricter `start.max_cost` still holds, and zero makes every such job ask first.
`default_model` is the model a step runs on when neither the step nor its job
names one: a `models.list` id, or `""` for the runner's own default; a runner on
one model for every task keeps that model. Each figure is at most $10,000. A set
changes only the fields it sends (at least one); `null` clears a figure and `""`
the model. Only the factory's operator may set them (`forbidden` otherwise).
`expect` is the three values as last read; a factory that keeps others answers
`stale` and changes nothing. A refused value is `bad_params` before anything is
sent; an unconfirmed write is `uncertain`.

`factory.job_create` keeps `title` trimmed and separate from literal `text`,
including details whitespace in the job ask and body; empty details stay empty.
Neither field lifts
terminal chips or changes budget, approval steps, rounds, effort or security.
The job title alone supplies the draft PR title. Existing jobs are not rewritten.
The method, reply, `factory.job_actions` hello feature and error codes are unchanged.
Factories without the separate-title capability retain their prior behavior.


An engine whose hello carries `factory.stop_any_version` accepts
`factory.job_stop {factory, job, expect?}` and
`factory.job_step_stop {factory, job, step, expect?}`. An omitted `expect`
is accepted; when supplied it retains the existing shape, including a positive
`version`. On an active job, a mismatching `expect` does not refuse Stop.
Step Stop still requires the named step to be current and running, and refuses
finished or publishing jobs. Ended jobs retain their existing `stale` or
`conflict` refusal; permissions are unchanged. Replies remain `FactoryJobActed`
and errors retain their existing codes, including `bad_params` for malformed
requests. Other job actions retain their guards except Pause and Resume, described below.

`factory.job_pause` in hello features says `factory.job_hold` also pauses a
running job. Both `factory.job_hold` and `factory.job_resume` take
`{factory, job, expect: {version}}` (`FactoryJobActionParams`) and return
`FactoryJobActed`, with action `hold` or `resume`. Hold keeps a queued job from
starting or cancels a running step's current model round. Its work, conversation
and recorded spending stay. Resume continues the same step and round, reopening
its conversation when available. Pause releases the worker lease and capacity
slot after the cancelled round's final work and spending are saved. Resume queues
the saved run for a worker: it continues as soon as a slot is free, or reads as
an ordinary queued job with the existing waiting presentation while slots are busy.
No model round starts while it waits. Progress alone never makes Pause or Resume
stale: both accept an outdated `expect.version`. A stopped or finished job refuses
Pause with `conflict` and “<job> is not running”; a stopped job refuses Resume
with `conflict` and “<job> is not paused”. A refusal changes neither the job nor
its activity record and starts no model round. Stop committed before Resume wins.
No additional hello feature is needed; desks still send the same `expect` shape.

The job's person may use these actions. `can.hold` permits a queued or running
job, excluding a job waiting for a person, finished or publishing; `can.resume`
permits a paused job. A paused running job keeps `state: "running"` even
after its lease is released, with `can.resume: true`,
`state_presentation.kind: "paused"`, title `Paused`, and no new question.
After Resume, `state` is `queued` until a worker takes a free slot; its paused
flag is cleared and the ordinary queue presentation applies. Its activity says `paused`, then `resumed`, and Held/Resumed receipts
name the caller. Explicit Resume also writes exactly one `resumed` line into the
manager chat, including while the old lease drains; a repeated Resume adds none.
Automatic continuation does not add this explicit-action note. Existing errors apply: `forbidden` for another person's job,
`conflict` for an inapplicable state, and
`bad_params` for an
invalid request. Neither action resets spending. If the saved chat cannot be reopened, the step
uses a new chat.


`factory.job_step_rerun` records retry progress as `trying <step> again` in the
manager conversation. Retry and `factory.job_control` handovers do not emit a
person's `stopped` progress fact. `factory.job_stop` and `factory.job_step_stop`
retain that fact. Their parameters, replies, hello features and error codes are
unchanged.

On the worker HTTP interface, a revoked lease still returns HTTP 409. Retry or
manual takeover adds `X-Codeaf-Lease-Withdrawn: 1`, so the worker cancels its old
execution without reporting a person's Stop. The latest withdrawn lease is
retained per worker until that worker pulls again.

`factory.job_step_rerun` records retry progress as `trying <step> again` in the
manager conversation. Retry and `factory.job_control` handovers do not emit a
person's `stopped` progress fact. `factory.job_stop` and `factory.job_step_stop`
retain that fact. Their parameters, replies, hello features and error codes are
unchanged.

On the worker HTTP interface, a revoked lease still returns HTTP 409. Retry or
manual takeover adds `X-Codeaf-Lease-Withdrawn: 1`, so the worker cancels its old
execution without reporting a person's Stop. The latest withdrawn lease is
retained per worker until that worker pulls again.

`factory.job_step_rerun` records retry progress as `trying <step> again` in the
manager conversation. Retry and `factory.job_control` handovers do not emit a
person's `stopped` progress fact. `factory.job_stop` and `factory.job_step_stop`
retain that fact. Their parameters, replies, hello features and error codes are
unchanged.

On the worker HTTP interface, a revoked lease still returns HTTP 409. Retry or
manual takeover adds `X-Codeaf-Lease-Withdrawn: 1`, so the worker cancels its old
execution without reporting a person's Stop. The latest withdrawn lease is
retained per worker until that worker pulls again.

`factory.job_step_rerun` records retry progress as `trying <step> again` in the
manager conversation. Retry and `factory.job_control` handovers do not emit a
person's `stopped` progress fact. `factory.job_stop` and `factory.job_step_stop`
retain that fact. Their parameters, replies, hello features and error codes are
unchanged.

On the worker HTTP interface, a revoked lease still returns HTTP 409. Retry or
manual takeover adds `X-Codeaf-Lease-Withdrawn: 1`, so the worker cancels its old
execution without reporting a person's Stop. The latest withdrawn lease is
retained per worker until that worker pulls again.

An engine whose hello carries `factory.spend_by_model` (and capabilities
`spend_by_model: true`) serves `factory.spend_by_model {factory, from, to}`:
the same days and the same job spend as `factory.spend_by_repo`, from
`GET /v1/spend/models`, as `{factory, from, to, time_zone?, models, unknown_usd?}`.
Each model is `{model, cost_usd, calls, input_tokens?, output_tokens?, jobs}`,
dearest first and then by name. `unknown_usd` is spend in that range that was
not recorded against a model, including every job from before model lines
were kept; it is omitted when there is none. The model costs and
`unknown_usd` add up to `factory.spend_by_repo`'s `total_usd` for the same
days. A triage read, a scout, and page AI that is not booked on a job are in
neither total. A factory older than the route answers not found.
The job manager's model tools also expose the plan send-back: `factory_status`
includes the exact pending `question_id` in its text at every waiting gate.
`factory_answer {action: "reject_plan", question_id, answer}` sends the requested
changes (1–4,000 characters) through the same typed plan-revision door as
`factory.job_reject_plan`. It checks the manager binding and exact decision on
the locked job; a stale decision changes nothing. The prior step runs again with
the words in a new round, then approval is requested again. Omitting `action`
retains persisted step replies; other gates keep their existing job controls.
These model tools add no app-wire method, hello feature or wire error code.
A confirmed send-back reports acceptance, not worker startup; an unconfirmed
result directs the manager to read status before retrying and explain the
outcome to the person without internal identifiers.

Cap questions use one next budget: `factory.job`'s existing
`spending_limit.next_limit` and `questions[].next_limit` are the smallest of
$5, $10, $20, $30 strictly above recorded spend. Once spend reaches $30,
double the current cap, repeatedly if needed, until it is above spend.
The next limit is never zero. A new question says
`budget of $30 reached · $31 spent · raise it to $60, or stop?`. Yes or
continue, including on an older saved question, uses that same next cap.
The desk sends that next cap as the existing `factory.job_set_budget`'s
`budget`, never as an answer field. No new field, method, hello feature, or
error code is added; `factory.job_actions` remains the existing feature.

`factory.job_set_budget` keeps its existing params (`factory`, `job`, `budget`,
required `expect.version`) and `FactoryJobActed` reply. Budgets accept $5, $10, $20, $30 or exactly the job's next cap.
Other amounts are refused with HTTP 422 (`bad_input`), mapped to app-wire
`bad_params`. Raising the cap above recorded spend resolves a `cap` question and resumes
the interrupted step in the same conversation and round, with spend retained,
in Auto or Manual on local and leased runners. Manual still asks before its next
step. Take over and publishing jobs do not resume through a budget change.
Chat records one
`budget raised to $10 · carrying on` line for a $10 cap. An increase at or below
spend updates the question and keeps work waiting. A decrease below spend is
still refused (`bad_params`). Other questions and publication approvals are
unchanged. This corrects existing behavior; method names, errors and hello
feature identifiers are unchanged.
An engine whose hello carries `factory.plan_files_answer` reads structured plan
files. `factory.job` keeps its existing `{factory, job}` request and `Job` reply;
`steps[].files` carries the plan step's answer (`null` for an absent field, `[]`
for an explicit unknown list). Worker step reports and pulls carry the same
`files` array in `StepResult`. `planned_files` is derived from the current finished
plan, never from prose or a cache. No method or error code changes.

The plan's model fills `stage_result.files` as part of its answer, alongside
`output`; the plan schema requires the field. At most 200 entries, 512 bytes per
path, with the strict repository-relative declaration path rules. Any invalid
entry, empty list or disagreement with a `Files:` declaration makes the list
unknown. An absent field falls back to the strict declaration for older plans.
One existing correction attempt asks for a missing or invalid list within the
existing step budget. A provider that ignores the schema can still omit it;
the protected-path gate then pauses safely. The stored unknown-list hold suffix
remains `the plan names no files`; visible words describe the missing checkable
list rather than claiming the plan names no files in prose.

`factory.job.approval` keeps the latest settled decision, including its kind,
actor and times, while later work or gates wait and after restarting the app.
A later gate retains its own waiting step, `needs_you` and `can.approve`; the
earlier decision never passes it. Beginning another approval replaces the single
summary with that decision and its own undo window. Activity keeps the receipts.
No method, parameter, reply field, error code or hello feature changes.

`factory.activity` records automatic approvals and their publications with
`actor: {kind: "factory"}`, an empty `handle`, and no actor name. A person's
approval keeps `actor: {kind: "person", name: "@handle"}` and their handle.
Automatic approval has an empty `approval.decided_by`. A stale action after
an automatic approval says `approved automatically at HH:MM` (UTC), with no
`by` or `approved_by`; `reason: "stale"`, the receipt event and commit remain.
Saved events keep their original attribution. Automatic approval trailers
use `Factory-Approval: evt_<id> factory <commit>`; personal trailers keep
`@handle`. Workers must accept the factory actor to publish automatic approvals.
No method, parameter, reply field, error code or hello feature is added.

An engine whose hello carries the feature `factory.rules_changed_by` answers
`factory.rules` and `factory.rules_set` with `changed_by`, the GitHub login of
the person who made the last write of the factory's rules, and `changed_at`,
the RFC3339 time of that write in UTC to the second, beside `rules`:
`{"factory": "...", "rules": {...}, "changed_by": "octocat",
"changed_at": "2026-10-11T12:00:00Z"}`. The rules belong to the factory, not to
one repository, so there is one pair per factory. Both fields are absent when
the rules were never written, and when the factory last wrote them with a build
that did not record the change. A write sent with the factory's script
credential records the factory's operator.

An engine whose hello carries `factory.repo_facts` serves
`factory.repo_facts {factory, repo, days?, months?}`: one repository's facts
counted by the factory over every job it keeps (`GET
/v1/repos/{owner}/{repo}/facts`), as `{repo, as_of, time_zone?, days, months,
facts, truncated?}`. Each fact is `{id, kind, value, phrase, about?, source}`
with `source` beginning `engine:`: jobs finished and pull requests merged in
the last `days` (1 to 366, default 30) and this month, spend for each of the
last `months` calendar months (1 to 12, default 3) by the rule of
`factory.spend_by_repo`, jobs, rounds and questions by area by the rule of
`factory.job_area`, and jobs stuck on one step for a day or more. At most 20
areas and 20 stuck steps are answered, busiest first (at most 97 facts);
`truncated` counts the rest. A repository the caller cannot read is not found.

An engine whose hello carries the feature `factory.pipeline_pr_update` (and
capabilities `pipeline_pr_update: true`) lets `factory.pipeline_pr` take `pr`,
the number of an open pull request from a branch of the same repository into
its default branch, and optionally `expected_head_sha`. The file is committed
onto that pull request's head branch: `outcome` is `updated` (`pr.head_sha` is
the new commit) or `unchanged` (`pr` is the pull request as it stands), and a
refusal before any write names stage `head`. `branch`, `body` and
`expected_base_sha` are refused with `pr`. An engine without the feature refuses
`pr` as an unknown field.

An engine whose hello carries the feature `factory.pipeline_source_ref` (and
capabilities `pipeline_source_ref: true`) lets `factory.pipeline_source` take
`ref`, one string: an open pull request's number in decimal (`"42"`), resolved
to its head when the read is made, or the 40-hex commit at its head. The pull
request must be open, from a branch of the same repository into its default
branch, and change only the pipeline file; anything else is `conflict`. The
answer is that file at that head, with `base_sha` the head and `ref` and `pr`
saying what was read; `factory.pipeline_preview` takes that `base_sha`, and
`factory.pipeline_pr` takes it as `expected_head_sha` with `pr`. A read without
`ref` answers no `ref` or `pr`. An engine without the feature refuses `ref` as
an unknown field; a factory older than the read refuses it, and the engine says
the factory cannot read a pull request.
An engine whose hello carries the feature `factory.repo_read` (and
`FactoryCapabilities.repo_read`) serves `factory.repo_read {factory, repo, op,
path?, pattern?, from?, to?, job?}`: one read-only look at the factory's own
checkout (`GET /v1/repos/{owner}/{repo}/read`), from git's object store at the
default branch's tip, or at job `job`'s head commit when the checkout has it.
`op` is `tree` (a folder's children: `entries` of `{path, type, size?}`, at most
500), `grep` (lines matching the RE2 `pattern`, at most 256 bytes: `hits` of
`{path, line, text}`, at most 50, searched for at most 5 seconds) or `read`
(lines `from`..`to` of one file: `text`, at most 400 lines and 32 KiB, with
`size` and `eof`). `truncated` says a bound stopped the answer. `untrusted` is
always true: the text is the repository's own and is data, never
instructions. Credential-looking paths (`.env*`, `*.pem`, `*.key`, `id_*`,
`.git/`, `.npmrc`, `.netrc`, `.pypirc`, `credentials*`, `*secret*`, `*.p12`,
`*.pfx`, `.aws/`, `.ssh/`, `.docker/config.json`, `kubeconfig`) are absent from
`tree` and `grep`, and naming one is `forbidden` with the message `denied`; a
binary file is `unreadable` with `binary`; a repository with no checkout on the
factory's machine is `not_cloned`; one the caller cannot see is `not_found`. A
factory older than the route is `conflict`.

A job read (`factory.job`, and jobs in `factory.queue`) carries
`start_rules {ask_first, never, unread?, unread_why?, unread_file?, unread_line?}` after it starts.
`unread: true` means the repository's `.codeaf/factory.yaml` could not be read.
A person's Start still succeeds, using the factory's rules; a job without its
own budget receives the built-in default job budget. Its plan holds for a
person, including in Auto. Activity says `Started with the repository's rules
unread; its plan will wait for you`. `unread_why` names the file, the parser's
line when available, and a safe explanation (one line, under 160 characters).
`unread_file` is the repository-relative path (`.codeaf/factory.yaml` today);
`unread_line` is the parser's positive line number, omitted when unknown.
The plan approval question includes `unread_why`; no build step starts until
a person approves, including in Auto. These fields survive worker reports.
It carries no raw transport errors, absolute paths or credentials. Fix the
named file before reviewing the plan. A clean read without a positive default
budget still refuses a job without its own budget. Unread factory rules still
refuse Start and leave the job new. These are additive job fields under the
existing factory features; method parameters and replies otherwise stay the
same.
A job's `steps[].rounds[]` carries the engine's optional `result` word:
`done`, `not done`, `clean`, `not clean`, `green`, `not green`, `proven`,
`not proven`, or `blocked` for a reported result. When a retry closes a step
that ended on a recorded failure, failure code `stopped` sends
`result: "stopped"`, and `machine_lost` (including a runner lost across an
engine restart) sends `result: "interrupted"`. Both have `rejected: false`
(the false field may be omitted). Other step errors send `result: "failed"`
and `rejected: true`. Display an explicit result word when present;
otherwise `rejected: true` can read as "didn't pass". Only rejected rounds
with a later round warrant "fixing them". Summary text never classifies an
outcome. Existing saved lines are not migrated.

This corrects outcomes carried by the existing `factory.job` and
`factory.queue` replies; method parameters, hello features and error codes
are unchanged. Round numbering and limits are unchanged: an explicit rerun
advances the round and grants the step's configured allowance anew; automatic
recovery still consumes its separate bounded retry allowance. Result words
and the rejected flag do not determine those allowances.

An engine whose hello carries `factory.job_area` sends `area` on each job of
`factory.queue` and on `factory.job`: the part of the repository the job's
change touched most, the repository's declared area or else the top-level
directory with a trailing slash (`"cli/"`). Under a container directory (`src`,
`internal`, `pkg`, `lib`, `app`, `apps`, `packages`, `crates`, `cmd`) it is the
first two segments (`"internal/factory/"`), and a file directly inside one is
the container (`"internal/"`). Every response that carries the job says the
same area. It is absent when the job changed no file or only files at the root.

An engine whose hello carries the feature `factory.estimate` reads an issue
with a complexity rubric and measures cost from its own finished jobs; a model
never writes a price. A job's `triage.rubric` is `{scope, clarity, coupling,
proof, novelty}`, each `{score 0..2, why?, low?}` (`low`: judged without the
repository), plus `from` (`read` or `scout`), `complexity` (0..10) and `band`
(`small` 0–3, `medium` 4–6, `large` 7–10), with `band_near` naming the
neighbouring band when the sum sits on an edge (3 or 4, 6 or 7) so the desk
shows both; `triage.est` is gone. An open job
carries `estimate {usd_lo, usd_hi, min_lo, min_hi, n, repo?, widened?, basis}`:
the 10th to the 90th percentile of `n` finished jobs (about eight in ten land
inside), in `repo` (absent: every repository). `basis` is `read` when they are
the nearest on the job's read, or `kind` when the job has no read or too few
similar read jobs and they are the latest finished jobs of the same kind, read
or not (the desk says `from 12 jobs in api, not read`). Every finished job
counts. The estimate is absent while fewer than five jobs finished on both
bases. A running job's `estimate.left {usd_lo, usd_hi, n}` is what similar
finished jobs spent past this job's spend so far, over the `n` that spent at
least that much (absent below five; a running job with `left` alone has `n` 0).
`queue.desk.estimates {jobs, hit, of, basis?}` is how many finished jobs the
figures see and how many of `of` backtested jobs spent inside their range;
`basis {read?: {hit, of}, kind?: {hit, of}}` is the same per basis, which is
backtested and widened on its own.

An engine whose hello carries the feature `factory.estimate_new` serves
`factory.estimate_new {factory, repo, kind}` (`GET
/v1/repos/{owner}/{repo}/estimate?kind=K`): what a job of that kind will
likely cost before it exists, for New job's Start now. It answers
`NewEstimate {factory, repo, kind, estimate?}`, where `factory` is the
factory itself (`{name, address, version}`, as in a queue) and `estimate` is the kind
pool's figure (basis `kind`, the repository first, then every repository),
from the finished jobs the caller can see, and is absent below five. The
repository dry run adds `cost_jobs`, that repository's finished jobs.

An engine whose hello carries the feature `factory.scout` serves
`factory.scout {factory, job, refresh?}` (`POST /v1/jobs/{id}/scout`): Look
closer. A model with `factory.repo_read`'s read-only tools on the factory's
own checkout reads the job's issue again, within at most 12 tool calls, one
minute and $0.10, and answers `ScoutLook {factory, job, cached, scout}`.
`scout` is `{files [{path, why}], plan [three steps], unknowns [questions],
tests [paths], rubric (from `scout`, each factor with `cite` "path:line"),
cited, dropped, calls, usd, model, took_ms, head, at, stale}`. Every path it
shows exists at `head`; `dropped` counts the paths the model named that did not
(`cited` is how many it named). The same issue text at the same head answers
the kept look free (`cached: true`); `stale` on a job's look says the issue
changed since (a moved head is found by the next look, which then runs anew),
and `refresh: true` asks for a new one. The look also lands on the job
(`job.scout`, its rubric replacing the read's), and its cost goes on the spend
ledger as `factory scout`. A new look is refused `limit` at the month's $10 of
factory looks ("Monthly limit for AI reached") or 20 looks a day in one
repository ("Daily limit for this repo reached"); a look with no answer within
its budget is `no_answer`; a repository with no checkout is `not_cloned`.
`queue.desk.scout_price` is what a typical look cost, absent before the first.

An engine whose hello carries the feature `factory.read_on_arrival` serves
`factory.read_on_arrival {factory, on?}` → `{on}`: "Read new issues as they
arrive", the factory's `read_on_arrival` in `GET /v1/settings`. Off (the
default: AI runs only on a person's click), the factory's AI reads
nothing by itself; on, each new issue gets the arrival read and a job the start
rules would start gets a look closer before it launches. Either way a read never
starts a job: size labels and the start rules decide; a read's rubric only places
the job among similar finished jobs, so its measured cost over `max_cost` asks
first. With `on` the operator changes
it (`PATCH /v1/settings`); anyone else is `forbidden`.

An engine whose hello carries the feature `factory.read` serves `factory.read
{factory, jobs}` (`POST /v1/read`): the arrival read of exactly the chosen jobs,
now, whatever `read_on_arrival` says. It answers `ReadResult {read, skipped,
failed}`: jobs read now; jobs skipped (already read, not an issue job, not
visible to the caller, or past the first 20 read); jobs whose model call
failed, left unread to try again. A factory with no model key fails every job.
`queue.desk.last_read_cost` is what the last read cost, for "about $X".
Both read and scout bank each reported provider charge once on Spending,
including failed generations. Their methods, replies and hello features are unchanged.

An engine whose hello carries the feature `factory.repo_checkout` answers
`factory.queue` with `checkout_state` on each of `repos[]`: how a run of that
repository gets its folder on the factory's machine, in the words a job's own
`checkout` says and from the same check a start makes. It is `here` (a
checkout is known), `will_clone` (the runner clones it on the first job's
first step; the normal state of a repository no job has run on yet) or
`missing` (a start is refused 409 `needs_checkout`), and it never names a path.
It is absent on a factory that does not know its machine's checkouts and on an
attached factory older than the field, which says nothing rather than a guess.

An engine whose hello carries the feature `factory.repo_read` (and
`FactoryCapabilities.repo_read`) serves `factory.repo_read {factory, repo, op,
path?, pattern?, from?, to?, job?}`: one read-only look at the factory's own
checkout (`GET /v1/repos/{owner}/{repo}/read`), from git's object store at the
default branch's tip, or at job `job`'s head commit when the checkout has it.
`op` is `tree` (a folder's children: `entries` of `{path, type, size?}`, at most
500), `grep` (lines matching the RE2 `pattern`, at most 256 bytes: `hits` of
`{path, line, text}`, at most 50, searched for at most 5 seconds) or `read`
(lines `from`..`to` of one file: `text`, at most 400 lines and 32 KiB, with
`size` and `eof`). `truncated` says a bound stopped the answer. `untrusted` is
always true: the text is the repository's own and is data, never
instructions. Credential-looking paths (`.env*`, `*.pem`, `*.key`, `id_*`,
`.git/`, `.npmrc`, `.netrc`, `.pypirc`, `credentials*`, `*secret*`, `*.p12`,
`*.pfx`, `.aws/`, `.ssh/`, `.docker/config.json`, `kubeconfig`) are absent from
`tree` and `grep`, and naming one is `forbidden` with the message `denied`; a
binary file is `unreadable` with `binary`; a repository with no checkout on the
factory's machine is `not_cloned`; one the caller cannot see is `not_found`. A
factory older than the route is `conflict`.

An engine whose hello carries the feature `factory.job_brief` serves
`factory.job_brief {factory, job, since?}`: the job's newest brief version as
its factory keeps it (`GET /v1/jobs/{id}/brief`). `status` is `ready`,
`generating`, `failed` (with `failure` and `failed_after`) or `stub`; a stub
(`brief.stub: true`, `rev: 0`) is made from engine data alone before the job
has a version: the issue's line, `steps_done` of `steps_total`, the facts and
no blocks. `brief.facts` are computed by the factory from the steps' structured
results (test counts or exit, findings, proof claims shown of total, rule
checks), never by a model; each names its `evidence {step, try, kind, ref?}`.
`changed_since_seen` lists the keys (`verdict`, a fact's `key`, a block's
`kind/heading`) changed after rev `since`, or by the newest version when
`since` is absent. A factory older than the route is `conflict`.

With hello feature `factory.job_brief_verdict_behind`, the reply also carries
`verdict_behind: bool`, including in `factory.job_brief_write.current`. The engine
compares the newest kept version's input hash with the current job input: a
changed state, approval, publication receipt or newer step makes it true until a
new version is kept. A failed write keeps the old verdict and its hash. The desk
shows that verdict as out of date when this flag is true. A stub is false; a kept
legacy version without an input hash is true until refreshed. Params and error
codes are unchanged.

An engine whose hello carries `factory.job_brief_writer` writes the briefs of
this machine's factory: after a step reports, when a step waits on its person,
when proof waits for publication approval, and at publication or the job's end.
Publication includes a PR opened or updated and a comment posted; end outcomes
include shipped, merged, closed without merge, stopped and failed runs, plus a
failed publication. Each job's news is taken as one after 10 seconds. Lifecycle
triggers come from committed store updates, separately from chat step metadata.
Auto's routine proof approval does not force an intermediate write; an exception
waiting on a person does. A normal manual job gets two additional forced writes:
at proof approval and after publication, folded when they share a burst. A
forced update that finds an earlier write generating remains in the debounce
until the writer is available. Stops before a round starts refresh too; they
need no failed-step record. The write is one errand
on the job's agent under the `brief` role (the small-work tier the small-jobs
model pins). The factory checks the answer against the job (`POST
/v1/jobs/{id}/brief/begin` and `/brief/keep`): the palette's limits, evidence
that names a step and try the job has, and every number in a claim found in its
evidence. The writer's input includes the job state in words, whether proof
approval waits, the approval kind and state, the PR number and state, publication
attempt and outcome, comment receipt and how the job ended. These facts enter the
input hash, so a lifecycle change cannot skip as unchanged. Evidence kind `pr`
refers to the published PR with no step and supports its number; an unrelated or
unsupported number is still rejected. The session books each provider call to the ledger and job's spend
once, including rejected drafts and calls whose keep request fails. Keeping a
version records its cost for the brief guard without charging the job again.
An app-side brief's charge remains on the factory when a remote worker reports
its additional spending. A failing verdict keeps the previous version and marks the brief `failed`. Without
a person's ask, a version is written only when the last one cost at most $0.03
and the brief's spend stays within 5% of the job's; an ask opening, the job's
end and a retry are written regardless, and unchanged inputs are never written
twice. `factory.job_brief_write {factory, job}` is the page's Retry: it answers
`{skip?, kept, drops: [{part, reason}], model?, cost_usd?, current:
FactoryJobBrief}`, with `skip` `writing` while another write is under way.

### Job references and jobs without a GitHub issue

`factory.job` and `factory.queue` return a job's `ref` as display text:
`api#87` for GitHub issue 87, or `job 21` when `num` is zero. A local
job id is never a GitHub issue number. Step briefs for such a job explicitly
say it has no GitHub issue, including Plan, Review and Proof.

The numeric `job_id` is the factory document id used as the `job` parameter
in app methods. The string `id` is an opaque compatibility key: it retains
its previous spelling (including `api#21` for a job with no issue). Use `ref`
for labels; never parse either identity into a GitHub link. Existing HTTP
reference lookups, worktree names and saved chat markers remain valid;
saved jobs need no migration. GitHub links come from the job's `url` and
actual issue `num`, never the local document id.

`factory.issue_comment {factory, job, body}` refuses a job with `num: 0`
with wire code `conflict`, typed outcome `refused`, and the message
"this job has no GitHub issue to comment on". It makes no GitHub call.
The existing `factory.remote` and `factory.issue_comment` hello features
continue to apply; no method, parameter or reply field is added.

## files methods

| method | params | result |
|---|---|---|
| `files.complete` | `{agent, limit, path}` | `PathCompletion` |
| `files.diff` | `{agent, base, path}` | `FileDiffResult` |
| `files.find` | `{agent, limit, query}` | `GroupsResult` |
| `files.read` | `{agent, limit, offset, path}` | `FileReadResult` |
| `files.stat` | `{agent, paths}` | `FilesResult` |

## harness methods

| method | params | result |
|---|---|---|
| `harness.list` | `{}` | `HarnessListResult` |

## health methods

| method | params | result |
|---|---|---|
| `health.check` | `{}` | `HealthResult` |

`health.check` answers plain facts an assistant can cite: `version` and
`build`, `started_at` and `uptime_s` for this engine process, `resume` when
this start carried on replies or tasks the last quit stopped (`at`,
`replies_resumed`, `replies`, `tasks`; absent when there was nothing),
`disks` with `free_bytes` and `total_bytes` for the filesystem of the work
folder (`work_folder`) and of codeaf's home (`codeaf_home`), each with an
`error` instead when it could not be read, and `factory`, the local factory's
status as `factory.local_status` shapes it. It reads memory, the app state and
one filesystem call per disk; it starts nothing. hello says `health.check`.

## inbox methods

| method | params | result |
|---|---|---|
| `inbox.answer` | `{id, option, scope, text, why}` | `UndoUntilResult` |
| `inbox.back` | `in.IDParams` | `Empty` |
| `inbox.later` | `in.IDParams` | `Empty` |
| `inbox.list` | `{list, query}` | `ItemsCountsResult` |
| `inbox.open` | `in.IDParams` | `AgentTaskResult` |
| `inbox.undo` | `in.IDParams` | `Empty` |

## keys methods

| method | params | result |
|---|---|---|
| `keys.check` | `in.KeyParams` | `KeyCheck` |
| `keys.remove` | `in.ProviderParams` | `Empty` |
| `keys.set` | `in.KeyParams` | `Empty` |

## manual methods

| method | params | result |
|---|---|---|
| `manual.corpus.set` | `in.ManualCorpusParams` | `ManualCorpusResult` |

`manual.corpus.set` registers the app's own manual pages, replacing every page
an earlier call set. Each page is `{name, markdown}`: a lowercase slug and its non-empty
Markdown, at most 64 pages, 32 KiB a page and 512 KiB in all; an empty list
removes them. The engine keeps them in memory for its lifetime and the chat's
`manual` tool searches them beside codeaf's own pages, filed as
`workaf/<name>`. The answer lists those names. hello says `manual.corpus.set`.

## media methods

| method | params | result |
|---|---|---|
| `media.attach` | `{agent, source, version}` | `MediaAttachResult` |
| `media.attach_frame` | `{agent, at_ms, png, source, version}` | `MediaAttachResult` |
| `media.attach_range` | `{agent, from_ms, source, to_ms, version}` | `MediaAttachResult` |
| `media.get` | `{agent, source}` | `MediaResult` |
| `media.list` | `{agent, cursor, limit}` | `MediaPage` |
| `media.resolve` | `{agent, expect_version, path}` | `MediaResult` |

## members methods

| method | params | result |
|---|---|---|
| `members.add` | `in.MemberAsk` | `MemberResult` |
| `members.brief` | `{agent, crew}` | `{brief}` |
| `members.brief.set` | `{agent, base, crew, text}` | `{brief}` |
| `members.cap` | `in.MemberAsk` | `MemberResult` |
| `members.handle` | `in.MemberAsk` | `MemberResult` |
| `members.home` | `in.MemberAsk` | `MemberResult` |
| `members.move` | `in.MemberAsk` | `MemberResult` |
| `members.pause` | `in.MemberAsk` | `MemberResult` |
| `members.remove` | `in.MemberAsk` | `MemberResult` |
| `members.resume` | `in.MemberAsk` | `MemberResult` |
| `members.role` | `in.MemberAsk` | `MemberResult` |
| `members.start` | `in.MemberAsk` | `MemberResult` |
| `members.stop` | `in.MemberAsk` | `MemberResult` |

## memory methods

| method | params | result |
|---|---|---|
| `memory.applies` | `in.MemoryAppliesAsk` | `MemoryScopeSnapshot` |
| `memory.forget` | `in.crewID` | `Empty` |
| `memory.list` | `{limit}` | `MemoriesResult` |

## models methods

`models.list` waits at most three seconds for discovery already in flight and
returns the models available then. When a pending catalog or provider listing
completes, the app emits `models.changed` with empty data (`{}`) and the usual
sequence number. Read `models.list` again to obtain the completed list. Repeated
reads do not start polling. Notices enter an ordered queue without holding the
backend lock while waiting for stdout. Close removes the completion subscription
and cancels and joins the initial catalog waiter and wire sender.

| method | params | result |
|---|---|---|
| `models.list` | `{}` | `ModelsResult` |
| `models.pin` | `{ids}` | `SettingsResult` |

Each `Model` in `ModelsResult` carries `id`, `name`, `provider`, and, when
known, `provider_name`, `default`, `listed_at` (the catalog's listing time, Unix
seconds), `price_in_per_m` and `price_out_per_m` (US dollars per million input
and output tokens), `context_length` (tokens) and `arena_elo` (the best Design
Arena Elo the catalog carries). Every one of these facts is absent when nobody
published it. A price is never sent as `0` for an unknown price; a present `0`
is a model the catalog says is free. A model priced by the request has no
per-million figure, so both prices are absent, from a connected service as
from any other. Prices are rounded to a billionth of a dollar per million, so
$0.10 goes out as `0.1`.

## orders methods

| method | params | result |
|---|---|---|
| `orders.change` | `in.orderAt` | `OrderResult` |
| `orders.list` | `{agent}` | `OrdersResult` |
| `orders.made_in` | `in.OrdersMadeAsk` | `OrdersMadeResult` |
| `orders.not_now` | `in.orderAt` | `Empty` |
| `orders.pause` | `in.orderAt` | `OrderResult` |
| `orders.ratify` | `in.orderAt` | `OrderResult` |
| `orders.resume` | `in.orderAt` | `OrderResult` |
| `orders.run` | `{id, run, verdict}` | `OrderResult` |
| `orders.runs` | `{id, limit}` | `RunsResult` |
| `orders.stop` | `in.orderAt` | `OrderResult` |

## ping methods

| method | params | result |
|---|---|---|
| `ping` | `{}` | `Empty` |

## ports methods

| method | params | result |
|---|---|---|
| `ports.inspect` | `{port}` | `PortInspection` |
| `ports.suggest` | `{after}` | `PortSuggestion` |

## power methods

| method | params | result |
|---|---|---|
| `power.sleep` | `{}` | `Empty` |
| `power.wake` | `{}` | `Empty` |

## processes methods

| method | params | result |
|---|---|---|
| `processes.actions` | `in.IDParams` | `ProcessActionsResult` |
| `processes.follow` | `in.IDParams` | `Empty` |
| `processes.list` | `in.AgentParams` | `ProcessesResult` |
| `processes.log` | `in.ProcessLogAsk` | `ProcessLogResult` |
| `processes.log_target` | `in.IDParams` | `ProcessLogTargetResult` |
| `processes.output` | `{from_line, id}` | `LinesNextLineResult` |
| `processes.page` | `in.ProcessPageAsk` | `ProcessPageResult` |
| `processes.rerun` | `in.IDParams` | `ProcessResult` |
| `processes.stop` | `in.IDParams` | `Empty` |
| `processes.stop_all` | `in.ProcessStopAllAsk` | `ProcessStopAllResult` |
| `processes.unfollow` | `in.IDParams` | `Empty` |

## providers methods

| method | params | result |
|---|---|---|
| `providers.balance` | `in.ProviderBalanceParams` | `ProviderBalance` |
| `providers.list` | `{}` | `ProvidersResult` |
| `providers.sign_in` | `in.ProviderParams` | `URLResult` |
| `providers.sign_in_cancel` | `in.providerCancelParams` | `Empty` |
| `providers.sign_in_start` | `in.ProviderParams` | `ProviderSignInStart` |

## queue methods

| method | params | result |
|---|---|---|
| `queue.edit` | `in.queueAt` | `queueResult` |
| `queue.move` | `in.queueAt` | `queueResult` |
| `queue.remove` | `in.queueAt` | `queueResult` |

## quick methods

| method | params | result |
|---|---|---|
| `quick.promote` | `in.IDParams` | `taskResult` |
| `quick.undo` | `in.IDParams` | `Empty` |

## render methods

| method | params | result |
|---|---|---|
| `render.cancel` | `{agent, id}` | `RenderCancel` |
| `render.get` | `{agent, id}` | `RenderResult` |
| `render.list` | `{agent, cursor, limit}` | `RenderPage` |

## roles methods

| method | params | result |
|---|---|---|
| `roles.list` | `{}` | `RolesResult` |
| `roles.update` | `{id, instructions, model, name}` | `RoleResult` |

## rules methods

| method | params | result |
|---|---|---|
| `rules.list` | `{}` | `RulesResult` |
| `rules.remove` | `in.crewID` | `Empty` |

## search methods

| method | params | result |
|---|---|---|
| `search.all` | `{limit_per_group, query}` | `GroupsResult2` |

With hello feature `search.jobs`, `search.all` includes a `job` group alongside
`agent`, `crew`, `task` and `message`, including empty groups (`hits: []`).
Each job hit has `{kind: "job", id: "<factory>#<document_id>", title,
factory, document_id, repo?, number?, state?}`. `factory` is the connection id;
`document_id` is the queue job's document id; `number` is its issue or PR number
when known. The state is the factory's wire state. `agent` is absent. Older
clients must ignore unknown groups; existing hit kinds retain their JSON shape.

Search ignores case and matches words in titles, then queue request text
(`ask.text`, when carried), then repository names. Numbers match `12`, `#12`,
`repo#12` or `owner/repo#12`. Title and number matches precede request text,
which precedes repository-only matches; newest creation time wins within each
rank, with stable id breaking ties. `limit_per_group` bounds jobs across all
factories (default 8, allowed 1–50).

All connected factory queues are read concurrently under one 1.5-second total
job-search deadline, using the caller's existing authority and visibility.
Search never starts or resumes a local factory. A failed or timed-out factory
adds no hits and no error. Dismissed and dropped jobs are excluded; other closed
jobs are included only for the week they closed, and only with a known close
time. `search.all` retains its existing `bad_params` error for an empty query.


## services methods

| method | params | result |
|---|---|---|
| `services.get` | `in.IDParams` | `ProcessResult` |
| `services.list` | `{agent}` | `ProcessesResult` |
| `services.output` | `{from_line, id, max_lines}` | `OutputPage` |
| `services.policy` | `{id, policy}` | `ProcessResult` |
| `services.remove` | `in.IDParams` | `Empty` |
| `services.restart` | `in.IDParams` | `ProcessResult` |
| `services.start` | `in.IDParams` | `ProcessResult` |
| `services.stop` | `in.IDParams` | `ProcessResult` |
| `services.stop_all` | `{}` | `StopAllResult` |
| `services.tail` | `{id, lines}` | `OutputPage` |

## session methods

| method | params | result |
|---|---|---|
| `session.act` | `in.ActionAsk` | `ActionReceipt` |
| `session.action.get` | `in.ActionGetParams` | `ActionReceipt` |
| `session.actions` | `in.ActionsParams` | `ActionCapabilities` |

## settings methods

| method | params | result |
|---|---|---|
| `settings.crew.get` | `{}` | `CrewPolicy` |
| `settings.crew.set` | `in.CrewPolicyPatch` | `CrewPolicy` |
| `settings.get` | `{}` | `SettingsResult` |
| `settings.models.get` | `{}` | `TaskModelsResult` |
| `settings.models.list` | `in.TaskModelsListParams` | `TaskModelsPage` |
| `settings.models.set` | `in.TaskModelsPatchWire` | `TaskModelsResult` |
| `settings.set` | `in.SettingsPatchWire` | `SettingsResult` |

An engine whose hello carries `settings.memory` includes `memory` in Settings
(`settings.get`, `settings.set`, and `settings.changed`):
`{enabled: bool, source: "default"|"saved", cost_note: string}`. No saved choice
means off with source `default`. A saved word (`on`/`off`) or boolean is respected.
`settings.set` takes optional `memory_enabled: bool`; any non-boolean, including
null, is `bad_params`. Its reply and `settings.changed` carry the updated Settings.
The switch applies live at the next memory step. Off prevents automatic learning
calls; saved knowledge and explicit keeps remain available. Calls already in
flight finish and are billed. Terminal Build saves an explicit on.
`cost_note` is the shared `config.MemoryCostNote` disclosure, including the example
of one small DeepSeek V4.1 Flash job at 39% ($0.0124 of $0.0315).

An engine whose hello carries the feature `settings.models.auxiliary` adds a
fourth role to the task models: `roles.auxiliary` in `settings.models.get` (and
in `settings.changed`'s `task_models`) is `{pin, warning, eligible_count}` like
the other three, and `settings.models.set` takes `pins.auxiliary` as
`{id, provider?}` or `null`. It is the model the small jobs beside a
conversation run on: titles, route questions, captions and fold-ups. It is not
a task role and Auto never picks it; a `null` pin gives back the build's own
small-work model. A pin is checked as the other pins are: a model the provider
serves, and one the allowed models admit. An engine without the feature leaves
`roles.auxiliary` out and refuses `pins.auxiliary` as an unknown role.

An engine whose hello carries the feature `settings.models.insight` adds the page
AI's roles: `insight` and its overrides `insight_notes` (repository notes),
`insight_ask` (questions about a repository), `insight_themes` (backlog themes),
`insight_pipeline` (Build with AI on a pipeline), `insight_views` (a factory's
topic views), `insight_factory_read` (the read of what arrived in a factory) and
`insight_factory_scout` (a closer look at it). Each is a row under `roles` in
`settings.models.get` (and `settings.changed`),
`{pin, warning, eligible_count, default?, follows?, label}`, and
`settings.models.set` takes the same keys in `pins` as `{id, provider?}` or
`null`. `label` is the row's name in the person's words (`Page AI`, `Repo notes`,
`Repo ask`, `Repo themes`, `Build with AI`, `Topic views`, `Factory read`,
`Factory scout`), so a client can draw a role it does not know yet. They are
errand roles (`agents.errand` `role`), not task roles. The ladder is the role's
own pin, then the `insight` pin, then the engine's default for `insight`, then
the small-work tier. `default` appears on `insight` only while the engine's
default applies (`deepseek/deepseek-v4.1-flash` on `openrouter`, while OpenRouter
is connected and the allowed models admit it); it is never written as a pin, and
its absence means an unpinned `insight` follows its tier. `follows: "insight"`
marks each override. Pins are stored in the `models.roles` row beside the other
role pins and checked as the other pins are.

## setup methods

| method | params | result |
|---|---|---|
| `setup.get` | `{}` | `setupResult` |
| `setup.set` | `in.Setup` | `setupResult` |

## shutdown methods

| method | params | result |
|---|---|---|
| `shutdown` | `{}` | `{}` |

## skills methods

| method | params | result |
|---|---|---|
| `skills.list` | `in.SkillsParams` | `Row`[] |
| `skills.set` | `in.SkillSetParams` | `SkillState` |

## slash methods

| method | params | result |
|---|---|---|
| `slash.catalog` | `{}` | `SlashCatalogResult` |


## One engine on a home

`codeaf app` holds an OS lock on its home until it exits. A second `codeaf app` on that home waits before `hello`, starting no recovery, Factory work or clock, and starts as soon as the first ends. If the first is still there when the wait runs out, it exits with status 1 and its stderr says `another codeaf app is still closing this home`. A crash releases the lock. No method or feature changes.

## spend methods

| method | params | result |
|---|---|---|
| `spend.calls.export` | `in.SpendCallsExportParams` | `SpendCallsExportResult` |
| `spend.export` | `{from, to}` | `PathResult` |
| `spend.ledger` | `in.SpendLedgerParams` | `SpendLedgerResult` |
| `spend.ledger.export` | `in.SpendLedgerExportParams` | `SpendLedgerExportResult` |
| `spend.report` | `in.SpendReportParams` | `SpendReportResult` |
| `spend.today` | `{}` | `SpendResult` |

## sync methods

| method | params | result |
|---|---|---|
| `sync.acknowledge` | `in.IDParams` | `Empty` |
| `sync.get` | `{}` | `SyncResult` |

## tasks methods

| method | params | result |
|---|---|---|
| `tasks.brief` | `{id, text}` | `Empty` |
| `tasks.decide` | `{id, text, verdict}` | `Empty` |
| `tasks.decline` | `in.IDParams` | `Empty` |
| `tasks.dismiss` | `in.IDParams` | `Empty` |
| `tasks.entries` | `{before, id, limit}` | `EntriesMoreResult` |
| `tasks.get` | `in.IDParams` | `PageResult` |
| `tasks.hold` | `in.IDParams` | `Empty` |
| `tasks.list` | `in.AgentParams` | `TasksResult` |
| `tasks.model` | `{id, model, state, version}` | `Empty` |
| `tasks.note` | `{client_id, id, text}` | `TaskNoteResult` |
| `tasks.pause` | `in.IDParams` | `Empty` |
| `tasks.program_actions` | `in.ProgramActionsParams` | `ProgramActionsResult` |
| `tasks.program_report` | `in.ProgramReportParams` | `ProgramReportResult` |
| `tasks.restore` | `in.IDParams` | `Empty` |
| `tasks.resume` | `in.IDParams` | `Empty` |
| `tasks.retry` | `in.IDParams` | `Empty` |
| `tasks.routing.act` | `in.TaskRoutingActWire` | `TaskRoutingActResult` |
| `tasks.routing.get` | `{changes, id}` | `TaskRoutingResult` |
| `tasks.routing.options` | `in.IDParams` | `TaskRoutingOptionsResult` |
| `tasks.since` | `{after, id}` | `Since` |
| `tasks.start` | `in.IDParams` | `Empty` |
| `tasks.stop` | `in.IDParams` | `Empty` |
| `tasks.typing` | `in.IDParams` | `Empty` |

## tools methods

| method | params | result |
|---|---|---|
| `tools.follow` | `in.toolCallParams` | `Empty` |
| `tools.output` | `in.toolCallParams` | `LinesNextLineTotalResult` |
| `tools.unfollow` | `in.toolCallParams` | `Empty` |

## work methods

| method | params | result |
|---|---|---|
| `work.graph` | `in.GraphRequest` | `GraphResult` |

## Events

| event | data |
|---|---|
| `agent.changed` | `AgentChanged` |
| `ask.closed` | `AskClosed` |
| `ask.opened` | `AskOpened` |
| `chat.changed` | `ChatChanged` |
| `chat.removed` | `ChatRemoved` |
| `pair.changed` | `PairResult` |
| `collection.changed` | `CollectionChanged` |
| `compaction.ended` | `CompactionEnded` |
| `crew.changed` | `CrewChanged` |
| `crew.log` | `CrewLogEvent` |
| `crew.proposal.delta` | `CrewProposalDelta` |
| `crew.removed` | `CrewRemoved` |
| `decision.changed` | `DecisionEvent` |
| `decision.opened` | `DecisionEvent` |
| `deliverable.changed` | `DeliverableChanged` |
| `devices.changed` | `DevicesResult` |
| `entry.added` | `EntryAdded` |
| `factory.sessions_changed` | `Empty` |
| `hello` | `Hello` |
| `inbox.added` | `InboxAddedEvent` |
| `inbox.counts` | `InboxCounts` |
| `inbox.resolved` | `InboxResolvedEvent` |
| `log` | `Log` |
| `media.changed` | `MediaSourceChanged` |
| `member.changed` | `MemberChanged` |
| `models.changed` | `Empty` |
| `note.changed` | `NoteChanged` |
| `order.asking` | `OrderFired` |
| `order.changed` | `OrderChanged` |
| `order.fired` | `OrderFired` |
| `order.missed` | `OrderChanged` |
| `order.removed` | `OrderRemoved` |
| `process.changed` | `ProcessChanged` |
| `process.output` | `ProcessOutput` |
| `provider.changed` | `ProviderChanged` |
| `queue.changed` | `QueueChanged` |
| `quick.changed` | `QuickChangedEvent` |
| `render.changed` | `RenderChanged` |
| `settings.changed` | `SettingsChanged` |
| `skills.changed` | `SkillsChanged` |
| `spend.changed` | `SpendChanged` |
| `sync.changed` | `SyncChanged` |
| `task.changed` | `TaskChanged` |
| `tool.output` | `ToolOutputEvent` |
| `turn.delta` | `TurnDelta` |
| `turn.done` | `TurnDone` |
| `turn.long` | `TurnLong` |
| `turn.retrying` | `TurnRetrying` |
| `turn.started` | `TurnStarted` |
| `turn.thinking` | `TurnThinking` |
| `turn.thinking_done` | `TurnThinkingDone` |
| `turn.tool` | `TurnTool` |

## Slash catalogue commands and forms

`slash.catalog` lists the shared terminal vocabulary; the table is 54 commands, 93 rows, 31 aliases.
Each canonical command occurs once on the wire with all its forms and aliases.
These rows describe terminal commands; listing a command does not execute it.
Installed programs can add rows with `source: "program"`; shared rows have
`source: "table"`. The reply supplies limits, host availability and any skipped
programs. A client uses the available engine methods for its own actions.

| command | aliases | forms |
|---|---|---|
| `/always` | none | `/always`; `/always <text>` |
| `/atlas` | none | `/atlas <map>` |
| `/attach` | `/upload` | `/attach`; `/attach <path>` |
| `/automations` | none | `/automations`; `/automations add <title> …` |
| `/autonomy` | none | `/autonomy`; `/autonomy <kind> <ask\|recommend DURATION\|decide>` |
| `/budget` | `/limits` | `/budget`; `/budget <amount>`; `/budget <row> <amount>` |
| `/cache` | none | `/cache`; `/cache clean`; `/cache clean now` |
| `/compact` | none | `/compact` |
| `/connect` | `/connections` | `/connect` |
| `/copy` | none | `/copy` |
| `/cost` | `/usage`, `/tokens` | `/cost` |
| `/crew` | none | `/crew`; `/crew pin <seat> <model[@provider]>`; `/crew unpin <seat\|all>`; `/crew models <rule>`; `/crew cap <dollars\|off>`; `/crew cap task <dollars>` |
| `/debug` | none | `/debug` |
| `/delete` | none | `/delete` |
| `/devices` | none | `/devices` |
| `/dismiss` | none | `/dismiss`; `/dismiss undo` |
| `/drafts` | none | `/drafts` |
| `/effort` | `/think`, `/thinking` | `/effort`; `/effort <rung>` |
| `/export` | `/save` | `/export`; `/export <path>` |
| `/files` | none | `/files`; `/files <path>` |
| `/folder` | `/place`, `/dir` | `/folder`; `/folder <path>` |
| `/harness` | `/harnesses` | `/harness` |
| `/help` | `/?` | `/help` |
| `/helpers` | none | `/helpers`; `/helpers <what to ask the manager>` |
| `/history` | none | `/history` |
| `/home` | none | `/home` |
| `/land` | none | `/land`; `/land now`; `/land <folder>` |
| `/manual` | none | `/manual`; `/manual <question>` |
| `/memories` | none | `/memories`; `/memories <words>` |
| `/memory` | none | `/memory`; `/memory <words>` |
| `/model` | none | `/model`; `/model <slug>` |
| `/new` | `/clear`, `/clean`, `/reset` | `/new` |
| `/pair` | `/sync`, `/link`, `/laptop` | `/pair`; `/pair <code>`; `/pair <link>`; `/pair <code> --replace` |
| `/permissions` | `/perms` | `/permissions` |
| `/processes` | none | `/processes` |
| `/project` | none | `/project`; `/project <path>` |
| `/quit` | `/exit`, `/q` | `/quit` |
| `/redo` | none | `/redo`; `/redo stronger` |
| `/remember` | none | `/remember <text>` |
| `/resume` | `/sessions` | `/resume` |
| `/rewind` | `/undo`, `/back` | `/rewind` |
| `/select` | none | `/select` |
| `/settings` | `/set`, `/config` | `/settings` |
| `/setup` | none | `/setup`; `/setup now` |
| `/skill` | `/skills` | `/skill` |
| `/spend` | none | `/spend` |
| `/status` | `/info`, `/context` | `/status`; `/status --json` |
| `/stop` | none | `/stop` |
| `/subharness` | `/sub` | `/subharness`; `/subharness <name>` |
| `/task` | none | `/task`; `/task <brief>`; `/task solo <brief>`; `/task --best <brief>`; `/task --cheap <brief>` |
| `/teams` | none | `/teams` |
| `/update` | `/upgrade` | `/update`; `/update <channel or tag>` |
| `/wall` | none | `/wall` |
| `/workspace` | none | `/workspace`; `/workspace <path>` |

## Relay, pairing and cross-computer chats

`devices.pair` advertises all four pairing modes: `offer_code`, `use_code`,
`request_link` and `approve_link`. It is absent when the native engine is
unavailable. `pair.start` starts one operation; a concurrent start is `busy`.
Questions carry engine text and explicit options; copy their ids to
`pair.answer`. `pair.cancel` waits for cleanup. `pair.get` without an id returns
this process's current or last finished operation. Every snapshot has a lines
array, including when empty. Final states emit exactly once. Joining with local
chats first asks `replace_own_chats`; joining with live work is `busy`. A join
that changes this computer's shared keys finishes with `restart:true`: restart
the engine before sending work. Device and sync snapshots precede done.

`sync.relay` advertises `Sync.relay`: resolved URL, source (`environment`,
`paired`, `default`, `none`) and `off`. The source follows CODEAF_SYNC_URL, the
saved pairing URL, then the hosted default. Off omits the URL.

`Sync.unshared` counts this computer's chats that the other computers cannot
list yet, because no directory record names them: `{total, waiting, failing,
kept_here}`, where `total` is the sum of the rest. `waiting` chats are queued,
uploading, or older chats still to be copied; `failing` chats failed their last
upload and are retried; `kept_here` are older chats a rule keeps local: last
used over 30 days ago, which opening on this computer shares, or working in the
home folder, which stays on this computer. It is present only in mode `on`,
after the first count, and a change emits `sync.changed`. One process per home
publishes existing chats (the engine or a terminal, whichever holds the home's
sharing lock); it counts and writes its count beside the lock, and the other
process reports that count. The count describes this computer only; other
computers' backlogs are not on the wire.

`chats.take` advertises inventory, events and conditional continuation even
when sync is off or unavailable; in those modes `chats.list` returns `[]`.
Never-paired homes also return `[]` without contacting a relay. The first
inventory read starts one shared Sync.Follow subscription; later version
changes reread and diff rows. Presence changes update cached online facts
without a directory read. No inventory polling timer runs. Kept branches are
outside this v1 inventory. Crew is omitted because relay rows carry no crew
membership; the local Agent keeps its existing crews.

`chats.list` returns a fresh inventory without emitting `chat.changed` or
`chat.removed`. The follower compares with its last emitted rows, including
when a list has already read a newer version. Identical content at the same
version is never emitted again; changed presence can update that version.
An unset presence reading omits `online`, rather than implying `false`.
The `off` status describes an expired chat hold, independently of computer
presence. Only a known `online:false` permits an offline sentence.
`saved_at`, when known, is the sealed head's turn time. The directory stores
that fact separately from its `durable_at` publication clock; older records
without a known turn time omit it. The desktop formats it locally. The
`takeover_line` contains only holder or pending-work facts, with no date or age.
Directory clients send the optional turn time only after the relay advertises
`Codeaf-Saved-Turn-Times: 1`; older relays keep the compatible publication wire
and rows omit the unknown time. Existing records are not backfilled by startup.

`local_agent` is the app's saved/open folder id, resolved independently from
`cell`. Remote-only chats have no local agent. Cell locations include the
observed computer, fence and optional saved `head` in opaque `seen`, plus `can_take` and the engine's
`takeover_line` when known. `taking` is true while `agents.take` runs for that
chat, including while it waits behind another take, so a row that says the chat
is here can still say it; a followed inventory emits the row again when the take
starts and when it ends without moving the chat. Once a take has acquired the
chat, rows that still show it elsewhere keep `taking:true` until a row says the
chat is here or a later take holds it, even when opening the moved chat fails.
A client that stopped waiting for a take reads `taking:false`
on a chat still elsewhere as a take that did not move it.

`chats.move` advertises move progress, outcomes and `agents.take_cancel`.
Moves run one at a time. While `agents.take` runs, its row carries
`move:{state, step, done, total}`: `state` is `queued` while it waits behind
another move, then `moving`; `step` is `preparing`, `copying` (bytes are
arriving) or `opening` (the chat is acquired and is being put in place);
`done` is the bytes copied here so far and `total` the bytes the chat's record
says a copy takes, absent when unknown, when this computer already holds most
of the chat, or once more than that has arrived. A running move emits its row
at most four times a second. A move that waits 2 minutes on the relay with
nothing coming through stops; local work between requests does not count.
When a move ends without moving the chat, the row that clears `taking` carries
`move_ended:{outcome, line, detail}` (`stopped`, `failed` or `cancelled`), and
every later row keeps it until the chat is here or another move of it starts,
so a client that stopped waiting always learns how the move ended. `line` is
the person's sentence (also `agents.take`'s error message) and `detail` the
error text for "Copy details". `agents.take_cancel {cell}` cancels a running or
queued move of that chat and answers `cancelled:false` when there was none; the
cancelled `agents.take` answers `engine` with the cancelled sentence.
Older chats working in the home folder stay local.
Copy `cell` and `seen` verbatim to `agents.take`; never open an agent using the
remote cell id. It returns the adopted local Agent through the normal open path.
Stale observations answer `conflict`, `detail:{kind:"holder_changed"}` and the
engine's lost-race sentence; there is no automatic retry. Ownership changes
also update an idle open agent's location. Unknown online facts are absent.
A newer fence from the same holder is accepted when `seen.head` still matches;
a different holder or saved head conflicts. The relay advertises atomic head
checks with `Codeaf-Head-Takes: 1`; older relays retain exact-fence requests.
Watch reconnects retain fresh presence, last-seen times never regress, and an
unchanged row emits no chat event. Device events wait for the complete roster
and compare the full last-emitted value across every emitter.

`agents.open` keeps the conversation open until explicit close, including while
its tab is hidden. Its idle relay hold renews through heartbeats with the same
fence, without release/acquire or `chat.changed` for unchanged content. Calls that
read the open agent keep using that conversation. Conversations opened only for
background work retain the ten-minute idle release, checked every 30 seconds;
new work reopens through existing admission. Running turns, unanswered questions,
established open crew holds and kept services retain their conversations.
`warm` marks a window's guess at a view (a hover's prefetch): a warm open of an
agent in Closed or the archive is refused with `conflict` and the mark stays.
Only an open without `warm` (the person's own, Reopen) takes it out.

## Unread

Every Agent carries `unread: bool`: it said something new to the person after
the last time they saw it. New output is an answer a turn ended on (a harness
run's report included), or a question put to the person; tool steps and
thinking alone never set it, nor does a partial reply a stop cut short or a
checkpoint's hand-over note. The answer's time is the conversation's own
`lastReplyAt` on its meta.json, stamped when the turn seals; a question's is
its own record's: a crew proposal's time, a crew decision's raising, when an
order run began to wait (its slot, for a run an older build parked), or its
presence file's `asked`, so a question asked again after a restart keeps its
time. A question counts from before the window is told of it, so a mark sent
once the window has it covers it. A question counts only while it is open: once
answered, wherever it was answered (its chat, the Inbox, another window, the
terminal), or gone with an agent that was closed or let go, needs-you clears without
turning into unread, and output after it still counts. A copied conversation
starts read. Output another process says (a turn sealed, a question put or
answered there) is said live for an agent this engine last saw working or
waiting in that process, with its state as `agents.list` reads it; one it last
saw idle is said at the next `agents.list`. A restart reads the agent the same
way.

`agents.mark_seen {id}` records "seen up to now" for that agent in the app's
state file (`seen`), so it survives restarts, and clears `unread`. The mark is
never ahead of the engine's clock: output the person saw that was stamped
ahead of it, by a device whose clock runs ahead, reads seen by its own instant
(`seen_ahead_at`), so the dot clears. Each answer and question is judged by
its own instant, so a question put after the mark still counts whatever the
stamps beside it. While the engine runs, a conversation's answer stamped ahead
stays its last answer, so a later answer stamped below it reads seen until a
restart reads the conversation's new stamp. It only
writes the mark: it never opens or warms the agent, and a Closed or archived
agent stays where it is. An unknown id answers `not_found`. The engine cannot
know which agent is on screen, so a window calls it when it shows an agent and
again when output lands while it is shown. `agent.changed` is sent when
`unread` turns true and when it turns false, `agents.mark_seen` included,
against what the window was last told; a mark never says the agent's state
otherwise. Each `agent.changed` works its `unread` out as it is sent, one at a
time, so the last one the window gets says what `agents.list` would. The
Agent that `agents.rename`, `agents.set_model` and `agents.set_effort` answer
carries `unread` too, and `agents.list` works it out only for the agents it
returns. An agent with no
mark is read up to the moment the engine first kept unread (`unread_from` in
the state file), so on upgrade every agent starts read, and a new agent starts
read because it has said nothing. Engines that serve it announce
`agents.mark_seen` in `hello.features`.

## Errands (`agents.errand`)

An errand is one short, detached, metered model call with the app's own prompt
on a named agent's door. It is never a turn: no tools, no journal entry, no
message in the conversation, no event besides the usual spend receipts. It uses
the agent's admission, provider and keys, one-model mode, job budget and
lifetime, and its usage is booked on the agent's auxiliary ledger under
`purpose`. The engine never branches on `purpose`; it is a ledger label and the
key of the one-in-flight slot. `agents.suggest_reply` runs on the same core with
codeaf's own fixed prompt (purpose `reply_suggestion`, the conversation's model as
the naming role's floor; every answer the model gave is cached, a failed request is
asked again).

The hello carries features `agents.errand` and `agents.errand.models` and an
`errand` object with the limits: `{v, roles[], model, efforts[], json,
json_schema, images, max_input_bytes, max_messages, max_schema_bytes,
max_cache_key, max_tokens, default_tokens, default_deadline_ms,
max_deadline_ms}`. Read the limits from there, never from this page. `images` is
false in v1; image parts will be added behind it.

Request (`v` is 1; absent means 1; a higher `v` is `bad_params`; unknown fields
are ignored):

- `agent` (required): whose door, ledger and job budget pay. The agent must be
  held by this engine (`agents.open` first); otherwise `reason: "not_open"`.
- `purpose` (required): `^[a-z][a-z0-9_]{1,31}$`.
- Route: `role` (any name in `caps.roles`; default `title`, the naming role,
  first rung only, the conversation's model as floor) or `model` (an explicit
  id). A model must be one `agents.errand.models` lists; under one-model mode
  only the agent's own model is allowed. A role or model outside that is
  `model_not_allowed` with a sentence naming why. `effort` is `low`, `medium`,
  `high`, `xhigh` or `max` and is sent only where the route accepts it.
- `system` text and `messages[]` of `{role: "user" | "assistant", text}`, the
  last one the user's, 1 to `max_messages`, together at most `max_input_bytes`
  of UTF-8.
- Output: text by default; `json: true` asks for one JSON object and
  `json_schema` (an object, at most `max_schema_bytes`) for one of that schema,
  sent unstrict, and dropped by the provider for a route that refuses it. The
  engine checks only that the answer is one JSON object (a single code fence is
  unwrapped); anything else is `reason: "invalid_json"`.
- `max_tokens` (default `default_tokens`) and `deadline_ms` (default 10000) are
  clamped into range, not refused. `cache` (at most `max_cache_key` bytes) reuses
  the answer to an identical ask with the same key, `cached: true`; 32 answers
  per agent are kept, unusable answers included; a failure or refusal is not
  kept and is asked again.

Response: `{text, model, provider, role, usage: {in, out, cost_usd?}, ms,
cached?}`. `model` is the model that answered, as the provider named it. When
`text` is `""`, `reason` says why: `closed`, `stopped`, `over_budget`, `busy`,
`deadline`, `cancelled`, `model_failed`, `invalid_json`, `no_answer` (the call was made and billed and said nothing; its usage is kept and it is not cached), `not_open` or
`unavailable` (an owner without the errand door). One errand per (agent,
purpose) runs at a time; a second answers `busy` at once and never queues.
Closing or stopping the agent, or its job running out of money, cancels one in
flight.

`agents.errand.models {agent?}` answers `{caps, one_model?, roles: [{role,
model, provider?, effort?, json?}], models: [{id, provider, provider_name?,
json?, json_schema?, price_in_per_m?, price_out_per_m?, context_length?}]}`
from this engine's live configuration (the same rows and catalog facts as `models.list`): `roles` is where
each role's errand goes now on that agent (or on the profile, with no agent),
`models` is every model an errand may name, and `json`/`json_schema` are absent
when no catalog knows. Under one-model mode `one_model` names the only model.

Remote owners answer errands over their own connection when they advertise it;
older ones answer `unavailable`.

### Mark a job pull request ready

Hello feature `factory.job_pr_ready` advertises the optional method of the same
name. Params are `{factory, job, expect}`; `expect.version` is required as for
Merge. It sends one `POST /v1/jobs/{id}/pr-ready` with `{expect}` and returns
`FactoryJobActed` with `action: "pr_ready"`, the record event and the current job.
Job reads offer `can.pr_ready` to a signed-in person allowed to merge this job
while its published PR is open and draft. The job's own GitHub repository and
the caller's credential are used, with no model call or automatic retry.

A confirmed mutation stores `pr.draft: false` immediately; no follow read is
needed. Merge still applies every other gate. Another person is `forbidden`,
a changed expected version is `stale`, and no PR or an already ready PR is
`conflict`. An unconfirmed outcome is `uncertain`: read the job before deciding
whether to try again. Ready has no Undo window and never merges the PR.

### Job note receipts and pull request comments

A manager's `factory_item` note remains saved on the job after all steps finish.
The tool result is computed from the saved job: it names the next remaining
conversation step that reads notes, or says no remaining step will read the note and it is not in the pull
request. The approval question is `save a note on the job?`. A running round
already has its brief; a subsequent round can read the note if it runs again.
Deterministic checks, publication steps and approval gates are not note readers.

No app method, parameters, reply fields, hello feature or error codes change.
`factory.issue_comment` still targets the source issue, not the job's created
pull request. The note receipt directs the person to comment on the PR in
GitHub, using its saved URL when present; it does not claim that a manager has
a PR comment tool. No comment or PR body edit is performed by saving a note.
## Stop spending after this step

Hello feature `factory.job_stop_after_step` enables
`factory.job_stop_after_step {factory, job, on: bool, expect?}`. It returns
`FactoryJobActed` with action `stop_after_step`. The write is
`POST /v1/jobs/{id}/stop-after-step {on, expect?}`. The job read carries
`stop_after_step: true` while armed, and `can.stop_after_step` grants the owner
this toggle on a running or queued job. Missing or nonboolean `on` is
`bad_params`; another owner is `forbidden`, a stale guard is `stale`, and an
inactive job is refused. An unconfirmed write is `uncertain` and is never retried.

The current step finishes. Before the next step, or before landing when there
is no next step, the job waits in `needs_you`, question kind `spend_stopped`:
"Stopped spending after <Step> as you asked · $<spent> so far. Carry on?"
Hello advertises `factory.job_spend_answer` exactly when it advertises
`factory.job_stop_after_step`. The narrow door is
`factory.job_spend_answer {factory, job, carry_on: bool, expect}` and returns
`FactoryJobActed` with action `spend_answer`. It sends one
`POST /v1/jobs/{id}/spend_answer {carry_on, expect}`. Missing or nonboolean
`carry_on`, or missing `expect.version`, is `bad_params`. Under the revision
lock the factory requires the current question to be `spend_stopped` and the
caller to own the job: another question is `conflict`, a changed revision is
`stale`, and another owner is `forbidden`. Refusals record nothing.
`can.spend_answer` is true only for the owner while that question waits.
`carry_on: true` behaves like yes on `/answer`, clearing the flag and continuing
at the next step; false behaves like no, stopping as the person's stop. Both
keep the same `answered` event and saved answer as `/answer`. An unconfirmed
write is `uncertain` and is never retried. This door cannot answer another
question, approve a plan or decline a start. Off before the boundary
withdraws the hold. A step's own question or failure comes first and leaves
the flag armed. A queued job without a started step runs its first step before
holding. Landing and any automatic publication remain behind the hold.

Each accepted toggle records `stop_after_step_changed`, the person's identity,
and `stop_after_step=true` or `stop_after_step=false`. Workers read the saved
choice at every step boundary through their own live lease:
`GET /v1/workers/leases/{lease}/stop-after-step` returns `{on: bool}`.
An unreadable choice falls back to the last value the runner saved; the next boundary reads it again.

### Run an ended job as a new job

Hello feature `factory.job_run_as_new` adds
`factory.job_run_as_new {factory, job, assignee?, expect?}`. When supplied,
`expect.version` must be positive and guards the source job. The engine
sends one `POST /v1/jobs/{id}/run-as-new {assignee?, expect?}`. The factory
applies the same ownership and repository guards as `run_again`, and accepts
failed, stopped, closed and done jobs. Running work is refused.

The reply is `FactoryJobActed` with `action: "run_as_new"` and `job` naming the
NEW job; `current` reads that job. The HTTP answer is `{factory, id}` with the
new document ID. Job reads add `can.run_as_new`, `from_job` (the original job's
document ID) and `source_issue` (an optional reference URL). Both jobs' records
carry `run_as_new` receipts with the actor and both document IDs.

The factory copies repo, title, Body and Ask.Text verbatim, autonomy, Cap,
Stages, the decided pipeline and model choices. It creates an ordinary issue
pipeline job with `number: 0` and a fresh run. The forge merge index uses repo,
kind and issue number, so retaining the source's number would claim its
identity. `source_issue` carries only the reference, never that identity.
Omitting assignee uses the caller as starter; an explicit assignee names the
new job's person. A person's ordinary Start queues it under the usual capacity
limits. The original document is never changed.

Errors follow other job writes: `bad_params` for malformed requests,
`forbidden` for ownership, `stale` for a changed source, `conflict` for ongoing
work or start refusals (`repo_off` or `needs_checkout` in refusal.reason),
`not_found` for a missing job, and `uncertain` when the outcome is unconfirmed.
A start refusal removes a provisional copy; its allocated document ID is not
reused. Storage or receipt failures after a successful start remain uncertain;
the engine never repeats the write automatically.

`factory.job`'s `desk.run.log[]` carries optional `kind: "tool"` for tool
captions; `text` remains readable, with one tool name and home paths replaced
by `~` before a 240-character limit. Other lines omit `kind`. Worker reports
retain their existing body shape, carrying tool identity in `tone: "tool"`;
no hello feature or new error code is needed.

## Factory job first message and empty agent compatibility

The hello feature `factory.job_send` announces first-message creation. With this
feature, `factory.job_chats` returns `agent: ""` when no manager exists, preserving
existing step chats. A client must render a composer even without an agent and
send `factory.job_send` with `factory`, `job`, `text` and optional `client_id`.
The result contains `agent` and `turn`; later messages can use `agents.send`.
Reads do not count as a person's touch. Storage and lifecycle maintenance remain.

Older desktops reject an empty agent in `jobChatsOf`, so the Run view has no
manager chat or composer (and discards the returned step chats). They require a
desktop update before pairing with this engine for jobs without a manager.
The hello feature is capability advertisement, not client negotiation; it does
not by itself make those older desktops compatible.
### factory.job_publish_anyway

Hello feature `factory.publish_anyway` advertises this person action. Params:
`factory`, `job`, and `expect.version`. The result is `FactoryJobActed` with
`action: "publish_anyway"` and the current job. The desk shows **Publish anyway**
only when `can.publish_anyway` is true.

The factory requires an authenticated person allowed to publish the job. Only
correctly skipped conditional checks under the every-check policy qualify.
Failed, pending or unmeasured checks refuse. Stale versions return `stale`
without recording consent. Auto mode and automatic answers cannot grant it.

The job's `publish_anyway` uses the existing approval shape. Its `matched`
identity hashes the reported head, change, files and skipped-check set. The
existing approved ledger event records the person and time. A held worker
receives this decision on the existing answer signal and validates the identity
before resuming. Its `words` carries the server-minted approval trailer; a post
checks its head and person before using it without a local copy of the ledger.
Skipped checks stay unmeasured and the PR lists each reason and the person decision.

## plugins methods

The optional `plugins` hello feature enables these methods. They use
`internal/connect`, the same account and capability stores as terminal
`/connect`. Provider connection methods and the `Provider` type stay unchanged.

| method | params | result |
|---|---|---|
| `plugins.list` | `{}` | `PluginsResult` |
| `plugins.describe` | `in.PluginID` | `PluginDescription` |
| `plugins.connect_browser` | `in.PluginBrowserParams` | `PluginBrowserResult` |
| `plugins.cancel` | `in.PluginID` | `Empty` |
| `plugins.connect_key` | `in.PluginKeyParams` | `PluginKeyResult` |
| `plugins.set_capability` | `in.PluginCapabilityParams` | `Empty` |
| `plugins.disconnect` | `in.PluginDisconnectParams` | `Empty` |

`plugins.list` returns `{plugins, providers, featured, featured_words}`.
Each plugin has `id, name, kind, category, blurb, connected, key_checkable`, and optionally
`account, key_env, expired, connected_at, key_saved_at, ask: {label, answers?},
key_hint: {words, url?}`.
Kind is `browser`, `key` or `server`. Category uses the catalog's spelling.
`key_checkable` is true only for key connectors with a catalog probe.
Blurb shortens the existing catalog words. Counts come from the returned
catalog. `featured` contains ordered ids; `featured_words[id]` contains
`{what, does}`.

Plugin dates are UTC RFC 3339 instants saved in the existing credential store.
`connected_at` records the first saved connection; replacing a key preserves it
and updates `key_saved_at`. Browser token refresh does not change either date.
Older entries retain absent dates; disconnect removes them.
`ask.label` is the catalog's short field label (for example, "Workspace"),
not the terminal's instruction to paste the fact and key together. The protocol
accepts the fact in `answer` and the credential in `key` or `key_env` separately.

Only `plugins.list` extends each existing provider view with optional
`account, key_env, connected_at, running, plan_paused: {resets_at?}`.
`providers.list` is unchanged. Account comes from ChatGPT's stored email;
key_env names the environment variable actually supplying the credential.
Provider connection dates have no stored source and remain absent.

`running` is only for loopback Ollama addresses. A background GET to `/api/tags`
has a 200 ms deadline, accepts an Ollama model-list response and follows no
redirects. The list returns immediately; `running` is absent until the first
probe finishes, then true or false, cached for five seconds. Remote Ollama
addresses leave it absent. Fetch the list again to read a completed probe.

`plan_paused` comes from subscription refusal phase news, identified by source.
`resets_at` preserves the vendor's reset value, including readable clock text;
it is absent when the vendor gives none. The refusal is remembered for this
engine connection, expires when its reset instant can be parsed, and clears
when the plan answers again. Metered overflow does not clear a paused plan.
A generic quota/rate limit is not evidence of a paused subscription. No account
quota polling or durable pause history is added.

`plugins.describe {id}` returns
`{capabilities, scopes_words, tools?, notes?, accounts?, key_note?}`.
Capabilities contain `{id, phrase, covers, acts, state, tools}`. State is
`yes`, `ask` or `off`; the same `SetCapabilityState` store serves chat's
Always answer and `plugins.set_capability {id, capability, state}`.
Off removes that capability's tools when the next session builds its tools.
Connected servers' `tools` come from the existing MCP listing, as
`{name, title, read_only}`. Tool names grouped under a capability also form
its `covers` text.

`plugins.connect_browser {id, answer?}` answers
`{state: "waiting", link}`. The desk opens `link` in the browser.
Completion emits `plugins.changed`; `error` carries a safe sign-in failure.
`plugins.cancel {id}` abandons an unfinished attempt. Closing the engine
also cancels pending attempts.

`plugins.connect_key {id, key?, key_env?, answer?}` accepts one key or one
environment variable name. `answer` supplies the workspace when needed.
It returns `{checked: bool, account?}` on success, or
`{checked: false, error: "shape"|"refused"|"no_answer", words}` as a successful wire answer.
`checked` is true only when a probe ran and passed; a key saved without a
catalog probe returns false and is first tested on real use.
A shape failure reaches no vendor, a refusal retains at most 120 characters,
and a missing answer saves nothing. Key values never appear in replies.
`plugins.disconnect {id, account?}` uses the existing disconnect path.
The optional account must match the stored account.

`settings.set` accepts `when_plan_paused: {[provider]: policy}`, where policy
is `wait` or `use pay-as-you-go`. It changes the existing persisted source
rows. `settings.get` and `settings.changed` return the map in
`settings.when_plan_paused`. An unknown provider is refused.

| event | data |
|---|---|
| `plugins.changed` | `PluginsChanged`: `{id, error?}` |
| `plugins.needed` | `PluginsNeeded`: `{agent, plugin, why, ask_id}` |
| `plugins.needed_done` | `PluginsNeededDone`: `{ask_id}` |

A connect question reaches the app on the existing `WatchQuestions` lane.
`plugins.needed` identifies that same question, also listed by `asks.list`.
After connecting through the plugin methods, the desk calls
`asks.answer {agent, id: ask_id, option: "1"}` to continue the turn without
a second sign-in. `option: "2"` means Not now and saves no refusal.
Answering in either chat or Inbox resolves the same question.
An answer or withdrawal emits `plugins.needed_done`.

The current account store holds one account per plugin, including Slack.
It has no plugin call history; a Lately section remains absent.
