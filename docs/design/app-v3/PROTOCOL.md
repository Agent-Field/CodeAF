# Engine protocol (UI2 ⇄ codeaf), contract 1 (additive revisions keep 1)

UI2's Rust core (`crates/core`) starts the bundled codeaf binary as a child
process, `codeaf app`, and talks to it over the child's stdin/stdout. No port
is opened. The webview never sees codeaf: React → Tauri command → `Backend`
method → this protocol.

Vocabulary: the app says **agent** (a codeaf session / conversation), **task**
(a node in an agent's task graph), **crew** (a codeaf team, `internal/teams`).
codeaf's own "crew" (`internal/crewroute`, model seats) is not exposed here.

## Framing

- UTF-8, one JSON object per line (`\n`), both directions. No other output on
  stdout; logs go to stderr.
- Requests (app → codeaf): `{"id": <u64>, "method": "<name>", "params": {...}}`.
  `params` may be omitted when empty. Ids are chosen by the app, unique per
  process lifetime.
- Responses (codeaf → app): `{"id": <u64>, "result": <any>}` or
  `{"id": <u64>, "error": {"code": "<word>", "message": "<sentence>"}}`.
  Exactly one per request, in any order.
- Events (codeaf → app): `{"event": "<name>", "seq": <u64>, "data": {...}}`.
  `seq` starts at 1 and increases by 1 per event for the process lifetime.
- Unknown request methods answer error code `unknown_method`. Unknown fields
  are ignored by both sides. Unknown event names are ignored by the app.

Error codes: `unknown_method`, `bad_params`, `not_found`, `busy`, `engine`
(the underlying engine failed; message says how), `closed`, and additively
`uncertain`: a mutation whose effect the engine itself cannot confirm (it may
have happened). An app must not retry an `uncertain` call blind; it reads the
state again first. An app that does not know the word treats it as a failure.

## Lifetime

1. App spawns `codeaf app [--home <dir>]`.
2. codeaf writes the first line, an event:
   `{"event":"hello","seq":1,"data":{"contract":1,"version":"<codeaf version>","pid":<int>,"home":"<dir>"}}`
   The app refuses a `contract` it does not know and kills the child.
3. Requests and events flow.
4. codeaf exits cleanly (closing every agent engine it started) when its stdin
   reaches EOF, when the process that started it ends (even while something
   else still holds its pipes), on SIGTERM/SIGINT/SIGHUP, or on request
   `shutdown`. The app closes stdin to stop it, then kills after 8 s. Leaving
   lets go of this machine's factory first (its runner stopped as a quit stops
   it, then its service, `serve.json` and `serve.lock`), so the next codeaf can
   serve it. Leaving is bounded: a codeaf still wrapping up 30 s after it began
   leaving ends anyway, and a second SIGTERM/SIGINT/SIGHUP ends it at once.
5. QUIT PAUSES, REOPEN RESUMES. Before anything stops, codeaf writes down every
   agent in the middle of a reply and every task it had running. A reply is
   stopped where it stands; at the next start codeaf opens that agent, sends
   `entry.added {kind: "notice", text: "Resumed after quit"}` and the reply carries
   on as a turn with no user entry in front of it (`turn.started` … `turn.done`); a
   task the quit left with nothing driving it is picked up where its record stands.
   Every running process gets SIGTERM, then SIGKILL after 5 s, and is listed
   afterwards with `state: "stopped", reason: "stopped by quit"`. Automations and
   standing orders are the store's and are untouched; they fire again once open.

## Types

```
Agent  { id, title, workspace, model?, state, updated_at?, crews?, line? }
       id: stable string (the conversation's session file id/path key)
       state: "idle" | "working" | "needs_you" | "stopped" | "closed"
       "working" whenever a turn runs, on every agent.changed and every list, whichever window
       started the turn (a terminal working in an agent the app closed reads "working" too)
       crews: the open crews it is a member of, on every agent.changed (said again when it joins or leaves one)
       line: its live one-liner: the step it is on while working, or the question it waits on
       "stopped": its conversation engine failed; the next send reopens it
       updated_at: RFC 3339; absent when nobody knows it, never the zero time 0001-01-01T00:00:00Z
       title: never empty on a listed agent; a crew member whose own row has no name goes by its
       crew's word for it (Member `title`), which for one a manager started is its brief's first words
Task   { id, agent, title, state, updated_at, parent?, group?, group_title?, line? }
       parent: the task that spawned it (nesting); group: the id of the handed-over run
       (the engine's one grouping: a run and the workers under it; absent for a lone task);
       group_title: that run's own name, the label to draw over the group;
       line: the one live status line the TUI shows for it
       state: "queued" | "running" | "done" | "failed" | "unverified"
Crew   { id, name, manager?, members: [{agent, handle?}], factory_managed? }
Entry  { id, agent, turn?, kind, at, ... }   one transcript item, in order
       kind "user"     { text }
       kind "answer"   { text, thinking? }
       kind "tool"     { call, name, ok?, summary? }
       kind "receipt"  { text }          e.g. "Allowed once · you · 14:02"
       kind "notice"   { text, ref? }
       kind "error"    { text, error_kind?, status? }
Model  { id, name, provider }
Settings { default_model, budget_usd, asks_before, day_cap_usd }
Spend  { today_usd, day_cap_usd }
Ask    { id, agent, kind, subkind?, title, detail?, options?: [{id, label}], irreversible? }
       subject?: {kind, ref?}: what the ask is about, so the app draws that thing's card —
         "automation" (ref = order id; Save ratifies it, Don't save takes it away), "task" (ref = task id),
         "call" (ref = tool call id), "run", "page", "account", "crew_proposal", "crew_decision" (ref = packet id),
         "budget" (ref = "agent"|"day")
       irreversible: true exactly when the engine itself grades the call irreversible (internal/approval's
         AlwaysAsks: bash's critical-command table, e.g. a recursive forced delete, or a call that acts in the
         person's name); then "always" is not offered
       subkind (approval): "permission" | "budget"; options carry the engine's ids,
       e.g. permission: allow_once · deny · always (absent when irreversible)
       kind: "approval" (a tool call waiting for allow/deny) | "question" (the agent asks the person)
       approval options are the engine's own choices (e.g. allow once / always / deny);
       a question may have options, free text, or both
```

## Methods

| method | params | result |
|---|---|---|
| `ping` | – | `{}` |
| `shutdown` | – | `{}` then exit |
| `agents.labels` | `{ids: string[]}` (at most 32) | `{labels:[{id,title}], incomplete?}`; optional metadata-only lookup, no transcript reads. Missing names stay unknown; incomplete means a scan bound was reached. |
| `agents.list` | `{workspace?}` | `{agents: Agent[]}` (recent conversations, newest first); a blank conversation, one nobody has said anything in, is never listed (see "Blank conversations") |
| `agents.create` | `{workspace?, model?, budget_usd?, text?}` | `{agent: Agent}`; with `text` the first turn starts at once (`turn.started` follows) and `title` is the opening name — the words of `text`, or the names of the files attached when there are none, cut to 40 characters with "…" — until the engine's own name arrives as `agent.changed`: one request to the model configured for titles, asked once from those words and file names only, which replaces it unless the person renamed the agent first. No workspace: the user's home folder. |
| `agents.entries` | `{id, before?, limit?}` | `{entries: Entry[], more: bool}` (oldest first; works for agents not held) |
| `agents.set_model` | `{id, model}` | `{agent: Agent}` |
| `agents.retry` | `{id}` | `{turn}` (re-run the last failed turn) |
| `agents.open` | `{id}` | `{agent: Agent}` (attach an engine to a saved conversation) |
| `agents.send` | `{id, text}` | `{turn: <u64>}`; output arrives as events |
| `agents.stop` | `{id}` | `{}` (interrupt the running turn) |
| `agents.close` | `{id}` | `{}` (detach and stop its engine; conversation stays on disk) |
| `asks.list` | `{agent}` | `{asks: Ask[]}` (open asks only) |
| `asks.answer` | `{agent, id, option?, text?}` | `{}`; `not_found` if already answered elsewhere |
| `tasks.list` | `{agent}` | `{tasks: Task[]}` |
| `models.list` | – | `{models: Model[]}` (what the configured keys can use; `[]` with no provider connected) |
| `settings.get` | – | `{settings: Settings}` |
| `spend.today` | – | `{spend: Spend}` |
| `crews.list` | – | `{crews: Crew[]}` |

## Events

| event | data |
|---|---|
| `hello` | see Lifetime |
| `agent.changed` | `{agent: Agent}` |
| `turn.started` | `{agent, turn}` |
| `turn.delta` | `{agent, turn, kind: "text" \| "thinking", text}` |
| `turn.tool` | `{agent, turn, run_id?, call, name, phase: "begin" \| "end", ok?, summary?}` |
| `turn.done` | `{agent, turn, ok, error?, error_kind?: "provider" \| "budget" \| "stopped" \| "uncertain" \| "other", status?}` (provider: the provider failed 3 times in a row; message is the provider's own words. uncertain: a created agent's first message was handed to its conversation and no answer came back — it MAY have been sent, so `agents.retry` is not armed for it) |
| `ask.opened` | `{ask: Ask}` (agent state becomes `needs_you`) |
| `ask.closed` | `{agent, id, by: "app" \| "elsewhere" \| "stopped", option?, at}`; a receipt Entry follows as `entry.added` |
| `entry.added` | `{entry: Entry}` (user message, final answer, receipt, notice, error; streaming text still uses turn.delta) |
| `spend.changed` | `{spend: Spend}` |
| `task.changed` | `{task: Task}` |
| `crew.changed` | `{crew: Crew}` |
| `crew.removed` | `{id}` |
| `log` | `{level: "info" \| "warn" \| "error", message}` (non-fatal notices) |

## The schema, and the index of every method and event

`codeaf app --schema` prints the whole wire as one JSON Schema (draft 2020-12), generated from
the Go types the server decodes requests into and answers with, and from the type each event's
data is: `methods.<name>.params`, `methods.<name>.result`, `events.<name>` and the named types
under `$defs` (a type reached from params is defined apart, under `in.<Name>`, with no field
required: a request may leave any field out and the method says which it needs). It is the
authority on every field's JSON type: a field is a `boolean` there or it is not, and a string
field that holds one of a fixed set of words carries that set as `enum`. Where this document and
the schema disagree, the schema is what the server does. The index below is that schema's
method and event lists, and `internal/appwire`'s tests fail when a method or event is served
and not listed here, or listed and not served. Params are the fields a request may carry; in a
result or an event, `?` marks a field that may be absent.

| method | params | result |
|---|---|---|
| `agents.archive` | `{ids}` | `{}` |
| `agents.close` | `{id}` | `{}` |
| `agents.budget.get` | `{id}` | `{budget}` |
| `agents.budget.set` | `{id, usd}` | `{budget}` |
| `agents.compact` | `{id}` | `{}` |
| `agents.create` | `{attachments, budget_usd, client_id, model, said, text, workspace}` | `{agent}` |
| `agents.entries` | `{before, id, limit}` | `{entries, more}` |
| `agents.idle` | `{days}` | `{ids}` |
| `agents.labels` | `{ids}` | `{incomplete?, labels}` |
| `agents.list` | `{query, state, workspace}` | `{agents}` |
| `agents.move_to_closed` | `{ids}` | `{}` |
| `agents.move_to_task` | `{id, turn}` | `{moving?, task?}` |
| `agents.open` | `{id}` | `{agent}` |
| `agents.queue` | `{attachments, client_id, id, reads, said, text}` | `{queued}` |
| `agents.rename` | `{id, title}` | `{agent}` |
| `agents.reopen` | `{id}` | `{agent}` |
| `agents.retry` | `{id}` | `{turn}` |
| `agents.send` | `{attachments, client_id, id, reads, said, text}` | `{turn}` |
| `agents.set_effort` | `{effort, id}` | `{agent}` |
| `agents.set_model` | `{id, model}` | `{agent}` |
| `agents.since` | `{after, id}` | `{at?, entries, files?, first?, found, last_at?, turns}` |
| `agents.steer` | `{attachments, client_id, id, reads, said, text}` | `{}` |
| `agents.stop` | `{id}` | `{}` |
| `asks.answer` | `{agent, batch_item, id, later, option, text, why, you_decide}` | `{}` |
| `asks.defer` | `{agent, id}` | `{ask?}` |
| `asks.hold` | `{agent, id}` | `{}` |
| `asks.list` | `{agent}` | `{asks}` |
| `asks.shown` | `{agent, id}` | `{ask?}` |
| `attachments.add` | `{agent, kind, path, text, url}` | `{attachment}` |
| `attachments.remove` | `{agent, id}` | `{}` |
| `caps.raise` | `{for, id, scope, to_usd}` | `{}` |
| `crews.archive` | `{id}` | `{crew}` |
| `crews.close` | `{id}` | `{}` |
| `crews.create` | `{agents, attachments, cap_usd_day, client_id, edits, ends_at, from, goal, hue, manager, manager_starts_members, model, name, parent, proposal, request_id, said, settings, start_manager, tint, workspace}` | `{crew, start?}` |
| `crews.decide` | `{option, packet, reason, text}` | `{decision}` |
| `crews.decisions` | `{all, id}` | `{decisions}` |
| `crews.defaults` | `{}` | `{defaults}` |
| `crews.delete` | `{id}` | `{ids}` |
| `crews.get` | `{id}` | `{crew}` |
| `crews.items` | `{id}` | `{items}` |
| `crews.link` | `{id, kind, ref, title}` | `{item}` |
| `crews.archived` | `{}` | `{crews}` |
| `crews.overview` | `{parent?, kind?, limit?, cursor?}` | `{revision, parent, kind, root, teams, members, total, next?}` — one bounded, read-only page of the teams directly under `parent` (`kind` `teams`, the default; omitted `parent` is the real root's children, or the top level when there is no root) or of one non-root team's direct members (`kind` `members`, `parent` required). `limit` is 1 to 100, default 25. `root` is the file's real root `TeamSummary` or `null`; `teams`/`members` are always arrays; `total` counts the paged collection only; `next` is absent only on the last page and is bound to `revision` (the SHA-256 of the exact bytes read), kind, parent and offset — a changed file answers `stale_cursor` and the caller restarts from the first page. `TeamSummary` is `{id, name, parent, root?, hue?, manager?, face_members?, member_count, child_count, made_at?}` (direct distinct members; direct open children; `manager` is the stored manager, always one of the members; `face_members` is the manager first, then distinct stored members, at most 3, absent for a team with no members — older engines omit it). `OverviewMember` is `{agent, handle?, title?, joined_at?}`. Spend, activity and latest updates are never present: unknown is absent, never zero. One read of `teams.json` of at most 2 MiB under a 2 s deadline, no repair, no migration, no write, no watcher or session; a missing file is an empty answer, a file over the limit `too_large`, a newer build's file, a malformed one or a broken tree (duplicate ids, missing parents, cycles, a top team beside the root, a closed root, an open team under a closed one, a stored manager who is not a member) `engine`. Unknown parent `not_found`; asking for a closed parent or the root's members `bad_params`. An engine that does not serve it answers `unknown_method` (check `hello.features` for `crews.overview`). The deadline cancels the engine's read; there is no request-cancel message. |
| `crews.list` | `{closed}` | `{crews}` |
| `crews.log` | `{after, id, limit}` | `{cursor, lines}` |
| `crews.log.at` | `{agent, post, crew_name?, crew?, around?}` | `{anchor: {crew, closed?, lines}}` — one line a conversation's own record points at (a handoff's or a delivery's `post`) with up to `around` (default 2, ≤10) neighbours each side, read by seeking to its id. The crew is the one the agent is a member of, with that name, whose log holds that id written by or to it; never matched by words or time. With `crew` (a handoff's, for a post up) that crew's log is read when the agent is in it (any line written by or to it) or in a crew under it (only a line naming its own membership); `crew_name` is then not matched. `not_found` when the line left the log, the crew is gone, or the agent left the crew or is in neither; `conflict` when two of its crews qualify |
| `crews.manager` | `{agent, id}` | `{crew}` |
| `crews.message` | `{attachments, client_id, crew, reads, said, text, to}` | `{entry}` |
| `crews.move` | `{ids, parent}` | `{}` |
| `crews.propose` | `{agent}` | `{}` |
| `crews.reopen` | `{id}` | `{crew}` |
| `crews.root.start` | `{model, workspace}` | `{crew, manager, root_created, manager_created}` |
| `crews.set_default` | `{key, value}` | `{defaults}` |
| `crews.unlink` | `{id, item}` | `{}` |
| `crews.update` | `{clear, ends_at, goal, hue, id, name, settings, tint}` | `{crew}` |
| `crews.wrap_up` | `{id, text}` | `{}` |
| `files.complete` | `{agent, limit, path}` | `{dir, entries, more?, state?}` |
| `files.diff` | `{agent, base, path}` | `{diff}` |
| `files.find` | `{agent, limit, query}` | `{groups}` |
| `files.read` | `{agent, limit, offset, path}` | `{file}` |
| `files.stat` | `{agent, paths}` | `{files}` |
| `inbox.answer` | `{id, option, text, why}` | `{undo_until}` |
| `inbox.back` | `{id}` | `{}` |
| `inbox.later` | `{id}` | `{}` |
| `inbox.list` | `{list, query}` | `{counts, items}` |
| `inbox.open` | `{id}` | `{agent, task?}` |
| `inbox.undo` | `{id}` | `{}` |
| `keys.check` | `{address, connection, key, provider, region}` | `{connection?, message?, models?, ok, status?}` |
| `keys.remove` | `{provider}` | `{}` |
| `keys.set` | `{address, connection, key, provider, region}` | `{}` |
| `media.get` | `{agent, source}` | `{source}` |
| `media.resolve` | `{agent, path, expect_version?}` | `{source}` |
| `media.attach` | `{agent, source, version}` | `{attachment}` |
| `media.attach_frame` | `{agent, source, version, at_ms, png}` | `{attachment}` |
| `media.attach_range` | `{agent, source, version, from_ms, to_ms}` | `{attachment}` |
| `media.list` | `{agent, cursor, limit}` | `{incomplete?, next?, sources}` |
| `deliverables.list` | `{scope, agent, crew, member, kind, state, live, query, cursor, limit}` | `{deliverables, next?, incomplete?}` |
| `deliverables.count` | `{scope, agent, crew, member, kind, state, live, query, cursor, limit}` | `{count, next?, incomplete?}` |
| `deliverables.get` | `{agent, id}` | `Deliverable` (bare, not wrapped) |
| `deliverables.read` | `{agent, id, version, max_bytes}` | `{text, bytes, truncated?, version}` |
| `deliverables.keep` | `{agent, id, dir, name, expect_version}` flat | `Deliverable` (bare); `kept.path` is the verified receipt |
| `deliverables.remove` | `{agent, id}` | `{}` |
| `deliverables.snapshot` | `{agent, path, url, process}` | `Deliverable` (bare) |
| `branches.list` | `{agent, cursor, limit}` | `{repo, head?, recorded?, branches, next?, incomplete?}` |
| `checkout.status` | `{agent, cursor, limit}` | `{repo, head?, branch?, detached?, unborn?, upstream?, ahead?, behind?, operation?, entries, counts?, revision?, next?, incomplete?, land}` |
| `branches.land.plan` | `{agent, branch}` | `{plan, revision, expires_at, task, title?, branch, branch_head, base, target, head, repository, fast_forward, files?, added?, removed?, counts_capped?, hooks}` |
| `branches.land` | `{agent, plan, revision}` | `{branch, target, head_before, head_after, fast_forward}` |
| `checkout.diff` | `{agent, path, revision?}` | `{diff, entry, revision, repo, workspace_path?, open?}` |
| `checkout.locate` | `{agent, path, revision?}` | `{entry, revision, repo, workspace_path?, open?}` |
| `members.add` | `{agent, brief, cap_usd, crew, handle, model, reason, role, to}` | `{member}` |
| `members.cap` | `{agent, brief, cap_usd, crew, handle, model, reason, role, to}` | `{member}` |
| `members.handle` | `{agent, brief, cap_usd, crew, handle, model, reason, role, to}` | `{member}` |
| `members.home` | `{agent, brief, cap_usd, crew, handle, model, reason, role, to}` | `{member}` |
| `members.move` | `{agent, brief, cap_usd, crew, handle, model, reason, role, to}` | `{member}` |
| `members.pause` | `{agent, brief, cap_usd, crew, handle, model, reason, role, to}` | `{member}` |
| `members.remove` | `{agent, brief, cap_usd, crew, handle, model, reason, role, to}` | `{member}` |
| `members.resume` | `{agent, brief, cap_usd, crew, handle, model, reason, role, to}` | `{member}` |
| `members.role` | `{agent, brief, cap_usd, crew, handle, model, reason, role, to}` | `{member}` |
| `members.start` | `{agent, brief, cap_usd, crew, handle, model, reason, role, to}` | `{member}` |
| `members.stop` | `{agent, brief, cap_usd, crew, handle, model, reason, role, to}` | `{member}` |
| `memory.forget` | `{id}` | `{}` |
| `memory.list` | `{limit}` | `{memories}` |
| `models.list` | `{}` | `{models}` |
| `models.pin` | `{ids}` | `{settings}` |
| `orders.change` | `{edits, id}` | `{order}` |
| `orders.list` | `{agent}` | `{orders}` |
| `orders.made_in` | `{crew, cursor?, limit?}` | `{orders, next?, unknown}` |
| `orders.not_now` | `{edits, id}` | `{}` |
| `orders.pause` | `{edits, id}` | `{order}` |
| `orders.ratify` | `{edits, id}` | `{order}` |
| `orders.resume` | `{edits, id}` | `{order}` |
| `orders.run` | `{id, run, verdict}` | `{order}` |
| `orders.runs` | `{id, limit}` | `{runs}` |
| `orders.stop` | `{edits, id}` | `{order}` |
| `ping` | `{}` | `{}` |
| `power.sleep` | `{}` | `{}` |
| `power.wake` | `{}` | `{}` |
| `ports.inspect` | `{port}` | `{port, state, checked_at, holder?, detail?}` |
| `ports.suggest` | `{after}` | `{port, checked_at, checked_count}` |
| `processes.follow` | `{id}` | `{}` |
| `processes.list` | `{agent}` | `{processes}` |
| `processes.log` | `{id, offset?, limit_bytes?, revision?}` | `{text, offset, next, size, eof, partial?, more_before?, output_truncated?, log_gone?, revision?}` |
| `processes.output` | `{from_line, id}` | `{lines, next_line}` |
| `processes.page` | `{agent, limit?, cursor?}` or `{crew, limit?, cursor?}` or `{scope:"all", limit?, cursor?}` | `{processes, next?, incomplete?, revision?, roster?, owners?}` |
| `processes.actions` | `{id}` | `{id, kind, state, unknown_reason?, stop, rerun, log}` |
| `processes.log_target` | `{id}` | `{id, kind, local, path?, size?, revision?, gone?, cap_bytes}` |
| `processes.rerun` | `{id}` | `{process}` |
| `processes.stop` | `{id}` | `{}` |
| `processes.stop_all` | `{agent}` | `{agent, scope, eligible, stopped, already_ended, not_stopped, services_eligible, services_stopped, services_not_stopped, partial, unenumerated, remaining_unknown, unsupported, jobs_deadline_ms, services_deadline_ms}` |
| `processes.unfollow` | `{id}` | `{}` |
| `providers.list` | `{}` | `{providers}` |
| `providers.sign_in` | `{provider}` | `{url}` |
| `providers.sign_in_cancel` | `{attempt, provider}` | `{}` |
| `providers.sign_in_start` | `{provider}` | `{attempt, url}` |
| `queue.edit` | `{agent, attachments, id, reads, said, text, to}` | `{queued}` |
| `queue.move` | `{agent, id, text, to}` | `{queued}` |
| `queue.remove` | `{agent, id, text, to}` | `{queued}` |
| `quick.promote` | `{id}` | `{task}` |
| `quick.undo` | `{id}` | `{}` |
| `roles.list` | `{}` | `{roles}` |
| `members.brief` | `{crew, agent}` | `{brief: {text?, from?, at?, revisions?, factory_stage?}}` — the member's brief: its start line, or the newest change since; `revisions: [{text, before?, by, at, post?}]` oldest first (≤16), absent when it never changed (no edit date is inferred for older records) |
| `members.brief.set` | `{crew, agent, text, base?}` | `{brief}` — changes the member's brief in place, as the person: a `brief` line to the member in the crew log (`post`), read at its next step; it wakes nobody, restarts nothing, posts nothing in its conversation, and leaves its role alone (the brief replaces the start line; a role is told on its own and stays in force). `base` is the `post` of the revision the person edited (`""` for a brief never changed): a brief changed since is `conflict` and nothing is written. `bad_params` for unchanged words, over 12000 characters, or the crew's manager; `closed` for a closed crew; `not_found` for a member that left |
| `roles.update` | `{id, instructions, model, name}` | `{role}` |
| `render.cancel` | `{agent, id}` | `{outcome, render}` |
| `render.get` | `{agent, id}` | `{render}` |
| `render.list` | `{agent, cursor, limit}` | `{renders, next, incomplete}` |
| `rules.list` | `{}` | `{rules}` |
| `rules.remove` | `{id}` | `{}` |
| `search.all` | `{limit_per_group, query}` | `{groups}` |
| `services.get` | `{id}` | `{process}` |
| `services.list` | `{agent}` | `{processes}` |
| `services.output` | `{from_line, id, max_lines}` | `{first_line, lines, next_line, total_lines, truncated?, partial?}` |
| `services.policy` | `{id, policy}` | `{process}` |
| `services.remove` | `{id}` | `{}` |
| `services.restart` | `{id}` | `{process}` |
| `services.start` | `{id}` | `{process}` |
| `services.stop` | `{id}` | `{process}` |
| `services.stop_all` | `{}` | `{stopped}` |
| `services.tail` | `{id, lines}` | `{first_line, lines, next_line, total_lines, truncated?, partial?}` |
| `settings.get` | `{}` | `{settings}` |
| `settings.models.get` | `{}` | `{task_models}` |
| `settings.models.list` | `{allowed_only, cursor, limit, provider, q, role}` | `{models, next_cursor, read_at, revision, total}` |
| `settings.models.set` | `{allowed, mode, one_model, pins, revision}` | `{task_models}` |
| `settings.set` | `{appearance, asks_before, budget_usd, checks_model, day_cap_usd, default_model, dock_badge, effort, fallback_model, idle_close_days, open_at_login, planner_model, proposed_task_clock_s, task_cap_usd, text_size, work_folder}` | `{settings}` |
| `setup.get` | `{}` | `{setup}` |
| `setup.set` | `{day_cap_usd, default_model, provider, start, step}` | `{setup}` |
| `shutdown` | `{}` | `{}` |
| `spend.export` | `{from, to}` | `{path}` |
| `spend.today` | `{}` | `{spend}` |
| `tasks.brief` | `{id, text}` | `{}` |
| `tasks.decide` | `{id, text, verdict}` | `{}` |
| `tasks.decline` | `{id}` | `{}` |
| `tasks.dismiss` | `{id}` | `{}` |
| `tasks.entries` | `{before, id, limit}` | `{entries, more}` |
| `tasks.get` | `{id}` | `{page}` |
| `tasks.program_actions` | `{before, id}` | `{actions?, before?, earlier_actions?, read, read_cut?}` |
| `tasks.program_report` | `{id, launch, name}` | `{id, program, launch, name, started_at, ended_at, bytes, sha256, data}` |
| `tasks.hold` | `{id}` | `{}` |
| `tasks.list` | `{agent}` | `{tasks}` |
| `tasks.note` | `{client_id, id, text}` | `{entry?}` |
| `tasks.pause` | `{id}` | `{}` |
| `tasks.restore` | `{id}` | `{}` |
| `tasks.resume` | `{id}` | `{}` |
| `tasks.retry` | `{id}` | `{}` |
| `tasks.since` | `{after, id}` | `{at?, entries, files?, first?, found, last_at?, turns?}` |
| `tasks.start` | `{id}` | `{}` |
| `tasks.stop` | `{id}` | `{}` |
| `tasks.typing` | `{id}` | `{}` |
| `tools.follow` | `{agent, call, from_line, limit}` | `{}` |
| `tools.output` | `{agent, call, from_line, limit}` | `{lines, next_line, total}` |
| `tools.unfollow` | `{agent, call, from_line, limit}` | `{}` |
| `work.graph` | `{cluster, cursor, focus, if_revision, limit, scope, view}` | `{contract, view, revision, as_of?, unchanged?, scope, about, totals, coverage, nodes?, edges?, clusters?, crews?, members?, metrics?, diagnostics, next_cursor?, returned, cut?}` — see "Work graph" below; an engine that does not serve it answers `unknown_method` (check `hello.features` for `work.graph`) |

| event | data |
|---|---|
| `agent.changed` | `{agent}` |
| `ask.closed` | `{agent, at, by, id, option?}` |
| `ask.opened` | `{ask}` |
| `compaction.ended` | `{agent, message?, outcome}` |
| `crew.changed` | `{crew}` |
| `crew.log` | `{line}` |
| `crew.removed` | `{id}` |
| `decision.changed` | `{decision}` |
| `decision.opened` | `{decision}` |
| `entry.added` | `{entry}` |
| `hello` | `{contract, home, pid, version}` |
| `inbox.added` | `{item}` |
| `inbox.counts` | `{done_today, later, waiting}` |
| `inbox.resolved` | `{by, id, receipt}` |
| `log` | `{level, message}` |
| `member.changed` | `{member}` |
| `note.changed` | `{id, read_at_step?, state, task}` |
| `order.asking` | `{order, run}` |
| `order.changed` | `{order}` |
| `order.fired` | `{order, run}` |
| `order.missed` | `{order}` |
| `order.removed` | `{id}` |
| `process.changed` | `{process}` |
| `process.output` | `{id, lines}` |
| `provider.changed` | `{key?, provider}` |
| `queue.changed` | `{agent, queued}` |
| `quick.changed` | `{quick}` |
| `render.changed` | `{agent, render}` |
| `media.changed` | `{agent, source, run?, call?}`; a successful immutable source-index append, with its actual source ID and known producer IDs. No path permission is granted. |
| `deliverable.changed` | `{agent, id, reason}`; reason `created`, `kept`, `removed` or `snapshot`, after the index row was successfully appended. `created` follows a creation row the conversation itself wrote (the `write` tool making a new document or diagram, or a media tool); it is said once per deliverable, carries no path, and never follows an edit or a failed append. A new document a task worker writes is announced on the conversation that owns the task. Files shells create are not announced because they are not recorded. Descriptors still come from `deliverables.get`. |
| `settings.changed` | `{settings, task_models?}` |
| `spend.changed` | `{spend}` |
| `task.changed` | `{task}` |
| `tool.output` | `{agent, call, lines}` |
| `turn.delta` | `{agent, kind, msg, text, turn}` |
| `turn.done` | `{agent, error?, error_kind?, ok, status?, turn}` |
| `turn.long` | `{agent, elapsed_s, steps, turn}` |
| `turn.retrying` | `{agent, attempt, in_s?, reason?, turn}` |
| `turn.started` | `{agent, turn}` |
| `turn.thinking` | `{agent, text, turn}` |
| `turn.thinking_done` | `{agent, seconds, turn}` |
| `turn.tool` | `{agent, ask?, batch?, call, connector?, detail?, ended_at?, exit_code?, files?, input?, lines?, name, ok?, phase, invoked?, harness_made?, media_attempt?, refused?, rejected_before_run?, retried_of?, started_at?, summary?, tail?, task?, title?, tool_kind?, turn}` |

## Identity and de-duplication (rev 2)

- `agents.create` and `agents.send` accept `client_id` (string chosen by the app).
  The engine echoes it on the user `Entry` (`client_id`) so the app replaces its
  optimistic bubble instead of adding a second one.
- Where the conversation's engine keeps it (the remote welcome's
  `messageClientIds`; always, for a conversation held in the app process), the
  `client_id` is written on the accepted user message's own journal line and
  read back on its `Entry` after any restart — so an entry listed by a fresh
  process still names the message the app sent. It is at most 256 bytes of
  valid UTF-8 with no control characters, kept exactly as sent; one that does not
  fit is refused `bad_params` before the turn starts and nothing is recorded.
  An entry with no persisted id falls back to the sending process's own memory
  of it, and a message from before this carries none. **It is correlation and
  not idempotency**: the engine never deduplicates, skips or retries a send
  because of it, two messages with one id are two messages, the model is sent
  nothing of it, and an entry that carries no id says only that the engine
  cannot tell — never that the message was not sent.
- `agents.create` answers at once (target under 50 ms): it returns the Agent
  (state `working` when `text` was given) before the conversation engine has
  finished loading; loading continues in the background and failures arrive as
  `turn.done {ok:false}` + `agent.changed`.
- Blank conversations. `agents.create` with no `text` and no `attachments` makes a
  blank conversation: its folder exists at once (its id is the folder's name, and a
  crew adds a member by it), but nobody has said anything in it. A blank agent is
  answered to its caller and to no one else: no `agent.changed` announces it
  (unless an open crew holds it as a member, so the crew can show its chat),
  `agents.list` (every `state`) and `search.all` never list it, and at quit codeaf
  removes each blank conversation it made unless a crew names it or another window
  holds it. The first `agents.send` (or a manager's start, which names a member by
  its brief) is what lists it. A member a manager started is not blank: its brief is
  its first message.
- Every assistant message inside a turn has an index `msg` (0, 1, 2 …).
  `turn.delta` carries `msg`; the `entry.added` answer for that message carries
  `turn` and `msg`. The app keys answers by `(agent, turn, msg)`: deltas append,
  the entry replaces. An answer is never shown twice.
