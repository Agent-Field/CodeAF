package msgmodel

import (
	"bytes"
	"encoding/json"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// Tool-part assembly for the stream processor's settlement path
// (ENGINE-DESIGN §2.4 / processor.ts:141-221, :634-656). Everything here is
// pure: the in-flight `ctx.toolcalls` registry and its Deferreds belong to
// engine/processor.

// Fixed strings the settlement path writes. They are behavioural contracts:
// `ToolAbortedError` is what message-v2.ts:941 replays as `errorText`, and
// `ToolInterruptedError` is what the pending/running replay emits (:953).
const (
	ToolAbortedError     = "Tool execution aborted"
	ToolInterruptedError = "[Tool execution was interrupted]"
	ToolCompactedOutput  = "[Old tool result content cleared]"
)

// PendingToolState is `{status:"pending", input:{}, raw:""}`
// (processor.ts:298).
func PendingToolState() ToolStatePending {
	return ToolStatePending{Status: ToolStatusPending, Input: RawObject("{}"), Raw: ""}
}

// RunningToolState is the schema-clean `running` state
// (message-v2.ts:296-307). NOTE: processor.ts:342-347 does not build it this
// way — it SPREADS the previous state, so a `pending` state's `raw` survives
// into the running one. Use SpreadToolState for that byte-level fidelity.
func RunningToolState(input RawObject, start uint64) ToolStateRunning {
	return ToolStateRunning{Status: ToolStatusRunning, Input: input, Time: ToolTimeStart{Start: start}}
}

// CompletedToolState is processor.ts:191-199. The TS writes the keys in the
// order status, input, output, metadata, title, time, attachments — which is
// NOT the schema order (title before metadata, :309-324). The struct keeps the
// schema order; see the fidelity note in the package comment.
func CompletedToolState(input RawObject, output, title string, metadata RawObject, start, end uint64, attachments *[]FilePart) ToolStateCompleted {
	return ToolStateCompleted{
		Status:      ToolStatusCompleted,
		Input:       input,
		Output:      output,
		Title:       title,
		Metadata:    metadata,
		Time:        ToolTimeCompleted{Start: start, End: end},
		Attachments: attachments,
	}
}

// ErrorToolState is processor.ts:211-216 — note it writes NO `metadata` key at
// all, so a running state's metadata is dropped on failure.
func ErrorToolState(input RawObject, message string, start, end uint64) ToolStateError {
	return ToolStateError{
		Status: ToolStatusError,
		Input:  input,
		Error:  message,
		Time:   ToolTimeSpan{Start: start, End: end},
	}
}

// AbortedToolState is the cleanup drain's force-write (processor.ts:645-655),
// in its schema-clean form: `status:"error"`, `error:"Tool execution aborted"`,
// `metadata:{...existing, interrupted:true}`, and `time.start` taken from the
// previous state or `now` when the state has none — which `pending` genuinely
// does not (message-v2.ts:287-294), so that fallback is reachable.
func AbortedToolState(prev ToolState, now uint64) ToolStateError {
	start := now
	if s, ok := prev.StartTime(); ok {
		start = s
	}
	var existing RawObject
	if prev != nil && IsRecord(prev.ToolMetadata()) {
		existing = prev.ToolMetadata()
	}
	var input RawObject
	if prev != nil {
		input = prev.ToolInput()
	}
	return ToolStateError{
		Status:   ToolStatusError,
		Input:    input,
		Error:    ToolAbortedError,
		Metadata: MergeInterrupted(existing),
		Time:     ToolTimeSpan{Start: start, End: now},
	}
}

// SpreadAbortedToolState is the same force-write kept BYTE-faithful: TS builds
// it as `{...part.state, status, error, metadata, time}`, so a `pending`
// state's `raw` and a `running` state's `title` survive into an object the
// ToolStateError schema never declares. Returned as raw JSON because the leak
// has no typed home. [BUG-CANDIDATE for BUGS-KEPT: processor.ts:646.]
func SpreadAbortedToolState(prev ToolState, now uint64) (json.RawMessage, error) {
	start := now
	if s, ok := prev.StartTime(); ok {
		start = s
	}
	var existing RawObject
	if prev != nil && IsRecord(prev.ToolMetadata()) {
		existing = prev.ToolMetadata()
	}
	base, err := stateObject(prev)
	if err != nil {
		return nil, err
	}
	timeRaw, err := jscompat.Stringify(ToolTimeSpan{Start: start, End: now})
	if err != nil {
		return nil, err
	}
	return SpreadObject(base,
		RawField{Key: "status", Value: jsonString(ToolStatusError)},
		RawField{Key: "error", Value: jsonString(ToolAbortedError)},
		RawField{Key: "metadata", Value: json.RawMessage(MergeInterrupted(existing))},
		RawField{Key: "time", Value: timeRaw},
	), nil
}

// SpreadToolState is `{...prev, ...overrides}` for any transition — used by
// processor.ts:342-347 (`tool-call` → running) as well as the cleanup drain.
func SpreadToolState(prev ToolState, overrides ...RawField) (json.RawMessage, error) {
	base, err := stateObject(prev)
	if err != nil {
		return nil, err
	}
	return SpreadObject(base, overrides...), nil
}

func stateObject(prev ToolState) (RawObject, error) {
	if prev == nil {
		return nil, nil
	}
	raw, err := jscompat.Stringify(prev)
	if err != nil {
		return nil, err
	}
	return RawObject(raw), nil
}

// MergeInterrupted is `{...metadata, interrupted: true}` (processor.ts:652).
// A pre-existing `interrupted` key keeps its ORIGINAL position, as JS object
// literals do.
func MergeInterrupted(metadata RawObject) RawObject {
	return RawObject(SpreadObject(metadata, RawField{Key: "interrupted", Value: json.RawMessage("true")}))
}

// SpreadObject reproduces a JS object literal `{...base, k1: v1, k2: v2}`:
// an overridden key keeps the POSITION it had in base and takes the new value;
// a new key is appended in the order given. Key order is load-bearing — it
// reaches JSON.stringify, and the doom-loop guard is a stringify comparison.
func SpreadObject(base RawObject, overrides ...RawField) json.RawMessage {
	fields := append([]RawField(nil), base.Fields()...)
	for _, o := range overrides {
		fields = upsertField(fields, o)
	}
	if len(fields) == 0 {
		return json.RawMessage("{}")
	}
	return json.RawMessage(encodeFields(fields))
}

// IsRecord is src/util/record.ts:1 — an object that is neither null nor an
// array.
func IsRecord(v RawObject) bool {
	t := bytes.TrimSpace(v)
	return len(t) > 0 && t[0] == '{'
}
