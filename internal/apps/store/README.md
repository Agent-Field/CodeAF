# Personal app store

`New(svc.Config, *svc.Services)` implements `svc.Store`. `Config.Root` is the
space's **apps directory**, not its home directory. An empty registry contains
no fixture apps. Constructors do no I/O and peers are read at call time.

Each allocated slug is both the app id and its folder name. Duplicate names
receive numeric suffixes. The folder contains `state.json` (summary, maker and
original request) and `definition/` (a local go-git repository). The store reserves
`records.db` and `sample.db` by leaving those paths to the records service.

Creation starts in `planning` with an unborn `draft/me` branch. `WriteFiles`
replaces the **complete** definition file set; callers editing only a plan must
merge that file into the current set before calling it. Partial definitions and
failed builds can be saved for Check. Paths are limited to the contract's five
definition file kinds. The registry owns `app.json`'s id, personal space and
version, even when the Builder starts from an example with another id.

`SetState` handles planning, building, failed and preview transitions. `Keep`
requires preview, validates the complete definition, commits on the draft branch,
tags `v1` and selects `main`. All kept reads come from the current tag. Keep never
copies sample records into real records. Each state change publishes an
`AppSummary` through `Services.Hub` on `app/<id>/state`; repeated state setters
publish nothing. `Discard` removes the entire unkept app folder and publishes
the final `discarded` summary.

The service also exports calls required by this lane beyond the current
`svc.Store` seam:

| Call | Behavior |
| --- | --- |
| `GetVersion(ctx, app, version)` | Files from an immutable numbered tag. |
| `Versions(ctx, app)` | Tags newest first, including commit, request, maker and time. |
| `Restore(ctx, app, version)` | A new commit and tag; prior tags and records survive. |
| `Archive(ctx, app)` / `Unarchive(ctx, app)` | Keep files, versions and records; change state. |
| `Delete(ctx, caller, app, confirmation)` | Require the owner's exact app name; mark deleted. |
| `RestoreDeleted(ctx, caller, app)` | Restore a deleted app before its 30-day cutoff. |
| `PurgeDeleted(ctx)` | Remove expired deleted folders during explicit maintenance. |

Archive, discard and delete state constants live in this package until a contract
change exposes those states on the bridge. Deleted apps are hidden from Get and
List; archived apps remain in List with their state so the shell can group them.
The owner is the only maker and recipient of personal apps. There are no
workspace access rules, migration executor or maintenance scheduler here.
Restore changes definition files and their version field; callers adding a
wave-2 UI must arrange Check and migration preview before exposing it.

File writes and version commits are prepared in a sibling directory. Failed
preparation leaves the current definition unchanged, and a metadata write failure
rolls the directory replacement back. Operations serialize within the sidecar's
single store instance. This is not a multi-process registry or a cross-filesystem
transaction. No git executable, network remote or cgo is used.

The walking-skeleton bridge tests in `internal/apps/server_test.go` must provision
their CRM fixture explicitly through the store before listing or calling it.
Automatically installing that fixture into every new personal space would hide
the behavior this service is intended to provide.
