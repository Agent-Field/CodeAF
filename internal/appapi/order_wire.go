package appapi

import (
	"time"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
type StandingOrder struct {
	Version      uint64        `json:"version"`
	Runs         []StandingRun `json:"runs"`
	ID           string        `json:"id"`
	AgentID      string        `json:"agent_id"`
	Title        string        `json:"title"`
	Trigger      OrderTrigger  `json:"trigger"`
	Does         string        `json:"does"`
	Wont         string        `json:"wont"`
	Scope        string        `json:"scope" jsonschema:"enum=this_chat,enum=this_crew,enum=everywhere,enum=not_here"`
	Trust        string        `json:"trust" jsonschema:"enum=asks_first,enum=earning,enum=trusted"`
	ApprovedRuns int           `json:"approved_runs"`
	BudgetPerRun Money         `json:"budget_per_run"`
	State        string        `json:"state" jsonschema:"enum=proposed,enum=active,enum=paused,enum=stopped,enum=declined,enum=fired"`
	LastRunAt    *time.Time    `json:"last_run_at,omitempty"`
	Skipped      int           `json:"skipped"`
}

// StandingRun exposes decision ids and skipped lines without retaining probe
// evidence in the app. The engine alone executes the evidence it persisted.
type StandingRun struct {
	ID    string    `json:"id"`
	At    time.Time `json:"at"`
	State string    `json:"state"`
	Line  string    `json:"line"`
}
type OrderTrigger struct {
	Kind   string `json:"kind" jsonschema:"enum=event,enum=schedule,enum=once"`
	Source string `json:"source"`
	Filter string `json:"filter"`
}
type OrdersResponse struct {
	Waiting   []StandingOrder `json:"waiting"`
	Active    []StandingOrder `json:"active"`
	Paused    []StandingOrder `json:"paused"`
	Reminders []StandingOrder `json:"reminders"`
}
type ChangeOrderRequest struct {
	Title        *string       `json:"title,omitempty"`
	Trigger      *OrderTrigger `json:"trigger,omitempty"`
	Does         *string       `json:"does,omitempty"`
	Wont         *string       `json:"wont,omitempty"`
	Scope        *string       `json:"scope,omitempty" jsonschema:"enum=this_chat,enum=this_crew,enum=everywhere,enum=not_here"`
	BudgetPerRun *Money        `json:"budget_per_run,omitempty"`
	Expect       *AgentExpect  `json:"expect,omitempty"`
}
type OrderRunRequest struct {
	Verb   string       `json:"verb" jsonschema:"enum=go,enum=skip,enum=pause,enum=not_right,enum=run_now"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type ReminderChangeRequest struct {
	At     time.Time    `json:"at"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type OrderChangedEvent struct {
	Agent        string `json:"agent"`
	Order        string `json:"order"`
	State        string `json:"state"`
	Trust        string `json:"trust"`
	ApprovedRuns int    `json:"approved_runs"`
}
type OrderFiredEvent struct {
	Order string `json:"order"`
	Run   string `json:"run"`
	Ask   string `json:"ask,omitempty"`
}

func orderContract() resourceContract {
	return resourceContract{Name: "order", Types: []any{StandingOrder{}, StandingRun{}, OrderTrigger{}, OrdersResponse{}, ChangeOrderRequest{}, OrderRunRequest{}, ReminderChangeRequest{}, OrderChangedEvent{}, OrderFiredEvent{}}, Routes: orderContractRoutes, Events: []eventContract{{"order.changed", OrderChangedEvent{}}, {"order.fired", OrderFiredEvent{}}}}
}
