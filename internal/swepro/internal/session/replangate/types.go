// Package replangate ports src/session/replan-gate.ts:1-495 from swe-pro
// commit 3b25a1a. It snapshots stalled PlanDB state, dispatches a structured
// replanner through agentjson, applies the chosen operation, and persists the
// replan budget history.
package replangate

import (
	"encoding/json"
	"strconv"
	"unicode/utf16"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/agentjson"
)

// ReplanOp is one member of ReplanOpSchema.
type ReplanOp interface {
	replanOp()
	OpName() string
}

type AddOp struct {
	Op     string   `json:"op"`
	Title  string   `json:"title"`
	Kind   string   `json:"kind"`
	Deps   []string `json:"deps"`
	Reason string   `json:"reason"`
}

func (AddOp) replanOp()                {}
func (operation AddOp) OpName() string { return operation.Op }

type CancelOp struct {
	Op     string `json:"op"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

func (CancelOp) replanOp()                {}
func (operation CancelOp) OpName() string { return operation.Op }

type AmendOp struct {
	Op      string `json:"op"`
	ID      string `json:"id"`
	Prepend string `json:"prepend"`
}

func (AmendOp) replanOp()                {}
func (operation AmendOp) OpName() string { return operation.Op }

type SplitPart struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	FileScope   []string `json:"file_scope"`
}

type SplitOp struct {
	Op     string      `json:"op"`
	ID     string      `json:"id"`
	Parts  []SplitPart `json:"parts"`
	Reason string      `json:"reason"`
}

func (SplitOp) replanOp()                {}
func (operation SplitOp) OpName() string { return operation.Op }

// ReplanDecision is the strict structured result.
type ReplanDecision struct {
	Action       string     `json:"action"`
	Reason       string     `json:"reason"`
	Ops          []ReplanOp `json:"ops"`
	DropIDs      []string   `json:"drop_ids"`
	AbortSummary *string    `json:"abort_summary"`
}

// ReplanFallback is the lowest-risk crash fallback.
var ReplanFallback = ReplanDecision{
	Action: "continue",
	Reason: "Replanner produced no parseable JSON after retries. Defaulting to continue " +
		"per the crash-fallback rule (do not abort on orchestration errors).",
	Ops: nil, DropIDs: nil, AbortSummary: nil,
}

// DecisionSchema implements ReplanDecisionSchema.
type DecisionSchema struct{}

func (DecisionSchema) SafeParse(raw json.RawMessage) agentjson.Validation[ReplanDecision] {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || len(object) != 5 {
		return invalidDecision(nil, "Expected strict object")
	}
	for _, key := range []string{"action", "reason", "ops", "drop_ids", "abort_summary"} {
		if _, ok := object[key]; !ok {
			return invalidDecision([]string{key}, "Required")
		}
	}
	var decision ReplanDecision
	if json.Unmarshal(object["action"], &decision.Action) != nil ||
		!oneOf(decision.Action, "continue", "modify_dag", "reduce_scope", "abort") {
		return invalidDecision([]string{"action"}, "Invalid enum value")
	}
	if json.Unmarshal(object["reason"], &decision.Reason) != nil {
		return invalidDecision([]string{"reason"}, "Expected string")
	}
	reasonLength := len(utf16.Encode([]rune(decision.Reason)))
	if reasonLength < 1 || reasonLength > 4000 {
		return invalidDecision([]string{"reason"}, "String constraint failed")
	}
	if string(object["ops"]) != "null" {
		var rawOps []json.RawMessage
		if json.Unmarshal(object["ops"], &rawOps) != nil {
			return invalidDecision([]string{"ops"}, "Expected array or null")
		}
		decision.Ops = make([]ReplanOp, 0, len(rawOps))
		for index, rawOp := range rawOps {
			operation, issue := parseOperation(rawOp, index)
			if issue != nil {
				return agentjson.Validation[ReplanDecision]{Issues: []agentjson.Issue{*issue}}
			}
			decision.Ops = append(decision.Ops, operation)
		}
	}
	if string(object["drop_ids"]) != "null" {
		if json.Unmarshal(object["drop_ids"], &decision.DropIDs) != nil {
			return invalidDecision([]string{"drop_ids"}, "Expected string array or null")
		}
	}
	if string(object["abort_summary"]) != "null" {
		var summary string
		if json.Unmarshal(object["abort_summary"], &summary) != nil {
			return invalidDecision([]string{"abort_summary"}, "Expected string or null")
		}
		decision.AbortSummary = &summary
	}
	return agentjson.Validation[ReplanDecision]{Data: decision}
}

func parseOperation(raw json.RawMessage, index int) (ReplanOp, *agentjson.Issue) {
	var object map[string]json.RawMessage
	path := []string{"ops", strconv.Itoa(index)}
	if json.Unmarshal(raw, &object) != nil {
		return nil, issue(path, "Expected object")
	}
	var name string
	if json.Unmarshal(object["op"], &name) != nil {
		return nil, issue(append(path, "op"), "Invalid discriminator")
	}
	switch name {
	case "add":
		if len(object) != 5 || !hasKeys(object, "op", "title", "kind", "deps", "reason") {
			return nil, issue(path, "Expected strict add operation")
		}
		var operation AddOp
		if json.Unmarshal(raw, &operation) != nil || operation.Title == "" ||
			operation.Reason == "" ||
			!oneOf(operation.Kind, "code", "research", "review", "test", "shell", "generic") {
			return nil, issue(path, "Invalid add operation")
		}
		if string(object["deps"]) != "null" && operation.Deps == nil {
			return nil, issue(append(path, "deps"), "Expected string array or null")
		}
		return operation, nil
	case "cancel":
		if len(object) != 3 || !hasKeys(object, "op", "id", "reason") {
			return nil, issue(path, "Expected strict cancel operation")
		}
		var operation CancelOp
		if json.Unmarshal(raw, &operation) != nil || operation.ID == "" || operation.Reason == "" {
			return nil, issue(path, "Invalid cancel operation")
		}
		return operation, nil
	case "amend":
		if len(object) != 3 || !hasKeys(object, "op", "id", "prepend") {
			return nil, issue(path, "Expected strict amend operation")
		}
		var operation AmendOp
		if json.Unmarshal(raw, &operation) != nil || operation.ID == "" || operation.Prepend == "" {
			return nil, issue(path, "Invalid amend operation")
		}
		return operation, nil
	case "split":
		if len(object) != 4 || !hasKeys(object, "op", "id", "parts", "reason") {
			return nil, issue(path, "Expected strict split operation")
		}
		var operation SplitOp
		if json.Unmarshal(raw, &operation) != nil || operation.ID == "" ||
			operation.Reason == "" || len(operation.Parts) < 2 {
			return nil, issue(path, "Invalid split operation")
		}
		var rawParts []map[string]json.RawMessage
		_ = json.Unmarshal(object["parts"], &rawParts)
		for partIndex, part := range operation.Parts {
			if partIndex >= len(rawParts) || len(rawParts[partIndex]) != 3 ||
				!hasKeys(rawParts[partIndex], "title", "description", "file_scope") ||
				part.Title == "" || part.Description == "" {
				return nil, issue(
					append(path, "parts", strconv.Itoa(partIndex)),
					"Invalid strict split part",
				)
			}
			rawScope := rawParts[partIndex]["file_scope"]
			if string(rawScope) != "null" && part.FileScope == nil {
				return nil, issue(
					append(path, "parts", strconv.Itoa(partIndex), "file_scope"),
					"Expected string array or null",
				)
			}
		}
		return operation, nil
	default:
		return nil, issue(append(path, "op"), "Invalid discriminator")
	}
}

func invalidDecision(path []string, message string) agentjson.Validation[ReplanDecision] {
	return agentjson.Validation[ReplanDecision]{
		Issues: []agentjson.Issue{{Path: path, Message: message}},
	}
}

func issue(path []string, message string) *agentjson.Issue {
	return &agentjson.Issue{Path: path, Message: message}
}

func hasKeys(object map[string]json.RawMessage, keys ...string) bool {
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			return false
		}
	}
	return true
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