- A turn the engine sends back to work (the completion check's carry-on, a
  nudge, team news at a step boundary) can reply more than once with no tool
  call between. Each reply is its own `msg`, as it is in `agents.entries`: the
  replies are `update` entries when the turn goes on to call a tool and
  `answer` entries when it ends first, and their `entry.added` arrive when that
  is known (the next tool call or the turn's end).
- Entry ids are stable: the id sent live in `entry.added` equals the id returned
  later by `agents.entries` for the same item.
- A factory job's agent (`factory.job_chats`'s `agent`) holds the run's own
  progress lines, written between turns as `answer` entries: `review started`,
  `review done · 4m · …`, `asking you: …`, `stopped`, `landed · …`. Each arrives
  live as `entry.added` the moment it is written, while the app holds that
  conversation, with the id, words and facts `agents.entries` answers for it
  afterwards. A line that reports one step's round carries
  `progress: {step, round?, event}`, where `event` is `started` or `done` (the
  round began or finished), `waiting` (the step waits on a question put to the
  person), or `failed` or `stopped` (the run ended under the step, on its error
  or on a person's stop or no); `round` is absent for an approve step. Lines about
  the job as a whole (`landed`, `answered: …`, a steer, a stop that cut no step)
  have no `progress`. It is the runner's own fact, never read out of `text`,
  which keeps the line's words.
- `turn.tool` and tool entries carry `title`: the same one-line label the TUI
  shows for that call (e.g. `read src/auth/session.ts`, `bash pnpm test`), and
  `detail?` (short result line). Never just the bare tool name.
- Engine notices (e.g. "skills carried: …") are `kind: "notice"` with
  `quiet: true` when the TUI shows them as a dim line; the app renders quiet
  notices as one muted line, not a card.
  The engine's two typed notices also carry facts, set only where the engine
  produces them and never parsed from `text`: `notice_kind: "skills"` with
  `skills: [name…]` (the skills a turn carried), and `notice_kind: "memory"`
  with `unrecorded: true` (a memory line no transcript record redraws, so it
  is not repeated after reopen). Other notices stay unclassified ordinary quiet lines. Actual `skills` or
  `unrecorded` producer facts can still travel without a recognized kind; their
  presence never authorizes classifying a notice from its words.
- `models.list` returns every model the user can pick in the TUI's model
  picker for their configured providers, plus the current default, each with
  `{id, name, provider, default?: bool}`; never empty when a turn can run, and
  empty (`{models: []}`, not an error) when no provider is connected, by the
  same rule `providers.list` answers `signed_in` with.
- `agents.set_effort {id, effort: "low"|"medium"|"high"}` and Agent gains
  `effort?`.

## Wave 2: setup, keys, settings, money

Types:
```
Provider { id, name, signed_in: bool, key_hint?: "…a91f", sign_in?: bool,
           kind?: "key"|"browser"|"address", key_optional?: bool, address?,
           regions?: [{id, name}], region?, plan?,
           auth_kind?: "account", sign_in_attempt?: string,
           sign_in_state?: "pending"|"saving"|"signed_in"|"failed"|"cancelled", sign_in_error? }
KeyCheck { ok, models?: Model[], status?, message?, connection?: Connection }
Connection { region?, door?, plan?, listed?: bool, address? }
Settings { default_model, budget_usd, asks_before, day_cap_usd, work_folder,
           task_cap_usd, proposed_task_clock_s, idle_close_days, effort? }
Setup    { step: 1|2|3|"done", provider?, default_model?, day_cap_usd?, start?: "agents" }
```
Setup is three steps: 1 connecting providers (any number of them, or none), 2
the defaults, 3 how to start. `provider` is the provider the
person last chose, which need not be connected; the providers that are connected
are the ones `providers.list` marks `signed_in`. Until 2026-10-15 setup was four
steps (welcome, connect, defaults, how to start). A setup kept by a build from
then is answered in three, so a person who quit halfway resumes where they were:
a kept 1 or 2 is answered 1, 3 is 2, 4 is 3, and `"done"` is done. `setup.set`
refuses a step past 3 (`bad_params`, `step is 1 to 3 or "done"`).
Keys never touch disk in codeaf: the app keeps them in the macOS Keychain and
hands them to codeaf at every launch with `keys.set`; codeaf holds them in memory.
The app sends `keys.set` before its first `models.list` or `agents.create`. A call
that reaches codeaf before any key fails with the missing-key engine error, whose
message is `no model provider is connected` (a turn that could not start for it
ends `ok:false` with that `error`; `models.list` answers `[]` instead), and
that failure is not kept: the next call after `keys.set` opens with the key.

Providers (additive). `providers.list` is every provider the terminal's
first-run screen offers, in its order — OpenRouter, Ollama, Codex (the ChatGPT
sign-in), DeepSeek, Z.ai, Moonshot, MiniMax, Alibaba Qwen, ai&, a custom
OpenAI-compatible endpoint — then any further custom endpoint the profile holds,
by the name it was connected as. `kind` says what connecting it asks for, and a
reader that predates it may assume `"key"`:

- `"key"`: a pasted key. `sign_in: true` (OpenRouter) offers a browser sign-in
  beside it. `regions` (Z.ai, Moonshot, Qwen: `intl` International, `cn` China)
  are separate accounts; one is chosen and sent as `region` before the key is
  checked. Once connected, `region` is the one in use and `plan` how the key is
  billed in the provider's word ("coding plan", "pay-as-you-go").
- `"browser"` (Codex, the ChatGPT account): a browser sign-in and nothing else;
  `keys.check` and `keys.set` refuse it. It is also `auth_kind: "account"`, and
  the two say one thing: `kind` for a reader of the connect shapes, `auth_kind`
  with `sign_in_attempt`, `sign_in_state` and `sign_in_error` for a reader of
  account sign-ins. See Account sign-in below.
- `"address"` (Ollama, custom): `address` is the one in use, or the one the field
  starts with (`http://localhost:11434/v1` for Ollama, empty for a custom
  endpoint). `key_optional: true`: the key may be empty, and then the provider is
  connected with none. A connected custom endpoint's `name` is the name it was
  connected as (its host: `127.0.0.1` reads `127-0-0-1`). `keys.remove`
  disconnects it from the profile.

Every provider but OpenRouter is checked by the terminal's own requests in the
terminal's own order: the model listing; the one-token call where the provider
lists none; for a provider with two billing roads, a one-token call on each,
plan first, until one answers. Each request has 10 s; the whole check at most
30 s. A passing check answers `connection`, which `keys.set` takes back as it
came; it holds nothing secret (`door` is the billing road the key opened, `plan`
its word, `listed` whether the provider answered with its models) and codeaf
keeps it in its profile, never the key. A launch's `keys.set {provider, key}`
carries no connection and keeps what is there. Models a check answers are spelled
as `models.list` spells them (`deepseek/deepseek-v4-pro`, provider `deepseek`); a
provider that lists none answers the one model the terminal moves a conversation
onto (`moonshot/kimi-k2.7-code`), and `models.list` offers that same model for it.

Account sign-in (`auth_kind: "account"`, Codex). The account's rotating tokens
stay in codeaf's profile, owner-only, in the same store and through the same
browser flow as the terminal — the one credential the app does not hand over — and its
row in the profile carries no key. They never appear in this protocol or the
app's key bridge, and its `provider.changed` carries no `key`. There is one
sign-in at a time, with a ten-minute deadline and no polling:
`providers.sign_in_start` starts it, or joins the one already waiting, and
answers its `attempt`; `providers.sign_in` starts or joins it the same way and
answers only the `url`. While it lasts, and after it ends, `providers.list` and
`provider.changed` carry its `sign_in_state` (`pending`, then `saving` once the
browser has answered, then `signed_in`, `failed` with `sign_in_error`, or
`cancelled`). `providers.sign_in_cancel` cancels a pending one and is not a
sign-out. `keys.remove` signs the account out, exactly as the terminal's
disconnect does, taking its tokens and its row away: a sign-in still pending is
cancelled first, one being saved answers `busy`, and the last sign-in's state is
forgotten.

| method | params | result |
|---|---|---|
| `providers.list` | – | `{providers: Provider[]}` |
| `keys.check` | `{provider, key, region?, address?}` | `{ok, models?: Model[], status?, message?, connection?}`; OpenRouter: exactly one provider request; 10 s timeout → `{ok:false, status:"timeout", message:"No answer · couldn't reach <host> in 10s"}`; a rejection carries the provider's status code and own words. Others: see Providers above; `status` is the provider's code, `"unreachable"` (`message: "No answer · couldn't reach <host>"`), `"shape"` (codeaf refused the key's shape before asking; `message` says the shape), or `"payment"` (every billing road refused for money); a pass may carry `message` when the account cannot pay or its plan's window is spent. `key` may be empty for a `key_optional` provider. Keeps nothing |
| `keys.set` | `{provider, key, connection?}` | `{}`; the key in memory only (models.list / turns use it); provider `github` is the factory's GitHub token, no model provider: held for the factory on this machine alone, never announced or listed, and refused empty; `connection` (from keys.check) kept in codeaf's profile; `key` may be empty for a `key_optional` provider, which is then connected with none; the provider's models are fetched in the background and reach `models.list`, which waits up to 3 s for that fetch. Before any provider is connected the engine's process cannot open: `models.list` answers `{models: []}` and a turn ends `ok:false` with `error: "no model provider is connected"`; the first connection opens it in the same launch |
| `keys.remove` | `{provider}` | `{}`; a key is let go for this launch; an address is disconnected from the profile; the Codex account is signed out (a pending sign-in cancelled first; `busy` while one is being saved) |
| `providers.sign_in` | `{provider}` | `{url}` browser sign-in for OpenRouter and Codex; completion arrives as `provider.changed` (with `key` for OpenRouter only); a Codex sign-in that finishes and cannot be kept ends `sign_in_state: "failed"` with `sign_in_error` |
| `providers.sign_in_start` | `{provider}` | `{url, attempt}` starts/joins the one account sign-in, returning its non-secret operation identity |
| `providers.sign_in_cancel` | `{provider, attempt}` | `{}` cancels a pending account sign-in; `busy` during its authorized save boundary; does not sign out an existing account; stale attempts have no effect |
| `setup.get` / `setup.set` | – / `Setup` | `{setup: Setup}` (persisted by codeaf so quitting mid-setup resumes at the same step; a step a four-step build kept is answered in three, see Setup above) |
| `settings.set` | partial `Settings` | `{settings}`; emits `settings.changed` |
| `caps.raise` | `{scope: "agent"\|"day", id?, to_usd, for: "today"\|"good"}` | `{}` |

Budget asks: when an agent nears/hits its budget or the day cap, an `Ask` with
`kind:"approval", subkind:"budget"`, `detail` = what it has done so far, and options
`raise` (`{to_usd}` via `asks.answer {option:"raise", text:"8.00"}`), `stop`, `tell`
(free text). Day cap options: `raise_today`, `wait`. Copy per U1/U2 edge cases
("It's close to its $5.00 budget", "Day cap reached · $40.00 of $40.00").

Events: `settings.changed {settings}`, `provider.changed {provider, key?}` (key: sent once after an OpenRouter browser sign-in so the app can keep it; never for Codex), `spend.changed`.

## Wave 3: tasks, quick tasks, processes, running long, sleep

Types:
```
Task (adds) { proposed?: bool, clock_until?: time, irreversible?: bool,
              your_call?: bool, result?, files?: [{path, add, del}], budget_usd?, spent_usd? }
TaskPage { task: Task, result?, files, log: Entry[], notes: [{id, text, at, from?, read_at_step?}], brief,
           program?: TaskProgram }
TaskProgram { name, started_at?, ended_at?, stages?: [{name, word?}], last_stage?, ceiling_usd?, calls?,
              models?, effort?, listens, listening, started, inbox_closed?,
              read: "complete"|"partial"|"unknown", read_cut?: "budget"|"damaged"|"unreadable"|"deadline",
              actions?: [{at, step?, text, outcome?, added?, removed?, steer?, memory?, model?, reason?}] (newest ≤60),
              earlier_actions?, before?, ending?: {at, status?, text, outcome?, facts?: EndingFacts} }
           program: present ONLY for a task a specialist was handed. Absent is not "none was run":
           an engine that predates it omits it too. Stages are the hello's own list and count; a
           stage with no word keeps the word before it; the APP groups consecutive equal words
           (senior-dev's 15 stages are 7 consecutive words, not 6). There is no index, percent,
           attempt number or auto: the record is the latest process, not a history.
           CURRENT PROCESS ONLY, BY ITS LAUNCH, NEVER BY TIME. Every launch of a program has an
           identity its producer draws before the process exists and stamps on the record, on a
           start line, on every action line and every call; actions, last_stage, ending and calls are
           the lines carrying the record's launch and nothing else — an earlier run's lines, however
           late or early their time, never. read: "complete" = every line of that process was read
           within the read's budget (a launch span of at most 16 MiB, 200 000 lines, 3 s;
           a bounded reverse search locates its start before the forward canonical read); actions are exact, earlier_actions is how many of
           its own came before them, before is the cursor for tasks.program_actions (present only when
           there are earlier ones), last_stage is its newest stage record's own name, ending is present
           only when its own last line is the ending. "partial" (read_cut says why: over the budget, a
           damaged or overlong line among its lines, a disk refusal, the deadline) = actions are carried
           ONLY under read_cut "damaged" (the read went past the bad line to the launch's end, so they
           are its newest, or the page before the cursor, minus what could not be read); under every
           other cut no actions are carried, because a read stopped early has no true window. There is
           NO earlier_actions, before, last_stage or ending — no claim of none either. "unknown" = no line can be tied to the record's process (a run from
           before launches were, a relaunch before its hello): no actions at all. calls: how many of
           that process's calls reached a model, PRESENT ONLY WHEN EVERY ONE WAS READ (its whole
           conversation log within 32 MiB, every line parsed); 0 is a real zero, absent is unknown.
           The metadata read is limited to 5 MiB; JSON escaping can make even a valid produced record exceed this bound. Unsafe, corrupt or over-budget metadata carries
           no authoritative launch. Reads accept only ordinary files, including links to ordinary
           files; pipes, devices and other special files are unavailable, never a known empty log.
           EndingFacts are typed and allow-listed (a count nobody said is absent, not 0), every key the
           engine's ending keeps (a test holds the two field for field); hunt_coverage is {unknown:true}
           alone or a whole {attempted, failures}; nothing_to_audit is true-only (absent is not "there
           was something to audit"). reports are display names, not a file handle and not for the
           renderer to open. `launch` and `report_files` (the declared bare file names that THIS launch's
           program issued a receipt for when it wrote them; absent for a failed rerun or a record from
           before receipts) are what `tasks.program_report` is asked with; they are a question, not a grant.
           log: ONE ENTRY PER STEP, each a tool `Entry` carrying its call's facts (see "Work log
           detail" below), so a task's log is grouped, folded and timed by exactly the rules a
           conversation's is. A step the record holds with NO TOOL PAYLOAD — an older journal or
           trajectory that kept a command and nothing else — stays a quiet notice with its own
           words and is never given a `tool_kind` it never declared. Facts are present only where
           they were recorded: no `ended_at` without a recorded end, no `exit_code` without a
           reported exit, no `ok` for a step still running or one a killed session left unsettled.
           notes.from: "you" | "task" — who said it, from the engine's own record of each note's
           origin, never read back out of its words. ABSENT IS UNKNOWN AND NOT "you": a note from a
           record that never labelled one is drawn without attribution, because a surface that
           defaulted to the person put a worker's handoff in their mouth.
           task.steps: the count the record kept, SENT WHEN KNOWN AND OMITTED WHEN NOT — a known
           zero is sent, so "took no steps" and "nobody wrote down how many" stay different facts
Quick    { id, agent, title, files: [{path, add, del}], state: "running"|"done"|"stopped"|"undone"|"grew"|"conflict",
           grew?: {files, reason}, conflict?: {changed: string[]} }
           stopped: ended without finishing; keeps its files and may still be undone.
           undone > promoted > settled (stopped or done) > running (grew only while awaiting promotion).
           A quick task promoted to a full task is sent as done. conflict is derived only from done,
           never from stopped; a state the engine adds later is sent as itself, never as done.
Process  { id, agent, title, command, state: "running"|"ended"|"failed"|"stopped",
           started_at, ended_at?, exit_code?, last_line?, reason? }
           reason: why a stopped process stopped when a person did not stop it: "stopped by quit"
           id: "<agent>:p<n>.<start>" — the job's number and when it started, so a process stopped by
           a quit and one started after the restart never share an id; an id naming a process the
           conversation no longer runs is answered not_found, never applied to another
```

| method | params | result |
|---|---|---|
| `tasks.get` | `{id}` | `{page: TaskPage}` |
| `tasks.program_actions` | `{id, before}` | `{read, read_cut?, actions?, earlier_actions?, before?}` OPTIONAL (unknown_method = this engine cannot page; never "no earlier actions"). The page of a program's actions before the opaque cursor `before` a `TaskProgram.before` or an earlier page handed out, ≤60, oldest first, read in the program's own words from its launch's first line, so a page reads exactly as the newest did. For a complete read, every action of the process is reachable by following `before` until it is absent. A launch beyond the canonical reader's byte/line budget is explicitly partial with no cursor; it is not an empty history. Same `read` rules as `TaskProgram`; a cursor of a launch that is no longer the task's (handed off again since) is `conflict` — read the task again; a task with no program is `not_found` |
| `tasks.program_report` | `{id, launch, name}` | `{id, program, launch, name, started_at, ended_at, bytes, sha256, data}` OPTIONAL (unknown_method = this engine cannot show a report: a `--host` agent today, or an older engine; never "no report"). The exact bytes of ONE report file a specialist wrote in its task's record folder, ON THAT LAUNCH'S RECEIPT: when the program wrote the file it issued `{name, bytes, sha256}` of exactly the bytes it wrote on its terminal line, and its worker kept that receipt on the launch's record (`delegate-program.json` `report_receipts`, in the write that sets `ended_at`). A declared name, an ended launch, a file under the name, its mtime/ctime, or `EndingFacts.reports` (display names) are each NO grant: a rerun that failed before writing leaves the earlier run's file standing, and that file is not the rerun's report. `launch` is `TaskProgram.launch` and `name` one of `TaskProgram.report_files`; the request carries no path. `data` is the file's bytes in standard padded base64 (a string, never `number[]`), `bytes` their count (≤ 2 097 152), `sha256` their lowercase hex digest, always the receipt's: nothing is decoded, trimmed, normalised or redacted, so an app can run strict UTF-8 and see a byte-order mark exactly as written. The folder is opened once without following a link and every later step is relative to it; the file must be an ordinary file with one name (a link, pipe, device, folder or second name is `engine`); a name the program did not declare, or any path, is `bad_params`; a `launch` that is no longer the record's, a file whose size or digest is not the receipt's (a later writer, a rewrite in place, an earlier run's file), or a file, folder or record that changed while read is `conflict` (read the task again); a task that is not this agent's, a missing file, a run that has not ended, or a launch that issued no receipt for `name` (a failed rerun, a record from before receipts — never backfilled) is `not_found`; over 2 MiB is `too_large` with no bytes. At most two reads run in the process at once, each admitted before any I/O and held until its answer is built; a third is `busy` immediately (nothing queues), as is a read that does not finish in 3 s, and a closing app answers `closed`. Never an empty or partial success, never a retry, nothing written, no model call. It grants no path and no permission to post: `/review post` still reads the path it is given |
| `tasks.start` · `tasks.hold` · `tasks.decline` | `{id}` | `{}` proposed-task card actions |
| `tasks.typing` | `{id}` | `{}` holds the clock while the person types in the card |
| `tasks.decide` | `{id, verdict: "accept"\|"not_right"\|"recheck"\|"let_it_decide", text?}` | `{}` Your call (Tell it… = `not_right` or a note with text) |
| `tasks.model` | `{id, model, state?, version?}` | `{}` proposal card Change model: moves ONE proposed or held task to a known model before it starts; refused for a started task, an unknown or empty model, or a stale `state`/`version`; never changes the agent |
| `tasks.routing.get` | `{id, changes? (1..64, default 20)}` | `{routing: {task_id, state: "routed"\|"picking"\|"blocked"\|"single", mode?: "auto"\|"one", running, settled, restored: [time], roles: [{role, model: ModelRef, billing?, source: "auto"\|"pinned"\|"task_pin"\|"one_model", status: "used"\|"pending"\|"not_run_yet"\|"not_used"\|"unknown", quality, estimate_usd: number\|null, price: {in_per_m, out_per_m}\|null, spent_usd: number\|null, calls, unknown_calls, alternatives: [{model, billing?, quality, estimate_usd, price}] (≤5, fallback rungs, not stronger picks), rejected: [{model, reason, until?}]}], model?: ModelRef, estimate_usd: number\|null, spent_usd: number\|null (helpers included; null when no task tally was kept), calls, unknown_calls, helper_usd (the helpers' part of spent_usd), task_cap_usd: number\|null, class?, effort?, warnings: [{role, kind, model}], stopped_action?, changes (newest ≤64): [{at, role?, from?, to?, by: "engine"\|"person", kind: "fallback"\|"held"\|"stronger_worker"\|"best"\|"pick"\|"auto"\|"cap"\|"restored", reason?, cap_usd?}], change_count (every change), restore_count, role_facts, blocked?: {role, mode, rejected: [{model, reason}], omitted, action?, at}, requested?: {worker?, planner?, checker?}}}` V80: one task's models as the engine recorded them; never a settings read. `unknown_calls` are answered calls whose cost nobody said and which `spent_usd` does not include; `estimate_usd` is null when a role's price is unknown. `single` is an older task with one recorded model and nothing known about planner or checker. A role is `used` only when its own call answered; `role_facts: false` (a task from before per-role facts) makes a role with nothing recorded `unknown` and its `spent_usd` null, never a proven zero. `price` is the provider's own tariff frozen at admission; null is unknown. `quality` is the router's raw figure, not a rank |
| `tasks.routing.act` | `{id, action: "stronger_worker"\|"best"\|"pick"\|"auto"\|"same"\|"retry", picks?: {worker?, planner?, checker?: ModelRef\|null} (pick only; null is Auto), rerun?, raise_cap_usd?, change_count?}` | `{routing, applied: [role], rerun_task_id?, refused?: {code: "over_cap"\|"day_cap"\|"no_eligible"\|"not_allowed"\|"route_failed"\|"unchanged"\|"strongest"\|"settled"\|"busy"\|"stale"\|"not_routed", message, role?, required_cap_usd?, cap_usd?, blocked?}}` V80: a person's change to one task's models. Not started: kept for its routing. Running: applied from each role's NEXT call. Finished: only with `rerun` (a new task; the finished record never changes). Over the task cap: refused with the cap it needs unless `raise_cap_usd` is at least that; the engine never raises a cap or picks a stronger model by itself. A pinned role whose route fails is held (`kind: "held"`), never moved. `retry` starts a task waiting on a model (`state: "blocked"`) |
| `tasks.routing.options` | `{id}` | `{options: {task_id, scope: "next_step"\|"rerun"\|"before_start"\|"waiting"\|"none", stronger?: Preview, best?: Preview, pick, auto, retry}}` V80: what Change models… / Rerun with… offers, each change worked out by the engine with the same policy as `tasks.routing.act` and moving nothing. Preview: `{moves: [{role, from, to, to_price: {in_per_m, out_per_m}\|null, to_estimate_usd}], estimate_usd: number\|null, cap_usd: number\|null, over_cap, refused?}`; `refused` is the refusal the change would meet (strongest, no_eligible, unchanged…). A not-started task's `best` carries no preview |
| `tasks.note` | `{id, text}` | `{}` |
| `tasks.stop` | `{id}` | `{}` |
| `tasks.dismiss` · `tasks.restore` | `{id}` | `{}` V92: hide a settled failure from the default view, and bring it back. A visibility fact only — the task's state, failure, result, files and spend are untouched, and nothing is retried, stopped, declined, deleted or dispatched (no paid operation). `tasks.dismiss` is refused with `bad_params` unless the task is a plain settled failure (`failed` and not stopped by the person, not declined; not `done`, running, waiting, proposed, paused, held, waiting on a model or in Your call) and nothing live waits on it or hangs under it (the refusal names which); a stopped or declined task keeps its state and result as they are. The state and the dependents are asked in the same locked step that writes the change (a retry or a resume cannot land between), and a retry of a dismissed task that is admitted clears the dismissal in that same step (a refused retry clears nothing), so a working task is never hidden; `dismissed` is only ever reported on a plain failure. Dismissing a dismissed task and restoring a task that is not dismissed succeed and change nothing. The change is written to the conversation's checkpoint before it is answered (a failed write is an error and changes nothing) and is said as `task.changed` (the task, with `dismissed`) and in the next `work.graph` read (`nodes[].dismissed`, `totals.dismissed`; the revision moves). Undo is `tasks.restore`, an explicit call. Works on graph tasks and run rows. Plan-row dismissal is explicitly refused before any read or mutation because this capability does not yet provide an atomic admission fence over the external plan store. Existing hidden Plan rows can still be restored safely. |
| `quick.undo` | `{id}` | `{}` or error `conflict` with the changed files |
| `quick.promote` | `{id}` | `{task}` Make it a task |
| `processes.list` | `{agent}` | `{processes: Process[]}` running and ended. A history it cannot carry whole — over 1000 records, holding records that cannot be read, or not walked to its end within 2 s — answers `too_large` rather than a part of it; read it with `processes.page` |
| `processes.output` | `{id, from_line?}` | `{lines: string[], next_line}`; live tail via `process.output` events only while subscribed |
| `processes.page` | `{agent, limit?, cursor?}` (the crew forms below need `processes.crew`; without it they are `bad_params`) | `{processes: Process[], next?, incomplete?, revision?}` — one bounded page of the agent's WHOLE process history, its task workers' processes included, newest first by job number. A page walks three phases in order — the agent's kept-running services (by service id), then every process the engine holding the conversation runs now, then the saved history — and EVERY row of every phase counts against `limit` (1 to 100, default 50) and the 4 MiB reply; the cursor (≤ 1024 bytes) carries the phase. A row that does not fit begins the next page; nothing is dropped. One row's ENCODED size is never larger than 256 KiB: its free text is cut to progressively smaller caps (4 KiB down to nothing) until the encoded row fits, and the row says `cut: true`; a row that could not fit with every free text emptied is refused with `too_large` rather than sent over its bound. A paged kept-running service carries no `last_line` (no file is opened for it; `processes.output` reads its output). A held conversation whose engine cannot page answers `unknown_method` — its whole legacy list is never fetched to page here; `processes.list` still lists it. A cursor still in the services answers `stale_cursor` when the agent's service membership changed (which services, not their state); services that change after the reader has left them arrive as `process.changed`. A row's `id` is stable; one that arrives twice (live on page 1, ended later) replaces the earlier. `next` is absent ONLY when the walk reached the oldest record: the limit, a 4 MiB reply budget, a hole budget over unused job numbers or the 2 s deadline each answer a `next` instead. `incomplete` says some history exists that could not be read (damaged, oversize, an older array not yet folded in) and is on no page. `stale_cursor` when the history was rebuilt; start again. Each row may add `dir` (folder at start), `log_path` (show/copy only, never accepted back), `log_gone` (output expired, record kept), `output_truncated`, `started_by: {kind: "chat"}\|{kind: "task", task}` (present only when the agent that started it said so) and, on this method only, `state: "unknown"` with `unknown_reason: "not_held"\|"owner_lost"` for a record saved as running that no live owner vouches for — such a row has no `exit_code` and no `ended_at`. A held conversation is answered by its own engine (which waits at most 250 ms for its record lock, then answers read-only); one nobody holds is read without opening, migrating or writing anything. At most 4 page/log reads at once (`busy`). Check `hello.features` for `processes.page` |
| `processes.page` (crew) | `{crew}` or `{scope:"all"}`, `limit?`, `cursor?`; never with `agent` | `{processes, next?, incomplete?, revision, roster:"recorded", owners:{total, done, unread?, partial?:[{agent\|key, reason}], more?}}` — feature `processes.crew`. The RECORDED roster read once at page 1: the crew and every team below it by recorded `parent` (closed and archived included), or every team with `scope:"all"`; members as recorded, nothing from former members, removals or traffic, no conversation in no team. Owners go in id order, up to five rows of each owner's own page per visit, live-first within one owner only; there is no cross-owner time or live order, so show `owners.done / owners.total`, never a whole day. A walk is complete only when `next` is absent and `owners.partial` was empty throughout. `reason` is `missing\|unreadable\|timeout\|stale`; a partial owner is skipped for the rest of the walk. `revision` changes with the recorded tree and (team, owner) pairs only; a changed one, an evicted, expired (5 min idle, 15 min) or older cursor is `stale_cursor`. The previous cursor replays its reply. Server-held walks: ≤ 16, ≤ 2 per target, 64 MiB, 8 MiB each. > 4096 owners or 16384 pairs: `too_large`. `busy` for a concurrent page or no progress. Only a member whose stored `File` is exactly `<places>/<bucket>/<id>/transcript.jsonl` names an owner (`agent` = id, verified through a pinned descriptor); any other member — another machine's path, a key with no file — is an unnamed `missing` partial carrying its stored `key` (omitted and only counted in `more` when over 256 bytes), and is never matched to a conversation held here by id or key. A held conversation is asked through its engine only when its live transcript path equals the stored `File`; otherwise the member is read as saved. Owners are read only through a pinned descriptor under the places root; their rows carry no `last_line`. The whole encoded reply, partial names included (≤ 32 and ≤ 32 KiB encoded; the rest in `more`), stays within 4 MiB. A `busy` page changes nothing: retry the same cursor. |
| `processes.log` | `{id, offset?, limit_bytes?, revision?}` | `{text, offset, next, size, eof, more_before?, output_truncated?, log_gone?, revision?}` — one window of a process's output, read from the path its OWN record names for that exact id and start (no path is accepted). No `offset` reads the tail; `limit_bytes` 1 to 524288, default 65536. Text is valid UTF-8, secrets redacted (line by line; a line split across windows is redacted per window), cut at a line end (one line longer than the window is split at a character, invalid bytes always advance). An ended process's last unterminated line is returned; a running one's is left for the next read and said as `partial`. `eof` means `next == size`. Continue with `offset: next`. Only the conversation's own `logs/jobs/<id>.log` for that job is opened, with no link followed below the conversation folder; a link or special file is refused without blocking; an offset past the end or a different `revision` answers `stale_cursor`. Kept-running services use `processes.output`. A held conversation whose engine cannot page answers `unknown_method`. Check `hello.features` for `processes.log` |
| `processes.follow` / `processes.unfollow` | `{id}` | `{}` |
| `processes.stop` · `processes.rerun` | `{id}` | `{}` / `{process}`. A job is acted on through the engine's stamped door: it finds the run again by number AND start, recomputes `processes.actions`, and refuses with `conflict` in the engine's words. A stop that was refused (a render already saving) or whose end was not seen in time is an error, never `{}`. Run again on an engine too old to check is `unknown_method`; never by number. Stop on such an engine falls back to the number, after this app checked the start |
| `processes.actions` | `{id}` | `{id, kind: "job"\|"service", state, unknown_reason?, stop: {available, reason?}, rerun: {available, reason?}, log: {available, reason?}}` — READ ONLY, and the ONLY authority for Stop / Run again / Open full log on a row; a row's text never is. `unknown_reason` is present only with `state: "unknown"`. Reasons — stop: `ended` · `unknown` · `task` · `saving` · `not_held` · `unsupported`; rerun: `running` · `unknown` · `not_command` · `no_command` · `command_cut` · `task_worker` · `starter_unknown` · `no_folder` · `other_folder` · `folder_gone` · `removed` · `not_held` · `unsupported`; log: `no_log` · `gone` · `unmanaged`. A job's run again is offered ONLY for a command its own chat started, whose ending its owner saw, kept whole, recorded in a folder that is the agent's workspace now and still exists; an unknown row or a task worker's is never. A conversation not open here is read from its store and offers nothing an owner must vouch for (`not_held`); it is never opened to answer. `not_found` when the id's start is another run's. Shares the 4-read admission and 2 s deadline. Check `hello.features` for `processes.actions` |
| `processes.stop_all` | `{agent}` (`scope` other than `agent`, or any `crew`: `bad_params`) | A receipt; every field present. A PERSON'S WORD ONLY — never the model's, an automation's or a poll's. One snapshot of the jobs the engine holding the conversation runs now (its own and its task workers', task kind excluded; at most 256 signalled, the rest counted in `unenumerated`; a scan over 4096 entries says `remaining_unknown`), stopped at once under one 5 s deadline, and the agent's own kept-running services (never another agent's) stopped beside them, all under ONE 10 s deadline from the moment it is asked. Saved and unknown rows are never touched. `not_stopped` names each job signalled whose end was not seen, and each render that was saving and not stopped; `services_not_stopped` each service not proven gone; each list at most 256 (cut: `remaining_unknown`). `unsupported`: `jobs` (the engine has no stamped door) or `services` (no supervisor). `partial` = `unenumerated > 0` or `remaining_unknown` or `unsupported` not empty. Say "all stopped" ONLY when `partial` is false and both lists are empty; `eligible + services_eligible == 0` is "nothing was running". One in flight per agent (`busy`) |
| `processes.log_target` | `{id}` | `{id, kind, local: true, path?, size?, revision?, gone?, cap_bytes}` — facts about the managed file, opened and identified by this app just now, for a NATIVE opener that takes only the id: a job's own `logs/jobs/<id>.log` below its conversation, a service's log below its supervisor. Nothing is followed through a link, a special file is refused without blocking, and the revision is `processes.log`'s. The path is NEVER a grant and never accepted back; the opener opens it again and compares `revision`. `gone` when the output expired (path absent). `not_found` for a record that kept no output, `engine` for one whose file is not where codeaf writes it or is not a regular file. `cap_bytes`: 10485760 for a job (compacted at it), 2097152 for a service (cut at it while a codeaf runs). Shares the 4-read admission |
| `agents.move_to_task` | `{id, turn}` | `{moving?: true, task?: Task}`, answered AT ONCE. The reply moves at its next step boundary; one already between steps moves inside the call and the answer carries `task`. Otherwise the answer is `{moving: true}`: the move is taken, a reading handed to a quick task is let go rather than waited for, and when the step in flight ends the task arrives as `task.changed` with exactly one notice entry, "Moved to a task · the chat is free again". A reply that ends first leaves the notice "The reply finished before it could move to a task". `not_found` when no reply is running |
| `power.sleep` · `power.wake` | – | `{}` sent by the app on macOS sleep/wake |

Events: `quick.changed {quick}`, `process.changed {process}`, `process.output {id, lines}`
(followed only), `turn.long {agent, turn, elapsed_s, steps}` once per reply at 180 s,
`agent.changed` gains state `asleep`.

Rules: in app mode the engine never moves a running reply to a task by itself (dev's
checkpoint auto hand-over is off); it emits `turn.long` and waits for `agents.move_to_task`.
Proposed tasks start on their own after `proposed_task_clock_s` unless held, typed in or
answered; irreversible tasks have no clock. Quick tasks: > 30 files or any shell command that can change
things (a test run included) → `grew`; a read-only command (search, listing, `git status`) stays quick.
App agents are always offered quick tasks (launch `quickTasks`), beside full tasks that are runs.
Never in entries: the lines the engine writes to its model (wakes, landing notes, the carry-on after a
quit, a checkpoint's question, the `[carry on]` continuations and the note after a reply was cut off at the
output limit, none of which counts as the person's message on a reread) and a reply that is only the
withdrawal token `[no change]`, live or reread; a quit-resumed reply is marked by the footer
`resumed_after_quit` ("Resumed after quit"),
`resumed_after_close` ("Resumed after the app closed") and
`stopped_by_close` ("The app closed while this was working · send a message to carry on").
These three notices are kept with the conversation and return in place after restart. Nor is the line saying a turn was sent back to work
(`the ask is not finished · carrying on rather than stopping here` and the two nudges, "… · asking it to go
on"): the turn going on is what the app already shows. The lines that end a turn ("stopping here · …",
"carried on … times · …") stay quiet notices.

## Work log detail (V63 · An agent at work, 20+ tool calls)

The app groups, folds, pins and tails tool rows itself (deterministic rules on V63);
the engine supplies the facts per call. `turn.tool` and tool `Entry` gain:

```
tool_kind: "read" | "search" | "run" | "edit" | "web" | "connector" | "task" | "asks_first" | "other"
                                 (named tool_kind so it never clashes with Entry.kind)
started_at, ended_at?            server-observed event wall times; absent history times stay unknown
                                event interval is not actual handler execution duration
batch?                           same value for calls the agent started together (parallel)
exit_code?                       run
rejected_before_run?: true       the tool turned the call's arguments away before it did any of its
                                 work (malformed JSON, a missing or invalid argument), by the engine's own
                                 typed marker at the validation return. True-only; never read from the
                                 output text, so a command that RAN and printed "Invalid arguments", a
                                 nonzero exit, an HTTP error or a timeout omit it. Set on the end row and
                                 the tool entry (live and after reopen); legacy rows omit it = unknown.
media_attempt?: true             this exact call made at least one render record carrying its own `run`
                                 and `call` (an image attempt, a refused image/video call, a video or
                                 music render begun); find it in `render.list` by that pair. Set by the
                                 code that wrote the record, never from the tool name or output. True-only,
                                 kept per occurrence (a reused provider call id never lends it); absent =
                                 unknown, including every row older than the field.
retried_of?                      call id this one re-runs (a later pass of the same command)
files?: [{path, add, del}]       edit
connector?                       e.g. "GitHub" (connector kind)
task?                            task id (task kind)
ask?                             ask id (asks_first kind: the log pauses here)
input?: {cwd?, timeout_s?, text} what was asked (command, query, path)
tail?: string[]                  last ≤3 output lines while running, ≤12 when ended
lines?                           total output lines
handoff?: {crew_name?, crew?, to, post?, answers?, delivered, reason?, up?, from_crew?}
                                 team_post only, once it has an answer: the crew log entry
                                 (`post`, a LogLine id) the line became and the one it answers;
                                 delivered false carries the engine's own words in `reason`.
                                 `up` is a post to the manager of the crew above (`from_crew`
                                 the member's crew): `crew` is then that crew's id. A post up
                                 answered by an older build has no `post` and no `crew`
```

#### Tool-result provenance

`invoked: true` means the engine actually dispatched the handler; it does not
prove a process or network request occurred. A validator can be invoked and return
`rejected_before_run: true`. `harness_made` and `refused` retain the engine's own
distinct gate/permission facts. A failed row may be treated as a known dispatched
failure only with `invoked: true` and without those rejection/refusal facts.
All are true-only optional facts: absence on legacy or ambiguous occurrences is
unknown, never evidence that the tool ran. These facts follow the exact result
message through journal replay/compaction; provider call IDs alone are not identity.

A notice entry that is a crew's delivery also carries `team: [{crew_name?, from, to?, kind,
text, post?}]`: each delivered line as facts (`kind` note · directive · start · brief (a brief
changed in place); `from` manager ·
you · system · a handle; `post` the line's LogLine id). Its `text` is the same lines worded.

An entry's `answer`/`update` kind is the live pump's: a message whose own tool calls ran after
it is an update, every other message is its turn's answer. A turn a crew delivery or a wake
opened (no person's line before it) never turns the previous turn's answer into an update.

| method | params | result |
|---|---|---|
| `tools.output` | `{agent, call, from_line?, limit?}` | `{lines: string[], next_line, total}` full output of one call (works after the turn) |
| `tools.follow` / `tools.unfollow` | `{agent, call}` | `{}`; live `tool.output {agent, call, lines}` events only while followed |

`turn.tool` is sent at begin, at end, and at most every 500 ms in between with a
fresh `tail` for calls running longer than 5 s.

## Wave 3b: conversation, dock, queue, tasks as chats (V65–V70)

Boards: docs/design/boards/V65-TaskIsAChat, V66-TaskTree, V67-TurnStates,
V68-DecisionDock, V69-ComposerQueue, V70-ConversationBehaviour.

Entries and turn events (adds):
```
Entry kind "update"   { text }   interim mid-turn message (eyebrow "Update"), full markdown
Entry kind "steer"    { text, state: "pending"|"consumed"|"fell_through", step? }
Entry kind "note"     { task, text, from: "you"|"task", state?: "sending"|"waiting"|"read", read_at_step? }
Entry kind "summary"  { text }   "Earlier messages summarized" (engine compaction point)
Entry kind "footer"   { text, footer_kind: "stopped_by_you"|"retrying"|"provider_failed"|"fallback"|"process_finished"|"process_limit"|"resumed_after_quit"|"resumed_after_close"|"stopped_by_close",
                        retry_in_s?, model_from?, model_to? }
Entry kind "receipt" with `op` { op: {kind: "crew.formed", crew, name?}, text: "" } — a crew made under the entry's agent as manager; durable (the teams file's `founder`/`made`), same id `rcrew:<crew>` and `at` live and after a restart; the app words it
Entry (user) adds     attachments?: [{kind: "file"|"link"|"paste", path?, url?, title?, lines?, add?, del?,
                                      state?: "exists"|"changed"|"missing"|"outside"}]
turn.thinking         {agent, turn, text}  live only, never saved; turn.thinking_done {agent, turn, seconds}
turn.retrying         {agent, turn, in_s, attempt}   (2 s, 4 s, 8 s), then provider_failed or fallback
```

Asks (the decision dock, V68) add:
```
Ask { …, source: {kind: "reply"|"task", task?}, reason?, evidence?: {lines: string[], more: int},
      choices?: [{id, label, detail?, suggested?: bool}], clock_until?, irreversible?,
      batch?: [{id, text, state?: "allowed"|"denied"}], holding_up?: [task ids], say_why?: bool }
```
A choice with a `suggested` answer carries `clock_until` (the app's proposed-task clock, 12 s by
default; none when the clock is off, the stakes are irreversible, or the project set its own rule
for choices): the card reads "Picks Suggested in 12s", and when the clock runs out the engine takes
the suggestion and the ask closes. A proposed task its clock started closes with the receipt
"Started on its own after 12s · 15:04".
`holding_up` is set only on an ask whose `source` is a task: that task's id, then every open task
(neither `done` nor `failed`) whose `waits_on` reaches it, nearest first, each once however the
graph loops, at most 256 — the same dependency graph `Task.holding_up` is read from, as it stood
when the ask was raised or restated. It is never read from work a model names as blocked, nor from
how many tasks are open; an ask from no task carries none.
| method | params | result |
|---|---|---|
| `asks.answer` (adds) | `{…, why?, batch_item?, later?: bool, you_decide?: bool}` | `{}` |
| `asks.hold` | `{agent, id}` | `{}` stops the choice clock (H / typing) |

Calm attention (V84). `Ask.blocking?: bool` is what the engine KNOWS waits on an ask: `true` for
a permission and for the model's own question that parks its turn or names tasks; `false` only for
the model's own question whose turn carries on and that names no task (its answer arrives later as
a message). It is absent for every other kind and from older engines, and absent means waiting. An
empty `holding_up` never means `false`. A surface that holds a `blocking: false` ask until the
person pauses defers its clock and starts it when it shows the ask:
| method | params | result |
|---|---|---|
| `asks.defer` | `{agent, id}` | `{ask?}` the clock stops (`clock_until` absent) until `asks.shown`, or by itself after 5 min; `bad_params` for an ask not known to be `blocking: false`; `engine` from a conversation that cannot defer, and the surface then shows the ask at once |
| `asks.shown` | `{agent, id}` | `{ask?}` a deferred clock starts now for its whole length; any other ask comes back as it stands. A person's hold is never undone |
| `agents.since` | `{id, after}` | `{found, at?, entries, turns?, files?, first?, last_at?}` counted after one entry (a finished turn's last) from the same cached shapes a page is cut from: `turns` is told by the record's own marks — a person's message, a wake or landing note the session wrote, or a crew's delivery opens a turn; a reply marked complete or cut short by a stop ends one; work after an end is a new one — and is absent where the marks cannot tell one turn from two (a note inside a turn that never ended, an unmarked reply followed by more work); `files` the distinct paths the tool calls changed, absent past 400 calls; `found: false` for an entry the record does not hold |
| `tasks.since` | `{id, after}` | as `agents.since`, over the task's own record: the same room journal tail `tasks.entries` reads, once per call; `after` and `first` are the task's entry ids (`<task>/<entry>`); `entries` counts the record's entries only (the notes and sub-task rows `tasks.entries` lists after them are not in it); an anchor that is not on the record (a note, a sub-task row, a step line, older than the tail) is `found: false`; an engine without it answers `unknown_method` |

Queue and steer (V69):
| method | params | result |
|---|---|---|
| `agents.steer` | `{id, text, client_id, said?, reads?}` | `{}` lands after the current step (`steer` entry pending → consumed / fell_through) |
| `agents.queue` | `{id, text, client_id, said?, reads?}` | `{queued: Queued[]}` sent when the turn ends |
| `queue.edit` · `queue.remove` · `queue.move` | `{agent, id, text?/to?, said?, reads?}` | `{queued}`; edit after send → error `sent` |
Events `queue.changed {agent, queued: [{id, text, reads?}]}`.

Files and links in the composer (V69 @ reference, chips):
| method | params | result |
|---|---|---|
| `files.find` | `{agent, query, limit?}` | `{groups: [{label: "Changed in this chat"\|<repo>, files: [{path, add?, del?, dir?}]}]}` |
| `files.stat` | `{agent, paths}` | `{files: [{path, state, add?, del?, size?, binary?}]}` |
| `files.complete` | `{agent?, path, limit?}` | `{dir, entries: [{name, path, dir?}], more?, state?}` read-only path completion for the @ reference when what follows `@` is a path. The directory `path` names is listed — `path` itself when it ends in `/`, its parent otherwise; `~`, `.` and `..` alone are the folders they name — from exactly where it says: absolute, `~/` (home), or relative to the agent's workspace (home with no `agent`). The name after the last `/` narrows it, case-blind: names that begin with it, then names that hold it; folders first, then files, each by name. Dot names and `.git`, `node_modules`, `vendor` are left out unless that name starts with `.`. `entries[].path` is `path` up to its last `/`, then `name`, then `/` for a folder — what the composer writes after `@`, spelled as typed. `dir` is the folder listed, absolute. `limit` defaults to 50 and is clamped to 200; `more` says matches were left off. `state` is absent when the folder was listed, else missing (nothing there, or not a folder) · unreadable; neither is an empty folder. One readdir per call, the terminal folder browser's (at most 5,000 names of each kind); no file is opened, nothing is written. A negative limit or a path with a newline is `bad_params`; an unknown agent `not_found`; an engine without it `unknown_method` (use `files.find`) |
| `files.read` | `{agent, path, offset?, limit?}` | `{file: {path, resolved?, kind, outside?, symlink?, size?, modified?, add?, del?, lines?, offset, text, next?}}` read-only preview. `kind` text · binary · too_large · directory · missing · unreadable. Text only for UTF-8 without NUL up to 1 MiB; a page is at most 64 KiB (limit clamped), cut after a line or at a character, `next` where the following one starts; limit 0 answers facts only. `resolved` is canonical (links followed); `outside` is judged on it. Nothing over 1 MiB, no folder and no non-regular file is ever read. A bad offset is `bad_params`; an unknown agent `not_found` |
| `files.diff` | `{agent, path, base?}` | `{diff: {path, resolved?, outside?, state, folder?, repo?, base?, commit?, requested?, requested_unavailable?, in_history?, change?, old_path?, add?, del?, binary?, text, lines, truncated?, reason?, detail?}}` read-only. The working tree (staged and unstaged) against `base`, a commit or ref name (no option, range, path or revision expression; else `bad_params`), or HEAD when absent. `base` in the answer is head · requested · none; `commit` is the full hash compared with. A `base` that names no commit falls back to HEAD with `requested_unavailable: unknown_commit`; one that does is compared and `in_history` says whether it is an ancestor of HEAD. `state` changed · unchanged · untracked (whole file as added, base none) · ignored · not_git (`folder`, no counts) · no_commits · missing · unavailable (`reason` timeout · no_git · filter · unsafe · too_large · git_failed · changed_during_read). `change` modified · added · deleted · renamed (`old_path`, staged renames). `text` is hunks from the first `@@`, no file headers, at most 256 KiB and 2,000 lines, `truncated` when cut; `add`/`del` count the whole diff and are absent for binary. Read-only git with optional locks off, no external diff, textconv or fsmonitor; a file with a `filter` attribute is unavailable, never filtered. One 3 s deadline for every git of one diff; output counted as it arrives, the process killed past the bound. If the source changes during comparison, `changed_during_read` carries no hunks or counts. At most 2 diffs at once; one more is `busy` |
| `media.list` | `{agent, cursor?, limit?}` | `{sources: [MediaSource], next?, incomplete?}` OPTIONAL capability (unknown_method = this host cannot say, not "no media"). The conversation's media files newest first: files tools made (`origin` generated), files the person attached (`attachment`). `limit` default 50, max 100. The whole history is read backward in bounded chunks (at most 16 MiB per call, one page kept in memory); `next` is an opaque resume cursor (index identity + byte offset), absent only when the whole history was read; `incomplete` says a page is short because the call spent its read budget, not because history ended — keep following `next`. A cursor into a replaced or shortened index is `bad_params`: list again. A reply is at most 4 MiB as JSON, escaping counted: the engine ends a page BEFORE the record that would pass it (the cursor resumes at that record, nothing dropped); a record whose strings/arrays pass their bounds (256 KiB a string, 256 an array) or that alone cannot fit is an `engine` error, never cut |
| `media.get` | `{agent, source}` | `{source: MediaSource}` one source by its engine-issued opaque `id`, found anywhere in the history. There is no path parameter: the app can only name a source the engine already recorded. Unknown id `not_found` only after the whole history was read; a search that ran out of its 8 s is `busy`, never `not_found`. `MediaSource`: `id kind(image·audio·video) origin(attachment·generated·evidence·published·shared) path` (recorded canonical) `state` (available · changed · missing · outside · unreadable — ONLY `available` may be opened) `recorded` `version?` (`sha256:<hex>` where the engine held the bytes, else `file:<size>:<mtime_ns>:<ctime_ns>:<dev>:<ino>` as recorded) `live_version?` (file identity now) `mime? bytes?` (RECORDED size) `live_bytes?` (size now, existing regular file only) `width? height? duration_ms?` (only when actually read; `probed` when a legacy image header was read on this get) `created title? model?` (the model ASKED for — also `requested_model`; no provider reports a serving model, so there is none) `requested_count?` (the request's own N; files with equal `tool` were asked for together) `run?` (turn execution id) `tool?` (provider tool-call id of the call that made it) `message?` (not yet recorded) `references? said?` `declared? change?` published only (origin `published` = a file a shell command declared in bash `outputs`, looked at before and after that one command; `declared` is the path as the model spelled it, `change` is `created` (absent before, a regular file after, no other job running and no other tool call overlapping the command, and a file with one name) · `unchanged` · `modified` · `unknown`; only `created` also has a deliverable; `charge` stays `unknown`; `run`/`tool` are the call that declared it, `message` only if the producer supplied it; never produced by a background or promoted command, a failure, or a file the command was not asked to name) evidence only: `verb` (looked_at · listened_to · watched) `saw?` (looking model's words verbatim) `saw_omitted?` `seen_by?` `working_model?` `transcribed?` (origin `evidence` = files the agent looked at via view_image or read of an image, sound or video; PDFs/documents are not sources yet; show a person's own attachments once by comparing an evidence `path` with attachment `path`s) (exact input text given to speak) shared only: (origin `shared` = an image, sound or video the agent deliberately handed over with `share_deliverable`; never generated, never `published`, never evidence and never Made; only its header's facts — `mime bytes width? height? duration_ms?` — and the sharing call's `run`/`message`/`tool`; no `model`, count, batch, price, `declared` or `change`, and `charge` is always `unknown`; `original?` is the file a task's copy was read from, display only — open `path`) `charge` (charged · not_charged · unknown) `cost_usd?` (only when charged; a non-finite provider figure is unknown). Source ids are minted once per record (`src_…`) so two files written at one path are two sources; a record older than ids has a stable `srl_…` id derived from its session, origin, canonical path and creation time. A missing or changed file keeps its recorded metadata. `changed` compares size, mtime, and — where the platform reports them and the record has them — change time and inode/device, so a same-size rewrite with the mtime put back is still changed |
| `attachments.add` | `{agent, kind, path?/text?/url?}` | `{attachment}` paste over 12 lines or 2,000 chars → paste card |
| `attachments.remove` | `{agent, id}` | `{}` drops one staged attachment; a missing or already sent id is fine, an unknown agent is `not_found` |
| `render.list` | `{agent, cursor?, limit?}` | `{renders: [render], next?, incomplete?}` the conversation's renders — image attempts, video and music — newest first (limit default 50, max 100); a render is `{id, job, session?, kind image\|video\|music, state, model, started, ended?, duration_ms?, run?, call?, committed, path?, charge, cost_usd?, upstream?, error?}`. `state` running · finished · failed · cancelled · interrupted · unknown (read from disk by a host that does not own the conversation). `charge` charged · not_charged · unknown, and `cost_usd` only when charged. `committed` says the provider's bytes were accepted for saving; a cancelled render is never committed. `upstream: "unknown"` on a cancelled render: only local waiting stopped, the provider is never told to cancel. The whole history is listable: `next` (an opaque offset cursor) is absent only when it has all been read, and `incomplete` means the page is short because the read budget ran out, so keep following `next`. `render.changed {agent, render}` carries the whole record on start, ending and a late charge receipt (same id, same state); it replaces the record with that id and is never a second completion. An engine that does not serve renders answers `unknown_method`, never an empty list. Contract: `RENDER-CONTRACT.md` |
| `render.get` | `{agent, id}` | `{render}` one render by its stable `rnd_` id; an id absent from the WHOLE history is `not_found`; a lookup that ran out of time is `busy` |
| `render.cancel` | `{agent, id}` | `{outcome, render}` never waits on the provider. `outcome` cancelled (accepted before commit; nothing will be written) · too_late (already committed, it will finish) · already_ended (record unchanged) |

Attachments on a message (2026-10-15). One message carries at most 10 attachments; one paste at
most 200,000 characters and all of a message's pastes at most 400,000 (`attachments.add` refuses
a paste over the first bound). The same bounds hold at every door:

- `agents.create` takes `attachments: [{kind: "file"|"paste"|"link", path?, text?, url?}]`. They
  are checked whole BEFORE anything is made — kind, its own field, the bounds, a file or folder
  that is there now (relative to the workspace, or absolute) — and any refusal is `bad_params`
  with no agent, folder or `agent.changed` behind it. With attachments `text` may be empty; the
  agent answers `working`, the first user `Entry` carries `text` as typed and `attachments` with
  ids, and the model is sent the words `agents.send` writes for staged ones (`Attached file:
  <path>`, `Link: <url>`, the paste fenced). A file is sent as its path, never its contents.
- `agents.send` and `crews.message` take staged ids, and with them `text` may be empty. An id
  named twice is `bad_params`. Staged attachments are consumed only by a turn that started: a
  busy, missing or failed send leaves every id staged.
- `crews.message` sends to the resolved target (the manager by default, or `to` exactly); the ids
  must be staged on that agent (`not_found` otherwise). A message with attachments to a target
  mid-turn is refused `busy` and NOT queued — the queue keeps words only; words alone still
  queue as before.

Reads on a message (2026-10-15, hello feature `messages.reads`). `agents.send` and
`crews.message` take `said` (the person's own words; absent means `text` is theirs, `""` a
message of attachments or reads alone) and `reads`, the reads of references the app performed
to write `text` — a collection expanded at send time — as it found them then:
```
MessageRead { source: MessageReference, count, missing, unavailable, unread, more, complete,
              warnings?: [string], items?: [MessageReference] }
MessageReference { kind, id?, name?, owner?, path?, url? }
```
- The model is sent `text` and nothing else; `said` is never replaced by it. The engine never
  reads a reference again: the person's user `Entry` carries `text` = `said` and `reads` as sent,
  on the live `entry.added` and on every history page after a restart. A message sent without
  them has no `reads`, however many links its words hold.
- `count` is exactly the references the read returned; `items` the first of them, at most 8 and
  never more than `count`, descriptors only. `missing`/`unavailable`/`unread` count what it
  could not return, `more` says it was cut. `complete` is the app's answer and may not be true
  while `unavailable`, `unread` or `more` is set; `missing` alone may be complete.
- Bounds, all refusing the whole message `bad_params` before anything is recorded or sent: 16
  reads, 8 items and 8 warnings per read, 4,096 bytes per reference field, 512 per warning,
  counts 0 to 1,000,000, 128 KiB of read strings in all. Nothing is trimmed to fit.
- An engine without `messages.reads` answers a send carrying `said` or `reads` `unknown_method`.
  A crew message carrying either to a busy target is refused `busy`, never queued.

Reads on a correction and a queued message (2026-10-15, hello features `agents.steer.reads` and
`agents.queue.reads`, each its own door: an engine with `messages.reads` need not have them).
`agents.steer`, `agents.queue` and `queue.edit` take `said` and `reads` as `agents.send` does,
with the same bounds; `text` is still required and is what the model reads.
- `attachments` on any of the three is `bad_params`, plain or not; a correction and a queued
  message carry none. Send them with `agents.send`.
- `said` or `reads` to an engine without the feature, or to a conversation whose hosted engine
  cannot keep them, is `unknown_method` before anything is steered, queued or changed.
- `Queued.text` is the person's words (`said` when given), never `text`; `Queued.reads` the
  reads. The queue keeps the whole message and sends `text` when the turn ends.
- `queue.edit` with neither `said` nor `reads` replaces the message with plain words and drops
  the old ones. With either, the whole replacement is checked before the queue changes. An edit
  of a message already sent answers `sent` and queues nothing; any refusal leaves the queue as
  it was.
- The `steer` entry carries `text` = `said` and `reads`; a correction that falls through starts
  a turn whose user entry carries the same. A queued message, once sent, is an ordinary user
  entry carrying them. Both are said again on every history page after a restart or compaction.

Tasks as conversations (V65, V66) add:
```
Task { …, paused?: bool, waits_on?: [task], holding_up?: [task], model?, steps?, cost_usd?,
       tokens?, started_at?, started_by?, instructions?, children_done?, children?, finished_at? }
```
Many tasks at one level (V92) add, optional on `Task` and on `work.graph`'s `GraphNode`:
```
Task { …, source?: TaskSource, dismissed?: bool }
TaskSource { id, kind: "message"|"directive", text?, cut?: bool, at?: time }
work.graph totals { …, dismissed?: int }
```
`source` is the message (kind `message`) or the `/task` directive (kind `directive`) that started
the task, captured by the engine when the work was admitted and persisted with the task: `id` is
shared by every task made from the same message or directive (compare it, do not parse it); `text`
is the first 4096 bytes of the person's words with secrets redacted (`cut` says it was shortened);
`at` is when the session heard it, absent when unknown. A task made by a task (`parent`), a division's
part and a plan row dispatched into the graph carry their parent's source, never the conversation's
current message; a plan row carries the source of the record that owns it (the run row whose plan task it is, or the node whose plan id it is) or of its nearest owned ancestor — never of a record that merely shares its number. **Absent means unknown**: a task from before
sources were captured, or work nobody typed into (a woken turn), has none, and the engine never
infers one from a title, the words, a time or an id range. `dismissed` is the V92 reversible hide
(`tasks.dismiss`/`tasks.restore` below); a dismissed task is still listed and still counted in
`work.graph`'s `totals.nodes`/`by_state` — `totals.dismissed` says how many, so a surface leaves them
out of its default counts. `finished_at` is the recorded end of the work for a task or run row; plan
rows carry none yet.

| method | params | result |
|---|---|---|
| `tasks.pause` · `tasks.resume` · `tasks.retry` | `{id}` | `{}` · also on a handed-over task (a moved reply, a run): pause holds the run, nothing new starts and the step in hand is the last until resume |
| `tasks.brief` | `{id, text}` | `{}` Change the brief (read at its next step) |
| `tasks.note` (adds) | `{id, text, client_id}` | note entry; `note.changed {task, id, state, read_at_step}` |
| `tasks.entries` | `{id, before?, limit?}` | the task's own conversation (log, notes, its sub-tasks); a handed-over task's log is its worker's steps, one quiet notice each, and its notes carry `read_at_step` once the worker had them |

Limits the engine enforces and reports: 16 processes per agent (17th fails with a footer
"16 processes are running for this agent."); process/tool output kept to the last 512 KB
("… earlier output trimmed to the last 512 KB").

## Wave 4: Inbox (V04, V68 "The dock and the Inbox")

Every Ask from every agent (held or not), task and crew is one Inbox item. The
Inbox needs agents with open asks to stay held (never idle-released while an ask is open).

```
InboxItem { id, ask: Ask, where: {agent, crew?, task?, path: string[]}, at,
            list: "waiting"|"later"|"done", answered?: {option?, text?, by, at, where} ,
            context: Entry[] }   context = the two turns before the question
```
| method | params | result |
|---|---|---|
| `inbox.list` | `{list, query?}` | `{items: InboxItem[], counts: {waiting, later, done_today}}` |
| `inbox.answer` | `{id, option?, text?, why?}` | `{undo_until}` the answer is held 5 s ("Approving… Undo") then sent |
| `inbox.undo` | `{id}` | `{}` within the 5 s window: nothing is sent, item returns to Waiting |
| `inbox.later` · `inbox.back` | `{id}` | `{}` Later / back to Waiting |
| `inbox.open` | `{id}` | `{agent, task?}` where to navigate |

Events: `inbox.added {item}`, `inbox.resolved {id, by: "app"|"elsewhere"|"stopped"|"no_longer_needed", receipt}`,
`inbox.counts {waiting, later, done_today}` (drives the rail badge and the macOS Dock badge).
Answering anywhere (dock, task, crew, Inbox) resolves it everywhere within 1 s.

`where` names an agent of an open crew by the crew it works in and its handle there:
`crew` is that crew's id and `path` starts `[crew name, handle]` ("Trudeau Watch / verifier"),
then the task's title when the ask is a task's. The crew it works in is one it is an ordinary
member of before one it manages, and the root team last. Any other agent is named by its title.

A worker may report an optional logical `room` id for its job crew. The factory records the first valid id; the same id is idempotent and a replacement is refused. Workers send this metadata separately so an older server rejecting the unknown field cannot discard step, log, or cost reports. A worker on another computer can create a local crew with the same logical id; an unrelated local id collision is never adopted. Legacy jobs missing a room can recover it only from a unique team holding the recorded stage transcripts and manager conversation, with that conversation actually leading the team. Ambiguous or incomplete evidence remains unknown.

Factory-owned crews carry additive `factory_managed: true` when the factory has recorded their origin. A crew's name alone establishes no origin. Older engines omit the flag.

A factory-stage membership carries additive `factory_stage: true` in `members.brief`. When recorded, `text` is its actual opening brief, with secrets redacted; a resume's carry-on note does not replace it. No manager, edit date, revision, or role precedence is inferred. Real brief edits retained from before stage ownership was tracked remain visible with their recorded history. The brief belongs to the job: `members.brief.set` refuses with `bad_params` before writing, including after the crew closes. An empty stage brief remains read-only. Ordinary member briefs retain their existing behavior.

## Wave 5: Crews (U3, X2, V01, V02, V15, V15b, V15c, V16–V20, V64)

A crew IS a codeaf team (`internal/teams`). The data is the engine's own: the same
`<profile>/teams.json`, the same per-team Traffic log `<profile>/teams/<id>/traffic.jsonl`, the
same decisions file, the same usage ledger. The app adds no store and no fields of its own; it
renames only the words the UI shows (team → crew). A crew made in the app is a team in the
terminal's Teams page, and the reverse.

The app process does what the terminal window does for teams it holds
(`internal/tui3/teamtraffic.go`): it performs a manager's `start` and `stop` lines for members it
holds, and it keeps every member of an open crew held while the crew is open (never idle-released).
Closing a crew does NOT stop its conversations (the engine's Disband); the app only stops holding them.

### Crew ⇄ Team

| Crew (wire) | Team (`internal/teams`) | Notes |
|---|---|---|
| `id` | `Team.ID` | random, minted once (`teams.NewID`) |
| `name` | `Team.Name` | unique among open teams (the TUI refuses a duplicate) |
| `parent?` | `Team.Parent` | sub-crew = child team; `crews` nest to `depth_limit` |
| `root` | `Team.Root` | the "All teams" row; never closes |
| `manager?` | `Team.Manager` | the manager's conversation key, sent as its agent id; always a member |
| `hue`, `tier` | `Team.Hue`, `Team.Tier` | OKLCH hue degrees and lightness tier 0/1 |
| `tint` | derived from `Hue` | see Tint below; never stored |
| `made_at` | `Team.Made` | |
| `state` | `Team.State` + live members | `closed` when `Team.State == "closed"`; otherwise `needs_you` / `working` / `idle` from members (below) |
| `idle_since?` | members' stop instants | no member has run since: on an `idle` or `needs_you` crew, the latest instant any member stopped running. A member at rest stopped at its conversation's `updated_at` (the end of its last turn); a member whose turn is open but held by a question that stops the turn stopped when the first such open question was put. Absent while any member runs (or may: an open turn with no turn-holding question), on a `closed` crew, and when any member's instant is unknown. A `needs_you` crew's wait on the person is a separate fact: each waiting ask's own Inbox `at` |
| `closed_at?`, `closed_with?` | `Team.ClosedAt`, `Team.ClosedWith` | `closed_with` is the crew whose close closed this one (itself or an ancestor) |
| `report?` | `Team.Report` | id of the closing-report packet; the Report itself is that packet's `report` |
| `wrap?` | `Team.Wrap` | `{started, bound_s}` while a wrap-up runs |
| `settings` | `Team.Settings` | the crew's own overrides, each nullable: `questions_up`, `cap_usd_day`, `depth_limit`, `sub_share`, `wake` |
| `effective` | `File.Effective(id, Defaults)` | each value with its `*_from` origin (`team`/`ancestor`/`settings`/`closed`) for "· from Settings" |
| `spend` | `teams.TeamSpend(profile, id, today)` | `{day, usd, calls, by_member}`; a crew's pool counts every crew under it |
| `members` | `Team.Members` | see Member |
| `former_members` | `Team.FormerMembers` | handles kept for history links; no membership |
| `brief?` | parent Traffic `start` line with `team == id` | the words the sub-crew's manager was started with (derived, read-only) |

| Member (wire) | Team member | Notes |
|---|---|---|
| `agent` | `Member.Key` → agent id | the conversation; also listed in `agents.list` with `crews: [id]` |
| `crew` | the team id | |
| `handle` | `Member.Handle`, `Member.HandleBy` | 2–12 lowercase, unique in the crew |
| `title`, `workspace` | `Member.Word`, `Member.Where` | |
| `joined_at` | `Member.JoinedAt` | |
| `started` | `Member.Started` | made by the manager's `team_start` |
| `home`, `independent` | `Member.Home`, `Member.Independent` | which membership reports to a manager |
| `reports_to?` | `File.Home(key)` | `{crew, manager, distance}`: the manager this member answers to |
| `also_in` | other teams that hold the key | derived |
| `leads?` | the open child crew whose manager is this member | derived; a member that runs a crew under this one is drawn as that crew (a sub-crew), not as a plain member. `team_start` kind `team` makes it: the new conversation joins this crew under its handle, then takes the child crew's manager seat on its first turn |
| `state` | live state of the member's conversation | `running` / `asking` / `idle` / `finished` / `failed` (the engine's `MemberState` words); the latest Traffic `event` line says the same |
| `line?` | the member's live one-liner | the agent's running line, same as `agents.list` |
| `spent_usd` | `Spend.ByMember[key]` today | |
| `model` | the member conversation's model | from the agent, not the team |

**Tint** (derived, read-only): the UI's named tints are the nearest anchor to `Hue`, in OKLCH
degrees: `rose` 15, `sand` 75, `sage` 145, `tide` 215, `iris` 285; `graphite` is the root crew and
any crew with no colour yet. Setting a tint writes the anchor's hue (`Team.SetHue`, tier kept);
a new crew takes `teams.NextHue` (the farthest free hue) unless the UI picks one, and the five
`hue_choices` the terminal offers come from `teams.HueChoices`.

**LogLine** is a Traffic `Entry` as stored: `{id, crew, at, kind, from, to, text, handles?,
answers?, files?, member?, state?, packet?, reply?, team?}` with kinds `event`, `note`,
`directive`, `stop`, `start`, `question`, `answer`, `packet`, `close`, `reopen` (and `you`).
`from`/`to` are member handles, `you` (the person), `everyone`, or `several`: the manager is
named by its handle (the word `manager` only for a crew with no manager), a line to the room is
to `everyone`, and a line codeaf wrote is from the member it is about, else from `codeaf`. In a
line codeaf wrote, the manager's mark is the manager's `@handle`. The app adds `agent` (the
member's agent id) when `member` names one.

**Decision** is a decision packet (`teams.Packet`) as stored: `{id, crew (Origin), waits_on
(Team: a crew id or "you"), kind: question|conflict|cap|judgement|closing, raised_by, parties,
question, options: [{id, label, consequence}], recommendation?: {option, reason}, report?, cap?,
state: open|escalated|decided, decided_by?, decision?, reason?, trail: [{from, to, by, reason?,
at}], raised, at}`. A packet waiting on you is also an Ask (`kind: "question"`, `subkind:
"crew_decision"`, `path` = the crew names from `Origin` up the `trail` to you, "Backend → Release
→ you", `decision_kind` = the packet's `kind`: `question`, `conflict`, `cap`, `judgement` or
`closing`; no other ask carries `decision_kind`), so it reaches the dock and the Inbox. A
member's permission prompt never becomes a packet: it stays the member's own ask, as in the
terminal, and climbs to the person by itself:
`ask.opened` on the member (`asks.list {agent: member}` lists it), an Inbox item whose `where`
names the crew and the member's handle, the member's `state` `asking` with the question as its
`line`, and the crew's `state` `needs_you`. A member woken while its ask waits still reads
`asking`. The app answers it with `asks.answer {agent: member, id}` (or `inbox.answer`), which
closes it everywhere and leaves one receipt, in the member's conversation.

That needs the app to hold the member's conversation, and every member of an open crew is held.
A member the app cannot open (another codeaf window holds it and cannot be reached) is never
silent: on the crew's next look after the failure the app writes one line to stderr naming the
member and the reason, emits `log {level: "warn", message: "@verifier in Trudeau Watch could not
be opened: <reason>"}`, and puts one quiet `entry.added` notice in the manager's chat ("@verifier
could not be opened here, so its questions cannot reach you: <reason>. codeaf keeps trying, and
tries again when you open Trudeau Watch."). While it lasts the member reads `state: "failed"` with
`line: "can't open here: <reason>"`. It is tried again on a wait that starts at one crew poll
and doubles up to a minute, at once on `crews.log` for its crew, and when the person opens it;
once held it is itself again, with one `log {level: "info"}` and one notice ("@verifier is open
here again; its questions reach you now."). Nothing is said again while it keeps failing. A
member whose conversation is not on this machine is retried the same way and never said. Nor is one that failed only for want of a key: every launch looks at the crews before the
window has handed the keys over, so that refusal is the launch, not the member; it is not said,
the row is not marked, and the member is tried again on the next poll.

**Report** is `Packet.report` of a `closing` packet: `{done, left?, files?, spend_usd?,
incomplete?}` with options `close`, `close-now`, `keep-going`.

**Cap**: a `cap` packet carries `{crew, day, cap_usd, spent_usd, raise_to}` with options `raise`
and `stop`.

| method | params | result | engine call |
|---|---|---|---|
| `crews.list` | `{closed?: bool}` | `{crews}` | `File.Open()` / closed and not archived (a closed list leaves archived crews out) |
| `crews.archived` | `{}` | `{crews}` | `File.ArchivedTeams()`, newest archived first. A host without archive answers `unknown_method`: the app reads that as "archive unsupported", never as an empty list |
| `crews.get` | `{id}` | `{crew}` | `File.Team` + `Effective` + `TeamSpend` |
| `crews.create` | `{name, agents: [id], manager?: id, parent?, hue?}` | `{crew}` | `Update`: a new `Team` with `NewID`, `AddMember` per agent, `SetManager`, `SetParent` (refused past `depth_limit`: `CanNest`) |
| `crews.update` | `{id, name?, hue?/tint?, settings?}` | `{crew}` | `Update`: rename, `SetHue`, `SetSettings` (bands: cap ≥ 0, depth 1–10, share (0,1]) |
| `crews.message` | `{crew, to: "manager"\|agent, text, client_id}` | `{entry}` | the person's words go INTO that conversation (`agents.send` to the manager's or the member's agent), exactly as typing in its chat; to an agent mid-turn they are queued (`agents.queue`) and `entry.turn` is 0; words to a member are also told to the manager by the app layer (below) |
| `crews.log` | `{id, after?, limit?}` | `{lines, cursor}` | `ReadTraffic` (oldest first, cursor = last id) |
| `crews.log.at` | `{agent, post, crew_name?, crew?, around?}` | `{anchor}` | `ReadAround` + `Involves` on each crew the agent is in; with `crew`, that crew for a member of it or of a crew under it (`Descendants`) |
| `crews.decisions` | `{id?, all?}` | `{decisions}` | `Packets(id)` / `OpenPackets(scope)`; `all` includes decided ones (`History`) |
| `crews.decide` | `{packet, option?, text?, reason?}` | `{decision}` | `Decide(id, "you", …)`; a decided `closing` packet with close/close-now then `AcceptClosing` |
| `members.add` | `{crew, agent, handle?}` | `{member}` | `AddMember` (+ `SetHandle`), as `team_add` |
| `members.stop` | `{crew, agent, reason?}` | `{}` | Traffic `stop` line; the app stops the member's running turn (nothing is deleted) |
| `members.remove` | `{crew, agent}` | `{}` | `RemoveMember`; never offered for the manager (the store clears the manager when removed) |
| `members.handle` | `{crew, agent, handle}` | `{member}` | `SetHandle` |
| `members.home` | `{crew, agent}` | `{member}` | `SetHome`: which manager this member reports to |
| `crews.manager` | `{id, agent}` | `{crew}` | `SetManager` (the agent must be a member) / `ClearManager` with `agent: ""` |
| `crews.move` | `{ids, parent}` | `{}` | `MoveCheck` then `Move` (re-parent crews; `""` = top level) |
| `crews.wrap_up` | `{id, text?}` | `{}` | `WrapUpRequest(text)` to Traffic; the manager wakes and raises a `closing` packet → `decision.opened` |
| `crews.close` | `{id}` | `{}` | `Disband` (cascades to sub-crews; conversations keep working) + Traffic `close` |
| `crews.archive` | `{id}` | `{crew}` | `File.Archive`: only a closed crew (open → `bad_params`, root refused); marks that crew alone with `archived_at` (state stays `closed`; sub-crews are not archived, nothing is closed, stopped or deleted; no Traffic line) |
| `crews.reopen` | `{id}` | `{crew}` | `Reopen` (refused while the parent is closed; clears `archived_at` on every crew it reopens) + Traffic `reopen` |
| `crews.delete` | `{id}` | `{ids}` | `Delete` (closed crews only; conversations are never deleted) |
| `crews.defaults` · `crews.set_default` | – / `{key, value}` | `{defaults}` | `DefaultsAt` / `config.ApplyTeamDefault` (the five `teams.` rows) |

Events: `crew.changed {crew}`, `crew.removed {id}`, `crew.log {line}` (every new Traffic line),
`decision.opened {decision}`, `decision.changed {decision}`, `member.changed {member}`. A crew that
closes is said again as `crew.changed` with `state: "closed"` (it stays readable and can be reopened);
`crew.removed` is only for a crew deleted from the store. A crew's
`state` moves to `needs_you` while a packet waits on you from it or a member is asking.

### The app layer (fields teams do not have)

What the design needs and teams do not keep lives in the APP LAYER: `<home>/app/crews.json`, keyed
by team id and member key, never written into `teams.json`. The terminal never sees it. Every
field below is marked **app** on the wire's Crew/Member; everything else is **teams**.

| Field / method | Kept as | How the app makes it true |
|---|---|---|
| Crew `goal?`, `ends_at?` | app | stored; `ends_at` passing raises nothing by itself (shown only) |
| Crew `history: [task id]` | app | the manager agent's tasks at the moment the crew was made |
| Crew `history_tasks: [{task, owner}]` | app | the same ids with `owner`, the agent that held them (the manager at creation; never moves with the seat). A layer from before the owner was kept uses the team's `Founder`; with neither, absent. `task` is the id in `owner`'s `tasks.list` (not paged); open it from there |
| Crew `items: Item[]`; `crews.items` · `crews.link {id, kind, ref, title?}` · `crews.unlink {id, item}` | app | `Item {id, crew, kind: "job"\|"issue"\|"pr"\|"file", ref, title, at}`; links only, nothing is fetched |
| Member `role?` and `Role {id, name, instructions, model?, made_at, copies: [{crew, agent}]}`; `roles.list` · `roles.update {id, name?, instructions?, model?}` | app | a member given a role gets the role's instructions as a message in its own conversation and the role's model as its model; editing a role sends each copy the new instructions the same way |
| Member `cap_usd?`; `members.cap {crew, agent, cap_usd}` | app (the agent's app budget) | the same per-agent budget `agents.create` sets: bound to the member's conversation as its spend limit, so the engine stops it at the cap with a budget ask, as for any agent |
| Member `paused`; `members.pause` · `members.resume {crew, agent}` | app | pause = the team's own stop (Traffic `stop`, the turn ends) + paused mark; while paused the app does not let the conversation start a turn on its own (a wake is held; the person's words still go); resume clears the mark and sends nothing |
| Member `was? {role?, crew?, at}` | app | written when a member's role changes or it moves crews |
| `crews.message` to a member, "@manager sees it" | app + Traffic | the words go into the member's conversation; the app also appends one Traffic `you` line (`kind: "you", from: "you", to: <handle>`) so the manager reads "you told @web: …" in its team digest at its next step; it does not wake the manager |
| `members.start {crew, handle, brief, role?, model?}` | app | a new conversation in the crew's folder whose first message is the brief, as the person's own words; added to the crew under the handle |
| `crews.propose {agent}` and the Bring in helpers card (`Proposal`) | app | the app asks the agent's own model for a crew proposal as one JSON block `{name, helpers: [{handle, role, brief, model?}]}` **beside its chat, never in it**: a side ask (the engine's `Conversation.SideAsk`) that reads the conversation as text, runs no turn and writes nothing into the conversation, so no `turn.*` event, no `entry.added` and no `agents.entries` row ever carries the question or the JSON, and the model's later turns never see either. The answer becomes an Ask (`subkind: "crew_proposal"`) with options `create`, `edit`, `keep` ("Keep it one agent"), carrying the whole proposal as `ask.proposal: Proposal {id, agent, name, hue?, helpers: [{handle, role, brief, model?}], existing?, cap_usd_day?, goal?, ends_at?, at}`. The proposal is kept in `<home>/app/crews.json` until it is answered, so after a restart its ask is opened again (`ask.opened`) and listed by `asks.list`. `crews.propose` answers `{}` at once; one ask runs per agent at a time (a second press while it is out is answered by the first's card); a press that brings no card adds exactly one quiet notice whose id opens `crewprop-none-` — "No crew proposal came back in a form the app can read" for an answer that is not a proposal, "No crew proposal came back · <reason>" for an ask that failed, and "No crew proposal came back · Day cap reached · $X of $Y" at the day cap, where the day cap's ask also opens — so a window waiting on the press is told it is over either way. Answering `keep` leaves the receipt "Kept as one agent · you · HH:MM"; `create` leaves only the crew's formation receipt (`op: crew.formed`, id `rcrew:<crew>`) |
| `Proposal {id, agent, name, hue, helpers: [{handle, role, brief, model?}], existing?: [agent], cap_usd_day?, goal?, ends_at?}` → `crews.create {from: "proposal", proposal, edits?}` | app → teams | create = a team with the agent as manager; each helper is a manager's start (`start` line from `manager`, its brief the manager's own words from the proposal) which the app performs; `existing` agents are added |
| Crew `plan?` | not kept | needs engine work: the manager keeps no plan text the app can read |

