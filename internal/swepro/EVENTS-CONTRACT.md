# `codeaf run` event contract

Authority: `swe-pro` commit `3b25a1abf8e84929b3851343d04809512fd361db`.
TypeScript citations below are paths and one-based lines in
`/home/abir/swe-migration/swe-pro` at that commit.

## Format truth table

| Invocation | Accepted | TypeScript stdout contract | Go result |
|---|---:|---|---|
| no `--format` | yes | Same as `--format json`; the default is `json` (`src/cli/cmd/run.ts:326-330`) | parity |
| `--format json` | yes | One compact JSON object plus `\n` for every event delivered by the wildcard bus subscription (`src/cli/harness-boot.ts:152-157`) | parity |
| `--format default` | yes | Identical to `json`. `args.format` is never read by `runHandler`; only `args.tui` selects the output path (`src/cli/cmd/run.ts:371-377,414-421`) | parity |
| `--format ndjson`, `text`, `pretty`, or another value | no | Rejected by the yargs choices before the handler (`src/cli/cmd/run.ts:326-330`) | parity |
| either format plus `--tui` | yes in TS | The wildcard NDJSON subscription is not installed; the renderer owns stdout (`src/cli/harness-boot.ts:152-177`). The flag is a full UI, not a formatting alias (`src/cli/cmd/run.ts:331-335`). | known deferral: Go exits 1 with a clear unsupported message |

There is no `ndjson`, `text`, or `pretty` choice. The accepted value named
`default` is somewhat misleading: on this command it still selects exactly the
same NDJSON bus tap as `json`.

## TypeScript wire shape and ordering

Every TS line is the result of `JSON.stringify(event)`, where the bus constructs
the event as:

```json
{"id":"evt_...","type":"message.updated","properties":{}}
```

The only universal top-level keys are `id`, `type`, and `properties`
(`src/bus/index.ts:21-25,87-95`). There are no universal `stage`, `status`,
`data`, `message`, `session_id`, or `ts` keys. Event IDs are generated unless a
sync event supplies its existing ID (`src/bus/index.ts:87-90` and
`src/sync/index.ts:316-322`).

Ordering guarantees are deliberately narrow:

- The run installs one wildcard subscription and writes callbacks in delivery
  order (`src/cli/harness-boot.ts:152-157`). A bus publication reaches the typed
  channel before the wildcard channel (`src/bus/index.ts:87-95`).
- Within one session creation, `session.created` is initiated before the
  compatibility `session.updated` publication (`src/session/session.ts:536-545`).
- `session.status` with `status.type == "idle"` is published immediately before
  `session.idle` (`src/session/status.ts:81-89`).
- There is no declared global phase order, terminal sentinel, or promise that
  concurrent child-session events are contiguous. Sync-event conversion may
  also complete asynchronously (`src/sync/index.ts:316-322`). Consumers must
  correlate by IDs inside `properties`.
- A normal TS run ends with `process.exit(0)` and emits no `terminal` event
  (`src/cli/cmd/run.ts:2921-2929`).

## TypeScript event truth table

`run.ts` subscribes to **all** instance-bus events rather than enumerating an
allowlist. The table therefore lists every event definition reachable from the
run's instance/runtime after the subscription is installed. A row is emitted
only when its publisher runs; model behavior, tool use, compaction, errors, and
background watchers make the count and sequence variable.

