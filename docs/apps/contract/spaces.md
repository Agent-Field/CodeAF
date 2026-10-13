# Apps · wave 2 · spaces contract

Track **spaces** (session `codeaf-w2-spaces`, branch `apps/w2-spaces`). Boards:
`06-spaces-and-people`, `12-app-settings-lifecycle` (Members, Move parts),
`01-model` §1 and §4. The boards win over this file; this file wins over code.

Scope: workspace server mode, the space switcher (Personal · acme · globex),
workspace roles Owner · Admin · Member, app roles Editor · User · Viewer,
workspace Members and invites, app Members, signatories and SignOff chains,
Move to workspace…, the append-only audit log and Activity.

Follows the wave-2 cross-track rules (R1, R4, R6): every new type, method and
route lives in a spaces-only file; a shared wave-1 file gets at most one
registration line per seam, each on its own line.

## 1 · Shared seams other tracks consume (R6, landed in c0)

These land first. **connect, crews and shape code against them.** Until a
service is real, its stub returns the Personal answer (one member `me`,
workspace Owner, app Editor, audit appended to `apps/audit.db`).

- `space.OnMemberGone(h func(ctx context.Context, member string))` in
  `internal/apps/space/hooks.go` registers member-data cleanup hooks in a
  mutex-guarded package registry; nil hooks are ignored. `memberGone(ctx,
  member)` invokes a snapshot of the hooks synchronously in registration
  order with the supplied context and member, outside the registry lock.
  No service caller is wired yet; b1 calls it from Remove and Leave.

### Caller (svc.go, two added lines)

```go
type Caller struct {
	Member string
	Role   Role          // app role for Scope.App; "" on space-level calls
	Agent  bool
	Space  string        // NEW: "personal" or the workspace id ("acme")
	WS     WorkspaceRole // NEW: owner | admin | member
}
```

`WorkspaceRole` (`owner`, `admin`, `member`) and helpers live in
`svc/svc_spaces.go`: `(Caller).IsAdmin() bool` (owner or admin),
`(Caller).CanShape() bool` (Role == editor and not Agent),
`(Role).AtLeast(r Role) bool` (editor > user > viewer).

The **gateway fills Caller once per call**: member, space and WS from the
WebSocket session (section 3); Role from `AppRoles.Role` for `Scope.App`.
Services never compute roles themselves; they read `scope.Caller`.
In Personal: `{Member: "me", Space: "personal", WS: owner, Role: editor}`.

### Services (svc.go: one embedded line; server.go: one call line)

```go
// svc.go, in Services:
	SpacesServices // spaces track: Spaces, Members, AppRoles, Audit, Signoff
// server.go, in Wire, after all.Caps:
	wireSpaces(cfg, all) // internal/apps/server_spaces.go
```

`svc/svc_spaces.go`:

```go
type SpacesServices struct {
	Spaces   Spaces
	Members  Members
	AppRoles AppRoles
	Audit    Audit
	Signoff  Signoff
}

// AuditWriter is what every track calls after a write, reveal, send,
// approval, denial, Apply, migration or role change (rule: append-only).
type AuditWriter interface {
	Append(ctx context.Context, e bridge.AuditEntry) (bridge.AuditEntry, error)
}

type Audit interface {
	AuditWriter
	List(ctx context.Context, scope Scope, p bridge.AuditListParams) (bridge.AuditListResult, error)       // owner/admin
	Activity(ctx context.Context, scope Scope, p bridge.ActivityListParams) (bridge.ActivityListResult, error) // members
}

type AppRoles interface {
	// Role is the caller's effective app role: own row, else Everyone, else
	// editor for the app's maker. Personal: always editor.
	Role(ctx context.Context, app, member string) (Role, error)
	List(ctx context.Context, scope Scope) (bridge.AppMembers, error)
	Set(ctx context.Context, scope Scope, p bridge.AppRoleSetParams) (bridge.AppMembers, error)
	// Seed makes the maker Editor and Everyone User; Store calls it on Keep
	// and on the first Apply (shape) and on Move.
	Seed(ctx context.Context, app, maker string) error
	AskEditor(ctx context.Context, scope Scope, p bridge.AskEditorParams) (bridge.Empty, error)
}

type Spaces interface {
	Info(ctx context.Context, caller Caller) (bridge.SpaceInfo, error)
	// Personal only: the spaces this desktop has joined and how to reach them.
	List(ctx context.Context, caller Caller) (bridge.SpacesListResult, error)
	Join(ctx context.Context, caller Caller, p bridge.SpaceJoinParams) (bridge.SpaceJoinResult, error)
	Connect(ctx context.Context, caller Caller, p bridge.SpaceConnectParams) (bridge.SpaceConnection, error)
	New(ctx context.Context, caller Caller, p bridge.SpaceNewParams) (bridge.SpaceJoinResult, error)
	Forget(ctx context.Context, caller Caller, p bridge.SpaceRef) (bridge.Empty, error)
}

type Members interface {
	// Authenticate maps a bearer token to the member (workspace mode).
	Authenticate(ctx context.Context, token string) (Caller, error)
	List(ctx context.Context, caller Caller) (bridge.MembersListResult, error)
	Invite(ctx context.Context, caller Caller, p bridge.InviteParams) (bridge.Invite, error)
	CancelInvite(ctx context.Context, caller Caller, p bridge.InviteRef) (bridge.Empty, error)
	SetRole(ctx context.Context, caller Caller, p bridge.MemberRoleParams) (bridge.Member, error)
	Remove(ctx context.Context, caller Caller, p bridge.MemberRef) (bridge.Empty, error)
	Leave(ctx context.Context, caller Caller) (bridge.Empty, error)
}

type Signoff interface {
	Roles(ctx context.Context, scope Scope, p bridge.SignoffChainRef) (bridge.SignoffRoles, error)
	SetRoles(ctx context.Context, scope Scope, p bridge.SignoffRoles) (bridge.SignoffRoles, error)
	State(ctx context.Context, scope Scope, p bridge.SignoffRecordRef) (bridge.SignoffState, error)
	Sign(ctx context.Context, scope Scope, p bridge.SignoffSignParams) (bridge.SignoffState, error)
}
```

