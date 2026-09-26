//go:build !windows

// Package steploop drives one turn of the agent. Run loads the persisted
// transcript, issues one model request per step, settles the tool calls the
// model made, and repeats until the model stops calling tools, the step cap is
// reached, or the processor asks to stop. When the transcript records a
// pending compaction, the step is handed to the TaskController instead.
//
// Seams:
//
//   - Store is the message persistence service. Messages must return fresh,
//     chronological values; the loop rebuilds its view from them every step.
//   - LLMClient/PartStream are the narrow model-client seam; scripted tests
//     return an in-memory stream.
//   - ToolExecutor executes one already-resolved tool call. Tool discovery,
//     permission checks and the concrete tools stay outside this package and
//     arrive in RunOptions.Tools.
//   - ModelResolver names the model for the latest user message.
//   - TaskController owns compaction: overflow detection, boundary creation,
//     processing and pruning.
//   - The clock and the ascending ID factories are package seams with
//     SetNowForTesting and SetIDFactoryForTesting restore closures.
//
// Behaviour worth knowing:
//
//   - The natural exit is: a non-empty finish other than "tool-calls", no
//     non-provider-executed tool part on the matching persisted assistant, and
//     the last user message older than the last assistant.
//   - Tool cleanup snapshots all outstanding calls, waits for each with an
//     independent short timeout, then force-writes every survivor as
//     "Tool execution aborted".
package steploop
