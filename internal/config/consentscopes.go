package config

import (
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/approval"
)

// ApprovalScopes offers only rules the gate can honour on a later call.
// The floor is checked before bash shapes so a critical compound stays critical.
func ApprovalScopes(tool string, args json.RawMessage) ([]string, string) {
	return ApprovalScopesIn(tool, args, "")
}

// ApprovalScopesIn is [ApprovalScopes] for a call whose folder is known, so a
// recursive delete inside the agent's own folder is offered the ordinary
// scopes rather than graded critical ([approval.AlwaysAsksIn]).
func ApprovalScopesIn(tool string, args json.RawMessage, workspace string) ([]string, string) {
	if approval.ActsInThePersonsName(tool, args) {
		return nil, "acts_as_you"
	}
	if approval.AlwaysAsksIn(tool, args, workspace) {
		return nil, "critical"
	}
	if tool == "" {
		return nil, "compound"
	}
	if tool != approval.ToolBash {
		return []string{tool}, ""
	}
	var call struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(args, &call)
	if scopes := BashShapes(call.Command); len(scopes) > 0 {
		return scopes, ""
	}
	return nil, "compound"
}

// SaveApproval keeps both surfaces on the same persistence path. A plain tool
// returns no banked bash shape, preserving the terminal's tool-session answer.
func SaveApproval(tool, shape string, bash, other func(string) error) (string, bool) {
	if tool == approval.ToolBash {
		if shape == "" || bash == nil || bash(shape) != nil {
			return "", false
		}
		return shape, true
	}
	if other == nil {
		return "", false
	}
	return "", other(tool) == nil
}