| `type` | `properties` keys (top level) | When/order notes | Source |
|---|---|---|---|
| `session.created` | `sessionID`, `info` | Each root or child session; creation precedes its compatibility update | `src/session/session.ts:303-320,504-545` |
| `session.updated` | `sessionID`, `info` | Full `info`, including when the stored update began as a patch | `src/session/session.ts:303-321`; expansion at `src/server/projectors.ts:11-18` |
| `session.deleted` | `sessionID`, `info` | Only if a run-path cleanup removes a session | `src/session/session.ts:322-327,589` |
| `message.updated` | `sessionID`, `info` | User and assistant message lifecycle | `src/session/message-v2.ts:596-622`; publish calls `src/session/session.ts:598` |
| `message.removed` | `sessionID`, `messageID` | Revert/cleanup only | `src/session/message-v2.ts:598-603,623-628`; `src/session/revert.ts:131-136` |
| `message.part.updated` | `sessionID`, `part`, `time` | Durable part snapshots; `part.type` and tool `part.state.status` carry their own discriminants | `src/session/message-v2.ts:604-634`; `src/session/session.ts:604-608` |
| `message.part.delta` | `sessionID`, `messageID`, `partID`, `field`, `delta` | Streaming text/reasoning deltas; may occur many times before the final part update | `src/session/message-v2.ts:635-644`; `src/session/session.ts:776-784` |
| `message.part.removed` | `sessionID`, `messageID`, `partID` | Revert/cleanup only | `src/session/message-v2.ts:610-615,645-650`; `src/session/revert.ts:143-148` |
| `session.status` | `sessionID`, `status` | `status.type` is `busy`, `idle`, or `retry`; retry also has `attempt`, `message`, optional `action`, and `next` | `src/session/status.ts:10-45,81-89` |
| `session.idle` | `sessionID` | Deprecated companion emitted after idle status | `src/session/status.ts:46-52,81-89` |
| `session.error` | optional `sessionID`, `error` | Prompt/processor/config/plugin failures | `src/session/session.ts:335-343`; examples `src/session/prompt.ts:526,672,823` |
| `session.diff` | `sessionID`, `diff` | Summary or revert computes file diffs | `src/session/session.ts:328-334`; `src/session/summary.ts:107`, `src/session/revert.ts:88` |
| `session.compacted` | `sessionID` | After a successful context compaction | `src/session/compaction.ts:39-46,811` |
| `file.edited` | `file` | Explicit write/edit/apply-patch tool publication | `src/file/index.ts:70-77`; `src/tool/write.ts:74`, `src/tool/edit.ts:119-120`, `src/tool/apply_patch.ts:258` |
| `file.watcher.updated` | `file`, `event` | `event` is `add`, `change`, or `unlink`; may also follow `file.edited` | `src/file/watcher.ts:24-32,101-103`; tool examples above |
| `question.asked` | `id`, `sessionID`, `questions`, optional `tool` | Conditional on a model using the question tool; TS then waits for an answer | `src/question/index.ts:61-69,96-99,155-179` |
| `question.replied` | `sessionID`, `requestID`, `answers` | Only an attached answerer can cause it | `src/question/index.ts:85-89,98,182-200` |
| `question.rejected` | `sessionID`, `requestID` | Only an attached rejector can cause it | `src/question/index.ts:91-99,202-215` |
| `lsp.client.diagnostics` | `serverID`, `path` | Conditional background LSP publication | `src/lsp/client.ts:42-50,169` |
| `lsp.updated` | none (`{}`) | Conditional background LSP state change | `src/lsp/lsp.ts:21-23,304` |
| `vcs.branch.updated` | optional `branch` | Conditional watcher-driven branch change | `src/project/vcs.ts:214-221,306-315` |
| `server.instance.disposed` | `directory` | Scope-disposal event on non-hard-exit/error cleanup; normal `run.ts` calls `process.exit(0)` first | `src/bus/index.ts:57-69`; wrapper cleanup `src/cli/effect-cmd.ts:160-166` |

Registered events that are not run-path output at this pin are
`permission.asked`/`permission.replied` (autonomous permission checks do not
publish or wait: `src/permission/index.ts:185-211`), `todo.updated` (the run
does not call the todo service), and `project.updated` (project loading happens
before the handler installs the wildcard tap).

## Go parity events

The Go runtime uses one instance bus for durable session services, its
runtime-owned question service, the headless question auto-reject subscriber,
and the stdout wildcard tap (`cmd/codeaf/runtime.go`, `cmd/codeaf/pipeline.go`).
The writer attaches before tool execution and writes each payload without
wrapping it (`cmd/codeaf/events.go`). Durable and question events are therefore
both emitted as `{id,type,properties}`. These events are **fixed divergences**:
they previously existed internally on split buses, leaving question events
missing from CLI stdout.

| Go `type` | `properties` keys | Classification |
|---|---|---|
| `session.created`, `session.updated`, `session.deleted` | `sessionID`, `info` | parity |
| `message.updated` | `sessionID`, `info` | parity |
| `message.removed` | `sessionID`, `messageID` | parity |
| `message.part.updated` | `sessionID`, `part`, `time` | parity |
| `message.part.delta` | `sessionID`, `messageID`, `partID`, `field`, `delta` | parity |
| `message.part.removed` | `sessionID`, `messageID`, `partID` | parity |
| `session.status` | `sessionID`, `status` | fixed divergence: Go incorrectly published runner idle/busy as `session.updated` with a `status` field; now matches TS |
| `session.idle` | `sessionID` | fixed divergence: restored TS's ordered idle companion |

`TestBusEventUsesPinnedTypeScriptWireShape` pins the exact envelope,
`TestPipelineStreamsTypeScriptBusEvents` pins the pipeline subscription and
creation ordering, and
`TestRunnerIdleUsesPinnedSessionStatusEvents` pins the corrected event name,
payload, and `session.status` -> `session.idle` order.

## Go control-plane extensions

Go also retains its pre-existing compact records because `codeaf serve` and the
AgentField control-plane bridge consume them. They are extensions, not claimed
TS parity. Their top-level shape remains
`type`, optional `stage`/`status`/`message`/`session_id`/`data`, and `ts`
(`cmd/codeaf/events.go`). Every `events.stage`/`events.emit` call is inventoried
below.

