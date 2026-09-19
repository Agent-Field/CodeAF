# Findings

## Task

Implement item 2: carry the six run-steering verbs through the remote protocol, client, server dispatch, and call classifications, with failing-first scripted tests proving a store refusal sentence is returned byte-for-byte without transport wrapping.

## Known at start

Hosted conversations expose `*main.countingAgent`, which embeds `*remote.Agent`; therefore the remote client must implement all six steering methods for the live surface to see them. `PlanSpend` is the repository's worked wire/client/server/classification example. The implementation must not modify the forbidden engine, store, task-belt, task, task-tool, check, audit, or TUI paths (except a necessary compile fix).

## Item 2 design

Each steering verb gets its own method constant and typed argument payload. Client methods return the remote call error directly so a server/store refusal sentence survives unchanged. Server dispatch asserts the optional steering method and returns its error directly; all six are classified as acts because they mutate the run plan without opening a stream.

## Item 2 result

The six steering verbs now cross through typed wire payloads, client methods, optional server dispatch, and the act call class. Focused scripted client and server tests pass and prove the exact store refusal `task "done-one" is already terminal` returns unchanged.
