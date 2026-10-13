# App connector door

Every call passes the app capability check. A scope whose data is anything
other than `real` uses a registered fake, records a `FakeCall`, and publishes
on `app/<id>/fakes`. Send, post, reply, pay, charge and external create require
the run's send permit, including in previews. MCP tools use their read-only
annotation; unknown tools keep the approval floor until their metadata is read.

Real calls resolve a connection through Grants before selecting an adapter.
Native verbs are Gmail search/read/send, Calendar list/create, and Slack
search/read/list/send/post/reply. Unsupported verbs return `invalid`; Gmail
`draft` has a fake but no native client API. GitHub, Stripe, Airtable and HubSpot
use the configured MCP server's own tool names, or return `invalid` with
`no adapter` when no server exists.

## Credential stores and Grants integration

`ManagerProvider.ConnectorManager(ctx, credential)` lets Grants hand the door
an `internal/connect.Manager` bound to the resolved connection's encrypted
store. The credential descriptor is passed intact. Workspace connections and
other members' accounts require this provider: they never fall back to the
server owner's tokens. The personal owner uses the existing profile or desktop
credential source when no provider is installed.

After Allow or reconnect, Grants can call `Resume(service, member)` on the
door. The lifetime sweep also resolves outstanding grants and wakes recovered
connections. The Allow sheet comes from Grants' optional `AllowSheet` query;
the door publishes it when it first records a `needs_allow` wait.

## Connection waits

`conflict` with `needs_allow` or `expired`, and native expiry responses, produce
a typed `WaitError`. Waits live in `$CODEAF_HOME/connect-waits.db`, beside the
configured apps root, and publish on `connect/<member>/waits` on open and clear.
`connect.waits` lists the caller's waits; `connect.fakes` lists fake calls for
one app, optionally filtered by run. Workspace Admin visibility awaits the
workspace role seam; the current Caller exposes only the app role.

The runs hook journals a `needs_you` step, preserves an already approved send,
and resumes that exact step after reconnect. Seven days from the original
wait, it stops with `stopped · Gmail expired` (using the service's name).
Repeated expiry does not restart the deadline. `NewWithClock` and `Sweep`
make this path deterministic in tests. The background sweep stops with the
server's context; callers without a lifetime context close the door explicitly.

## Capability policy handoff

The gateway's activity methods now dispatch to this door. The wave-1 Caps
checker still needs policies for the global `connect.waits` query, app-scoped
`connect.fakes`, and connect/fakes subscriptions. That work belongs to the
capability owner; the door keeps the checker in place for every connector call.
