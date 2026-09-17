# The older task engine — inventory for its removal

A map for the wave that deletes `internal/session`'s node task runtime. **No Go
file is deleted or edited by the wave that writes this document**; this records,
for every file, type and function that exists only to run or judge a node task,
where it lives, what replaces it, and who outside `internal/session` holds it.

## What "the older engine" is, and what replaces it

The older engine is a `TaskGraph` of `TaskNode`s held in one session's memory: a
frontier that admits nodes, a child agent per node in a `git worktree` of its
own, an auditor that decides a node is done, a division road a worker uses to
hand out parts, and the `propose_task` / `quick_task` / `divide_work` /
`revise_assignment` verbs and the `tasks` window that reach it. Its nodes are
what `CODEAF_TASK_BELT` chooses a belt for; it is the "legacy" road
`CODEAF_TASK_ENGINE=legacy` keeps reachable.

The replacement is the run: `internal/run`'s supervisor launches every ready
task of one `internal/plandb` store, one bash-belt session agent per task
(`internal/session.NewBeltWorker`), all of them in the run's one working copy,
coordinated through the store the worker reaches by running `plandb` through
bash. The run's session-side road is `internal/session/task_run_belt.go` (the
`RunEngine` door), `NewBeltWorker` and `BeltWorkerBrief`
(`bashbelt_worker.go`), `LandRunTree` (`land_run_tree.go`), the belt
(`bashbelt.go`), and the store bridge (`plandb_plan.go`). The chat, the pane,
the room and `codeaf do` become readers of the store rather than engines.

**Groups.** Every item is filed under exactly one of:

- **delete whole file** — nothing outside `internal/session` names anything in
  it, and nothing the run road needs is in it.
- **delete these functions** — the file survives for its shared vocabulary or
  its run-road half; the functions below go with the engine.
- **keep — shared** — the run road, or a surface outside the package, reads it;
  it stays (some of it re-pointed at the store rather than the graph).

Symbol lists are read from the files' own top-level declarations
(`^(func|type|const|var)`); "outside references" are `git grep` hits outside
`internal/session` (a lowercase symbol can only be reached from another package
through an exported door, so the exported doors are what the column names).

---

## Group 1 — delete whole file

These files are the node engine and nothing else. Each is replaced by the run
road named beside it; none is named from outside `internal/session` except where
the "outside references" column says so (those references are the surfaces that
go with the engine, called out in Group 2).

`internal/session/task_run.go` (the executor) and `internal/session/task_audit.go`
(the auditor) are the two largest files and are **partial**: their engine half is
in Group 2 below and their run-road helpers are in Group 3. They are not whole-file
deletes.