### Starting a crew with its own manager, and the global manager (`crews.start`)

Hello advertises `crews.start` when the engine serves both halves; otherwise a
`start_manager` create and `crews.root.start` answer `unknown_method` before anything is made.

`crews.create {…, start_manager: true, manager_starts_members, request_id, goal?, said?,
client_id?, attachments?, workspace?, model?}` makes ONE new manager conversation for the crew and,
with a goal or attachments, hands the goal to it as its first message at most once. `said` is the
person's words exactly as given (not trimmed; an empty `said` is an empty entry); `goal` is what
the model is sent. The result adds `start {request_id, replayed?, manager, goal: {state,
client_id?, error?}}`.

`request_id` is claimed durably (`<home>/app/crew-starts/<sha256>.json`, exclusive link, synced)
BEFORE the crew is committed, and the goal is handed over only after that claim is rewritten to
say it may be. `goal.state`:

| state | meaning |
|---|---|
| `none` | no goal; nothing sent |
| `starting` | the first turn is starting behind the answer; `turn.done` says how it went (`error_kind: "uncertain"` = may have been sent) |
| `not_sent` | the crew and manager exist; the goal was provably never handed over (`error` says why). It is never sent later by itself |
| `unknown` | a repeat (`replayed: true`) of a request whose goal may have been handed over. Never sent again; read the manager's entries by `client_id` |

