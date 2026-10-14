# Apps · wave 1 contract

The single source of truth for every Apps lane in wave 1. The owner's spec boards
in `docs/apps/spec/` win over this file where they disagree; raise the
disagreement with the contract lane and fix this file, never work around it.

Code is the other half of this contract and must agree with it:

| What | Go | Desk |
| --- | --- | --- |
| File schemas | `internal/apps/spec/schema/*.schema.json` (embedded) | — |
| File types, loader, validation | `internal/apps/spec` | — |
| Kit catalog | `internal/apps/spec/kit.json` | renders exactly these parts |
| Wire protocol | `internal/apps/bridge/proto.go` | `src/lib/types/apps.ts` |
| Service seams | `internal/apps/svc/svc.go` | — |
| Golden app | `internal/apps/spec/testdata/crm/` | `fixtures/apps/crm/` (copy) |

A change to any of these is a contract change: one commit that updates the code,
this file and both copies of the fixture, announced to every lane.

## 1 · Wave 1 scope

Owner types a paragraph → Builder writes `plan.json` → plan card → Write →
Check passes → Preview on sample data → Keep → the app opens in a tab in Use
and works: records CRUD through screens, runs started by hand or on record
change, the app agent answers about and edits content, outbound sends wait on
approvals and go to fakes.

Personal space only: the codeaf sidecar on 127.0.0.1, one member (`me`) who
holds every role. **Not in wave 1:** Shape, Apply, migrations, versions after
v1, workspaces, roles UI, real connectors (fakes only), the six document
surfaces, crews, factory-as-app, `custom/*.tsx`, schedule and connector
triggers firing (they validate but do not fire), freeze, blind fields.

## 2 · File formats

An app is a folder; its definition is these files (relative slash paths). The
Builder writes them and nothing else does (rules 01 and 02).

| File | Format | Schema `$id` |
| --- | --- | --- |
| `app.json` | JSON | `https://codeaf.dev/schema/app/1.json` |
| `plan.json` | JSON | `https://codeaf.dev/schema/plan/1.json` |
| `screens/<id>.json` | JSON | `https://codeaf.dev/schema/screen/1.json` |
| `runs/<id>.yaml` | YAML, parsed to JSON first | `https://codeaf.dev/schema/run/1.json` |
| `agent.md` | Markdown with YAML front matter | — (parsed by `spec.ParseAgent`) |

`app.json`, `plan.json` and `agent.md` are required. A screen's `screen` and a
run's `run` must equal the file name. Read `testdata/crm/` next to this file's
Go twin for a complete example of each.

### app.json

`id` (slug `^[a-z][a-z0-9_-]{0,47}$`), `name`, `description`, `space`,
`version`, `kit` (2), `agent {name, file: "agent.md", model}`, and:

- `records`: `{ <type>: {label, plural, title, fields, constants?, freeze?} }`.
  `title` names the field shown as the record's name.