| file | lines | what it is | what replaces it | outside references |
| --- | --- | --- | --- | --- |
| `internal/session/orchestrate.go` | 2196 | the adaptive run: a planner call, an executor, the family lane | the run engine owns the loop; there is no planner call and no planning node | `RunOrchestrate`, `OrchestrateSnapshot`, `Orchestrations`, `WatchOrchestrations`, `SteerOrchestrate`, `ResolveOrchestrate`, `OrchestrateNodeJournal` (tui3 `roomorch`, `cmd/codeaf`, remote) — the adaptive-run surfaces go with it |
| `internal/session/task_divide.go` | 1926 | the `divide_work` verb, its two gates, the reviewer | `plandb split` run through bash; the store's own dependency edges | none |
| `internal/session/task_divide_compose.go` | 274 | what a division part is told | `BeltWorkerBrief`'s plan section and the part's store task | none |
| `internal/session/task_divide_sketch.go` | 764 | the division proposal drawn from a checkpoint mark | gone; a run's workers split through `plandb split` | none |
| `internal/session/task_divide_scope.go` | 705 | scope ownership between parts | gone; a run's shared working copy plus the store's edges | none |
| `internal/session/task_divide_wip.go` | 360 | the family's frozen world before parts run | gone; one working copy per run | none |
| `internal/session/task_quick.go` | 1106 | the `quick_task` verb, its items, its claims | a leaf task in the store; the item list is the model's own | none |
| `internal/session/task_child_run.go` | 1177 | one node's run, the phases as methods | the worker's turn loop in `internal/run/bashworker.go` | none |
| `internal/session/task_beat.go` | 291 | the graph's liveness sidecar (`tasks.json` pulse) | the trajectory file and the store's own rows | none |
| `internal/session/task_store.go` | 2196 | the graph checkpoint and recovery | the store's persistence (`internal/plandb`) and the trajectory file | none |
| `internal/session/task_pressure.go` | 550 | the admission governor over the machine | the supervisor's slot bound and the run's limits | none |
| `internal/session/task_calltrail.go` | 401 | the call trail on a node's behalf | the trajectory's step lines | none |
| `internal/session/task_land_unsaved.go` | 262 | a landing that could not save is not a landing | `LandRunTree`'s own refusal | none |
| `internal/session/task_landing_question.go` | 302 | a landed `your call` put to the person | the store's note on the root plus the thread | `Agent.PendingDecisions`, `EventQuestion` — the lanes are shared; the land-time rule goes |
| `internal/session/task_merge_round.go` | 685 | one merge attempt before a person's call | landing from the working copy; a conflict is the person's | none |
| `internal/session/task_branch_protection.go` | 233 | protected-branch refusal at landing | `LandRunTree`'s refusal | none |
| `internal/session/taskgit.go` | 387 | what work on its own may do to a repository | the run's command guard | none |
| `internal/session/task_ledger.go` | 233 | the ledger as the contract of what ships | the working copy's own `git status` | none |
| `internal/session/task_lay.go` | 287 | laying a ledger over a folder | gone; the working copy is the run's record | none |
| `internal/session/task_mirror_manners.go` | 400 | a landing does not overwrite a changed file | gone | none |
| `internal/session/task_tree_mirror.go` | 175 | a folder family's tree is a repository | gone | none |
| `internal/session/task_baseline.go` | 436 | the checks as they stood before one task | gone | none |
| `internal/session/taskpreflight.go` | 408 | who else is in the files, before the money | gone | none |
| `internal/session/task_claims.go` | 995 | a landing's claims are its checklist | gone | none |
| `internal/session/taskclaims.go` | 160 | who else is in these files | gone | none |
| `internal/session/taskmanifest.go` | 95 | the check reads the ground, not a diff | gone | none |
| `internal/session/taskgrade.go` | 539 | the grading loop into the ratings store | the run's spend rows (`role`, `model`) feed the same store | none |
| `internal/session/taskground.go` | 271 | the ground moving under a run, caught at land time | gone | none |
| `internal/session/taskname.go` | 544 | the two or three words a piece of work is called | a store task's title | none |
| `internal/session/taskmodel.go` | 398 | which model a task runs on | the crew's role→seat resolution (`internal/run/crew.go`) | none |
| `internal/session/task_person.go` | 233 | the sizing judge, and the escalation note beside it | gone | none |
| `internal/session/task_forward.go` | 377 | the person steering from the main chat | a store write (a person-note on the task's thread) | none (door re-pointed) |
| `internal/session/task_continue.go` | 472 | first-class continuation of a settled task | reopening the store task; a `done --result` carries the sha | none |
| `internal/session/task_beside.go` | 85 | beside-work admitted beside a task | gone | none |
| `internal/session/task_depends_kept.go` | 87 | a kept dependency edge | the store's own `--after` edges | none |
| `internal/session/task_checks.go` | 1147 | what the checker may run | gone; a `check`-role task reads against acceptance | none |
| `internal/session/taskdelta.go` | 726 | what changed outside this conversation, for the model | the store's `context` reads | none |
| `internal/session/assignment.go` | 1214 | the direction/assignment machinery a `revise_assignment` writes | a store write (`task amend`, a person-note) | none |
| `internal/session/assignment_tool.go` | 173 | the `revise_assignment` tool | gone as a tool (a person's revision is a store write) | none |

---

## Group 2 — delete these functions

The file survives, but this slice of it exists only for the graph.

### `internal/session/task_run.go` (the executor half)

Delete: the graph and the node life. Keep the run-road helpers listed in Group 3.

| symbol | line | what it is | replaces it |
| --- | --- | --- | --- |
| `TaskNode` | 281 | one node: spec, life, leavings | the store's `plandb.Task` |
| `TaskGraph` | 996 | nodes plus edges plus the frontier | the supervisor's ready-set |
| `keepRunRows`, `runRowsLocked`, `newTaskGraph`, `(a *Agent).graph`, `(a *Agent).tasker` | 1174–1260 | the graph a session owns | gone |
| `runOwned`, `runner`, `reserve`, `admit`, `claimChild`, `releaseChild`, `children` | 1273–1462 | admission and the fan cap | `store.Add` / `Claim` / `ReadySet` |
| `runFrontier` | 1496 | the whole scheduler | `Supervisor.pass` |
| `holdOnStartingLocked`, `lanesTaken`, `takeLaneLocked`, `giveLaneLocked`, `armPoll` | 1649–1722 | slot and machine admission | the supervisor's slots |
| `stopAll`, `doomedDependencies`, `readinessLocked` | 1769–1834 | cascade and readiness | the store's cancel cascade |
| `briefLocked`, `inheritedLocked`, `shareReports`, `inheritedLearnedLead`, `inheritedReportFloor`, `inheritedLearnedHeading` | 1871–2047 | a dependent's brief carrying reports | the store's `context` / `--after` results |
| `complete`, `handBackSlotLocked`, `resettle`, `announce`, `reportHome` | 2047–2166 | node lifecycle transitions | store endings |
| `TaskNode` methods `stateNow` … `leavings` | 2174–2945 | the node's state, model, run, finish | gone |
| `workingCopy`, `ladderRecord`, `setTree`, `resumeTree`, `openTaskWorld` | 2945–5265 | the node's worktree | the run's one working copy |
| `runTaskNode`, `workTaskNode`, `settleUnfinished`, `briefMatchesItsWorld` | 4860–5405 | the runner | `internal/run`'s supervisor + `bashworker` |
| `landUnchecked`, `landStopped`, `landShifted`, `landConflicted`, `landBashBeltUnaudited` | 5394–5989 | the landing roads | `LandRunTree` |
| `prepareTaskTree`, `prepareTaskTreeAt`, `prepareTaskTreeOn`, `cutTaskWorktree`, `cutWorktreeAt`, `cutWorktreeFrom`, `taskOwnFolder`, `mirrorGround`, `resolveTaskWhere`, `taskTreeSession` | 7970–8412 | worktree cutting | the run's working copy |
| `taskTree` and its methods `comeHome`, `landMirror`, `releaseKept`, `reopenReleased`, `branchStandingOn`, `releaseIdentity`, `restoreReleasedRegistration` | 7856–6540 | the worktree and its landing | `LandRunTree` |
| `commitTaskWork`, `commitTaskWorkAs`, `stageTaskWork`, `stagedPaths`, `stagedDiffStat`, `stageableWork`, `unheldLedgerPaths`, `porcelainPaths` | 8814–9153 | committing the ledger | the working copy's own status |
| `abandonMerge`, `conflictNames`, `conflictedPaths`, `leftBehind`, `rememberLeftBehind`, `namedFew`, `nonEmptyLines` | 8603–8783 | merge conflict handling | gone |
| `taskProgress`, `childrenOutstanding`, `reportedChildren`, `reported`, `taughtSomething`, `couldHaveTaught`, `addedSomething`, `freshAnswer`, `knowledgeTools`, `progressLedger` uses | 6408–6724 | the no-progress detector | the step cap |
| `newTaskAgent`, `newTaskAgentOn`, `foldTaskUsage`, `spendLedger`, `nextNodeModel`, `modelAlreadyMoved` | 7187–9385 | building a node's child agent | `NewBeltWorker` |
| `taskJournalPath`, `taskJournalDir`, `findTaskJournal`, `taskLog` | 7733–7846 | the node's transcript path | `plandb.TaskDir` |
| `taskDroppingNames`, `readTaskDropping`, `isTaskDropping`, `codeafDroppings`, `legacyCodeafDroppings`, `worktreeDirt`, `worktreeDirtIn` | 6755–6807 | droppings checks in the worktree | gone |
| `savingTools`, `producedAFile`, `landsLater`, `landingBelt`, `landingInstruction`, `englishList`, `changedPath`, `declaredFiles`, `cutDeclaration`, `declarationWord`, `insideWorktree` | 6859–7084 | the landing belt and file detection | `LandRunTree`'s own status read |
| `noteWrote`, `rememberedWrites`, `wrote`-tracking, `landingFacts`, `shiftedBy`, `heldByYourFiles`, `groundHeldNow` | 2885–6043 | the write ledger | gone |

### `internal/session/task_audit.go` (the auditor half)

Delete: `auditNode`, `auditOnce`, `afterTheCut`, `askForTheWord`,
`auditWithRepair`, `repairNode`, `repairInstruction`, `repairGround`,
`mendingLine`, `alsoChanged`, `checkerConclusion`, `auditQuestion`,
`auditReceiptBlock`, `parseAuditVerdict`, `auditWord`, `auditEvidence`,
`auditGround`, `auditGroundFor`, `restoreTaskWork`, `restoreFromGround`,
`restoreFromFolder`, `restoreFromBranch`, `restoreByCopy`, `layWork`,
`copyOriginal`, `appearedDuringTheRun`, `coveredByWrites`, `copyPath`,
`toolReceipt`, `lastToolReceipts`, `keepReceipts`, `lastReceipts`,
`auditBelt`, `boundedResult`, `shellLeash`, `verifyOnlyBash`,
`readingOnlyBash`, `auditRefusal`, `refuseOutsideAllowlist`, `refuseOutsideDoor`,
`matchesCommandPrefix`, `auditVerdict` and its methods, `auditPace` and its
methods, `noVerdict`, `secondAuditOutcome`, `auditCallRan`, `checkerRanOut`,
`checkerStalled`, `gapsOutcome`, `landAudit`, `auditorModel`, `newAuditAgent`,
`keepWorkerConclusion`, the `checker*` and `audit*` constants.

Keep (Group 3): `saidSince` (used by `taskReport`), `ResolveUnverified`,
`resolveUnverifiedBy`, `HandUnverifiedToModel`, `TakeBackDecision`,
`acceptTask`, `refuteTask`, `reauditTask`, `ErrTaskDecided`,
`ErrTaskHandedOver`, the decide doors tui3/remote/cmd call.

### `internal/session/task_brief.go` (the node road)

Delete: `composeBrief` (the node's one-message shape), `rememberAskLocked`,
`taskRequest`, `taskOriginRef`, `bindSiblingTrees`, `replaceSiblingTree`,
`treeChild`, `firstRelComponent`, `replaceWholePath`, `groundAliases`,
`indexWholePath`, `pathByte`, `originPointer`, `newTaskCopy`,
`newTaskCopyOf`, `copyOnto`, `checkCopy`, `briefScopeLocked`, `standsOn`,
`namesGround`, `cleanFolder`, `taskCopy.names`, `taskCopy.note`.

Keep (Group 3): `composeBriefScoped`, `briefScope`, `briefScopeFor`, `briefPart`,
`briefPiece`, `taskCopy` and its binding methods, the heading constants,
`withReport`'s consumer in `bashbelt_worker.go`.

### `internal/session/task.go` (the graph doors, not the vocabulary)

Delete: `taskDescription`, `taskSchemaJSON`, `taskArguments`, `taskTools`,
`mayProposeTask`, `mayFanOut`, `fansOutAt`, `proposeTask`, `stageTask`,
`stagedProposal` and its methods, `parseTaskArguments`, `taskReceipt`,
`dependencyRefusal`, `numberedTasks`, `ResolveTask`, `HoldTask`, `openTask`,
`taskWait` and its methods, `taskQuestion`, `newTaskQuestion`, `announceTask`,
`taskSilenceAdmits`, `taskClock*`, `taskWhereNotice`, `taskEscalationNote`,
`alreadyWorking`, `forgetTask`, `PendingTasks`, `proposalAsk`,
`taskNeverAnswered`, `taskWithdrawnReason`, `taskHandoffWakeSentence`.

Keep (Group 3): `taskSpec`, `taskOrigin` and its `empty` (read by `plandb_plan.go`
and `task_run_belt.go`), and `Config.mayProposeTask`/`mayFanOut` only if a
surface still asks — verify before deleting.

### `internal/session/tools_tasks.go` (the `tasks` window's engine half)

Delete: `oneTask`, `stopOneTask`, `continueOneTask`, `resolveOneTask`,
`resolvedReply`, `continueNoGraph`, `thisSessionTask`, `taskRows`,
`taskByToken`, `taskNodeCount`, `taskChildRowText`, `taskRowText`'s graph
branches, `tasksTool`'s graph verbs, `tasksDescription` and `tasksSchemaJSON`'s
graph arguments (`say`, `stop`, `continue`, `resolve`).

Keep (Group 3): the index/elsewhere rendering — `taskSearchText`,
`taskConversationHint`, `taskElsewhereText`, `taskAwayRow`, `taskAwayFamilyRow`,
`everyFamily*`, `otherProjectRow`, `taskEverywhereText`, `taskToken`,
`taskRowsText`, `taskRowsTextLimit`, `taskEntryWord`, `taskWhenWord`,
`taskFilesWord`, `taskSpanWord`, `TaskAgeWord`, `TaskIndexEntry`-shaped rows.

### `internal/session/bashbelt.go` and `plandb_plan.go` (the node-runtime road)

`bashbelt.go` keeps the belt the run's worker wears; the node-runtime slice is
delete:

| symbol | line | what it is | replaces it |
| --- | --- | --- | --- |
| `(a *Agent).bashBelt`'s node-only arms | 66 | the belt composed for a graph node | the belt `NewBeltWorker` sets (`config.InTask` + `config.bashBelt`) |
| `planCommand`, `planCommandArgs`, `withBashCommand` | 160–232 | the shim prefix on a node's bash call | the same, read off the worker's own graph (`NewBeltWorker` arms it) |

Keep: `bashBelt` (the belt itself), `branchBash`, `truncatingBash`,
`branchBashDescription`, `bashBeltSourceCaps`, `bashBeltAsked`, `BashBeltAsked`
— with the switch and the belt re-pointed at the run.

`plandb_plan.go` is two roads in one file. The node-runtime road is delete:

| symbol | line | what it is | replaces it |
| --- | --- | --- | --- |
| `planNodeSnapshot`, `(g *TaskGraph).planIfArmed`, `planPath`, `planChat` | 46–170 | reading a graph node's plan id | gone |
| `(g *TaskGraph).planSeed` | 200 | seeding the store from a graph node | the chat door's `openBeltRunStore` / `OpenRunPlan` |
| `(g *TaskGraph).planPulse` | 330 | writeback and dispatch between graph and store | the supervisor's own pass |
| `planSettleStoreTask`, `planStopLandedChildren`, `planStoreTaskSaysSomething`, `planStoreID`, `planRole`, `planIsRoot`, `planIsTask`, `planBrief` | 470–700 | graph↔store translation | the store's own rows |
| `(g *TaskGraph).planBashPrefix`, `planReviseThrough`, `planRecordSpend`, `planRunSpend`, `(a *Agent).recordPlanSpend`, `errPlanNoStore` | 700–915 | the graph's spend and revise writes | the run's spend rows (`internal/run/bashworker.go`) |
| `(g *TaskGraph).planNote` | 300 | engine-side plan log lines | gone |

Keep (Group 3): `planState`, `planStoreFilename`, `planRootID`, `planState.open`,
`planState.armShim`, `planState.shimDir`, `OpenRunPlan`, `PlanStorePath`,
`planBashPrefix`'s worker form, `resolvePlanCLI`, `planCLIProbes`,
`looksLikeTestBinary`, `fileExecutable`, `quoteShWord`, `terminalStoreStatus`,
`planCLIBinEnv`.

### `internal/session/task_run_belt.go`

Delete: the legacy fallback inside `startTaskRun` (the road that admits a node
when no engine is registered) and any graph-node admission it still carries.

Keep (Group 3): `RunSpec`, `RunSummary`, `RunLanding`, `RunEngine`, `RegisterRunEngine`
and the `/task` door over the run engine.

### `internal/session/beltfacts.go` and `prompt.go`

The belt-fact tables and prompt sections name the graph verbs
(`propose_task`, `quick_task`, `divide_work`, `revise_assignment`) and the
`tasks` window. With those tools gone, the facts that put them on the belt and
the prompt sections that teach them lose their subject and must change in the
same commit. `beltFacts`/`handoffFacts`/`programFacts`/`revisionFacts` and
`promptSections` are the symbols; the specific rows to delete are the ones whose
`tools:` list is a graph verb.

---

## Group 3 — keep — shared

The run road and the surface vocabulary the deletion does not touch. These are
read by `internal/run`, by `internal/tui3`, `internal/remote`, `cmd/codeaf`, or
by `internal/session`'s own run road.

### The run road (`internal/run`'s imports)

`NewBeltWorker`, `BeltWorkerBrief`, `TaskReport`, `Config`, `Completer`,
`LandRunTree`, `RunEngine`, `RunSpec`, `RunSummary`, `RunLanding`,
`RegisterRunEngine`, `OpenRunPlan`, `PlanStorePath`, and the `Event*` kinds
`internal/run/bashworker.go` reads (`EventToolEnd`, `EventToolFailed`,
`EventTurnDone`, `EventError`).

### The session run-road helpers (`task_run.go`, `task_audit.go`, `task_brief.go`)

| symbol | file:line | why it stays |
| --- | --- | --- |
| `taskReport`, `composeTaskReport`, `lastSaid`, `firstLines`, `taskReportFenceMarker` | `task_run.go:7084` | `Agent.TaskReport`, which `internal/run` writes as the task's result |
| `saidSince` | `task_audit.go:1424` | used by `lastSaid` |
| `withReport` | `task_run.go:6091` | used by `BeltWorkerBrief` |
| `taskResumeClause` | `task_run.go` (const) | used by `BeltWorkerBrief` |
| `journalMoment`, `journalClock` | `task_run.go:7756` | `workerJournalName` in `bashbelt_worker.go` |
| `slugify`, `shortID` | `task_run.go:9250` | branch and id minting the run road uses |
| `git`, `gitWith`, `repositoryRoot`, `climbingOutOfScratch` | `task_run.go:9153` | the run's landing uses the same git calls |
| `newTaskGraph`'s graph is not kept, but `(a *Agent).graph` is — `bashbelt_worker.go` mints the worker's own graph for the shim | | |

### The shared task vocabulary (surfaces draw with it)

`task_contract.go` wholesale — `TaskKind`, `TaskMode`, `TaskEnding`,
`TaskState`, `TaskResolution`, `TaskSettle`, `TaskNotice`, `TaskAnswer`,
`TaskPhaseNotice`, `TaskCall`, and their words. `task_status.go` wholesale —
`TaskPresence`, `TaskTier`, `TaskStatus`, `TaskAsk`, the reading function.
`task_index.go` wholesale — `TaskIndexEntry`, `ReadTaskRecordUnder` (cmd/codeaf
and tui3 read it). `taskrecord.go` — `TaskRecord` and its readers. These are the
vocabulary the pane, the rail, the room and the remote wire draw a row with,
whatever engine produced it.

### The room and steering doors (`task_room.go`)

`WatchTask`, `SteerTask`, `RetargetTask`, `TaskJournal`, and the `room` an
`Agent` opens (`openRoom`). tui3's room, remote's task lane and cmd/codeaf all
call them; the design re-points them at the store rather than deleting them.

### The task lifecycle doors (`task_run.go`, `task_audit.go`)

`WatchTaskUpdates`, `TaskUpdates`, `replayTaskRoster`, `emitTaskUpdate`,
`handBackUnsettled`, `handBackOnLoad`, `handToModelOnAuto`, `ResolveUnverified`,
`resolveUnverifiedBy`, `HandUnverifiedToModel`, `TakeBackDecision`,
`acceptTask`, `refuteTask`, `reauditTask`, `ErrTaskDecided`,
`ErrTaskHandedOver`, `settleClause`, `taskNote`, `taskNoteHead`,
`landingRecord`, `taskMergeNote`, `taskBranchFollowup`.

### The safety rails (named as staying)

`taskoutside.go` (the ground guard that already reads bash), `task_lock.go` (the
repository lock), `internal/exec/bare` (untouched), the ground ladder and the
seal. The belt's own files `bashbelt_envelope.go`, `bashbelt_truncate.go`,
`harness_belt.go`, `standingbelt.go` and `prompts/bashworker.md` are the run
road's and stay.

---

## Section 2 — the tests, the manual pages and the prompts

These must change in the same commit as the deletion: the manual gates read the
corpus and the prompt, and a page that names a tool that is gone fails them.

## Tests, grouped the same way

### Tests that drive the node engine (delete with it)

Every `internal/session/*_test.go` that builds a `TaskGraph` or a `TaskNode`,
calls the runner, the auditor, the division or quick road. The full set (from
`git grep -l 'TaskGraph\|TaskNode\|...' -- '*_test.go'`, restricted to
`internal/session`):

`admission_test.go`, `assignment_test.go`, `attach_test.go`,
`attribution_test.go`, `bashbelt_frame_test.go`, `bashbelt_plandb_test.go`*,
`bashbelt_test.go`*, `briefscope_test.go`, `cancel_test.go`, `carryladder_test.go`,
`carryontasks_test.go`, `carryonwait_test.go`, `checker_context_test.go`,
`checkpoint_quick_test.go`, `checkpoint_test.go`, `clientdoor_wire_test.go`,
`completion_ownership_test.go`, `completion_stale_test.go`, `complexity_test.go`,
`cutboundary_test.go`, `effects_test.go`, `effort_test.go`, `failedcall_test.go`,
`groundcarry_test.go`, `harness_build_test.go`, `harness_revise_test.go`,
`headlessdoor_test.go`, `inherit_test.go`, `jobrow_test.go`,
`landing_road_chat_test.go`, `landing_road_parent_test.go`, `landing_test.go`,
`loop_speed_test.go`, `mailbox_test.go`, `media_everywhere_test.go`,
`memory_test.go`, `nochange_test.go`, `orchestrate_close_test.go`,
`orchestrate_cost_test.go`, `orchestrate_family_test.go`,
`orchestraterail_test.go`, `orchestrate_rows_test.go`, `orchestrate_stuck_test.go`,
`orchestrate_test.go`, `pause_status_test.go`, `place_trees_test.go`,
`plandbcli_e2e_test.go`*, `plandb_spend_test.go`*, `principal_audit_test.go`,
`principal_delivery_test.go`, `prompt_belt_test.go`, `requestbooks_test.go`,
`route_judge_test.go`, `routing_usage_test.go`, `spawnfloor_test.go`,
`stageearly_test.go`, `standing_width_test.go`, `standing_world_test.go`,
`stopwork_test.go`, `sweep_test.go`, `task_audit_waste_test.go`,
`task_audit_words_test.go`, `task_baseline_committed_test.go`,
`task_baseline_test.go`, `task_brief_copy_test.go`, `task_brief_test.go`,
`task_carry_test.go`, `task_cascade_test.go`, `task_check_bind_test.go`,
`task_checks_contract_test.go`, `task_checks_test.go`, `task_claims_test.go`,
`taskclaims_test.go`, `task_close_test.go`, `task_contract_bind_test.go`,
`task_contract_sibling_test.go`, `task_continue_test.go`, `taskcustody_test.go`,
`taskcustody_unit_test.go`, `taskdelta_test.go`, `task_depends_kept_test.go`,
`task_divide_compose_test.go`, `task_divide_family_check_test.go`,
`task_divide_law_test.go`, `task_divide_sketch_test.go`, `task_divide_test.go`,
`task_divide_wip_test.go`, `task_ending_e2e_test.go`, `task_ending_test.go`,
`task_escalation_test.go`, `task_forward_test.go`, `taskgrade_test.go`,
`taskground_test.go`, `task_index_ending_test.go`, `task_index_started_test.go`,
`task_inherit_test.go`, `task_interrupt_test.go`, `task_journal_test.go`,
`task_landing_once_test.go`, `task_landing_protected_test.go`,
`task_landing_stamp_test.go`, `task_landing_test.go`, `task_latefold_test.go`,
`task_mirror_manners_test.go`, `taskmodelwire_test.go`, `taskname_test.go`,
`task_nested_accept_test.go`, `task_nest_test.go`, `task_park_test.go`,
`task_phase_test.go`, `taskpresence_live_test.go`, `taskpresence_test.go`,
`task_pressure_test.go`, `task_quick_door_test.go`, `task_quick_test.go`,
`task_released_landing_test.go`, `taskremodel_test.go`, `task_reply_tag_test.go`,
`task_resolve_test.go`, `task_result_e2e_test.go`, `taskretarget_test.go`,
`task_room_test.go`, `task_settle_test.go`, `task_shape_test.go`,
`task_standing_model_test.go`, `taskstands_test.go`,
`task_states_automation_test.go`, `task_states_test.go`,
`task_steer_admission_test.go`, `task_steer_durable_test.go`,
`task_steer_identity_test.go`, `task_steer_revision_test.go`,
`task_store_test.go`, `task_test.go`, `task_throttle_test.go`,
`task_tree_mirror_test.go`, `task_unsaved_test.go`, `task_yourfiles_test.go`,
`taxonomy_boundary_test.go`, `tools_conversations_test.go`,
`tools_tasks_test.go`, `treehold_test.go`, `turncontext_test.go`,
`turnhandoff_test.go`, `turnwall_test.go`, `unattendeddoor_test.go`,
`unattendedrun_test.go`, `usage_tree_test.go`, `wakecause_test.go`,
`wake_test.go`, `wallclock_test.go`, `watchwake_test.go`, `workerpage_test.go`,
`writeseam_delivery_test.go`, `writeseam_test.go`.

(* marked files also cover the run road and must be re-pointed, not deleted.)

### Tests of the partial keeps (re-point, do not delete)

`internal/session/bashbelt_test.go`, `bashbelt_plandb_test.go`,
`bashbelt_frame_test.go`, `prompt_belt_test.go`, `promptprofile_test.go`,
`tools_tasks_test.go`, `task_steer_revision_test.go`, `assignment_test.go`,
`actioncategory_test.go`, `belt_wiring_test.go`, `schemalaw_test.go`,
`capabilities_test.go`, `lawregistry_test.go`, `media_everywhere_test.go`,
`task_contract_bind_test.go`, `task_states_test.go`.

### Tests that exercise the run road (keep)

`internal/session/land_run_tree_test.go`, `plandb_spend_test.go`,
`plandb_tasks_test.go`, `task_run_belt_test.go`, `plandbcli_e2e_test.go`,
`bashbelt_plandb_test.go`.

### Tests outside `internal/session` that name the tools or the engine

- `internal/tui3` — `bundle_test.go`, `forming_test.go`, `palette_test.go`,
  `refusal_test.go`, `rowfit_test.go`, `settleboundary_test.go`,
  `taskdecided_test.go`, `taskpagelive_test.go`, `taskphase_test.go`,
  `roomorch_test.go`, `roomorch_transcript_test.go`, `runlane_test.go`,
  `railwork_test.go`, `railworkername_test.go`, `stop_test.go`,
  `taskstrip_test.go`, `salience_test.go`, `localtaskroom_test.go`,
  `nested_landing_accept_test.go`, `offlooplaw_test.go`, `sharedagent_test.go`.
- `internal/e2e` — `contracts_e2e_test.go`, `families_e2e_test.go`,
  `jobpark_e2e_test.go`, `refusedargs_e2e_test.go`,
  `taskroom_layout_e2e_test.go`, `taskcustody_e2e_test.go`,
  `taskstates_e2e_test.go`, `roomfeed_e2e_test.go`, `roomsteer_e2e_test.go`,
  `tui_e2e_test.go`, and **`tuiwords_test.go`** (the words a screen shows).
- `cmd/codeaf` — `chatv3_belt_test.go`, `chatv3_orchestrate_test.go`,
  `taskaudit_law_test.go`, `do_engine_test.go`, `role_ladder_test.go`.
- `internal/remote` — `tasklane_test.go`, `standinglane_test.go`.
- `internal/manual/chat_test.go` — the page-versus-tool gate.

## Manual pages that describe the older engine's tools by name

`internal/manual/chat/`:

- **The graph verbs** (`propose_task`, `quick_task`, `divide_work`,
  `revise_assignment`): `how-tasks-run.md`, `tasks.md`, `what-i-can-do.md`,
  `saved-shapes-of-work.md`, `adaptive-runs.md`, `screen.md`,
  `making-pictures-audio-and-video.md`, `models-and-cost.md`, `permissions.md`,
  `running-on-another-machine.md`.
- **The `tasks` window**: `tasks.md`, `how-tasks-run.md`,
  `reading-a-task-page.md`, `task-controls.md`, `adaptive-runs.md`,
  `commands.md`, `home.md`, `keys.md`, `models-and-cost.md`, `permissions.md`,
  `places.md`, `screen.md`.
- **The adaptive run**: `adaptive-runs.md`.

## Prompts that describe the older engine's tools by name

`internal/session/prompts/`:

- `fanout.md` — teaches `propose_task`'s fan-out.
- `divide.md` — teaches `divide_work`.
- `quick.md` — teaches `quick_task`.
- `revise.md` — teaches `revise_assignment`.
- `bashrules.md` — names `revise_assignment` among the worker's verbs.
- `worker.md`, `bashtask.md` — the worker's belt and the task page.
- `system.md` — whose `BELT_FACTS`, `PROGRAM_FACTS` and `STANDING_FACTS` slots
  (filled from `beltfacts.go`) put the graph verbs and the `tasks` window on the
  belt and teach them. With the verbs gone, those fact rows and the tool
  descriptions that fill them must change in the same commit.

The tool **descriptions** that name the verbs live with the tools themselves and
go with them: `taskDescription` / `taskSchemaJSON` (`task.go`),
`quickTaskDescription` / `quickTaskSchemaJSON` / `quickItemsDescription`
(`task_quick.go`), `divideDescription` / `divideSchemaJSON` (`task_divide.go`),
`reviseDescription` / `reviseSchemaJSON` (`assignment_tool.go`),
`tasksDescription` / `tasksSchemaJSON` (`tools_tasks.go`).

---

## Section 3 — what the deletion commit must carry

- This document, its `docs/changes/unreleased/1109-worker-harness-wave6.md`
  change entry, and every manual page and prompt named above, in one commit —
  the manual gates fail otherwise.
- The `CODEAF_TASK_BELT` switch removed with the belt's readers, and the
  `CODEAF_TASK_ENGINE=legacy` road removed with the engine.
- No change to `internal/exec/bare`, the ground ladder, the seal, the
  conversation's own belt, or the resident.
