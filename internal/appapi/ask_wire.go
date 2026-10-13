package appapi

import (
	"time"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
type Ask struct {
	ID           string         `json:"id"`
	Kind         string         `json:"kind" jsonschema:"enum=question,enum=clarification,enum=permission,enum=your_call,enum=proposal,enum=budget,enum=order_ask,enum=reminder"`
	Source       SourceRef      `json:"source"`
	Path         []SourceRef    `json:"path"`
	Title        string         `json:"title"`
	Options      []AskOption    `json:"options"`
	Suggested    string         `json:"suggested,omitempty"`
	Irreversible bool           `json:"irreversible"`
	TouchesProd  bool           `json:"touches_prod,omitempty"`
	Clock        *AskClock      `json:"clock,omitempty"`
	Before       []DisplayEntry `json:"before" jsonschema:"minItems=2,maxItems=2"`
	State        string         `json:"state" jsonschema:"enum=waiting,enum=answered,enum=withdrawn"`
	Receipt      *DisplayEntry  `json:"receipt,omitempty"`
}
type AskOption struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}
type AskClock struct {
	StartsAt *time.Time `json:"starts_at,omitempty"`
	PausesAt *time.Time `json:"pauses_at,omitempty"`
}
type AnswerAskRequest struct {
	Choice string       `json:"choice,omitempty"`
	Words  string       `json:"words,omitempty"`
	Verb   string       `json:"verb,omitempty" jsonschema:"enum=later,enum=you_decide"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type AskEvent struct {
	Ask   string `json:"ask"`
	Agent string `json:"agent,omitempty"`
	Kind  string `json:"kind"`
	How   string `json:"how,omitempty"`
	By    string `json:"by,omitempty"`
}

func askContract() resourceContract {
	return resourceContract{Name: "ask", Types: []any{Ask{}, AskOption{}, AskClock{}, AnswerAskRequest{}, AskEvent{}}, Routes: askContractRoutes, Events: []eventContract{{"ask.created", AskEvent{}}, {"ask.resolved", AskEvent{}}, {"ask.withdrawn", AskEvent{}}}}
}
