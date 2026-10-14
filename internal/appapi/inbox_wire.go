package appapi

import (
	"time"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
type InboxItem struct {
	Answer       *InboxAnswerRequest `json:"answer,omitempty"`
	Receipt      *DisplayEntry       `json:"receipt,omitempty"`
	Resolved     *InboxResolvedEvent `json:"resolved,omitempty"`
	ID           string              `json:"id"`
	Kind         string              `json:"kind" jsonschema:"enum=question,enum=clarification,enum=permission,enum=your_call,enum=approval_plan,enum=approval_proof,enum=approval_start,enum=budget,enum=out_of_rounds,enum=failed,enum=changes_requested,enum=conflict,enum=publish_failed,enum=runner_offline,enum=order_ask,enum=reminder"`
	Source       SourceRef           `json:"source"`
	Title        string              `json:"title"`
	Path         []SourceRef         `json:"path"`
	CreatedAt    time.Time           `json:"created_at"`
	Context      InboxContext        `json:"context"`
	Verbs        []string            `json:"verbs"`
	WhoCanAnswer []string            `json:"who_can_answer"`
	Permission   *InboxPermission    `json:"permission,omitempty"`
	// UndoUntil is set while an answer waits out its Undo window; Answer then
	// carries that decision, so a refetch can tell the item from an open one.
	UndoUntil *time.Time `json:"undo_until,omitempty"`
}

// InboxPermission is the permission card's whole content: the exact commands
// (never a tool name), what they touch, and the scopes a yes can take.
type InboxPermission struct {
	Reason       string            `json:"reason"`
	Commands     []string          `json:"commands"`
	CommandIDs   []string          `json:"command_ids,omitempty"`
	Irreversible bool              `json:"irreversible"`
	Task         string            `json:"task,omitempty"`
	Scopes       []PermissionScope `json:"scopes"`
	Touches      []string          `json:"touches,omitempty"`
	ReadOnly     bool              `json:"read_only,omitempty"`
}

// PermissionScope is one rule a yes can save: this exact command, or its class.
type PermissionScope struct {
	ID      string `json:"id" jsonschema:"enum=exact,enum=class"`
	Command string `json:"command"`
	Label   string `json:"label"`
}
type InboxContext struct {
	Before []DisplayEntry `json:"before" jsonschema:"minItems=2,maxItems=2"`
}
type InboxResponse struct {
	Items []InboxItem `json:"items"`
}

// Inbox forwards these source decision fields without storing an app Later or Done state.
type InboxAnswerRequest struct {
	Choice string                `json:"choice,omitempty"`
	Words  string                `json:"words,omitempty"`
	Verb   string                `json:"verb,omitempty"`
	Rule   *PermissionRuleChoice `json:"rule,omitempty"`
	Items  []PermissionDecision  `json:"items,omitempty"`
	Expect *AgentExpect          `json:"expect,omitempty"`
}
type InboxAnswerResponse struct {
	OK        bool          `json:"ok"`
	UndoUntil *time.Time    `json:"undo_until,omitempty"`
	Receipt   *DisplayEntry `json:"receipt,omitempty"`
}
type InboxAddedEvent struct {
	Item string `json:"item"`
	Kind string `json:"kind"`
}
type InboxResolvedEvent struct {
	Item string    `json:"item"`
	How  string    `json:"how" jsonschema:"enum=here,enum=in_place,enum=someone_else,enum=no_longer_needed"`
	By   string    `json:"by"`
	At   time.Time `json:"at"`
}
type InboxUndoWindowEvent struct {
	Item  string    `json:"item"`
	Until time.Time `json:"until"`
}

func inboxContract() resourceContract {
	return resourceContract{Name: "inbox", Types: []any{InboxItem{}, InboxPermission{}, PermissionScope{}, InboxContext{}, InboxResponse{}, InboxAnswerRequest{}, InboxAnswerResponse{}, InboxAddedEvent{}, InboxResolvedEvent{}, InboxUndoWindowEvent{}}, Routes: inboxContractRoutes, Events: []eventContract{{"inbox.added", InboxAddedEvent{}}, {"inbox.resolved", InboxResolvedEvent{}}, {"inbox.undo_window", InboxUndoWindowEvent{}}}}
}
