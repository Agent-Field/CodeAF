// Package plannertranslate ports src/session/planner-translate.ts:1-896 from
// swe-pro commit 3b25a1a. It translates architecture manifests into PlanDB
// DAGs, dispatches the structured planner agent, and applies validated tasks.
package plannertranslate

import (
	"bytes"
	"encoding/json"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/msgmodel"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// DAGDep is one direct dependency edge. Field order matches the canonical
// object produced by normalizeRawDAG.
type DAGDep struct {
	FromTask string `json:"from_task"`
	Kind     string `json:"kind"`
}

// DAGTask is one planner-generated PlanDB leaf.
type DAGTask struct {
	TaskKey     string             `json:"taskKey"`
	Title       string             `json:"title"`
	Kind        string             `json:"kind"`
	Description string             `json:"description"`
	Tags        []string           `json:"tags"`
	Priority    *jscompat.JSNumber `json:"priority,omitempty"`
	Deps        []DAGDep           `json:"deps"`
}

// DAGContext is the optional frontier context attached to a translated plan.
type DAGContext struct {
	Kind    string  `json:"kind"`
	Content string  `json:"content"`
	TaskKey *string `json:"taskKey,omitempty"`
}

// DAGData is the validated planner output. Residual remains raw because the TS
// schema accepts either a string or {content:string}.
type DAGData struct {
	Summary  *string         `json:"summary,omitempty"`
	Tasks    []DAGTask       `json:"tasks"`
	Contexts []DAGContext    `json:"contexts,omitempty"`
	Residual json.RawMessage `json:"residual,omitempty"`
}

// DAGFallback is returned after agent-json exhausts its repair ladder.
var DAGFallback = DAGData{
	Summary: stringPtr("Fallback: planner-translate did not produce a valid DAG file. No tasks added."),
	Tasks:   []DAGTask{},
}

func stringPtr(value string) *string { return &value }

// NormalizeRawDAG ports normalizeRawDAG at planner-translate.ts:127-159.
//
// json.RawMessage is the preferred boundary: it preserves JavaScript object
// insertion order while replacing dependency variants. Other Go values are
// round-tripped through JSON, which is adequate for typed callers but cannot
// recover insertion order already lost in a map.
func NormalizeRawDAG(raw any) any {
	switch value := raw.(type) {
	case json.RawMessage:
		return normalizeRawDAGJSON(value)
	case []byte:
		return normalizeRawDAGJSON(json.RawMessage(value))
	default:
		encoded, err := jscompat.Stringify(value)
		if err != nil {
			return raw
		}
		return normalizeRawDAGJSON(encoded)
	}
}

func normalizeRawDAGJSON(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	object := msgmodel.RawObject(trimmed)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		object = spreadArrayObject(trimmed)
	} else if !msgmodel.IsRecord(object) {
		return append(json.RawMessage(nil), raw...)
	}
	tasksRaw, ok := object.Field("tasks")
	tasks := []json.RawMessage{}
	if ok {
		_ = json.Unmarshal(tasksRaw, &tasks)
	}
	normalized := make([]json.RawMessage, 0, len(tasks))
	for _, task := range tasks {
		normalized = append(normalized, normalizeTaskJSON(task))
	}
	tasksJSON, _ := marshalRawArray(normalized)
	return msgmodel.SpreadObject(object, msgmodel.RawField{
		Key: "tasks", Value: tasksJSON,
	})
}

func normalizeTaskJSON(raw json.RawMessage) json.RawMessage {
	object := msgmodel.RawObject(bytes.TrimSpace(raw))
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '[' {
		object = spreadArrayObject(trimmed)
	} else if !msgmodel.IsRecord(object) {
		return append(json.RawMessage(nil), raw...)
	}
	depsRaw, ok := object.Field("deps")
	deps := []json.RawMessage{}
	if ok {
		_ = json.Unmarshal(depsRaw, &deps)
	}
	normalized := make([]json.RawMessage, 0, len(deps))
	for _, dep := range deps {
		if value, ok := normalizeDepJSON(dep); ok {
			normalized = append(normalized, value)
		}
	}
	depsJSON, _ := marshalRawArray(normalized)
	return msgmodel.SpreadObject(object, msgmodel.RawField{
		Key: "deps", Value: depsJSON,
	})
}

func normalizeDepJSON(raw json.RawMessage) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	var bare string
	if len(trimmed) > 0 && trimmed[0] == '"' && json.Unmarshal(trimmed, &bare) == nil {
		return canonicalDepJSON(bare, "feeds_into"), true
	}
	object := msgmodel.RawObject(trimmed)
	if !msgmodel.IsRecord(object) {
		return nil, false
	}
	from := ""
	for _, key := range []string{
		"from_task", "taskId", "task", "task_id", "id", "upstream",
	} {
		value, exists := object.Field(key)
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}
		// Nullish coalescing stops at the first non-null value even if its type
		// is wrong; the subsequent typeof guard then drops the dependency.
		if json.Unmarshal(value, &from) != nil || from == "" {
			return nil, false
		}
		break
	}
	if from == "" {
		return nil, false
	}
	kind := "feeds_into"
	if value, exists := object.Field("kind"); exists {
		var candidate string
		if json.Unmarshal(value, &candidate) == nil &&
			(candidate == "feeds_into" || candidate == "blocks" || candidate == "suggests") {
			kind = candidate
		}
	}
	return canonicalDepJSON(from, kind), true
}

func canonicalDepJSON(from, kind string) json.RawMessage {
	value := DAGDep{FromTask: from, Kind: kind}
	encoded, _ := jscompat.Stringify(value)
	return encoded
}

func marshalRawArray(values []json.RawMessage) (json.RawMessage, error) {
	if values == nil {
		values = []json.RawMessage{}
	}
	return json.Marshal(values)
}

func spreadArrayObject(raw json.RawMessage) msgmodel.RawObject {
	var elements []json.RawMessage
	if json.Unmarshal(raw, &elements) != nil {
		return msgmodel.RawObject("{}")
	}
	object := msgmodel.RawObject("{}")
	for index, element := range elements {
		object = msgmodel.RawObject(msgmodel.SpreadObject(object, msgmodel.RawField{
			Key: itoa(index), Value: element,
		}))
	}
	return object
}
