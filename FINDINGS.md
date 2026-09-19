# Findings

## Task

Implement item 3: carry PlanRunSummary and RefreshRunSummary across the remote wire, including call classification, round-trip tests, engine-side deadline cancellation, and dropped-link nothing-kept behavior.

## Known facts

- PlanSpend is the existing wire/client/server/call-class model.
- RefreshRunSummary makes a model call and must honor the caller context deadline on the engine side.
- A dropped link must return nothing kept rather than expose a frame-facing transport error.
- Refresh carries the caller deadline explicitly because ordinary protocol calls do not transmit contexts.
- The server derives a context from that transmitted deadline, so cancellation reaches the engine-side model call rather than only releasing the client waiter.
- Forbidden paths will not be changed.
## Verification

- Client and server tests now cover whole summary values, explicit deadline propagation and engine-side cancellation, and dropped-link nothing-kept behavior.
- `make test-laws` is not green on the shared branch: unrelated `cmd/codeaf` kept-nothing tests exit before running, and `internal/tui3` off-loop law reports taskplan calls owned by another cell. The item-3 remote and enginehost tests pass.
