# Apps · wave 2 · track crews · contract

Track **crews** builds: Crews and apps (board 13), the App ⌄ settings menu
items Open files · Make my copy · Needs · Triggers · Budget · Archive app… ·
Delete app… (board 12), Factory as codeaf's prebuilt app in the Apps section
(boards 05, 07), and the `custom/*.tsx` escape hatch (boards 03, 04). Branch
`apps/w2-crews` in both repos. The owner's boards in `docs/apps/spec/` win over
this file; `docs/apps/CONTRACT.md` (wave 1) still holds for everything not
named here.

Cross-track rulings R1–R7 apply: all code lives in new per-track files; a
shared wave-1 file gets at most one registration or call line per track.

## 1 · Files this track adds

| Repo | New files (crews owns) | One-line edits in shared files |
| --- | --- | --- |
| bin | `internal/apps/bridge/proto_crews.go` (types, method and topic constants) | — |
| bin | `internal/apps/svc/svc_crews.go` (interfaces below) | `svc.Services`: one line `Crews CrewsServices` |
| bin | `internal/apps/gateway/crews.go` (method table for §2) | gateway: one `registerCrews()` hook line |
| bin | `internal/apps/lifecycle/` (Store decorator, archive, delete, copy) | `server.go`: one wiring line |
| bin | `internal/apps/settings/` (triggers list/pause, budget) | `runs/trigger.go`: one hook call · `runs/budget.go`: one hook call |
| bin | `internal/apps/crewlink/` (record ↔ crew links) | — |
| bin | `internal/apps/crewagent/` (an app's agent as a crew member) | — |
| bin | `internal/apps/prebuilt/` (Factory definition, embedded) | — |
| bin | `internal/apps/custom/` (custom/*.tsx check rules + bundle) | `check`: one rule-hook line · `spec` loader: accept `custom/*.tsx` |
| bin | `internal/appapi/crew_apps.go` (record items, app-agent members) | appapi crew/member handlers: additive lines (R5) |
| bin | `internal/session/crew_apps.go` (route crew messages to an app's agent) | session crew routing: one dispatch line (R5) |
| bin | `kit.json`: one `Custom` part entry (R3) | `cmd/codeaf/app_serve.go`: one line handing `svc.Services` to appapi |
| desk | `src/lib/types/apps-crews.ts` (mirror of proto_crews.go) | — |
| desk | `src/lib/apps/bridge/mock-crews.ts` (mock methods for §2) | mock bridge: one registration line |
| desk | `src/features/apps-shell/menu-items/` **registry** (R2) + `menu-items/crews.ts` | `app-menu.tsx` renders the registry |
| desk | `src/features/app-settings/` (sheets and dialogs) | — |
| desk | `src/features/app-crews/` (Discuss in a crew, Discussed in, record items, app-agent member) | crew Items/Team/Add-a-member: additive hooks |
| desk | `src/features/app-factory/` (Factory row + tab) | Apps section: one line |
| desk | `src/kit/custom/` (Custom part, sandbox iframe, postMessage bridge) | kit index: one line |

## 2 · Bridge methods (WebSocket, wave-1 framing)

All app-scoped unless marked *global*. Roles: Personal = Owner + Editor of
everything (until the spaces track lands `Caller` workspace/app roles, R6;
code against `svc.Caller` and a `RoleOf` helper, never against `svc.Owner`).

### Lifecycle (`internal/apps/lifecycle`)

| Method | params → result | Who | Notes |
| --- | --- | --- | --- |
| `apps.archive` | `{}` → `AppSummary` | Editor, Admin | hides from `apps.list` and ⌘K; triggers stop; runs in flight finish; records kept |
| `apps.restore` | `{}` → `AppSummary` | archived: Editor, Admin · deleted: Owner, Admin | back as it was |
| `apps.delete` | `{confirm: "<app name>"}` → `{}` | Owner, Admin | `confirm` must equal the name exactly, else `invalid`; app, versions, triggers removed for everyone; records kept 30 days then purged |
| `apps.copy` | `{name?}` → `AppSummary` | anyone who can open it | "Make my copy": new **Personal** app, files at the current version, **no records** (rule: records belong to the space), seeded with sample records, own budget and versions, `kept` v1; name default `"<name> (my copy)"` |
| `apps.lifecycle` *global* | `{}` → `{archived: LifecycleApp[], deleted: LifecycleApp[]}` | deleted list: Owner, Admin | `LifecycleApp = {app: AppSummary, at, by, purge_at?}` |

`lifecycle.Wrap(store svc.Store) svc.Store` decorates B1's store: `List`
drops archived and deleted apps, `Get`/`Definition` of a deleted app return
`not_found` except to `apps.restore`. State lives in
`$CODEAF_HOME/apps/<id>/lifecycle.json`; purge runs on server start and daily.
Every action writes one Activity line through the spaces track's Activity
writer interface (R6; no-op until it lands): `"@mona archived CRM · Restore"`.

Open files needs no method: `apps.get` already returns `files`.

### Settings (`internal/apps/settings`)

| Method | params → result | Who |
| --- | --- | --- |
| `triggers.list` | `{}` → `{triggers: TriggerInfo[]}` | anyone who can open it |
| `triggers.pause` | `{id, paused: bool}` → `TriggerInfo` | Editor |
| `budget.get` | `{}` → `BudgetInfo` | anyone who can open it |
| `budget.set` | `{month: number}` → `BudgetInfo` | workspace Owner, Admin (Personal: you) |

```
TriggerInfo = {id, on, type?, run, source: "record"|"schedule"|"connector",
  label,            // "Deal quiet 7 days → draft a follow-up"
  ai: bool, paused: bool, paused_by?, paused_at?, last_fired?, next_fire?}
BudgetInfo  = {month, spent, currency: "USD", resets_at, at_cap: "ask"|"stop",
  paused: bool,     // true once spent >= month: agent stops, AI triggers pause
  can_edit: bool, presets: [5, 10, 20, 50]}
```

Hooks (in `svc_crews.go`; runs engine calls them, one line each, R4):

```go
// TriggerGate is consulted by runs before a trigger fires. Allow records the
// fire (last_fired) and returns false while the trigger is paused or the app
// is archived. Events while paused are skipped, not queued.
type TriggerGate interface { Allow(app, trigger string, at time.Time) bool }
// BudgetCap overrides app.json budget.month when an Admin set one.
type BudgetCap interface { Month(app string) (usd float64, ok bool) }
```

Overrides persist in `$CODEAF_HOME/apps/<id>/settings.json`. Pause/resume and
budget changes write one Activity line with Undo (`"@mona paused Deal quiet 7
days · Undo"`).

### Crew links (`internal/apps/crewlink`)

A link, never a copy. One link = one record in one crew.

| Method | params → result |
| --- | --- |
| `links.add` | `{crew, type, id}` → `CrewLink` |
| `links.remove` | `{crew, type, id}` → `{}` (record and its Discussed-in history stay: the link becomes `unlinked`) |
| `links.for_record` | `{type, id}` → `{links: CrewLink[]}` — "Discussed in", newest first |
| `links.for_crew` *global* | `{crew}` → `{links: CrewLink[]}` across every app the caller can open |

```
CrewLink = {crew, crew_name, app, app_name, type, id, label,
  by, linked_at, state: "open"|"finished"|"unlinked",
  access: "ok"|"no_access"|"deleted"|"archived"}
```

`access` is computed per caller: `no_access` → desk shows "CRM record · no
access"; `deleted` → "Deleted in CRM" (row dimmed); `archived` → "CRM is
archived · Restore" (Restore for Owner/Admin). Stored in
`$CODEAF_HOME/apps/<id>/links.json` with a reverse index by crew. Topic
`app/<id>/links` publishes `CrewLink` on every change. Finish crew sets
`state: finished` on all its links; reopening relinks the same links.

### App agent in a crew (`internal/apps/crewagent`, `internal/appapi`, `internal/session`)

| Method | params → result |
| --- | --- |
| `crewagent.badge` | `{}` → `{app, app_name, agent_name, can: "can read deals, draft emails", role}` — the line from the app's capabilities ∩ the adder's app role (a Viewer adds an agent that only reads) |
| `crewagent.candidates` *global* | `{}` → `{agents: Badge[]}` — app agents of apps the caller can use, for Add a member |

```go
// CrewAgent answers a crew message as the app's agent.
type CrewAgent interface {
    Ask(ctx context.Context, req CrewAsk) (CrewAnswer, error)
}
type CrewAsk struct {
    App, Crew, CrewName string
    Asker  svc.Caller          // the person who asked the crew; approvals go to them
    Text   string
    Linked []bridge.Record     // the crew's linked records in this app, read as Asker
}
type CrewAnswer struct {
    Text      string
    Changed   []ChangedRecord  // {type, id, label, what, version} → "Changed in CRM through the CRM agent"
    Approvals []bridge.Approval
    ShapeThis string           // non-empty → "That changes the app · Shape this"
    Cost      float64          // charged to the app's budget, not the crew's
}
```

`Ask` calls `svc.Agent.Send` with `Scope{App, Caller: {Member: Asker.Member,
Role: Asker.Role, Agent: true}}` and thread `crew:<crew id>`. Approvals it
opens carry `Payload.crew`, `Payload.crew_name`, and `Who = Asker.Member`
(never the manager). Rule 02 holds: asked to change the app it answers with
`ShapeThis`.

appapi (R5, additive, new file `crew_apps.go` + one line per handler):

- `POST /app/v1/crews/{cid}/items` accepts `{kind: "record", app, type, id}`
  → `links.add`; `DELETE …/items/{ref}` with `ref = app:<app>/<type>/<id>`
  → `links.remove`.
- `GET /app/v1/crews/{cid}` `items` includes record links as
  `{kind: "record", ref, app, app_name, type, id, label, detail, state, access, href: "codeaf://apps/<app>/<type>/<id>"}`,
  `detail` read live from the app (e.g. `"Proposal · $48,000"`).
- `POST /app/v1/crews/{cid}/members` accepts `{kind: "app_agent", app}`;
  the member carries `badge` (above). One live copy per crew.
- Finish crew → links `finished`, the app agent member leaves.
- Crew report lists record links under "Everything it touched", each "CRM · View".

session (R5): a crew message addressed to an `app_agent` member is routed to
`CrewAgent.Ask`; the answer is posted to the crew Log as that member, with the
changed-records block and the approval cards.

### Factory as a prebuilt app (`internal/apps/prebuilt`)

| Method | params → result |
| --- | --- |
| `apps.prebuilt` *global* | `{}` → `{apps: [{id: "factory", name: "Factory", kind: "factory", version, space}]}` |

Factory is codeaf's own app: listed in the Apps section like any app, opens in
an app tab with the usual header (Use · App ⌄); its screens are codeaf's
existing Factory pages (Jobs · Running · Repos · Pipelines · Runners · People ·
Activity). Its definition files are embedded and shown read-only by Open files;
Shape, Delete and Make my copy are dimmed with "prebuilt". No AI decides in
Factory (board 17 non-goal).

### custom/*.tsx (`internal/apps/custom`)

- `kit.json` gains one part: `{"name": "Custom", "kind": "pattern",
  "interactive": false, "events": [], "children": "none", "bind": ["records",
  "record", "run", "runs", "selection"], "props": {"file": {"type": "string",
  "required": true}, "title": {"type": "string"}}}`.
- The spec loader accepts `custom/<name>.tsx` files; only the Builder writes them.
- Check rule `kit/custom`: the file exists, ≤ 64 KB, imports only `react` and
  `@codeaf/bridge`, no `fetch`, `XMLHttpRequest`, `WebSocket`, `EventSource`,
  `eval`, `Function(`, dynamic `import(`, `window.top`, `parent.`,
  `localStorage`, `document.cookie`.
- `custom.bundle` `{file}` → `{js, hash}`: the server compiles the tsx to one
  ES module (pure Go), cached per version by hash.
- Desk: one iframe per Custom node, `sandbox="allow-scripts"` (no
  same-origin), CSP `default-src 'none'; script-src 'unsafe-inline'`
  (srcdoc), no network, no storage, no top navigation; created when it scrolls
  into view, destroyed when it leaves; a "custom" label chip on its frame.
  The iframe talks only by `postMessage`; the host forwards typed calls
  (`records.*`, `runs.start`, `approve`, `tools.call`) to the same bridge, so
  every call is checked on the server (rule 05).

## 3 · Desk: App ⌄ item registry (R2)

`src/features/apps-shell/menu-items/registry.ts`:

```ts
export interface AppMenuContext { app: AppSummary; detail?: AppDetail;
  role: "editor" | "user" | "viewer"; spaceRole: "owner" | "admin" | "member";
  personal: boolean; prebuilt: boolean }
export interface AppMenuItem {
  id: string; label: string; order: number;        // board 12 order
  meta?(c: AppMenuContext): string | undefined;    // right-side live number
  disabled?(c: AppMenuContext): string | undefined; // reason shown dimmed, e.g. "Owner · Admin"
  destructive?: boolean;
  Sheet: React.ComponentType<{ ctx: AppMenuContext; onClose(): void }>;
}
export const registerAppMenuItems: (track: string, items: AppMenuItem[]) => void;
```

Order (board 12): Versions… 10 (shape) · Open files 20 · Make my copy 30 ·
Needs 40 · Triggers 50 · Budget 60 · Members 70 (spaces) · Move to
workspace… 80 (spaces) · Import records… 85 (connect) · Archive app… 90 ·
Delete app… 100. Every item shows for every role; one you may not use is dimmed
with the reason on its right. Files: `menu-items/<track>.ts`, one import line
each in `menu-items/index.ts`.

## 4 · Lanes

| Task id | Repo | Owns | Deps |
| --- | --- | --- | --- |
| `w2crews-b0-seams` | bin | proto_crews.go, svc_crews.go, gateway/crews.go, stub packages, registration lines, kit Custom entry, appapi handle | — |
| `w2crews-d0-registry` | desk | apps-crews.ts, mock-crews.ts, menu-items registry | — |
| `w2crews-b-lifecycle` | bin | internal/apps/lifecycle | b0 |
| `w2crews-b-settings` | bin | internal/apps/settings + runs hook lines | b0 |
| `w2crews-b-link` | bin | internal/apps/crewlink + appapi crew_apps.go items | b0 |
| `w2crews-b-agent` | bin | internal/apps/crewagent + appapi members + session crew_apps.go | b-link |
| `w2crews-b-factory` | bin | internal/apps/prebuilt | b0 |
| `w2crews-b-custom` | bin | internal/apps/custom + check hook + loader | b0 |
| `w2crews-d-menu` | desk | src/features/app-settings | d0 |
| `w2crews-d-crews` | desk | src/features/app-crews | d0 |
| `w2crews-d-factory` | desk | src/features/app-factory | d0 |
| `w2crews-d-custom` | desk | src/kit/custom | d0 |
