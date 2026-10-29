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
| `agents.idle` | `{days}` | `IDsResult` |
| `agents.labels` | `{ids}` | `AgentLabels` |
| `agents.list` | `in.AgentsListParams` | `AgentsResult` |
| `agents.manual` | `{client_id, id, question}` | `TurnResult` |
| `agents.move_to_closed` | `in.tidyIDs` | `Empty` |
| `agents.move_to_task` | `{id, turn}` | `MoveResult` |
| `agents.take` | `in.TakeParams` | `TakeResult` |
| `agents.open` | `in.IDParams` | `agentResult` |
| `agents.queue` | `in.sayTo` | `queueResult` |
| `agents.rename` | `{id, title}` | `agentResult` |
| `agents.reopen` | `in.crewID` | `agentResult` |
| `agents.retry` | `in.IDParams` | `TurnResult` |
| `agents.rewind` | `{id, point}` | `RewindToken` |
| `agents.rewind.points` | `in.IDParams` | `ConversationPoints` |
| `agents.rewind.undo` | `{id, token}` | `Empty` |
| `agents.run_harness` | `in.RunHarnessParams` | `TurnResult` |
| `agents.send` | `{attachments, client_id, id, reads, said, submission, text}` | `TurnResult` |
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
| `factory.job_change_assignee` | `in.FactoryJobAssigneeParams` | `FactoryJobActed` |
| `factory.job_chats` | `in.FactoryJobChatsParams` | `FactoryJobChats` |
| `factory.job_close` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_control` | `in.FactoryJobControlParams` | `FactoryJobActed` |
| `factory.job_create` | `in.FactoryJobCreateParams` | `FactoryJobActed` |
| `factory.job_dismiss` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_drop` | `in.FactoryJobDropParams` | `FactoryJobActed` |
| `factory.job_hold` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_merge` | `in.FactoryJobMergeParams` | `FactoryJobActed` |
| `factory.job_reject_plan` | `in.FactoryJobRejectPlanParams` | `FactoryJobActed` |
| `factory.job_reopen` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_request_changes` | `in.FactoryJobChangesParams` | `FactoryJobActed` |
| `factory.job_restore` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_resume` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_retry_publish` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_run_again` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_set_budget` | `in.FactoryJobBudgetParams` | `FactoryJobActed` |
| `factory.job_set_model` | `in.FactoryJobSetModelParams` | `FactoryJobActed` |
| `factory.job_set_priority` | `in.FactoryJobPriorityParams` | `FactoryJobActed` |
| `factory.job_start` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_step_edit` | `in.FactoryJobStepEditParams` | `FactoryJobStepEdited` |
| `factory.job_step_edits_read` | `in.FactoryJobStepEditsParams` | `FactoryJobStepEdits` |
| `factory.job_step_rerun` | `in.FactoryJobStepParams` | `FactoryJobActed` |
| `factory.job_step_stop` | `in.FactoryJobStepParams` | `FactoryJobActed` |
| `factory.job_stop` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_unassign` | `in.FactoryJobActionParams` | `FactoryJobActed` |
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
| `factory.pipeline_preview` | `in.FactoryPipelinePreviewParams` | `FactoryPipelinePreview` |
| `factory.pipeline_source` | `in.FactoryPipelineSourceParams` | `FactoryPipelineSource` |
| `factory.queue` | `in.FactoryRef` | `Queue` |
| `factory.refresh` | `in.FactoryRefreshParams` | `Empty` |
| `factory.rename` | `in.FactoryRenameParams` | `Empty` |
| `factory.repo_set` | `in.FactoryRepoSetParams` | `FactoryRepoSetDone` |
| `factory.repos` | `in.FactoryRef` | `FactoryRepos` |
| `factory.revoke_sessions` | `in.FactoryAdminParams` | `FactoryAdminDone` |
| `factory.rules` | `in.FactoryRef` | `FactoryRules` |
| `factory.rules_set` | `in.FactoryRulesSetParams` | `FactoryRules` |
| `factory.runner_slots` | `in.FactoryRunnerSlotsParams` | `FactoryRunnerSlotsDone` |
| `factory.runners` | `in.FactoryRef` | `Runners` |
| `factory.setup` | `in.FactoryRef` | `FactorySetup` |
| `factory.setup_back` | `in.FactoryLocalParams` | `FactorySetupView` |
| `factory.setup_clone` | `in.FactorySetupFolderParams` | `FactorySetupView` |
| `factory.setup_connect` | `in.FactorySetupConnectParams` | `FactorySetupView` |
| `factory.setup_discover` | `in.FactoryLocalParams` | `FactorySetupView` |
| `factory.setup_done` | `in.FactoryLocalParams` | `FactorySetupView` |
| `factory.setup_read` | `in.FactoryLocalParams` | `FactorySetupView` |
| `factory.setup_retry_read` | `in.FactorySetupRepoParams` | `FactorySetupView` |
| `factory.setup_select` | `in.FactorySetupSelectParams` | `FactorySetupView` |
| `factory.setup_skip` | `in.FactorySetupRepoParams` | `FactorySetupView` |
| `factory.setup_use_folder` | `in.FactorySetupFolderParams` | `FactorySetupView` |
| `factory.sign_in_cancel` | `in.FactorySignInFlowParams` | `Empty` |
| `factory.sign_in_poll` | `in.FactorySignInFlowParams` | `FactorySignInState` |
| `factory.sign_in_start` | `in.FactorySignInStartParams` | `FactorySignIn` |
| `factory.sign_out` | `in.FactoryAdminParams` | `FactoryAdminDone` |

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
`takeover_line` when known. Older chats working in the home folder stay local.
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