A repeat with the same `request_id` and the same ask is a lookup that makes and sends nothing; a
different ask is `bad_params`. A repeat whose earlier outcome cannot be read (claim, teams file or
crew unreadable; claimed but not committed; two earlier records) is `uncertain`, never new. A
request whose crew was deleted is `bad_params`; use a new `request_id`. One start runs per engine
at a time; a concurrent one is `busy`.

`crews.root.start {workspace?, model?}` → `{crew, manager, root_created?, manager_created?}` makes
the root (All teams) and its manager on the person's call. An existing manager whose conversation
is there is returned with both flags false. One whose folder is gone or carries the
permanent-deletion mark is replaced, and leaves the root. One whose folder cannot be checked is an
error, not a replacement. No first message is sent to the global manager.

## Wave 7: closing, archiving, finding (V21, V22, J03, J04, X4)

Agent adds `closed_at?`, `archived_at?`, `last_active_at?` (when the person last spoke in it;
absent when that is not known, always for a conversation they never spoke in such as a member
a manager started, and never the zero time: sort by `updated_at` then), `spent_usd` (all it has
spent). `state` adds `"archived"`.
| method | params | result |
|---|---|---|
| `agents.list` (adds) | `{workspace?, state?: "open"\|"closed"\|"archived"\|"all", query?}` | open only by default; `query` matches title or folder |
| `agents.move_to_closed` | `{ids}` | `{}` "Move to Closed": lets its conversation go, keeps everything, leaves the rail at once; `agent.changed` per id |
| `agents.archive` | `{ids}` | `{}` closed and put away (the conversation's own archive mark, the one the terminal's list reads) |
| `agents.reopen` | `{id}` | `{agent}` exactly as it was; opening or sending to a closed agent also reopens it |
| `agents.rename` | `{id, title}` | `{agent}` the person's name, kept literally; no namer renames it afterwards |
| `agents.idle` | `{days?}` | `{ids}` open agents nobody has spoken in for `days` (default `idle_close_days`) |
| `search.all` | `{query, limit_per_group?: 1–50, default 8}` | `{groups: [{kind: "agent"\|"crew"\|"task"\|"message", hits: [{kind, id, agent?, title, excerpt?, at?}]}]}` ⌘K / All crews and agents |

Where it comes from: closed and archived times are the app's (`<home>/app/state.json`); the archive
mark is the conversation's meta; agents and their spend are the conversations' rows; crews are the
teams; tasks are each project's task index (`id` is `<agent>:<n>`); messages are the conversation
search store (`id` is `<agent>#<seq>`, `excerpt` the matched words). An agent idle past
`idle_close_days` still reads `closed` without a `closed_at`.
Tidy commands are undoable until quit: the app keeps the inverse call (`agents.reopen`).

## Wave 6: Standing orders and reminders (V40, U2 7–8, X1)

An order IS an automation (`internal/automation`): one store (`<home>/v3/automations/automations.db`),
one clock, the same rows the terminal's `/automations` list reads. The app adds no store of its own.
The app holds an open window for its whole life, so the clock runs while the app is open; nothing
runs while it is closed, and a slot missed while the Mac slept or the app was closed runs ONCE,
late, when it is open again (`order.missed`).

```
Order { id, agent?, crew?, kind: "order"|"reminder"|"watch", title, workspace?, when, when_exact?,
        does, wont?, answers_in: "chat"|"crew"|"everywhere"|"not_here",
        trust: "asks_first"|"earning"|"trusted", approved_runs, trust_after: 5,
        budget_per_run_usd, time_per_run_s, state: "proposed"|"active"|"paused"|"stopped"|"done",
        fires_at?, last_run?: OrderRun, asking?: OrderRun, created_at }
OrderRun { id, ask_id?: "order:<run id>", due_at, at?, result?: "ok"|"quiet"|"incomplete"|"skipped"|"not_right",
           late?, line?, usd?, transcript? }
```

Order ⇄ Automation, field by field:

| Order | Automation | note |
|---|---|---|
| `id` | `ID` | |
| `agent` | `Origin.Transcript` (folder name), else `Origin.SessionID` | the conversation it was proposed in |
| `crew`, `wont` | `Crew`, `Wont` | new; kept in the automation's document |
| `kind` | `Look` set → watch; `Action.Do` → order; `Action.Say` → reminder | |
| `title`, `workspace` | `Title`, `Workspace` | |
| `when`, `when_exact` | `Describe(Schedule)`, `Exact(Schedule)` | |
| `does` | `Action.Do`, or `Action.Say` for a reminder | |
| `answers_in` | `AnswersIn` (empty reads `chat`) | new; the app decides where a result is shown |
| `trust`, `approved_runs`, `trust_after` | `Trust` (empty reads `trusted`), `Approved`, `TrustAfter` = 5 | new (trust.go) |
| `budget_per_run_usd`, `time_per_run_s` | `Limits.USD`, `Limits.Time` (defaults $5, 30 min) | the per-run cap the clock enforces |
| `state` | `Status`: proposed · active · paused · stopped · finished → `done` | proposed and stopped are new |
| `fires_at` | `Next` (active only) | |
| `last_run` | `Last` | `result` from `Outcome`: done → ok, quiet, skipped (+ line) → skipped or not_right, else incomplete |
| `asking` | the run in phase `asking` | new phase |
| `OrderRun.late` | `Why == "late"` | missed while asleep or closed |

| method | params | result |
|---|---|---|
| `orders.list` | `{agent?}` | `{orders}` |
| `orders.made_in` | `{crew, limit?: 1–100, default 50, cursor?}` | `{orders, next?, unknown}` — the orders MADE IN that crew: those whose recorded creation team (`Origin.Team`, written from the creation context the door that accepted the turn's opening message stated: `crews.message`, the first brief `members.start` sends a new member and the goal `crews.start` hands its manager state the crew's exact id, which the receiving engine records only when it can prove the crew holds that conversation — otherwise the order is unknown, and the message is sent exactly as before; a turn into which a message with another context was spliced is unknown too) is exactly the crew's id, in id order. Each page is ONE read (one SQLite snapshot) of at most 500 stored rows and 2 MiB of stored bytes by id after `cursor`, the sizes taken from the rows' headers before any document is read (≤ 100 orders, ≤ 256 KiB of encoded orders measured as the wire encodes them — HTML escaping on, so `<` counts six bytes — 2 s, at most 4 reads at once → `busy`): it may hold fewer orders than `limit`, even none, and still carry `next`; `next` is absent only when the store's end was reached with every row covered, and may be present on a page that happens to end there (the next page is then empty with no `next`). `next` is an opaque cursor of at most 128 bytes bound to this crew (another crew's cursor is `bad_params`) and to the store's walk revision, which moves when an order is created or deleted or its id or recorded creation team changes — by this build or an older one sharing the store — and at no other write (a run, a status, a wake time, an edit keeping the team); a cursor from another revision is `stale_cursor` and the walk starts again from the first page, so a finished walk is one revision of the store and never misses an order created behind its cursor. Only the fields an order shows are read out of a matching document (a watch's memo never is), and ids over 30 bytes are never read out. A row too large to read (an id over 30 bytes, named by its position; a stored document of ANY crew over the 2 MiB scan, which stops every crew's walk there until it shrinks; or an order over 256 KiB) is never skipped: the page ends before it, and a page that would start at it is `too_large`, with the cursor unmoved; a cursor this method did not give is `bad_params`; a missing walk revision or an order whose status or wake time is damaged is `engine`. A crew that does not exist is `not_found`, a closed one reads. It does NOT read who is in the crew, who manages it now or its workspace, and is NOT `Order.crew` (where an order belongs) — a manager or membership change never moves a row. Rows are `orders.list`'s shape without `last_run` or `asking`. `unknown` counts, in the stretch of the store THIS page covered and from the same read, orders with no recorded creation team (saved before the field; made in a turn whose door stated nothing — an older app, the terminal, words the app wrote itself; or not JSON). Sum it over the walk; the sum is the store's only once `next` is absent. It is a real 0 when 0, and a surface must show it beside the rows and never read "none made here" while it is above 0. An order made in the person's own ordinary send is known none: neither this crew's nor unknown. An order whose document names this crew but cannot be read fails the page (`engine`), never an empty page. Check `hello.features` for `orders.made_in`; without it the method is `unknown_method` |
| `orders.runs` | `{id, limit?: 1–100, default 20}` | `{runs}` newest first |
| `orders.ratify` | `{id, edits?}` | `{order}` the person's yes; NOTHING RUNS BEFORE RATIFY. Answers the proposal card too, if it is still up |
| `orders.change` | `{id, edits}` | `{order}` `edits: {title?, when?, does?, wont?, answers_in?, crew?, budget_per_run_usd?, time_per_run_s?, trust?: "asks_first"\|"trusted"}`; `when` is `every 15m`, `every 0 9 * * 1`, `at 18:00`, `in 2h` |
| `orders.not_now` | `{id}` | `{}` a proposed order is taken away (and its card answered no); refused once agreed to |
| `orders.pause` · `orders.resume` · `orders.stop` | `{id}` | `{order}` pause keeps the trust; stop is for good (listed, never resumed) |
| `orders.run` | `{id, run?, verdict?: "go"\|"skip"\|"not_right"}` | `{order}` with a verdict, the answer to the run that is asking (`run` 0 = whichever is); without one, run now — the person's own go, counted on the ladder |

Proposals: the conversation's `automation` tool saves what it proposes as `proposed` BEFORE the card is
up (launch `standingOrders`), and the card's ask carries `subject.ref` = the order id. The card's Save
ratifies it, Don't save or a change takes it away, and a card left unanswered leaves it proposed.
Work proposed this way starts at `asks_first`; reminders and watches never ask.

Per-run ask: when work that is not trusted comes due, its run waits as `asking` instead of starting,
and later slots are passed over while it waits. `order.asking {order, run}` says it; answer with
`orders.run {id, verdict}` or `asks.answer {agent, id: run.ask_id, option: go|skip|not_right}`. The ask
shows the run's budget (`budget_per_run_usd`).
The run's ask is an ordinary Ask everywhere an ask is: `ask.opened` when the run starts waiting
(and, for runs already waiting, as soon as the app opens), listed by `asks.list {agent}` for the
order's agent, and in the Inbox. Its id is `order:<run id>`: the RUN's number, which the store never
uses twice, so the id is stable for the life of the run and names one run of one order (an order
asks once per run). It is `{kind: "question", subkind: "order_run", title: "Run <title> now?",
detail: does, subject: {kind: "automation", ref: <order id>}, options: go · skip · not_right}`, and
`ask.closed` follows when the run stops waiting (`by: "app"` with the option when it was answered
here). When a run ends with something to say and the order answers in `chat` or `everywhere`, its
line lands in the order's agent's chat as a notice entry `"<title> · <result>: <line>"` whose
`ref` is the run's ask id.
Trust: `asks_first` (no go yet) → `earning` (n of 5 go's) → `trusted` after 5, runs alone. Any
`not_right` resets to `asks_first`.

Events: `order.changed {order}` (new, edited, ratified, run ended, changed in the terminal),
`order.removed {id}`, `order.asking {order, run}`, `order.fired {order, run}` (a run started),
`order.missed {order}` (a run that started late). The app polls the store every 2 s.

## Wave 8: Settings window (V42–V44, V60, V24)

`settings.get` / `settings.set` carry every row; `settings.set` takes any subset and answers the
whole `{settings}`, with `settings.changed {settings}` to every window.
```
Settings (adds) { appearance: "system"|"light"|"dark", text_size: "small"|"medium"|"large",
                  open_at_login: bool, dock_badge: bool,
                  pinned_models: [model id] (⌥⌘1–3, at most 3), checks_model, planner_model,
                  fallback_model }
```
Types, exactly as sent: `open_at_login` and `dock_badge` are JSON booleans (`true`/`false`, never a
word); `appearance` and `text_size` are words from the sets above (`enum` in the schema);
`budget_usd`, `day_cap_usd` and `task_cap_usd` are numbers or `null` (none); `proposed_task_clock_s`
and `idle_close_days` are whole numbers; `pinned_models` is an array of model ids (may be `null`
when none were ever pinned). `settings.set` refuses a field of the wrong type with `bad_params`
naming it, and refuses a field it does not know.

| row | kept as | notes |
|---|---|---|
| General: appearance, text size, open at login, dock badge | app (`state.json`) | defaults system, medium, false, true; the window applies them (login item, badge) |
| Models: default | profile `models.talk` | `default_model`, as before |
| Models: pinned | app | `models.pin {ids}` → `{settings}` |
| Models: Checks / Planner | profile crew rows `models.tiers.high` / `models.tiers.mastermind` | `""` follows the default (the router's pick); the same rows `/crew` writes |
| Advanced: fallback model | profile `models.fallbacks` | the first entry; `""` clears |
| Spending: day cap, per-agent budget, task cap | as before | `day_cap_usd`, `budget_usd`, `task_cap_usd` |
| Advanced: proposed-task clock, idle days, work folder | as before | the clock also times a suggested choice |

| method | params | result |
|---|---|---|
| `rules.list` | – | `{rules: [{id, kind: "tool"\|"bash", tool?, match?, action: "allow"\|"prompt"\|"deny"}]}` tool rules A–Z, then command patterns in match order |
| `rules.remove` | `{id}` | `{}` (`tool:<name>` or `bash:<pattern>`); not_found for a rule nobody wrote |
| `memory.list` | `{limit?: ≤500, default 200}` | `{memories: [{id, title?, text, owner, project?, origin?}]}` newest first; engine error "memory is off" |
| `memory.forget` | `{id}` | `{}` |
| `spend.export` | `{from: "YYYY-MM-DD", to: "YYYY-MM-DD"}` | `{path}` a CSV under `<home>/app/exports/` (at, day, conversation, model, role, seat, calls, tokens in/out, usd), both days included |
| `spend.ledger` | `{from, to, filters…, limit?, cursor?}` | `{from, to, totals, rows, next?, skipped}` — see Spending ledger below; unchanged `spend.export` stays the raw call CSV |
| `spend.ledger.export` | `{from, to, filters…, destination, overwrite?}` | `{path, rows, bytes, totals, skipped}` the same rows as CSV, written whole at `destination` |
| `spend.report` | `{from, to, filters…}` | `{from, to, totals, cells, models, gaps, not_recorded, limits}` — the ledger added up by local day, model and subject; see Spending report below |
| `slash.catalog` | – | `{commands: [{name, aliases?, forms: [{args?, description}], source: "table"\|"program", door?, brief_optional?, lands?, host}], skipped?, limits}` — the words the terminal chat answers to; see Slash catalogue below. An engine that does not serve it answers `unknown_method` (check `hello.features` for `slash.catalog`) |
| `session.actions` | `{agent}` | `{agent, actions: [{action, available, why?, programs?, scopes?, efforts?}]}` — what the conversation's own engine can be shown to do of `task.start`, `program.start`, `memory.remember`; see Session actions below. `unknown_method` without the `session.actions` feature |
| `session.act` | `{action, agent, client_id, task? \| program? \| memory?}` | `{action, agent, client_id, digest, state: "done"\|"uncertain"\|"in_progress", replayed?, at, task?, memory?, producer_error?, note?}` — runs ONE of the three through the producer the terminal uses, once per `client_id`; unknown params fields are `bad_params` |
| `session.action.get` | `{agent, client_id}` | the persisted receipt of that `client_id`; `not_found` is not proof nothing was sent |
| `providers.balance` | `{provider}` | `{provider, name?, supported, status, remaining_usd?, account_remaining_usd?, key_limit_remaining_usd?, stale?, last_success_at?, last_error_at?, error_kind?, message?, retry_after_ms?, busy?}` — what the provider itself reports; see Provider balance below |
| `spend.calls.export` | `{from, to, filters…, destination, overwrite?}` | `{path, rows, bytes, totals, skipped, not_recorded}` — one CSV row per matching ledger record; see Spending calls export below |
| `models.pin` | `{ids}` | `{settings}` |
Accounts (providers and keys) are Wave 2's `providers.list` / `keys.*`; GitHub sign-in is not served yet.

## Spending ledger (V62)

`spend.ledger` reads the machine's usage ledger a page at a time; `spend.ledger.export` writes the
same rows to a file. Both take ONE filter, so a preview and the file saved from it ask the same
question. Both are optional: an engine that is not a `SpendLedgerer` answers `unknown_method`.

**Filter.** `from` and `to` are required local calendar days (`YYYY-MM-DD`), both included, read
in the engine's zone from midnight to midnight (a 23- or 25-hour day is still that day). Every
other field is an exact match on what the ledger wrote: `model`, `kind` (`agent`, `task`,
`automation` — what the money was spent on, classified as the spend place classifies it),
`subject` (that thing's id; needs `kind`), `session`, `root`, `task`, `automation`, `run`,
`workspace`. A field the method does not know is `bad_params`, never ignored. `limit` is 1–500,
default 200; `0` or absent is the default, anything else is refused. `cursor` is a page's `next`
and belongs to the filter that made it — another filter's cursor, or one that does not decode,
is `bad_params`.

**Grain.** A row is either `grain: "run"` — every call one immutable execution made, summed
whatever models it used (`models` lists them, sorted) — or `grain: "call"` — one ledger line
that carries no run: a line older builds wrote, an auxiliary call made beside a turn, a receipt
that arrived late. Call rows are never grouped, with each other or into a run. A run is the calls the filter matched: one that crosses
the range's edge, or used a model the filter excludes, is summed over its matching calls only. `key` is a row's
stable identity (`run:<id>` or `call:<byte offset>`). Rows come newest first by `time` (the first
call's instant, with the engine's offset); `end` is the last call's.

**Unknown stays absent.** `kind`, `subject` and the ids are present where the ledger wrote them
and, on a run, only where every call agreed. `name` is present only where the engine holds a
title it can look up for that row alone: the title the app holds for the agent, a task's title
in the conversation named by `root` while that conversation is held open (a task row with no
`root` is not named), an automation's title in the store. No list of every conversation is read
to name a page, so an older conversation the app is not holding has no `name` here, and the
client falls back to the id. Nothing names who asked for a
call, so there is no `by`. `cost_usd` is absent when no record in the row was priced;
`unpriced_records` counts records with no figure and `unbilled_records` those marked as a
missing receipt — neither is $0.

**Totals** are the whole filtered range, exactly, whatever `limit`: `rows` = `runs` +
`unknown_run_records`; `records`, `calls`, `tokens_in`, `tokens_out`; `cost_usd` with its halves
`run_cost_usd` and `unknown_run_cost_usd`; `unpriced_records`, `unbilled_records`. `skipped`
counts ledger lines passed over: `malformed` (did not parse) and `oversized` (over 64 KiB). A
last line still being written is not read.

**Reading costs.** Every request reads the ledger from its first byte, line by bounded line; there
is no index, so a long history is a slower answer, not a larger one. What is held is the runs
in the range, under a budget, and a page chosen from them without sorting them all: a range
holding more than 100 000 runs (or an estimated 64 MiB of them) is `bad_params` asking for a
shorter range — never a truncated answer. The budget is an estimate used to refuse early, not a
bound on the engine's memory. At most two readings run at once; a third is `busy`, to be retried.

**Reply size.** One `spend.ledger` answer is at most 2 MiB of JSON. A page past it — a run that
met tens of thousands of models, or very long ids — is `bad_params` naming its size and asking
for a smaller `limit` or a narrower filter (`model`, `run`, a shorter range); models and rows are
never dropped to make it fit. The export has no such cap: it is streamed to disk.

**Figures.** A range whose figures cannot be added up truthfully — a negative token count, or
sums past what a 64-bit count or a finite dollar figure holds — is `bad_params` on both methods,
never a wrapped number, an infinite cost or a zero; a refused export leaves the destination as it
was.

**One paging, one reading.** Every page after the first reads exactly the bytes of the ledger the
first page read — the cursor carries that length and a hash of its first and last 4 KiB — so
`totals` are the same on every page and paging to the end visits exactly `totals.rows` rows. What
is recorded meanwhile appears in the next paging from no cursor. A ledger shorter than that
length, or whose first or last 4 KiB before it differ (rotated, restored, rewritten), refuses
the cursor as `bad_params` ("start again without a cursor"). A rewrite that keeps the length and
both ends is not detected; codeaf only ever appends to the ledger.

**Export.** `destination` is an absolute, clean path whose folder exists; the file is the CSV
below, written to a temporary sibling (mode 0600), synced, and moved into place. An existing
file is refused unless `overwrite: true`; any failure leaves the destination's previous bytes
as they were and removes the temporary file. The file holds the rows the filter matched at the
moment of the save, in ledger order (a run placed at its first call), and `totals` are those
rows'. Columns: `time, kind, name, models, tokens_in, tokens_out, cost_usd, by, grain, run, key,
subject, session, root, task, automation, workspace, end_time, records, calls, unpriced_records,
unbilled_records, unnamed_model_records`. `name` falls back to `subject`, then `run`; `models`
is `;`-joined; `by` is always empty; an unpriced cost is an empty cell; a text cell starting
`=`, `+`, `-` or `@` gets a leading `'`. `cost_usd` is written at full precision (the shortest
decimal that reads back as the same number), not rounded to four places: most calls cost less
than a hundredth of a cent, and a rounded column would sum to a different bill from `totals`.

Without `overwrite` the file is placed by a hard link, which refuses a destination that appeared
while the export was writing. A folder whose filesystem cannot make links is refused —
`bad_params`, "choose another folder, or pass overwrite" — rather than checked and renamed,
which could replace a file created between the two steps.

| method | params | result |
|---|---|---|
| `spend.ledger` | `{from, to, model?, kind?, subject?, session?, root?, task?, automation?, run?, workspace?, limit?, cursor?}` | `{from, to, totals, rows: [{grain, key, run?, time, end, kind?, subject?, name?, session?, root?, task?, automation?, workspace?, models, unnamed_model_records?, records, calls, tokens_in, tokens_out, cost_usd?, unpriced_records?, unbilled_records?}], next?, skipped: {malformed, oversized}}` |
| `spend.ledger.export` | `{from, to, …the same filters, destination, overwrite?}` | `{path, rows, bytes, totals, skipped}` |

## Spending report

`spend.report` takes `spend.ledger`'s filter (`from` and `to` required local days, both included, at
most 366 days apart; the same exact matches; no `limit` or `cursor`) and answers the lines it
matches added up into **cells**, one per stored local `day`, `model`, and subject (`kind`,
`subject`, plus `root` on a task, because a task id repeats in every conversation). `totals` is the
whole range, exactly; `calls` (provider requests) and `records` (ledger lines) are different
numbers. `models` are the distinct named models, sorted. Cells are sorted by day, model, kind,
subject, root. The engine folds nothing: no top three, no "Other" — that is the screen's job.

**Unknown is absent.** The ledger does not record which provider billed a call, whether the task it
served landed, or which crew owned it, and the engine does not join those from today's crews or
task states, which are not what they were when the money was spent. No cell has such a field;
`not_recorded` lists them (`provider`, `billing_source`, `outcome`, `crew`) on every answer. A
call that named no model is a cell with no `model` — not a model named "Unknown". A cell with no
`cost_usd` has no priced line; `unpriced_records` says how many, and it is not $0. `day` is the day
the ledger stored on the line, which can differ from the instant's day in the engine's zone;
`gaps.days_derived` counts lines with no valid stored day, placed by their instant instead.

**The totals do not claim a complete bill.** `gaps` is what is known to be missing:
`dropped_since_start` (records this process failed to write; not range-specific, and blind to
earlier runs), `malformed` and `oversized` (ledger lines skipped). `totals.unpriced_records` and
`unbilled_records` count lines with no figure or a missing receipt. All zeros is not proof that
nothing is missing.

**Bounds, refusals and cost.** One pass reads the ledger from its first byte to the length it had
when the request began; a line appended meanwhile belongs to the next request. It is linear in the
ledger, with no cache and no index. A range with more than 8 192 cells, or whose cells are
estimated at over 4 MiB, is `bad_params` asking for a shorter range or a narrower filter — never
a truncated list; likewise an answer over 4 MiB of JSON. The byte figure is an estimate used to
refuse early, not a promise about the engine's memory. Figures that cannot be added up (negative,
overflowing, non-finite) are `bad_params`, and a third concurrent ledger reading is `busy`.
`spend.ledger` and its cursors are unchanged.

| method | params | result |
|---|---|---|
| `spend.report` | `{from, to, model?, kind?, subject?, session?, root?, task?, automation?, run?, workspace?}` | `{from, to, totals: {cells, records, calls, tokens_in, tokens_out, cost_usd?, runless_records, unpriced_records, unbilled_records}, cells: [{day, model?, kind?, subject?, root?, records, calls, tokens_in, tokens_out, cost_usd?, unpriced_records?, unbilled_records?}], models, gaps: {dropped_since_start, malformed, oversized, days_derived}, not_recorded, limits: {max_cells, max_bytes}}` |

## Provider balance

`providers.balance` asks what a provider **itself reports** an account has left. Only OpenRouter
reports one today; any other well-formed provider id (lowercase letters, digits, `.`, `_`, `-`, 1–64
characters; anything else is `bad_params`, as is an unknown param — there is no `force`) answers
`supported: false`, `status: "unavailable"`, `error_kind: "unsupported"`, with no request made.
A custom connection that is labelled OpenRouter is not OpenRouter. The method is optional: an engine
without it answers `unknown_method`.

**Amounts, never guessed.** `account_remaining_usd` is `total_credits − total_usage` and is present
only when the provider reported both; `key_limit_remaining_usd` is the key's own cap left, present
only when the key has a cap. `remaining_usd` is the lesser of the two when both are present, else
the one that is. A figure the provider did not send is absent, never `0`; a negative figure is the
provider's and is not clamped; a non-finite one makes the answer `invalid`. No plan or OAuth
subscription is read as a balance.

**Status** describes the latest attempt: `known` (at least one figure), `unavailable` (no key
connected — `not_connected`; the provider answered without any figure — `not_reported`; or
unsupported), `unauthorized` (`key_rejected` for a 401, `key_expired` when the provider says the key
expired), `unreachable` (`timeout`, `unreachable`, or `provider_error` for 408, 429 and 5xx),
`invalid` (`bad_response` for a body that is over 64 KiB, malformed or non-finite;
`unexpected_status` for any other status). `message` appears only where there is something to do,
never carries a response body, key or address, and every string is at most 256 bytes; the reply is at
most 4 KiB and is refused, never cut.

**Cache and rate limit, in engine memory only.** One entry per account, identified by provider,
address and a digest of the key as read at the call. At most one request per account per 60 seconds,
failed or not; an ask sooner is answered from memory with `retry_after_ms`. Concurrent asks share
one request (at most 8 wait; a ninth, or any ask while the 8-account cache is full of requests in
flight, is answered from memory with `busy: true`). The request has a 10-second deadline and ends
with the engine. After a transient failure (`unreachable`, `invalid`) the last good figures stay,
with `stale: true` and `last_success_at`; a rejected or expired key, or an answer without figures,
drops them. A changed or removed key is a different account: the first ask under it is made at
once and nothing of the earlier account is returned. `last_success_at` and `last_error_at` are RFC
3339 and absent until the first such event. Nothing is written to disk and no events are emitted;
the screen asks, about once a minute while it is showing the figure.

## Spending calls export

`spend.calls.export` takes `spend.ledger.export`'s params (the same filter, `agent` included — a chat's
own lines and its tasks' lines both count — an absolute clean `destination` used byte for byte, and
`overwrite?`) and writes **one row per ledger record**, in the order recorded, with exactly these
columns: `date,time,crew,agent,task,model,provider,outcome,amount_usd,requests,tokens_in,tokens_out`.
`date` and `time` (`15:04:05`) are the call's instant in the engine's zone, the zone the filter's days
were read in. `agent` is the recorded root conversation, else the line's session; `task` and `model` are
the ids the ledger wrote, with no title joined. `amount_usd` is the full-precision price and **blank**
where nobody priced the call (not 0). `requests` is what the line says: an older line covering 41
requests is one row with 41, never split. **`crew`, `provider` and `outcome` are blank on every row**
because the ledger records none of them; the answer's `not_recorded` (`crew`, `provider`, `outcome`)
says so. `totals` is `{records, calls, tokens_in, tokens_out, cost_usd?, unpriced_records,
unbilled_records}` of exactly the rows written; it carries no run count, because the single pass keeps
no set of runs. An empty range is a valid file holding the header alone.

