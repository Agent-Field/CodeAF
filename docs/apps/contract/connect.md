# Apps · wave 2 · track connect · contract

Track **connect** (session codeaf-w2-connect) builds: connectors on two levels
(personal connections with a per-app **Allow**, workspace connections added by
Admins), the connector **door** that sends drafts, Preview and Check to fakes
and applied apps to real Gmail · Calendar · Slack · MCP through
`internal/connect`, "Approval needed: …" for everything outbound, expired
connections that wait instead of failing, and **Import** from CSV · Airtable ·
HubSpot with a field-mapping preview.

Boards: `11-connectors.*` (all of it), the Import card of
`12-app-settings-lifecycle.*`, the New app sheet's Connectors row and the
Preview "Uses" strip of `05-rail-with-apps.*`, and flow (c) of
`03-architecture.*`. The boards win over this file; copy comes from the boards
exactly. `docs/apps/CONTRACT.md` (wave 1) still holds for everything not named
here. Branch `apps/w2-connect` in both repos. Rulings R1–R7 of the wave-2 brief
apply: real code in new per-track files; a shared wave-1 file gets at most one
registration or call line per track.

Not here: workspace server mode, roles and the audit/Activity store (spaces),
drafts and their overlay (shape), the App ⌄ menu mechanics (crews), surfaces.

## 1 · Words

- **Connection** — one signed-in account of a service: `{service, account}`
  (`gmail · mona@acme.com`, `stripe · acme live`). Services: `gmail`,
  `calendar`, `slack`, `github`, `stripe`, `airtable`, `hubspot`, `mcp:<server>`.
- **Personal connection** — belongs to one person, the same in every space
  (Settings → Accounts). A workspace app uses it only after that person's
  **Allow** for that app. In Personal there is no Allow: Personal apps use your
  accounts directly.
- **Workspace connection** — belongs to the workspace (`support@`, Stripe, the
  GitHub org). Only Owner and Admin add, change or remove it. **Who can use**:
  `all` apps or `chosen` apps. Apps use it with no per-person sheet.
- **Allow** (grant) — `{member, app, service}`: "CRM may send email as Mona".
  Once per person, per app, per service. Revoke at any time; the next use asks
  again.
- **Fake** — the dry-run double of every connector: records the call, returns a
  canned or recorded reply, sends nothing. Used whenever `Scope.Data` is not
  `real` (sample: Preview and Check; `draft`: Shape drafts, rule 04).
- **Outbound verb** — send, post, reply, pay, charge, create-external, and any
  MCP tool marked outbound. Always behind a `send` gate (rule 03); Allow never
  removes the gate.

## 2 · Packages and files this track adds

| Repo | New files (connect owns) | One-line edits in shared files |
| --- | --- | --- |
| bin | `internal/apps/bridge/proto_connect.go` (types, method and topic constants, §4) | — |
| bin | `internal/apps/svc/svc_connect.go` (interfaces, §3) | `svc.Services`: one line `Connect ConnectServices` |
| bin | `internal/apps/gateway/connect.go` (method rows for §4) | gateway: one `registerConnect()` line |
| bin | `internal/apps/connect/` — the connector **door** (svc.Connectors), fakes registry, real adapters over `internal/connect`, expiry waits | `server.go`: `all.Connectors = connect.New(...)` replaces the runs fake (R4) |
| bin | `internal/apps/grants/` — Allow store, workspace connections + who-can-use, Needs-connections view | `server.go`: one wiring line |
| bin | `internal/apps/imports/` — CSV/Airtable/HubSpot sources, mapping, preview, run once, Undo | `server.go`: one wiring line |
| bin | `internal/apps/runs/connector_hook.go` — the hook interface the runs engine calls for connector steps (R4) | `runs/connectors.go`: one call line delegating to the door |
| desk | `src/lib/types/apps-connect.ts` (mirror of proto_connect.go) | — |
| desk | `src/lib/apps/bridge/mock-connect.ts` (mock methods for §4, fake data from board 11) | mock bridge: one registration line |
| desk | `src/features/app-connect/` — Allow sheet, Needs-connections section, fake bar + fake rows, Inbox lines | Inbox: one line registering the two connect line kinds |
| desk | `src/features/settings-accounts/` — Settings → Accounts (your connections, Apps that act as you) | settings tabs: one line |
| desk | `src/features/workspace-connections/` — Workspace settings → Connections, Who can use, Remove dialog | workspace settings tabs: one line (spaces owns that page) |
| desk | `src/features/app-import/` — the Import sheet | `apps-shell/menu-items/connect.ts` (R2): Import records…, Needs connections section · ⌘K "<App>: Import records…": one line |