- Field: `{type, label?, required?, unique?, index?, values?, default?, to?,
  currency?, masked?, computed?, ai?, blind?}`. Types: `text longtext number
  money date datetime duration enum enums bool person people ref refs email
  phone url file files place`. `enum`/`enums` need `values` (order = meter
  order). `ref`/`refs` need `to` — **that is how relations are declared**.
  `index: true` asks the store for an indexed generated column. `computed` is a
  CEL expression the server evaluates; `ai` is the question an `ai.sort` answers
  (value labelled AI; a person's edit pins it). `default: "$me"` = the caller.
- `capabilities`: `records {<type>: "r"|"rw"}`, `connectors {<name>: {as:
  member|workspace, can: [verbs], approve: [outbound verbs]}}`, `ai: [sort,
  summarise, draft, extract]`, `tools: []`. A capability not listed does not
  exist for the app.
- `triggers`: `[{id, on, run, type?, when?, cron?, params?}]`; `on` is
  `record.created | record.changed | record.deleted | schedule | connector`.
  `record.*` need `type`; `when` is CEL (`changed('stage') && stage == 'Won'`).
  Manual starts need no trigger: any run may be started by `runs.start`. Wave 1
  fires `record.*` only.
- `commands`: `[{id, label, for?, key?, confirm?, run | do}]` — RecordPage
  `actions` and ⌘K entries. `do` is a list of screen actions.
- `budget`: `{month (USD), currency: "USD", per_run?, at_cap: ask|stop, models?}`.

Needs are never written by the Builder; codeaf computes them from `app.json`
(`plan.json` carries `"needs": "computed"`).

### plan.json

`{ask, name, data[], automation[], screens[], agent, needs: "computed"}`. Each
of `data`, `automation`, `screens` is a list of lines `{id, line}`; `agent` is
`{id: "agent", line}`. The `id` is the record type, run or screen the line
becomes, so the card edits a line in place and Check can match plan ↔ files.

### screens/<id>.json

`{screen, title, params?, nav?, body}`. `nav` is the one `SectionTabs` node.
A node is `{part, id?, label?, props?, bind?, on?, when?, children?, slots?}`:

- `part`: a kit part name from `kit.json` (section 3). Anything else fails.
- `id`: unique in the screen; lists need it for `$selection.<id>`.
- `label`: required on every interactive part (kit law `kit/label`).
- `props`: only the props the part declares in `kit.json`; strings may hold
  `{{templates}}`; tokens only, no raw colours/sizes/fonts.
- `bind` — exactly one of:
  `{"records": "<type>", "query": "<tokens>"}` live list ·
  `{"record": "<$ref or id>", "type": "<type>"}` one record ·
  `{"run": "<run id>", "for": "<$ref>"}` latest run of that id ·
  `{"runs": "<run id>"}` runs list ·
  `{"approvals": "open"|"all"}` approvals ·
  `{"selection": "<list id>"}`.
- `on`: `{<event>: [action, …]}`; events per part are listed in `kit.json`. The
  list runs in order; the first failure stops it and shows a Toast.
- `when`: CEL; false = not rendered, no fetch.
- `slots`: `{row?, empty?}` single nodes for data parts.

Actions — one verb key per object, plus the modifiers shown:

| Verb | Shape | Notes |
| --- | --- | --- |
| `open` | `{"open": "<screen>", "id"?, "params"?, "beside"?}` | navigates inside the app tab |
| `records.create` | `{"records.create": {"type", "set"}, "ask"?, "confirm"?}` | |
| `records.update` | `{"records.update": {"id" \| "ids", "set"}, "ask"?, "confirm"?}` | one Undo toast per batch |
| `records.delete` | `{"records.delete": {"id" \| "ids"}, "confirm"}` | `confirm` required (kit/confirm) |
| `runs.start` | `{"runs.start": "<run>", "params"?, "ask"?, "confirm"?}` | cost check first |
| `approve` | `{"approve": "<approval id ref>", "answer": "yes"\|"no"}` | |
| `tools.call` | `{"tools.call": ["<connector>", "<verb>", {args}]}` | outbound → approval |
| `ai.sort` | `{"ai.sort": "<list id>", "field": "<ai field>"}` | labelled AI |
| `agent.ask` | `{"agent.ask": "<text>", "record"?}` | opens the docked agent with the text |

`ask: {"<name>": "<field type>"}` asks for `$input.<name>` in a small Sheet
before the action runs. References: `$me $space $params.x $record $row
$selection.<id>.ids|.id|.count $input.x $now`; runs add `$steps.<id>.output`
and loop variables (`$deal`). Templates: `{{deal.name}}`, no logic inside.

Queries use QueryField tokens: `stage:Proposal,Won stage:-Lost owner:me
amount>10000 name:north* has:email sort:-amount` (`-` excludes, `,` = or,
`sort:` last).

### runs/<id>.yaml

`{run, title?, budget? {per_run}, params? {<name>: {type, to?, values?,
default?}}, steps}`. A step has `id` plus, besides an optional `gate`, one
action: `agent` (+ `prompt`, optional `output` JSON Schema — an AI step, its
output labelled AI) · `tool` (`connector.verb`, + `as`, `args`) ·
`records.query | records.create | records.update | records.delete` (`{type,
query?, id?, ids?, set?, limit?}`) · `wait {for | until | timeout}` · nested
`steps` (with `loop {over, as, max}` or `loop {until, max}`; `max` required) ·
`shell` (only under `workspace: repo`; not in wave 1). Modifiers: `if` (CEL),
`on_error stop|continue|goto <id>`, `retry {max ≤ 5, backoff}`, `on_timeout`.
`gate {approval: plan|proof|start|send, who, title}` waits for a person. **An
outbound tool always runs behind a `send` gate, inserted if the YAML omits
it.** The schema enforces **exactly one action besides gate**. Put the gate and
the action it protects in the same step; a standalone gate step is invalid.
Approval resumes that action, then the following step.

Run references use one convention: `$params.bug` preserves a typed value;
`{{params.bug}}` interpolates the same value into text, including queries.
Params always live under `params`; `$bug` / `{{bug}}` only mean a variable
introduced by a containing `loop.as: bug`. Results use
`$steps.triage.output.severity` / `{{steps.triage.output.severity}}`;
`records.query` returns fields directly in output. The other roots are
`$me`, `$space`, `$now`. Schema validation rejects malformed templates,
undeclared params and unknown namespaces, including the bare-param spelling.
Record ids are declared as `ref` with `to`; lists as `refs` with `to`.
Record triggers explicitly bind run params in app.json, e.g.
`params: {bug: "$record.id"}`. Runs then read `$params.bug`.

Queries are `field:value` tokens, not CEL or SQL. Quote spaces, apostrophes
and literal commas **inside** the query: `status:"in progress"`,
`status:"won't fix"`, `title:"{{params.title}}"`. YAML string quotes alone
do not quote the embedded query value; use a block scalar when convenient.

```yaml
run: wont-fix
params:
  bug: {type: ref, to: bug}
steps:
  - id: approve-change
    gate: {approval: start, who: $me, title: "Approval needed: won't fix"}
    records.update: {type: bug, id: $params.bug, set: {status: "won't fix"}}
  - id: load
    records.query: {type: bug, query: 'id:{{params.bug}}', limit: 1}
```

Check supplies a typed fake value for every declared param, sample record ids
for refs, and the triggering sample record when resolving record.* bindings.
Its reports retain the failing query and repair hints in issue messages.
WRITE and FIX share the reference, query and gate rules; FIX includes every
current definition file in full, including each failing file.

### agent.md

```
---
name: CRM agent
model: deepseek/deepseek-v4.1-flash
tools: [records.query, records.get, records.create, records.update, records.delete, gmail.draft, gmail.send]
---
Instructions (Markdown body).
```

`name` is required. `tools` must be a subset of what `app.json` capabilities
allow; the agent never gets definition-writing tools. Derive this list from
the app, not the few-shot example: every r/rw type requires records.query
and records.get; every rw type also requires records.create, records.update
and records.delete. These global tools cover each permitted type; capability
checks still apply to the type on every call. Include runs.start for runs
the plan needs the agent to start and only granted connector verbs. Check
enforces the subset and record coverage rules.

### Records (wire)

`{id, type, version, fields {…}, ai? [fields set by AI], created_at,
updated_at, created_by, updated_by}`. Dates are RFC 3339 / ISO 8601, money is a
number in major units in wave 1, refs are record ids. `sample.json` is
`{<type>: [{id, fields}]}`.

## 3 · Kit · wave-1 parts

`internal/apps/spec/kit.json` is the catalog as data: per part `name`, `kind`
(`primitive | pattern | data`), `interactive` (needs `label`), `events`,
`children` (`"none" | "any" | [allowed parts]`), `bind` (allowed binding
kinds), `slots`, `props` (`{type, values?, required?}` with types from
`propTypes`). The desk renders exactly these 39 parts; Check rejects others.

- Primitives (20): Button, IconButton, Chip, Field, Select, Segmented, Toggle,
  Menu, Dialog, Avatar, StateGlyph, Icon, Text, Stack, Row, Divider,
  DatePicker, PeoplePicker, Timer, Masked.
- Patterns (13): SectionTabs, QueryField, StateMeter, List, Panel, Table,
  Board, BulkBar, Thread, Empty, FilterTokens, StatStrip, ScreenSubtitle.
- Data (6): RecordList (QueryField + StateMeter + views incl. Board by
  `groupBy`), RecordPage, Form, RunTimeline, ApprovalCard, Chart.

Not in wave 1: the six surfaces, Pages, Compare, Checklist, SignOff,
DiffViewer, ProofList, LogStream, FileViewer, Media, Kbd, Code, Tooltip,
Sheet, Toast as screen parts (Toast and Sheet are shell-owned: `ask` and
action errors use them).

Board 08 adds three general RecordList header children: `FilterTokens`,
`StatStrip`, and `ScreenSubtitle`. `FilterTokens` inherits the parent list's
query; removing a chip re-queries the records. Its optional `labels` array gives
friendly display labels for the initial predicates in query order, without
changing the predicates sent to codeaf. `sort` and `ai` label its sort action;
`on.sort` uses normal screen actions. Its Views menu contains the list's `views`
and the screen sections when `SectionTabs.props.placement` is `views` (the
default placement remains `header`). `StatStrip` binds `records`, uses `field`
to request `count_by`, and displays enum values in schema order. A separate
unfiltered binding keeps every stage count visible while the list is filtered.
`ScreenSubtitle` inherits the list's live `total`, followed by its `noun`,
static `phrase`, and optional keyboard `hint`. With header children present,
RecordList replaces its default query, meter and summary with those children.

Kit law rules Check enforces in wave 1: `kit/parts` (known parts and props),
`kit/label`, `kit/one-nav`, `kit/record-list` (growing lists use RecordList),
`kit/confirm` (destructive actions confirm), `kit/approve` (outbound goes
through approval), `kit/no-raw-style`, `kit/no-network`, `kit/children`
(children rule), `kit/bind` (binding kind allowed for the part),
`kit/capability` (every action covered by `app.json`), `kit/selection`
(`$selection.<id>` names a list in the screen).

## 4 · Bridge protocol

One WebSocket per space at **`/app/v1/apps/ws`** on the sidecar (the same
listener and guard as the rest of `/app/v1`). Subprotocols:
`["codeaf.apps.v1", "bearer.<token>"]` — browsers cannot set Authorization on a
WebSocket, so the token rides as the second subprotocol; the server answers
`codeaf.apps.v1`. Origin and host rules are the existing guard's. Messages are
JSON text frames:

```
{ id?, type, method?, app?, topic?, params?, result?, error?, event?, seq? }
```

| type | dir | meaning |
| --- | --- | --- |
| `call` | → | invoke `method` with `params`; `app` set for app-scoped methods |
| `result` | ← | answer to a call, sub or unsub with the same `id` |
| `error` | ← | `{code, message, details?}` for the same `id` |
| `sub` / `unsub` | → | start/stop `topic`; answered with `result` |
| `event` | ← | one event on `topic`, `seq` increases per topic |

Calls may resolve out of order; match on `id`. On reconnect, resubscribe and
re-query (events are not replayed).

### Methods

| Method | `app` | params → result |
| --- | --- | --- |
| `apps.list` | — | `{}` → `{apps: AppSummary[]}` |
| `apps.get` | ✓ | `{}` → `AppDetail` (summary + `files {path: text}` + `needs` + `left_out`) |
| `apps.create` | — | `{paragraph, model?, budget_month?, connectors?, just_build?}` → `AppSummary` (state `planning`) |
| `builder.plan.edit` | ✓ | `{edits: [{section?, id?, text}]}` → `{plan, needs}` |
| `builder.write` | ✓ | `{}` → `{}`; progress on `app/<id>/build` |
| `builder.run.fix` | ✓ | `{file: "runs/<id>.yaml"}` → `{}`; WRITE/FIX only that left-out run |
| `builder.cancel` | ✓ | `{}` → `{}` |
| `apps.keep` | ✓ | `{}` → `AppSummary` (state `kept`, version 1, real records empty) |
| `apps.discard` | ✓ | `{}` → `{}` (folder and records gone) |
| `records.query` | ✓ | `{type, query?, limit?, after?, count_by?}` → `{rows, next?, total, counts?}` |
| `records.get` | ✓ | `{type, id}` → `Record` |
| `records.create` | ✓ | `{type, set}` → `Record` |
| `records.update` | ✓ | `{type, id \| ids, set}` → `{rows}` |
| `records.delete` | ✓ | `{type, id \| ids}` → `{ids}` |
| `runs.start` | ✓ | `{run, params?}` → `RunState` |
| `runs.get` | ✓ | `{run_id}` → `RunState` |
| `runs.list` | ✓ | `{run?, for?}` → `{runs}` newest first |
| `approvals.list` | ✓ | `{state?}` → `{approvals}` |
| `approvals.decide` | ✓ | `{approval_id, answer: yes\|no, edit?}` → `Approval` |
| `agent.send` | ✓ | `{thread?, text, record?, screen?}` → `{thread, turn}`; reply streams on `app/<id>/agent/<thread>` |

App state: `planning → building → preview → kept`, or `building → failed`
(Fix it · Cancel). Records calls read and write **sample** data while the app
is not kept and **real** data once kept; the gateway decides from the state.

### Topics

| Topic | Event type |
| --- | --- |
| `app/<id>/build` | `BuildEvent {step: plan\|write\|check\|preview, status: running\|done\|failed, round?, log?, files?, plan?, needs?, check?, left_out?: [{file, name, reason}], cost {spent, cap}, at}` |
| `app/<id>/state` | `AppSummary` on every state change |
| `app/<id>/records/<type>` | `RecordDiff {op: upsert\|delete, type, id, record?, by}` |
| `app/<id>/runs` | `RunState` on every step change |
| `app/<id>/approvals` | `Approval` on open and decide |
| `app/<id>/agent/<thread>` | `AgentEvent {kind: delta\|tool\|approval\|shape_this\|done\|error, turn, text?, tool?, summary?, approval?, request?, cost?}` |

Server-side, a hub subscription to `app/<id>/*` receives every topic of the app
(runs use it for triggers).

### Errors

`code` is one of `denied` (capability or role) · `invalid` (params, values,
unknown method) · `not_found` · `budget` (would pass the app or run budget) ·
`check_failed` · `conflict` (wrong state, stale version) · `not_built` (lane
stub) · `internal`. Services return `*bridge.Error`; anything else becomes
`internal`.

## 5 · Builder and Check event stream

`apps.create` → state `planning`; Builder publishes `{step: plan, status:
running}` then `{step: plan, status: done, plan, needs}` (the plan card).
`builder.plan.edit` rewrites lines and returns the new plan. With `just_build`
or after `builder.write`: state `building`, then for each step `running` →
`done` with `files` (write) and `check` (`CheckReport {pass, round, issues
[{file, path, rule, message}], rendered [screen ids], runs [run ids]}`). A
failed round publishes `{step: check, status: failed, round, check}` and the
Builder retries for three Check rounds (two FIX calls). If the final report
contains only issues attributed to existing `runs/*.yaml` files, the Builder
leaves those automations out: removes the files, their plan lines, triggers,
commands, screen start actions and run bindings. It removes `runs.start` from
agent.md when no runs remain. It checks the reduced definition again before
preview. If that Check finds a screen or app/record failure, the original files
are restored and the build remains `check_failed` in state `failed`.
Otherwise the build finishes in `preview` with
`left_out: [{file, name, reason}]` on build events and `apps.get`. `name` is the
run title, falling back to its file id; `reason` retains the last issues and
repair hints. The final preview event carries `left_out: []` after all omitted
runs have been repaired. Results survive restarts in Builder-owned
`build-result.json`, outside the definition directory.

Fix it on an omitted automation calls `builder.run.fix` with its exact file.
WRITE and FIX can change only that run; FIX includes the current definition as
context and ignores unrelated returned files. The Builder restores the run's
plan line and the removed references on definition files that have not been
changed since the omission, then performs the usual Check rounds. A repeated
run-only failure returns to preview with the omission still listed. Screen,
record and app schema failures stay real failures.

Check validates screen query filter and sort tokens against the bound record
schema, including hidden bindings and resolved templates, as well as field
props such as groupBy and Table.sort. Run query field names are checked even
when a gate prevents execution. Run parameter references must name declared
parameters; ref/refs parameters target declared record types and contain ids,
not field objects. Trigger parameter field references must belong to their
record type. Unknown fields include the file, bad field and valid fields in
the repair hint passed to FIX.

On pass, Builder seeds sample records (`Sample.Generate` → `Records.Seed`), publishes `{step:
preview, status: done}` and sets state `preview`. `cost` on every event is the
Builder's spend against the app budget. `builder.cancel` stops at the next
step boundary and returns to `planning`.

## 6 · Package and directory ownership

| Lane | Owns | Implements |
| --- | --- | --- |
| C0 contract | `docs/apps/CONTRACT.md`, `internal/apps/spec/**`, `internal/apps/bridge/proto.go`, `internal/apps/svc/`, `internal/apps/server.go`, desk `src/lib/types/apps.ts`, both CRM fixtures | — |
| B1 store + versions | `internal/apps/store/` | `svc.Store` |
| B2 records + query + sample | `internal/apps/records/`, `internal/apps/query/`, `internal/apps/sample/` | `svc.Records`, `svc.Sample` |
| B3 gateway | `internal/apps/gateway/`, `internal/apps/caps/`, `internal/apps/realtime/` | dispatch, `svc.Caps` (+ Needs), `svc.Hub` |
| B4 runs engine | `internal/apps/runs/` | `svc.Runs`, `svc.Approvals`, `svc.Connectors` (fakes via `internal/connect` shapes) |
| B5 builder | `internal/apps/builder/` | `svc.Builder`, `svc.Model` |
| B6 check | `internal/apps/check/` | `svc.Check` |
| B7 app agent | `internal/apps/agent/` | `svc.Agent` |
| D1 kit | desk `src/kit/` | the 39 parts |
| D2 runtime | desk `src/lib/apps/runtime/` | screen renderer, bindings, actions, CEL `when` |
| D3 bridge client | desk `src/lib/apps/bridge/` | WS client, subscriptions, mock bridge for browser dev and e2e |
| D4 apps shell | desk `src/features/apps-shell/` | rail Apps section, app tab, Use mode, docked agent |
| D5 create flow | desk `src/features/app-create/` | New app sheet, plan card, Building, Preview, Keep/Discard |
| E1 integration | `internal/apps/*_test.go` beyond unit tests, desk `e2e/apps-*` | real-model e2e |

### The seam

Tracks add bridge methods with `gateway.Register(name, Method{Handle: handler, Allow: allow})`
from an `init()` in their own file; the method's own Allow decides it.
Cross-cutting limits use `caps.AddDenyRule`, which can only deny. Never wrap
`gateway.call` or `caps.Allow`. Duplicate method names panic at startup. Only
the spaces track sets the caller resolver (`gateway.SetCallerResolver`, from
its `init()`); the default is the Personal owner; a second Set panics.
`gateway.OnDisconnect(func(svc.Caller))` hooks run in registration order when
a socket closes, with recovery and logging per hook.

The permission registry lives in caps: `gateway.Register` forwards each
method's Allow to `caps.RegisterAllow` (including nil for wave-1 methods), so
caps never imports gateway. Generic context, member and object checks run
first, then every deny rule in registration order, then the method's own
Allow or the wave-1 role and capability checks. Audit appends once per call.
Topic owners register their prefix with `caps.RegisterTopic(prefix, Allow)`;
`app/*/shape/` matches any app id, and unregistered topics retain wave-1 checks.
Duplicate or overlapping topic prefixes are refused.

Every lane package exports `func New(cfg svc.Config, all *svc.Services) *T`
(runs also `NewApprovals`, `NewConnectors`; builder also `NewModel`).
`internal/apps/server.go` calls them once and fills `all` before any call, so
a service reaches its peers at call time (`all.Records`, `all.Hub`, …). **A
lane replaces the `stub.go` file in its own package and never edits
`server.go`, `svc.go` or another lane's package.** Interface changes go
through the contract lane.

Walking skeleton today: `store` serves the golden CRM as kept v1; `records`
is in memory, seeded from `sample.json`, publishes row diffs, and understands
only `sort:`; `caps` allows everything; `realtime` works; everything else
returns `not_built`.

### Data layout

`$CODEAF_HOME/apps/<id>/` (`svc.Config.Root` is `$CODEAF_HOME/apps`):
`definition/` (the files above; git with tag `v1` on Keep), `records.db`
(SQLite, real records), `sample.db` (sample records), `runs/` journal state.
Only B1 decides more.

## 7 · The ten rules

| # | Rule | Wave 1 |
| --- | --- | --- |
| 01 | The Builder never reads content | **Enforced**: `svc.Builder` gets no records/content API; it reads files, kit, sample, check reports. |
| 02 | The app's agent never changes the app | **Enforced**: agent tools are records + connectors only; asked to change the app it emits `shape_this`. |
| 03 | Nothing leaves without approval | **Enforced**: outbound tools always gate on `send`; fakes only in wave 1. |
| 04 | Drafts never write real records | Partly: not kept = sample data only. Shape drafts are wave 2. |
| 05 | Every bridge call is checked on the server | **Enforced**: gateway → `Caps.Allow` before every method and sub. |
| 06 | Screens use the kit and tokens only | **Enforced** by Check kit law. `custom/*.tsx` not in wave 1. |
| 07 | Apply never loses data | Not in wave 1 (no Apply). Delete keeps nothing in sample; real deletes keep history in B2. |
| 08 | Acts as a person only with their Allow | Not in wave 1 (personal space, fakes). |
| 09 | AI is always labelled; people win | **Enforced**: `Record.ai`, `StepState.ai`; a person's edit clears the field from `ai`. |
| 10 | Runs run on the server, inside a budget | **Enforced**: runs engine on the sidecar, `per_run` and monthly cap → `budget` error / `needs_you`. |

## 8 · Running it

- Server: `make build` then `bin/codeaf app serve --home <dir>`; stdout prints
  the hello `{url, token, …}`; the bridge is at `<url>/app/v1/apps/ws`.
  `go test ./internal/apps/...` dials it in-process (`internal/apps/server_test.go`).
- Desk: `pnpm typecheck`, `pnpm test`; the sidecar is started by `src-tauri`
  from the pinned binary in `codeaf-sidecar.json`. Browser dev uses D3's mock
  bridge serving `fixtures/apps/crm/`.

- Wave 2 · spaces: [contract/spaces.md](contract/spaces.md)
- Wave 2 · shape track: [contract/shape.md](contract/shape.md)

### c2 run fallback · gateway and desk handoff

The Builder owns the wire extensions as `builder.ResultEvent` and
`builder.ResultDetail`, embedding the existing bridge envelopes. It exposes
`AppDetail(context.Context, svc.Scope) (any, error)` for the `apps.get` handler.
**Gateway lane still needs to call that optional adapter** before its existing
Store.Get path; the existing handler does not expose `left_out` until wired:

```go
if provider, ok := all.Builder.(interface {
    AppDetail(context.Context, svc.Scope) (any, error)
}); ok {
    return provider.AppDetail(ctx, scope)
}
```

This keeps Store metadata and service interfaces unchanged. The new
`builder.run.fix` method is already registered from the Builder through the
contract's gateway registry, with editor-only access.

Desk lane: add to `src/lib/types/apps.ts`:

```ts
export type LeftOutAutomation = { file: string; name: string; reason: string };
// AppDetail
left_out: LeftOutAutomation[];
// BuildEvent (the final preview event always carries an array)
left_out?: LeftOutAutomation[];
// builder.run.fix params
{ file: string }
```

Show the omissions in Preview with Fix it sending `builder.run.fix`, and use
`apps.get` after reconnect to recover the durable result. A final empty array
clears the previously displayed omissions.
