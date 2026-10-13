package spec

import (
	"encoding/json"
	"sort"
)

// Screen is screens/<id>.json: a declarative tree of kit parts.
type Screen struct {
	Screen string   `json:"screen"`
	Title  string   `json:"title"`
	Params []string `json:"params,omitempty"`
	Nav    *Node    `json:"nav,omitempty"`
	Body   *Node    `json:"body"`
}

// Node is one kit part in a screen tree.
type Node struct {
	Part     string              `json:"part"`
	ID       string              `json:"id,omitempty"`
	Label    string              `json:"label,omitempty"`
	Props    map[string]any      `json:"props,omitempty"`
	Bind     *Binding            `json:"bind,omitempty"`
	On       map[string][]Action `json:"on,omitempty"`
	When     string              `json:"when,omitempty"`
	Children []*Node             `json:"children,omitempty"`
	Slots    map[string]*Node    `json:"slots,omitempty"`
}

// Binding says where a node's data comes from. Exactly one of the
// discriminating fields (Records, Record, Run, Runs, Approvals, Selection, Doc)
// is set.
type Binding struct {
	Records   string `json:"records,omitempty"`
	Query     string `json:"query,omitempty"`
	Record    string `json:"record,omitempty"`
	Type      string `json:"type,omitempty"`
	Run       string `json:"run,omitempty"`
	For       string `json:"for,omitempty"`
	Runs      string `json:"runs,omitempty"`
	Approvals string `json:"approvals,omitempty"`
	Selection string `json:"selection,omitempty"`
	Doc       string `json:"doc,omitempty"`
	Surface   string `json:"surface,omitempty"`
}

// Action verbs a screen or command may use.
const (
	VerbOpen          = "open"
	VerbRecordsCreate = "records.create"
	VerbRecordsUpdate = "records.update"
	VerbRecordsDelete = "records.delete"
	VerbRunsStart     = "runs.start"
	VerbApprove       = "approve"
	VerbToolsCall     = "tools.call"
	VerbAISort        = "ai.sort"
	VerbAgentAsk      = "agent.ask"
)

// Verbs lists every action verb in the order the contract documents them.
var Verbs = []string{VerbOpen, VerbRecordsCreate, VerbRecordsUpdate, VerbRecordsDelete, VerbRunsStart, VerbApprove, VerbToolsCall, VerbAISort, VerbAgentAsk}

// Action is one action object: exactly one verb key plus modifiers
// (confirm, ask, id, params, field, record, beside, answer).
type Action map[string]json.RawMessage

// Verb returns the action's verb key, or "" when none is present.
func (a Action) Verb() string {
	for _, verb := range Verbs {
		if _, ok := a[verb]; ok {
			return verb
		}
	}
	return ""
}

// Walk calls visit for every node of the screen, nav first, depth first.
func (s *Screen) Walk(visit func(*Node)) {
	walkNode(s.Nav, visit)
	walkNode(s.Body, visit)
}

func walkNode(n *Node, visit func(*Node)) {
	if n == nil {
		return
	}
	visit(n)
	for _, child := range n.Children {
		walkNode(child, visit)
	}
	for _, name := range sortedKeys(n.Slots) {
		walkNode(n.Slots[name], visit)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
