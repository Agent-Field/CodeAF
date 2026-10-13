# Grants

`New(cfg, all)` uses Personal defaults. `NewWithOptions` accepts a trusted
server space identity, workspace roles/members/names, the audit writer, and a
small connections adapter. Spaces seams pending: this branch has neither
`svc_spaces.go` nor the member-gone callback. `seams.go` is the adapter door;
workspace wiring must supply those options and register `ForgetMember` on
member departure. App roles are taken directly from the gateway's scope.

The database is `filepath.Dir(cfg.Root)/connect-apps.db`, WAL with foreign
keys. Grant credentials are encrypted with AES-256-GCM and bound to the
member, app, space and service. Revoke and member departure cascade-delete
those rows. The private encryption key is kept beside the database at 0600.
`internal/connect/key.go` and `keyref.go` on this branch provide keys and
environment references, but no encryption API; `credentials.go` isolates the
local encryption and store adapter until such a shared API is available.

Personal reads prefer the desktop's `config.AppCredentialSource` (Keychain
handover), including explicit removals. Outside desktop mode, the adapter
reads `internal/connect`'s existing credential file. Unknown connection start
dates remain blank. Workspace Resolve and Accounts never read that personal
store: only an encrypted grant credential, delivered by the desktop, can act
as a member. Export is global, Personal-only, and limited to the desktop's own
caller; app and agent scopes are denied in the handler as well as `ExportRule`.
The composition lane must register that rule with Caps.AddRule (R10).

Workspace sign-in uses a separate `internal/connect` store for each generated
connection id, so adding or removing a workspace account never touches a
personal account. The browser URL is published as a `WorkspaceConnection`
with `auth_url` on `connect/workspace` before Add waits for sign-in. Tests use
a fake auth/store and never contact a service. Key-only services need the
owning auth UI's credential-entry adapter through `Options.Connections`;
the built-in browser adapter refuses them without writing a connection.

An AllowSheet on a workspace server cannot inspect the personal sidecar.
The desktop passes non-secret `connected` and `account` metadata to
`connect.allow.sheet`; `missing` then offers Connect first only when the
personal connection is actually absent. Without metadata the sheet leaves
that state unknown (false), and a grant is still required. Metadata cannot
create permission: Allow/refresh must carry a credential. A refresh replaces
only an existing grant and retains its original Allow time.

Allow and not-now notifications on `connect/<member>/allow` are AllowSheets;
Allow/refresh publish after the committed update so the door can retry
Resolve. Revoke does not change the saved app records or other members.