The file is written whole or not at all, by the same writer as `spend.ledger.export` (0600 sibling,
synced, linked into place without `overwrite` — a folder that cannot link is refused rather than
checked-then-renamed). Figures that cannot be added up (negative, overflowing, non-finite) are
`bad_params` and leave the destination as it was; a third concurrent ledger reading is `busy`. An
engine without the capability answers `unknown_method`; `spend.ledger.export` is unchanged.

## Wave 9: Collections (V75)

A collection is a hand-picked list of references: conversations, tasks, files, crews, processes and
other collections. It is a list, not a saved query and not memory: nothing is copied, nothing is
learned, and removing a reference never touches the thing. The store is `workspace.Store`
(`<home>/v3/collections.db`, the same file `codeaf collections` reads), separate from memory.
Hello carries the feature `collections`; an engine without it answers `unknown_method`.

Ref is `{kind, id, session_id?}`: `kind` is `collection` · `conversation` · `task` · `artifact` ·
`crew` · `process` (`standing` is legacy: found and removed, never added). A task is
`{kind: "task", id: "<n>", session_id: "<agent>"}`; a process is `{kind: "process", id, session_id:
"<agent>"}`; an artifact is a clean absolute path. Member is a Ref plus `added_at` (RFC 3339; absent
for a row written by a store older than schema 2).

Collection is `{id, name, created_at?, count, last_added_at?, holds?}`: `count` its direct members,
`last_added_at` the newest membership, `holds` (only when `collections.list` was given `holds`)
whether it directly holds that ref.

Target is what `collections.open` found a member to be now, never stored:
`{state: "live" | "deleted" | "missing" | "unknown", reason?, title?, in?: string[], status?, repo?,
branch?, path?, count?}`. `deleted`: the conversation, task, crew, process or collection is gone (a
nested collection the store could not find is `deleted`); `missing`: the file is moved or missing;
`unknown`: the owner could not be asked (it says nothing about the thing). `reason` is present only
when `state` is `unknown`: one short sentence of the form `<owner> could not be asked: <why>`, where
owner is `the conversation list`, `the task list of its conversation`, `the process list of its
conversation`, `the crew list` or `the file system`, and why is the owner's own words with secrets
redacted and every path said as `a path` (at most 200 characters), e.g. `the file system could not be
asked: file name too long`. `title` is the thing's own name (a conversation's title, a task's title, a crew's name, a
process's title or command, a collection's name, a file's base name). `in` is where it lives, outer
first: a conversation's crew path (empty: not in a crew), a task's or a process's crew path then its
agent's title, a crew's parent crew path. `status`: a task's state, a process's state, a crew's
state, a conversation's state. `repo` and `branch` for a file inside a git work tree (`repo` is the
origin's `owner/name`, else the work tree's folder name) and `path` the file relative to that tree.
`count`: a crew's members, a collection's direct members.

| method | params | result |
|---|---|---|
| `collections.list` | `{holds?: Ref, after?: string, limit?: 1–1000 (default 1000)}` | `{collections: Collection[], next?: string, total: int}` one page, oldest first (by creation); `next` is present iff more remain and is passed back as `after`; `total` counts every collection |
| `collections.create` | `{name}` | `{collection}`; a name already taken (case-insensitive) answers `conflict` |
| `collections.rename` | `{id, name}` | `{collection}`; `conflict` as create; renaming to its own name in another case is allowed |
| `collections.duplicate` | `{id, name}` | `{collection}` a new collection holding the same refs with the same `added_at`; `conflict` as create |
| `collections.delete` | `{id}` | `{}` the collection, its memberships, and every membership naming it in another collection; the things stay |
| `collections.add` | `{id, refs: (Ref & {added_at?})[]}` 1–200 | `{added: Member[], already: Ref[]}` all or nothing; `added_at` given restores a removed one in place (Undo); a collection ref that would make a cycle (itself, or one that already holds `id` at any depth) answers `cycle` and changes nothing |
| `collections.remove` | `{id, refs: Ref[]}` 1–200 | `{removed: Member[]}` what was there, with its `added_at`, for Undo |
| `collections.open` | `{id, depth?: 0–2 (default 1), limit?: 1–500 (default 500), after?: string}` | `{collection, members: Node[], more: bool, next?: string}` one page of the top level, newest added first (rows with no `added_at` last, newest inserted first); Node is Member + `{target, children?: {members: Node[], more}}`; a `collection` node within `depth` carries its children, deeper ones carry `target.count` only; at most 1500 nodes in all, a collection already on the path is never opened again; `next` (present iff the top level has more) continues the top level when passed back as `after`; `more` is true whenever anything was cut, the top level or the node budget |
| `collections.expand` | `{id, depth?: 1–3 (default 3), limit?: 1–500 (default 200)}` | `{collection, refs: (Member & {target})[], missing, unavailable, unavailable_refs: (Member & {target})[], unread, collections, more}` every live non-collection ref the collection holds to `depth` levels, each once, outer first, at most `limit`; every ref the store answered is counted, past the limit too: `missing` counts refs whose target is `deleted` or `missing` plus nested collections that are gone; `unavailable` counts refs whose owner could not be asked (`unknown`; never in `missing`), the first 50 of them listed in `unavailable_refs` with `target.reason`; `unread` counts nested collections deeper than `depth` that were not read; `collections` the nested collections read; `more` says `limit` or the store's own bound (2000 refs) cut the live refs |
| `collections.relink` | `{id, from: Ref, to: Ref}` | `{member: Member, already: bool}` in one write: `to` takes `from`'s `added_at` and place; when `to` is already a member, `from` is only removed and `already` is true (`member` is `to` as it was); `from` not a member answers `not_found` (`that reference is not in this collection`); `to` is checked before anything is written: an artifact must be a regular file at that path now (else `not_found` `nothing is at that path now`), a conversation, task, process or crew must resolve `live` (gone: `not_found`; owner not asked: `engine` with the reason as message), a collection gets add's checks (`not_found`, `cycle`); `standing` is refused as `to` (`bad_params`); relinking a ref to itself changes nothing and answers `already: true` |

| event | data |
|---|---|
| `collection.changed` | `{id, deleted?: bool}` after any change made through this door (create, rename, duplicate, delete, add, remove, relink), once per collection changed |

Codes: `conflict` (name taken), `cycle` (add or relink refused), `not_found` (no such collection, a
`from` that is not a member, a relink `to` that is not there now), `bad_params` (a malformed ref,
name, bound or cursor), `busy` (the store waited 10 s for another writer), `engine` (a relink `to`
whose owner could not be asked).

Paging. A cursor is an opaque string (URL-safe, no spaces); pass `next` back unchanged as `after`.
It names the last row a page answered, never an offset: `collections.list` keys on creation order,
`collections.open` on the top level's order (`added_at`, then insertion) and only for the collection
it was given for. A walk of pages never repeats or skips a row that existed when it began, whatever
is created, added, deleted or removed meanwhile: a collection created during a listing walk lands
after every page already read; a member added during an open walk is newer than the cursor and does
not reappear on a later page (re-open from the start to see it); a deleted or removed row is simply
not answered. A malformed cursor, a listing's cursor given to `collections.open`, or one collection's
cursor given to another answers `bad_params`. A collection within `depth` that the node budget left
unopened carries `children: {members: [], more: true}`, and a child list the budget cut says
`more: true`; the top level is always continuable from `next`. `codeaf collections list` and `show`
print everything.

Names are unique under simple Unicode case folding (Go's `strings.EqualFold`): one rune for one rune,
no locale, no normalization. `Ärger`/`ärger`, `ΣΊΣΥΦΟΣ`/`σίσυφος` (Σ, σ and final ς fold together)
and `K`/`K` (the Kelvin sign) are one name; `İstanbul`/`istanbul`, `Straße`/`STRASSE` and a precomposed
`é` against `e` + combining accent are two.

Schema 2: `collections.created_at`, `memberships.added_at` (unix ms, 0 unknown), kinds `crew` and
`process`, and deletes that cascade. A schema-1 file is migrated in place by the first write (one
immediate transaction); reads of a schema-1 file still work. A build that only knows schema 1 refuses
a migrated file ("unsupported collections database"), never misreads it.
## Compaction (V78)

The engine states compaction as facts; the app words them.

- **`agent.context?`** on every agent the process holds: `{tokens, window?, threshold?,
  summarizing?}`. `tokens` is the figure the compaction threshold is checked against; `window`
  and `threshold` are absent when no catalog, configuration or endpoint said what the window is
  (unknown, never zero room). `summarizing` is true only while a summary request is in flight.
  It moves at step boundaries and pass boundaries, never per token.
- **Summary entries** (`kind: "summary"`, id `cs<seq>`) carry `compaction`: `{seq, state:
  summarizing|settled|failed, tokens_before?, tokens_after?, stubbed?, folded?, summarized?,
  kept?, kept_clipped?, model?, turns?, covers_from?, covers_until?, failed?, full?, started_at?,
  ended_at?}`. The entry's `at` is `started_at` when known: a pass stands where it began, before
  any message that arrived while its summary was written, live and read back alike. `seq`
  numbers the conversation's passes from 1, failures included, and is the same live and read
  back, so a live `summarizing` entry is replaced in place by its `settled` or `failed` one.
  `summarized` zero means no model wrote a summary (a mechanical pass); `kept` is the summary
  the model now reads, at most 64 KiB (`kept_clipped` when cut), `model` the model that answered.
  `failed` is `interrupted|declined|empty|changed|not_shorter|error`; `full` only when the
  conversation is still at or over its window.
- **`agents.entries`** tells the whole conversation once, from the journal: every original line
  above every pass, each pass's entry at its own place. Nothing above a pass is dropped.
- **`agents.compact {id}`** starts one pass (the same pass `/compact` runs) and answers at once;
  it answers `busy` while a pass the app asked for on that agent is still running, or while the
  process already runs its bound of them. The pass says itself on `entry.added` (also for a pass
  another window asked for), and every call ends exactly once on `compaction.ended {agent,
  outcome: compacted|busy|nothing|error, message?}`. `compacted` from an engine too old to state
  pass facts arrives with no entry. Stop cancels a running pass's summary request.
## V76: Competence map (Settings → Models → Competence)

What each model has PROVEN, read-only. A **checked result** is one finished approved task
whose declared checks (the tests, lint or build its proposal named) ran against the work with
a physical exit status; every check's last run exiting 0 is a pass, any non-zero a failure.
It comes from the default run road: the run's check seats run the declared checks, the plan
store records each declared command's exit off the check's own trajectory, and the settled
run becomes one result on the ONE model that answered its work (after any fallback, read
only from the run's spend rows marked answered; a row naming the model a seat was built on,
as every row before this build does, leaves the run unrecorded). An answer is counted
whether or not it cost anything or carried usage, so a free second model is a second model.
A run answered by more than one model or by an unnamed one, a run another approved task
joined, and a run with ANY declared check the shell could not start (126/127, or a negative
code) is not recorded — never folded as a pass from the checks that did run. Never counted:
the checker's verdict, unchecked done, a model's account of itself. A check a signal killed (128+signal) or that
the run cut off is a failure, never a pass. The ledger is
`<home>/v3/checked-results.jsonl`, append-only, and survives deleting the task or the
conversation. Work finished before it existed is no evidence. `undone` is in the shape and
reads 0 today: the only undo door, `quick.undo`, takes back quick tasks, which run no
declared checks, and an undo never makes a result. `source.kind: "conversation"` is reserved.

| method | params | result |
|---|---|---|
| `competence.map` | `{model?, scope?}` | `{model, models: [{id, results, last_at}] (≤50), scope, filters: [{id, kind: "place"\|"work", path?, place_kind?, removed?, words?}] (≤200), groups: [{class, scopes: [Scope]}] (always strong, frontier, weak, stale; ≤300 scopes), totals: {scopes, results}, policy: {stale_after_days, strong_at_least, weak_at_most, min_results}, read_at, truncated, omitted: {scopes, filters, models}}` |
| `competence.evidence` | `{model, scope, outcome?: "passed"\|"failed"\|"undone", cursor?, limit?: default 7, ≤100}` | `{model, scope, counts: {all, passed, failed, undone}, items: [Item] newest first, next_cursor?}` |

```
Scope { id, place: {id, path, place_kind: "repo"|"folder", removed}, work: {id, words},
        class, results, passed, failed, undone, last_at, since? }
Item  { id, title, at, outcome, source: {kind: "task"|"conversation", id, agent, turn?, title?},
        source_missing: ""|"task"|"conversation", check?: {command, outcome}, checks: [...] (≤8),
        undone_at? }
```
`model` absent is the model with the newest result (`""` with none). A scope is a place and
the engine's exact kind of work (the task title's words), so its id is stable. `scope` on the
map is a `place:`, `work:` or `scope:` id; an unknown one, or a cursor from another model or
scope, is `bad_params`. Class: stale when nothing landed in `stale_after_days`; otherwise over
that window, passed/results ≥ `strong_at_least` with ≥ `min_results` is strong,
≤ `weak_at_most` is weak, else frontier. An item's outcome is decided over every check that
ran, and `check` is the first that failed (else the last run) and is always among `checks`.
`totals` count every matching scope; `omitted` is what this answer left out for size.
`place.removed` is a folder no codeaf conversation works in now. `source.title` is the owning
conversation's current title. Each call reads one complete ledger snapshot up to 32 MiB and
answers within 10 s or errs `engine` "competence read timed out". A larger ledger returns
`engine` "checked-result history exceeds this bounded read; competence unavailable"; an
unreadable or unsupported row returns "checked-result history has an unreadable row;
competence unavailable". Neither returns groups, counts or partial classifications.
`truncated` remains false for wire compatibility; `omitted` only describes output lists,
whose displayed scopes were classified from complete readable evidence. There is no event:
the app reads on open, filter and Retry.
`codeaf competence --json [--model id] [--scope id]` prints the same `competence.map` answer.

## V79: Task models (Settings → Models → Task models)

How one task gets its worker, planner and checker. **Auto** (the default) picks each role per
task from the models the person allows, best quality for the price inside the caps; a role may
be **pinned** to one model, which Auto never changes. **One model for every task** runs every
role on one explicit model and picks nothing. Read at task ADMISSION and frozen into the task's
decision: running tasks keep their picks. Engine asserts `TaskModelsLister`; an older engine
answers `unknown_method`. Wire types: `internal/appwire/taskmodels.go`.

| method | params | result |
|---|---|---|
| `settings.models.get` | `{}` | `{task_models: {read_at, revision, mode: "auto"\|"one", one_model: ModelRef\|null, one_model_warning: null\|"provider_unavailable"\|"pin_unknown", roles: {worker, planner, checker: {pin: ModelRef\|null, warning: null\|"pin_not_allowed"\|"provider_unavailable"\|"pin_unknown"\|"no_eligible", eligible_count}}, providers: [Provider]}}` |
| `settings.models.list` | `{provider?, q? (≤100), allowed_only?, role?, cursor?, limit?: default 40, ≤100}` | `{read_at, revision, models: [Row], total, next_cursor?}` |
| `settings.models.set` | `{revision?, mode?, one_model?: ModelRef, pins?: {worker?, planner?, checker?: ModelRef\|null}, allowed?: [{model: ModelRef, allowed}] (≤200)}` | `{task_models}` |

```
ModelRef { id, provider? }        // a catalog id and the provider route; a row's provider is required in allowed[]
Provider { id, name, state: "connected"|"expired"|"no_key"|"unavailable", fix: "none"|"sign_in"|"add_key",
           reason?: "key_refused"|"signed_out"|"out_of_credit"|"turned_off", key_hint?, model_count, allowed_count }
Row      { id, provider, name, billing: "metered"|"plan"|"local"|"free", allowed, available, auto_eligible,
           price: {in_per_m, out_per_m}|null,
           reason?: "not_allowed"|"no_price"|"provider_unavailable"|"route_unavailable"|"cannot_do_role" }
```
A tick is one model on ONE provider: unticking GLM 5.3 on OpenRouter leaves it ticked on Z.ai.
It is kept in the allowed rule `/crew models` reads, as `-model@provider` / `+model@provider`.
`price` is that provider's OWN published price (OpenRouter's rows from OpenRouter's catalog, a
direct provider only from its own cached listing), and null when it published none — such a row is
still listed, tickable and pinnable, and Auto skips it (`reason: "no_price"`); a direct route with
its own price is weighed at that price. A published zero is `{in_per_m: 0, out_per_m: 0}`; a
listing seeded from model ids alone publishes nothing. A provider with no key
is a header with no models; a provider whose key was refused, whose sign-in ran out, or whose
credit is gone keeps its models listed with their ticks, `available: false`, until it answers again.
`fix` is only a fix this engine can start: `sign_in` where `providers.sign_in` serves it.

A pin may only be SET to an allowed model; unticking a pinned model succeeds, the pin stays saved,
the role reads `pin_not_allowed` and runs on Auto. Allow it again with `allowed`, or remove the pin
with `pins: {role: null}`. `settings.checks_model` and `settings.planner_model` are the checker and
planner pins (the same rows). One mode needs `one_model` (in the patch or saved) on a connected
provider that lists it; it ignores pins and ticks, which stay as they are for when Auto is back.
A role with no eligible model fails the task's admission with the typed no-eligible refusal (the
task waits and asks); it is never moved onto the conversation's own model. So does an ALLOWED pin
(or the one model) whose route is known not to answer — provider gone or off, key refused, out of
credit, or resting — and setting such a one model is `bad_params`; only a pin the allowed models
no longer admit is set aside for Auto.