Move to workspace is a method on the personal sidecar implemented in
`internal/apps/move` (section 6); it uses Store, Records and AppRoles and adds
no interface.

### Audit entry (bridge/proto_spaces.go)

```go
type AuditEntry struct {
	Seq      int64           `json:"seq"`       // assigned by Append, increasing
	At       time.Time       `json:"at"`        // assigned by Append
	Space    string          `json:"space"`
	App      string          `json:"app,omitempty"`
	Member   string          `json:"member"`    // who; "builder" or "system" for codeaf
	Agent    bool            `json:"agent,omitempty"`
	Action   string          `json:"action"`    // method or verb: "records.update", "app.apply", "role.set", "member.remove", "approval.decide", "trigger.pause"
	Object   string          `json:"object,omitempty"` // "deal/d_12", "member/raj", "v12"
	Decision string          `json:"decision"`  // allow | deny
	Detail   json.RawMessage `json:"detail,omitempty"`
	Activity *ActivityLine   `json:"activity,omitempty"` // set = also an Activity line
}

type ActivityLine struct {
	Text string      `json:"text"`           // "Mona shaped CRM · v12"
	Undo *UndoAction `json:"undo,omitempty"` // shown as "Undo"
	View string      `json:"view,omitempty"` // open target, e.g. "versions/v12"
}

type UndoAction struct {
	Method string          `json:"method"`
	App    string          `json:"app,omitempty"`
	Params json.RawMessage `json:"params"`
}
```

Rules: the table is append-only (no update, no delete API exists; a SQL
trigger rejects both). Undo is a new call, which writes its own entry. Every
track appends through `all.Audit.Append` and never writes its own log. Caps
denials are appended by the caps hook (`caps/spaces_audit.go`, one call line
in `caps/audit.go` replacing the jsonl write). Activity is the projection of
entries with `Activity != nil` that the caller's app role may see.

## 2 · Wire methods (bridge/proto_spaces.go, gateway/spaces.go)

Registered by **one line** in `gateway`'s method table init:
`registerSpaces(methods)`. `app` ✓ = app-scoped.

