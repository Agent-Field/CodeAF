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
or `shutdown` closes the engine. `codeaf app --home <dir>` chooses its state root. It moves codeaf's own state and
its own `.codeaf/skills`, and not the person's login home, so other programs'
skills are still read from that home; the engine records it in
`CODEAF_LOGIN_HOME` for the processes it starts.

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
| `agents.continue` | `in.IDParams` | `agentResult` |
| `agents.create` | `in.CreateAsk` | `agentResult` |
| `agents.debug` | `in.IDParams` | `ConversationDebug` |
| `agents.delete` | `{confirm, id}` | `Empty` |
| `agents.entries` | `in.EntriesParams` | `EntriesResult` |
| `agents.errand` | `in.ErrandParams` | `Errand` |
| `agents.errand.models` | `in.ErrandModelsParams` | `ErrandModels` |
| `agents.hosted_list` | `{org}` | `HostedListResult` |
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
| `agents.set_scope` | `{id, scope}` | `AgentScope` |
| `agents.set_workstream` | `{id, workstream}` | `AgentWorkstream` |
| `agents.since` | `{after, id}` | `Since` |
| `agents.steer` | `in.sayTo` | `Empty` |
| `agents.stop` | `in.IDParams` | `Empty` |
| `agents.suggest_reply` | `in.ReplySuggestionParams` | `ReplySuggestion` |

The `agents.hosted` feature enables `agents.hosted_list` and the optional
`org` connection ID on `agents.create`. The hosted list returns the caller's
own and shared chats, newest first. Its `available` field is true when the
organization's server serves hosted chats. A server with hosted chats off
returns an empty `agents` array and `available: false`. The server's
`hosting` policy object is included when supplied. Authentication failures
answer `signed_out` and transport failures answer `unreachable`.

Hosted rows carry `hosted` facts and use a self-describing
`<session>_<connection>` ID. `hosted.verbs` is what the organization's server
says the person may do in that chat, taken from the `verbs` of the server's
list row as it stood when the list was read; the app works nothing out from a
role name, and a row the server sent no verbs for reads `["view"]`. The
server's wire enforces the verbs whatever a window shows. `agents.open` attaches that session's wire and
`agents.close` releases it. Use `agents.stop` to interrupt its current turn.
`agents.list` stays scoped to chats on this computer.

`agents.stop` is a bounded stop. It interrupts the running turn and answers at
once; a turn that has still not let go ten seconds later (one parked on a wait
that never reads its cancelled context) is abandoned by the engine: its stream
closes, the turn ends `turn.done` with `error_kind: stopped` and the "Stopped by
you" footer as any stopped turn does, and the next `agents.send` opens an
ordinary turn. The conversation's journal records the turn as let go of, with
what it had spent. A running factory stage lent to the app, and a turn the
factory's runner gives a job manager the app holds open, are interrupted but
never abandoned from here: the runner reads those turns to their end. The job's
own Stop ends them. A person's own turn in a job manager's chat keeps the bound.

A chat the app's quit or unexpected close stopped mid-turn is never carried on
by the engine's next start. Its `Agent` reads `interrupted: true`, an
`interrupted` footer (words "Interrupted") sits where the turn stopped, and an
open of it holds its recovered task work back. `agents.continue {id}` consumes
the mark once: held tasks carry on and, when a reply was cut, one turn carries
on behind a `resumed_after_quit` or `resumed_after_close` footer. A second call
answers the agent and does nothing. A person's send, queue, steer, answer or
retry in that chat also clears the mark.

## Conversation work scope

`agents.set_scope {id, scope}` answers `{id, scope}` with the stored word.
`project` is the default: the conversation is told about other conversations'
landed and running work in its folder, as today. `conversation` means it is told
only about its own work: its turns receive no other-conversation `<elsewhere>`
block, and its `tasks` tool lists and addresses only its own tasks. Neither the
other-window running section nor the wider `scope: "everywhere"` section is
added. Its instructions and the `tasks` tool description say that other
conversations' work is hidden from it, instead of promising that work. Task
nodes keep their existing scope.

The choice is stored with the conversation and survives restarts. It takes
effect from the conversation's next turn, instructions and tool description
included; a live tasks call also reads the stored choice. Setting it never opens, warms or interrupts the conversation,
never moves its folder, and never changes its transcript, title, model or tasks.
The write-safety checks still warn before its own work writes over files other
conversations are writing. The person's task list is unchanged.

Engines that serve this method announce `agents.scope` in `hello.features`.
A missing id or a scope other than `project` or `conversation` is `bad_params`;
an engine without the door answers `unknown_method`. An unknown conversation is
`not_found`; a lent conversation or one without a local folder is `conflict`.
No `agent.changed` event is emitted, because the Agent view carries no scope.

## Conversation workstream mark

`agents.set_workstream {id, workstream}` answers `{id, workstream}` with the
stored mark. `workstream` is the id of the workstream the conversation belongs
to (an Afi or one of a workstream's chats), at most 128 characters; `""` clears
it. Any client that owns workstreams may set it: a desk for a Personal
workstream, an organization server for its own. A conversation an organization
server runs with its own workstream door counts as marked without the call.

A marked conversation:

- is never captured into the knowledge Library: its sources are passed over and
  never become the compiler's sources, whatever the Library's own setting;
- has no `automation` tool, so standing work for a workstream is set up in the
  workstream itself;
- is told on its instructions page that what it makes goes to the workstream's
  Files, that the Library is reached only when a person adds something there,
  and where a person sets up work on a clock.

The mark is stored with the conversation and survives restarts. A held
conversation follows it from its next turn's start, when the `automation` tool
is withdrawn and the page re-rendered; clearing the mark returns the tool at the
next open. Setting it never opens, warms or interrupts the conversation, and
never changes its transcript, title, model or tasks.

Engines that serve this method announce `agents.workstream` in `hello.features`.
A missing id or a workstream over 128 characters is `bad_params`; an engine
without the door answers `unknown_method`. An unknown conversation is
`not_found`; a lent conversation or one without a local folder is `conflict`.
No `agent.changed` event is emitted.

## Client tools

The optional features `agents.client_tools` and `agents.client_tools.source`
appear together in `hello.features` exactly when the engine implements the
client-tools interface. The latter guarantees message identity on calls whenever
the running turn answers a message. `agents.client_tools.remote` additionally
advertises forwarding through a joined conversation's remote connection when
its holder supports it. Sets belong to this
stdio connection and one conversation. They are memory-only: register again
after reconnecting. Attachment does not open the conversation or start a turn.
Children, workers and clarification conversations inherit neither tools nor
instructions. Within the same app connection, every join or re-join pushes the
conversation's current sets to its holder; a roaming link resends them after
redial only when the welcome identifies the same conversation. A changed
conversation forgets the old snapshot and ends its calls. When another window
starts a new conversation or opens another one in the holder, every
connection's sets for the old conversation are dropped and its calls end, so
none reach the replacement. A returning owner's
random token moves its sets to the replacement connection even while the old
connection remains attached; other owners still face collision checks. A
conversation returning to this engine uses the registry directly.
If another computer holds the chat (`Location.Here == false`), attachment still
validates and stores the sets without opening the conversation. They apply
when this engine runs it again after "Continue here" or take, without a second
`agents.tools_set`.

| method | params | result |
|---|---|---|
| `agents.tools_set` | `{agent, set, instructions?, deadline_ms?, tools?: [{name, description, input_schema, category}]}` | `{agent, set, tools: [name, ...]}` |
| `agents.tool_result` | `{call, result: <any JSON>}` or `{call, error: <text>}` | `{accepted: true}` or `{accepted: false}` |

`agents.tools_set` replaces that `agent` + `set` whole; other sets remain.
Omitted `tools` or `tools: []` removes only that set and returns `tools: []`.
An agent holds at most 4 sets, 32 tools per set and 64 tools total. All validation
is atomic: a refusal leaves every set unchanged.

- `set` matches `^[a-z][a-z0-9_.-]{0,63}$`.
- `name` matches `^[a-z][a-z0-9_]{0,63}$`, unique within and across the agent's sets.
- `description` is 1–2048 bytes. `input_schema` is a JSON object whose `type`
  is `"object"`, at most 16 KiB. Schema validation of call arguments is the app's job.
- `category` is `read`, `edit` or `create` and supplies this conversation's tool
  category. Reading-only contexts refuse edits and creations. Ordinary tool
  approval rules apply; attachment is not approval.
- `instructions` is at most 4096 bytes per set and may be empty. It appears in a
  section headed `Tools from the app: <set>`. A caller-supplied system page is
  preserved and receives no section. Ordinary conversations gain no text.
- `deadline_ms` is 0 (the default 30000) or 1000–120000.

Invalid fields, aggregate limits, duplicates and built-in tool names answer
`bad_params`; a collision message names the tool. Collision checks include the
live belt and all classified engine tools. A built-in arriving later wins and
the colliding client tool leaves the belt. No model request has duplicate names.
Unknown agents answer `not_found`. A joined holder without forwarding support
answers `bad_scope`: `client tools need a conversation this engine runs`.
Holder refusals answer `bad_params` with the holder's message; a failed or timed
out link answers `unreachable`, leaving the app registry unchanged for retry.
Different remote connections keep separate sets. The aggregate limits count
all of them, tool-name collisions are refused atomically, and only the
connection that owns a set can receive or answer its calls.
Tools and instructions update at the next step boundary, with client tools at
the tail after all built-ins.

| event | data |
|---|---|
| `agents.tool_call` | `{agent, turn, call, turn_call, source_message_id?, set, name, args: <JSON object>, deadline_ms}` |

`source_message_id` names the message the running turn answers: the same string
as `Entry.message_id` on that message's chat entry in `agents.read` (and in
`entry.added` when present). It is captured when the turn starts, never changed
by a mid-turn steer or note, and survives journal restarts and call replays.
It is absent when no message can be named exactly. A stopped answer reuses its
recorded message id. Turns the engine starts itself, including ordinary session
wakes, mixed batches and a carry-on after a quit or close, carry none.
With `agents.client_tools.source` advertised,
an absent field means no such message, rather than an older engine.

One model call emits exactly one `agents.tool_call`. `turn` is the engine's
process-local conversation counter, which resets after restart;
`turn_call` matches `turn.tool.call`. `call` is `ct_` followed by 64 lowercase
hex digits. It is a hash of the whole call: the agent, the provider call id, the
set, the tool name and the exact argument bytes. The same call dispatched again
keeps its `call`, including after an engine restart. A provider call id alone
can repeat: Kimi-family models write their own `name:index` ids, which repeat
after `agents.rewind` or compaction, and a faulty server can repeat any id. So a
reused provider id with another tool or other arguments is a new call with its
own `call` and its own event. Without a provider id the engine mints a random
`call`, which no later launch repeats. A concurrent redispatch joins the
pending wait and emits no second event. A settled redispatch returns the saved
answer without an event for 10 minutes, subject to a maximum of 1024 saved
answers per connection. Non-object model arguments become an invalid-arguments
error result without an event.

`agents.tool_result` requires exactly one of `result` and `error`. `result`
accepts any JSON, including `null`, and reaches the model as compact JSON text,
cut at 64 KiB with a plain dropped-byte marker. `error` must be a nonempty string
of at most 4096 bytes and becomes an error tool result:
`<set> could not do <name>: <text>`. Invalid answers are `bad_params`.
A pending answer returns `accepted: true`; unknown, late or duplicate ids return
`accepted: false` without an error or any other effect.

Connection end (EOF, shutdown or context cancellation) disconnects the registry
before conversations close: all sets are dropped and all pending calls settle
immediately. These failures reach the model as error tool results, with no
engine retry.

For forwarded calls, a disconnect observed by the holder immediately removes
that connection's sets and settles its waits. A link loss the holder cannot
observe ends at the set's `deadline_ms`. The app drops the pending entry when
its link ends or the holder reports that the call ended; later results return
`accepted: false`. Failure text is:

- A stale call after removal or disconnection:
  `client_unavailable: the app that provides <name> is not connected; nothing was done.`
- Disconnection during a call:
  `client_unavailable: the app that provides <name> disconnected before answering; the outcome is unknown, so check before trying again.`
- Deadline:
  `client_timeout: the app that provides <name> did not answer within <n>s; the outcome is unknown, so check before trying again.`
- Turn cancellation: the turn ended before the app answered. The pending entry
  is dropped immediately and later answers return `accepted: false`.

## asks methods

| method | params | result |
|---|---|---|
| `asks.answer` | `{agent, batch_item, id, later, option, scope, text, why, you_decide}` | `Empty` |
| `asks.answer_secret` | `{agent, id, secret}` | `Empty` |
| `asks.defer` | `in.askAt` | `AskResult` |
| `asks.hold` | `{agent, id}` | `Empty` |
| `asks.list` | `in.AgentParams` | `AsksResult` |
| `asks.shown` | `in.askAt` | `AskResult` |

