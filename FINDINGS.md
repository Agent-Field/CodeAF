# Findings

## Task

Implement item 3: carry PlanRunSummary and RefreshRunSummary across the remote wire, including call classification, round-trip tests, engine-side deadline cancellation, and dropped-link nothing-kept behavior.

## Known facts

- PlanSpend is the existing wire/client/server/call-class model.
- RefreshRunSummary makes a model call and must honor the caller context deadline on the engine side.
- A dropped link must return nothing kept rather than expose a frame-facing transport error.
- Refresh carries the caller deadline explicitly because ordinary protocol calls do not transmit contexts.
- Forbidden paths will not be changed.