| Method | app | Who | params → result |
| --- | --- | --- | --- |
| `space.info` | — | any | `{}` → `SpaceInfo {id, name, kind: personal\|workspace, address, me: Member, apps: n}` |
| `spaces.list` | — | personal | `{}` → `{spaces: [SpaceEntry {id, name, address, role, apps, waiting: bool, order}]}` (Personal first) |
| `spaces.join` | — | personal | `{link}` → `SpaceJoinResult {space: SpaceEntry, invite {by, role, server, expires}, signin: {kind: google\|github\|email, url?, email_hint?}, state: needs_signin\|joined\|expired}` |
| `spaces.signin` | — | personal | `{space, code?}` → `SpaceJoinResult` (email code, or completes an OAuth return) |
| `spaces.connect` | — | personal | `{space}` → `SpaceConnection {url, token, expires}` (desk opens the bridge) |
| `spaces.new` | — | personal | `{name, address, setup_code}` → `SpaceJoinResult` (you become Owner) |
| `spaces.forget` | — | personal | `{space}` → `{}` (Leave already done server-side) |
| `members.list` | — | any | `{}` → `{members: [Member], invites: [Invite]}` |
| `members.invite` | — | owner/admin | `{email?, role}` → `Invite {id, email?, role, link, by, expires, state}` |
| `members.invite.cancel` | — | owner/admin | `{invite}` → `{}` |
| `members.invite.resend` | — | owner/admin | `{invite}` → `Invite` |
| `members.role` | — | owner/admin | `{member, role}` → `Member` (only Owner makes/unmakes Owner) |
| `members.remove` | — | owner/admin | `{member}` → `{}` |
| `members.leave` | — | any | `{}` → `{}` (last Owner: `conflict`) |
| `approles.list` | ✓ | any | `{}` → `AppMembers {everyone: Role, people: [AppMember {member, role, from: everyone\|set, set_by?, at?, maker?}]}` |
| `approles.set` | ✓ | editor, owner/admin | `{member \| "*", role \| null}` → `AppMembers` (null removes the row) |
| `approles.ask` | ✓ | user/viewer | `{text, kind: shape\|role}` → `{}` (Inbox line to the last shaper) |
| `signoff.roles` | ✓ | any | `{chain}` → `SignoffRoles {chain, steps: [{id, name, people: [member]}], warnings: [string]}` |
| `signoff.roles.set` | ✓ | editor | `SignoffRoles` → `SignoffRoles` |
| `signoff.state` | ✓ | any | `{chain, type, id}` → `SignoffState {chain, steps: [{id, name, state: waiting\|signed\|rejected\|stuck, by?, at?, eligible: [member]}], done}` |
| `signoff.sign` | ✓ | signatory | `{chain, type, id, step, answer: yes\|no, note?}` → `SignoffState` |
| `audit.list` | — | owner/admin | `{app?, member?, action?, before?, limit?}` → `{entries, next?}` |
| `activity.list` | — | any | `{app?, before?, limit?}` → `{lines: [ActivityEntry {seq, at, app?, member, text, undo?, view?}], next?}` |
| `apps.move` | ✓ | personal | `{to: space, editors: [member]}` → `AppSummary` (in the workspace) |

Roles: a method listed for owner/admin returns `denied` for members; the role
check lives in `caps/spaces.go` (R4 hook, one call line in `caps/checker.go`).
App-role checks for existing methods stay in caps: Viewer reads only, User
works with records, Editor shapes. Workspace Admin/Owner may call
`approles.*` on any app (fix "no Editor left").

### Topics (gateway/spaces.go registers the `space/` prefix)