An ask whose answer is a credential carries `secret: {placeholder?, keeps_for_s?}`.
The app draws one masked box, never shows the value back, and sends it with
`asks.answer_secret`, a method of its own so a wire trace drops `secret` by name.
`bad_params` refuses an ask without `secret` and an empty value; to say no, answer
the ask's Decline option with `asks.answer`, which refuses any `text` on a
secret ask (`bad_params`) and so never carries the value. `keeps_for_s` is how long the engine
keeps the value in a private file the agent's commands read (the model's own
`request_secret`); absent, the value goes to its lane (a connection's key).

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
| `chats.live_keyboard` | `ChatLiveKeyboardParams` | `ChatLiveKeyboardResult` |
| `chats.open_live` | `ChatOpenLiveParams` | `ChatOpenLiveResult` |
| `chats.read` | `ChatReadParams` | `ChatReadResult` |
| `chats.preview` | `{cell}` | `{last_user?: string, last_assistant?: string}` |
| `chats.send` | `ChatSendParams` | `ChatSendResult` |

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
| `devices.permit` | `in.DevicePermitParams` | `DevicePermitResult` |
| `devices.remove` | `in.DeviceRemoveParams` | `DeviceRemoveResult` |
| `devices.rename` | `in.DeviceRenameParams` | `DeviceRenameResult` |

## factory methods

| method | params | result |
|---|---|---|
| `factory.access_cancel` | `in.FactoryAccessParams` | `FactoryAccess` |
| `factory.access_check` | `in.FactoryAccessParams` | `FactoryAccess` |
| `factory.access_sign_in` | `in.FactoryAccessParams` | `FactoryAccess` |
| `factory.access_sign_up` | `in.FactoryAccessSignUpParams` | `FactoryAccess` |
| `factory.access_reset` | `in.FactoryAccessResetParams` | `FactoryAccess` |
| `factory.password_sign_in` | `in.FactoryPasswordSignInParams` | `FactoryPasswordSignedIn` |
| `factory.password_change` | `in.FactoryPasswordChangeParams` | `Empty` |
| `factory.passkey_start` | `in.FactoryPasskeyStartParams` | `FactoryPasskeyStarted` |
| `factory.passkey_poll` | `in.FactoryPasskeyPollParams` | `FactoryPasskeyPolled` |
| `factory.passkey_cancel` | `in.FactoryPasskeyPollParams` | `Empty` |
| `factory.passkey_list` | `in.FactoryPasskeyListParams` | `FactoryPasskeyList` |
| `factory.passkey_remove` | `in.FactoryPasskeyRemoveParams` | `Empty` |
| `factory.password_confirm` | `in.FactoryPasswordConfirmParams` | `Empty` |
| `factory.activity` | `in.FactoryActivityParams` | `FactoryActivity` |
| `factory.attach` | `in.FactoryAttachParams` | `FactoryConnection` |
| `factory.capabilities` | `in.FactoryCapabilitiesParams` | `FactoryCapabilities` |
| `factory.intake_add` | `in.FactoryIntakeParams` | `inbox.Result` |
| `factory.features` | `in.FactoryRef` | `FactoryFeatures` |
| `factory.teams` | `in.OrgConn` | `OrgTeams` |
| `factory.team_create` | `in.OrgTeamCreateParams` | `OrgTeam` |
| `factory.team_rename` | `in.OrgTeamRenameParams` | `OrgTeam` |
| `factory.team_delete` | `in.OrgTeamParams` | `Empty` |
| `factory.team_member_add` | `in.OrgTeamMemberParams` | `OrgTeam` |
| `factory.team_member_remove` | `in.OrgTeamMemberParams` | `OrgTeam` |
| `factory.org_workstreams` | `in.OrgWorkstreamsParams` | `OrgWorkstreams` |
| `factory.org_workstream` | `in.OrgWorkstreamParams` | `OrgWorkstream` |
| `factory.org_workstream_create` | `in.OrgWorkstreamCreateParams` | `OrgWorkstream` |
| `factory.org_workstream_update` | `in.OrgWorkstreamUpdateParams` | `OrgWorkstream` |
| `factory.org_workstream_delete` | `in.OrgWorkstreamParams` | `Empty` |
| `factory.org_workstream_owner` | `in.OrgWorkstreamOwnerParams` | `OrgWorkstream` |
| `factory.org_workstream_shares` | `in.OrgWorkstreamSharesParams` | `OrgWorkstreamShared` |
| `factory.org_workstream_share_preview` | `in.OrgWorkstreamSharePreviewParams` | `OrgSharePreview` |
| `factory.org_workstream_import` | `in.OrgWorkstreamImportParams` | `OrgWorkstream` |
| `factory.org_workstream_archive_keep` | `in.OrgWorkstreamParams` | `OrgArchiveKeep` |
| `factory.org_workstream_fold` | `in.OrgWorkstreamParams` | `OrgWorkstreamFolded` |
| `factory.org_presence` | `in.OrgPresenceParams` | `OrgPresence` |
| `factory.org_workstream_activity` | `in.OrgActivityParams` | `OrgActivity` |
| `factory.org_doc_read` | `in.OrgDocParams` | `OrgDocState` |
| `factory.org_doc_send` | `in.OrgDocSendParams` | `OrgDocUpdated` |
| `factory.org_doc_snapshot` | `in.OrgDocSnapshotParams` | `OrgDocSnapshotKept` |
| `factory.org_doc_aware` | `in.OrgDocAwareParams` | `Empty` |
| `factory.org_doc_follow` | `in.OrgDocParams` | `Empty` |
| `factory.org_doc_unfollow` | `in.OrgDocParams` | `Empty` |
| `factory.org_workstream_page_create` | `in.OrgPageCreateParams` | `OrgWorkstream` |
| `factory.org_workstream_page_update` | `in.OrgPageUpdateParams` | `OrgWorkstream` |
| `factory.org_workstream_page_delete` | `in.OrgPageParams` | `OrgWorkstream` |
| `factory.org_workstream_page_versions` | `in.OrgPageParams` | `OrgPageVersions` |
| `factory.org_workstream_page_version` | `in.OrgPageVersionParams` | `OrgPageVersion` |
| `factory.org_workstream_page_snapshot` | `in.OrgPageSnapshotParams` | `OrgPageVersion` |
| `factory.org_workstream_page_restore` | `in.OrgPageVersionParams` | `OrgPageVersion` |
| `factory.org_workstream_plan_get` | `in.OrgWorkstreamParams` | `OrgWorkstreamPlan` |
| `factory.org_workstream_plan_set` | `in.OrgPlanSetParams` | `OrgWorkstreamPlan` |
| `factory.org_workstream_wakes` | `in.OrgWorkstreamParams` | `OrgWorkstreamWakes` |
| `factory.org_workstream_wake_add` | `in.OrgWakeAddParams` | `json.RawMessage` |
| `factory.org_workstream_wake_update` | `in.OrgWakeUpdateParams` | `json.RawMessage` |
| `factory.org_workstream_suggestions` | `in.OrgSuggestionsParams` | `OrgWorkstreamSuggestions` |
| `factory.org_workstream_suggestion_add` | `in.OrgSuggestionAddParams` | `json.RawMessage` |
| `factory.org_workstream_suggestion_update` | `in.OrgSuggestionUpdateParams` | `json.RawMessage` |
| `factory.org_workstream_job_add` | `in.OrgWorkstreamJobParams` | `OrgWorkstream` |
| `factory.org_workstream_job_remove` | `in.OrgWorkstreamJobParams` | `OrgWorkstream` |
| `factory.org_workstream_unarchive` | `in.OrgWorkstreamParams` | `OrgWorkstream` |
| `factory.org_workstream_files` | `in.OrgFilesParams` | `OrgWorkstreamFiles` |
| `factory.org_workstream_file_create` | `in.OrgFileCreateParams` | `OrgWorkstreamFile` |
| `factory.org_workstream_file_update` | `in.OrgFileUpdateParams` | `OrgWorkstreamFile` |
| `factory.org_workstream_file_move` | `in.OrgFileMoveParams` | `OrgWorkstreamFiles` |
| `factory.org_workstream_file_delete` | `in.OrgFileParams` | `OrgWorkstreamFile` |
| `factory.org_workstream_file_restore` | `in.OrgFileParams` | `OrgWorkstreamFile` |
| `factory.org_workstream_file_content` | `in.OrgFileParams` | `OrgFileContent` |
| `factory.org_agent_runner` | `in.OrgConn` | `OrgAgentRunner` |
| `factory.org_agent_runner_set` | `in.OrgAgentRunnerSetParams` | `OrgAgentRunner` |
| `factory.org_workstream_drive` | `in.OrgWorkstreamParams` | `OrgWorkstreamDriver` |
| `factory.org_workstream_let_go` | `in.OrgWorkstreamParams` | `Empty` |
| `factory.org_workstream_spend` | `in.OrgWorkstreamSpendParams` | `OrgWorkstreamSpend` |
| `factory.org_keys` | `in.OrgConn` | `OrgKeys` |
| `factory.org_key_set` | `in.OrgKeySetParams` | `OrgKey` |
| `factory.org_key_delete` | `in.OrgKeyParams` | `Empty` |
| `factory.org_workstream_agent` | `in.OrgWorkstreamAgentParams` | `OrgWorkstreamAgent` |
| `factory.org_workstream_agent_send` | `in.OrgWorkstreamAgentSendParams` | `OrgWorkstreamAgent` |
| `factory.org_workstream_agent_cards_add` | `in.OrgAgentCardsAddParams` | `OrgWorkstreamAgent` |
| `factory.org_workstream_agent_stop` | `in.OrgWorkstreamAgentStopParams` | `OrgWorkstreamAgent` |
| `factory.org_profile` | `in.OrgConn` | `OrgProfile` |
| `factory.org_profile_set` | `in.OrgProfileSetParams` | `OrgProfile` |
| `factory.org_setup` | `in.OrgSetupParams` | `OrgSetupView` |
| `factory.org_setup_send` | `in.OrgSetupSendParams` | `OrgSetupView` |
| `factory.org_setup_stop` | `in.OrgConn` | `OrgSetupView` |
| `factory.org_setup_apply` | `in.OrgSetupApplyParams` | `OrgSetupApplied` |
| `factory.org_setup_by_hand` | `in.OrgConn` | `OrgProfile` |
| `factory.org_work_home_get` | `in.OrgConn` | `json.RawMessage` |
| `factory.org_workstream_overview_get` | `in.OrgTasksParams` | `json.RawMessage` |
| `factory.org_workstream_task_work` | `in.OrgTaskWorkParams` | `OrgTaskWork` |
| `factory.org_workstream_task_steer` | `in.OrgTaskSteerParams` | `OrgTaskWork` |
| `factory.org_workstream_chats` | `in.OrgWorkstreamParams` | `{chats, others}` |
| `factory.org_workstream_chat_new` | `in.OrgWorkstreamChatNew` | `{id, title, state, created_at, updated_at}` |
| `factory.org_myspace` | `in.OrgConn` | `{workstream, limit_usd, spent_usd, at_limit?}` |
| `factory.org_myspace_totals` | `in.OrgConn` | `{month, limit_usd, people}` |
| `factory.org_myspace_limit_set` | `in.OrgMySpaceLimitParams` | `{month, limit_usd, people}` |
| `factory.org_policy` | `in.OrgConn` | the policy |
| `factory.org_policy_set` | `in.OrgPolicySetParams` | the saved policy |
| `factory.org_grants` | `in.OrgResourceParams` | `{resource, grants}` |
| `factory.org_grant_add` | `in.OrgGrantParams` | the grant |
| `factory.org_grant_preview` | `in.OrgGrantParams` | `{gains, loses, audience_before, audience_after}` |
| `factory.org_grant_remove` | `in.OrgGrantRemoveParams` | `{}` |
| `factory.org_move` | `in.OrgMoveParams` | `{resource, dropped, agent_usable?, agent_words?}` |
| `factory.org_transcript` | `in.OrgTranscriptParams` | `{kind, id, state, seq, entries}` |
| `factory.org_agents` | `in.OrgConn` | `{agents}` |
| `factory.org_agent_new` | `in.OrgAgentNewParams` | the agent |
| `factory.org_agent_set` | `in.OrgAgentSetParams` | the agent |
| `factory.org_agent_delete` | `in.OrgAgentParams` | `{}` |
| `factory.org_agent_roster` | `in.OrgAgentRosterParams` | the agent |
| `factory.org_workstream_agent_list` | `in.OrgWorkstreamParams` | `{agents}` |
| `factory.org_workstream_agent_add` | `in.OrgWorkstreamAgentLink` | `{agents}` |
| `factory.org_workstream_agent_drop` | `in.OrgWorkstreamAgentLink` | `{agents}` |
| `factory.org_library` | `in.OrgConn` | `{pages}` |
| `factory.org_library_page` | `in.OrgLibraryPageParams` | the page |
| `factory.org_library_add` | `in.OrgLibraryAddParams` | the page |
| `factory.org_library_set` | `in.OrgLibrarySetParams` | the page |
| `factory.org_library_delete` | `in.OrgLibraryPageParams` | `{}` |
| `factory.org_workstream_tasks` | `in.OrgTasksParams` | `OrgTasks` |
| `factory.org_workstream_task` | `in.OrgTaskParams` | `json.RawMessage` |
| `factory.org_workstream_task_create` | `in.OrgTaskCreateParams` | `json.RawMessage` |
| `factory.org_workstream_task_update` | `in.OrgTaskUpdateParams` | `json.RawMessage` |
| `factory.org_workstream_task_answer` | `in.OrgTaskAnswerParams` | `json.RawMessage` |
| `factory.org_workstream_task_comment` | `in.OrgTaskCommentParams` | `json.RawMessage` |
| `factory.org_workstream_task_events` | `in.OrgTaskParams` | `OrgTaskEvents` |
| `factory.org_workstream_tasks_start_all` | `in.OrgTasksStartAllParams` | `OrgTasks` |
| `factory.org_work` | `in.OrgConn` | `json.RawMessage` |
| `factory.org_work_set` | `in.OrgWorkSetParams` | `json.RawMessage` |
| `factory.org_workstreams_requestable` | `in.OrgConn` | `json.RawMessage` |
| `factory.org_workstream_request` | `in.OrgTaskRequestParams` | `json.RawMessage` |
| `factory.org_workstream_task_view` | `in.OrgTaskViewParams` | `json.RawMessage` |
| `factory.org_workstream_orders` | `in.OrgOrdersParams` | `json.RawMessage` |
| `factory.org_workstream_order_create` | `in.OrgOrderCreateParams` | `json.RawMessage` |
| `factory.org_workstream_order_update` | `in.OrgOrderUpdateParams` | `json.RawMessage` |
| `factory.org_workstream_order_delete` | `in.OrgOrderParams` | `Empty` |
| `factory.org_workstream_decision_withdraw` | `in.OrgDecisionParams` | `json.RawMessage` |
| `factory.org_workstream_decision_replace` | `in.OrgDecisionParams` | `json.RawMessage` |
| `factory.org_invites` | `in.OrgConn` | `json.RawMessage` |
| `factory.org_invite_create` | `in.OrgInviteNewParams` | `json.RawMessage` |
| `factory.org_invite_resend` | `in.OrgInviteParams` | `json.RawMessage` |
| `factory.org_invite_withdraw` | `in.OrgInviteParams` | `json.RawMessage` |
| `factory.org_welcome` | `in.OrgConn` | `json.RawMessage` |
| `factory.org_welcome_seen` | `in.OrgConn` | `json.RawMessage` |
| `factory.org_person_remove` | `in.OrgPersonParams` | `json.RawMessage` |
| `factory.org_workstream_page_comments` | `in.OrgPageCommentsParams` | `OrgPageComments` |
| `factory.org_workstream_page_comment` | `in.OrgPageCommentParams` | `json.RawMessage` |
| `factory.org_workstream_page_comment_reply` | `in.OrgPageCommentReplyParams` | `json.RawMessage` |
| `factory.org_workstream_page_comment_resolve` | `in.OrgPageCommentResolveParams` | `json.RawMessage` |
| `factory.org_workstream_page_comment_seen` | `in.OrgPageCommentSeenParams` | `json.RawMessage` |
| `factory.org_workstream_page_comments_for_you` | `in.OrgTasksParams` | `OrgPageComments` |
| `factory.org_canvases` | `in.OrgConn` | `OrgCanvasList` |
| `factory.org_canvas_get` | `in.OrgCanvasParams` | `json.RawMessage` |
| `factory.org_canvas_create` | `in.OrgCanvasCreateParams` | `json.RawMessage` |
| `factory.org_canvas_update` | `in.OrgCanvasUpdateParams` | `json.RawMessage` |
| `factory.org_canvas_delete` | `in.OrgCanvasParams` | `Empty` |
| `factory.org_canvas_snapshot` | `in.OrgCanvasParams` | `json.RawMessage` |
| `factory.org_canvas_ops` | `in.OrgCanvasOpsParams` | `OrgCanvasOps` |
| `factory.org_canvas_apply` | `in.OrgCanvasApplyParams` | `OrgCanvasOps` |
| `factory.org_canvas_versions` | `in.OrgCanvasParams` | `OrgCanvasVersions` |
| `factory.org_canvas_version_create` | `in.OrgCanvasVersionParams` | `json.RawMessage` |
| `factory.org_canvas_restore` | `in.OrgCanvasRestoreParams` | `OrgCanvasOps` |
| `factory.org_canvas_marks` | `in.OrgCanvasParams` | `OrgCanvasMarks` |
| `factory.org_canvas_mark_create` | `in.OrgCanvasMarkParams` | `json.RawMessage` |
| `factory.org_canvas_shares` | `in.OrgCanvasSharesParams` | `json.RawMessage` |
| `factory.org_canvas_import` | `in.OrgCanvasImportParams` | `json.RawMessage` |
| `factory.org_canvas_asset_get` | `in.OrgCanvasAssetParams` | `OrgCanvasAssetData` |
| `factory.org_canvas_asset_put` | `in.OrgCanvasAssetPutParams` | `json.RawMessage` |
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
| `factory.agentfield_run` | `in.FactoryAgentFieldRunParams` | `FactoryAgentFieldRun` |
| `factory.agentfield_reasoners` | `in.FactoryAgentFieldReasonersParams` | `FactoryAgentFieldReasoners` |
| `factory.foreman_open` | `in.FactoryRef` | `FactoryForeman` |
| `factory.foreman_fresh` | `in.FactoryRef` | `FactoryForeman` |
| `factory.job_send` | `in.FactoryJobSendParams` | `FactoryJobSent` |
| `factory.job_close` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_control` | `in.FactoryJobControlParams` | `FactoryJobActed` |
| `factory.job_context_set` | `in.FactoryJobContextSetParams` | `FactoryJobActed` |
| `factory.job_create` | `in.FactoryJobCreateParams` | `FactoryJobActed` |
| `factory.job_dismiss` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_drop` | `in.FactoryJobDropParams` | `FactoryJobActed` |
| `factory.job_follow` | `in.FactoryJobFollowParams` | `FactoryJobActed` |
| `factory.job_hold` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_pr_ready` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_open_pr` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_merge` | `in.FactoryJobMergeParams` | `FactoryJobActed` |
| `factory.job_reject` | `in.FactoryJobRejectParams` | `FactoryJobActed` |
| `factory.job_reject_plan` | `in.FactoryJobRejectPlanParams` | `FactoryJobActed` |
| `factory.job_reopen` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_request_changes` | `in.FactoryJobChangesParams` | `FactoryJobActed` |
| `factory.job_answer` | `in.FactoryJobAnswerParams` | `FactoryJobActed` |
| `factory.job_restore` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_resume` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_publish_anyway` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_accept_step` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_retry_publish` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_run_as_new` | `in.FactoryJobRunAsNewParams` | `FactoryJobActed` |
| `factory.job_run_again` | `in.FactoryJobActionParams` | `FactoryJobActed` |
| `factory.job_stop_after_step` | `in.FactoryJobStopAfterStepParams` | `FactoryJobActed` |
| `factory.job_spend_answer` | `in.FactoryJobSpendAnswerParams` | `FactoryJobActed` |
| `factory.job_set_budget` | `in.FactoryJobBudgetParams` | `FactoryJobActed` |
| `factory.job_set_model` | `in.FactoryJobSetModelParams` | `FactoryJobActed` |
| `factory.job_set_priority` | `in.FactoryJobPriorityParams` | `FactoryJobActed` |
| `factory.job_set_repo` | `in.FactoryJobSetRepoParams` | `FactoryJobActed` |
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
| `factory.agentfield_add_trigger` | `in.FactoryAgentFieldAddTriggerParams` | `FactoryAgentFieldAddedTrigger` |
| `factory.agentfield_connect` | `in.FactoryAgentFieldConnectParams` | `FactoryAgentFieldStatus` |
| `factory.agentfield_disconnect` | `in.FactoryRef` | `FactoryAgentFieldStatus` |
| `factory.agentfield_sources` | `in.FactoryRef` | `FactoryAgentFieldSources` |
| `factory.agentfield_status` | `in.FactoryRef` | `FactoryAgentFieldStatus` |
| `factory.agentfield_trigger_add` | `in.FactoryAgentFieldTriggerAddParams` | `FactoryAgentFieldTrigger` |
| `factory.agentfield_triggers` | `in.FactoryRef` | `FactoryAgentFieldTriggers` |
| `factory.ai_search` | `in.FactoryAISearchParams` | `FactoryAISearchStarted` |
| `factory.ai_search_estimate` | `in.FactoryAISearchParams` | `FactoryAISearchEstimate` |
| `factory.ai_search_status` | `in.FactoryAISearchRunRef` | `FactoryAISearchRun` |
| `factory.ai_search_stop` | `in.FactoryAISearchRunRef` | `FactoryAISearchRun` |
| `factory.jira_connect` | `in.FactoryJiraConnectParams` | `FactoryJiraStatus` |
| `factory.jira_disconnect` | `in.FactoryRef` | `FactoryJiraStatus` |
| `factory.jira_status` | `in.FactoryRef` | `FactoryJiraStatus` |
| `factory.library` | `in.FactoryRef` | `FactoryLibrary` |
| `factory.library_preview` | `in.FactoryLibraryPreviewParams` | `FactoryLibraryPreview` |
| `factory.library_set` | `in.FactoryLibrarySetParams` | `FactoryLibrary` |
| `factory.me` | `in.FactoryRef` | `FactoryMe` |
| `factory.people` | `in.FactoryRef` | `People` |
| `factory.policy_get` | `in.FactoryPolicyGetParams` | `Policy` |
| `factory.policy_set` | `in.FactoryPolicySetParams` | `Policy` |
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
| `factory.repo_issues` | `in.FactoryRepoIssuesParams` | `FactoryRepoIssues` |
| `factory.repo_read` | `in.FactoryRepoReadParams` | `FactoryRepoRead` |
| `factory.repo_set` | `in.FactoryRepoSetParams` | `FactoryRepoSetDone` |
| `factory.repos` | `in.FactoryRef` | `FactoryRepos` |
| `factory.repos_refresh` | `in.FactoryRef` | `FactoryRepos` |
| `factory.revoke_sessions` | `in.FactoryAdminParams` | `FactoryAdminDone` |
| `factory.rules` | `in.FactoryRef` | `FactoryRules` |
| `factory.rules_set` | `in.FactoryRulesSetParams` | `FactoryRules` |
| `factory.routes` | `in.FactoryRef` | `FactoryRoutes` |
| `factory.routes_set` | `in.FactoryRoutesSetParams` | `FactoryRoutes` |
| `factory.routes_test` | `in.FactoryRoutesTestParams` | `FactoryRoutesTried` |
| `factory.runner_slots` | `in.FactoryRunnerSlotsParams` | `FactoryRunnerSlotsDone` |
| `factory.runners` | `in.FactoryRef` | `Runners` |
| `factory.scout` | `in.FactoryScoutParams` | `ScoutLook` |
| `factory.session_restore` | `in.FactorySessionRestoreParams` | `FactorySessionRestored` |
| `factory.sessions_store` | `in.FactorySessionsStoreParams` | `Empty` |
| `factory.sessions_take` | `in.Empty` | `FactorySessionsTaken` |
| `factory.setup` | `in.FactoryRef` | `FactorySetup` |
| `factory.setup_back` | `in.FactoryLocalParams` | `FactorySetupView` |
| `factory.setup_clone` | `in.FactorySetupFolderParams` | `FactorySetupView` |
| `factory.setup_connect` | `in.FactorySetupConnectParams` | `FactorySetupView` |
| `factory.setup_continue` | `in.FactoryLocalParams` | `FactorySetupView` |
| `factory.setup_discover` | `in.FactoryLocalParams` | `FactorySetupView` |
| `factory.setup_done` | `in.FactoryLocalParams` | `FactorySetupView` |
| `factory.setup_github_cancel` | `in.FactorySignInFlowParams` | `Empty` |
| `factory.setup_github_poll` | `in.FactorySignInFlowParams` | `FactorySetupGitHubState` |
| `factory.setup_github_start` | `in.FactoryLocalParams` | `FactorySignIn` |
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
| `factory.events_since` | `in.FactoryEventsSinceParams` | `FactoryEvents` |
| `factory.usage_by_model` | `in.FactoryUsageParams` | `FactoryUsage` |
| `factory.spend_by_repo` | `in.FactorySpendByRepoParams` | `FactorySpendByRepo` |
| `factory.spending` | `in.FactoryRef` | `FactorySpending` |
| `factory.spending_set` | `in.FactorySpendingSetParams` | `FactorySpending` |
| `factory.waits_set` | `in.FactoryWaitsSetParams` | `FactoryWaitsSetResult` |

`factory.repos_refresh` in `hello.features` says the engine serves
`factory.repos_refresh {factory}`. It reads `GET /v1/repos?refresh=1` with the
same caller and returns the same `FactoryRepos` shape as `factory.repos`.
The factory waits for an active discovery or starts one if at least 30 seconds
have passed since the last discovery started. During the cooldown it returns
the saved list. Older factories ignore the query and return their ordinary list.
Clients can use `factory.repos` when the engine omits the feature.

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

An engine whose hello carries `org_teams` and `org_workstreams` reads and changes an
organization's teams and workstreams, which live only on its `codeaf factory serve`
server; the engine keeps no copy. Each method takes `connection` (the factory
connection id; `factory` is read when it is absent) and is one request with the
caller's session: `factory.teams` `GET /v1/teams`, `factory.team_create {name}`
`POST /v1/teams`, `factory.team_rename {team, name}` `PATCH /v1/teams/{id}`,
`factory.team_delete {team}` `DELETE /v1/teams/{id}`, `factory.team_member_add` and
`factory.team_member_remove {team, handle}` `PUT`/`DELETE /v1/teams/{id}/members/{handle}`;
`factory.org_workstreams {archived?}` `GET /v1/workstreams` (`?archived=1` when
archived is true, which adds the archived rows; hello word `org_workstreams_archived`), `factory.org_workstream {id}`,
`factory.org_workstream_create` `POST /v1/workstreams`, `factory.org_workstream_update
{id, rev, ...patch}` `PATCH` with `If-Match: rev`, `factory.org_workstream_delete {id}`,
`factory.org_workstream_owner {id, kind, owner}` `PUT .../owner`,
`factory.org_workstream_shares {id, shares?, teams?, owner?, private?, rev?, include_linked}`
`PUT .../shares`,
`factory.org_workstream_share_preview {id, teams?, shares?}` `GET .../shares/preview?teams=&shares=kind:id:role,...`, and
`factory.org_workstream_import {from_local_id, name, ...}` `POST /v1/workstreams/import`.
The answer is the server's, in its own shape (lists are `[]`, never null). Ids are
checked before anything is sent (`bad_params`); the server's refusal keeps its status:
403 `forbidden`, 409 `conflict`, 412 `conflict` with `detail.reason` `stale` (read the
workstream again). A write that may have happened is `uncertain`. `factory.me` also
answers `teams`, the ids of the teams the person is in (`[]` from an older server).

An engine whose hello carries `org_docs` carries an organization's **live documents**:
the Yjs documents two people type into at once, a workstream page
`ws/<workstream>/page/<page>` or the text of a canvas frame
`canvas/<canvas>/frame/<frame>`. The server keeps, per document, an append-only log of
opaque Yjs updates stamped `seq`, `by`, `at`, plus the latest client-made snapshot and
the `seq` it covers; it never parses Yjs. Each method takes `connection` and `doc`:
`factory.org_doc_read {since}` `GET /v1/docs/{doc}?since=` answers `{doc, seq,
snapshot, updates, awareness, can_edit, compact}` (load `snapshot` when it is set, then
apply `updates` in order; `seq` is what to follow from);
`factory.org_doc_send {base_seq, update, seed?}` `POST .../updates` answers the stamp
`{doc, seq, by, at, compact}`; `factory.org_doc_snapshot {seq, state}` `PUT
.../snapshot` answers `{doc, seq, kept}` (`kept` false: a snapshot at or past `seq`
was already kept, not an error); `factory.org_doc_aware {client, state}` `POST
.../awareness` sends this client's y-protocols awareness state (`null`: it left), true
for 30 s unless sent again. `update`, `state` and `snapshot.state` are base64. Seeing the
parent item (`view`) reads, follows and sends awareness; `edit` sends updates and
snapshots; an archived workstream's pages are read-only; a document whose parent you
cannot see is `not_found`; a canvas frame's document follows its canvas (`org_canvases`):
found when the canvas is visible and the frame is in its snapshot, read with `view`, written
with `edit`, and dropped when the frame or the canvas is deleted. Refusals: `conflict` with `detail.reason` `seeded` (a `seed: true` update into a
document that has content: read it instead), `reset` (`base_seq` or a snapshot's `seq`
is past the server's head: the document was reset, read it again) or `snapshot_needed`
(see compaction); `bad_params` before anything is sent past 256 KiB per update, 2 MiB
per snapshot, 4 KiB per awareness state; `busy` (HTTP 429, `Retry-After: 1`) past 30 updates a second (burst
120) or 10 awareness states a second (burst 20) per person per document.

**Compaction.** The client compacts; the server only keeps. Past 500 updates or 512 KiB
since the snapshot, every answer says `compact: true`: an editor posts
`Y.encodeStateAsUpdate` as the snapshot with the `seq` it has applied. Past 5000 updates
or 2 MiB the server refuses new updates with `snapshot_needed` until one arrives. A kept
snapshot drops the updates it covers. **Versions** are E2's: on 30 s idle the client that
wrote last posts the snapshot here and then `POST /v1/workstreams/{id}/pages/{pid}/versions
{body_md, state, reason:"idle"}` there; a restore answers `{body_md, state}` and the
restorer applies it as an ordinary update, so collaborators see it live.

`factory.org_doc_follow {since}` keeps the document's stream (`GET
/v1/docs/{doc}/events?since=`) open with the same session and relays it until
`factory.org_doc_unfollow`, the connection is detached, or its sign-in changes or ends:
`factory.org_doc_update {connection, doc, seq, by, at, update, snapshot}` in `seq` order,
never skipping one (a stream that drops or falls behind is reopened from the last `seq`
relayed and the server replays the rest; a replay that starts behind the snapshot opens
with the snapshot, `snapshot: true`, applied the same way with `Y.applyUpdate`);
`factory.org_doc_awareness {connection, doc, by, client, state, at}` (`state` null: the
client left or lapsed); and `factory.org_doc_closed {connection, doc, reason}`,
`removed` (the page was deleted) or `gone` (no longer visible, or signed out).
Document updates do NOT travel on `GET /v1/events`, whose durable window of 200 events
keystrokes would flush.
An engine whose hello carries `org_workstream_parts` changes one part of an
organization workstream at a time, so two people changing two different parts never
refuse each other. A page's BODY is not one of them (it is a live document); these keep
a page's place, title, kind and saved versions. `factory.org_workstream_page_create
{id, title, kind?, parent?, index?}` `POST .../pages`, `factory.org_workstream_page_update
{id, page, rev, title?, kind?, parent?, index?}` `PATCH .../pages/{page}` with `If-Match`
the PAGE's `rev` (every page carries one), `factory.org_workstream_page_delete {id, page}`
(its pages move up to its parent) — each answers the workstream. Versions:
`factory.org_workstream_page_versions {id, page}` (newest first, without words),
`factory.org_workstream_page_version {id, page, version}` (with `body_md` and `state`),
`factory.org_workstream_page_snapshot {id, page, body_md, state?, reason}` (`reason`
`idle` or `manual`; the same words twice answer the version already kept) and
`factory.org_workstream_page_restore {id, page, version}`, which keeps the old words as
a new version by the caller and answers them so the caller's editor applies them to the
live document. A version the agent kept (`reason` `agent`, `kept` true) carries
`based_on`, the version whose words the agent read and changed (0 when unknown, as on
every version kept before it existed); a client restoring it reads `based_on`'s words
and merges the change from them to the kept words onto the live document, three ways,
so nothing typed since the agent read the page is dropped, and says so in plain words
and keeps both texts where the two changes collide. `factory.org_workstream_plan_get {id}` answers `{plan, rev}` (`null` and
`0` before the first) and `factory.org_workstream_plan_set {id, rev, plan}` replaces it
(`rev` 0 for the first). `factory.org_workstream_wakes {id}`, `_wake_add {id, wake}` and
`_wake_update {id, wake_id, rev, wake}`, and the same three for suggestions
(`_suggestions {id, page?}`, `_suggestion_add`, `_suggestion_update {id, suggestion_id,
rev, suggestion}`), keep the desk's own objects with the server's `id`, `rev`, `by`, `at`,
`updated_by` and `updated_at` on top; an update merges one level deep and `null`
removes a key. `factory.org_workstream_job_add` and `_job_remove {id, job}` keep which
factory jobs (by number) belong to it, one workstream per job; the workstream answers
`jobs`. `factory.org_workstream_unarchive {id}` brings an archived one back. The
workstream also answers `budget_usd` (set with `factory.org_workstream_update
{budget_usd}`, negative clears) and `spend_usd`, the spend posted against it. A
decision a patch leaves out is kept as `superseded`, never deleted. Every author is
the server's.

An engine whose hello carries `org_workstream_files` keeps a workstream's Files, its
shared drive. A file (`OrgWorkstreamFile`) is `{id, ws, rev, folder?, kind, name,
made_by {kind person|afi|chat, id?, name, for?}, from_task?, used_by[], created_at,
changed_at, changed_by?, versions?, open_comments?, person_edited?, page_id?, canvas_id?,
url?, repo?, upload? {size, mime?, blob?}, target? {ws, file}, deleted_at?, deleted_with?,
source?}`; `kind` is `folder`, `document`, `canvas`, `upload`, `link`, `repo` or
`shortcut`. A file POINTS at what it holds: a document is a page of the workstream
(`page_id`; its words are the page's live document, its versions the page's), a canvas
an organization canvas made inside it (`canvas_id`), an upload bytes in the server's blob
store, a shortcut a file in another workstream (`target`), copied nowhere. A shortcut to
a Library page is a `link` whose `url` is the page's own address,
`codeaf://library/<page>` (Copy Link on the page); the desk draws it as "From the Library"
and opens the page in the Library, and the Personal store accepts that one non-web
address for a link. Files never mentions `versions` to a person and never offers a
restore: the desk asks before a delete instead (final spec, Files and the Library).
`factory.org_workstream_files {id, query?, deleted?}` `GET .../files` answers `{files,
deleted?, from?}`: the live files, Recently deleted when `deleted`, and `from {id, name,
files}`, the parent workstream's files, read-only, when the caller can see it. `query`
matches a name, an address or a document's words. The FIRST read of a workstream folds
in everything it kept before Files: each page becomes a document (a page holding pages
becomes a folder too), each source a link, repo or (a `file` source) an upload with no
bytes, and each canvas made inside it a canvas file. It makes no Decisions document: the
decisions are the workstream's kept list. Later reads fold in a page,
source or canvas an older client added through its own route.
`factory.org_workstream_file_create {id, kind, name?, folder?, from_task?, made_by?,
body_md?, canvas_id?, url?, repo?, data?, mime?, target?}` `POST .../files` (a document
also makes its page; `data` is an upload's bytes, base64, at most 25 MiB; `made_by` is
honoured only for kind `afi` or `chat`, stamped `for` the caller).
`factory.org_workstream_file_update {id, file, rev, name?, from_task?, used_by?,
edited?}` `PATCH .../files/{file}` with `If-Match` the FILE's `rev`; a document's rename
renames its page; `edited` `person` sets `person_edited` (an agent edits directly only a
file it made that no person has edited — every other change it makes is a suggestion).
`factory.org_workstream_file_move {id, files, folder, to?}` moves all or none (`folder` ""
is the top; a folder never moves into itself). With `to` (another workstream's id) the
files leave `id`'s Files for `to`'s, each still itself: same id, maker and upload bytes,
and a document takes its page, its versions and its live text. It asks what a task's move
asks: edit on both, and the caller looks after `id` or made every file (a chat's or Afi's
file counts as made for the person it was made for); else 403. A folder, an id `to`
already keeps, or a `folder` that is not `to`'s is 422. Both workstreams publish
`workstream.changed` with `part` `files` (`deleted` on `id`'s side). Bring along uses it. `factory.org_workstream_file_delete {id,
file}` moves it to Recently deleted for 30 days, a folder with everything under it
(`deleted_with`); `factory.org_workstream_file_restore {id, file}` brings it back, to the
top when its folder is gone. `factory.org_workstream_file_content {id, file}` answers an
upload's bytes `{data, type}`. Every change publishes `workstream.changed` with `part`
`files` and the file's id; a change with `edited` also carries `flagged`, the `used_by`
tasks it flags.

With `org_roles` in hello, a workstream's access is roles. A workstream row carries
`shares` `[{kind:"person"|"team", id, role}]` with `role` one of `view`, `comment`,
`edit`, `full` (each grants the ones before it), `private` (only its person owner
reaches it) and `my_role`, the caller's role. The owner holds `full`. `view` reads,
`edit` changes its words and approves its go-aheads, `full` archives it and changes
its owner, shares or privacy. The operator holds `view` on every workstream that is
not private and may change its owner or shares. For someone else's private
workstream the operator gets a name-only row (`my_role` `""`, id, name, stage, owner,
`private`, `updated_at`, zero counts, no shares), and `factory.org_workstream` on it
answers `not_found`. `shared_with` is still answered: the team ids among `shares`.
`factory.org_workstream_shares` lands in one save on the server: `shares` (the whole
list) when sent, else `teams` (the older call: each team as `edit`, person shares
kept); `owner` when sent; `private` when sent (`true` needs a person owner and takes
it from everyone it was shared with; beside a non-empty `shares` it is a 422); a non-empty `shares` on a private workstream
makes it not private; `rev` when sent must be the rev read, else `conflict`/`stale`.
`factory.org_workstream_create` takes `private`. The server signs every decision that
is new or changed (`by` is the caller, `at` is now) and ignores the client's values,
and an import's `created_by` is always the caller. An import's `links` may name the
Personal ids of workstreams the same person already moved in; they are linked by
their organization ids, and a later import is linked from earlier ones that named it.
When the operator removes a person, their workstreams go to `?heir=<handle>` or the
operator, and they leave every share; each change is a `workstream.changed` event.

**A workstream's structure (Workstreams v5, SPEC §2.1).** An engine whose hello carries
`org_workstream_nesting` and `org_workstream_rules` carries these fields; an older server
answers a workstream without them.

- `parent` (summary and whole): the top-level workstream it sits inside. NESTING IS ONE
  LEVEL: `factory.org_workstream_create {parent}` or `factory.org_workstream_update
  {parent}` naming a workstream that has a parent, or moving one that has children, is a
  422. A child ALWAYS HAS ITS PARENT'S PEOPLE: the server copies the parent's owner,
  shares and privacy onto every child in the save that changed them, so
  `factory.org_workstream_owner` and `factory.org_workstream_shares` on a child are a
  422, and `create {parent}` beside `owner` or `private` is a 422. Creating inside needs
  edit on the parent.
- `steward {id, name}` and `my_steward` (summary): who looks after it, its maker until
  handed on with `update {steward: handle}`. ONLY THE STEWARD sends `rules` or `steward`
  (else 403 `forbidden`); the steward also governs its people beside its owner, owning
  team and the operator.
- `people` (whole): owner and shares as one list, `{kind: person|team, id, name, role,
  members?, steward?, added_at?}`, owner first. A team's `members` are read now, so
  adding a team adds everyone in it, now and later.
- A person's `name` here, in the whole answer's `steward` and in `rules`' `default_owner`
  and `fallback` is the name they joined with ("Santosh Kumar"), or their handle when the
  organization knows none. A summary's `steward.name` stays the handle.
- `rules` (whole, filled with defaults): `{default_owner: {kind: person|everyone, id?,
  name?}, fallback?: {id, name}, start_without_asking_usd, keeps_running: {factory,
  orders: [order id]}}`. The default is the steward owning incoming work and `0`. A child
  starts with a copy of its parent's rules (its maker as `default_owner`) unless `create`
  sends `rules`, and changes its own copy. `update {rules}` replaces them whole and
  publishes `workstream.changed` with `parts: ["rules"]`.
- `start_without_asking_usd` IS THE WORKSTREAM'S GO-AHEAD: the server keeps one
  allowance `start-without-asking` in the policy under the workstream's id, covering its
  jobs with `per_job_usd` set to the amount (every job it starts is held to it) and
  `cap_usd` the amount times the jobs. It is written again whenever the rule or the
  workstream's jobs change; `0` takes it away (never start without asking). Allowances a
  person made keep their own ids and are not touched. An allowance's `per_job_usd` is
  `0..cap_usd`.
- `factory.org_workstream_archive_keep {id}` `GET .../archive/keep` answers `{parent?,
  keep: [{kind: page|source, id, title}]}`: what a child could leave in its parent when it
  is archived (nothing for a top-level one).
- `factory.org_workstream_fold {id}` `POST .../fold` turns a child back into a heading in
  its parent: its jobs move into the parent and it is archived. It answers `{parent,
  heading}`, the heading being the child's name; tasks and files fold by that heading in
  their own lanes. A top-level one is a 422; it needs edit on both.
- Removing a person (from shares, or a team leaving) hands their open work in the
  workstream and its children to `rules.fallback`, else the steward (the tasks lane's
  hook).

**Standing orders scoped to a workstream.** An order carries `workstream` (absent: none),
and `orders.change {id, edits: {workstream}}` sets it, `""` clearing it. It changes
nothing about when or how the order runs; the desk lands a scoped order's results in
that workstream's task list as a group named after the order, and its People and rules
page switches it through `rules.keeps_running.orders`.

An engine whose hello carries `workstream_orders` makes one from that page.
`orders.create {title?, when, does, workstream, budget_per_run_usd?}` saves a work order
as `proposed` (nothing runs before `orders.ratify`), asking first, its results said
`not_here`, running from the person's home; an empty title takes the brief's first line.
`when` is the dialect `orders.change` reads (`every 0 9 * * 1`, `every 1d`, `at …`, `in
2h`), read in this machine's zone; one that cannot be read is `bad_params` and nothing
is kept. `orders.preview {when}` answers `{when, when_exact, next_runs}` from the same
reading without keeping anything. An order also carries `next_runs` (at most three)
whenever it repeats and may still run, switched on or not, and a run that is over
carries `outcome`, its own word (`done`, `your-call`, `incomplete`, `stopped`,
`skipped`, `quiet`, `unchecked`), beside `result`, which folds several into
`incomplete`. The desk that holds the order makes each run a task in the workstream
(`came_from {kind: order, order_id, name, run}`, `group {kind: order}`): an order's
workstream is the desk's own address for it, a Personal id or `org:<connection>/<id>`,
opaque to the engine.

An engine whose hello carries `org_agent_runner` reads and keeps where an organization
workstream's agent runs. `factory.org_agent_runner` `GET /v1/settings` answers
`{agent_runner, agent_runners}`: the operator's choice, `driver` (on the laptop of the
person driving the workstream, their engine and model key) or `server` (on the
organization's server with keys the operator set there), and the choices that server
offers; an older server reads as `driver` of `[driver]`. A server offers `server` once it runs
agents (`codeaf factory serve` does) AND the operator has given it a model key; choosing
`server` lets go of every driver's hand. `factory.org_agent_runner_set {agent_runner}` `PATCH /v1/settings` is the
operator's alone (`forbidden` otherwise); a choice not offered is refused by the server.
While it is `driver`, one person at a time drives a workstream's agent through its
driver hand: `factory.org_workstream_drive {id}` `PUT /v1/workstreams/{id}/driver` takes
it, or renews it when it is already yours, and answers `{handle, since, until, ttl_s}`;
the hand lapses `ttl_s` (90) seconds after its last renewal, so a desk renews it while
the window is open. Someone else's live hand is `conflict` with `detail.reason`
`driving` and `detail.by` `@handle`. `factory.org_workstream_let_go {id}` `DELETE`
lets go; its owner and the operator may also take it from someone else. A workstream
answers `driver` while someone holds it. `factory.org_workstream_spend {id, usd, key,
job?, chat?}` `POST /v1/workstreams/{id}/spend` posts what the driver's agent spent,
with `key` sent as `Idempotency-Key` so a retry counts once, and answers `{spend_usd,
budget_usd}`; the server names the spender from the session.

An engine whose hello carries `org_agent_server` speaks to the organization's own
model keys and to a workstream's agent on the server. `factory.org_keys` `GET
/v1/org/keys`, `factory.org_key_set {provider, key}` `PUT /v1/org/keys/{provider}` and
`factory.org_key_delete {provider}` `DELETE` are the operator's alone; the only provider
is `openrouter`, and a key is answered back only as `{provider, last4, set_at,
set_by}`, never whole. In `server` mode every workstream has one agent on the server,
and each person who can edit it has their OWN conversation with it, which nobody else
reads (the agent's memory is per person too); it is run with that key as an ordinary engine conversation whose tools are
`workstream_pages` (read the workstream and one page, write a page, make a page,
suggest a decision) and the workstream tools `ws_tasks`, `ws_task_create`,
`ws_task_update`, `ws_task_comment`, `ws_files`, `ws_file_read`, `ws_file_create`,
`ws_file_suggest`, `ws_file_comment`; no shell, repository or machine. Each `ws_*` call is answered by the
same route a person's client uses (`/tasks`, `/tasks/{tid}` with `If-Match`,
`/tasks/{tid}/comment`, `/files`), as the asker, so the routes' rules hold: what Afi
adds to a thread is `by {kind:"afi", name:"Afi"}`, a task it makes is `done_by
{kind:"afi"}`, `came_from {kind:"chat", chat_id:"afi", name:"Afi"}` and owned by the
asker unless it names another person who can edit or Everyone, and it changes only
tasks whose `done_by` is `afi` or `chat`; a file it makes is `made_by {kind:"afi",
name:"Afi", for:<asker>}`. It writes a page directly only when its file is
`made_by` an agent and not `person_edited`, first keeping the words before as a
version; any other page, and any page with a live document, gets its text as a page
version with `reason` `agent` (Kept · not applied), which is also what `ws_file_suggest`
keeps. `ws_file_read` on a document also answers `comments` `[{thread, quote, comments: [{by,
text, at}], resolved?}]`, its passage threads, open ones first (at most 100);
`ws_file_comment {file, quote | thread, text}` answers a thread through
`/comments/{cid}/reply` or starts one through `/pages/{pid}/comments` on words the page's
Markdown says exactly once, with `block` `[]` and `block_text` the quote (the desk finds
them on the live page), as Afi; neither marks anything read for the asker, and neither
changes the page. `ws_file_read {file, from?}` on an upload whose bytes are UTF-8 text answers
them as `text` (at most 100 KiB from byte `from`, with `more_from` and `said` when more
follows); any other upload answers `said`, that it has no words to read. The desk's
Personal door answers the same shape. `factory.org_workstream_agent
{id, since?}` `GET /v1/workstreams/{id}/agent?since=` (`view`) answers `{id, state,
turn?, for?, since?, entries, seq, can_talk, cannot?}`: `state` is `idle`, `working`
(`for` is whose message) or `stopping` (`for` is who stopped it); `entries` are the
lines after `since` (at most 200), each `{seq, at, kind, by?, text, tool?, failed?,
turn}` with `kind` `person`, `agent`, `step` or `note`, all of them the caller's own
conversation; `private` is `true` from a server that keeps each person's conversation to
them (an older server, which kept one conversation everyone read, leaves it out, and the
desk then must not say "Private to you"); `cannot` is the one sentence saying why an
editor cannot talk now (another runner mode, no key, archived, budget spent), absent for
a viewer. `factory.org_workstream_agent_send {id, text, tz?}` `POST
.../agent/messages` (`edit`, 1–16384 bytes) answers 202 with the agent working; `tz`
is the sender's IANA time zone (`America/Toronto`), and every turn tells the agent the
date and time in it, or in the server's own zone when it is absent or unknown; while
it still answers the caller's previous message the refusal is `conflict` with
`detail.reason` `busy` (another person's conversation never makes it busy).
`factory.org_workstream_agent_stop {id}` `POST .../agent/stop` (`edit`) stops the
caller's own answer. The agent writes as the person who asked (their rights are checked
on every write); a decision is added `proposed`. What a
turn spent is posted to the workstream's spend under the asker with `chat` `agent`.
The server's `agent.entry {id, person, entry}` and `agent.state {id, person, state, ...}`
reach only `person`, while they can see the workstream, and are relayed as `factory.org_workstream_agent
{connection, id, chat?, entry?, state?}`.

An engine whose hello carries `org_myspace` serves each person's own space inside an
organization ("My space") and a person's own chats on the server. `factory.org_myspace`
`GET /v1/myspace` answers the caller's space, made on first read: `{workstream,
limit_usd, spent_usd, at_limit?}`, where `workstream` is a workstream with `space: true`,
owned by the caller and private, read further with the workstream methods (files,
canvases, agent). A space is never in `factory.org_workstreams` or the requestable list;
it refuses tasks, shares, a new owner, a parent, a rename, archive and delete
(`invalid`), and a workstream cannot be made inside it. `spent_usd` is what it has spent
this calendar month (UTC); at `limit_usd` its chats answer `can_talk: false` with
`cannot` "Paused: this month's limit for your space is reached. …". The operator's
`factory.org_myspace_totals` `GET /v1/myspace/totals` answers `{month, limit_usd,
people: [{person, name?, spent_usd, items, at_limit?}]}`, totals only: nothing in it
names a space's things. `factory.org_myspace_limit_set {limit_usd}` `PUT
/v1/myspace/limit` (operator, 0–10000; default 20) answers the totals. When the operator
removes a person, their space goes to the heir as one private workstream named
"<handle>'s space". `factory.org_workstream_chats {id}` `GET .../chats` lists the
caller's own chats in a workstream (`{chats: [{id, title, state, created_at,
updated_at}]}`, newest first) and, metadata only, the chats of others that the
caller may view (`others: [{id, workstream, title, owner, owner_name?, state,
last_activity}]`, newest activity first; never a line of what is said; always empty
in a space). A chat is private by default (the policy's default for chats), and a
private chat is listed only to its owner and to the people it is shared with, so
`others` is empty until someone shares a chat with the caller; v2 ruling C10 once
listed every chat of a shared workstream here whatever its own privacy, and no longer
does. The list is decided by the same decider as `factory.org_grants`. `id` is the
`chat_id` on the tasks and files that chat made. `factory.org_workstream_chat_new {id, title?}` `POST
.../chats` (`edit`) makes one (`ch_` and 26 letters or digits; an empty title is the
first message's first line). `factory.org_workstream_agent`, `_send` and `_stop` take
`chat` to read, talk in or stop one; a chat that is not the caller's is `not_found`. A
chat, and every turn in a space, runs on the server whatever `agent_runner` says. Since
`org_myspace`, the operator is no longer told a private workstream or canvas exists:
the `name_only` row is gone.

A server with hosted chats on (`CODEAF_HOSTED_URL`, deploy/org/README.md) also posts what
every hosted chat's model calls cost to the caller's space: one spend row per call, `chat`
`hosted:<chat id>` (`hosted:<chat id>:own` when the call used the person's own key), so
`spent_usd` and `at_limit` above count them and a person at `limit_usd` has their hosted
chat refused with "You have reached your monthly limit of $L, so this chat is paused. The
organization's operator can raise it in Settings." (HTTP 402 from the model door, whose every
error carries `"door":"organization"` in its envelope so a client shows the sentence as written
rather than as a payment failure; a chat's own cap, `CODEAF_HOSTED_SESSION_CAP_USD`, default 5, refuses with "This chat has reached its
spending limit of $C, so it is paused. Start a new chat to carry on."). These are HTTP
routes of the organization server with no engine method yet, answered only to a signed-in
person and only about them: `GET /v1/hosted/keys` answers `{keys: [{provider, last4, at,
source}]}` (`provider` is `openrouter` or `github`, `source` is `own` or `sign-in`, never
the key), `PUT /v1/hosted/keys/{provider}` with `{key}` answers 204 (409 "This server cannot
keep personal keys safe yet, so it did not store yours. Its operator can set that up." when
the server has no `CODEAF_SERVER_KEY`), `DELETE /v1/hosted/keys/{provider}` answers 204, and
`GET /v1/hosted/audit` answers `{entries: [{at, person, chat, kind: "git", host, repo,
service}]}`, the caller's own latest git operations. A chat reaches git through
`/v1/hosted/git/<host>/<owner>/<repo>.git/...`, authenticated by the chat's own model access
as the password; the server adds the person's GitHub access upstream.

An engine whose hello carries `org_access` serves an organization's access policy and the
grants on its things. `factory.org_policy` `GET /v1/org/policy` answers the policy to any
member: `{rev, roles: [{name, verbs}], owner_role, org_role, defaults: {<kind>: {visibility}},
sharing: {who}, operator: {role, sees_private, may_govern}, agents: {may_create, may_share,
may_grant, default_scope, scopes?}, chats?: {move_by?, approvals_by?, wake_by?}}` (`chats` is present only when set, and
each field only when set: `move_by` is `owner`, the default, or `manage`, who may move a chat; `approvals_by` is `owner`, the
default, or `answer`, who approves a tool call in a shared hosted chat; `wake_by` is `view`, the default, `run` or `owner`, who
may start a hosted chat that is not running), and the default policy when none was ever saved, which
is how the server behaved before policies existed. A role is a named bundle of verbs from a
closed list (`view`, `comment`, `edit`, `run`, `steer`, `answer`, `share`, `manage`), every
role holds `view`, and the four names `view`, `comment`, `edit` and `full` are kept. Only
the operator saves one: `factory.org_policy_set {policy}` `PUT /v1/org/policy` takes the
document as the last read answered it, `rev` included, and answers the saved one (a stale `rev` is
`conflict` with the reason `stale`; a document the rules refuse is refused with the server's words). A change to who may
see anything reaches everyone it concerns as that thing's own change event, to the people who
could see it before and the people who can now, and as `org.policy_changed {rev, by?}` to
every member, who then read the policy again. `factory.org_grants {kind, id}` `GET
/v1/resources/{kind}/{id}/grants` lists, for anyone who may view the resource, the owner, each
share on its workstream or canvas (`source: "share"`) and each stored grant (`source: "grant"`,
or `"policy"` when the organization's policy wrote it): `{resource, grants: [{id, subject:
{kind, id}, role, granted_by?, created_at, expires_at?, note?, source, live}]}`; a resource the
caller may not view is `not_found`. `factory.org_grant_add {kind, id, subject, role,
expires_at?, note?}` `POST .../grants` needs the share verb, and the role may hold only verbs the
caller holds on the thing: a person's own verbs, or for a server-run agent its person's verbs
cut to what the policy lets the agent do. A grant to an agent is refused with words: an agent's reach is the
policy's. `factory.org_grant_preview` takes the same params and answers `{gains, loses,
audience_before, audience_after}` without writing anything. `factory.org_grant_remove {kind, id,
grant}` `DELETE .../grants/{grant}` removes a stored grant; a share on a workstream or canvas keeps
its own methods and answers `conflict` naming them.

| Request or event | Server behavior |
| --- | --- |
| `factory.org_move {kind, id, to}` → `POST /v1/resources/{kind}/{id}/move` | Moves a canvas or a chat. It needs `manage` on the item and `edit` on the destination. An unseen destination is `not_found`. Spaces, workstreams and sessions stay where they are and return `conflict`. |
| Canvas move | Answers `{resource, dropped}`. The canvas's own grants appear in `dropped` when the destination is private. |
| Chat move | Only the chat's owner can move it. A chat that is answering or an archived destination returns `conflict`. The chat retains its owner and moves its conversation folder. Moving into My space drops its own grants and leaves it only in the owner's chat list. A shared workstream's chat list still shows its title, owner, state and last activity. |
| Chat move answer | Adds `agent_usable` to `{resource, dropped}` and includes `agent_words` when the named agent is unavailable in the destination. The conversation stays readable and future spending goes to the destination. |
| `factory.org_transcript {kind, id, since?}` → `GET /v1/resources/{kind}/{id}/transcript` | Reads a registered chat or session through the current view permission. Answers `{kind, id, state, seq, entries}`. With `since`, entries begin after that sequence number. The transcript offers read access only. A missing or inaccessible resource returns `not_found`. |
| `resource.changed {kind, id}` | A chat move or a lapse of a grant on a registered resource reaches the combined old and new audience. Canvas and workstream grant lapses emit `canvas.changed` and `workstream.changed`. |

An engine whose hello carries `org_bots` can manage bot identities on an organization
server. The server returns each secret in its minting answer once. Lists show token
metadata and a short hint.

| method | HTTP request | Server behavior |
| --- | --- | --- |
| `factory.org_bots` | `GET /v1/bots` | Lists the caller's bots and their token metadata. The operator sees all bots. |
| `factory.org_bot_new {name, scope?}` | `POST /v1/bots` | Creates a bot and returns `{bot, token}`. The optional scope names a current organization role. |
| `factory.org_bot_token_new {bot, label?, expires_at?}` | `POST /v1/bots/{bot}/tokens` | The bot's creator mints a token. The response includes `token_id` and `token`. |
| `factory.org_bot_token_revoke {bot, token}` | `DELETE /v1/bots/{bot}/tokens/{token}` | The creator or operator revokes a token by ID. |
| `factory.org_bot_remove {bot}` | `DELETE /v1/bots/{bot}` | The creator or operator removes a bot and ends its tokens. |

Who an organization server takes a request to be: a person's token or sign-in, a
bot's token, or, only where `loopback_trust` is on (solo by default, off on a team
server), a request from the server's own computer. A request carrying a forwarding
header is never from the server's own computer unless a listed proxy names a
loopback client in `X-Forwarded-For`. Registering a runner (`POST /v1/workers/attach`
and `POST /v1/runners/attach`) follows the same setting: with it off, a runner
presents the host's access token or a signed-in person's session even from the
server's own computer, and names its handle unless its session does; with it on,
nothing changes. A bot token is refused there.

An engine whose hello carries `org_agents` keeps an organization's agents: a name and
instructions one person made in their space. `factory.org_agents` `GET /v1/agents`
answers `{agents: [{id, name, instructions?, owner, owner_name?, roster?, team?, mine?,
workstreams?, created_at, updated_at}]}`: the caller's own, the organization's list
(`roster: "org"`), and for the operator the ones waiting on them (`roster: "asked"`). An
agent its person keeps to themselves is never in anyone else's answer, the operator's
included; the totals count it among the space's `items`. `factory.org_agent_new {name,
instructions, team?}` (`ag_` and 26 letters or digits; name 1–60 characters and never
"Afi", instructions 1–8000 bytes, at most 100 per person), `_set {agent, name?,
instructions?, team?}` and `_delete {agent}` are its person's only. `factory.org_agent_roster
{agent, on}` is its person asking for the organization's list (`asked`; the operator's
own goes straight to `org`) or leaving it, and the operator approving or declining an
`asked` one. `factory.org_workstream_agent_list {id}` is People and rules › Agents, the
list a task's doer is picked from: `{agents: [{id, name, kind: "agent", from:
"space"|"org", owner, owner_name?, team?, by, at}]}`. `factory.org_workstream_agent_add
{id, agent}` (`edit`) adds the caller's own (Share, `from: "space"`) or one on the
organization's list (`from: "org"`) and `_drop` takes one off (`edit`, or its person); a
space takes none. Off the organization's list, an agent leaves the workstreams others
added it to; deleted, it leaves them all. `factory.org_workstream_chat_new` takes `agent`
to talk to one instead of Afi: in a space one's own or one on the list, in a workstream
one it lists. Its chats carry `agent: {id, name}`, and a message to one whose agent is
no longer usable there is refused (`conflict`). When the operator removes a person,
their agents go to the heir.

Starting an organization (hello `org_setup`, v2 designs J2 and J3). `factory.org_profile`
`GET /v1/org` answers `{name, description?, set_up, by_hand?, relay?}` to anyone signed in (`relay` is the address of the relay the organization runs for its members' computers, when the operator named one);
`factory.org_profile_set {name?, description?}` `PUT /v1/org` is the operator's "Start your
organization" (a name of 1 to 40 characters, a line of at most 200 that every Afi on the
server is told). `factory.org_setup {since?}` `GET /v1/org/setup` is the operator's own
conversation with Afi about the set-up, `{agent, card?, made, set_up}`, where `card` is
what Afi's `org_propose_setup` proposed (`{workstreams:[{name, purpose?, people?,
inside?:[{name, purpose?}]}], teams?:[{name, people?}], invites?:[{name, email?, team?}]}`);
`factory.org_setup_send {text, tz?}` and `factory.org_setup_stop` work as the workstream
agent's do, and its lines are relayed as `factory.org_workstream_agent` with id `org-setup`.
`factory.org_setup_apply {card}` `POST /v1/org/setup/apply` makes the card as the person left
it in one order (teams, workstreams, the workstreams inside them, invites) and answers
`{made:[{kind, name, id, parent?, url?}], failed?, done}`; a part that fails is named in
`failed`, and the same call again makes only what is left. `factory.org_setup_by_hand`
marks the organization set up with nothing made.

**Afi's task cards** (hello `org_agent_cards`). A desk that draws them sends `cards: true`
on `factory.org_workstream_agent_send`; only that turn's agent carries `ws_task_propose`
(a task's own run never does). A proposal is a line of `kind` `cards` in the caller's own
conversation, its `text` the plain words ("Proposed 2 tasks: …") and `cards {group?, tasks:
[{title, request, done_by: afi|person, person?, owner?, due?, waits_on?, checklist?}]}`, 1 to 8 cards,
`person` and `owner` handles (absent is the caller), `waits_on` the 1-based numbers of
earlier cards; a `group` is a plan and needs 5 or more. Nothing is written until
`factory.org_workstream_agent_cards_add {id, seq, group?, tasks}` `POST
.../agent/cards/{seq}` (`edit`; only the conversation's own person): the ticked cards, as
changed, are made as tasks in order as the caller (`came_from` Afi), five or more sharing
one group that waits for Start, and a `note` line `{took: seq, tasks: [ids]}` says "Added
N tasks · <names> is told". A cards line is added once: again is `conflict`; one no longer
kept is `not_found`. If some tasks are refused after others were made, the note says
"Added k of N tasks; the rest were not added: …" with `failed`, and the line is taken.

An engine whose hello carries `org_task_work` reads and steers a task's Working
conversation: what Afi says and does while it works a task on the organization's
server (OWNER-DECISIONS 2026-11-17 §8). Unlike Afi's pane, it is shared: everyone who
can see the workstream reads it. `factory.org_workstream_task_work {ws, id, since?}`
`GET /v1/workstreams/{ws}/tasks/{id}/work?since=` (`view`) answers `{id, task, state,
turn?, for?, since?, entries, seq, can_steer, steerer?, can_revise?, usd?}`: `state` is `working` while a
run goes (`for` is who started it) and `idle` otherwise; `entries` are the lines after
`since` (at most 200) in the pane's shape, a steer being a `person` line with `by`, and
the run's first line a `note` "Started by <handle>"; `can_steer` is whether the caller may
steer now, and `steerer` whether they could while a run goes (they own the task, or
started the run, or the task is everyone's and they can edit); `can_revise` is whether
their words would start a revision run now (a done Afi task with nothing working), and
`usd` what every run of the task has spent.
`factory.org_workstream_task_steer {ws, id, text}` `POST .../work/messages` (1–16384
bytes) answers 202 with the conversation; the words reach the run at its next step, or
start a turn of the run's own when its turn has just ended. On a done Afi task with no
run going they start a revision run and the task stays done. Otherwise, with no run
going, the refusal is `conflict` with `detail.reason` `idle`; from anyone else it is
`forbidden`. A person's comment that asks for a change on an Afi task that is running or
done gets Afi's offer "Revise with this" under it, and taking that offer (`offer` state
`taken`) hands the comment's words to the run, or starts a revision.
The server's `task.work {id, task, entry?, state?}` reaches everyone who can see the
workstream and is relayed as `factory.org_workstream_task_work {connection, id, task,
entry?, state?}`.

An engine whose hello carries `org_library` keeps an organization's Library: pages its
people added on purpose, which everyone in the organization reads. `factory.org_library`
`GET /v1/library` answers `{pages: [{id, title, by, by_name?, from_workstream?,
from_name?, mine?, changeable?, created_at, updated_at}]}`, newest change first and
without words; `factory.org_library_page {page}` adds `body`. `from_workstream` and
`from_name` are said only to a caller who may see that workstream (`from_name` is "My
space" for the caller's own). `factory.org_library_add {title, body, from_workstream?,
from_file?}` (`lb_` and 26 letters or digits; title 1–200 characters, body 1–64000
bytes of text, at most 500 pages) is Add to Library: nothing else adds a page, and a
`from_workstream` must be one the caller can see. `_set {page, title?, body?}` and
`_delete {page}` are the person who added it, or the operator (`changeable`). Every
chat the server runs carries the read-only `org_library` tool beside its others.

An engine whose hello carries `org_workstream_tasks` reads and changes an organization
workstream's tasks (Workstreams v5, SPEC §2.2). Every param names the workstream `ws`
and the task `id`, the same words the desk's Tauri `workstream_task_*` commands take,
and a task and an event are the server's shapes passed through whole: a task is `{id,
ws, rev, title, request, owner, done_by, came_from, state, state_reason?, step?,
waiting_on, due?, group?, results, check?, ask?, cost_usd?, created_by, created_at,
updated_at, done_at?, answered_by?, handed_by?, for_you?, stopped_by?, run?, queue?,
title_by?, request_by?}`, `title_by` and `request_by` being who last wrote each (`person`,
`afi`, …; server-written only, absent on older tasks), `state` one of `up_next`, `running`, `needs`,
`done`, `failed`, `interrupted`, `stopped` (Stopped by you, to `stopped_by`; Stopped by
<name> to everyone else), `no_report` (Didn't report back); an event is `{id, task, at, by, kind, text, steps?, duration_s?, offer?,
undo_until?, option?, mentions?}`, `option` being the chosen answer's id on an `answered` event; a
standing order's task names its run in `came_from.run`. A person on a task (`owner`, `done_by`, `came_from` and
`created_by` of kind `person`) is named, in every answer that carries the task, by the
name they joined with ("Abir Das"), or their handle when the organization knows none;
a task made with `owner: {kind: person, id}` alone reads back named. `factory.org_workstream_tasks {ws}` `GET /v1/workstreams/{id}/tasks`
(`view`) answers `{tasks}`: the kept tasks and every Factory job of the workstream (its
membership, else its go-ahead group) as a task with id `job:<n>`, `done_by.kind`
`factory`, its state from the job's, its Ask the job's open question and `step` "Step
4 of 4, Review"; jobs are never copied, and what a person sets on one (owner, waits,
due, heading) is kept as an overlay under its id. `factory.org_workstream_task {ws,
id}` reads one; `_task_events {ws, id, seen?}` answers `{events}`, oldest first, and
with `seen: true` (`GET .../events?seen=1`, sent only when a person opens the task on a
screen, never by an agent) marks the thread read for the caller. `handed_by` (a
PersonRef) is who last handed the task to its owner, absent when the owner took it by
their own hand. `for_you` is the reader's own and never kept: `{comments, mentioned?,
last}`, the unread `comment` lines of the last fourteen days on a task the reader owns
or that name the reader with @, not their own, and the newest of them as `last`.
`_task_create {ws, title, request?, owner?, done_by, came_from?, waiting_on?, due?,
group?, state?, words_by?}` `POST` (`edit`) answers the task, 201; `words_by: "afi"` says
the desk's Afi wrote the title and request through the person's token, and only ever lowers
trust in them (the server agent's web_fetch follows an address only in words a person wrote);
`"person"` or leaving it out changes nothing, and `_task_update` and `_workstream_request`
take it the same way; whoever asked owns it, work
from the Factory or a standing order goes to the workstream's default owner, and a
group named without an id joins that workstream's group of the same name and kind.
`_task_update {ws, id, rev, ...change}` `PATCH If-Match: rev` (`edit`; rev 0 for a
Factory task's first overlay) takes any of `title`, `request`, `owner` (hand on),
`waiting_on` (same workstream, never itself, never a circle: 422), `due`, `group`
(null clears), `state`, `state_reason`, `results`, `check`, `ask` (null clears) and
`move_to` (another workstream, edit on both; the thread moves with it). Only the owner
ticks a person task (`state` `done`). Stopping (`state` `stopped`) and `move_to` are the
task owner's or whoever looks after the workstream's (its steward, or full access);
anyone else gets 403 "only the task's owner or whoever looks after this workstream can
stop or move it". TASKS START BY THEMSELVES WHEN THEIR WAITS CLEAR (hello
`org_task_starts`; owner decisions 2026-11-17, 1): when a tick or a run's `done` finishes
the last thing an Afi task waited on, and the task starts by itself, the server starts Afi
on it inside that same write, with the rights of whoever finished the work, and its thread
gains "Started: <title> is done". Whether it starts by itself is, in order: its own `starts`
(`when_ready` or `by_hand`; set with `_task_update`'s `starts` by its owner or whoever looks
after the workstream, 403 otherwise, `""` back to the default, and the thread reads "<name>
set it to “Starts when ready”"); a plan (a `batch` of five or more) nobody has started waits
for its Start, and a started plan is consent for all of it; else the owner's "Start tasks
when what they wait on is done", on unless they turned it off. Every Afi and chat task reads
back `starts_when_ready`, what that resolves to, filled when read and never kept.
`factory.org_work {}` `GET /v1/me/work` and `factory.org_work_set {start_when_ready}` `PUT
/v1/me/work` read and write the signed-in person's own default (`{start_when_ready}`). Any
other release starts nothing: a chat's task (woken on its person's machine), a task Afi
cannot take now (no model key, already being worked on) and a task that does not start by
itself stay `up_next`, unchanged, and the thread gains "Ready to start: <title> is done";
there, `running` is set only by whoever wakes the doer. On the server that is a
person's own `state` `running` on an Afi task: the server starts Afi on it in the
task's own run (the caller's rights, nobody's conversation) inside the same write, and
the task says `running` only once Afi took it; a server that cannot run Afi or a task
already being worked on is 409 with nothing changed. A person's `done`, `stopped` or
`up_next` ends a run still going on it.
An engine whose hello also carries `org_workstream_task_start` serves this Start; a desk
offers Start and Start all on an organization only where it hears that word.
Where the hello also carries `org_workstream_task_doers`, every agent doer starts the
same way: a chat's task and a named agent's (`done_by {kind:"agent", agent_id, name,
crew?}`, from the workstream's agent list, which names and kinds it) run in the task's
own seat, from the task's brief and never the chat's conversation; an agent the list no
longer names is 409. `done_by` is also a change (Change who does it): the task's owner's
or whoever looks after the workstream's (403 otherwise), never on a `running` task (403,
stop it first), a `done` one (403) or a Factory task (403); a person not in the
workstream or an agent not on its list is 422. Work that ended without finishing goes
back to `up_next`, the owner is unchanged, and the thread gains a `handed` line "from
<old doer> to <new doer>".
Send back is `state`
`up_next` on a `done` task by someone other than its owner, or with a `state_reason`:
anyone who edits may do it (a person task included), and the thread line and the
task's `state_reason` read "Sent back by <name>[: <why>]" until its next change. A done
task reopened (sent back or unticked) makes every unfinished task that waits on it wait
again: one that was `running` goes back to `up_next` (a Factory task keeps its job's
state), and each gains the thread line "Waiting again: <title> was reopened". A person's
`stopped` records `stopped_by {id, name}`, and its thread line reads "Stopped by <name>";
any later state clears it. A `running` task Afi does carries `run {live, since?}`, filled
at read time: `live` false means nothing on the server is working on it, and a PATCH
`state: running` then starts Afi on it again (thread line "Started again by <name>: nothing
was working on it"); with `live` true that PATCH changes nothing. The server settles a run
a restart cut off (thread lines "Interrupted: the server restarted while Afi was working on
it", then "Resumed after the restart"; a second cut leaves it `interrupted`) and one quiet
for a minute (`no_report`). At most 3 Afi runs go at once per workstream and 10 per
organization; a start over either limit is accepted and the task, `running`, carries
`queue {place}` (read time; its place among the workstream's waiting runs, from 1) until a
place frees; Stop takes it out of the queue. A run past its spending cap (the workstream's
Start without asking, else $0.50), the workstream's budget, 60 steps or 15 minutes is
paused: the task goes `needs` with an Ask whose id begins `ask_limit_`, options `keep_going`
and `stop_here` (thread line "Paused: <title>"); answering `keep_going` starts Afi again,
`stop_here` stops the task, and neither records a decision. A running task's `run` also
carries `now` (its last step, in a person's words: the running row's second line), `steps`
and `usd` (so far; each absent while zero); a queued task has none of them. A Factory task's state is its job's (409). A stale rev is 412.
`_task_answer {ws, id, option?, text?}` `POST .../answer` is the owner's (with owner
`everyone`, anyone who edits; after a day, also the workstream's fallback), 403
otherwise; the first answer decides and records `answered_by`, a second is 409; a
Factory task's answer goes through the job's own answer door and its rules.
`_task_comment {ws, id, kind?, text, offer_for?}` `POST .../comment` (`edit`, 201)
adds a `comment` (the default), `posted`, `request_note`, `worked` or `offer` (Afi's,
under the comment `offer_for`); a comment or post never changes the task's state. A
`comment` answers with `mentions`, the handles of the workstream's people (its persons
and everyone in its teams) it names with `@handle`, `@Name` or `@FirstName`; an @
naming nobody in the workstream, or inside a word (an address), names nobody. With
`offer {event, state}` instead it takes or dismisses an open offer (`undo_until` is five
seconds on) or, with `state` `open` before then, undoes the take (the owner or the
taker). `_tasks_start_all {ws, group}` `POST .../tasks/start_all` names the group's up
next Afi tasks that wait on nothing unfinished the way a person's `running` starts one,
and answers `{tasks}` with those Afi took, now `running`; a chat's tasks are left
`up_next`. If none could be taken it is that refusal (409).
Every change publishes `workstream.changed` with `part` `tasks` and `part_id` the task.

AN ORGANIZATION WORKSTREAM'S STANDING ORDERS (hello `org_workstream_orders`; fix wave
X1). They are kept and run by the organization's server, never by a person's computer:
`orders.create` refuses an `org:` workstream with `bad_scope`, and the clock passes such
an order over. `factory.org_workstream_orders {ws}` `GET /v1/workstreams/{id}/orders`
(view) answers `{runs, orders: [{id, ws, title, does, every, zone, on,
budget_per_run_usd?, next?, runs, created_by, created_at, updated_at, rev, when}]}`;
`runs` is false on a server that cannot run Afi (no model key), which then refuses to
make one or switch one on (409). `_order_create {ws, title?, when, zone?, does,
budget_per_run_usd?}` `POST` (the steward; `when` is `every <cron or interval>`) makes
one switched on; `_order_update {ws, id, rev, on}` `PATCH` switches it; `_order_delete
{ws, id}` `DELETE` removes it. Each run is ONE task (`came_from {kind: order, order_id,
name, run}`, done by Afi, owned by the owner of incoming work, group `order:<id>`): it
starts at once when `start_without_asking_usd` covers `budget_per_run_usd`, else waits
on the Ask `order:<run>` "Run … now?" (go, skip, not_right). Go starts the workstream's
agent on the server as whoever answered; skip and not_right leave it `stopped` with the
reason. A change relays part `orders`.

A REQUEST INTO ANOTHER WORKSTREAM (hello `org_workstream_requests`; Workstreams final
spec, flow 7). `factory.org_workstreams_requestable {}` `GET /v1/workstreams/requestable`
answers `{workstreams: [{id, name, parent?, in, receiver?}]}`: every open workstream
the caller is in or that is not private, by name, `in` saying whether they are in it
and `receiver` its owner of incoming work (absent for Everyone).
`factory.org_workstream_request {ws, title, request?, from_ws?, from_task?}` `POST
/v1/workstreams/{id}/requests` (any member; 404 for a private workstream they are not
in, or an archived one) makes a task there: owned by the workstream's owner of
incoming work, done by that person (Afi when it is Everyone), `up_next`, `came_from`
`{kind: person, id, name, ws?, ws_name?}` naming the workstream it came from when the
caller can edit `from_ws`, and the thread line "Requested by <name>[ from <workstream>]".
The task `from_task` in `from_ws` gains "Sent a request to <workstream>: <title>". It
answers 201 `{id, ws, ws_name, title, owner, task?}`, the whole task only to someone in
the workstream.

Invites (workstreams v2, J4 and J7; hello word `org_invites`). `factory.org_invites {}` `GET /v1/invites`
lists the organization's invites, each with `id`, `ref` ("invite:<id>", the name the person goes by before
joining), `email`, `name`, `team`, `team_name`, `workstreams`, `workstream_names`, `state` ("invited",
"joined", "expired"), `joined_at`, `handle` and `emailed`, and says `mailer` when the server mails links.
`factory.org_invite_create {email?, name?, team?, workstreams?}` `POST /v1/invites` makes one (or adds to the
open one for the same email) and answers `{invite, url}`; `factory.org_invite_resend {id}` makes a new link
for seven more days; `factory.org_invite_withdraw {id}` ends one nobody used. Only the operator calls those
four. `factory.org_welcome {}` `GET /v1/me/welcome` is the caller's "added you to" line, `show` false once
`factory.org_welcome_seen {}` was sent. `factory.org_person_remove {handle}` `DELETE /v1/people/{handle}/sessions`
is the operator removing someone: their workstreams go to the operator and the tasks they owned arrive in the
operator's Handed to you.

An engine whose hello carries `org_workstream_decisions` adds Asks, views and decisions
(Workstreams final spec, "Working with other people"). A task's `ask` is its OWNER's
Ask; `asks` are its other open Asks, each `{..., decider: {id, name}}`, and the task is
`needs` while any is open. Every Ask may carry `by` (who raised it, a thread `by`),
`views` `[{by: {id, name}, text, at}]` (one per person, never the decider's) and
`against` `{decision, text, by, why}` (the Ask the decision check raised); an option
may carry `decides`, the decision its answer records in one line. A task may carry
`assumed` `[{id, text, at, by, overturned_by?}]`, what its agent decided without
asking. `_task_update` also takes `add_ask` (another Ask, with its `decider`),
`ask_later {ask, until}` (Later, by that Ask's decider), `assumed` (lines to add) and
`overturn` (an assumed id; with `state_reason` the why; a done task goes back to its
doer, its reason reading "instead of “…”, <why>"). `_task_answer` takes `ask` (which
Ask; absent, the task's own); only its decider answers it (else 403 "only <name>
answers this Ask"), the task runs again only once no Ask is open, and the answer
RECORDS A DECISION in the workstream: the option's `decides`, else the question and
the answer; a changed file's question and "Keep the decision" on a conflict record
none. `factory.org_workstream_task_view {ws, id, ask, text}` `POST .../tasks/{tid}/view`
(`edit`) puts the caller's view on an Ask (`text` "" takes it away; the decider's own
is 403) and answers the task. `_task_comment {kind: request_note, decision}` records
`decision` as the caller's, from that task. A rules change (`update {rules}`) records
one decision per rule that changed, the steward's ("Tasks under $5.00 start without
asking"). A decision (`WorkstreamDecision`) gains `by_name`, `from {kind:
ask|request|rule, task?, task_title?, ask?, asked_by?, views?, replaces?}`,
`replaced_by`, `changed_by {id, name}`, `changed_at` and the state `withdrawn`.
`factory.org_workstream_decision_withdraw {id, decision}` `POST
/v1/workstreams/{id}/decisions/{did}/withdraw` and `factory.org_workstream_decision_replace
{id, decision, text}` `POST .../replace` are ITS MAKER'S ALONE (403 "only the person who
made a decision can withdraw or replace it"); the old one stays, `withdrawn` or
`superseded` with `replaced_by`, and both answer the workstream. A whole-workstream
save that changes or leaves out someone else's kept decision is refused the same way.

THE DECISION CHECK runs in the engine's session, around `ws_task_create` and around
`ws_task_update` with `state` `done`, whichever door answers them. It calls the door's
`ws_decisions` (`{decisions: [{id, text, by, by_name}]}`, a tool the model also holds)
and, with any, asks the conversation's model one short question; for finished work it
reads the task (`ws_tasks {task}`) and up to three result files (`ws_file_read`). A
conflict makes new work and then raises the Ask, or keeps everything but `done` of
finished work, through `ws_task_conflict {task, decision, why}` — a verb the doors
answer and the model never holds — which adds the Ask to the decision's maker with
"Keep the decision" (recommended, records nothing) and "Go ahead anyway" (records an
exception). A door without `ws_decisions`, no decisions, or a reply the check cannot
read lets the call through unchanged. The organization's server answers both for its own Afi
(`internal/factory/serve/workstream_agent_work.go`), so a server conversation is checked
the same way, with the server's model; `ws_task_conflict` there is no route, only the
agent door reaches it.

COMMENTS ON A PASSAGE OF A PAGE (hello `org_workstream_page_comments`; Workstreams final
spec, "Comments attach to passages and go to people"). A thread is the desk's Personal
shape: `{id, ws, page_id, block, block_text, offset, quote, comments: [{id, by: {kind,
id?, name}, text, at}], resolved?: {by, at}, unread?}`, where `block` is the block's
path through the editor's content arrays, `block_text` that block's whole text when the
thread was posted and `offset` the code points before `quote` in it. THE SERVER KEEPS
THE ANCHOR EXACTLY AS POSTED and never says `anchored`: the page is a live document it
never parses, so the desk finds the words again in its editor on every read (the path,
then the one place in that block, then the one place on the page, else not anchored).
A post is refused 422 only when it contradicts itself (`quote` is not the words at
`offset` in `block_text`), when it has no words or more than 16384 bytes, a quote over
4000 bytes, or no block. A page holds at most 2000 threads (413).
`factory.org_workstream_page_comments {ws, page, resolved?}` `GET
/v1/workstreams/{id}/pages/{pid}/comments[?resolved=1]` (view) answers the open threads,
or every one; `_page_comment {ws, page, block, block_text, offset, quote, text}` `POST`
the same path (edit) answers 201 the thread; `_page_comment_reply {ws, thread, text}`
`POST /v1/workstreams/{id}/comments/{cid}/reply` (edit, 201) opens a resolved thread
again; `_page_comment_resolve {ws, thread, reopen?}` `POST .../resolve` (edit);
`_page_comment_seen {ws, thread}` `POST .../seen` (view) marks it read for the caller
alone; `_page_comments_for_you {ws}` `GET /v1/workstreams/{id}/comments/for_you`
answers the open threads with a comment by someone else the caller has not read. A
person's own comment is read the moment it is written. Every change relays as
`factory.org_workstream_changed` with part `page` (the page's id) and part `files`, and
a document's file counts its open threads as `open_comments`.

An engine whose hello carries `org_canvases` reads and writes an organization's
canvases, which live on its server and are kept there by canvas-core (`canvas rpc`), so
every canvas shape on these methods is canvas-core's own, passed through unchanged — the
same snapshot, op, op record, version and mark the desk reads for a Personal canvas. Each
takes `connection` and, but for the list, create and import, `id` (`cv_` and twelve
letters or digits): `factory.org_canvases` `GET /v1/canvases` (`{canvases}`, each the
canvas-core summary plus `owner`, `shares`, `private`, `my_role`, `seq`, `updated_by`,
`workstream?`; a private canvas is answered to its owner alone, never to the operator),
`factory.org_canvas_get`, `factory.org_canvas_create {title, owner?, shares?, private?,
workstream?}` (with `workstream`, hello word `org_workstream_canvases`: the canvas belongs
to that organization workstream and is reached the way the workstream is; it needs `edit`
on the workstream, and `owner`/`shares`/`private` beside it are refused),
`factory.org_canvas_update {title?, trashed?, workstream?}` (`workstream` puts it in one, or
`""` takes it out; full on the canvas), `factory.org_canvas_delete`, `factory.org_canvas_snapshot`,
`factory.org_canvas_ops {since}` → `{seq, ops}`, `factory.org_canvas_apply {base_seq, ops}`
→ `{seq, ops}` (the records written, each stamped `person:<handle>` by the server),
`factory.org_canvas_versions`, `factory.org_canvas_version_create {label?}`,
`factory.org_canvas_restore {version}` → `{seq, ops}`, `factory.org_canvas_marks` (each
mark with `by`), `factory.org_canvas_mark_create {mark}`, `factory.org_canvas_shares
{shares:[{kind, id, role}], owner?, private?}`, `factory.org_canvas_import {snapshot,
assets:[{sha, data, type, name}], from_local_id, title?}` (a laptop canvas moving in once;
again answers the first), `factory.org_canvas_asset_get {sha}` → `{data, type}` and
`factory.org_canvas_asset_put {sha, data, type, name?}` (`data` is base64). Roles: `view`
reads; `comment` adds marks and takes a hand; `edit` writes ops, versions and restores;
`full` shares, trashes and deletes; a canvas you cannot view is `not_found`. A hand is a
lease op `{t:"lease", frame, actor:"", until:"hand"}` (`until:""` lets go); `member`,
`wait`, `proposal` and `suggest` ops and reserved meta keys are `bad_params`. A write
into, or a hand on, a frame another person's hand is on is `conflict` with
`detail: {reason:"conflict", frame, holder, seq, ops}`, `ops` being every record after
`base_seq`. Text inside a frame is not these ops: it is a shared document (`org_docs`).

The event `factory.org_changed` `{connection, kind, id}` says a team or workstream
changed on an organization's server, so a window showing that organization reads it
again instead of polling. Once a connection has answered any of the methods above,
the engine follows its `GET /v1/events` with the same session and relays the
server's organization kinds, already filtered to what this person may see:
`kind` `team.changed` (a team was made, renamed, deleted or its members changed;
read `factory.teams`), `workstream.changed` (made, edited, owner or shares changed;
read `factory.org_workstream {id}`, which answers `not_found` once it is no longer
visible), `workstream.removed` (archived or purged; drop it), `canvas.changed` (a canvas
was made, written, renamed or shared; it also carries `seq`, the seq it reached, and `by`,
the handle that wrote it, so a window already at `seq` reads nothing and any other reads
`factory.org_canvas_ops {since}`) or `canvas.removed` (deleted or no longer shared with
you; drop it). A `workstream.changed` from a
server with `org_workstream_parts` also carries `part` (`page`, `versions`, `plan`,
`wakes`, `suggestions`, `jobs`, `spend`, `files` or `workstream`), `part_id`, `by`, `rev`,
`deleted` and, for `files`, `flagged`, so a window can read just that part. `connection` is the
`wakes`, `suggestions`, `jobs`, `spend`, `tasks` or `workstream`), `part_id`, `by`, `rev` and
`deleted`, so a window can read just that part. `connection` is the
id the methods take. For example:

```json
{"event":"factory.org_changed","seq":812,"data":{"connection":"fc_acme","kind":"workstream.changed","id":"ws_01JD…"}}
```

**A job in an organization workstream answers to that workstream.** A job belongs to
the workstream whose go-ahead started it (`desk.policy_group` `ws_…`). On a team
factory, every `POST /v1/jobs/{id}/<verb>` (stop, steer, priority, prioritize, reply,
nudge, recheck, request_changes, undo and the rest) needs the job's person, `edit` on
its workstream, or the operator; a job with no person needs `edit` on its workstream.
`follow`, `brief_seen` and `scout` only need sight of the job, and `take`/`release`
keep the lease desk's own person rule. A job outside every workstream with a person is
changed only by that person or the operator (it used to be anyone who could read the
repository). The refusal is 403 `reason: "not_job_person"`, `error` "only @santosh and
people who can edit this job's workstream can do this" (`job_person` absent when the
job has none). Undo of a stop is allowed to whoever stopped it and to the job's person.
A job is seen only with read on its repository AND `view` on its workstream: the
queue, `GET /v1/jobs/{id}` (404), the record and `activity`/job events leave it out.
A sign-in whose token row names no one sees no workstream job and acts as no one's
person, where it used to read as the server machine's own GitHub login.

An empty `id` means the stream dropped and the server could not replay what was
missed; the engine then sends one `team.changed` and one `workstream.changed`, both
with `id` `""`: read both lists again. A dropped stream is otherwise reopened with
`Last-Event-ID`, so nothing is lost. Following ends when the connection is detached,
its sign-in changes, ends or is refused, or the engine stops; the next organization
method starts it again.

An engine whose hello carries `org_presence` also says who is in an organization
item and who changed it. Every `factory.org_changed` carries `by`, the handle of the
person who made the change as the server stamped it from the sign-in (`""` on a
resync and from an older server).

`factory.org_presence {item: {kind, id}, part?, editing?, left?, client?}` is
`POST /v1/presence`, a heartbeat the desk sends every 10 s while the item is open:
`kind` is `workstream` (or `canvas`, on a server that keeps canvases), `part` the
page or frame the person is on (`""` for the item itself), `editing` whether they
are typing in it, `client` a window id so two windows of one person are told apart,
and `left: true` once when the item closes. A person not heard from for 30 s has
left. The answer is `{item, here: [{handle, part, editing}]}`, who is on it now,
one row per person and part. An item the caller may not view is `not_found`. The
server publishes only a CHANGE to that list (an arrival, a move to another part,
editing starting or stopping, a leaving or a silence), relayed as the event
`factory.org_presence {connection, item, here}` to those who may view the item:

```json
{"event":"factory.org_presence","seq":813,"data":{"connection":"fc_acme","item":{"kind":"workstream","id":"ws_01JD…"},"here":[{"handle":"santosh","part":"pg_retry","editing":true}]}}
```

Presence is not kept: a restarted server knows nobody until the next beat. Carets
inside a document are not presence; they travel with that document's awareness.

`factory.org_workstream_activity {id, since?, limit?}` is
`GET /v1/workstreams/{id}/activity`: every recorded change to the workstream and its
go-aheads, oldest first (at most 500, the newest), as
`{id, events: [{id, at, kind, verb, object, handle, detail, actor}], people}`. `verb`
is the words a person reads (`created workstream`, `changed workstream`, `changed
sharing`, `changed owner`, `archived workstream`, `changed go-aheads`, and the page,
plan and job verbs); `detail` names the fields that changed, never their words;
`handle` is who made it, stamped by the server. A workstream the caller cannot see is
`not_found`. Changes to teams and to the factory's rules, routing, spending defaults
and settings are recorded the same way and read in the factory's activity list
(`object` `team:<id>` or `factory`).

An engine whose hello carries `factory.group_policy` and `factory.waits_set`
(and capabilities `group_policy: true`, `waits_on: true`) serves go-aheads the
factory follows by itself. `factory.features {factory}` answers `{features}`, what
that one factory keeps (`GET /v1/features`): `group_policy` and `waits_on` (a factory
with a store), `keyed_create` (the keyed create rule below), or `[]` from a factory
older than the route. `factory.policy_get {factory, group}` and
`factory.policy_set {factory, group, stopped, allowances}` (`GET /v1/policy?group=`,
`PUT /v1/policy`) answer `{group, version?, stopped, owner?, updated_at?, spent_usd,
allowances: [{id, jobs, cap_usd, ends_at, spent_usd, state}]}`; a set replaces the
group's whole policy and its caller becomes its owner (on a team, only the owner or
the operator may change one someone keeps: `forbidden`). When `group` is an organization workstream's id, only those who can see that
workstream read its policy (`not_found` otherwise, as for the workstream itself) and only
those who can write it set one (`forbidden`). `group` is 1–200 plain
characters; at most 50 allowances, each a distinct id of 1–64 of `A–Z a–z 0–9 . _ : -`,
1–200 distinct job numbers, a cap above 0 and at most 10,000, and an RFC 3339
`ends_at`, kept as sent; anything else is `bad_params` before it is sent. `state` is
`ended` once the policy is stopped or `ends_at` has come, `spent` once the jobs the
allowance started spent its cap, else `active`. `factory.waits_set {factory, job,
waits_on, expect?}` (`POST /v1/jobs/{id}/waits_on`) replaces the jobs one job waits on
(0–20 distinct others on the same factory, no circle) and answers `{factory, job,
waits_on, current?}`. After every poll — on a team server (`codeaf factory serve`) every
tick of its own poll, with or without a GitHub grant and with no app attached; on the
app's own local factory only while the app runs with its runner on (`factory.local_run`,
which needs `factory.setup_connect` first), never after the app quits — the factory starts a covered job that never ran
and sits in Backlog once every job it waits on has merged, while one allowance naming
it is active and the job is not someone else's; each started job holds its budget
(lowered to what is left of the cap) until it finishes. A started job carries
`policy_run` (the allowance id) and its start record `auto_started` with detail
`allowance <id>`; a covered job not yet started carries `blocked {by: waits_on|policy,
reason, job?, allowance?, since?}` — a full cap reads "its go-ahead's cap is spent" when
the allowance's jobs spent it and "its go-ahead's cap is held by jobs already running"
when running jobs only reserve it — and every job answer, `factory.queue` rows included,
carries `waits_on` and `waiting_on`.
Stopping, ending or withdrawing an allowance starts nothing further and stops nothing
already running.

A Factory job create (`POST /v1/jobs`, or `POST /v1/issues` when it opens the
GitHub issue first) sent with an `Idempotency-Key` header is remembered for 24
hours per caller, route and key, so a resend with the same key does not make a
second job or issue, within two limits. The caller is the exact `Authorization`
and `Cookie` headers: a resend under a new bearer or a rotated session cookie is
not matched to the first answer and runs as a new create. And the answer is saved
only after the create has committed, so a Factory that stops between the two runs
the resend again. A Factory with this rule lists `keyed_create` in
`GET /v1/features`; an older one keeps every keyed answer, a refusal or a 5xx
included, for 24 hours, so an engine sends a create key only to a Factory that
lists it. An answer given before the create wrote anything carries
the response header `X-Codeaf-Phase: pre-mutation` and is not kept, so the same
key runs the create again. Such answers are refusals of the request (a bad field,
a repository the caller cannot see or that is off, a missing pipeline), sign-in and
read failures, GitHub's own 4xx refusal of the issue, and GitHub not answering
before the issue request was sent. An answer given once the issue request may
have reached GitHub or the job exists is replayed unchanged, including a 5xx or
408. Examples are a lost or 5xx GitHub answer, or a job made whose start failed.
After an `uncertain` keyed create, resend the same key: a create that wrote nothing
runs again, and one that did returns its first answer. If that is the same failure
again, read the queue, since the job or issue may exist. A create without a key is
never replayed. Worker and runner routes keep their own rule: a 5xx, 408 or 429 is
never kept, and a report refused as invalid is checked again.

`factory.job_create` keeps `title` trimmed and separate from literal `text`,
including details whitespace in the job ask and body; empty details stay empty.
Neither field lifts
terminal chips or changes budget, approval steps, rounds, effort or security.
The job title alone supplies the draft PR title. Existing jobs are not rewritten.
The method, reply, `factory.job_actions` hello feature and error codes are unchanged.
Factories without the separate-title capability retain their prior behavior.

`factory.job_create` also accepts these optional fields. Engine capabilities are
`create_issue`, `create_group` (group and milestone), `create_budget`,
`create_key`, `create_split_from` and `job_context` (the separate attachment write
below). Each is true when the engine carries that feature end to end; an older
engine omits it or answers false. These flags describe the engine; the attached
Factory may still be older than a feature. `create_key` alone also describes one
Factory: `factory.capabilities` takes an optional `factory`, and `create_key` is
true only in an answer naming a Factory whose `GET /v1/features` lists
`keyed_create` (it keeps a keyed create's answer only once the create wrote
something). Without `factory`, or for a Factory that does not list it or cannot
be asked, `create_key` is false. A Factory without the word keeps every keyed
answer, a refusal or a 5xx included, for 24 hours, so the engine never sends it
a key: it asks the Factory again on every create with a key, and to a Factory
that does not list `keyed_create` that create answers `conflict` with
`detail.reason: "factory_too_old"` and no create is sent. A create without a key
is sent as before and asks nothing.

| Parameter | Rule |
| --- | --- |
| `open_issue` | Boolean, default false. True sends `POST /v1/issues`: open one GitHub issue using only `title` and `text`, then create the job tied to its number. False sends `POST /v1/jobs` and opens no issue. |
| `group`, `milestone` | Opaque labels, trimmed, at most 200 characters each, with no control characters. Empty means none. |
| `budget` | Initial job budget in USD: one of the standard steps, 5, 10, 20 or 30. Recorded as a budget action before starting; a start now or later keeps it instead of taking the pipeline default. |
| `key` | A create key of 1–128 characters from `[A-Za-z0-9._:-]`, sent only as the create request's `Idempotency-Key` header, and only to a Factory that lists `keyed_create`. `key: ""` is the same as no key (`omitempty`): no header is sent. |
| `split_from` | Integer at least 1, naming a job held by the same Factory that the caller can see. Stores the job this new job was split from. An explicit zero or negative number is `bad_params` before sending. |

The Factory stores group and milestone outside the description.
`factory.job`, queue rows and `current` carry the labels when present.
Those reads also carry `split_from` when supplied, omitted otherwise. The parent
job stays unchanged. The new job's own activity adds one line, `split from #N`.
Split lineage leaves normal pipeline stamping, step briefs and run-as-new
cleanup behavior in effect; it is stored separately from `from_job`.

A missing or hidden `split_from` parent is the same Factory 422 refusal, mapped
to `bad_params`, before any job or GitHub issue is made, including with
`open_issue: true`.

Bad extras answer `bad_params`, naming the field and limit, before anything is
sent. The Factory also validates extras before opening an issue or making a job.
A Factory's 422 strict-decoder refusal of `group`, `milestone`, `budget`,
`issue` or `split_from`, or the issue create route missing (a 405 there, or a 404
whose body is the Factory's `factory endpoint not found` or is not its JSON),
answers `conflict`
with `detail.reason: "factory_too_old"` and the existing update instruction:
"This factory doesn't support that yet; nothing was changed. Update the factory
to use it." No fallback request is sent. Any other 404 on the issue create route,
such as the Factory's own "repository not found" for a repository the caller
cannot see, stays `not_found` with the Factory's words. A 501 stays `engine` with
"this factory does not provide that action"; GitHub and sign-in refusals retain
their existing mapping. A timeout or 5xx stays `uncertain`.

The reply is unchanged: `FactoryJobActed` with `action: "create"`, `job` and
optional `current`. On a Factory that lists `keyed_create`, a resend with the
same key within 24 hours does not make a second job or issue; a different key
creates a separate job. After an `uncertain` create, resend the same key, never
a new one, and if the answer is still uncertain read the queue before trying
again. Two limits remain: the Factory matches a resend by its exact
`Authorization` and `Cookie` headers, so one sent under a new bearer or a
rotated session cookie runs as a new create, and it saves the answer only after
the create has committed, so a Factory that stops between the two runs the
resend again. With none of the new fields, the route, body and absent
idempotency header are unchanged.

`factory.job_create` takes `workstream` (an organization workstream id, `ws_…`),
passed to the factory as `POST /v1/jobs {workstream}`. The factory checks the
caller can edit that workstream (403 otherwise, 404 when they cannot see it, 409
when it is archived) before the job exists, and the job joins the workstream in
the same request, as `PUT /v1/workstreams/{id}/jobs/{job}` would. On an
organization server a signed-in person reads the repositories turned on in Repos
(the organization's) besides those their own GitHub sign-in reads; a job on one
they read ONLY through the organization must name such a workstream, or is
refused 403 with "Make this job in a workstream you can edit: this repository is
the organization's." The operator is never asked for one. An older factory
ignores the field, so a desk that sends it still adds the job to the workstream
itself afterwards; that add is a no-op when the job is already in it.


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

`factory.setup_github` in hello features says the engine can connect the
first run's GitHub in the browser with GitHub's OAuth device flow. It is
announced only by a build that names an OAuth app (the client ID stamped in at
build time, or `CODEAF_GITHUB_CLIENT_ID`); without one the three methods are
`unknown_method` and setup offers the gh login and a pasted token.
`factory.setup_github_start {caller}` answers `FactorySignIn` (`flow`,
`factory`, `user_code`, `verification_uri`, `interval`, `expires_in`), the
shape `factory.sign_in_start` answers; GitHub's device code stays in the engine
and a second start from the same caller replaces the first. The scope asked
for is `repo`. `factory.setup_github_poll {flow, caller}` answers
`{state: pending|signed_in|expired|denied, interval?, message?, github_token?,
view?}`; a poll sooner than `interval` is answered `pending` without asking
GitHub, and `slow_down` raises `interval`. On approval the engine connects the
token exactly as `factory.setup_connect {source: token}` does and only then
answers `signed_in` with the setup `view` after the connect and the token,
once, as `github_token`, for the app to keep in its Keychain as it keeps a
pasted token. It is the only answer of the setup wire that carries a secret. A
connect that fails is that connect's error, and the flow is over either way.
`factory.setup_github_cancel {flow, caller}` forgets the flow. Another
caller's flow, an ended flow and an unknown one are `not_found`.

`factory.intake` in hello features says the engine serves `factory.intake_add`:
outside work filed as a job through `inbox.Add` in the engine's own process, on
the factory this engine serves; the factory has no HTTP intake, and another
factory is `not_found` ("work is filed on the machine that runs the factory; on
another machine, connect GitHub or Jira there or send it through AgentField").
Work for a repository that is off is `conflict` with the factory's repo-off words.
Its params are `{factory, source, external_id, title, body?, url?, author?,
labels?, priority?, repo?, trust?}`; `source` is a lowercase name (`jira`,
`inbox:ops`), `priority` is `p0`..`p3`, and `trust` is `owner` (the default),
`collaborator` or `stranger`, the job's tier; a repeat may lower it, never
raise it. The pair `(source, external_id)` is
the identity: a second call updates the job it made and answers `created:
false`. The reply is `{id, created}`. A malformed entry is `bad_params` with
the inbox's own sentence.

`factory.agentfield` in hello features says the engine serves the six
`factory.agentfield_*` methods, which make this machine's factory a node on an
AgentField control plane so AgentField's triggers are its intake
(internal/factory/agentfield).
`factory.agentfield_connect {factory, url, api_key?, base_url, node_id?,
listen?, trust?}` checks `url` and `api_key` against the control plane's
`GET /api/v1/sources`, binds the node's listener on `listen` (default:
`base_url`'s port, on 127.0.0.1 when `base_url` is loopback, else 0.0.0.0),
registers node `node_id` (default `codeaf-factory`) with reasoners `intake`
(accepts webhooks), `message` and `status`, and answers `FactoryAgentFieldStatus`
once the control plane took the registration; a connect that fails any of
those keeps nothing (`bad_params` for a refused key or registration,
`unreachable` for a control plane that does not answer). `trust` is the
ceiling for every entry the node files: `owner`, `collaborator` (default) or
`stranger`. The registered `base_url` carries a 64-hex address secret
(`<base_url>/n/<secret>`), the node answers nothing outside it, and neither the
key nor the secret is ever in a reply. `factory.agentfield_status {factory}`
answers `{factory, connected, url?, node_id?, base_url?, listen?, trust?,
key_kept?, listening?, registered_at?, lease_seconds?, lease_until?,
last_event?, last_event_source?, last_event_what?, last_error?,
last_error_at?, executions?}`. `factory.agentfield_disconnect {factory}` tells
the control plane the node is offline, stops the listener and forgets the
settings, the key and the secret; jobs already filed stay.
`factory.agentfield_triggers {factory}` is `GET /api/v1/triggers` for this node:
`{factory, node_id, triggers: [{id, source, reasoner, event_types?, enabled,
managed_by?, secret_env_var?, ingest_url, events_24h?, failed_24h?,
last_event_at?}]}`, `ingest_url` being the control plane's `/sources/<id>`.
`factory.agentfield_sources {factory}` is the control plane's catalog,
`{factory, sources: [{name, kind, secret_required, config_schema?}]}`.
`factory.agentfield_trigger_add {factory, source, config?, secret_env_var?,
event_types?}` is `POST /api/v1/triggers` targeting `<node_id>.intake` and
answers the trigger. Every method but connect is `conflict` on a factory that
is not connected, and every method is `not_found` for a factory other than
this machine's.

`factory.agentfield_triggers` in hello features says the engine also serves
the trigger page's two calls. `factory.agentfield_triggers {factory}` then
carries the catalog beside the triggers, `sources: [{name, label, kind,
secret_required, config_schema?, config_fields?: [{name, label, secret?}],
event_types?}]` (`event_types` only when the control plane's catalog lists the
events a source can emit)
(every source in `factory.agentfield_sources` carries `label` and
`config_fields?` too), and each trigger adds `entry_source` (the source a job
it files carries, `agentfield:<source>` with `_` written `-`, what a routing
rule's `when.sources` names), `label?`, `webhook_url?` (the `ingest_url`,
absent for a `loop` source such as cron, which nothing posts to) and
`created_at?`. `factory.agentfield_add_trigger {factory, source,
event_types?, config?, secret_env_var?}` makes one trigger aimed at
`<node_id>.intake` and answers `{factory, trigger, setup?: {webhook_url?,
secret_env_var?}}`. A source the catalog does not have, or one that needs a
signing secret given no `secret_env_var`, is `bad_params` in words before
anything is made. `setup.secret` is never filled: AgentField reads a source's
secret from its own environment variable and never hands one back. A refusal
of either add never quotes the `config` it was sent.

**A pipeline step that runs on AgentField.** `factory.pipeline_edit_agentfield`
in hello features says `factory.pipeline_preview` takes an `add_step` of
`kind: "agentfield"`: `prompt` is the reasoner, `node.reasoner`, and the
optional `input` maps the reasoner's field names to text with the job's
`{placeholders}` (`{title}`, `{body}`, `{repo}`, `{repo_url}`, `{base}`,
`{issue_url}`, `{issue_number}`, `{job}`, `{pr_url}`; a value that is exactly one of them is
sent as that fact's type, so `{issue_number}` is a number). `{pr_url}` explicitly
binds the job's first opened PR for a downstream step. Before dispatch, earlier
phases must be done or skipped, at least one earlier work step must be done,
and the job must have a PR URL; otherwise the consuming step fails before
contacting its reasoner. Unmapped default inputs do not gain this field.
A job's `steps[]`
read it back as a `StepDef {kind: "agentfield", target, input}`, and the run's
executions follow it as steps whose `parent` is that step. The job also carries
`agentfield_steps`, each run by its step's name. `factory.agentfield_reasoners
{factory}` (hello feature of the same name) lists what such a step may name:
`{reasoners: [{node, reasoner, target, description?, online, input?: [{name,
required?, type?}]}], placeholders: [...]}`, read from the control plane's
`GET /api/v1/nodes`, and `conflict` on a factory that is not connected.

**A run's pause for a person.** A reasoner that pauses for approval puts its job
in Needs you with `needs_you.question.kind: "agentfield_approval"`,
`who_can_answer: "job_person"`, and on the question `text`, `body` (the whole
markdown it asks about), `options: ["Approve","Ask for changes","Reject"]`,
`from_step` (a step in `steps[]`), `approval_kind` (`plan_review`, `question`,
`approval`), `node`, `round` (from 1), `previous_body` and `note` (the words
that asked for this round) from round 2, and `url`. Its id changes with each
round. `can.approve`, `can.request_changes` and `can.reject_plan` are true. The
answers are the job verbs: `factory.job_approve {words?}` is the control plane's
`approved`, `factory.job_request_changes {words}` is `request_changes` with the
words (required), and `factory.job_reject_plan {words?}` (words may be empty for
this kind) is `rejected` and stops the job. An answer given first somewhere else
stands: the job's question comes down, and a late answer is `conflict` with
"this was already answered somewhere else, and that answer stands".

**A harness step's question.** A codeaf harness step that asks
(`chat --once --events --steer`) waits for an answer. The factory puts the
question to the job's manager and then to the person as an ordinary step
question (`needs_you.question.kind: "step"`, with `options`, `pick` and `why`),
answered with `factory.job_answer {factory, job, question_id, option?, words?,
expect: {version, state}}` (hello feature `factory.job_answer`): `question_id`
is `needs_you.question.id`, `option` one of its `options` by label, `words` the
person's own; at least one, and with both the step reads `<option>: <words>`.
It is `FactoryJobActed` with action `answer`. A changed question or a moved job
is refused as the factory's answer door refuses it, and an
`agentfield_approval` question is `bad_params` (answer it with the three verbs
above). This answers any step's question, a native step's too.

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

An engine whose hello carries `factory.job_events` and whose capabilities
answer `job_events: true` serves `factory.events_since
{factory, after?, limit?, follow?}`. `factory` is required; `after`, when
present, is an integer at least zero. An absent cursor requests an initial
reset; zero requests the beginning. `limit` is 1–500, or 0/omitted for 100.
Invalid parameters answer `bad_params`. An engine without the Factory outcome
reader answers `unknown_method` and does not announce the feature.

The result is `{factory, supported, events, cursor, reset, more}`. `events`
is always an array, in increasing `seq` order, and includes only jobs the
caller may see; the cursor also advances past hidden outcomes. It does not
advance past an outcome whose repository GitHub could not answer for just
now (a 5xx, a failed connection or a spent GitHub rate limit, which
is never kept as a denial): the page ends before it with
`more: true`, and a read that would start at it fails (the Factory's HTTP
503, the engine's `engine` error) so the caller reads again later from the
same cursor. The cursor
belongs to the Factory and survives an engine restart. `more: true` asks for
another read after `cursor`. With no further outcomes, `cursor` is the latest
position. Other Factory errors retain their usual codes. A Factory older
than `GET /v1/job-events` answers `supported: false` with an empty event array
and no error; following does not start for that answer.

Each event is `{factory, seq, kind, job, at, title?, group?, how?, reason?,
need?, code?, step?, pr_number?, pr_url?}`. `kind` is `finished`, `merged`,
`failed` or `needs_you`. `how` describes a non-merge finish; merged outcomes
carry the PR number and URL; failures carry code and step; needs-you outcomes
carry need and reason. Titles are limited to 200 bytes at a rune boundary.
A `failed` outcome is a new failure on a job that `factory.job` then shows
as `needs_you`, or a failure other than `stopped` on a `done` job. A job a
person stopped, by any door or by an answer that stops it, and a job a
change of assignee stopped, read `backlog` and record nothing; a Close or
Reject records only its `finished`. A change of assignee or an unassign
also takes down the question the cut run asked, so the job shows no
`waiting_since`. A `needs_you` outcome is recorded only for a job with no
failure on it, once per wait: its `at` is the job's `needs_you.since`, which
is when that wait began (its failure's, question's or step's own time, or,
for a wait with none, such as a start held for approval, a take-over or an
AgentField run's approval, the moment the Factory saw it begin) and does not
move while the job waits with the same need and question, however many
writes the job takes. A client that forms the same occurrences
from a full `factory.job` read keys `failure` as a failed job only when
`state` is `needs_you`, or `done` with a code other than `stopped`, and keys
a wait only when `state` is `needs_you` and the job has no `failure`, from
`needs_you` (or `waiting_since` when `needs_you` is absent): the `stopped`
failure a `backlog` job (a person's stop) or a closed or rejected job
carries is not a failed job, a failed job's own wait is its `failed`
occurrence, and a job in any other state waits on nobody.
The Factory records outcomes at job updates even with no app connected and
keeps the newest 2,000 independently of its live notifications.

`reset: true` means read the Factory's current state once, then continue from
the returned cursor. A reset has no events. It occurs on an initial read,
an expired or replaced cursor, or a gap caused by an older writer. Cursor
zero on an empty record is caught up and does not reset repeatedly. A gap
resets once: the Factory read that finds it closes it, and the returned
cursor is past it.

`follow: true` starts one follower per attached Factory. A later outcome is
pushed as `factory.event` with the same `FactoryEvent` shape and sequence.
The push is a prompt copy: the app can always read again from its own cursor.
Repeated follow calls raise the follower's cursor without duplicating it.
A follower takes reset cursors silently; the app's own read observes resets.
After a reset that leaves its cursor where it was (a Factory without the gap
repair resets every read during a gap), the follower waits 2 seconds,
doubling to a minute while that repeats, before it reads again.
Closing the engine or detaching the Factory ends following. The Factory HTTP
read takes `after`, `limit` and optional `wait` seconds (0–15, default 0),
returning on an outcome or reset when a watch is available.

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

The job search bar's AI search (hello feature `factory.ai_search`) answers a
person's question in their own words with the jobs it is about.
`factory.ai_search_estimate {factory, question, jobs?}` asks no model and answers
`{factory, question, model, items, reused, reads, judge_most, usd_typical?,
usd_most?}`: `reads` screen reads of 40 items each, at most `judge_most` (60)
yes/no questions, `reused` items whose answers to this question are already
kept, and dollars only when the model's price is known. `factory.ai_search`
with the same params starts one run and answers `{run}` at once; the run reads
the caller's own queue (issue jobs, not dropped), or only `jobs` (at most 5000
ids) when sent. `factory.ai_search_status {run}` answers `{run, factory,
question, words, phase, total, read, related, checked, over?, asked, reused?,
failed?, usd, model, matches: [{job, ref, title, repo, why, kept?}], error?}`
where `phase` is `screening`, `judging`, `done`, `failed` or `stopped`, and
`factory.ai_search_stop {run}` ends it. There is no event: a surface polls
status. A question is at most 300 characters; a leading `?` is dropped. Every
call is booked as `factory search`.

An engine whose hello carries the feature `factory.routes` serves
`factory.routes {factory}` and `factory.routes_set {factory, rules, expect?}`:
the factory's routing table, an ordered list where the first rule that
matches an arriving item decides it. A rule is
`{"name": "...", "when": {sources?, repos?, labels?, authors?, text?,
priority?}, "then": {repo?, pipeline?, priority?, start, run_on?}}`; every `when` list
matches when any entry does and an absent one matches anything; `start` is
`auto`, `ask` or `backlog`; priorities are `p0`..`p3`; repositories are
`owner/name`. The reply is `{"factory": "...", "rules": [...], "changed_by"?,
"changed_at"?}`, `rules` always present (`[]` for none). `rules_set` replaces
the whole table; an omitted `rules` is `bad_params`, a bad rule is
`bad_params` naming it, only the operator may write (`forbidden`), and an
`expect` that is not what the factory keeps is `stale` with nothing written.
A rule's `when` may also carry `ask`, one yes/no question of at most 300
characters for the factory's judge; a rule with a question matches only on a
yes kept on the job. `factory.routes_test {factory, rules, limit?}` tries a
table (need not be the kept one) on the newest `limit` jobs the caller may see
(default 50, at most 200) and answers `{"factory": "...", "jobs": [{job, ref,
title, outcome, rule?, why?, reason?}]}`, newest first, where `outcome` is
`matched`, `pending` (a question with no answer kept yet), `refused` (the rule
matched, and the repository it sends the job to lists no pipeline of the name
it gives; `reason` is `routing rule <name>: <repo> has no pipeline <p>`) or
`none`. It writes nothing and asks no model. `factory.routes_set` refuses, as
`bad_params` in that same sentence, a rule that names both a repository and a
pipeline that repository's file (or the factory's copy of it) does not list,
when the file can be read; one whose file cannot be read now is kept. A rule
refused while routing never fails the factory's pass: the job stays unrouted
and its activity gets one `routed` row in that sentence.

With the feature `factory.route_run_on`, a rule's `then` may name `run_on`,
`"<node>.<reasoner>"`: a job the rule matches is run by that AgentField
reasoner in place of the factory's steps, needs no repository, and runs as a
job filed with `run_on` does. Such a rule names no `pipeline` (`bad_params`).
On this machine's factory `factory.routes_set` checks every `run_on` against
the reasoners the control plane lists: one it does not have is `bad_params`
(`routing rule <name>: AgentField has no agent <node.reasoner>; pick one it
lists`), and a factory not connected to AgentField is `conflict`. A source in
`when.sources` written `agentfield:*` matches every AgentField source, as
`agentfield` does.

`factory.job_set_repo` in hello features says the engine serves
`factory.job_set_repo {factory, job, repo, expect?}`: giving a job that never
ran its repository (`owner/name`), the way out of Backlog for outside work
that arrived with none. The factory sets the job's repository and places,
stamps that repository's steps, keeps that a person chose it (a routing rule's
repository never moves it again), clears the rule that routed it and routes it
again at once. It replies `FactoryJobActed` with `action: "set_repo"`. Only
the operator or the job's person may (`forbidden`); a job that has started is
`conflict` "This job has already started, so it keeps its repository.", and a
GitHub issue's job is `conflict` too. A Backlog job with no repository reads
`state_presentation {kind: "backlog", title: "Waiting for a repository",
detail: "Say which repository the work is on, or add a routing rule that sends
it to one."}`, and `factory.job_start` on it is `conflict` "say which
repository the work is on". `factory.job_start {pipeline}` is taken by a job
queued and not yet taken: its steps are replaced and its run laid out again
on them; only a job that ran is `conflict` "This job has already run, so it
keeps its steps."

An engine whose hello carries the feature `factory.library` serves the
factory's PIPELINE LIBRARY: named pipelines and the step definitions they use,
kept by the factory (not by one Mac, not in any repository) for every
repository it works on, in the same YAML as a repository's
`.codeaf/factory.yaml`, `pipeline:` and `steps:` only.
`factory.library {factory}` answers `{"factory": "...", "text": "...",
"version": 3, "pipelines": [{name, from: "library", steps}], "can": {"change":
bool}, "changed_by"?, "changed_at"?}` (`GET /v1/pipeline-library`); `text` is
`""` for an empty library and `version` starts at 1. Everyone signed in may
read it. `factory.library_set {factory, text, expect: {version}}` replaces it
whole (`PUT /v1/pipeline-library`): `text` is required (`""` empties it), it is
parsed by the factory's own parser and kept as its encoder writes it, which
writes only what the text wrote; a key other than `pipeline` and `steps`, a
pipeline named `pr` or `ci`, a problem in the file or a step the runner would
skip is `bad_params`; only the operator may write (`forbidden`); an `expect`
that is not the kept version is `stale` with nothing written.
`factory.library_preview {factory, base_text, edits}` applies the same edits
as `factory.pipeline_preview` (only the verbs that touch `pipeline:` and
`steps:`: `set_step`, `add_step`, `remove_step`, `move_step`, `rename_step`,
`set_approver`, `add_pipeline`, `remove_pipeline`, `rename_pipeline`) to a
library (`base_text` `""` is an empty one) and answers `{factory, text,
changed, steps, structure?}`, `text` being exactly what `library_set` would
keep. It writes nothing and asks the factory nothing.

A JOB'S PIPELINE NAME RESOLVES IN ONE ORDER, for `factory.job_start
{pipeline}`, `factory.job_create {pipeline}`, a routing rule's
`then.pipeline` and a job's own kind: the repository's file as the factory
runs it (its own copy when it keeps one, else main's file), then the library,
then the built-in steps (which name only `issue`). A repository pipeline of
the same name overrides the library's. A name found nowhere keeps the
refusals it always had. Behind the same feature `factory.pipeline` carries
`pipeline.pipelines`, every pipeline a job on that repository can run, each
`{name, from, steps, over?}` where `from` is `repo`, `factory_copy`,
`library` or `built_in` and `over` names the lower layers it overrides:
what "Pick the pipeline" offers. A library pipeline also appears in
`pipeline.by_kind`.

An engine whose hello carries the feature `factory.pipeline_edit_pipelines`
(beside `factory.pipeline_edit_structure`) previews three more edits in
`factory.pipeline_preview`: `add_pipeline {name, steps}` or `{name, from}`
makes a named pipeline (each step one the file or codeaf defines, or
`approve`; `from` copies a pipeline the file holds), `remove_pipeline {name}`
takes one out (its steps' definitions stay) and `rename_pipeline {name, to}`
renames one. A name is spelled as a step is (2 to 32 lowercase letters, digits
and inner hyphens); `pr`, `ci` and `issue` are refused as `locked`. A step not
yet defined is added after `add_pipeline` with `add_step` naming the new
pipeline, in the same preview. The answer's `structure.pipelines` lists every
pipeline made, removed or renamed as `{op, pipeline, to?, steps?}`, and the new
list is in `structure.order`. The pull request route and
`factory.pipeline_copy_set` take the resulting text as any other.

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

An engine whose hello carries `factory.repo_issues` and whose capabilities
answer `repo_issues: true` serves `factory.repo_issues {factory, repo, since?,
page?}`. `repo` is `owner/name`, `since` is RFC 3339 (absent means no lower
bound), and `page` is 1–10, default 1. It reads only `GET
/v1/repos/{owner}/{repo}/issues?since=&page=` through the caller's Factory
connection. The route accepts only those two query keys, once each, and uses
the caller's GitHub sign-in, or a solo Factory's own profile.

The result is `{factory, repo, issues, rows, newest?}`. `issues` contains at
most 50 entries, oldest update first, in every state: `{id, number, title,
body, url, state, created_at, updated_at, body_cut?}`. `id` is GitHub's int64
issue identity, `url` is its HTML URL, and `state` is `open` or `closed`.
`body` is cut at 8,192 bytes on a rune boundary; `body_cut: true` marks a
cut. Pull requests are excluded from `issues`, but `rows` counts all rows
GitHub returned (0–50) and `newest` is their latest `updated_at`, including
pull requests. An empty result has `issues: []`, `rows: 0` and no `newest`.

Invalid params are `bad_params` before any request. A repository the caller
cannot see, or GitHub's repository 404, is `not_found`. A spent GitHub rate
limit on the visibility check or issues request is `busy` with
`detail.retry_at` in RFC 3339: GitHub's `Retry-After` hint, or its
`X-RateLimit-Reset` when `X-RateLimit-Remaining` is `0` or absent, the later
of the two when both apply. A secondary limit, which leaves requests
remaining, therefore never waits for the primary window's reset.
Every GitHub HTTP 429 is a rate limit, even without headers; a 403 is a
rate limit only with `X-RateLimit-Remaining: 0` or `Retry-After`.
A rate-limited visibility check is not cached as a repository denial.
Until `retry_at`, and for at most the ten-minute visibility cache life, the
Factory answers that GitHub token's uncached repository checks as the same
rate limit without asking GitHub; the next check after that asks again.
Without a usable reset or retry hint, `detail.retry_at` is one minute after
the Factory's clock.
A sign-in the Factory accepts but that has no GitHub access the route can use
(a pasted access token with no GitHub sign-in, or a solo Factory whose host
has no GitHub profile) gets HTTP 401 with reason `github_expired` and no
bearer challenge. The engine answers `github_expired` for it, and for any 401
from this route without a `WWW-Authenticate` challenge, and keeps the sign-in.
Only the Factory's sign-in gate refusing the bearer, which always challenges,
is `signed_out`; that forgets a restored sign-in as every read does.
A Factory missing the route is `conflict` with
`detail.reason: "factory_too_old"`; its own
"repository not found" remains `not_found`. An engine without the reader
answers `unknown_method`.
The route itself stores nothing and appends no activity; the Factory's
ordinary per-request housekeeping runs as it does for every authenticated request.

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

### Factory job facts (`factory.job_source`, `factory.job_steps`, `factory.job_runs_on`, `factory.job_merge_offered`)

Each fact below is computed once in the engine (`internal/factory`) and sent on
every job of `factory.queue` and on `factory.job`; a client draws it and never
derives it again. All are additive; an engine without the word sends none of
them, which reads as unknown.

- `factory.job_source`:
  - `source: {name, ref?, url?}` on every job that says where it came from.
    `name` is `github`, a connector (`jira`), a trigger (`inbox:ops`), or
    `terminal`/`chat` for a typed job. `ref` is the job's key there in the
    source's spelling (`owner/repo#147`, `ENG-142`, `A1`); `url` is its address
    there when the source sent one.
  - `repo_url`: the repository's address, read off an address the forge gave
    for it (the job's pull request, then its GitHub issue) so a non-github.com
    host is kept; `https://github.com/<owner>/<name>` only when neither exists;
    absent when the job names no `owner/name`. Clients stop building it from
    `repo`.
  - `route: {rule, answer?: {yes, why, model?}}`: the routing rule that decided
    the job, and the AI's answer when that rule asked one. Absent when no rule
    matched.
  - `route_declined: [{rule, why, model?}]`: every other rule whose AI question
    was answered no about this job, in rule-name order. A job no rule started
    says why with it. Absent when there is none.
- `factory.job_steps`: `steps_done` and `steps_total`, always present, THE ONE
  STEP COUNT. The run's steps, then switched-on steps it has not reached; a
  skipped step counts for neither side. The Brief's `steps_done`/`steps_total`
  read the same function, and `step.index`/`step.count` stay the run's
  position, never an "N of M".
- `factory.job_runs_on`: `runs_on: {model, from}` on a read job (its current
  conversation step's model, else its next one's, else its default) and on each
  conversation step that has not finished. It is the resolution the runner
  opens the step on. `from` names the rung: `step` (a person's pick for the
  step), `job` (for the job), `one_model` (the runner runs every task on one
  model), `effort` (the model the step's effort word runs on), `factory` (the
  factory's default), `worker` (the person's worker model on the runner) or
  `runner` (the runner's launch model). Absent where this factory cannot know
  it (no pick, no default, its runner elsewhere). `model` keeps meaning the
  person's pick for the job, `factory_model` the default it was queued under,
  and `step.model` the model a step ran on; a worker's report never carries
  `runs_on`.
- `factory.job_merge_offered`: `merge_offered_at` (RFC 3339) is when the job's
  manager last offered the person the merge of its open pull request again
  (`factory_answer` action `offer_merge`, through `POST
  /v1/jobs/{id}/offer_merge`), which it does when the person asks to merge or
  whether the job is ready to, and only while the merge gate offers Merge. A
  client brings the job's Merge card up again, as it stands now, when the value
  moves. Absent when never re-offered. It merges nothing.

Public acts (no hello word yet; the app does not read them in this phase). A
job carries `acts`, the public acts on its own pull request, oldest first:
those that wait for the person's yes, those the factory is carrying out with
the person's GitHub sign-in, and the last 20 that are over. Absent when the job
has none.

- Each act: `id`, `kind` (`edit_pr`, `change_base`, `reply`, `comment`,
  `resolve_thread`, `rerequest_review`, `rerun_checks`, `mark_ready`), the
  kind's own content (`title`, `body`, `base`, `thread`, `text`, `reviewers`),
  `by` (`main_agent` or `you`), `why`, `at`, `state` (`proposed`, `approved`,
  `running`, `done`, `failed`, `declined`), `approved_by`, and once it is over
  `result` (what it did, or the person's words on a decline), `error` (why it
  failed), `url` and `done_at`. `text` is a reply's or a comment's words
  exactly as they are posted, with the footer line "Posted by a WorkAF factory
  agent." already on. `after_push` is the commit whose push an approved act
  waits for.
- While acts are `proposed` the job's `wait` is `{who: "you", action:
  "approve_acts", words: "Approve: post 2 replies and change the base to
  dev?"}`, unless a narrower action of the person's is already waiting (a push
  to approve, a merge, the budget, a question): that wait stays and its `words`
  name the acts. `offers` then carries `acts_approve` and `acts_decline`.
- `POST /v1/jobs/{id}/act` with `{acts: [{kind, ...}], by, why?, expect?}`
  proposes acts; an act is approved at once when `by` may (the person, or the
  job's manager in Auto) and waits as `proposed` otherwise. `push`, `open_pr`
  and `merge` are accepted as kinds and never stored: a push presses or points
  at the waiting publish approval, and a merge only offers the merge card
  again. A proposal with one act the job refuses is refused whole.
- `POST /v1/jobs/{id}/acts_approve` with `{ids?, expect?}` is the person's one
  yes to every act that waits (`ids`, when sent, must be exactly the acts
  waiting, or the answer is 409); `POST /v1/jobs/{id}/acts_decline` with
  `{words?, expect?}` is their one no. Approving a waiting publish approval
  (`approve`) approves the waiting acts with it; they are carried out after
  that push has landed.
- All three answer `{factory, job, results: [{id?, kind, state, words}],
  waiting?, acts?, wait?}`. `expect` is the job's version as a number, or the
  usual `{version, state, step}` object.
- `pr.feedback_items[]` gains `Thread` (the review thread's node id),
  `CommentID`, `ReplyTo`, `By`, `Association` (GitHub's word for the author's
  standing) and `Untrusted` (anyone who is not an owner, member or
  collaborator); `pr.reviews[]` gains `association` and `untrusted`; `pr.title`
  is the pull request's title once an act has edited it.

An extra task (no hello word yet). `POST /v1/jobs/{id}/task` with `{words,
now?, by?, expect?}` sends a delegate into the job's checkout with `words` as
its instructions; `by` is `main_agent` or `you`. It answers `{factory, step,
runs}`: `step` is the step it runs as (`task`, `task2`, …, or `followpr` on a
job whose pull request is open) and `runs` is `now` or `after_round`. A
refusal is 409 with the reason in `error`. A task step is on the job's `steps`
with its instructions as `ask` and who asked as `by`; a task that waits for a
running Follow PR round is `follow_up.next`. `offers` carries `task` wherever
one is taken.

A stop the job's manager decides first (no hello word yet). A job that stops
on something its manager may decide (out of rounds, a step's question, a failed
step, results not saved, a failed publish that was approved, a Follow PR stop)
stores `wait: {who: "main_agent", what: <the action the person would be asked
for>, deadline}` and is read by the app as running, with `state_presentation`
`{kind: "running", title: "Running", detail: "The main agent is deciding what
to do."}` and no `needs_you`; `wait` and `offers` stay as stored. When the
manager's one turn has not moved the job, the job reads as it would have:
`needs_you`, and `wait.who` `you`. `POST /v1/jobs/{id}/manager_decide` with
`{decision, step?, words?}` is the manager's decision: `another_round`,
`send_back`, `rerun`, `accept`, `resume_pipeline`, `retry_publish`,
`wait_github` or `hand_to_person`. A decision that does not fit the stop, a
third time the manager puts one step back to work, and any stop that is the
person's alone (an approval of something public in Manual, the merge, the
budget, a take-over) are refused with 409 and a sentence in `error`.

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

## factory.job_context_set

Hello feature `factory.job_context` and capability `job_context` announce this
write. `factory.job_create` refuses `context` as an unknown field.

| Parameter | Rule |
| --- | --- |
| `factory`, `job` | One attached Factory and a positive job number. |
| `expect` | Required `expect.version` from the job read; stale versions answer `stale` without changing attachments. |
| `context` | Required list of at most 20 `{title, markdown, source?}` attachments. Title is 1–200 characters after trimming; markdown must contain more than whitespace (blank markdown is `bad_params` "context markdown must be non-empty"); source is at most 500 characters. Total markdown is at most 100,000 characters, separate from the 20,000-character `text` limit. `[]` clears attachments. |

The engine sends one `POST /v1/jobs/{id}/context` with `{context, expect}`.
The job must be in Backlog or queued with no run (no stream): before its first
run, or after Bring back cleared its last one, so its next run reads it. Caller rules
match model edits: solo callers, the job's person, or anyone when no person is
assigned. A begun job answers `conflict`: "The job has started; its steps
already read their context." Malformed attachments answer `bad_params` before
transport. The Factory validates again and records one activity per replacement.

Job, queue and `current` reads list `context: [{title, source?}]` only.
Worker admission carries full markdown. A non-skipped conversation `plan`,
otherwise the first non-skipped conversation, receives every attachment whole
after notes as reference material. Without a stream only eligible stages count.
Other steps receive none; attachments never enter a GitHub issue, PR or comment.

Reply: `FactoryJobActed` with `action: "context_set"`, `job`, optional `current`.
The mux's 404/405 (including "factory endpoint not found") means `conflict` with
`detail.reason: "factory_too_old"`; JSON "job not found" stays `not_found`.
A lost answer or 5xx is `uncertain`. The engine never re-sends the write.

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
status as `factory.local_status` shapes it. On every OS, the optional
`provider_key`, also returned by `setup.get`, is the launch-cached OpenRouter auth
reading: `state` is `ok`,
`invalid`, or `unknown`, `checked_at` is present after a probe finishes, and
`message` gives the reconnect hint for an invalid key. Starting the engine makes
no key-check request. codeaf checks it once in the background when setup or health
is first read, with a two-second deadline. Reads never wait for the auth probe;
until it completes they say `unknown`. A key handover also starts the check, and
the cache deduplicates later reads for the same key and address for this launch.
A real turn's 401 invalidates the matching cached credential. `codeaf doctor`
checks directly on every OS, only when explicitly invoked.

On Windows, optional `checks` contains the same machine readiness rows as doctor:
git availability,
`git config user.name` and `user.email`, gh availability and `gh auth status`,
and native Git Bash resolution on Windows. Each row has `name`, `state`
(`ok`, `error`, `info`, or `unknown`), `message` and `fix`. Missing gh is `info`.
Each command has a two-second timeout and inherits the user's normal environment;
the shell resolver bounds its git-shim lookup at three seconds. The method opens
no conversation. Linux and macOS omit `checks` and run no git or gh commands
on desk setup/health reads, avoiding gh login Keychain prompts. Explicit
`codeaf doctor` runs machine checks on every OS. Existing wire fields, events and
saved setup retain their meanings; new fields are optional and old payloads still
decode. Fresh setup on every OS proposes the CLI default without replacing a
saved model. Provider health is transient and is not saved in setup. hello says
`health.check`.

## inbox methods

| method | params | result |
|---|---|---|
| `inbox.answer` | `{id, option, scope, text, why}` | `UndoUntilResult` |
| `inbox.back` | `in.IDParams` | `Empty` |
| `inbox.dismiss` | `in.IDParams` | `Empty` |
| `inbox.later` | `in.IDParams` | `Empty` |
| `inbox.list` | `{list, query}` | `ItemsCountsResult` |
| `inbox.open` | `in.IDParams` | `AgentTaskResult` |
| `inbox.undo` | `in.IDParams` | `Empty` |

Hello says `inbox.dismiss` when the engine serves `inbox.dismiss`; a client offers
Dismiss only then.

## keys methods

| method | params | result |
|---|---|---|
| `keys.check` | `in.KeyParams` | `KeyCheck` |
| `keys.remove` | `in.ProviderParams` | `Empty` |
| `keys.set` | `in.KeyParams` | `Empty` |

A credential a door gives an app-run engine (a pasted GitHub token through
`factory.setup_connect`, a Jira token, an AgentField key and node secret through
`factory.agentfield_connect`) is held in memory and handed to the app to keep, BY
PULL, never on an event: the engine emits an empty `credentials.changed`, and the
app's core calls `credentials.take`, which answers each change once, the latest per
provider, as `{changes: [{provider, key}]}` or `{provider, removed: true}`; a change
not taken within 60 seconds is dropped. The core keeps the key in its secure store
under `provider` and hands it back at the next launch with `keys.set {provider, key}`,
which queues nothing. `keys.set` takes `github`, `jira`, `agentfield` and
`agentfield-node` this way, beside the model providers. Hello announces
`credentials.take` when the engine serves it.

| method | params | result |
|---|---|---|
| `credentials.take` | `Empty` | `CredentialsTaken` |
| `credentials.ready` | `Empty` | `Empty` |
| `credentials.confirm` | `{ticket, applied}` | `Empty` |

`credentials.ready` is also a hello feature. The native host calls it after its
secure-store hand-back is complete, including a successful read of an empty store.
Provider key sync stays idle until then. A provider change in `credentials.take`
has a nonzero `ticket` and an `expected_key` (empty when the store had no key).
The host compares the current stored key before persisting the proposed key or
removal, then calls `credentials.confirm` with the ticket and result. The engine
accepts a successful confirmation only while the captured local revision still
matches. Failed writes can retry for the ticket's remaining lifetime. A client
without these methods still uses the original `credentials.take` behavior for
credentials entered through factory doors.

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

A `Model` may also carry `limits`, a list of `ModelLimit` saying where the model
falls short of what the app asks of one. The engine reports the facts and the
app words the warning. `limits` is absent when nothing is known against the
model, and today only a model of an Ollama service (provider `ollama`) can carry
one: on each `models.list` the engine asks that server's own API, at the
service's address without its `/v1`, what it runs now (`GET /api/ps`) and what
it says about each of its listed models (`POST /api/show`). Each question waits
at most 300 ms, all of them 600 ms, inside the list's three seconds; a server
that is absent, refuses or answers in an unknown shape reports no limit.

| `kind` | sent when | other fields |
|---|---|---|
| `small_window` | a window the server is known to give the model is under `needs` | `window`, `needs`, `source`, and `most` when known |
| `no_tools` | the server lists the model's capabilities and `tools` is not among them | none |

`needs` is the window the app's prompts ask for, 32000 tokens (the engine's lean
threshold). `source` says how `window` is known: `loaded` (the server is running
the model with this window and its Modelfile sets no `num_ctx`), `modelfile`
(its Modelfile sets `num_ctx`, which overrides the server's own setting and is
what the app's next turn loads the model with, since the engine's requests set
no window; `window` is the Modelfile's figure whether or not the model is
loaded, because a running window may be another client's) or `last_loaded` (not
loaded, nothing sets it, and `window` is what this engine last saw the server
running the model with in `GET /api/ps`, at a list). A window of `needs` or more
is no limit; a Modelfile's figure is judged clamped to `most`, as the server
clamps it, so a Modelfile asking for more than the model can hold still has a
`small_window` when `most` is under `needs`. A model that is not loaded, sets no
`num_ctx` and has not been seen running has no `small_window`: the server
decides its default window when it loads a model, from the machine's memory or
the person's setting, and its API does not say what that will be. The engine
remembers each model's last window per service address for as long as it runs,
not on disk, and at most a few hundred models' windows, dropping the oldest.
`most` is the most the model itself can hold, when the server says. A model the
server only forwards to another machine has no `small_window`. A server that
sends no capability list has no `no_tools`, and a model whose capability list
lacks `completion` (an embedding model) has no limits at all. A list that runs out of time before
it can ask about a model repeats that model's limits from the last list that
could. Engines before `last_loaded` could send `source` `default` with no
`window`; an app reads it as nothing measured and shows no warning for it.

When a turn ends on an Ollama model the server was not running at the last
`models.list`, or one that list gave a `small_window` of a size other than the
one it had when a turn last raised such a notice, the engine emits
`models.changed` once for that turn: the turn loaded the model, or may have
reloaded it with a larger window after the person raised Ollama's setting, so
the next list reads the window the server really gave it. A small window that
stays the same raises one notice, not one per turn. A model the server lists as
running without a window is not counted as not running, and a model the list
after such a notice still finds not running raises no further notice for not
running until a list sees it running. A turn of a hosted chat, which runs on
its organisation's server, raises none.

## orders methods

| method | params | result |
|---|---|---|
| `orders.change` | `in.orderAt` | `OrderResult` |
| `orders.create` | `OrderCreate` | `OrderResult` |
| `orders.list` | `{agent}` | `OrdersResult` |
| `orders.made_in` | `in.OrdersMadeAsk` | `OrdersMadeResult` |
| `orders.not_now` | `in.orderAt` | `Empty` |
| `orders.pause` | `in.orderAt` | `OrderResult` |
| `orders.preview` | `{when}` | `OrderPreview` |
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

An engine whose hello carries `settings.remote_control` includes
`remote_control: bool` in Settings (`settings.get`, `settings.set`, and
`settings.changed`), always, off as `false`, and `remote_control_pinned_by:
string` only when an environment variable fixes the value, which makes the
switch read only. It is the row the terminal's Settings > Devices tab writes, in
the engine's own profile. `settings.set` takes optional `remote_control: bool`;
any non-boolean, including null, is `bad_params`, and so is the field sent to an
engine without the feature (`settings has no field remote_control`), so a window
draws the switch only when the word is in the hello. A write the environment
pins is refused whole, before any other field of the patch lands, as
`bad_params` with `Remote control is set by CODEAF_REMOTE_CONTROL`. ON applies
to chats opened afterwards: the holder door starts with the next chat the
desktop opens, and chats already open stay as they were. `chats.live` and
`devices.permit` are announced in the hello, which is read once when the engine
starts, so they appear only after the engine restarts; a window that finds the
switch and `devices.permit` disagreeing says so and offers the restart. OFF
applies at once: the door stops handing chats out and every view another
computer has of a chat this desktop holds is ended (that computer reads it as the
chat closing); the desktop's own chats go on and keep what was said.

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
own pin, then the `insight` pin, then a small-work row the person wrote, then
the model the person chose. Since 2026-11-16 the engine has no default model for
`insight` (background seats follow the chosen model), so `default` is always
absent; the key stays in the shape for older clients. `follows: "insight"`
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

`spend.report` cells may carry `library_activity`: `learning`, `answers`, or
`unspecified` for historical memory calls whose activity was not recorded.
Non-Library calls omit it. These cells are parts of the existing totals, never
additional charges. The report retains the same date, model and owner filters,
recorded model names, and unknown-price semantics. Older clients can ignore
this optional field; older engines provide no Library breakdown.


## sync methods

| method | params | result |
|---|---|---|
| `sync.acknowledge` | `in.IDParams` | `Empty` |
| `sync.get` | `{}` | `SyncResult` |

When `sync.storage.live_transfer` is true, the paired computers can move a chat
by blind pass-through while both are running, with sync storage still off.
The field is optional and does not indicate that provider keys can sync.

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

## Transcript entry author (`agents.entry_author`)

An engine whose hello carries `agents.entry_author` sends `author` on the
entries it reads from a conversation's transcript (`agents.entries` and
`entry.added`):

- `person`: a person's message;
- `engine`: codeaf's own words. These are a factory step's opening
  instructions (the step conversation's first message), a `[carry on]` nudge
  in the person's seat (such as a step's reminder to call `stage_result`), the
  factory runner's progress lines, and the session's quiet notes and
  compaction summaries;
- `agent`: the model's answers, updates and tool calls, and a crew's delivery.

A user entry the engine wrote keeps kind `user`, because it is what the model
reads in the person's seat; `author` says it is not the person's, so a client
folds it without reading its words. Absent on entries built outside the
transcript and from an older engine.

`agents.suggest_reply` never offers a draft addressed to the model. A draft
that names a tool of the conversation's belt (an underscored name such as
`stage_result`) or opens with `[carry on]` is dropped where drafts are made,
and the reply is empty.

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
| `credentials.changed` | `Empty` |
| `crew.changed` | `CrewChanged` |
| `crew.item` | `CrewItemEvent` |
| `crew.log` | `CrewLogEvent` |
| `crew.proposal.delta` | `CrewProposalDelta` |
| `crew.removed` | `CrewRemoved` |
| `decision.changed` | `DecisionEvent` |
| `decision.opened` | `DecisionEvent` |
| `deliverable.changed` | `DeliverableChanged` |
| `devices.changed` | `DevicesResult` |
| `entry.added` | `EntryAdded` |
| `factory.foreman_changed` | `FactoryForemanChanged` |
| `factory.jobs_changed` | `FactoryJobsChanged` |
| `factory.org_changed` | `FactoryOrgChanged` |
| `factory.org_presence` | `FactoryOrgPresence` |
| `factory.org_workstream_agent` | `FactoryOrgWorkstreamAgent` |
| `factory.org_workstream_task_work` | `FactoryOrgTaskWork` |
| `factory.org_doc_awareness` | `FactoryOrgDocAwareness` |
| `factory.org_doc_closed` | `FactoryOrgDocClosed` |
| `factory.org_doc_update` | `FactoryOrgDocUpdate` |
| `factory.sessions_changed` | `Empty` |
| `factory.event` | `FactoryEvent` |
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

`turn.tool` optionally carries `spent_usd`, the conversation's live spend when
the step was said (the same meter as `agents.budget.get`'s `engine.spent_usd`,
which can be ahead of the saved `Agent.spent_usd`); zero or unknown readings and
history rows omit it, and absence means the engine did not say.

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
array, including when empty. Final states emit exactly once. Joining with live
work is `busy`. A join refuses what this computer already holds, and changes
nothing: chats that already sync with other computers, or chats of its own. It
refuses before the other computer is asked whenever this computer's directory
says so, and otherwise after the approval; either way the failure's `error.code`
is `cannot_join`, because a new code or link would end the same way. The engine
no longer asks `replace_own_chats` (the kind stays for older engines). The
computer that showed the code finishes `done` only once the joining computer
says it kept what it was sent; without that word it fails, with a message that
names the other computer, including when the person cancels after answering yes. While `offer_code` shows a code, a computer that has
never paired keeps Sync and Devices in mode `quiet`: they turn `on` only when a
person answers yes and its identity is sent, and stay `on` after that even if
the pairing then fails (both are emitted again before that failure). A code that
is cancelled, runs out, is answered no or is used up by a wrong code leaves them
`quiet`. A join that changes this computer's shared keys finishes with
`restart:true`: restart the engine before sending work. Device and sync
snapshots precede done.

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
without a directory read. While live transfer is active, the follower also
refreshes cached titles from paired holders over the encrypted files door.
The titles are never added to directory records. Kept branches are
outside this v1 inventory. Rows read from this computer's own cells carry crew facts
behind `chats.crew` (below); relay rows carry no crew membership, so the local
Agent keeps its existing crews.

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
`chats.preview` in `hello` advertises a read of the selected saved conversation.
The optional row field `can_preview` says a saved head appears readable. The
result contains short text excerpts, with either field omitted when empty.
The read uses store frames or the holder’s encrypted files door. It takes no
lease and installs no local copy of the chat or its workspace.

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
cancelled `agents.take` answers `engine` with the cancelled sentence. A lease
the move had already acquired is handed back. A second `agents.take` of a chat
already moving answers `busy` ("This chat is already moving to this
computer."); the first keeps its Cancel and its outcome.

`sharing:{pending, sent, total}` is on a row whose holder (another computer)
has `pending` sealed turns it has not finished sharing; a move now would copy
the chat without them. `sent` of `total` bytes is how far the holder's upload
had got at its last beat (every 30 seconds while it changes), absent when the
holder or the relay did not say. While a known computer (`online` present)
holds such a chat, `can_take` is false; a holder whose presence is unknown or
that was removed never finishes, so its chat stays takeable. Directory beats
carry `upload_sent`/`upload_total` only to a relay that answers
`Codeaf-Upload-Progress: 1`, which keeps them on the lease until the next
publish, release or acquire. The record's `size`, set by publishers since
this change, is the bytes of the frames its plan names (`0`: unknown); a move
shows it as `move.total`. A chat shared in the background gets the bytes its
upload attempts put.

`working:true` is on a `running` row whose holder said at its last beat that a
turn is running there; absent while the chat is merely open there, and never
on any other status. The holder beats as soon as a turn starts, and as soon as
the ended turn's last saves are published, so a take on seeing the flag drop
names the newest save. `chats.working` in `hello` says rows can carry
it. Directory beats carry `working` only to a relay that answers
`Codeaf-Working-Beats: 1`; the lease keeps it through publishes until the next
beat, and a release or an acquire clears it. Behind a relay that does not keep
it, rows never say `working`, so a client says "open there", not "idle there".
It is display only: `agents.take` does not consult it.

`chats.send {cell, text, client_id?}` leaves a message for whichever computer
holds the chat; `chats.send` in `hello` says it exists, and it is absent while
the account's hosted sync storage is off, because a waiting message is sealed
text kept on the relay (a send that arrives anyway is refused with "Sync storage
is off for this account, so a message can't wait for another computer. Bring the
chat here to write to it." and leaves nothing there; a self-hosted relay and an
approved account always have it). The message travels in
this device's own directory record (`outbox`, sealed under the metadata key,
at most 8 messages of at most 2000 bytes), only to a relay that answers
`Codeaf-Chat-Notes: 1`; a relay that does not is refused with "This sync
service can't carry a message to another computer yet." The engine holding the
chat reads it with the chat list, puts it in that chat's queue (`agents.queue`,
client id `note:<id>`), so it is sent when the turn there ends or at once when
none runs, and lists the id in its own record's `took`; the sender then drops
it. The sender's row carries it in `notes [{id, text, at, state}]`: `waiting`
while it is in the outbox, then `delivered` once taken, kept while that engine
runs (20 per chat) and dropped once the chat is here. The
chat, not the computer, is the address: a message not yet taken goes to
whichever computer holds the chat when it is read, including the sender after a
move. Only an engine that keeps the chat list (the app) takes messages.

`chats.read {cell, before?, limit?}` answers `{entries, more, before?, head,
sealed_at_ms?, fetched_bytes}`: one page of the transcript of a chat another
computer holds, as that computer last sealed it, without taking the chat. A
turn running there now is not on it. `entries` are the same `Entry`
`agents.entries` answers for a local chat, oldest to newest within the page;
the first call (no `before`) is the newest page, and `before` from a page is
the opaque cursor of the next older one, read at the same `head` even after
newer turns land. `more` is false once a page reaches the start. A page holds
at least `limit` entries (default 50) unless it reaches the start, and never
splits a turn, so it can hold more. Entry ids are unique within a page read;
a client replaces its pages when `head` changes. Turn footers, notices and tool
output facts are local records and are not on a remote page. `fetched_bytes`
is what the call pulled from the relay (0 when every byte was held). It fetches
only the transcript's own objects and the chunks under the page, into the
store a later `agents.take` of the chat fetches into, so continuing the chat
afterwards copies only the rest. No lease is taken, nothing is restored, no
agent is opened. `chats.read.paged` in `hello` says the method exists: only
when a relay is configured and the engine's furrow can read one path of a
snapshot. It is a word of its own. The older `chats.read` word means something
else and is advertised on its own terms: that `agents.entries` on a remote row
follows a saved remote head without taking the chat. An engine may say either,
both or neither, so a client checks `chats.read.paged` before calling the
`chats.read` method and `chats.read` before relying on `agents.entries` for a
chat held elsewhere. The `chats.read` method and the older `chats.read` hello word
are different things that share a name: the method reads a page, and the word only
says `agents.entries` follows a saved remote head.

`chats.crew` in `hello` says every `chats.list` row and `chat.changed` row
carries the crew its chat belongs to on its holder, additively: `crew` (the
crew id), `crew_name`, `crew_manager:true` for the crew's manager chat (the one
to `chats.send` to), and `crew_kind:"factory"` for a factory item's crew (absent
for an ordinary crew). All four are absent for a chat in no crew. A chat this
computer's teams store lists (one here, or a member that moved away) takes its
crew from that store; a chat held elsewhere takes it from what its holder sealed
into the chat's directory record at its last saved turn (`Codeaf-Chat-Crews: 1`;
publishers send the sealed crew only to a relay that answers it), so it stays
while the holder is offline, and a rename or removal there arrives with that
chat's next saved turn. An older holder or relay says nothing, and the row is
loose. A chat is shown under one crew: the one it manages, else the one it
reports into, never `All teams`. A crew held elsewhere is read-only here in
this version: `crews.list` and `crews.get` do not list it, no crew verb acts on
it, and `agents.take` still moves one member as a single chat.
Example row: `{"cell":"01M7H89T8KA9W804DZX67NFAAT","title":"Todo CLI in
Python","here":false,"status":"running","crew":"da48d479ea82","crew_name":"Todo
crew","crew_manager":true,...}`.

Remote control (a live view). `chats.live` in `hello` says `chats.open_live
{cell}` and `chats.live_keyboard {agent, take?}` exist and rows may carry
`can_live:true`. It is absent unless the engine can dial the live door AND the
person turned Remote control on, so a client that never sees it must never send
either call. `can_live` is true on a row whose chat another computer holds and
runs while that computer is online. `chats.open_live` opens that chat WITHOUT
moving it and answers an `agent`, a local-looking one: its `location` carries
`here:false`, the chat's `cell`, `holder`, `can_take` and `seen` as for any held
chat (so `agents.take` still works from it) plus `live`. Everything the app does
in a chat is the agent's own call: a live send is `agents.send` on that agent,
never `chats.send`; steer, queue, stop, close and `asks.answer` likewise; older
messages are `agents.entries {before, limit}`. Opening it again is how a view
that detached or went offline is repaired, and answers the same agent id. It
refuses with `not_found` (the chat is not listed), `unreachable` (the computer
is not on, is not running that chat, or needs a restart, each in its own
sentence), or `conflict` (that computer does not share its chats with this one,
or this computer was removed). Every refusal carries the sentence a person
reads and `detail:{kind:"live_refused", reason}` with the closed reason.

`location.live` is `{state, driver?, road?, seen_at?, permit?}`. `state` is
`live`, `reconnecting`, `offline`, `idle` (the view detached after ten quiet
minutes and sent nothing first) or `watching` (the holder allows reading and not
typing, or its daily allowance is used). `driver` is `{yours?, machine?, here?}`
and is present only when another window holds the keyboard; `chats.live_keyboard
{agent, take:true}` asks for it and answers `{driver}`. `road` is `relay`.
`seen_at` is RFC 3339. `permit` is `{watch, drive, reason?}`; `reason` is the
finished sentence for why typing is off, shown verbatim. Every change to any of
these arrives as one `agent.changed` carrying the whole agent; there is no new
event and no field on `turn.delta` or `turn.thinking`. A live agent is left out
of `agents.list`.

`location.keyboard` is `{yours?, machine?, here?}`, the same three words as a
live view's `driver`, on a chat THIS computer holds and runs when another window
than the one drawing it has the keyboard: another of the person's computers that
took it, or another window here. It rides on the existing `agent.changed` of
that agent and nowhere else, with Remote control on (`chats.live` in `hello`);
absent says the window drawing the chat holds the keyboard, which is every chat
nobody else has joined, and with Remote control off no agent ever carries it. A
window that sees it draws who is typing and offers one button, which is
`chats.live_keyboard {agent, take:true}` on the held agent itself (not a live
one): it asks the engine for the keyboard and answers `{driver}` as it does for
a live view. A held agent whose chat is not open to other computers answers
`conflict` as before.

`devices.permit {id, watch?, drive?}` edits ONLY this computer's own book of what
a paired computer may do in chats here (`devices.permit` in `hello`, absent while
Remote control is off); a switch left out keeps what it was. Each device row
then carries `permit:{watch, drive}` (absent on this computer's own row and on a
removed one). The answer is `{device?, line?}` and `devices.changed` follows
with the whole roster. A permit edited by the terminal's `codeaf devices permit`
arrives the same way.

Before a move, a row says what it would copy: `folder` is the name of the
folder the chat works in on its holder (absent for a chat in its own private
folder, from older publishers, or from a relay that does not answer
`Codeaf-Chat-Folders: 1`; publishers send the sealed name only then), and
`size` is the record's size, absent when unknown or when this computer already
has a copy of the chat (a move then copies only what changed). The desktop asks
before moving a chat whose `size` is over 100 MB; the terminal's continue card
says it.
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

## Saved agent spend

`Agent.spent_usd` is the conversation's cumulative spend saved in `meta.json`.
`agents.list` carries its disk row's saved figure. `agents.open` reads the saved
figure from disk, including while the engine holds or is opening the conversation.
Every `agent.changed` reads it in the same step that sends the event, and
`agents.take` answers through `agents.open` and carries it too. Other method
answers omit `spent_usd`, so the window keeps its last figure. A zero
figure is omitted.
Turn-end saves and late recovered receipts update it; a save sends one
`agent.changed` if the saved figure differs from the figure last said. Duplicate
or delayed news sends nothing when that figure has already been said.
An answer dressed before an event may reach the window after it, as for every
other field of an answer. `agents.budget.get` keeps the live meter in
`engine.spent_usd`, which can include calls not yet saved to metadata.

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

### Open a pull request from a folder job

Hello feature `factory.job_open_pr` advertises the optional method of the same
name. Params are `{factory, job, expect}`. It sends one
`POST /v1/jobs/{id}/open_pr` and returns `FactoryJobActed` with
`action: "open_pr"` and the current job. Only a Personal factory serves it, for a
job made on a folder on its own computer (create's `folder`) that finished on
its local branch. Job reads offer `can.open_pr` only while the folder's `origin`
is on GitHub, GitHub is connected and no pull request was opened from it. It
pushes the job's branch (never forced, the checkout untouched) and opens a draft
pull request; the job's `folder.pr` then names it. A folder with no GitHub
remote, or GitHub not connected, is `conflict`.

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
failed, stopped, closed and done jobs, and a job whose run a change of
assignee or an unassign cut. Running work is refused.

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

## The factory's own manager (`factory.foreman_open`)

The hello feature `factory.foreman_open` announces the factory-wide manager,
titled "Factory manager" (the method keeps its `foreman` name, and its team
handle stays `@foreman`). `factory.foreman_open {factory}` answers `{factory, agent}`: `agent` is
an agent id the desk opens and sends to like any conversation (`agents.send`).
The first call makes the conversation; every later call answers the same one.
Only the factory this engine serves is accepted; any other is `not_found`, as
`factory.job_chats` refuses it.

The Factory manager reads only the floor of the factory home it belongs to, through
`factory_floor`: `ask` is `running`, `queue` (not started, most urgent then
oldest), `waiting`, `needs_you` (waiting on the person now, longest wait
first), `shipped` (shipped today, with pull requests and changes),
`risky`, `cheapest`, `oldest`, `all` or `item`. Every row says the job's stored
wait and the actions it offers the person (the job's `wait` and `offers`). When the person asks, `start`
starts one job and `priority` (`p0`–`p3`, with `item`) sets one job's priority,
each through the same factory verb the job page's own Start and priority send,
as the person. `press` (with `item`) sends, as the person, the job verb of one
action the job offers them now (approve, answer, merge, pr-ready, acts_approve,
task and the rest), and `tell` (with `item`) gives the job's main agent one turn
of its own conversation with the person's words. Both are refused on a turn
that carries no words of the person's own. No new wire method or event is
involved: the desk sees the job change as it does after its own buttons. It
never stops or drops a job. `repos` lists every
repository with its jobs counted by state, `shipped` takes `days` (1 is today
and the default, at most 90) and counts the pull requests merged, and every
answer but `item` and `repos` says the whole factory's counts by state on its
first line. One read or act waits a bounded time on the factory (20 s to read,
60 s to act) and then answers that the factory did not, so a floor that never
answers cannot hold the turn open.

The open is the person's own: a Factory manager they closed or archived is
taken out of Closed and answered open. Opens are answered one at a time, so two
asks at once are answered the same manager.

The Factory manager's conversation is the factory's on disk (a mark beside its
journal), so `agents.open` and every other reading answer it `factory_owned`
after a relaunch too, and it is left out of `agents.list`. A manager made
before the mark existed takes it when the factory starts or when it is next
opened, and an app that had already read it as an ordinary chat is sent
`agent.changed` with `factory_owned: true`.

A manager whose conversation works in a folder the factory refuses for its
chats (the codeaf home, a folder above it, or a folder inside it outside the
factory's own areas; one made by a build that gave it the engine's own folder)
is made again in `<floor>/chats/foreman`. This happens as the factory on this
machine starts, and on `factory.foreman_open` if it had not happened yet, so a
tab the app brings back never writes to the old one as the manager. The
conversation it replaces is stopped and archived after the new one is
answered, on a best effort, and is no longer the factory's: it reads
`factory_owned: false`, as one of the person's own chats in Archived, titled
"Factory manager (earlier)" when its title was still the manager's own.

Whenever the engine replaces the manager (here, by `factory.foreman_fresh`, or
when a start or open finishes retiring an earlier manager left behind by an
interrupted replacement)
it sends the event `factory.foreman_changed {factory, agent, replaced}`:
`agent` is the manager now and `replaced` the conversation that no longer is.
A window showing `replaced` swaps it for `agent`; without the event a tab the
app brought back went on showing the old conversation under the manager's name.

### Starting the Factory manager over (`factory.foreman_fresh`)

The hello feature `factory.foreman_fresh` announces the app's Clear for the
Factory manager. `factory.foreman_fresh {factory}` answers `{factory, agent}`
as the open does, but `agent` is a NEW conversation with only the manager's
brief in it; it takes the manager's place (every later `factory.foreman_open`
answers it) and its `@foreman` handle. It opens on the default model and
settings, not the old conversation's. The conversation it replaces is stopped
and archived after the new one is answered, on a best effort: its history
stays on disk, and it is one of the person's own chats from then on
(`factory_owned: false`). A manager has this where another conversation has
Archive, because it is the factory's one conversation with one door: archived,
that door opened a chat nobody could write to. Only the factory this engine
serves is accepted.
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

### factory.job_accept_step

Hello feature `factory.accept_step` advertises this person action. Params:
`factory`, `job`, and `expect.version`. The result is `FactoryJobActed` with
`action: "accept_step"` and the current job. The desk shows **Accept** on a
stuck step only when `can.accept_step` is true.

It is the answer `accept` to an `out_of_rounds` wait: the step counts as
finished as it stands and the run goes on. The factory offers it only on a step
that judges the work and reported what it found (Test, Review, Proof), before
anything is published. A step that makes the work, one that never reported, a
check the machine could not finish and every wait after publish refuse it.

Each check that was still failing is kept on the step's result as accepted. A
later step is not blocked by the same check failing in the same words, a new or
different failure still blocks, and the pull request lists what was accepted.
Only the person who may answer the job can accept; a manager cannot.

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
| `plugins.connect_local` | `in.PluginID` | `PluginKeyResult` |
| `plugins.set_capability` | `in.PluginCapabilityParams` | `Empty` |
| `plugins.disconnect` | `in.PluginDisconnectParams` | `Empty` |

`plugins.list` returns `{plugins, providers, featured, featured_words}`.
Each plugin has `id, name, kind, category, blurb, connected, key_checkable`, and optionally
`account, key_env, expired, connected_at, key_saved_at, ask: {label, answers?},
key_hint: {words, url?}, also_key, local_login: {account, source}`.
`also_key: true` marks a `browser` plugin that ALSO accepts `plugins.connect_key`.
`local_login` is present when a login already on this computer can be adopted
with `plugins.connect_local`; `source` is `gh`.
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
`{state: "waiting", link, code?}`. The desk opens `link` in the browser.
`code` is a device sign-in's one-time code (GitHub's): the sheet shows it
with a copy control and an open-the-page button, and does not open `link`
before the code is on screen.
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
`plugins.connect_local {id}` adopts the login named by `local_login` and
answers in `connect_key`'s shape: `{checked: true, account}`, or
`{checked: false, error, words}`.
`plugins.disconnect {id, account?}` uses the existing disconnect path.
The optional account must match the stored account.

### GitHub

The `plugins_github` hello feature says this engine lists `github` and serves
`plugins.connect_local`; an engine without it does not list GitHub at all.
GitHub is featured first, category `developer`. Its `kind` is `browser` with
`also_key: true` when the build names the GitHub OAuth app (device flow,
scope `repo`), and `key` when it does not. `key_hint.url` is
`https://github.com/settings/tokens/new?scopes=repo&description=codeaf`.
`local_login` appears when `gh auth status` reports a login GitHub accepts;
codeaf reads gh's token only after `plugins.connect_local`.

`plugins.connect_browser {id: "github"}` answers
`{state: "waiting", link: "https://github.com/login/device", code}`. The
engine polls GitHub at its pace; approval emits `plugins.changed {id}`, and an
expired or declined code emits `plugins.changed {id, error}` with a sentence
("The code expired before it was approved. Start again for a new code." or
"GitHub sign-in was declined, so nothing was connected."). `plugins.cancel`
stops polling. `plugins.connect_key` checks a pasted token with `GET /user`;
a refused token is `refused`, an unanswered check `no_answer`.

THERE IS ONE GITHUB TOKEN. Every way in keeps it where the Factory keeps its
GitHub connection (the profile's `github_token` or the yes to gh, and in app
mode the app's keychain through `credentials.changed`), so connecting in Factory
setup shows GitHub connected here, a setup connect also emits
`plugins.changed {id: "github"}`, and `plugins.disconnect {id: "github"}`
disconnects the Factory too. `plugins.describe` reads capabilities from
GitHub's hosted tool server (`https://api.githubcopilot.com/mcp/`, with the
token as a bearer): reads default to yes, acts to ask. `accounts` is the
GitHub login, and `notes` says the Factory uses this connection while this
app serves or joins the local Factory.

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

## Library

Hello feature `knowledge` exposes the same opt-in profile vault as Memory → Knowledge.
Errors contain no source text or credentials. Stale mutations return `conflict`.

| method | params | result |
|---|---|---|
| `knowledge.snapshot` | `{}` | revision, enabled, build counters, pending, model, page heads |
| `knowledge.read` | `{id}` | current Page including sections, evidence and links |
| `knowledge.search` | `{query, limit?}` | full-text hits; no model call |
| `knowledge.recall` | `{question, scope?}` | answer, abstain, citations |
| `knowledge.edit` | `{id, revision, markdown}` | new revision |
| `knowledge.rename` | `{id, revision, title}` | new revision; changes only the label, retaining page identity, scope, links and generated learning |
| `knowledge.delete` | `{id, revision}` | `{}` |
| `knowledge.build` | `{}` | starts the process-owned build with the profile knowledge model |

| event | data |
|---|---|
| `knowledge.changed` | `{revision}`; refresh the snapshot |

| method | params | result |
|---|---|---|
| `knowledge.settle` | `{id, revision, conflict, keep, note?}` | `{revision}`; keeps the chosen account and replacement history |
| `knowledge.source` | `{id, source}` | `{chat, message}`; exact message locator, or `not_found` |

| method | params | result |
|---|---|---|
| `knowledge.capture` | `{chat, message, text}` | admits a bounded passage from an actual saved agent reply (Keep in Library); with hello word `knowledge.keep_always`, works whether or not learning is on |
| `knowledge.capture` | `{file: {workstream, id, name?}, text}` | the file form (hello word `knowledge.capture_file`): keeps a workstream file's words, at most 64000 characters, with no chat; with `knowledge.keep_always`, works whether or not learning is on |

Both forms are a person's deliberate add. An engine that says `knowledge.keep_always` keeps one with "learn from chats and jobs" off, and writes it into the Library then, scoped to that add's own words: no saved chat is read. With learning on the Library's ordinary build writes it. A desk that does not see the word offers Add to Library and Keep in Library only while `enabled` is true. A guide's head in `knowledge.snapshot`, and its detail in `knowledge.read`, carry `from_chats: true` when learning from chats wrote some of it (any evidence that is not a kept file or passage); the desk marks such a guide "From your chats". The Library's build never reads a conversation marked as a workstream's (`agents.set_workstream`).

The file form names no `chat` or `message`. The engine records the words as a user source of kind `workstream_file`, session `workstream:<workstream>` and id `workstream-file:<workstream>/<id>@<hash of the words>`, so keeping the same words again is one source and keeping an edited file adds a new one beside it. `knowledge.source` on such a source answers `{chat: "", message: "", file: {workstream, id}}`, which the desk opens as that file. A desk that does not see `knowledge.capture_file` in hello leaves "Add to Library" off its file menus.

Settings includes `knowledge_model`; `settings.set {knowledge_model}` changes the same profile row the terminal uses. An empty `knowledge_model` means the Library follows the person's chosen model (the default); clearing it goes back to that. Building the Library never changes a saved model choice.

`knowledge.recall` is an explicit read of saved guides, including while `enabled` is false. It does not enable capture, import chats or publish new knowledge. Automatic memory use remains gated by the profile setting.

Library page detail includes `changes: [{before, after, reason}]` for automatic replacements; `claim_details` and `sources` retain the corresponding claims and chat locators.

## Provider connection add, replace and final save

`providers.list` remains the engine's provider catalog, with `kind`, optional-key,
address, regions and browser sign-in metadata. Compatible local runtimes (LM
Studio, vLLM, llama.cpp) use `custom` with their configured OpenAI-compatible base
URL; Ollama has its own catalog row. `localhost` means the engine's machine.
Connecting never installs a runtime or downloads a model.

`keys.check` and `keys.set` accept optional `mode: "add" | "replace"`.
Omitting it preserves legacy behavior. `add` requires `provider: "custom"` and
an address, and allocates a separate custom connection. `replace` names an
existing custom provider ID. A passing check returns `connection.id`, the
persisted provider and credential identity, alongside its address/region/plan.
Pass that connection unchanged with the same mode, provider, address and key to
`keys.set`. An intervening add that occupies the checked ID refuses the stale
save rather than overwriting it. Recheck before trying a new add.

An interactive save (connection, mode, region or address supplied) proves the
current usable model list before writing the profile or adopting credentials.
A reachable listing without usable model IDs answers `keys.check` with
`ok: false, status: "empty"`; save returns an error and leaves credentials and
connections unchanged. Bare `keys.set {provider,key}` remains offline launch
handover. OpenRouter check also returns a connection ID, so its interactive save
can be distinguished from launch handover. Live catalog model IDs are retained
exactly, and connecting never changes the default model.

The desktop keeps secrets securely under `connection.id` only after engine save
succeeds. Profile storage failure returns an error before credential adoption;
retrying the unchanged connection after repairing storage does not create a
second row. If engine save succeeds but desktop secure storage fails, retry only
secure storage with the held key, rather than sending another add. Ignore
cancelled or superseded UI results. Browser sign-in methods are unchanged.

The hello's `engine.features` includes **`providers.instances`** only when the
engine implements the explicit instance contract. Clients must gate add-another
custom and compatible local preset actions on that exact capability: older
engines may silently ignore the new mode field. `ProviderConnector` alone does
not advertise this capability.

For custom instances, every model returned by `keys.check` and `models.list`
sets `provider` to the persisted provider ID (`connection.id` / `providers.list`
`id`). Model IDs keep their qualified routing prefix and display names remain
human-readable; a routing prefix is not a connection ID.

## Username + password on an organization (`factory_password`)

Hello announces **`factory_password`** when the engine serves the four methods
below; an engine that does not answers them `unknown_method`. Username and
password is the default way into an organization; GitHub's device flow stays as
the second way.

- `factory.access_sign_up {link, caller, username, password, name?}` makes an
  account through an operator claim link or an invitation and answers
  `FactoryAccess`. `state: "joined"` means the account exists and the engine
  now holds its session for that connection (kept exactly as
  `factory.access_sign_in` keeps one; the session credential is never in an
  answer). A refusal is `state: "ready"` with `reason` (`username_taken`,
  `username_invalid`, `password_short`, `password_long`, `password_common`,
  `password_contains_username`, `name_invalid`, `github_link`) and `message`
  in the server's words. `github_link` is an invitation made for one GitHub
  login: it takes no password (its `factory.sign_in` leaves `password` out),
  only that person's GitHub sign-in. An operator claim link is never one of
  these on a server that takes passwords: it is not named for the host's GitHub
  login even when the server holds a GitHub token, so its check lists
  `password` and a password claim is taken (until film D14 such a server's
  operator link answered `github_link`). A recovery link (`--new-operator-link`)
  is still for the operator who claimed. A username already used anywhere on the
  server (a GitHub login that ever signed in, a share, a team, a job) is
  `username_taken`. A link that is revoked, expired or used up answers that
  state.
- `factory.access_reset {link, caller, password}` sets a new password through
  the operator's reset link (`kind: "reset"` on `factory.access_check`'s
  answer); `state: "joined"` signs the person in, and every other session of
  theirs has ended.
- `factory.password_sign_in {factory, caller, username, password}` signs a
  person in again with no link and answers `{state: "signed_in", me}`. A
  refusal is a `forbidden` error carrying the server's sentence: the same one
  for an unknown username and a wrong password ("That username and password
  don't match."). Too many tries is `busy` ("Too many tries. Wait a few
  minutes."): 10 wrong tries of a username from one address, 100 from
  everywhere, or 50 from one address (an IPv6 address counts by its /64);
  and a second sign-in of a username while one is being verified. A machine
  the organization trusts skips the 100: see the device token below. Every session is tied to
  the password it was made with: a change or a reset ends all of them.
- `factory.password_change {factory, current, new}` answers `{}`; every other
  session of the person ends. A wrong current password is `forbidden`; a new
  one that breaks a rule is `bad_params` with the rule's words; a change that
  lost a race with another change is `conflict` ("Your password was changed
  meanwhile; sign in again.").
- `factory.link_create` takes `kind: "reset"` with `person: <handle>` (and no
  other choice): a one-time link for one hour, for a password person only,
  copied like an invitation. Its entry carries `kind: "reset"`.

Fields that say how a person signs in: `factory.list`'s connection `sign_in`
and `factory.access_check`'s `factory.sign_in` (`["password","github"]`,
preferred first, from the server's `/v1/about`; absent means GitHub only);
`factory.me`'s `sign_in_kind` (`"password"`, `"passkey"` or `"github"`); and
`factory.people`'s rows' `kind`. `factory.me`'s `invite_available` no longer
needs GitHub.

THE DEVICE TOKEN. A sign-up, a reset, a change, and a sign-in without a
valid one hand this machine the organization's device token, but only when the
engine asks (`want_device`), and it asks only while the app's core has said
`factory.sessions_store {can_keep: true}`: that it can keep a secret in the
OS's secure store. The core says it at launch, before the windows are let go,
and says `false` the moment a keep is refused; unsaid is false. A person holds
at most 16 live device tokens; past that a sign-in still succeeds but gets
none, and no live token is ever pushed out. The engine holds
it per connection like a session, registers it with its redaction, presents it
on every `factory.password_sign_in` (the server then counts the try by the
machine, not against the username's ceiling of 100, so a stranger guessing at
the name from elsewhere never locks out a machine the person used), and hands
it to the app's core through `factory.sessions_take`: each
`FactorySessionChange` may carry `device`. It outlives the session: a change
with `removed` forgets the session only, and a `device` on it is still kept;
`device_removed` (the connection was detached) forgets the device too.
`factory.session_restore` takes `device` beside `token`, and either alone. A
password change or reset revokes every older device token on the server; a
new machine during an attack waits, or uses the operator's reset link, which
is never throttled.

A password crosses this wire only inward, in `password`, `current` and `new`,
on these four methods; a client's wire trace drops those params by name. The
engine registers each with its redaction the moment it arrives.

## Passkeys on an organization (`factory_passkey`)

Hello announces **`factory_passkey`** when the engine serves the five methods
below; an engine that does not answers them `unknown_method`. A passkey
(WebAuthn: Touch ID, Windows Hello, a phone over QR, a security key, a password
manager) is the first way into an organization wherever the server can offer
one: its address is https with a DNS name. `sign_in` then lists `"passkey"`
first (`["passkey","password","github"]`); a server that cannot offer passkeys
leaves it out, and these methods answer `not_found` from it.

The ceremony runs in the person's **default browser**, on a page the
organization server serves: only a page on the server's own origin may use a
passkey for it (a desk web view cannot, for any domain not in the app's
build-time `webcredentials` entitlement). The engine starts the flow with the
SHA-256 of a 32-byte verifier it keeps and never sends anywhere else; the server
answers the session only to a poll carrying that verifier, so the url alone
cannot mint a session.

- `factory.passkey_start {factory?|link?, caller, purpose, username?, name?}`
  answers `{flow, url, expires_at}`. `purpose` is `sign_in` (with `factory`, an
  attached organization), `add` (with `factory`, signed in: another passkey for
  this person) or `join` (with `link`: an invitation, the operator claim link,
  or an operator's reset link). A join may carry the `username` (and `name`)
  the person already chose; otherwise the page asks. `url` is always
  `https://<the organization's host>/passkey/<flow>` (or a test server's
  `http://localhost:<port>/…`); the engine refuses a start whose url is on any
  other origin, and a client should open it only in the default browser. A link
  that is revoked, expired or used is `conflict` with the server's sentence.
- `factory.passkey_poll {flow, caller}` answers `{state, message?, factory?}`:
  `pending` until the browser has finished; `done` with `factory` = the
  connection now signed in (a join attaches a new one); `expired` (ten minutes
  passed, or the flow is unknown); `refused` with `message`; `elsewhere`
  when the ceremony finished in a browser that has not reached this computer
  (not final: keep polling, or cancel). A client polls about every two
  seconds. Done is answered once; the session stays in the engine exactly as
  `factory.access_sign_in` keeps one. An `add` and a `confirm` sign nobody in.
- Every ceremony ends on this computer. The engine opens a one-shot listener
  on `127.0.0.1:<ephemeral>` under a random path, with a key, for each flow;
  the page's browser (bound to the flow by a cookie on its first ceremony) is
  sent there by a 303 with a one-time code only that listener accepts, and the
  server answers `done` only to a poll carrying that code and the verifier.
  Copy address works in another browser on this computer used from the start;
  on another device the page says why before the browser's "can't connect",
  and "Finish on the computer that started this" after, and the flow never
  completes.
- `purpose: "confirm"` (with `factory` and `action`, one of `add_passkey`,
  `remove_passkey` with `passkey` = its id, or `set_password`) is a step-up
  for that one action: the person uses one of their passkeys on a page that
  names the action and this computer. `factory.password_confirm {factory,
  password, action, passkey?}` answers `{}` and does the same with the
  current password; a wrong one is `forbidden`, a person without a password
  or a `remove_passkey` for a passkey they don't have `not_found`, too many
  tries `busy`. A session holds at most three confirmations; a fourth
  replaces its oldest.
- Adding a passkey, setting a first password and removing a passkey each
  need a confirmation for that action, which lets it happen once within five
  minutes; a sign-in never counts. Without one they fail with `step_up`,
  whose `detail` is `{action, passkey?}`; the client confirms that action and
  retries once.
- A finished flow's code works for 60 s; the loopback answers its code with
  a 303 to a clean `http://127.0.0.1:<port>/done`.
- `factory.passkey_cancel {flow, caller}` answers `{}`; best effort.
- `factory.passkey_list {factory}` answers `{passkeys: [{id, name, created_at,
  last_used_at?, synced}], password}`. `password` says whether the person also
  has a password. A GitHub person has no passkeys here: `not_found`.
- `factory.passkey_remove {factory, id}` answers `{}`; every other session of
  the person ends. Removing the last way in (the only passkey of a person with
  no password) is `conflict`: "This is your only way to sign in. Add a password
  or another passkey first."

`factory.me`'s `sign_in_kind` is how this session was made: `"passkey"` after a
passkey sign-in or join, `"password"` after a password one (the same person may
hold both), with `github_signed_in` false for either; `factory.passkey_list`'s
`password` says whether the person has a password. `factory.password_change` takes an empty `current` from a
person with no password yet, which sets their first one.


### Workstream Fold and imported files

`factory.org_workstream_update` accepts `fold_into` (destination workstream id),
or `unfold` (the source id, on the destination). Fold requires governance on both
workstreams. The response is the destination after Fold, or the restored source
after Undo. `org_workstream_fold_undo` advertises this capability. The source is
absent from both live and archived lists. A durable receipt prevents Undo from
overwriting intervening edits. Fold refuses working tasks and nested sources.

`factory.org_workstream_import` also carries `files: [{file, data?}]`, with upload
bytes in base64, `tasks` and `task_events`. File and task identities, `from_task`
and `used_by` links survive. `to` brings a chat's limited package into an existing
workstream rather than creating one. Imported upload paths are replaced by server
blobs. Repeating a whole-workstream import completes the same destination.

`from_chat` on file creation and file records associates an attachment with its
chat without changing its maker. File-bearing imports share the ordinary upload
request envelope (`uploadBodyMost`) rather than the 1 MiB JSON-only limit.

### Task Delete and short Undo

| method | params | result |
|---|---|---|
| `factory.org_workstream_task_delete` | `{connection, ws, id, rev}` | `{undo_until}` |
| `factory.org_workstream_task_restore` | `{connection, ws, id}` | `OrgTask` |

Hello `org_workstream_task_delete` announces two person-only methods:
`factory.org_workstream_task_delete({connection, ws, id, rev})` forwards
`DELETE /v1/workstreams/{ws}/tasks/{id}` with `If-Match: rev` and returns
`{undo_until}`; `factory.org_workstream_task_restore({connection, ws, id})`
forwards `POST .../tasks/{id}/restore` and returns the restored task. The task's
owner or workstream steward needs edit access. A running task is refused with
`Stop this task before deleting it`. The desk offers Undo for ten seconds; the
server accepts it for thirty. Late Undo returns 409.

Deletion publishes `workstream.changed`, part `tasks`, deleted true. Dependants
lose that wait atomically and get a state-event explanation; agent tasks newly
unblocked wait for a person's Start. Undo restores unchanged dependants' waits
and their start choice. Files and Factory jobs are untouched. Hidden task IDs
remain suppressed across restarts; stale task writes cannot revive them. Afi's
workstream toolbelt exposes neither verb.