Consumed seams (R6), coded against interfaces until they land:

- spaces: `svc.Caller` workspace role (`owner|admin|member`) and app role, and
  the Activity/audit writer. Until then: Personal, caller = Owner + Editor;
  audit writes go to a no-op writer.
- shape: `svc.DataDraft`. Until then: anything not `real` = fake.
- crews: `menu-items/` registry. Until then the Import sheet opens from ⌘K and
  the empty record list only.

## 3 · Service seams (`internal/apps/svc/svc_connect.go`)

```go
// ConnectServices groups this track's services on svc.Services.
type ConnectServices struct {
	Grants  Grants
	Imports Imports
}

// Grants (internal/apps/grants) owns Allow and workspace connections.
type Grants interface {
	Accounts(ctx context.Context, caller Caller) (bridge.AccountsResult, error)
	Allow(ctx context.Context, scope Scope, p bridge.AllowParams) (bridge.Grant, error)
	Revoke(ctx context.Context, caller Caller, p bridge.RevokeParams) error
	Workspace(ctx context.Context, caller Caller) (bridge.WorkspaceConnectionsResult, error)
	AddWorkspace(ctx context.Context, caller Caller, p bridge.AddWorkspaceConnectionParams) (bridge.WorkspaceConnection, error)
	SetWho(ctx context.Context, caller Caller, p bridge.SetWhoParams) (bridge.WorkspaceConnection, error)
	RemovalImpact(ctx context.Context, caller Caller, id string) (bridge.RemovalImpact, error)
	RemoveWorkspace(ctx context.Context, caller Caller, id string) error
	Needs(ctx context.Context, scope Scope) (bridge.ConnectionNeeds, error)
	// Resolve picks the credential for one call: a workspace connection the
	// app may use, or the member's personal connection with an Allow on file.
	// Errors: denied (not allowed for this app), needs_allow, expired.
	Resolve(ctx context.Context, scope Scope, service string) (Credential, error)
}

// Credential is what the door hands an adapter. Tokens never leave the server.
type Credential struct {
	ID      string // connection id
	Service string
	Account string
	Level   string // personal | workspace
	Member  string // owner of a personal connection
}

// Imports (internal/apps/imports) owns one-time imports.
type Imports interface {
	Sources(ctx context.Context, scope Scope, p bridge.ImportSourcesParams) (bridge.ImportSourcesResult, error)
	Preview(ctx context.Context, scope Scope, p bridge.ImportPreviewParams) (bridge.ImportPreview, error)
	Run(ctx context.Context, scope Scope, p bridge.ImportRunParams) (bridge.ImportResult, error)
	Undo(ctx context.Context, scope Scope, p bridge.ImportUndoParams) (bridge.ImportUndoResult, error)
}
```

`svc.Connectors` (wave 1) keeps its shape; `internal/apps/connect` implements
it and becomes `all.Connectors`.

## 4 · Wire (`proto_connect.go` · `apps-connect.ts`)