The caps are `settings.task_cap_usd` (the router's per-task limit) and `settings.day_cap_usd` (the
app's all-usage day cap); neither is part of this family. Each patch is validated whole and written
in one profile write, refused with `conflict` when its `revision` is stale or the rows moved under
it. `revision` covers every task-models row and not the caps. `read_at` is when the engine's read
BEGAN; one answer is one consistent observation, or `busy` when the rows kept changing under three
tries. `settings.changed` carries `task_models` (the get answer) only when the change touched a
task-models row: `settings.models.set`, or `settings.set` of `checks_model`/`planner_model`. get and
list answer within 10 s or err `engine` "task models read timed out"; a cursor is only good for the
filters and revision it came from (else `bad_params`).

## Kept running: services (V77)

An agent may ask to keep a command running after its turn. The ask is an Ask with
`kind: "approval", subkind: "keep_running"` and exactly three options, in this order:
`allow` ("Allow"), `allow_never` ("Allow, never restart") and `deny` ("Deny"). Deny runs the
command as an ordinary process that ends with the turn. The person's answer is kept for that
exact command in that exact folder with the health check they saw; the agent cannot change the
restart policy afterwards, only the person can (`services.policy`).

The ask carries the proposal typed, read from the engine's card and never from its words:
`ask.keep_running: {name, command (redacted), dir, health: ServiceHealth, policy, call_id?,
timing: ServiceTiming}`.

```
ServiceTiming { check_every_s: 10, check_timeout_s: 5, unhealthy_after: 3, first_backoff_s: 2,
                restart_limit: 3, restart_window_s: 600, healthy_reset_s: 600, stop_grace_s: 5 }
```
The figures are the supervisor's constants, the same for every service, sent so the app never
spells its own.

A service is a `Process` with `service` set, listed in the same Running/Ended list as the
agent's other processes and counted with them. Its id is `"<agent>:s<n>"`, opaque, never
reused. `processes.output`, `processes.follow`, `processes.unfollow`, `processes.stop` and
`processes.rerun` accept a service id too (rerun = Start again with the saved command).
`process.changed {process}` carries every change, the episode included.

```
Process (adds) { service?: Service }
Service { name, dir, health: {kind: "port"|"url"|"cmd", port?, url?, cmd?, words}, timing: ServiceTiming, open_url?,
          policy: "never"|"on_failure"|"always",
          lifecycle: "starting"|"healthy"|"unhealthy"|"restarting"|"gave_up"|"not_running"|"stopped"|"failed",
          reason?, running, stopping?, pid?,
          started_how?: "agent"|"restart"|"person"|"after_restart"|"re_adopted",
          started_by: {label?, turn?, call_id?, at}, check?: {ok, at, fails, detail?},
          restarts, restart_limit, backoff_until?, re_adopted?, re_adopted_at?, port_conflict?,
          episode?, episode_open?, episode_reason?, episode_started_at?, episode_resolved_at?,
          stopped_by?, output_truncated?, removed? }
```

`Process.state` is `running` while the engine owns a live process (`service.running` or
`service.stopping`) or while it is starting or restarting, so a restarting or pending service is
active; `stopped` for a stopped service; `failed` for `failed` and `gave_up`; `ended` for
`not_running`. A stop that could not prove the process gone leaves `running: true`.

Checks run every 10 s, the first 10 s after start, each with 5 s to answer; a url check passes on
200 only. Three failures in a row make it `unhealthy`; one pass makes it `healthy` again.
Automatic restarts wait 2 s, 4 s, 8 s, at most `restart_limit` (3) in any 10 minutes, and the
count clears after 10 healthy minutes; past that it is `gave_up`. `services.restart` is the
person's restart and is not one of the automatic ones. `services.stop` sends TERM, then KILL
after 5 s; output is kept. `services.start` starts it again from the saved command.
`services.remove` takes it off the list and keeps its output, still readable by its id.

An **episode** (`episode`, e.g. `"s3.2"`) is one run of trouble from the first failure to
healthy again or stopped. The app shows exactly one Inbox line per episode, updated in place
by `process.changed`, and moves it to Today when `episode_open` turns false;
`episode_resolved_at` is fixed when it closes, and the done line's `answered.at` is that moment. An
episode already open when the app starts has its line at once, not at the next check. The Inbox
line is an `Ask {kind: "question", subkind: "service_health", subject: {kind: "service", ref:
<process id>}, options: restart, stop}`; `inbox.answer` with `restart` or `stop` acts on the
service, and the line resolves `by: "recovered"` or `"stopped"`.

The origin chat gets one notice per episode: an `entry.added` footer `footer_kind:
"service_trouble"`, `ref` the process id, id `"svc<process id>.<episode>"`. It is kept on disk
per agent, never cut, and `agents.entries` lays every kept notice into the history by time, so
it is there after a relaunch and its `ref` still opens the output of a removed service. A
notice's id is a valid `before` cursor.

`agents.close` stops the agent's services and waits until each is gone; while it waits nothing new
starts for that agent (a start answers an `engine` error, "its agent is closing; nothing new is
started for it"). If one would not stop, `agents.close` fails naming it and the agent stays open.
The idle sweep never lets go of an agent with a service up, starting or restarting.
`processes.follow` of a service is counted per view: each follow adds one and each unfollow takes
one, the tail ends at none, on removal, or at quit; neither an unfollow nor a quit waits for a
send the window is not reading.

`services.list {agent?}` with no agent lists every agent's, for the quit sheet's count.
`services.stop_all` stops them all before a quit that was asked to; an error means at least
one could not be proved stopped. Quitting without it leaves them running, and the next codeaf
re-adopts only a process it can prove is the same one (pid, start time and boot).
`services.output {id, from_line, max_lines?}` and `services.tail {id, lines?}` read at most 1000
lines and 256 KiB per call; `truncated` says earlier output is no longer kept.
An engine without a supervisor answers every `services.*` with `unknown_method`.

`ports.inspect {port}` (1 to 65535) answers `state` `free`, `in_use` or `unknown`, with `holder`
`{pid, name}` only when the engine proved it (a positive pid and the program's name, never a
command line or a path) and `detail` when `unknown`. `ports.suggest {after}` (1 to 65534) answers
the first port past `after` that was free when bound, starting at `max(after+1, 1024)`, looking at
no more than 128 candidates in about a second and never wrapping past 65535; a candidate that is
busy or could not be checked is skipped, and none found is `not_found`. BOTH ARE OBSERVATIONS: a
port is not reserved, a later bind can still lose, and a port held by another program is never
touched. Out of range is `bad_params`, a spent concurrency budget `busy`, a deadline `engine`.
An engine without the capability answers `unknown_method`.

## One agent's own budget

`settings.budget_usd` is the profile's DEFAULT conversation limit, the figure a conversation with
nothing of its own is built with. It is never one agent's cap: read that from `Agent.budget`.

```
AgentBudget { scope: "agent", source: "set"|"unlimited"|"inherited",
              limit: "limited"|"unlimited"|"unknown", usd?, base_usd?, raised_until?,
              from: "record"|"engine", engine?: { limit: "limited"|"unlimited", usd?, spent_usd } }
```

`source` is where the cap comes from: `set` is this agent's own figure (`agents.create
{budget_usd}`, `caps.raise {scope:"agent"}` or `agents.budget.set`), `unlimited` is an agent
explicitly set to none, and `inherited` is nothing of its own — it runs on the limit its engine
read when the conversation opened. `limit` is what is in force now; `usd` is present only when it
is `limited`, `base_usd` only for `set`, and `raised_until` while a `for:"today"` raise lifts `usd`
above `base_usd` (until the next local midnight). Every `Agent` the engine sends carries `budget`
from the app's own record, so an `inherited` one reads `limit:"unknown"` there; an absent `budget`
is an engine that does not say, which is also unknown. Neither is ever `settings.budget_usd`.

`agents.budget.get {id}` adds `engine` — the open conversation's own limit and the spend it is
measured against — when the conversation is held and its engine can say; an `inherited` budget is
then that figure, with `from:"engine"`. `agents.budget.set {id, usd}` takes a positive amount for
good (clearing a raise for today) or `null` for none; a missing `usd`, zero, a negative or a
non-number is `bad_params`. It is written to the app's file before it is bound to the open
conversation; a write the disk refuses changes nothing, and one the conversation refuses is rolled
back and answers `engine`. If rollback cannot be confirmed, the error explicitly asks the caller to read the budget again. A set closes the agent's open budget ask and
retries the turn it refused. `caps.raise {scope:"agent", for:"today"}` on an `unlimited` agent is
`bad_params`. An engine without the capability answers `unknown_method`.

The cap is the agent's alone: it is measured against the conversation's own spend over its whole
life (the journaled figure, kept across reopens) and checked before each turn, never cutting one in
half. A task's cap, the day cap and a task's models are other limits. A `set` or `unlimited` budget
is in the app's file and bound again at every open, so a relaunched app reopens an agent with the
same cap.

The top-level recorded cap is what the app binds before the next turn. The optional `engine`
reading is the conversation's last bound configuration; it can differ after a temporary raise
expires until that next bind. In-flight work retains its admission and is never cut short.
Successful creation, individual Set and `caps.raise` publish `Agent.budget` on `agent.changed`
so other windows update without polling. Ordinary list enrichment does not query the engine.

Ledger display names are optional and limited to 4096 UTF-8 bytes. An oversized title is omitted rather than truncated; the original subject IDs and billing figures remain in the row and CSV. Automation-name caching is therefore bounded by both its 256 entries and the per-title byte limit.

For task report cells without a recorded `root`, optional `session` preserves the recorded session identity; these tasks are never merged solely because their numeric task IDs match. An absent root remains absent.

The optional `agent` filter in `spend.ledger`, `spend.ledger.export`, and `spend.report` matches the recorded `root`, falling back to recorded `session` only when root is absent. It includes that conversation and its recorded task charges without joining current crew membership. Report cells preserve this identity as optional `agent`. Existing queries without this field retain their cursor fingerprint.
## Work graph (V87, V88)

`work.graph {scope, view?, cluster?, focus?, cursor?, limit?, if_revision?}` answers the facts of work that
waits on other work. It is GENERAL: tasks, who owns them, which waits on which, what state each is in and since
when, what each changed and cost. It knows no column, coordinate, colour, card or board; a surface computes every
one of those. The types are `internal/workgraph`'s (`GraphRequest`, `GraphResult`, `GraphNode`, `GraphEdge`), the
schema is generated from them, and the call is read-only. An engine that does not serve it answers
`unknown_method`; `hello.features` carries `work.graph` for one that does.

* **Scope.** `{kind: agent|crew|task, id}`. An agent is one conversation, a crew is every member of it and of the
  crews under it, a task (`"<owner>:<n>"`) is that task's own sub-tasks at every depth, with the task itself as
  `about.root`.
* **Views.** `nodes` (default) one page of tasks in creation order and the edges whose dependent is on the page;
  `clusters` the sub-crews (crew scope) and task groups with true member counts; `people` the crews and members
  with per-member task counts; `metrics` the poster figures. `cluster` expands one cluster through `nodes`;
  `focus` answers one task's neighbourhood along waits-on links.
* **Identity.** A node's `id` is `"<owner>:<task>"`, the spelling `tasks.*` and `task.changed` use, so two
  agents' task 3 never collide. `owner` is the conversation (its folder name). A task of a run's plan store keeps
  the store's own token (`"<owner>:t-<id>"`), as `tasks.list` lists it.
* **Edges.** `from` is the DEPENDENT and `to` the PREREQUISITE. An edge exists only where a wait was stored and
  is enforced: `via:"record"` (the task's own `depends_on`, or a plan store's hard dependency) or
  `via:"external"` (a wait on a task of another conversation, `Agent.WaitOnTask`). Nothing is ever inferred from
  a title, a body or an order. `target` says how the prerequisite stands against the scope: `node` (may be on
  another page; `to` is always a real id), `external` (a real task elsewhere, with `to_state` when readable) or
  `missing` (the record names a task its owner no longer has). A node's `links` is `recorded` or `unrecorded`: an
  adaptive run's row persists no waits, so zero edges there means unknown, not none.
* **Time.** `clock.admitted_at`, `started_at`, `state_at` (when the task entered the state it is in) and
  `ended_at` are what the record carries. A record written before they were kept carries none, and none is
  replaced by the read time.
* **Bounds.** A page is at most 500 nodes (default 200) and 4096 edges (it ends early and says so through
  `next_cursor`); 64 edges and 50 paths per node (`waits_on_total` and `files.total` are the true counts); titles
  160 runes, text 1000. `totals` are always the scope's true sizes. A scope is NOT a promise of any size: see
  "Memory" for what is admitted and what is refused. A cursor is opaque, signed and bound to scope, view and cluster; a tampered or foreign
  one is `bad_params`. A cursor outlives a revision change (compare `revision` with the one you hold and restart
  if it moved).
* **Revision.** `revision` names the exact source facts. `if_revision` with the current one answers only
  `{unchanged: true, revision}` and builds nothing. The sources are files, so the revision is stat calls; the
  events the wire already has (`task.changed`, `member.changed`, crew events) say that something moved, and no
  caller polls.
* **Coverage.** `coverage.complete` is true when every conversation in scope was read and nothing was cut;
  `coverage.unread` names those that could not be (`missing`, `unreadable`, `too_large`), and
  `coverage.excluded` the kinds of work the graph does not hold (`proposals`: an unapproved proposal has an id
  but no record). A figure in `metrics` is `{value, known, partial, counted, of}`: a scope read whole with
  nothing in it is a known zero, a figure nothing established is unknown and never 0, and a sum over some of the
  work says `partial`.
* **State.** `state` is the engine's word (queued, running, done, failed, unverified, interrupted). Where the
  producing store has finer words, `state_detail` is its own: a plan row says `pending` (waiting on a hard
  dependency), `ready` (free to start), `claimed`, `running`, `done`, `failed` or `cancelled`, so queued-and-waiting
  is told from queued-and-free by a fact and not by a guess. Absent where `state` is already the store's word.
* **Route.** `route[]` is the crew the record accepted, per seat: `role`, `model`, `pinned`, and `price`
  (`{prompt, completion, cache_read}`, dollars per token) — the accepted route's own published tariff at
  admission. `price` absent is UNKNOWN (an older record, or a route nobody prices), never a price of nothing, and
  it is never read from Settings.
* **Memory.** The engine holds the graph in at most 128 MiB however many scopes are asked at once, and no gate
  has an exemption: retained — conversations 16, check indexes 4, snapshots 16 MiB; in flight — files being read
  28, snapshots being built 64 MiB. Each snapshot is priced by a conservative estimate of its fields (strings in
  full) and each build by an allocation weight measured against real builds. WHAT FITS: about ten thousand
  ordinary tasks (the size a board is tested at; a cold ask peaks near 30 MiB of live heap and every later page
  builds nothing and reads no file). WHAT DOES NOT FIT IS REFUSED, NOT SHRUNK: a scope whose files, facts or
  build would pass a gate is answered `too_large` BEFORE the memory is spent, reading stops at the conversation
  that crossed the line, and nothing is served partly. `error.detail` is `{what: "files"|"facts"|"build"|
  "working set", need, limit (bytes), reason: "needs_narrow_scope", scope?: "agent"|"task"}`: ask for the smaller
  scope it names. Ten thousand tasks that each carry the longest text a node may are refused too (their weight is
  over the build gate). A snapshot over its 16 MiB share but inside the build gate is built, served whole and
  not remembered, so each ask of it pays the build. No 100 000- or 200 000-task scope is supported; the
  `clusters` and `metrics` views of a scope that is refused are refused with it, because their counts need the
  whole scope and a count over part of it would be false. At most 8 distinct builds may be pending and 16
  requests may wait for room: past either, `busy`. A conversation yields at most 8 MiB of facts (`partial`) and
  its checkpoint is read to 7 MiB (`too_large`).
* **Not available.** `coverage.unavailable` is `["steps","asks"]`: a task's steps live in a running node's memory
  and in its journal (a per-event trace this source never parses), and the person's open asks live in memory
  naming blocked work by title and not by reference, so neither is a fact of the persisted graph. A surface says
  nothing about them. `elapsed_ms` (the age the record froze at the end of work) and `attempt` are present only
  where the record kept them.
* **Errors.** `bad_params` (scope, view, limit, cursor, focus), `not_found` (unknown agent, crew or task), `busy`
  and `closed` as elsewhere (`busy` also when the engine is already building as many scopes as it admits), `too_large` (additive; see Memory),
  `engine` (cancelled, or longer than 10 s).

A wait on a task of another conversation is recorded with `Agent.WaitOnTask` (there is no wire verb that writes
one). The task must be queued; the link is stored in its record (`waits_external`) and the engine's frontier
enforces it before the task starts: the prerequisite done lets it run, failed fails it, anything else —
unreadable included — keeps it waiting. The other conversation is never read while a lock of the waiting one is
held.

### Connected media admission refusals

Connected image/video admission refusals also appear in render history with `state:"failed"`, `committed:false`, `charge:"not_charged"`, optional structured `refusal` and bounded `options` (up to 8 model IDs). This is an actual tool invocation rejected before any provider request, not a generated source or a guessed user intent. Image refusal records may use `job:0`; no cancellable process was created. A missing model remains the empty string, never a substituted paid model. Every other image request is also a render (`kind:"image"`, `job:0`), written before the provider is asked; a request whose start cannot be written is refused with no provider request. It ends `finished` (committed; no `path` — the files are the call's media sources, found by `run`/`call`), `failed` (provider error, unusable reply, nothing saved; `committed` true only when the bytes were accepted for saving) or `cancelled` (`render.cancel`, or the turn ending under the call; `upstream:"unknown"`). `render.cancel` stops an image through its own context: `cancelled` before the bytes are accepted for saving, `too_late` after. A receipt after a stop is a `render.changed` on the same cancelled record. `model` is the model the request named; an absent provider figure is `charge:"unknown"`. The command-line `codeaf image` and a saved harness's run have no ledger and keep no record.
### Source invalidation

`media.changed` is emitted after a source record was successfully appended. It carries the actual source identity and known immutable run/call IDs; consumers still obtain descriptors from `media.list`/`media.get`. Empty or failed writes emit no invented source. Turn-stream and standing-lane duplicate delivery is filtered through a bounded 128-ID recent window; this is not a retained all-history source cache. A reopened view first reads its snapshot, fences replies with event sequence, and may coalesce newly reported IDs into one page reconciliation. No token, prose, tool-name heuristic or filesystem path from this event authorizes a media read.

### Producing runs on conversation entries

`Entry.run_id` and `turn.tool.run_id`, when present, name the immutable engine
execution that produced the assistant message or tool call. They join media
source `run` facts to actual work; they are independent of the app's numeric
`turn` and survive journal reopen and compaction. Missing means unknown, including
older journals. A background media notification does not acquire the run of a
foreground turn merely because it arrived during that turn. Reused provider call
IDs must be disambiguated with actual run identity; no nearest-message inference
is part of this contract.

Media sources may additionally carry `input_messages`, at most 64 distinct,
known user-message IDs found in the successful provider request that returned
the tool call. `input_messages_omitted: true` means further known IDs exceeded
that bound. This is an input set, not proof of one causal message: `message`
remains absent unless a producer separately knows that relationship. Unknown
legacy message identities are never synthesized. The snapshot belongs to that
request; later steering cannot change a background job's inputs. Early warm
reads that started before the successful response keep their existing unknown
input provenance. These fields do not grant access to paths or message bodies.

### Recorded image references

An engine implementing optional `MediaAttacher` advertises `media.attach` in hello.
`media.attach {agent, source, version}` returns `{attachment}`: a staged file attachment
for the exact recorded image version belonging to that conversation, with typed source
provenance. A caller supplies no path or image bytes. Unknown IDs, changed sources,
legacy sources without a recorded version, nonimages and out-of-scope files are refused.
The original is never modified. The engine copies into owned private storage with the
ordinary 10 MiB/image bound, at most two active copies, ten unsent/in-flight snapshots
per conversation and 64 process-wide. A full admission answers `busy` before new copy I/O.

Ordinary `agents.send` (or a crew message delivered to that same conversation) consumes
the returned attachment ID as an image automatically, using the engine-owned source
and snapshot digest. Its immutable provenance is retained on the person's message.
`attachments.remove` abandons a definitely-unsent snapshot without touching its original.
Accepted or uncertain deliveries retain their image file, including after shutdown;
a later explicit refusal never clears uncertainty from an earlier attempt.
An engine without this optional door answers `unknown_method`; a surface must not
substitute a path-based staging request for a recorded source grant.

### Captured video frames

An engine implementing optional `MediaFrameAttacher` advertises `media.attach_frame`.
`media.attach_frame {agent, source, version, at_ms, png}` returns `{attachment}` for one
frame of the exact recorded VIDEO version of that conversation. `png` is base64 of at most
3,000,000 bytes (checked on the encoded length before decoding); the engine validates the
signature, every chunk CRC, IHDR-first, an IDAT and a final IEND, at most 4096 a side and
8 Mpx, without decoding a pixel. A caller supplies bytes and never a path, so a direct client
can offer a picture but cannot make the engine read a file. The video must be recorded,
still the recorded version, in scope and a video; stale, changed, legacy, foreign or
non-video sources are refused before any file exists. The frame is written create-new into
owned private storage under the same admission as `media.attach` (two active, ten per
conversation, 64 process-wide, `busy` before I/O). The base64 is decoded only after that
admission, so a full engine allocates nothing for the frame. A moment past a known clip
duration is refused. The attachment's typed source names the clip, its canonical recorded
`version` and `atMs` (a picture source now carries `version`, persisted on the message); ordinary `agents.send` carries it as an image with the digest the engine
computed, and `attachments.remove` abandons a definitely-unsent frame. Accepted or uncertain
deliveries keep their file. The clip is never modified.

### Recorded retry support

`Task.retryable` and `work.graph` node `retryable` are explicit booleans from the
owning producer. Graph-node tasks support a fresh attempt; quick, handed-over
Run and Plan rows do not support this action. This is road support, not a promise
that admission will succeed: the current workspace, state and budget are still
validated when `tasks.retry` is requested. A client reading an older engine that
omits this additive field treats support as unknown and disables bulk retry.
### Recorded audio and video ranges

An engine implementing optional `MediaRangeAttacher` advertises `media.attach_range`.
`media.attach_range {agent, source, version, from_ms, to_ms}` returns `{attachment}`: an
ordinary `file` attachment that is an exact, byte-for-byte snapshot of the recorded AUDIO or
VIDEO at that version, named `<stem>.range-<from>-<to><ext>`. The range is provenance, never
a cut: nothing is clipped, rendered, decoded or sent to a model, and the snapshot is never a
picture. `from_ms` and `to_ms` are integers, `0 <= from_ms < to_ms`, within a year; the
source's RECORDED duration must be known and `to_ms` must not pass it (an unknown duration
is refused, never guessed). The source must be recorded, unchanged, in scope, a regular
non-symlink file of at most 32 MiB whose first bytes sniff as its kind; it is opened without
blocking and streamed in 32 KiB pieces, so a FIFO swapped in cannot hold the worker. Admission
and ownership are `media.attach`'s: two active, ten per conversation, 64 process-wide, `busy`
before I/O; `attachments.remove` abandons a definitely-unsent snapshot, an accepted or
uncertain delivery keeps its file. `attachment.media.source` carries `source_id`, `path`,
`version`, `from_ms`, `to_ms` (never `at_ms`).

The attachment rides `agents.send` in `attachments`, as any file does, and is refused if
named in `submission.media` (that list is for pictures). The engine records the source and
range on the person's own line (`attachments[].source`), beside the unchanged words and
`client_id`; a reopened conversation returns it as the entry attachment's `media.source`.
Only an engine-made snapshot can carry one: a source on an ordinary staged attachment is
dropped when it is staged. A remote host advertises `attachmentSources` in its welcome, and
a client refuses to send a source to a host that does not.



### Recorded media references

An engine implementing optional `MediaReferenceResolver` advertises `media.resolve`
in hello. `media.resolve {agent, path, expect_version?}` returns the same `{source}`
descriptor as `media.get`, with its actual source ID, versions and provenance.
The path must be an absolute, clean canonical spelling, at most 4096 bytes; it is
only an exact lookup key among that conversation's existing media records. No
file discovery, registration or path capability is granted. Callers must still
use the existing source-ID/version authorization to read the file. Evidence stays
evidence, and a viewed image is never relabeled as a generated output.

The newest matching owner's recorded row wins. `ResolveMedia` supplies its real
available, changed, missing, outside or unreadable state. `expect_version` may
match the descriptor's recorded version or live file version; a mismatch is
`conflict` with `detail.reason="changed"`. A matching old recorded version does
not make a changed file available. A foreign or unrecorded path is `not_found`
only after the whole index was searched; deadline, cancellation or exhausted
shared lookup capacity is `busy`, never proof of absence. A surface may offer an
explicit retry, without replaying a send or silently granting the supplied path.
### Deliverables (engine `40c2c2a55` and later)

A file the agent hands over with `share_deliverable` is a `Deliverable` with
`origin: "shared_by_agent"`: `authored` absent, `state` absent (unknown — never
Draft or Kept inferred from where it sits or that it is new), no `from`,
`profile`, `branch` or `rel`. `turn`, `run` and the new `call` are the sharing
dispatch's; `task` is the owning node when a worker shared. A file that is not
media and not Markdown/Mermaid text is `kind: "file"` with its sniffed `mime`
(a PDF is `file`, `application/pdf`, and is not a media source). A worker's
file is copied into the conversation's own artifacts folder: `path`/`open` are
the copy and `original` names the file it was read from (display only).
`deliverable.changed` `created` (and `media.changed` for a picture, sound or
video) follows each shared row once it is durably on the index and found again
by its exact id — and again on a retry or replay of the same `operation_id`,
from the receipt's own plan with the original ids, so a crash between the
append and the announcement is not silent. Shared announcements are at least
once: dedupe on `id`.

`deliverables.get`, `.keep` and `.snapshot` answer the bare `Deliverable`; params
are flat (`dir`, not a nested destination). `version`/`expect_version` pin the
observed `live_version` (the `file:` fingerprint), never the recorded `version`
(possibly `sha256:`). `open` is the checked location to open now and `open_from`
is `source` or `kept`; `path` stays the recorded original. A kept picture, sound
or video whose original is gone has no `media` and carries
`preview_unavailable: "source_gone"`: its media source names the original, and a
lease for the kept copy is a separate capability not yet offered. A snapshot's
`process`, when given, must have printed a loopback address and `url` must be on
the same origin. Conflicts are `conflict` with `detail.reason` `exists`,
`changed` or `already_kept`. A Keep whose copy was published but whose index
row could neither be confirmed nor its copy taken back answers `uncertain` with
a fixed message and no detail and no path: re-read the deliverable (and look in
the folder) before keeping it again, or a second copy may be made. A Keep that
answers `engine` left no copy of its own behind. A snapshot whose search for an
earlier registration of the same file could not finish within one overall
bound (about 10 s, waiting included) answers `busy` and recorded nothing.
A task worker's row carries `task`, `branch` and `rel` and no `state` until a
merged landing proves the exact authored bytes arrived (HEAD's blob at `rel` is
the checkout file's bytes, which hash to the recorded sha256, and the commit the
task was cut from held nothing there): then `state: kept`, `kept.how:
created_in_repo`. Without that proof it stays without a state. Until then, get,
read and Keep take the row's source from one rule, never from whatever sits at
the path it will have: while its task branch is unmerged and holds a regular
file at `rel`, it is `live: on_branch` with `live_version` `git:<blob>` and no
`open`, read serves that blob, and Keep answers `not_found`; otherwise the
checkout file is it only when its bytes hash to the recorded sha256 (checked
through one descriptor, same size first, bounded), else `changed` (or
`unreadable` when it could not be checked in time) with no `open`, and read and
Keep refuse. A kept copy is judged by its own recorded identity. Keep checks
the copied bytes against the expected sha256 before publishing. A list or count
page decides its rows within the page's own deadline: a branch is asked about
once per page and a folder of its files in one call; when the time runs out the
page ends BEFORE the first row it could not decide, with `incomplete` and a
`next` that resumes at that row (`busy` when not even one row could be
decided), so no row is skipped and none is called gone on an unanswered
question. A branch read
(`deliverables.read` of a row on an unmerged task branch) reads only a regular
file at exactly `rel` from git's object store; a link, folder or submodule there
is not a file, and `version` is the blob id read.
`branches.list` pages the conversation's own task branches - only names its
task records carry (tombstoned tasks and their descendants left out), never
another `task/*` ref - in record order, `limit` 50 by default and 100 at most,
within a 3 s page. Each row is unmerged against `head`, the HEAD commit pinned
for the page; `recorded` is how many names the records give. `next` resumes at
the first name not examined and is tied to that recorded list and that `head`:
when either has changed it is refused `bad_params` ("list again from the
start"), never re-aimed. A page whose time ran out ends there with `incomplete`
(a sparse page may hold no rows and still a `next`); one that examined nothing
is `busy`, and so is a third listing while two run in the engine process (no
queue). `repo: false` only when it is known there is no HEAD commit: no folder,
git's exact "not a git repository", no work tree, or an unborn branch; any other
git failure, a refused or unreadable folder, or a HEAD naming nothing is
`engine`, and a timeout `busy`. The two-listing cap bounds how many run, not
the engine's overall CPU. Counts are absent when git could not give them, never zero;
`counts_capped` counts are lower bounds. Unreadable task records are `engine`,
oversized ones `too_large`, never an empty list.

`checkout.status {agent, cursor?, limit?}` reads the agent's own workspace
checkout (the one files.diff and branches.list use; the caller never names a
path) as typed facts, read-only. `entries` are in git's order, 100 a page and
200 at most: `kind` changed · renamed · copied · untracked · conflicted, with
`index` and `worktree` each modified · added · deleted · typechange · renamed ·
copied (absent when that side is unchanged), `old_path` for a rename or copy,
`conflict` (both_modified, both_added, both_deleted, added_by_us, added_by_them,
deleted_by_us, deleted_by_them) and `submodule` (only its recorded commit is
compared; its own work tree is never entered). A path that is not valid UTF-8
has no `path` and says `path_unreadable`. `upstream`, `ahead` and `behind` are
absent when git has none, never zero; `operation` (merge · rebase ·
cherry_pick · revert · bisect) is read by stat from the files git itself leaves
while one is under way. `counts` {staged, unstaged, untracked, conflicted} are
present only when the whole status was read; a path both staged and edited
again is counted in both of the first two. `revision` names the exact status; `next`
is `<revision>:<offset>` and a cursor is refused `bad_params` ("read the
status again from the start") once the status differs, never re-aimed. Git
runs read-only (no optional locks, no fsmonitor, submodules by recorded commit
only) and runs no program the repository's configuration names: every
configured filter driver is set empty (and `required` set false, so a git-lfs style
driver does not fail the status) on the command line before git compares a
file, and the index is never written. Untracked files are listed one by one
(`--untracked-files=all`), never as their directory, so a large untracked tree
costs the 4 MiB budget below; a nested repository is still one entry. The 2-read
limit is engine-wide, not per agent: one agent's reads can make another's `busy`
for up to 3 s. One 3 s deadline covers every git of the
call (a timeout is `busy`, never `repo: false`); status text past 4 MiB kills
the process and answers `incomplete` with only the whole records read (a rename
whose original path was cut away is dropped, never sent without `old_path`), no `counts` and no
`next` past it; at most 2 are read at once and a third is `busy` at once.
`repo: false` only for no workspace or git's own "not a git repository"; any
other refusal is `engine`.

`checkout.status` entries carry optional line counts, `added` and `removed`
(absent = unknown, never zero) and `binary`. For a changed or renamed path they
are the NET change of the work tree (staged and unstaged together) against the
status's base, the HEAD commit it names or the empty tree when unborn; the index
is not the base, so edits staged and unstaged that cancel read 0 and 0. A rename
counts the edits since its original path (paired old/new only). Untracked files
have no counts and are never opened; conflicted, copied, submodule,
unreadable-path and filter-governed entries have none either. Counts are for the
page's entries only and come from fixed read-only `git diff-index --numstat`
runs (no external diff, textconv or clean filter; the index is not written).
When the reply cannot carry them, no entry does and `lines_unavailable` says why:
`incomplete` (status cut), `changed_during_read`, `timeout`, `too_large`,
`git_failed`. Every git read of one call, both status reads included, spends
from one 6 MiB budget, so a status too big to be read twice is `too_large`.
"Same checkout" is NOT a content hash: counts are attached only if the status
text, the index file's stamp and each counted path's stamp (type and mode bits,
size, nanosecond modification time, or absent) are identical before and after
the diff. A write that preserves all of those between the two reads is not
detected.

`checkout.status` derives `land` from the engine's actual protocol producer:
`{available: true}` when it implements both `branches.land.plan` and
`branches.land`, otherwise `{available: false, reason: "no_producer"}`. A
checkout reader cannot manufacture this capability in its own reply. This
fact does not prepare or confirm a plan, grant a renderer permission to land,
or say that a particular branch is eligible. The native host must still show
and confirm the exact plan described below; planning or confirmation may
refuse changed, dirty or unsupported checkouts.

Review of a branch is what `branches.list` gives (name, head, base, counts);
a per-file diff of a branch against its base is not offered, and `files.diff`
compares the working tree only.
`checkout.diff {agent, path, revision?}` and `checkout.locate {agent, path, revision?}`
are Review and Open/Reveal of ONE row of the checkout status. `path` is exactly a
current `checkout.status` entry's `path`: repository-root-relative, "/"-spelled,
whatever folder the workspace is (workspace `repo/sub`, row `root.txt` is
`repo/root.txt`, never `repo/sub/root.txt`). A client never joins it to anything:
the engine reads ONE status, finds the row byte for byte, and only then touches
the file. An empty, absolute, `.`/`..`/empty-segment, NUL, backslash or
over-4096-byte path is `bad_params`; a path that is not a changed row is
`not_found`; a status cut at its byte bound that does not contain the path is
`too_large` (it never pages). `revision` is optional on the wire and a client
that displays a row MUST send the revision it displayed: a checkout whose status
has since moved is `conflict` with `detail.reason: stale_revision` and nothing is
diffed; omitted means an explicit fresh lookup, and the reply's `revision` says
which one was used. THE REVISION NAMES THE STATUS TEXT (rows, kinds, HEAD and
index object ids), NOT the content of a work-tree file that was already modified:
such a file edited again keeps its revision, so the diff can show newer content
than the list's counts did. No immutable-content promise is made. A path that
names more than one row (`git rm --cached f` leaves a staged deletion and an
untracked `f`) is `conflict` with `detail.reason: ambiguous_row` for both
methods, with no diff and no `open`; such a row cannot be reviewed until an
additive row selector exists. A status cut at its bound confirms no path
(`too_large`), because a second row could lie past the cut. `checkout.diff` answers the existing `FileDiff` for the row against the
status's base (its HEAD commit, or the empty tree when unborn) from read-only
`git diff-index` run at the repository root (no external diff, textconv,
fsmonitor or clean filter; the index is never written): changed, renamed (the
row's own `old_path`), deleted, untracked (read through an `os.Root` anchored at
the toplevel, every parent a real directory, the leaf opened no-follow and
non-blocking and checked to be the same regular file, bounded by `FileReadMax`;
anything else is `unavailable` / `unreadable`; the read draws on the call's 6 MiB pool and a file that does not fit what is left is `too_large` with detail "the byte budget of this call is spent"). A row whose net content equals the
base is `unchanged`, never an empty diff. `copied`, `conflicted` and submodule
rows are `unavailable` with `reason: unsupported_row` (a stated M1 limitation).
A path a configured filter governs is `unavailable` / `filter`. After the diff the
status is read again and the index's and the path's stamps compared; a difference
is `unavailable` / `changed_during_read` (not a content hash). Everything shares the
one 3 s clock, the 6 MiB byte pool and the two admission slots of `checkout.status`.
`workspace_path` is the row relative to the workspace folder, absent outside it.
`open` is an absolute path inside `repo` to an existing regular file, not a link,
reached through real directories, valid UTF-8; absent otherwise and on platforms
that cannot prove it. THE REPLY IS FACTS, NOT A GRANT: a native host must not take
`open` back from a renderer as authority. It receives agent, row path and the
displayed revision, calls `checkout.locate` itself, validates the answer against
the trusted repository, and only then acts. `files.diff` is unchanged and takes a
workspace-relative path; it is not for status rows.

An index row whose `deliver.id` is not a shape the engine issues (`dlv_` and 32
hex, or `dll_` and 24 hex) is not a deliverable, and no update under such an id
is applied.

### Landing a task branch

`branches.land.plan` and `branches.land` merge one of the conversation's own
task branches into its checkout, at a person's explicit word. `branch` is a key
into the conversation's task records (any other name is `not_found`); no path,
repository, target or ref is taken from the caller. The repository is the one
the conversation's folder resolves to, and the target is the branch that
checkout is on, whichever it is.

- **Plan.** Reads and binds, and writes nothing to the checkout: the branch's
  commit and merge base, the target and its HEAD, the resolved repository
  (top level, git directory and common directory), the whole git configuration,
  every hook a merge can run (`pre-merge-commit`, `prepare-commit-msg`,
  `commit-msg`, `post-merge`, `reference-transaction`, `post-index-change`,
  where git looks for them, `core.hooksPath` honoured) by name, size, executable
  bit and SHA-256, any other worktree the branch is checked out in, and the tree
  `git merge-tree --write-tree` says the merge makes. `revision` is the digest of
  all of it. A plan is held 120 s, 8 per agent (the oldest goes) and 64 in all
  (`busy` past that), and is used once.
- **Refusals** are `conflict` with `detail: {reason, paths?}`, and a refused
  plan has no id. Reasons: `dirty` (anything `git status` reports, untracked
  included), `ignored_in_way` (an untracked or ignored file or directory where
  the branch adds a file), `conflict` (merge-tree's paths), `merged`, `gone`,
  `detached` (no branch, or no commit yet), `operation` (a merge, rebase,
  cherry-pick, revert, bisect or index lock in progress), `held` (a program's
  run is working there), `task_copy_dirty`, `custom_merge` (any configured merge
  driver), `filter` (any configured clean, smudge or process filter), `sparse`,
  `submodule`, `signing` (`commit.gpgSign` on a merge commit), `ff_only`,
  `merge_options` (the target sets `branch.<name>.mergeOptions`),
  `hook_unbounded` (a hook that is not a regular file of at most 1 MiB),
  `unrelated`, `outside`, `git_old` (git older than 2.38), `too_large`.
- **Land** is the native host's only, sent after its own confirmation dialog
  has shown the plan — the target, the branch, the counts and every hook that
  will run. The plan is taken out first; an unknown, spent, expired or other
  agent's plan is `conflict` `expired`, and a revision that differs is
  `changed`. Under the repository's lock (`busy` when it cannot be had within
  15 s, never unserialized) every fact is read again and any difference is
  `changed`. Then `git merge --no-edit --no-autostash --no-overwrite-ignore
  -s ort --ff|--no-ff <branch_head>` runs on the planned commit, never the
  branch name, with a 60 s ceiling of its own. Every git a land runs has
  `core.fsmonitor`, `maintenance.auto` and `gc.auto` off, so no automatic gc
  (and no `pre-auto-gc` hook) starts after the merge; each read is one process
  group killed whole at its 3 s deadline. A land answers only when it is
  proved: the target still checked out, HEAD the planned commit (fast-forward)
  or a merge of exactly `head` and `branch_head`, its tree the predicted one, no
  merge in progress. A merge git refused, when no hook was there to run and
  afterwards HEAD, the target, the index and `git status` are all as planned,
  is `conflict` `refused`; its message says what was read and that ignored
  files were not, and never that nothing changed (git has written `ORIG_HEAD`
  by then). With a hook present, or anything else, it is `uncertain` with
  `detail: {head?, target?, merge_in_progress?, index_locked?, clean?,
  conflicted?}`; nothing is aborted,
  reset, stashed or retried, and a merge left in progress is the person's to
  finish or undo with `git merge --abort`.
- **What is not covered.** A write by an editor in the instant between the last
  check and the merge is not serialized; git refuses to overwrite a changed
  tracked file, and the answer is then `uncertain` rather than a refusal.

## Slash catalogue

`slash.catalog` (no params) lists the slash commands the terminal chat accepts so a client draws its command
menu from the engine's words. It **lists words and runs nothing**: the terminal's `app.slash` is a Bubble Tea
dispatcher, not an engine call, and nothing on the wire interprets a command line — `agents.send` with `/cost`
sends a message. Where a typed method already does what a command stands for, the client calls it; where none
does, the action is the client's own. `slash.catalog` is read-only and takes no model call, file walk,
transcript read or watcher.

- **One table.** `commands` with `source: "table"` are `internal/slashcat`'s rows — the same rows the terminal
  dispatches on — folded by name: one entry per command, `forms` in table order (the bare form has no `args`),
  `aliases` in the order written. At this contract the table is 48 commands, 82 rows, 28 aliases.
- **Programs.** `source: "program"` entries are the running build's programs (`/review`, `/sec`,
  `/senior-dev` in a full build), reported by the engine after the table, by name. One form, `args`
  `<brief>` unless the program says otherwise. `brief_optional`: a bare `/<name>` runs the program's default
  brief. `lands` is `tree` or `text`. A program whose word is already a command, an alias, or not a word is left
  out and named in `skipped`.
- **`description` is the terminal's wording**, key names included (`ctrl+.`), except that a page the terminal
  opens with an alt+digit chord has the chord left off. Icons, groups and fates are the client's.
- **`host`** is whether the command works against a connection to another machine. That is the terminal client's
  state and this engine cannot see it, so it is always `"unknown"`; it is a field so a later engine can say more.
- **`door: "task"`** marks `/task`: it also acts inside a sentence.
- **Bounds.** At most 128 commands, 16 forms and 8 aliases per command, 64-byte names and 512-byte text;
  `limits` repeats them. A program row over a bound is skipped, never cut.

A client that sees no `slash.catalog` feature in `hello` has no catalogue to read and must not guess one.

## Session actions

`session.act` is the typed door for three things the terminal does with `/task <brief>`, `/<program> <brief>`
and `/remember <text>`. It is **not** a command interpreter: the actions are named for what they do
(`task.start`, `program.start`, `memory.remember`), and `agents.send` with a slash line is still a message to
the model. Everything else in the slash catalogue (rewind, export, delete, land, cache, skills, …) has no verb
here and says so by having none.

- **The producers are the terminal's own** (`StartTask`/`StartTaskEffort`, `StartDelegate`, `Remember`/
  `RememberAlways`), called through the open conversation's engine handle, so over `--host` they run on the far
  machine against its build and its program catalogue. Their admission — spending caps, a blocked provider, a task
  that cannot hand work on, a full memory — is the whole authority; there is no force or skip field and an
  unknown field is `bad_params`.
- **A producer error is `uncertain`, never a refusal.** No producer proves by type that its error came before any
  effect (a task id is reserved, a run registered, a line written before some of their errors), so an error is a
  successful answer with `state: "uncertain"`, `producer_error.message` (the producer's words, secrets removed,
  ≤1 KiB) and a `note`; it is never run again. The ONLY refusals are the ones the engine can prove before it claims
  or calls anything — malformed or unknown fields, a door the conversation lacks, a program not in its catalogue, a
  program that needs a brief, memory off, `busy`, `closed`, a claim that could not be written (`engine`) — and they
  are RPC errors that leave no record.
- **`program.start` takes a program NAME** from `session.actions`' `programs` (the engine's own rows); never a
  path or command. A program without `brief_optional` needs a brief. Over a conversation reached through the engine
  wire — every conversation the app holds — a failed catalogue read and an empty one look the same, so an empty
  catalogue there is UNKNOWN: `session.actions` says "the engine's programs could not be read, or it reported none"
  and `session.act` answers `engine` with nothing claimed, never "carries no programs" or "no program named".
- **Only what the engine can be shown to do is offered.** A handle to an engine across the wire has every method
  whatever the far engine can do, so its type proves nothing. No far fact states that the far agent can choose a
  task's crew, so over such a handle `efforts` is absent and a `task.effort` is `bad_params` before anything is
  claimed. `everywhere` is offered wherever memory is: the far engine answers a project line and an everywhere line
  through one memory door.
- **`memory.remember`** keeps authored text as given (`scope` `project`, or `everywhere` where offered). No model
  rewrites it; the answer carries the producer's receipt sentence and no id (the producer has none). The answer's
  `memory.scope` is OPTIONAL and present only where the method fixes where the line holds: `"everywhere"` for an
  everywhere line. A project line holds for the project, or for the person everywhere when the conversation has
  no project of its own, and the producer says which only in its receipt sentence, so its `scope` is absent
  (unknown) on the answer, a replay and a read — never `"project"`, and never read out of that sentence.
- **One `client_id` is one attempt, ever.** It is claimed durably (digest of the request, never its words)
  before the producer runs. Before every claim the claims folder's entry in `<home>/app` and that folder's entry
  in `<home>` are synced, and a failed sync is `engine` with nothing claimed or run; `<home>`'s own entry is not
  synced here. So a claim survives a process crash, and a power loss once `<home>` itself is on disk. On Windows
  a folder cannot be synced this way and the step makes nothing durable, so there it claims a process crash only. A repeat is answered from the record (`replayed: true`) and runs nothing; the same
  `client_id` with other words, another action or for another conversation is `conflict`; a
  claimed attempt whose outcome is not known is `state: "uncertain"` and is never run again by the engine. A
  record that cannot be read is `uncertain` (code), never "new"; so is a `client_id` another process claimed whose
  record is gone a moment later. To try again after an uncertain attempt, read `tasks.*` or the memory, then use a
  new `client_id`. Another process's attempt still running reads `uncertain`, not `in_progress`: only the process
  running it knows it is running.
- **A receipt whose record could not be confirmed** carries the producer's real answer and a `note` that a later read
  MAY say `uncertain`: the rewrite may or may not have reached the disk before it failed.
- **`done` means admitted**, not finished: the work's own events (`task.changed`) and `tasks.*` say the rest. The
  answer that ran the producer carries `task.title`/`note` and the memory receipt; a replay carries the ids
  (`task.id` is `"<agent>:<n>"`) and the memory scope only — for a project line an empty `memory: {}`.
- **`session.action.get`** reads the record. `not_found` means only that this engine's home has none: it does not
  prove nothing was sent from elsewhere.
- **Bounds.** `client_id` ≤128 bytes, brief ≤32 KiB, memory text ≤8 KiB; one action per conversation and eight per
  process at a time; sixteen reads (`session.actions`, which may open the conversation and read a far catalogue, and
  `session.action.get`) share one gate held until the read has answered. A full gate is `busy` at once; no
  goroutine, queue or poller is added. A record is read only from a descriptor proven to be the plain file that was
  looked at, opened without waiting, so a pipe swapped in cannot hold a read. A filesystem call cannot be cancelled:
  the slot it holds is held until the disk answers.

## Teams deliverable scope

For both deliverable reads, `scope: "teams"` selects conversations held by the
actual open team hierarchy and excludes loose conversations. `crew`, when
present, selects a canonical top-level team and its descendants. Each result's
optional `teams` records its team, top-level team, and whether it belongs to the
real root's own roster. A rootless hierarchy has no invented root. A cursor is
bound to the membership revision and filter; a changed membership requires a
new first page. Inconsistent ancestry, an open child under a closed parent, or
an oversized union is an error, never a truncated complete result. File reads
are bounded to the shared two-MiB team snapshot; unions admit at most 4096
conversation owners and 16384 membership pairs under the operation's shared
deadline. This scope does not add a union Changes feed.

Stamped process mutations refuse an engine without the corresponding stamped door; `processes.stop` never falls back to an unverified `Cancel`. A caller deadline travels on the private engine action wire and an expired rerun is refused before launch. Cancellation after dispatch remains an uncertain RPC outcome and never causes an automatic retry.


## Factory: attached remote connections

Feature `factory.remote` is present only when the engine serves the typed factory interface. Other engines answer these methods with `unknown_method`. Runners are not exposed as a list-and-manage surface by this protocol slice; the local factory, its setup and the one door that turns this engine's own runner on are a separate optional feature, `factory.local` (below). Job writes are a separate optional feature, `factory.job_actions` (below); signing out and revoking sessions are another, `factory.admin` (below); `FactoryCapabilities.writes` stays `false` until an app binds them end to end.

| method | params | result |
|---|---|---|
| `factory.capabilities` | none | FactoryCapabilities |
| `factory.list` | none | FactoryList (state: ok, unreachable, signed_out, github_expired; reason?) |
| `factory.attach` | url, token? | FactoryConnection |
| `factory.detach` | factory | empty |
| `factory.me` | factory | FactoryMe (public identity, github_signed_in, can_sign_out, github_sign_in_available, invite_available) |
| `factory.queue` | factory | api.Queue (a repository that is off takes its jobs out, in every state, and is not in `repos`; a listed repository's `watched` is true unless the factory keeps no watched list, and there is no `on`) |
| `factory.job` | factory, job | api.Job (a job of a repository that is off is still answered, with `repo_off: true` and no `can.run`, `can.start_now`, `can.run_again`, `can.approve`, `can.retry_publish`, `can.merge`, `controls.rerun_step` or `controls.sync`) |
| `factory.repos` | factory | FactoryRepos (no checkout path) |
| `factory.pipeline` | factory, repo (owner/name) | FactoryPipeline |
| `factory.runners` | factory | api.Runners |
| `factory.people` | factory | api.People (empty in solo) |
| `factory.activity` | factory, since?, limit? (default 200, max 1000) | FactoryActivity |
| `factory.setup` | factory | api.FactorySetup (server-side first-run progress only) |
| `factory.access_check` | link, caller | FactoryAccess |
| `factory.access_sign_in` | link, caller | FactoryAccess |
| `factory.access_cancel` | link, caller | FactoryAccess |
| `factory.sign_in_start` | factory, caller | FactorySignIn |
| `factory.sign_in_poll` | flow, caller | FactorySignInState (pending, signed_in, expired, denied) |
| `factory.sign_in_cancel` | flow, caller | empty |

`factory.me` never returns session identifiers, machines, runners or checkout paths. Its `github_signed_in` comes only from the serving factory's explicit attestation of a GitHub-verified caller profile; a credential ID, handle or operator flag cannot imply it. An older factory omitting the attestation yields no GitHub claim without changing the connection's actual authentication state. `can_sign_out` separately indicates a caller session that the session endpoint can end: a verified legacy access grant can identify a person without supporting that action. Desktops offer Sign out only with both explicit flags. `github_sign_in_available` and `invite_available` are the serving factory's own statement of its configuration (a GitHub client id is set; and, for the second, a tokens file exists to keep invitations in); they are never inferred from a role, an id or a mode, and an older factory omits them, which reads as `false`.

**Step times.** Each of `job.steps[]` says when its latest round began, `started_at` (RFC 3339), beside `at`, when it ended: a running step has `started_at` once its round has begun, and no `at`, a finished one both, a pending one (including one sent back to run again) neither. A round that carries on in the chat a pause cut short keeps the start it had; a round in a fresh chat starts afresh. Each finished round in `steps[].rounds` (`{round, summary?, at}`) also carries its own `started_at`, so a surface can place every round of a step from the job alone. A job, step or round recorded before these were kept simply has no `started_at`. A round's conversation id (`rounds[].chat`, like `steps[].chat`) is a path on the machine that ran it and is withheld from every reader of the job; the app opens a round's chat through `factory.job_chats`, whose `steps[].rounds` names it as an agent.

**Job size.** `job.size` is the size LABEL alone (`S`, `M`, `L`, `XS`, `XL`) and is the only size a start reads. `job.read_size` `{value: "S"|"M"|"L", from: "read"|"diff"}` is additive and display only: it is present only when `job.size` is absent and the engine has a size to show, the completed read's guess (`read`) or a pull request's measured lines (`diff`). Never both on one job; neither for a chat guess, an item with no recorded source, or a read the text has since outdated. A client that ignores `read_size` stays correct. `triage.size_from` (`read`, `diff`, `chat`, absent = unknown) carries the provenance between machines.

Runner and activity reads optionally carry `people`, keyed by the normalized GitHub handles referenced in their returned rows. Names and photos come from recorded GitHub sign-ins; unknown handles have no invented profile. The activity limit also limits this profile map. Join previews optionally carry `factory.operator_name` and `factory.operator_avatar_url` after the access link matches.

Types come from `internal/factory/api` where the server has one; `appwire/factory.go` defines the rest. The schema carries enum values from both appwire `enum:` tags and the reused factory API `jsonschema:` tags.

### Factory job actions (`factory.job_actions`)

Feature `factory.job_actions` is present only when the engine implements `FactoryActor` on top of the factory interface; otherwise every method below answers `unknown_method`. Each method is one POST to one `/v1/jobs` route of the attached factory and answers `FactoryJobActed`. There is no method that takes a path, verb or body, none that acts on more than one job, and unknown params are `bad_params` (a field this wire does not carry is refused, not ignored).

| method | params (all but create also `factory`, `job`, `expect`) | factory route |
|---|---|---|
| `factory.job_start` | | `POST /v1/jobs/{id}/start_now` (assignee only; queues, never asks a model here) |
| `factory.job_stop` | | `…/stop` |
| `factory.job_dismiss` | | `…/dismiss` (personal) |
| `factory.job_drop` | discard? (bool, the person's yes to deleting work) | `…/drop` (assignee only; stops a running job, then throws away everything of its work on the factory's machine: its LOCAL worktree folder and local branch (a pull request's job keeps its branch), and its agent's and every step's conversations, every round's, with its factory-made crew; nothing on GitHub is touched. A job whose branch has commits beyond its start, or whose folder has uncommitted or untracked changes, or whose branch could not be read, is refused `has_work` unless `discard` is true; its conversations are never asked about. The answer waits for the deletes, which wait for a step running there to stop (up to about 8 s) and for each conversation's turn to stop (up to 30 s each). No event names a deleted conversation: the app learns of the drop from its next `factory.queue` or job read (the job's `version` moves), after which `factory.job_chats` refuses the job, and of a deleted crew as `crew.removed`. A delete that fails still drops the job, which no longer names what was kept, and is said on its activity. No words) |
| `factory.job_restore` | | `…/restore` (Bring back: assignee only; only a dropped job, `can.restore`; back to Backlog as a fresh job, as if it had never run: no steps run, `cost` 0, no failure, change, head commit, runner or `pr`; its notes and its steps' edits are cleared (steps back to the repository's recipe), and a later `factory.job_chats` answers a new agent; its issue, assignee, priority, budget, model and activity stay; nothing starts. Before the job is reset, while it is still dropped, a folder or branch re-created since the drop is deleted again, and so are the conversations and crew a job dropped before drops deleted them still names (a crew is then `crew.removed`). A pull request it had opened is forgotten, never closed; the `restored` receipt's detail names it) |
| `factory.job_run_again` | | `…/run_again` (Run again: assignee only; only a job carrying a failure, a person's stop that cut a step among them (`failure.code` `stopped`), or whose pull request was closed unmerged; the failure is cleared and a new run starts at the first step, in the worktree and branch the job's runner kept) |
| `factory.job_unassign` | | `…/unassign` (assignee only) |
| `factory.job_assign_to_me` | | `…/assign_to_me` (only an unassigned job) |
| `factory.job_change_assignee` | assignee (login, `@` optional) | `…/change_assignee` (assignee only) |
| `factory.job_set_priority` | priority: urgent, high, medium, low | `…/priority` |
| `factory.job_set_budget` | budget: 5, 10, 20 or 30 (US dollars, raise only) | `…/raise_budget` (assignee only) |
| `factory.job_approve` | commit? (the head the person saw), words?, edited? | `…/approve` (assignee only) |
| `factory.job_undo` | | `…/undo` (your own approval or stop, within the Undo window) |
| `factory.job_request_changes` | words | `…/request_changes` |
| `factory.job_retry_publish` | | `…/retry_publish` (assignee only; only after a failed publish) |
| `factory.job_step_stop` | step | `…/steps/{step}/stop` (assignee only) |
| `factory.job_step_rerun` | step, words? (what to do differently; kept on the job's notes) | `…/steps/{step}/rerun` (assignee only) |
| `factory.job_merge` | commit? (the pull request's head the person saw) | `…/merge` (assignee only; the factory's merge gate decides) |
| `factory.job_set_model` | model (a model id; empty for the factory's default) | `…/model` (assignee only; the next step opens on it and a running step switches at its next turn) |
| `factory.job_create` | factory, repo (owner/name), title, text?, assignee?, start? (`backlog`\|`now`) | `POST /v1/jobs` |

**A stuck step.** A job waiting with `needs_you.kind` `out_of_rounds` is a step that stopped short (it never reported, ran out of time, or its until did not hold); `needs_you.question.text` says so in the factory's words and `state_presentation.title` is `Stuck`. Its recovery is `factory.job_step_rerun`: on the stuck step to try again, or on `needs_you.back_to`, the finished step before it, to send the work back (left out when there is none, as for the first step); `words` are kept on the job's notes, which every step's brief carries. Stopping is `factory.job_stop`. The question's own answers are not carried.

**Result.** `FactoryJobActed {factory, action, job, approval?, undo_until?, event?, current?}`. `current` is the job as the factory shows it after the action (one extra GET, never a retry); when it is absent the action still happened. The next write on the job carries `current.version`.

**Guards.** `expect.version` is required on every job write (`bad_params` without it); `expect.state` and `expect.step` are passed through. The factory refuses a mismatch, and the engine does not resend. `job_approve` carries `commit`, which the factory compares with the job's head: another head is `stale` (`detail.reason` `changed_since_opened`, `detail.head_commit`). Only the job's assignee (anyone while it has none) approves, publishes, starts, runs again, drops, brings back a dropped job, raises the budget and stops or reruns a step; the factory answers anyone else `forbidden`.

**Undo.** `job_approve` does not act at once. The factory holds the decision for 5 s (`approval.state` `approving`, `undo_until`) and acts when the window ends. `job_undo` by the same person inside the window withdraws it (or a stop, restoring the run); after the window, or by another person, it is `conflict`. Approval is per job: there is no bulk approve.

**Merge.** `job_merge` merges the pull request the job opened, and only that one, through ONE gate in code (`factory.MergeGate`) that runs on the factory when the merge is asked for and again on the runner, on a fresh read, right before the forge is asked. It merges only the exact commit whose proof was approved, never a draft, a conflict, a failing or still-running check, or a branch still waiting on required reviews; a fact the factory has not read (the head, mergeability, checks, reviews) refuses. A refusal is `conflict` with the gate's own words (`detail.reason` `merge_refused`). A merge the gate takes waits 5 s with Undo (`job.merge.run.state` `asked`, `undo_until`; `job_undo` withdraws it), then is sent (`queued`, then `merging`); the forge is handed the exact commit, so a head that moves after both checks is refused there too. The job is done when its pull request reads `merged`; a merge that failed is `needs_you` with reason `merge_failed` and `job.merge.run.why`, and is never retried on its own. **Auto-merge** is the pipeline's alone: `.codeaf/factory.yaml` `merge: {auto: on|off, method: merge|squash|rebase}` (off unless the file turns it on; a personal file may only turn it off) is pinned on the job when its proof is approved (`job.merge.auto`, `job.merge.method`), and the factory sends a merge on its own only for a pinned `auto` that is on and only when the gate passes. There is no merge tool on any agent's belt.

**Job merge fields.** `job.merge` is absent until the job has a pull request of its own: `{auto, method, run?, blocked?}`. `run` is the merge on its way or the last that failed: `{state: asked|queued|merging|failed, by, auto, rule, why, commit, method, at, undo_until}`. `blocked` is why Merge would be refused now, in the gate's words ("2 checks are still running"), and `can.merge` is the gate passing for the caller. `job.pr` gains `head_sha`, `mergeable` (`yes`, `no`, absent while the forge has not worked it out), `checks_read` and `reviews_read`. Running reasons gain `merging`; Needs-you reasons gain `merge_failed`.

**New job.** `job_create` adds a backlog job: nothing starts, no model is asked, no GitHub issue is opened (the wire has no issue flag). `start: "now"` is the only way to also start it and requires `assignee` to be present (`""` for no one; a team factory refuses that with `bad_params`).

**Errors.** `forbidden`, `stale` and `has_work` are new codes; all three, and `conflict`, carry `detail` = `FactoryRefusal {reason, by, job_person, at, now_state, taker, machine, approved_by, event, commit, current_commit, head_commit, request_id}`. `stale` means read the job again and let the person decide again; nothing retries. `has_work` answers only `job_drop` (`detail.reason` `has_work`): the message is the factory's sentence naming the work, such as `codeaf#1844's branch has 3 commits and uncommitted changes.`, and the same drop with `discard: true` is the person's yes; nothing sends it on their behalf. `conflict` also covers a refusal like "already approved by @x" or "already taken", and a job whose repository is off (`detail.reason` `repo_off`, message `acme/api is off · turn it on in Repos to run its jobs.`). `uncertain` answers a write that may have reached the factory without an answer (dropped connection, timeout, any 5xx): read the job before doing anything. `unreachable` is only a connection that was never made. `signed_out`, `github_expired`, `not_found`, `busy` and `bad_params` map as for reads; a factory that lacks a verb answers `engine` ("this factory does not provide that action").

**Not carried:** sync (it opens a GitHub issue), hand-off cards (per-job step edits are the separate `factory.job_step_edit` feature below), the pipeline pull request, answering a question other than a plan (the one answer this wire carries is a plan sent back, the separate `factory.job_reject_plan` feature below), and any arbitrary job verb. `factory.job_request_changes` is the PROOF send-back of a landed job and never acts on a plan.

### Sending a plan back (`factory.job_reject_plan`)

Feature `factory.job_reject_plan` is present only when the engine implements `FactoryPlanRejecter` on top of the factory interface; it is independent of `factory.job_actions`. Otherwise the method answers `unknown_method`. It is a native "Request changes" on a PLAN: the person's words go back to the finished step before the plan's approve step, which runs again in a new round and plans again with those words. It is **not** `factory.job_request_changes`, which is the proof send-back of a landed job, and it is **never** carried to the factory's `/answer` route.

| method | params | factory route |
|---|---|---|
| `factory.job_reject_plan` | `factory`, `job`, `words` (required, trimmed, at most 4000 characters), `expect` (`version` required, 1 or more; `state` required and exactly `needs_you`) | one `POST …/jobs/{id}/reject_plan` with exactly `{"words":…,"expect":…}`, then one `GET …/jobs/{id}` for `current` |

It answers `FactoryJobActed` with `action` `reject_plan`. There is never an `approval`, an `undo_until` or a commit: a plan sent back is an answer, not a decision to publish. It leaves one `answered` receipt (not an approval) and the answered question on the job, and has no Undo window; `factory.job_undo` finds nothing to withdraw.

**Why a route of its own.** The factory's answer route takes a no for any question a job can wait on: a no to a spending limit or to an exhausted round budget cancels the job, a no to a start the factory asked about declines it, and a no to a plan with no finished step before it STOPS the job. A "Request changes" on a plan must reach none of those, so it has its own typed route, and `/answer` is unchanged.

**The factory decides on the locked job document, not on timing.** The route's own read of the job only refuses early. The decision is made again inside the factory's lease-desk lock and the job's file lock, on the document used for the item write: the job's document must still be exactly `expect.version` (a revision compare-and-swap in the store), must be `needs_you` with the plan's approve question asked (`needs_you.kind` `approve_plan`), its run must have a finished, non-approve step before the approve step (the shape older workers stored, the finished plan step waiting with the approve step pending after it, counts), and the caller must be allowed to answer it. A worker or another person who writes the job after the person's read moves its version, and the revision is refused. A validation refusal adds no job, version, note, receipt or worker-signal change of its own; normal lease expiry can still update the lease file.

The item, lease signal and answer receipt are written separately, as for `/answer`, rather than one crash transaction. An unheld job rewinds during the item write; a held job receives a signal and rewinds when its worker handles it. A crash or I/O failure after the item write can leave an applied decision without a receipt. Such an outcome is uncertain: read the job again, never resend automatically.

| refusal | code | `detail` |
|---|---|---|
| the job moved since the person read it (including while the factory was deciding) | `stale` | `reason` `stale`, `now_state` |
| not a plan waiting (a spending limit, an exhausted round budget, a start, a free question, a proof, a job not waiting) | `conflict` | "Not a plan waiting for you.", `reason` `conflict`, `now_state` |
| a plan with no finished step before it | `conflict` | "This plan has no finished step before it to send back to." |
| a run the factory cannot read a target in (no run, an unrecognised shape) | `conflict` | "This plan's run is not in a shape that can be sent back." |
| not the job's person | `forbidden` | `reason` `not_job_person`, `job_person` |

**Who may.** The answer route's rule, asked of the locked document: in a team factory only the job's person (a caller the factory cannot name is not that person, so it is `forbidden` too), a plan with no person yet by any caller (the answer route's rule, unchanged), and a solo factory by anyone. The consent lock the answer route holds applies.

**What the read says.** `can.reject_plan` (on every job read, mutation reply and queue row) is true only when this caller may answer the plan, its run has that finished step to go back to, and this factory serves the route. `needs_you.back_to` names that step on an `approve_plan` wait when it can be named; it is left out when there is no target, and also when an old document's step carries no name (the revision is still possible: say "the previous step"). A client offers the control from `can.reject_plan` alone and never re-derives the target from `steps`. A factory from before the route sends no `can.reject_plan`, which reads as false.

**What the run does.** If no worker holds the job, the factory itself rewinds the run to that step (a new round for it), keeps the words on the job's notes after any already there for that round's brief, and queues the job; the response's `current.state` is `queued`. If a worker holds the job, the factory takes the question down, sets the approve step back to pending, leaves the job `running` and signals the worker with the no and the words, exactly as an answer would; the worker's loop does the rewind and keeps the words when it hears the no, and until then `current` shows the job running at the approve step.

**An older factory.** A factory without the route answers the `POST` as an endpoint it does not have (`not_found` or `engine`), and nothing is written. The engine never falls back to `/answer`.

**Not carried:** `commit`, `choice`, `reason`, `question_id`, a yes, and answering a spending limit, an exhausted round budget, a start question or a free question.

### Per-job step edits (`factory.job_step_edit`)

Feature `factory.job_step_edit` is present only when the engine implements `FactoryJobStepEditor` on top of the factory interface; it is independent of `factory.job_actions`. Otherwise both methods answer `unknown_method`. They carry the factory's per-JOB route `GET` and `POST /v1/jobs/{id}/step-edits` and change the steps of ONE job that is not finished. **They never read or write the repository's `.codeaf/factory.yaml`**; that file and the recipe future jobs start from belong to `factory.pipeline_*`.

**The feature is a carrier, not a permission, and no native app edits steps yet.** It says only that this engine can send the two requests. A client may offer an edit only when the job's `controls.edit_steps` is true AND a `factory.job_step_edits_read` of that job succeeded with `version` of 1 or more. Even then the factory can refuse the write: `controls.edit_steps` does not know whether the factory has a worker desk. Who is editing is decided solely by the authenticated route; no param names an author.

| method | params | factory route |
|---|---|---|
| `factory.job_step_edits_read` | `factory`, `job` | `GET …/step-edits` |
| `factory.job_step_edit` | `factory`, `job`, `expect` (`version` required, 1 or more), `edit` | `POST …/step-edits` |

`edit` is `{add, ask, thinking, on, skip, why}` and nothing else (unknown fields, nested ones too, are `bad_params`):
- `add`: up to nine of `{step: {name?, kind?, ask?, until?, effort?}, after?}`. `kind` is `chat` or `gate`; `effort` is `cheap` or `strong`. A chat step needs a name or an ask; a gate needs neither. Any other step field (`max`, `when`, `fanout`, `proof`, `by`, a path) is `bad_params`; a step is placed only by `after`.
- `ask` and `thinking`: maps of step name to the new ask (at most 240 characters) or to `cheap`, `strong` or `""`.
- `on` and `skip`: step names to switch on or off.
- `why`: one line, recorded with the edit (at most 4000 characters on this wire).
There is no reorder, no removal, no change of a step's kind or max, and an edit that names nothing is `bad_params`. The engine checks the shape and the factory's published bounds (new chat step names are one word of 2 to 12 letters; a gate is named by the factory; nine steps) and sends nothing else; every other rule is the factory's, answered whole: only the job's person edits, a step that already ran is not changed, a landed, finished or publishing job is `conflict`, a fixed step is refused. Proof is the step named `proof`, a `prove*` step, or any step that runs until proven; checks and posts are not proof. A landed job refuses every nonempty edit with "the job has landed, so the steps can no longer be edited" and does not offer `controls.edit_steps`. Before landing, once any proof has started, every nonempty step edit is refused without changing the job. Before proof starts, new work cannot be added, switched on or reworded after the earliest proof; a person's approval gate may still be added there before proof starts.

**Result.** The read answers `{factory, job, version, steps, lines}`, with `lines` empty. `version` is the next write's `expect.version`; `0` means the factory gave none (it keeps no versions) and no write can be made. The write answers the same facts after the edit plus `current`, the job as the factory shows it after ONE reread; `current` is absent when the reread failed, was another job (`document_id` differs), or reads a `version` older than the receipt's; that says nothing about the edit. Empty `lines` mean the factory changed nothing, and only a whole receipt can say so: a 2xx reply that is not JSON, or lacks `steps`, `lines` or `version` (each must be present; `steps` and `lines` may be an array or `null`), or whose `version` is below 1 or below the `expect.version` sent, is `uncertain`, not an empty edit. An equal `version` with empty `lines` is a real no-op. No reread is made when the receipt cannot be read.

**One send.** The write is sent once and never retried or replayed. `stale` (HTTP 409 with a stale reason) means the person reads again and decides again; `forbidden` (403, not the job's person) and `conflict` (other 409) carry `detail` as `factory.job_*` do. A refused edit (an unknown step, a started step, a too-long ask, a tenth step) is `conflict` with the factory's sentence and `detail.reason` of `conflict`; `bad_params` means the request body itself was refused. A 5xx, a timeout or a dropped connection after sending is `uncertain`: read the steps again before any new edit, because the factory has no request id and a second send could apply twice. A factory without the route answers `not_found` ("factory endpoint not found"); one with the route but no edit seam or worker desk answers `engine`.

**Requeue.** An accepted edit with lines can put a running job back in the queue and cut off the worker that held it. The app should say so before it sends.

**Gaps.** The factory records the edit but returns no record event id, so there is no undo; a factory without a record store keeps no event of the edit at all.

## Factory: signing out and revoking sessions (`factory.admin`)

Feature `factory.admin` is present only when the engine implements `FactoryAdmin` on top of the factory interface; `FactoryCapabilities.admin` is then `true` and is `false` for every other engine. `writes` is unchanged. Both methods take exactly `{factory}`; a session id, a path or a body is `bad_params`, and no session id is ever answered. Each makes ONE write to ONE fixed route and is never retried.

| method | params | result |
|---|---|---|
| `factory.sign_out` | | `FactoryAdminDone {factory, action: sign_out, signed_out: true}`. The engine asks `GET /v1/me` with the held credential, then `DELETE /v1/me/sessions/{own id}` with the same credential, and drops the held session only after the factory said yes. The row stays, so the person can sign in again. `conflict` for the machine's own factory (the app made its key; nothing is detached or stopped) and for a connection that holds a key or access link rather than a sign-in; `signed_out` when nothing is held (no request is sent) |
| `factory.revoke_sessions` | | `FactoryAdminDone {factory, action: revoke_sessions, signed_out}`. `POST /v1/access/revoke-sessions` with the credential held for that factory, only when `GET /v1/me` says it is the operator's (`forbidden` otherwise, no request sent). `signed_out` is true when the caller's own sign-in ended with the rest and is dropped; false when the connection holds a key, which revoking sessions does not end and which is kept. `conflict` for the machine's own factory |

An outcome the engine cannot confirm (a dropped connection after the request left, a timeout, a 5xx) is `uncertain`: the person may or may not still be signed in, the held credential is kept, and nothing is claimed. A refused connection is `unreachable` (nothing was sent). `busy` answers a second call for the same connection while one is in flight. Nothing here reads the terminal's attach record, the host key or the loopback owner, so restarting or resuming the machine's factory never undoes a sign-out.

## Factory: invitations (`factory.links`)

Feature `factory.links` is distinct from `factory.admin` and is present only when the engine implements `FactoryLinkAdmin` on top of the factory interface; `FactoryCapabilities.links` is then `true`. `FactoryCapabilities.copy_link` is `true` only while the engine has a working clipboard for THIS machine, which in production is **never until a trusted local launcher installs one** (`appBackend.useClipboard(clipboard.NewSystem(true))` in `cmd/codeaf/appfactory_links.go`; nothing in the tree calls it, and neither the OS, `DISPLAY` nor an installed helper enables it). With `copy_link` false `factory.link_create` makes nothing.

Every method needs the **operator** of a **team** factory, resolved from `GET /v1/me` with the credential held now (`forbidden` / `conflict` before any request otherwise; the machine's own factory is `conflict`; nothing held is `signed_out`). A GitHub-verified profile is not required (an operator key may manage invitations). Params are strict: a url, token, path or body is `bad_params`. A second call for the same connection while one is in flight is `busy`.

| method | params | result |
|---|---|---|
| `factory.links` | `{factory}` | `FactoryLinkList {factory, items: FactoryLink[], earlier_unconfirmed}`: only people's invitations (the operator's claim, script keys and rows with an unknown status are dropped), open first then newest, at most 500. `FactoryLink {id, name?, login?, created_at, expires_at?, created_by, max_uses, used, remaining, status: open\|used_up\|expired\|revoked, ended_at?, used_by[], counted?}`; `counted` is true/false only when the factory said so (false: revoking also ends the members' sessions) and absent otherwise. A successful list clears the refusal below |
| `factory.link_create` | `{factory, name?, login?, max_uses?, expires_at?}` | `FactoryLinkMade {factory, entry, outcome: copied\|not_copied\|unconfirmed, ref?, holds_until?, earlier_unconfirmed}`. Omitted choices are the factory's own (one person, seven days); `max_uses` is 1 to 50, `login` needs one person, `name` is at most 80 characters of plain text, `expires_at` is in the future and within seven days. Needs `me.github_sign_in_available` and `me.invite_available` (else `conflict`) and an available clipboard (else `conflict`, nothing made) |
| `factory.link_copy` | `{factory, ref}` | `FactoryLinkMade`. Copies the HELD invitation again: no write to the factory, no new invitation. `not_found` when the hold has run out, was cleared, belongs to another caller, or the invitation has since ended |
| `factory.link_revoke` | `{factory, id}` | `FactoryLinkRevoked {factory, id, revoked}`. `id` is `[A-Za-z0-9_-]{1,64}`. The engine re-reads the list first and sends `DELETE /v1/access/links/{id}` only for a person's invitation in it (`not_found` otherwise, no DELETE) |

**The invitation's address and secret never cross this wire**: no field, error or event carries them. The factory shows them once, in the answer to the create; the engine writes them to the clipboard through the helper's standard input and registers them with the redactors.

**Outcomes.** Only `copied` may be shown as copied. After a confirmed create whose copy failed (`not_copied`) or did not answer (`unconfirmed`) the invitation exists: it is never deleted and never made again. The engine holds that same invitation in memory behind `ref` (one per connection, at most 32 in all, at most 10 minutes and never past the invitation's expiry) and `factory.link_copy` retries it. The hold is cleared on a successful copy, a revoke of that invitation, sign-out, revoke-all, a different caller, detach, close and expiry; it is never written to disk.

**Uncertainty.** A create is sent once. A dropped connection, a timeout, a redirect, a 5xx, a non-201 success, or a 201 whose body cannot be read as an invitation is `uncertain`: nothing is copied, deleted or retried, the words claim neither that it was made nor that it was not, and the next `factory.link_create` on that connection is `conflict` until `factory.links` has been read successfully. After that read the create is allowed, and `earlier_unconfirmed` is `true` on the list and on that create's result until the unconfirmed one could no longer be live (so a desktop can warn that an extra invitation may exist). An HTTP refusal is definite only when it carries `X-Codeaf-Phase: pre-mutation` and is a 4xx (or the documented unsupported 501): the handler has not begun its requested mutation. The four link/session admin routes, and `PUT /v1/repos/{owner}/{repo}` (see `factory.repos_manage`), clear that marker before their first change. An unmarked refusal can follow a persisted mutation whose event recording failed, including 403/404/408/409, so it is `uncertain`; older factories or proxies that omit the marker are treated the same way. Refusal text is fixed; the factory's prose is never passed on. Link/session deletion and session revocation complete only on 200 or 204; an unexpected 2xx, including 202 Accepted, is uncertain. An uncertain session write keeps the credential until the outcome is checked. A proven pre-mutation 404 is `not_found`. No write is automatically repeated.

An explicitly requested expiry must match exactly; the default invitation lifetime cannot exceed seven days from the factory's reported creation time. A malformed or broader scope is uncertain and is neither copied nor held.

**Caller changes.** Metadata and copy retries are bound to the current held credential. The engine checks it after network waits and before copying, and stores a retry hold atomically for that credential. If a create completed after the caller changed, it returns uncertainty without scoped metadata, a copy or a hold, and does not delete or remint the invitation. A clipboard write already completed when the caller changes cannot be taken back; the late reply reveals nothing to the new caller.

**Trusted local launch.** The native shell grants create/copy only to its directly bundled local sidecar; overrides and unproven paths are refused before RPC. It supplies the non-secret parent-bound `CODEAF_APP_LOCAL_GUI=v1:<parent-pid>` arrangement marker. The engine consumes only its canonical spelling through `env.ConsumeExact`, removes both spellings before tools run, and enables the real clipboard provider only on a matching native launch. This marker is coordination, not a same-user security boundary; the independent shell provenance guard is required. The provider sends invitations only through helper stdin and discards helper output. Unsupported/unproven launches stay unavailable.

**No ambient Team authority.** A known Team connection without its held credential is signed out before private reads or writes. A stale local registry marker without a live local credential is unreachable and authorizes no listener that reused its port. Only a fresh explicit solo-mode probe can permit legacy unauthenticated solo reads.

## Factory: turning a repository on or off (`factory.repos_manage`)

Feature `factory.repos_manage` is distinct from `factory.admin` and `factory.links` and is present only when the engine implements `FactoryRepoAdmin` on top of the factory interface; `FactoryCapabilities.repos_manage` is then `true`. The list itself is still `factory.repos`, which is how a caller learns the watched list after any outcome that was not a plain yes. The engine never reports whether THIS caller may use the switch in the capability; it asks the factory (`GET /v1/me`) on every call.

| method | params | result |
|---|---|---|
| `factory.repo_set` | `{factory, repo, on, stop?}` | `FactoryRepoSetDone {factory, repo, on}`: the same connection, repository and switch that were asked for, answered only after the factory answered 200 or 204. `repo` is `owner/name` (owner `[A-Za-z0-9_.-]` up to 39 characters, name `[A-Za-z0-9_.-]` up to 100, neither part `.` or `..`); `on` is required and has no default. `stop` (bool, only with `on: false`) is the person's yes to stopping the repository's jobs that are under way first; with `on: true` it is `bad_params`. Params are strict: a path, verb, body, folder or any other field is `bad_params`. |

**Who may.** The OPERATOR, with the credential held now, resolved from `GET /v1/me` on every call (`forbidden` before any write otherwise; a member is `forbidden`). Unlike the sign-out and invitation methods this includes the machine's own factory (the local row and a solo factory), but only while the app HOLDS a live credential for it: the key the app made for the local factory, or a key the person attached. With none held, a team or attached factory is `signed_out` and the local row is `unreachable` ("the local factory is not running for this app"), before any request leaves. Nothing asks the factory with no bearer, nothing reads the host's key or the terminal's attach record, and a loopback team service therefore cannot hand its host's authority to this caller. A second call for the same connection while one is in flight is `busy`.

**One write.** The engine sends exactly one `PUT /v1/repos/{owner}/{repo}` with `{"on": true|false}`, and `"stop": true` when the app sent `stop` (owner and name escaped; the path and verb are never taken from the app). It never retries, replays or tries another route, and it does not edit repository configuration or write to GitHub.

**Off stops the factory working on the repository.** Its jobs leave `factory.queue` in every state (`factory.job` still answers each, with `repo_off: true`); intake, triage and the pull-request follow pass them by; and the factory refuses run, start_now, run_again, a step's rerun, resume, approve, retry_publish, merge, request_changes, recheck and sync on them, and a new job on the repository, with 409 `reason` `repo_off` and `acme/api is off · turn it on in Repos to run its jobs.` (`conflict` on the wire). While any of its jobs is running, queued or waiting on a person (a job's Stop would end it), turning it off is refused with `has_running` and the factory's sentence naming them, such as `acme/api has 1 job running (api#12) and 1 waiting on you (api#7).`; nothing changed. With `stop` each of those jobs is first stopped as its own Stop stops it (work kept, the stop recorded, Undo offered), then the repository is turned off; a stop that fails leaves the repository on and is `has_running` naming that job (`api#12 could not be stopped, so acme/api is still on: <why>.`), and the jobs stopped before it stay stopped. Turning it back on brings every job back as it was: nothing is deleted or re-made, and the poll resumes from its cursor. Checkouts and folders are never touched.

**Outcomes.** 200 or 204 is completed (an unexpected 2xx such as 202 is `uncertain`). A refusal is definite only when it carries `X-Codeaf-Phase: pre-mutation` and is a 4xx or the documented 501; this route clears the marker immediately before the factory saves the list, so a failure of the save or of the event recorded after it has no marker and is `uncertain`. Definite refusals use fixed words: 403 `forbidden`, 404 `not_found` ("that repository is not one this factory can see"), 401 `signed_out` (or `github_expired`), 422 or 400 `bad_params`, 429 `busy`, 501 `conflict`. The one refusal that carries the factory's own sentence is 409 `reason` `has_running`, answered `has_running`, and it is definite with or without the marker: the factory sends it only before it saves the list, so the repository did not change (a stop made first changed a job, which `factory.queue` shows). A connection that never opened is `unreachable` (nothing was sent). A dropped connection, a timeout, a redirect, a 5xx or any unmarked refusal is `uncertain`: the repository may or may not have changed, so read `factory.repos` before trying again. Nothing is repeated automatically.

**Caller changes.** After each wait the engine checks that the connection still holds the credential it used. If it was replaced while a request was in flight, the answer is `conflict` (before the write, or after a refusal that proves nothing changed) or `uncertain` (the write may have completed), and in neither case does the reply carry the repository or the switch.

## Factory: opening a pull request for the pipeline file (`factory.pipeline_pr`)

Feature `factory.pipeline_pr` is distinct from every other factory feature and is present only when the engine implements `FactoryPipelineOpener` on top of the factory interface; `FactoryCapabilities.pipeline_pr` is then `true`. It is a TRANSPORT SEAM over the factory's existing caller-scoped route `POST /v1/repos/{owner}/{repo}/pipeline/open-pr`. It does not edit a pipeline, build or diff YAML, read the repository's file or change what a pipeline may say: the factory parses and re-encodes `text` itself, so the bytes it commits are the factory's, and comments and key order in the old file are not kept.

| method | params | result |
|---|---|---|
| `factory.pipeline_pr` | `{factory, repo, text, title?, body?, branch?, expected_base_sha?}` | `FactoryPipelinePRDone {factory, repo, outcome, pr?, base_sha}` with `outcome` `opened` (`pr {number, url, branch, base, base_sha, head_sha}`) or `unchanged` (no `pr`: the default branch already holds these bytes and nothing was written). Failures are errors, below. |

**Params.** Exactly the route's own fields plus `factory` and `repo`; `factory`, `repo` and `text` are required. Anything else (a path, verb, token, reviewers, an edit) is `bad_params` and is never believed to have been honoured, as is a value that is not one JSON value. `repo` is `owner/name` as for `factory.repo_set`. `text` is at most 262144 bytes of UTF-8 and not blank; `title` is one line of at most 200 characters; `body` at most 16384 bytes; `branch` is a valid NEW Git branch name of at most 200 bytes (empty lets the factory suggest one; it is never renamed); `expected_base_sha` is a 40-character commit, folded to lower case, and when it is not the default branch's tip nothing is written. The encoded request may not pass 1 MiB. Every bound is the route's own and a test fails when one drifts; a request that breaks one is refused before anything is sent. `text`, `title`, `body` and `branch` are forwarded exactly.

**Who may, and whose GitHub access.** Anyone who holds a credential for the connection now, resolved from `GET /v1/me` on every call; it is not an operator action. The pull request is opened as that person with the GitHub access the factory holds for their sign-in. The engine never supplies a GitHub token, never reads the host's `gh` or git identity, and never asks the factory with no bearer: with none held, a team or attached factory is `signed_out` and the local row is `unreachable`, before any request leaves. The factory decides what a solo host may do with its own profile token; this method adds nothing to it. A second call for the same connection and repository while one is in flight is `busy`.

**One write.** One `POST`, never retried, replayed, split, or tried by a second route or verb when the first said no. Redirects are not followed. The route's flow is not a transaction: it can stop after creating a branch or committing the file, and it never deletes a branch. The wait is up to 75 seconds (the route's own is 60), longer than other factory calls, so a slow success is not turned into an uncertain answer.

**Outcomes.** A reply is believed only in its documented shapes.

| reply | wire |
|---|---|
| 201 `opened` with a positive number, the canonical `https://github.com/<owner>/<repo>/pull/<n>` URL of THIS repository, a valid branch (the requested one, if any), a 40-hex `base_sha` and `head_sha` (and the requested `expected_base_sha`, if any) | result `opened` |
| 200 `unchanged` with no pull request and a 40-hex `base_sha` | result `unchanged` |
| the flow's own typed failure: `outcome` `refused`, `not_opened` or `unknown`, a known `stage` (`request`, `credential`, `repository`, `permission`, `base`, `file`, `branch`, `commit`, `pull_request`), a `request_id`, and a status and `branch_left` that agree with it | error with `detail` `FactoryPipelinePRFailure {factory, repo, outcome, stage, github_status?, branch?, branch_left, request_id}` |
| anything else | `uncertain`, no `detail` |

`refused` says nothing was written (`branch_left` is false and the stage is neither `commit` nor `pull_request`). `not_opened` says no pull request was opened; if `branch_left` is true, `branch` remains on GitHub. `unknown` (always `branch_left`, always a branch, at a write stage) says the engine cannot tell, so the person looks at the repository on GitHub before anything else, and the words never call it a failure. The error's code is coarse: `refused` is `github_expired` (stage `credential`), `forbidden` (`permission`), `conflict` (`base`, `branch`), `bad_params` (`request`), else from GitHub's status `github_expired`, `forbidden`, `not_found` or `engine`; `not_opened` is `engine`; `unknown` is `uncertain`. The `outcome` in `detail` is what a screen reads. The words are fixed in the engine; the factory's own prose is never passed on, because it can echo a branch, a path or a token.

**Uncertainty.** A dropped connection, a timeout or cancellation, a redirect, any 2xx other than the two above (including 202), a 4xx or 5xx that is not the flow's typed failure (a proxy, the shared authentication gate, the JSON gate: the route does not send `X-Codeaf-Phase`, so no bare status proves nothing was written), a body that is unreadable, too large, contradicts itself, names a stage or outcome the wire does not know, or confirms a different repository, branch or base than was asked: all `uncertain`, with no repository, branch, number or `detail`. The pull request may or may not exist and a branch may remain. A connection that never opened is `unreachable` (nothing was sent). The next write is the person's call after looking at GitHub; nothing is repeated automatically.

**Caller changes.** After the wait the engine checks that the connection still holds the credential it used. If it was replaced or signed out meanwhile, EVERY answer, a success and a refusal alike, is `uncertain` with no repository, branch, number or detail, because it describes a caller who is no longer this connection's.

## Factory: reading and previewing an edit of the pipeline file (`factory.pipeline_edit`)

Feature `factory.pipeline_edit` is distinct from every other factory feature and is present only when the engine implements `FactoryPipelineEditor` on top of the factory interface; `FactoryCapabilities.pipeline_edit` is then `true`. It announces two methods that come BEFORE `factory.pipeline_pr` and WRITE NOTHING: no pull request, no branch, no file, no checkout, to GitHub, the factory or disk.

| method | params | result |
|---|---|---|
| `factory.pipeline_source` | `{factory, repo}` | `FactoryPipelineSource {factory, repo, path, default_branch, base_sha, can_push, archived, runtime_fields, state, editable, text?, problems?, default_budget?, budget_in_file?, pipelines?, steps?, fixed?}` |
| `factory.pipeline_preview` | `{factory, repo, base_sha, base_text, edits[]}` with `edits[]` of `{op, name?, pipeline?, after?, prompt?, until?, max?, when?, effort?, fanout?, value?}` | `FactoryPipelinePreview {factory, repo, base_sha, path, text, changed, format_rewritten, comments_dropped, steps[], budget?, structure?}` |

**The source is the repository's own file.** `text` is the raw `.codeaf/factory.yaml` as the repository's DEFAULT BRANCH holds it at one pinned commit, `base_sha` (40 hex), read with the CALLER's GitHub credential through the factory's `GET /v1/repos/{owner}/{repo}/pipeline/edit-source`. It is never the host's checkout or working tree, never the merged read `factory.pipeline` gives (personal file, defaults, markdown recipe) and never `personal_text`. A team never falls back to a host token; only a solo host may use its own profile token, as for the pull request route. The route takes no parameter: `ref`, `path`, a URL or anything else is `bad_params` here and 422 there, and it makes three GETs (repository, default-branch tip, the file at that commit) and no write. `can_push` is GitHub's answer for the caller; a pull request needs it, reading does not. `archived` is GitHub's flag for the repository and is always present (`false` is written, never omitted); the engine refuses a factory answer without it rather than assume "not archived". An archived repository can have `can_push` true and the pull request route still refuses it, so the pull request is possible only when `can_push && !archived`; the two are never merged into one field. A base the runner would silently drop a step of (a step name over 12 letters once hyphens are removed, or `lint-pass` beside `lintpass`) is `invalid`, not editable. The engine accepts up to 4 MiB of factory answer (a 256 KiB file of `<` is 1.5 MiB escaped, twice).

**`runtime_fields`** is always present. It is `true` only when the FACTORY said so in this very read (its parser reads a step's `kind`, `when`, `effort` and `fanout` and its runner obeys them). A factory that omits it, or says `false`, gives `false`; a value that is neither is an unreadable reply (`engine`). A file that already spells any of those four words from a factory that did not say `true` is refused at the read, not shown. `steps[]` carries `kind`, `when`, `effort` and `fanout` (each omitted when empty) as the file wrote them. Whether the app may offer the three word edits is `hello` feature `factory.pipeline_edit_runtime` AND `source.runtime_fields` AND `source.editable`, all three; any one missing means offer `prompt`, `until` and `max` only. `kind` is read-only: it appears so a screen can show a step, and there is no edit for it.

**State.** `state` is `file` (it exists and reads without a problem: `editable` is `true` and the parse is present), `missing` (the default branch has no such file; NOTHING is invented in its place, and the shipped defaults are not offered as though they were the file), `invalid` (it exists and does not read cleanly: `text` is carried, `problems` say why in the engine's own words, `editable` is `false`) or `too_large` (more than 262144 bytes, which a pull request cannot carry: no `text`). Only `file` is editable; for the other three `steps`, `pipelines`, `fixed` and `default_budget` are absent. A blank file is `invalid`.

**The parse** is the file as the parser reads it. `pipelines[]` is `{kind, steps[], in_file}` in order, the kinds `issue`, `pr`, `ci` first; `in_file` is `false` for a kind the file does not list, which runs the shipped steps. `steps[]` is each step once, in order of first use, as `{name, gate?, prompt?, until?, max?, kind?, when?, effort?, fanout?, locked, in_file}`: `gate` is a person (`approve`) with no prompt, `locked` is a gate or any name under `fixed` and is refused by the preview, `in_file` is `false` for a step whose wording is a shipped default the file does not write. `default_budget` is dollars and `budget_in_file` is `false` when the file does not state one. The engine parses `text` itself with the pull request route's own parser; a factory answer that disagrees (called editable what the parser refuses, or parsed into other steps, another budget, a different lock) is not believed and is an `engine` error. The factory's own prose and problem lines are never passed on.

**The edits.** `set_step` `{name, prompt?, until?, max?, when?, effort?, fanout?}` (at least one of the six) and `set_budget` `{value}`, which takes none of the six; and, only from an engine that announces `factory.pipeline_edit_structure`, `add_step`, `remove_step` and `move_step` (below). `pipeline` and `after` belong to those three and are `bad_params` on the other two. There is no `kind` field (a step becomes a check, gate or post only by editing the file itself: `"kind"` is an unknown field and `bad_params`), model, per-step budget, only_if, loop, on/off switch, reviewers or any other field: each is `bad_params`. The words, matched after lower-casing and folding whitespace: `when` is `always`, `thin`, `large`, `touches auth` or `has ui`; `effort` is `cheap` or `strong`; `fanout` is `one`, `per-file`, `per-finding` or `per-claim`; any other word is `bad_value`. `""` clears that word back to the default and counts as a runtime edit. Naming the default word (`when: always`, `fanout: one`) is a change from `""` and the file then spells the key; a screen should present `""` and the default word as one setting. These three are edited only on a chat step: a gate, a fixed step or a non-chat step is `locked`. The shape of the edit is an exact DTO and an unknown field is refused. `name` is a step name exactly as the source gave it and must sit in one of the file's pipelines. **Attestation.** The three words may be previewed only for a file this engine has seen the factory attest: a read of the source in the same sign-in (same caller credential, factory origin, repository and commit) that said `runtime_fields: true`, whose bytes are exactly `base_text`; the claim is held in memory for thirty minutes and a restart forgets it. A preview that has an edit naming `when`, `effort` or `fanout` (clearing included), or a `base_text` that already spells `kind`, `when`, `effort` or `fanout`, without that claim is refused BEFORE anything is read or sent, as `bad_params` with `detail {code:"bad_op", op}` where `op` is the index of the first such edit and is absent when the base is what spells them; the words tell the person to read the file again. A result that would spell those keys without the claim is refused the same way. A preview that names a runtime field still needs that claim when it clears the last runtime key. The pull request route needs no runtime seal for resulting text that contains none of those keys; that does not waive the preview’s read claim. Bounds are the preview's own and are checked before anything else: 1 to 64 edits; a step edited once and the budget set once; `prompt` non-blank, at most 8192 bytes, valid UTF-8, no control character other than newline and tab; `until` one line of at most 200 characters (`""` asks to clear it); `max` a whole number of rounds from 1 to 99; `value` dollars above 0 and at most 10000. Texts are trimmed as the file reads them, otherwise forwarded exactly. The preview refuses all or applies all.

**The preview.** The params are the file exactly as the source gave it (`base_text`, at most 262144 bytes, `base_sha`) and the edits. The engine asks nothing but who holds the connection (`GET /v1/me`, as for the pull request), parses `base_text` with the real pipeline parser, applies the edits, encodes with the real encoder, then reads the result back and requires the same bytes and the same values: if the file format cannot hold an edit as asked (clearing a built-in step's `until`, which the reader puts back), it is refused as `not_representable` rather than previewed as something else. Every field the edits did not name, including `start`, `size`, `fixed`, `rules`, the order of every pipeline and every other step, passes through the same round trip the pull request route performs.

**What `text` and the flags say.**

| `changed` | `text` | `steps[]`, `budget` | `format_rewritten`, `comments_dropped` |
|---|---|---|---|
| `true` | the exact canonical file the pull request route will commit, byte for byte (its parse then encode is itself) | the steps whose `prompt`, `until`, `max`, `when`, `effort` or `fanout` really differ (`{name, fields[], before{prompt, until, max, when?, effort?, fanout?}, after{...}}`: `when`, `effort` and `fanout` are omitted when empty, so a legacy file's preview is byte for byte what it always was; `fields` never holds `kind`), and the budget `{before, after}` if it differs | `format_rewritten`: the committed bytes differ from the base beyond the edit (the encoder's layout, keys the file left out written in); `comments_dropped`: the base holds a YAML comment and the committed file will not |
| `false` | `base_text`, byte for byte: there is nothing to write | `[]` and absent | both `false` |

`changed` compares the edit with the base AS THE PARSER READS IT, so reformatting alone, an edit that sets a value to what it already is, or a base that is simply laid out differently from the encoder never makes it `true`: a screen must not offer to open a pull request when it is `false`. The text is never assembled by the caller.

**Refusals.** An edit the file cannot take is `bad_params` whose `detail` is `FactoryPipelineEditRefusal {factory, repo, code, op?}`; `code` is `base_invalid` (the base is blank, does not parse or has a problem), `bad_op` (a verb or field combination that is not one of the two), `locked`, `unknown_step`, `bad_value` (out of bounds), `too_large` (the result would pass 262144 bytes) or `not_representable`; `op` is the position of the edit that caused it and is absent when the base or the whole request did. The words are fixed in the engine and never echo the file or the edit.

**Source errors.** With no held credential a team or attached factory is `signed_out` and the local row is `unreachable`, before any request leaves. The factory's typed failure (a known `stage` of `credential`, `request`, `repository`, `base` or `file`, and a `request_id`) maps by stage and status: `github_expired` (stage `credential`, or 401), `bad_params` (`request`), `not_found` (404), `forbidden` (403), else `engine`; anything else goes through the fixed-words mapper the other doors use. A connection that never opened, or that dropped, is `unreachable`: it is a read, so nothing needs checking before trying again.

**Caller changes.** After the wait the engine checks that the connection still holds the credential it used. If it was replaced or signed out meanwhile, the answer is `conflict` or `signed_out` with no repository, commit, text or step in it, a success and a refusal alike. The same check follows a preview.

**Opening the pull request** is `factory.pipeline_pr` with the preview's `text` and the source's `base_sha` as `expected_base_sha`; the route re-reads the default branch and writes nothing if it has moved. Nothing here opens it. Text that spells `kind`, `when`, `effort` or `fanout` is sealed again at that door: it needs a held attestation for `expected_base_sha`, then the file is read once more as the same caller and must still say `runtime_fields: true`, serve the same commit and the same bytes, or nothing is sent (`bad_params`, the same words to read the file again). Text with none of the four words skips this and takes the path it always took.

**Capability.** `FactoryCapabilities.pipeline_edit_runtime` and the hello feature `factory.pipeline_edit_runtime` say this engine decodes the three words and refuses them without the attestation above. The feature is announced beside `factory.pipeline_edit` only by an engine that declares it guards, never by having the two methods alone; it says nothing about any remote, which is `runtime_fields`, read per file. The desktop does not yet expose these edits: this page is the contract it builds to.

### Adding, removing and moving a step (`factory.pipeline_edit_structure`)

Hello feature `factory.pipeline_edit_structure` and `FactoryCapabilities.pipeline_edit_structure` say this engine previews three more verbs. They are announced beside `factory.pipeline_edit` only by an engine that declares it (`FactoryPipelineStructureEditor`); an editor that does not refuses each verb as `bad_params` before it is asked, exactly as before they existed. They say nothing about any remote and need no attestation of their own: they write only a pipeline's list and, for a new step, one definition under `steps`, keys every YAML-reading factory already parses. The desktop does not yet expose them.

| verb | fields | does |
|---|---|---|
| `add_step` | `{pipeline, name, after, prompt, until?, max?, when?, effort?, fanout?}` | puts `name` into `pipeline`'s list just after `after` (`""` = first); a new name gets a chat definition from these fields |
| `remove_step` | `{pipeline, name}` | takes `name` out of that one list; its definition under `steps` is KEPT |
| `move_step` | `{pipeline, name, after}` | moves `name` within that one list to just after `after` (`""` = first) |

`pipeline` is a kind the file has (`issue`, `pr`, `ci`, or another it lists); a kind the file does not have is `unknown_step`, and a step never moves between kinds. `after` is required on `add_step` and `move_step` (a JSON `null` or an omitted `after` is `bad_params`) and names a step already in that list at that point in the edits, else `unknown_step`. There is no `kind` and no `value` on any of the three: a step added here is a chat. `add_step` requires a `prompt`, bounded as on `set_step`; `when`, `effort` and `fanout` on it are runtime edits under the same attestation as `set_step`'s, refused at that edit's index without it.

**Names.** `name` is lowercase letters, digits and inner hyphens, 2 to 32, exactly as the file spells a step (`Lint` is `bad_params`). An added name must also be one the runner runs: 2 to 12 letters or digits once hyphens are removed, not a person's name (`approve`, `approve2`, or `approve-2`, which reads as one), and not the same stage as a step already in that list (`lintpass` beside `lint-pass`); each is `bad_value` (an `approve` word is `locked`). A name already in the list is `bad_value`. A pipeline holds at most 9 steps; a tenth is `too_large`, never truncated. Removing a pipeline's last step is `bad_value`: an empty kind runs the issue steps.

**Definitions are never rewritten here.** A new name writes a new definition and is listed in `structure.added`. A name the file already defines (another kind's step, or a shipped step such as `security`) is only listed, and only when every field the edit names already says the same; its other fields are the definition's own. Any difference is `bad_value`, so another kind's step cannot be changed through this door. `remove_step` keeps the definition: another kind may list it, and a shipped step whose definition went would come back with its shipped words.

**Locked rows stay put.** `approve`, any step whose kind is `check`, `gate` or `post`, and any name under `fixed` are `locked` to all three verbs, so those rows keep their order among themselves. A step the runner reads as proof is `locked` to `remove_step` and `move_step` whether or not `fixed` lists it: named `proof`, named `prove…`, or with `until` `proven` (or `every claim has evidence`), the same test the runner applies to the compiled stage. A preview with a structural edit of a pipeline whose base runs proof may not leave that list with no proof step: a `set_step` that changes `until` on a row that was proof only by `until` is then `locked` at that `set_step`. A `set_step` alone, with no structural edit, is a field edit and may still end that row's proof, as it always could. A chat step may move past a fixed chat step before proof (a benchmark before `proof`). This is the editor's policy, matching the board's fixed rows; the runtime's fixed law binds job-level edits, and a reviewed pull request to the file is the file changing.

**No chat runs after proof.** A pipeline that a structural edit touched may not end with a chat step after its first proof step, because proof's sheet is written before that work runs and the work would land unproved. The list is judged once every edit has applied, and the refusal is `locked` with `op` the edit that brought the shape: the `add_step` or `move_step` that placed the row, else the edit that made an earlier row proof, else the `set_step` that ended that row's own proof (a base proof row left after another proof), else the pipeline's first structural edit when the base already had that row there. Adding after proof, after a person who follows proof, or before a post that follows proof is refused, and so is adding a second proof step after the first or a proof step before the work. A base that already has a chat step after proof is repaired by removing that step or moving it before proof, in the same preview as any other edit of that pipeline; an edit of that pipeline that keeps it is refused, and an edit of another pipeline is not judged by it. A check, a post, a person, a fixed chat step and an existing proof step standing after proof keep the locks above and are not refused for standing there. `structure.gates[]` says nothing about proof.

**Crossing a person is allowed and said.** Adding, removing or moving a chat step before a person (`approve`, or a step whose kind is `gate`) changes what runs unattended before that person answers. It is not refused and no role is asked for; `structure.gates[]` names every such person, `{pipeline, gate, before[], after[]}`, where `before` and `after` are the steps (people excluded) that run before that person, before and after the edit. It is reported whenever that set changes; a reorder within it is not. A person who approves is not proof: crossing one is never refused for that reason.

**What a merged change reaches.** A job compiles its stage list when it starts and keeps that order, so a merged add, remove or move does not reorder a job already running. It is not pinned to a version of the file, though: a steer of a running job reads the recipe as it then stands to decide which of that job's stages are fixed and which policy sentences apply, so a merged change can alter those checks for a job that began before it.

**Once, and all or nothing.** A row (one pipeline, one name) is the subject of one structural edit per preview, else `bad_op`; the same name in two kinds is two rows. A `set_step` and an `add_step` of the same name in one preview, in either order, is `bad_op` at the later one: an added step's fields go on the `add_step`. Edits apply in order, so a later edit may anchor on a step an earlier one added. Any refusal refuses the whole preview, with `op` the index of the edit that caused it.

**The answer.** A preview with any structural edit carries `structure {order[], added[], counts[], gates[]}`, each always an array: `order` is `{pipeline, before[], after[]}` for every pipeline whose list changed; `added` the definitions the file did not hold; `counts` is `{pipeline, steps}` for every pipeline after the edit, in the source's kind order; `gates` as above. There is no loop count: the file has no loop. A preview with no structural edit has no `structure` key, byte for byte what it always was. A structural edit that changes nothing (a step moved to where it is) is `changed: false` with `base_text` back and `structure` present with an empty `order`. The read-back now also requires every pipeline to come back as the same list in the same order. `steps[]` does not list an added or removed step: no definition changed.

**Comments and layout.** A structural preview is the same canonical encoding as every other: the base's YAML comments are dropped and its layout rewritten, and `comments_dropped` and `format_rewritten` say so. The pull request route parses and encodes whatever it is sent, so a comment could not survive it either; keeping them needs a node-level patch and a route that commits validated text verbatim, which do not exist yet. A key the parser does not know is never stripped: the base is `base_invalid`.

## Factory: the local factory and its first run (`factory.local`)

Feature `factory.local` is present only when the engine implements `FactoryLocal` on top of the factory interface; `FactoryCapabilities.local` and `FactoryCapabilities.runner` are then `true` and are `false` for every other engine. `writes` is unchanged. Every method takes `caller` (required, at most 128 characters), refuses unknown params as `bad_params`, and speaks one fixed route of the factory; none takes a path, verb or body.

| method | params | result |
|---|---|---|
| `factory.local_start` | | `FactoryLocalStatus`. The only door that serves or joins the machine's factory; idempotent |
| `factory.local_status` | | `FactoryLocalStatus` `{state: stopped\|running, authority?: this_app\|other_codeaf, factory?, name?, url?, runner, attachment}`; `runner` is true once this engine's runner is on |
| `factory.local_run` | | `FactoryLocalStatus`. Starts this engine's runner and GitHub poll if they are not on already (the engine starts them itself; see below); idempotent; starts no job. `not_found` before `factory.local_start`; `conflict` when another codeaf serves the factory (its runner takes the jobs) or when GitHub is not connected (the factory takes no worker without a handle); `closed` while the engine stops |
| `factory.local_adopt` | | `FactoryLocalStatus`. Shows the factory `codeaf attach` named in the app's list (bearer kept in memory) |
| `factory.setup_read` | | `FactorySetupView` |
| `factory.setup_discover` | | `FactorySetupView` (lists the repos GitHub shows; reads only) |
| `factory.setup_connect` | source: `gh`\|`token`, token? (with `token` only) | `FactorySetupView` |
| `factory.setup_select` | repos (1 to 100, owner/name) | `FactorySetupView`; begins reading open issues into Backlog |
| `factory.setup_retry_read` | repo | `FactorySetupView` (only after the 40 s wait the factory enforces) |
| `factory.setup_clone` | repo, folder (`/…` or `~/…`) | `FactorySetupView` (`clone` becomes `cloning`, then `found` or `failed`) |
| `factory.setup_use_folder` | repo, folder | `FactorySetupView` (keeps a checkout already there; its origin must match) |
| `factory.setup_skip` | repo | `FactorySetupView` (clone later) |
| `factory.setup_back` | | `FactorySetupView` |
| `factory.setup_done` | | `FactorySetupView` (unfound checkouts become skipped) |

**View.** `FactorySetupView {state: github\|repos\|reading\|checkouts\|done, github {connected, gh_login?, handle?, name?, avatar_url?, organization?, visible_repos, error?}, repos[] {id, open_issues, pipeline?, archived, selected, read, read_count, waiting_s?, resumes_at?, clone, clone_error?, clone_mb?, folder?, suggested_folder?}}`. Repository discovery does not fetch each visible repository's pipeline. An absent `pipeline` means its source is not known; it must not be labelled defaults. The selected repository's background read resolves its source when possible. There is no clone percentage: the factory reports only `clone` (`missing`, `cloning`, `found`, `skipped`, `failed`) and the megabytes GitHub quoted. `folder` and `suggested_folder` are shown paths (`~/code/acme-api`, or `.../name` outside the home folder) and are empty for anything inside codeaf's own folders. The factory's checkout path, `gh` path, and server address are never carried. `github.organization` is an owner label, not organization membership: it is present only when every valid discovered repository has the same owner (case-insensitive; the first spelling is kept) and is absent for mixed owners, for zero repositories, and after any rediscovery that no longer agrees. `visible_repos` counts the same rows. The connected profile (`handle`, `name`, `avatar_url`) is independent of it.

**GitHub identity.** Setup `handle`, `name` and `avatar_url` come from `/user` with the connected credential. `gh_login` describes the ambient CLI account and may differ; it is not the connected identity. After Back from repository selection, `setup_discover` continues the existing verified connection without another token or a new credential choice.

**The connection outlives a launch.** codeaf holds the connected token in memory only, and each way of connecting is kept by its owner. `source: gh` keeps the person's yes to gh's own login in the factory's profile (gh keeps its token), and every later launch reads gh again. `source: token` takes that yes back, and the app keeps the pasted token in its keychain beside the model keys and hands it back at every launch with `keys.set {provider: "github", key}`, before the factory resumes. A finished setup (`state: done`) asks GitHub's `/user` once on `setup_read`; only a refusal (401) sends it back to `github` with `connected: false`, so a revoked or expired token is connected again by reopening setup. A GitHub that cannot be reached, or answers anything else, leaves it `done`.

**Authority.** One factory serves per machine, elected through the same `serve.json`/`serve.lock` the terminal uses. If another codeaf serves, `authority` is `other_codeaf` and this engine opens no second service; otherwise it serves on `127.0.0.1` with a bearer made for the launch. The app's home, not the environment's profile, decides where GitHub custody and the floor live.

This machine's factory starts nothing by itself and asks no model by itself. Once the factory this engine serves knows whose GitHub it is, the engine turns its runner on, with no switch to find: on `factory.local_start`, on a resume, after a setup step and on `factory.local_status`; before then it has none. Beside the runner runs the GitHub poll alone, so issues filed after setup reach Backlog: no repository start rule launches a job here and no triage reads an item, so an arriving issue starts nothing and the model is asked only by work a person started. A job starts only through `factory.job_start`. The runner is the worker loop terminal windows already use, behind the same worker lock (one runner per home), serving this factory with this engine's own keys (the app's memory) and the profile's model settings; a runner that waits behind another process's lock still reports `runner: true`. Quitting the engine stops the runner and the poll (waited for at most 10 s), the service and the bearer it held; a job the runner held returns to the queue when its lease expires.

The list marks the engine's own row (`local` in `<home>/app/factories.json`) and gives it the fixed id `factory_local`. The id is not derived from the address (the port is new at every launch), so a saved tab keeps naming it. A service the engine itself served on an explicit `factory.local_start` also marks the row `resume`: the person's standing choice, holding no credential. The row stays at quit whether or not it is marked `resume`, and `factory.detach` is the one thing that removes it; `factory.detach` of `factory_local` also ends the choice, and an explicit `factory.local_start` makes it again.

Resume. When the app opens again, the first `factory.list`, `factory.local_status` or read of `factory_local` serves the same floor again, on a new port with a new bearer, with its runner and GitHub poll (no job, triage or model call), and the row's url is refreshed before the answer. A profile with no `resume` row stays unstarted. A resume never joins or duplicates another codeaf's service: while one holds the election the row stays unreachable, and a `resume` row says why in its `reason`: "Another codeaf on this machine is still running this factory. It comes back here once that one stops." Every `factory.list` tries the resume again, so the first list after that codeaf lets go (its service closes, or its process is gone) serves the factory here. A person may instead start again, which joins it as `other_codeaf` and neither sets nor clears `resume`. A row left by an earlier release, or one made by joining another codeaf, has no `resume` and is not resumed (it stays listed, unreachable until a start). A profile whose rows are already gone has nothing to resume: start again with `factory.local_start`; no old remote id is mapped to it.

One row per connected factory. `factory.attach`, an access link or `factory.local_adopt` for an address already listed (the live local service's included) resolves to that row's id and adds none. The local row is answered without a second probe. Attaching by address leaves `resume` and the engine's own bearer as they were: it keeps the bearer it holds. Only a completed explicit sign-in through an access link installs the verified caller grant, replacing that bearer for the row.

Collisions on one address. A row a person attached by address, solo or not, and the session held for it, is never replaced or forgotten by a start, resume or quit. When a saved registry holds a manual row (not marked `local`) and a row marked `local` at the same address, the manual row wins with its own metadata and id; only the conflicting local marker is discarded, and no `resume` survives with it. When this engine joins another codeaf's service at an address a person already attached, the manual row keeps its id and is left unchanged. When this engine would serve its own factory at an address a manual row holds, it fails closed with `conflict` and changes nothing; the person can explicitly detach the conflicting saved connection before starting again. Only a stale row carrying the mark and not this factory's is dropped when the engine starts. A registry that holds the local row twice loads as one.

Setup after a restart. Saved setup progress is not proof of a live GitHub credential. If the factory no longer holds a current one, `factory.setup_read` answers `state: github`, clears the connected profile (`connected`, `handle`, `name`, `avatar_url`, `organization`, `visible_repos`), and keeps the repository and checkout progress for when the person connects again.

**Attachment.** `attachment.state` is `none`, `available` (a safe `codeaf attach` exists), `adopted` (the app lists it) or `unsafe` (plain http to another machine, never adopted: `forbidden`). `factory.local_adopt` is explicit; the bearer in `attach.json` is read once, held in memory, and not written to `<home>/app/factories.json`.

**Errors.** Setup before start is `not_found` ("the factory on this machine is not started"). A folder inside codeaf's own folders, or the filesystem root, is `forbidden`; a relative one is `bad_params`. The factory's own refusals pass on as `engine` (`busy` while its setup worker runs) with its sentence and no secret. A pasted token is registered with the redactors before it is sent and is never in a result, event or error.

## Rules the app can rely on

- **Credentials stay engine-side.** `token` on attach is input only. No result, event or error carries a token, session, device code, cookie, or the access link. `FactoryAccess` has no `link` or `access_token` field by construction. A frame test searches for each.
- **Sessions are memory-only.** A restart keeps the connection (url, name) and loses the sign-in: the factory reads `signed_out` until the person signs in again.
- **`caller`** is any stable string the app chooses per window (at most 128 characters). A sign-in flow or access-link flow belongs to the caller that started it. Another caller polling or cancelling it gets `not_found`, the same as a flow that never existed. A session is per factory, not per caller.
- **Sign-in:** `factory.sign_in_start` returns `user_code` and `verification_uri` to show, and `flow` to poll no sooner than `interval` seconds (earlier polls answer `pending` without a request). `signed_in` carries the person's `me`. `denied.message` is the factory's reason when it gave one (not on the team, link is for someone else).
- **Access links:** `access_check` asks, `access_sign_in` starts, `access_check` again polls, `access_cancel` ends. **Safe check:** a factory that advertises `access_check_post: true` in its public `GET /v1/about` is checked and polled with `POST /v1/factory-access {link, verb:"check"}` (verbs: `sign_in`, `cancel`, `check`), so the invitation secret is never in a URL; `check` only reads or polls and never starts OAuth, mints a cookie or resets a flow. The engine asks `/v1/about` first (no link sent; cached per origin for one flow life, bounded) and a failed, refused or malformed probe, or a failed advertised POST, is an error and never falls back to GET. **Legacy limitation:** a factory whose valid `/v1/about` lacks the flag is still checked with `GET /v1/factory-access?link=…`, which puts the secret in the request URL (proxy and access logs) exactly as those factories always have; only upgrading the factory removes that. `joined` and `already_joined` save the connection; `factory.id` in the answer is the id `factory.list` uses.
- **Bounds:** 32 connections; 256 pending flows (sign-ins plus access links), 8 sign-ins per caller; flows expire (sign-in with the code, access link 10 minutes of silence); one call 25 s; one request to a factory 20 s; answers over 8 MiB (1 MiB for small ones) are `too_large`; no ambient proxy; redirects are refused (`unreachable`). Plain `http` is accepted only for loopback; the address may not carry credentials, path or query.
- **Recovery codes (in wire.go):** `unreachable`, `signed_out`, `github_expired` (the screen shows "Reconnect GitHub"). Also `too_large`, `busy`, `not_found`, `bad_params` as elsewhere.
- **Persistence:** `<home>/app/factories.json`, 0600 in a 0700 folder, urls and names only.

## Gaps

- Server routes the brief names that this slice does not carry: hand-off cards, step edits, runner add/pause/remove and presence (only a slots change is served: `factory.runner_slots`), the whole-list repository door and any clone, folder or checkout action (`factory.repo_set` turns one repository on or off and nothing else; `factory.pipeline_pr` opens a pull request from a whole file of text and neither reads the repository's file nor edits it; `factory.pipeline_source` and `factory.pipeline_preview` read it and preview two kinds of edit (a step's prompt, until, max, when, effort and fanout; the budget) and write nothing, and there is no reorder, add or remove of a step in the repository's file (a job's own steps are `factory.job_step_edit`), no kind edit, no model, budget per step, only_if or loop, and no way to learn whether a pull request is still open), settings, session lists, factory events stream. `factory.queue` is a poll; there is no `factory.changed` event yet.
- `factory.setup` (an attached remote's progress) is read-only. Setup verbs exist only for the local factory (`factory.setup_*`); a team member's checkout on this Mac is not carried. The local service runs no poll or triage, so issues opened after setup are not read until a later slice decides who polls. There is no stop or pause verb for the local runner: it ends with the engine.
- Device sign-in against a factory that refuses unauthenticated callers needs the connection's token or session as the credential (the factory binds a flow to it). Renewal after `github_expired` through that route is unproven against a real expired session: the server answers `github_expired` on the same bearer. The access link flow is the proven way back.
- Sessions do not survive a restart (the local factory's bearer is made again at each resume); `config.AppCredentials` has no slot for factory sessions, so the app cannot hand them back at launch yet.
- `codeaf attach` (CLI, attach.json) and the app registry are separate stores; neither reads the other.


## Scoped memory reading

| method | params | result |
|---|---|---|
| `memory.applies` | agent | MemoryScopeSnapshot |

Available when hello includes `memory.applies`; other engines answer `unknown_method`. This reads the memory scope applicable to an existing agent.

## Factory: naming a factory in the app (`factory.rename`)

Feature `factory.rename` is present only when the engine implements `FactoryRenamer` on top of the factory interface. Other engines answer `unknown_method`.

| method | params | answers |
|---|---|---|
| `factory.rename` | `{factory, name}` | `{}` |

`name` (at most 60 characters; inner runs of space folded) is the person's own name for the connection. It is kept on the app's record of it (`<home>/app/factories.json`), never sent to the factory, and `factory.list` answers it as the connection's `name` over the factory's own. An empty `name` goes back to the default: the factory's own name, or for the factory on this Mac `<login>'s factory` once it knows whose GitHub it is (`Your factory` before). A factory that is not listed is `not_found`.

## Factory: check GitHub now (`factory.refresh`)

Feature `factory.refresh` is present only when the engine implements `FactoryRefresher` on top of the factory interface. Other engines answer `unknown_method`.

| method | params | answers |
|---|---|---|
| `factory.refresh` | `{factory}` | `{}` |

One `POST /v1/github/poll` to the factory, sent once. The factory wakes its GitHub poll and answers before the read; it never waits for it. The read's outcome is on the next `factory.queue`: the `desk.sources` row named `github` carries `polling` (a read in flight), `polled` (RFC 3339, when GitHub was last read without a failure; absent until one has) and `trouble` (the last failure in a few words). The row is absent while no poll is alive on the factory's machine. A factory without the route is `engine` ("this factory does not provide that action").

**Start re-checks GitHub.** `factory.job_start` (and the factory's `run` and `run_again`) on a job that came from GitHub reads that one issue or pull request first. Closed or merged there, the job is not started: it is done (`done_how` `closed` or `merged`) and the start is `conflict` with `#12 was closed on GitHub, so it wasn't started.` (`detail.now_state` `done`). GitHub unreachable starts the job anyway.

## Factory: a job's conversations (`factory.job_chats`)

Feature `factory.job_chats` is present only when the engine implements `FactoryJobChatter` on top of the factory interface. Other engines answer `unknown_method`.

| method | params | answers |
|---|---|---|
| `factory.job_chats` | `{factory, job}` | `FactoryJobChats {factory, job, agent, steps: [{step, agent, rounds?: [{round, agent}]}]}` |

`agent` is the job's agent: the item's own conversation, which the run reports into and which reads what the person types as the brief before a run and the steer during one (and the answer while the job waits on a question). It is made when the job has none yet; that makes a conversation and its team on this machine, asks no model and starts no job. `steps` is the latest chat of each step that ran, in the run's order; a step that has not run, or whose chat cannot be opened, is left out. A step's `rounds` names the chat of each of its finished rounds, oldest first, matched to `job.steps[].rounds` by `round` (the latest finished round's is usually the step's own `agent`); a round with no chat (a check step's), one recorded before round chats were kept, or one whose chat cannot be opened is left out, and a step with none has no `rounds`. Each `agent` is an ordinary agent id: the engine holds it open before answering (a running step is lent, as to any window), so the app opens and messages it as it does any conversation, and a message to a step's chat goes to that step directly. The chats are read from the factory's own record of the job, never from a team's names.

Only the factory this engine serves (`factory_local`) is answered; any other is `not_found` ("this factory's chats are on the machine that runs its jobs"). A dropped job has no conversations (the drop deleted them) and is never given an agent: it is `not_found` with `the job's conversation could not be opened: #12 was dropped · Bring back returns it to Backlog`. A job dropped before drops deleted conversations still answers the ones it names, until Bring back deletes them. A job with a model of its own (`job.model`) puts its agent on that model, and `factory.job_set_model` moves the agent with the job, so every chat of one job reads one model.

The factory's teams (`factory_managed`) are the job's and no longer appear in `crews.overview`'s team pages; reading one by id still answers it.

## Factory: changing a computer's slots (`factory.runner_manage`)

Feature `factory.runner_manage` is distinct from every other factory feature and is present only when the engine implements `FactoryRunnerAdmin` on top of the factory interface; `FactoryCapabilities.runner_manage` is then `true`. It announces one method. Whether THIS factory and THIS computer can take the change is not in the capability: it is read from `factory.runners` (`slots_policy`, and each runner's `live_capacity`).

| method | params | result |
|---|---|---|
| `factory.runner_slots` | `{factory, runner, slots}` | `FactoryRunnerSlotsDone {factory, runner, slots}`: the same connection, computer id and number that were asked for, answered only after the factory answered 200 or 204. |

**Params.** Exactly the three fields; a route, verb, body, token, checkout list or any other field is `bad_params`. `runner` is the id exactly as `factory.runners` lists it: opaque text of 1 to 200 bytes, valid UTF-8, with no control or format character, `/` or `\`, no surrounding space, and not `.` or `..`. `slots` is required, a whole number from 1 to the factory's own maximum (`api.MaxRunnerSlots`; the app reads `slots_policy.max` and does not hard-code it).

**Who and when.** Only the OWNER of that computer: each call asks `GET /v1/me` and `GET /v1/runners` with the credential held now and the runner's `owner` must be the handle `GET /v1/me` names. The operator has no power over another person's computer. With no held credential nothing is sent. Before any write the engine refuses with `conflict` when the directory carries no `slots_policy` (an older factory), when it is not `{min:1, max:<expected>, requires_live:true}`, or when the computer's `live_capacity` is not true (its worker predates live capacity: it needs a compatible update, and a restart alone does not help). An unlisted id is `not_found`; one that belongs to someone else is `forbidden`.

**The write.** One `PUT /v1/runners/{runner}` with `{slots, require_live_capacity:true}` and no checkouts, never retried. The factory re-checks `live_capacity` of the machine's newest attach under the lock that writes, so a worker that restarted onto an older binary since the read is refused with 409 and nothing changes. Only a 4xx or 501 carrying `X-Codeaf-Phase: pre-mutation` is a definite refusal (fixed words); the marker is cleared immediately before the first change. A dropped connection, a timeout, a redirect, a 5xx, an unmarked refusal, or an unexpected success such as 202 is `uncertain`: read `factory.runners` for what is granted. If the sign-in changes during a wait, no computer or number is revealed; after the write the answer is a neutral `uncertain`.

**Directory fields.** `factory.runners` now may carry `slots_policy {min, max, requires_live}` (only from a factory that enforces the guard) and per runner `live_capacity`, which is the NEWEST attach's attestation (attaches that cannot be ordered must all attest).
