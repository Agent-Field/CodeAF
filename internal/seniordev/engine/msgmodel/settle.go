//go:build !windows

package msgmodel

import (
	"bytes"
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

// Tool-part assembly for the stream processor's settlement path. Everything
// here is pure: the in-flight tool-call registry belongs to the step loop's
// processor.

// Fixed strings the settlement path writes. ToolAbortedError is replayed to
// the model as the tool's errorText; ToolInterruptedError is what a pending or
// running tool replays as.
const (
	ToolAbortedError     = "Tool execution aborted"
	ToolInterruptedError = "[Tool execution was interrupted]"
	ToolCompactedOutput  = "[Old tool result content cleared]"
)

// PendingToolState is `{status:"pending", input:{}, raw:""}`.
func PendingToolState() ToolStatePending {
	return ToolStatePending{Status: ToolStatusPending, Input: RawObject("{}"), Raw: ""}
}

// CompletedToolState is the typed `completed` state. The processor writes its
// keys in the order status, input, output, metadata, title, time, attachments;
// this struct keeps the declared order (title before metadata).
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

// SpreadAbortedToolState is the cleanup drain's force-write: `status:"error"`,
// `error:"Tool execution aborted"`, `metadata:{...existing, interrupted:true}`
// and `time.start` taken from the previous state, or `now` when the state has
// none (`pending` does not). It is a spread over the previous state,
// `{...state, status, error, metadata, time}`, so a `pending` state's
// `raw` and a `running` state's `title` survive into an object ToolStateError
// does not declare. Returned as raw JSON because those fields have no typed
// home.
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
	timeRaw, err := jsonutil.Marshal(ToolTimeSpan{Start: start, End: now})
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

// SpreadToolState is `{...prev, ...overrides}` for any transition: the
// tool-call → running step as well as the cleanup drain.
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
	raw, err := jsonutil.Marshal(prev)
	if err != nil {
		return nil, err
	}
	return RawObject(raw), nil
}

// MergeInterrupted is `{...metadata, interrupted: true}`. A pre-existing
// `interrupted` key keeps its original position.
func MergeInterrupted(metadata RawObject) RawObject {
	return RawObject(SpreadObject(metadata, RawField{Key: "interrupted", Value: json.RawMessage("true")}))
}

// SpreadObject is the object spread `{...base, k1: v1, k2: v2}`: an
// overridden key keeps the position it had in base and takes the new value; a
// new key is appended in the order given. Key order is load-bearing: the
// doom-loop guard compares the stored bytes verbatim.
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

// IsRecord reports a JSON object: neither null nor an array.
func IsRecord(v RawObject) bool {
	t := bytes.TrimSpace(v)
	return len(t) > 0 && t[0] == '{'
}