| `type` | `stage` | `status` values | `data` keys | Go source | Classification |
|---|---|---|---|---|---|
| `stage` | `bootstrap` | `ready` | `workspace`, `db_path` | `cmd/codeaf/pipeline.go` (`prepareWorkspace`) | Go extension |
| `stage` | `resume` | `rehydrated` | `project_id`, `root_task_id` | `cmd/codeaf/pipeline.go` (`run`) | Go extension |
| `stage` | `root-cut` | `decompose`, `selected` | none | `cmd/codeaf/pipeline.go` (`run`) | Go extension |
| `stage` | `pre-gates` | `disabled` | none | `cmd/codeaf/pipeline.go` (`run`) | Go extension |
| `stage` | `root-orchestrator` | `running`, `completed`, `draining`, `stalled`, `root-complete` | none; or `root_task_id`, optional `open_tasks`, `error` | `cmd/codeaf/pipeline.go` (`run`); `cmd/codeaf/root_orchestrator.go` (`runRootOrchestrator`) | Go extension |
| `stage` | `entry-agent` | `running`, `completed` | running: `agent` | `cmd/codeaf/pipeline.go` (`run`) | Go extension |
| `stage` | `classifier` | `running`, then `trivial`/`focused`/`vague` | result: `reason` | `cmd/codeaf/pipeline.go` (`planAndPopulate`) | Go extension |
| `stage` | `product` | `running`, `wrote`, `failed` | none | `cmd/codeaf/pipeline.go` (`planAndPopulate`) | Go extension |
| `stage` | `architecture` | `running`, `wrote`, `force_approved`, `failed` | running: `mode` | `cmd/codeaf/pipeline.go` (`planAndPopulate`) | Go extension |
| `stage` | `planner` | `running`, `fallback-root`, `translated` | translated: `tasks` | `cmd/codeaf/pipeline.go` (`planAndPopulate`) | Go extension |
| `stage` | `issue-writer` | `running`, `failed`, `completed` | running: `tasks`; completed: `written`, `failed` | `cmd/codeaf/pipeline.go` (`planAndPopulate`) | Go extension |
| `stage` | `plan-apply` | `failed`, `completed` | failed: `reason`; completed: `tasks`, `edges`, `root_task_id`, `project_id` | `cmd/codeaf/pipeline.go` (`planAndPopulate`) | Go extension |
| `stage` | `stale-reaper` | `released` | `tasks` | `cmd/codeaf/pipeline.go` (`dispatchUntilQuiet`) | Go extension |
| `stage` | `scheduler` | `cycle`, `cycle-complete`, `cycle-failed`, `dependency-impossible`, `stuck-silence`, `quiet-wait`, `exhausted`, `budget-exhausted`, `drain-sweep`, `stalled` | union of `cycle`, `dispatched`, `failed`, `scanned`, `summary`, `error`, `reason`, `open`, `active`, `ready`, `resume_seed`, `max_loops`, `open_tasks`, `open_descendants`, `statuses`, `quiet_cycles`, `pause_cycles`, `quiet_reason`, `live_dispatches`, `closed_count`, `closed_task_ids`, `released_count`, `released_task_ids` | `cmd/codeaf/pipeline.go` (`dispatchUntilQuiet`); `cmd/codeaf/root_orchestrator.go` (`Pump`) | Go extension; `drain-sweep` is Go-only machinery |
| `stage` | `verification` | `pass`, `fail` | `commands`; fail also `reason` | `cmd/codeaf/full_verification.go` | Go extension |
| `stage` | `audit` | `running`, `pass`, `fail`, `skipped`, `escalated` | `cycle` | `cmd/codeaf/pipeline.go` (`auditFixLoop`) | Go extension |
| `stage` | `fix-generator` | `running`, `dispatch_fixes`, `give_up` | running: `cycle`; result: `added` | `cmd/codeaf/pipeline.go` (`auditFixLoop`) | Go extension |
| `stage` | `pr-ready` | `running`, `completed`, `skipped`, `planner-failed`, `formatter-failed` | none | `cmd/codeaf/pipeline.go` (`run`) | Go extension |
| `stage` | `exit-guard` | `fired`, `recovery-completed`, `recovery-failed`, `redispatch-completed`, `redispatch-failed` | fired: `failure_ids`, `task_ids`; recovery: `applied`, `failed`, optional `error` | `cmd/codeaf/exitguard.go` | Go extension |
| `terminal` | none | `pass`, `refused`, `fail`, `escalated`, `budget-exhausted`, `crashed` | `cycle`, `cost_usd`, `project_id`, `root_task_id`; plus top-level `message`, `session_id` | `cmd/codeaf/main.go` (`runCLI`) | Go extension; TS has no terminal sentinel |
| `supervisor` | `auto-resume` | `passed`, `budget-exhausted`, `no-progress`, `attempt-cap` | `attempts`; plus top-level `message` | `cmd/codeaf/main.go` (`runAutoResume`) | Go extension |

The former TUI `notice` event has been removed: `--tui` now fails before an
event writer or pipeline is created, so it cannot misleadingly continue as a
headless run.