```
Connection   {id, service, account, level: personal|workspace, status: connected|expired, since, detail?}   // detail: "read and push in 6 repos"
Grant        {member, app, app_name, space, service, can: [string], at}          // can: lines from the app's Needs
AccountsResult {connections: Connection[], grants: Grant[]}                      // Settings → Accounts
AllowParams  {service, answer: "allow"|"not_now"}
AllowSheet   {app, app_name, member, member_name, service, account, title, can: [string], cant: [string]}  // "CRM may send email as Mona"
RevokeParams {app, space, service}
WorkspaceConnection {id, service, account, added_by, who: "all"|"chosen", apps: [app ids], used_by: [{app, app_name, for}]}
WorkspaceConnectionsResult {connections: WorkspaceConnection[], can_edit: bool}
AddWorkspaceConnectionParams {service, who, apps?}       // sign-in runs through internal/connect auth
SetWhoParams {id, who, apps?}
RemovalImpact {id, service, apps: [{app, app_name, for, runs_this_week}]}
ConnectionNeed {service, level: personal|workspace, label, for, status: allowed|not_yet|ask_admin|expired|set_by_admin, allowed?: {n, of}}
ConnectionNeeds {connections: ConnectionNeed[]}
ConnectorWait {app, run_id, step, service, member, reason: expired|needs_allow, since, stops_at}   // one Inbox line
FakeCall     {app, run_id?, tool, verb_label, summary, at, fake: true}         // "Would send · Re: Team plan… to Priya Shah"

ImportSource {kind: csv|airtable|hubspot, label, connected: bool, detail}
ImportSourcesParams {type}             ImportSourcesResult {sources: ImportSource[]}
ImportPreviewParams {type, source: {kind, file?: base64 csv, base?, table?, object?: "deals"}, mapping?: Mapping[]}
Mapping      {column, field?: string, as?: "field"|"note"|"skip", values?: {<source value>: <enum value or "">}, people?: {<source>: <member or "">}}
ImportPreview {import_id, type, total, rows_shown: Record-like[5], columns: ColumnMap[], need_choice: int, duplicates: int, new_refs: {<type>: n}, can_import: bool, count: int}
ColumnMap    {column, first, field?, match: name|ref|enum|people|none, note, needs_choice: bool, options?}
ImportRunParams {import_id, mapping: Mapping[]}
ImportResult {import_id, created: int, skipped: int, new_refs: {<type>: n}, activity: string}
ImportUndoParams {import_id}           ImportUndoResult {removed: int, kept_changed: [record ids]}
```

### Methods

| Method | `app` | params → result | Who |
| --- | --- | --- | --- |
| `connect.accounts` | — | `{}` → `AccountsResult` | anyone (own only) |
| `connect.allow` | ✓ | `AllowParams` → `Grant` | the caller, for themself |
| `connect.allow.sheet` | ✓ | `{service}` → `AllowSheet` | the caller |
| `connect.revoke` | — | `RevokeParams` → `{}` | the caller, own grants |
| `connect.workspace.list` | — | `{}` → `WorkspaceConnectionsResult` | members read; `can_edit` for Owner, Admin |
| `connect.workspace.add` | — | `AddWorkspaceConnectionParams` → `WorkspaceConnection` | Owner, Admin |
| `connect.workspace.who` | — | `SetWhoParams` → `WorkspaceConnection` | Owner, Admin |
| `connect.workspace.impact` | — | `{id}` → `RemovalImpact` | Owner, Admin |
| `connect.workspace.remove` | — | `{id}` → `{}` | Owner, Admin |
| `connect.needs` | ✓ | `{}` → `ConnectionNeeds` | anyone who can open the app |
| `connect.waits` | — | `{}` → `{waits: ConnectorWait[]}` | own waits; Admins also workspace-connection waits |
| `connect.fakes` | ✓ | `{run_id?}` → `{calls: FakeCall[]}` | anyone who can open the app (preview/draft) |
| `imports.sources` | ✓ | `ImportSourcesParams` → `ImportSourcesResult` | Editor, User |
| `imports.preview` | ✓ | `ImportPreviewParams` → `ImportPreview` | Editor, User |
| `imports.run` | ✓ | `ImportRunParams` → `ImportResult` | Editor, User; `invalid` while `need_choice > 0` |
| `imports.undo` | ✓ | `ImportUndoParams` → `ImportUndoResult` | who ran it, Editors |

### Topics

| Topic | Event |
| --- | --- |
| `connect/<member>/allow` | `AllowSheet` — a run needs this person's Allow (opens the sheet if the app is in view, else one Inbox line "CRM wants to send email as you · Allow") |
| `connect/<member>/waits` | `ConnectorWait` on open and on clear |
| `connect/workspace` | `WorkspaceConnection` on add, change, remove |
| `app/<id>/fakes` | `FakeCall` per fake call in preview, check or a draft |

### Errors (wave-1 codes, `details.reason` names the case)

`denied` + `reason: not_allowed_for_app` (workspace connection not in Who can
use → Needs shows "Stripe · ask an Admin") · `denied` + `reason: admin_only` ·
`conflict` + `reason: needs_allow` · `conflict` + `reason: expired` ·
`invalid` + `reason: needs_choice`.

## 5 · Behaviour (each line is an acceptance test)

