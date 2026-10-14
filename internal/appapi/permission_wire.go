package appapi

import (
	"time"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
type Permission struct {
	ID           string              `json:"id"`
	AgentID      string              `json:"agent_id"`
	TaskID       string              `json:"task_id,omitempty"`
	Command      string              `json:"command"`
	Class        string              `json:"class,omitempty"`
	Irreversible bool                `json:"irreversible"`
	Why          string              `json:"why"`
	Before       []DisplayEntry      `json:"before" jsonschema:"minItems=2,maxItems=2"`
	Batch        []PermissionCommand `json:"batch,omitempty"`
	Touches      []string            `json:"touches,omitempty"`
	ReadOnly     bool                `json:"read_only,omitempty"`
}
type PermissionCommand struct {
	ID      string `json:"id"`
	Command string `json:"command"`
}
type PermissionDecision struct {
	ID   string `json:"id"`
	Verb string `json:"verb" jsonschema:"enum=allow_once,enum=deny,enum=always"`
}
type PermissionRuleChoice struct {
	Match string `json:"match" jsonschema:"enum=exact,enum=class"`
	Who   string `json:"who" jsonschema:"enum=agent,enum=all_agents"`
}
type PermissionRequest struct {
	Verb   string                `json:"verb,omitempty" jsonschema:"enum=allow_once,enum=deny,enum=always"`
	Rule   *PermissionRuleChoice `json:"rule,omitempty"`
	Items  []PermissionDecision  `json:"items,omitempty"`
	Expect *AgentExpect          `json:"expect,omitempty"`
}
type PermissionRule struct {
	ID         string    `json:"id"`
	Match      string    `json:"match" jsonschema:"enum=exact,enum=class"`
	Pattern    string    `json:"pattern"`
	ClassLabel string    `json:"class_label"`
	Who        string    `json:"who" jsonschema:"enum=agent,enum=all_agents"`
	AgentID    string    `json:"agent_id,omitempty"`
	MadeAt     time.Time `json:"made_at"`
}
type RulesResponse struct {
	Rules []PermissionRule `json:"rules"`
}
type RuleWhyResponse struct {
	Rule    PermissionRule    `json:"rule"`
	Matched PermissionCommand `json:"matched"`
}

func permissionContract() resourceContract {
	return resourceContract{Name: "permission", Types: []any{Permission{}, PermissionCommand{}, PermissionDecision{}, PermissionRuleChoice{}, PermissionRequest{}, PermissionRule{}, RulesResponse{}, RuleWhyResponse{}}, Routes: permissionContractRoutes, Events: []eventContract{{"rule.changed", IDChangedEvent{}}}}
}
