# Findings

## Task

Implement item 2: carry the six run-steering verbs through the remote protocol, client, server dispatch, and call classifications, with failing-first scripted tests proving a store refusal sentence is returned byte-for-byte without transport wrapping.

## Known at start

Hosted conversations expose `*main.countingAgent`, which embeds `*remote.Agent`; therefore the remote client must implement all six steering methods for the live surface to see them. `PlanSpend` is the repository's worked wire/client/server/classification example. The implementation must not modify the forbidden engine, store, task-belt, task, task-tool, check, audit, or TUI paths (except a necessary compile fix).