1. **Door.** `Scope.Data != real` → fake, always, for every service; the call
   is recorded and published on `app/<id>/fakes`. `RunState.fake = true`.
   Nothing reaches `internal/connect` (rule 04).
2. **Real.** `Scope.Data == real` → `Grants.Resolve` → adapter over
   `internal/connect` (Gmail, Calendar, Slack, MCP tools; GitHub, Stripe,
   Airtable, HubSpot through MCP or their `internal/connect` client where one
   exists). Every call passes `Caps.Allow` first (rule 05).
3. **Outbound.** An outbound verb without an approved `send` gate is `denied`
   (wave-1 behaviour kept). Allow does not remove the gate (rule 03).
4. **Allow.** In a workspace, the first personal-connection use by a person in
   an app with no grant → the step waits (`needs_allow`), `connect/<member>/allow`
   fires; `allow` → the step resumes at once; `not_now` → the step keeps
   waiting with one Inbox line. Other members' runs are not affected. In
   Personal: no grant needed (rule 08).
5. **Can / can't** lines on the sheet are written from `app.json`
   capabilities (`connectors.<name>.can` and `approve`), never by the agent.
6. **Revoke** takes effect on the next call; the next use asks again.
7. **Workspace connections.** Add/who/remove: Owner, Admin only (`denied` +
   `admin_only` otherwise). An app outside `chosen` gets `denied` +
   `not_allowed_for_app`. Remove signs out, writes one Activity line "Mona
   removed Stripe · used by 2 apps", those apps' Needs show "ask an Admin";
   their Stripe steps wait and ask an Admin. Records already saved stay.
8. **Expired.** A call on an expired connection never fails silently: the step
   waits (status `needs_you`, amber), one `ConnectorWait` goes to the owner
   (personal → that person; workspace → every Admin); Reconnect → the step runs
   at once. After 7 days of waiting the run stops: "stopped · Gmail expired".
9. **Needs.** `connect.needs` lists every connection the app uses and as whom:
   "as each person" or "<workspace> workspace"; caller's own status; Editors
   see `allowed {n, of}`.
10. **Import.** Sources list CSV always, Airtable/HubSpot when the caller has
    that connection. Preview auto-matches columns by name, refs by title
    (counting new ref records), enums need a value map, people match
    workspace members by email, unmatched columns → note or skip. Rows that
    match an existing record by title are duplicates and skipped. Run once,
    no sync, writes real records directly (no draft), never changes the app.
    Undo removes the imported records except ones changed since (listed).
    One Activity line: "@santosh imported 300 deals into CRM from HubSpot ·
    12 skipped as duplicates · 9 new companies". Airtable/HubSpot reads go
    through the door, so tests use fakes.
11. **Audit.** Allow, revoke, add/remove workspace connection, every real
    outbound call, every import and undo → one audit line (spaces writer).

## 6 · Data on disk

Space-level (`$CODEAF_HOME/connect-apps.db`, SQLite, modernc, owned by
`grants`): `grants`, `workspace_connections`, `workspace_connection_apps`,
`waits`. Tokens stay in `internal/connect`'s store / Keychain — never in app
files, never on the wire (architecture: Secrets). Per app
`$CODEAF_HOME/apps/<id>/imports/<import_id>.json`: mapping, created record
ids and their versions at import time (for Undo).

Open item (raised to the manager): a workspace server reaching a member's
personal token across hosts. Wave 2 resolves personal credentials from the
`internal/connect` store on the server the call runs on; a cross-host
delegate is a follow-up.

## 7 · Lanes

| Id | Engine@host | Owns | Deps |
| --- | --- | --- | --- |
| w2connect-b0-wire | codex@spark | proto_connect.go, svc_connect.go, gateway/connect.go, stubs, registration lines | c0 |
| w2connect-b1-door | codex@studio | internal/apps/connect, runs/connector_hook.go | b0 |
| w2connect-b2-grants | codex@spark | internal/apps/grants | b0 |
| w2connect-b3-import | codex@studio | internal/apps/imports | b0 |
| w2connect-d0-types | codex@studio | apps-connect.ts, mock-connect.ts + its registration line | c0 |
| w2connect-d1-consent | cursor@spark | src/features/app-connect | d0 |
| w2connect-d2-settings | cursor@spark | settings-accounts, workspace-connections | d0 |
| w2connect-d3-import | codex@studio | app-import, menu-items/connect.ts | d0 |
