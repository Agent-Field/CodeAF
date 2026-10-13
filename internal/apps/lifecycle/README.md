# App lifecycle

`New` binds the `Wrap` decorator already installed by `wireCrews`, purges expired
entries synchronously, and starts daily purges when `Config.Ctx` is set. Tests
supply `NewWithClock`; cancellation stops the daily loop. `Purge` is also a
synchronous maintenance door and returns cleanup errors for retry.

`lifecycle.json` is an atomic tombstone beside the definition and records. Archive
and delete leave the underlying Store intact so Restore can recover the same
files, versions, and records. Deleted definitions are inaccessible through the
wrapped Store. Archived Get and Definition calls pass through unchanged. The
Definition result and error are preserved even when the Store's detail lookup
fails. A successful detail lookup reporting deleted still hides the definition.
The lifecycle listing and action results carry their archived state. At 30 days
Restore refuses, the lifecycle listing drops the entry,
and the next purge calls the underlying `Store.Discard` and removes the app folder.
A failed discard retains the tombstone for the next purge.

Copy allocates through Store, clones current definition files with a new Personal
identity, generates and seeds only sample records, enters Preview, and calls Keep.
No source records, settings overrides, connections, lifecycle state, or version
history are copied. Its definition starts with its own budget value. Failed copies
are discarded. Keep provides the new app's kept v1 and empty real database.

## Integration handoff

This lane owns only `internal/apps/lifecycle`. The remaining wiring belongs to the
seams/settings lanes:

- The settings gate can consult
  `all.Crews.Lifecycle.(interface { Archived(string) bool }).Archived(app)`.
  Alternatively, change its crews wiring registration to
  `TriggerGate: lifecycle.Gate(all, preferences)`. That gate consults lifecycle
  through `svc`, blocks archived and deleted apps, and then calls the settings
  gate. No runs file needs another change. Lifecycle checks run before settings
  records a trigger fire; existing runs are untouched.
- The current `svc.Caller` exposes app roles only. As R6 requires, Personal grants
  every role; a workspace Editor may archive, but deletion and deleted restore
  remain denied until the spaces lane supplies a workspace Owner/Admin resolver.
  An app Editor is never treated as a workspace Admin.
- `store/stub.go` still returns `not_built` for Create, WriteFiles, SetState, Keep,
  and Discard. Copy and final purge use those doors and need the B1 implementation.
  Unit tests use a Store double and the real Sample and Records implementations.
- `gateway/crews_test.go` still expects every lifecycle call to return `not_built`
  and issues a sequence of mutations against the same app. Its lifecycle assertions
  must be replaced with real behavior assertions, with independent fixtures where
  subsequent tests require a live app. The restore-routing test must inject a
  lifecycle spy rather than depend on a stub lifecycle implementation.

The board HTML files named in the task are absent from this checkout; the full
text copies of boards 01 and 12 are the available owner specification.
