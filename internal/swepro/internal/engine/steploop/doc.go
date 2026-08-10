// Package steploop is a bug-for-bug port of the outer state machine in
// src/session/prompt.ts:1478-1866 and the tool-settlement slice of
// src/session/processor.ts:141-221,602-660. One Run call drives as many
// one-request OpenRouter turns as the persisted transcript requires.
//
// ── seams ────────────────────────────────────────────────────────────────
//
//   - Store is the MessageV2 persistence service. Messages must return fresh,
//     chronological values: prompt.ts mutates its filtered copy when wrapping
//     late user text and relies on the next load rebuilding those values.
//   - LLMClient/PartStream are the narrow orclient seam. OpenRouterClient is
//     the production adapter; scripted tests can return an in-memory stream.
//   - ToolExecutor executes one already-resolved tool call. Tool discovery,
//     permission checks, and concrete tools deliberately remain outside this
//     package and are supplied in RunOptions.Tools.
//   - Scheduler, TaskController, and ExitGuard are orchestration seams. The
//     latter has a native PlanDB implementation, PlanDBExitGuard.
//   - Date.now(), MessageID.ascending(), and PartID.ascending() are package
//     seams with SetNowForTesting and SetIDFactoryForTesting restore closures.
//
// ── sibling ports ────────────────────────────────────────────────────────
//
//   - internal/engine/orclient supplies one-request normalized stream parts.
//   - internal/engine/msgmodel supplies persisted message/part unions,
//     conversion to provider ModelMessages, and tool-state object spread
//     helpers.
//   - internal/engine/calc supplies OpenRouter usage normalization and cost.
//   - internal/plandb supplies the native task/dependency snapshot read by the
//     exit guard.
//
// ── fidelity notes (deliberate, do not "fix") ───────────────────────────
//
//   - The natural exit predicate is prompt.ts:1612-1620 literally: a nonempty
//     finish other than "tool-calls", no non-provider-executed tool part on
//     the persisted matching assistant, and lastUser.id < lastAssistant.id.
//     It does NOT exclude "unknown"; that exclusion exists only later at
//     prompt.ts:1834 and is dead on the OpenRouter path.
//   - Tool cleanup snapshots all outstanding calls, waits for each concurrently
//     with an independent 250 ms timeout, then force-writes every survivor as
//     "Tool execution aborted". Registry deletion precedes done-channel close.
//   - The running and aborted tool states retain leaked fields from TS object
//     spreads (notably pending.raw) by carrying their exact JSON object bytes.
//   - Exit-guard discovery and last-model lookup are degraded: either failure
//     lets the loop exit. A successful guard nudge continues without
//     incrementing step.
//   - ReminderQueue is bounded to eight and remembers only the last drained
//     string. Run never drains it; DrainIntoUserMessage is the sole drain
//     operation, mirroring prompt.ts:1313 inside createUserMessage.
package steploop