| Topic | Event |
| --- | --- |
| `space/members` | `MembersListResult` on every change |
| `space/activity` | `ActivityEntry` (filtered per subscriber's app roles) |
| `space/waiting` | `{count}` — what needs the member here (drives the switcher's amber dot) |
| `app/<id>/roles` | `AppMembers` on change; the subscriber's own new role is applied at once |
| `app/<id>/signoff` | `SignoffState` on every step change |

## 3 · Workspace server mode (internal/apps/space)

- Start: `codeaf app serve --workspace --name acme --listen :8443 --home DIR
  [--public-url https://acme.codeaf.dev] [--signin email|google|github]`. Not
  `codeaf serve` (taken by the relay). One flag branch in
  `cmd/codeaf/app_serve.go`; the code is `cmd/codeaf/app_serve_workspace.go`.
- First start prints a one-time **setup code**; `spaces.new` with that code
  makes the caller Owner. A workspace has exactly one Owner.
- Workspace mode does not use the appapi loopback guard. `space.Mux` serves:
  `POST /app/v1/space/join {invite} → {signin}`,
  `POST /app/v1/space/signin/email {invite?, email} → {}`,
  `POST /app/v1/space/signin/code {email, code} → {token, member}`,
  `GET /app/v1/space/signin/oauth/{provider}` + callback → `{token, member}`,
  and the bridge at `bridge.Path`, authenticated by `Members.Authenticate`
  on the `bearer.<token>` subprotocol. Origin rule: the Tauri origins of the
  existing guard. TLS terminates in front (documented, not built).
- Sessions: random 32-byte token, stored hashed, 30 days, revoked on Remove
  and Leave (open sockets closed). Invites: once, 7 days, link
  `codeaf://join/<host>/<invite-id>.<secret>`; used or expired →
  `"This invite has expired · Ask @<by> for a new one"`.
- Email codes go through a `Mailer` interface; v1 ships `log` (prints to the
  server log) and `smtp`. Google and GitHub OAuth behind a `Provider`
  interface, configured by client id/secret env vars.
- Storage: `$HOME/space.db` (SQLite): members, invites, sessions, app roles,
  signoff roles, audit. One file per space.

### Personal sidecar as the desktop's keyring

The personal sidecar keeps the joined spaces (`spaces.*` methods) and their
member tokens in the OS keychain (`internal/apps/space/keyring.go`, Secret
Service / macOS Keychain via the existing credential code). The desk asks
`spaces.connect` and opens one bridge per space with the returned
`{url, token}`. Tokens never go to disk in the desk.

## 4 · Desk

| What | Where |
| --- | --- |
| Types | `src/lib/types/apps-spaces.ts` (mirror of `proto_spaces.go`) |
| Mock | `src/lib/apps/bridge/mock-spaces.ts`: Personal · acme (Owner) · globex (Member), the 06 board people, registered with one line in the mock bridge |
| Connections | `src/lib/apps/spaces/` — `useSpaces()`, `useSpace()`, one bridge client per space via the existing `acquire(origin, token)` |
| Switcher | `src/features/space-switcher/` — replaces the unmounted `PersonalSwitcher`; mounted with one line in `src/app/v2-frame.tsx` as the rail's first row; ⌃⌘0 menu, ⌃⌘1–9 jump, amber dot, rail + tab strip swap per space (tabs of the space you left are kept) |
| Join · New workspace | `src/features/space-join/` — the 3-step join sheet, `codeaf://join/…` deep link, New workspace… sheet |
| Workspace settings → Members | `src/features/workspace-members/` — Members list, invite row, role chip menu, Remove…, Leave… (last-Owner rule) |
| App Members | `src/features/app-members/` — People · Sign-off roles tabs; App ⌄ item via the crews registry (`menu-items/spaces.ts`, R2) |
| Role gating in an app | `src/features/app-members/role-gate.tsx` — "Only editors can shape CRM" popover + Ask; Viewer "read only" tag; exported hook `useAppRole(app)` in `src/lib/apps/spaces/` for other tracks |
| Activity | `src/features/space-activity/` — Activity lines with View · Undo |
| Move to workspace | `src/features/app-move/` — the Move sheet; App ⌄ item via `menu-items/spaces.ts` |

Words and layouts are exactly the 06 and 12 boards.

## 5 · Kit (R3)

`SignOff` (data part) is added to `kit.json` by this track: `bind {record}`,
`props {chain: string required}`, `events {signed}`, interactive, label
required. Desk renders it in `src/kit/data/sign-off.tsx` (new file). Check's
`kit/capability` accepts `signoff.*` when `app.json` declares the chain.

## 6 · app.json additions (schema by one `$ref` line)

```json
"signoff": {
  "refund_over_500": {
    "label": "Refund over $500",
    "for": "refund",
    "when": "amount > 500",
    "steps": [ {"id": "maker", "name": "Maker"},
               {"id": "checker", "name": "Checker"},
               {"id": "quality", "name": "Quality lead"} ]
  }
}
```

The Builder writes chains and step names; Editors assign people (only members
with app role User or Editor). "Maker can never check own work" is fixed:
`created_by` of the record and any earlier signer are never eligible. A step
with nobody eligible → warning in `signoff.roles`, and a stuck record opens an
Inbox item for the app's Editors: `"Refund #2041 has no Quality lead who
didn't make it"`. A record under a chain is frozen for writes until done.

Move to workspace (`internal/apps/move`): personal sidecar exports the
definition at the current version plus `records.db` and posts them to the
workspace's `POST /app/v1/space/import` as the member; the workspace makes it
the app's next version, seeds AppRoles (`editors` → Editor, Everyone → User),
the budget becomes the workspace's, personal connectors need Allow again.
One way; the personal copy is removed after the workspace confirms.

## 7 · Package ownership (spaces track)

| Lane | Owns |
| --- | --- |
| c0 | this file, `bridge/proto_spaces.go`, `svc/svc_spaces.go`, `gateway/spaces.go`, `server_spaces.go`, the stubs in each package below, the registration lines |
| c0d | desk `src/lib/types/apps-spaces.ts`, `src/lib/apps/bridge/mock-spaces.ts` |
| b1 | `internal/apps/space/` (Spaces, Members, server mode, sign-in, keyring), `cmd/codeaf/app_serve_workspace.go` |
| b2 | `internal/apps/approles/`, `caps/spaces.go` |
| b3 | `internal/apps/audit/`, `caps/spaces_audit.go` |
| b4 | `internal/apps/signoff/`, schema `signoff` fragment, kit `SignOff` entry, check rule file `check/spaces.go` |
| b5 | `internal/apps/move/` |
| d1 | `src/lib/apps/spaces/`, `src/features/space-switcher/` |
| d2 | `src/features/space-join/`, `src/features/workspace-members/` |
| d3 | `src/features/app-members/`, `src/kit/data/sign-off.tsx` |
| d4 | `src/features/space-activity/`, `src/features/app-move/` |

Other tracks: append with `all.Audit.Append`, read roles from `scope.Caller`,
call `all.AppRoles.Seed` when an app gets a maker (shape: first Apply; crews:
Make my copy). Ask `codeaf-w2-spaces` before changing anything above.
