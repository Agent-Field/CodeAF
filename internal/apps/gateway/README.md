# Apps bridge

`codeaf app serve --home <dir>` mounts `GET /app/v1/apps/ws` through the
existing app API guard. Use the hello's launch token in `bearer.<token>`
and offer `codeaf.apps.v1`; native clients can use the bearer header instead.
The server selects only `codeaf.apps.v1`, never the credential subprotocol.
The contract specifies no additional Apps HTTP resource routes; the sidecar's
existing health and shutdown routes remain available.

Calls, subscriptions and unsubscribe receipts use request ids. Every valid
call, subscription and unsubscribe goes through `svc.Caps.Allow` before
service dispatch. The server supplies member `me`, role `editor`, and uses
sample records until the app is kept. It ignores client claims about caller
or data mode. Missing record capabilities deny access even to the owner.

`caps.Needs(*spec.App)` computes Needs for Builder plan results; `apps.get`
computes it directly from the returned `app.json`. Decisions append to
`<apps root>/audit.jsonl` with timestamp, app, member, method and decision.
Audit lines contain no tokens, prompts, arguments or record values. Append
failures are logged to the sidecar's logger with the decision metadata.

The hub accepts exact topics and trailing `/*` prefixes. The checker allows
only contract topics, checks record reads, and denies a broad wildcard if
any record type is unreadable. Duplicate subscriptions share one delivery.
Disconnect and unsubscribe cancel their hub subscriptions.

A hub subscriber buffers 256 events. Overflow discards its pending events,
delivers a terminal `svc.Event` whose `Data` is a `*bridge.Error` with code
`conflict`, and closes the subscription. In-process subscribers must treat
that error as an incomplete stream and re-query. The socket sends the typed
error and disconnects so the client resubscribes and re-queries. The socket
also bounds outgoing messages to 64, active calls to 32 and subscriptions to
128, with a two-second write deadline. A blocked network can prevent the
terminal frame from reaching the client; disconnection still forces recovery.

Integration documentation needs the same endpoint, authentication, data-mode,
audit and reconnection behavior added to
`internal/manual/chat/running-from-the-terminal.md` by the integration owner;
that shared corpus is outside this lane's package ownership.
