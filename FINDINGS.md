# Findings

## Task

Implement item 2: carry the six run-steering verbs through the remote protocol, client, server dispatch, and call classifications, with failing-first scripted tests proving a store refusal sentence is returned byte-for-byte without transport wrapping.

## Known at start

Hosted conversations expose `*main.countingAgent`, which embeds `*remote.Agent`; therefore the remote client must implement all six steering methods for the live surface to see them. `PlanSpend` is the repository's worked wire/client/server/classification example. The implementation must not modify the forbidden engine, store, task-belt, task, task-tool, check, audit, or TUI paths (except a necessary compile fix).

## Item 2 design

Each steering verb gets its own method constant and typed argument payload. Client methods return the remote call error directly so a server/store refusal sentence survives unchanged. Server dispatch asserts the optional steering method and returns its error directly; all six are classified as acts because they mutate the run plan without opening a stream.

## Item 2 result

The six steering verbs now cross through typed wire payloads, client methods, optional server dispatch, and the act call class. Focused scripted client and server tests pass and prove the exact store refusal `task "done-one" is already terminal` returns unchanged.

## Verification

`gofmt -l ./cmd ./internal`, `go build ./...`, `go vet ./internal/remote ./internal/enginehost`, and `go test ./internal/remote/ ./internal/enginehost/` pass. `make test-laws` reaches the broader suite but is currently blocked by unrelated headless default-model failures in `cmd/codeaf` and the pre-existing TUI off-loop law now seeing plan calls owned by another item; item 2's remote tests pass both focused and in the full remote package.
