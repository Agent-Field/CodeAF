package appapi

import (
	"time"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
type SpendingToday struct {
	Spent Money `json:"spent"`
	Cap   Money `json:"cap"`
}
type SpendingWho struct {
	Kind    string `json:"kind"`
	ID      string `json:"id,omitempty"`
	Purpose string `json:"purpose,omitempty"`
}
type SpendingRow struct {
	At    time.Time   `json:"at"`
	Who   SpendingWho `json:"who"`
	What  string      `json:"what"`
	Cost  Money       `json:"cost"`
	Model string      `json:"model"`
}
type AgentSpending struct {
	Agent string `json:"agent"`
	Spent Money  `json:"spent"`
}
type SpendingResponse struct {
	Today   SpendingToday   `json:"today"`
	Rows    []SpendingRow   `json:"rows"`
	ByAgent []AgentSpending `json:"by_agent"`
}
type RaiseCapRequest struct {
	Scope  string       `json:"scope" jsonschema:"enum=day,enum=agent,enum=member,enum=crew,enum=task"`
	ID     string       `json:"id,omitempty"`
	To     Money        `json:"to"`
	For    string       `json:"for" jsonschema:"enum=today,enum=for_good"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type SpendChangedEvent struct {
	Today Money  `json:"today"`
	Cap   Money  `json:"cap"`
	Agent string `json:"agent,omitempty"`
}

func spendingContract() resourceContract {
	return resourceContract{Name: "spending", Types: []any{SpendingToday{}, SpendingWho{}, SpendingRow{}, AgentSpending{}, SpendingResponse{}, RaiseCapRequest{}, SpendChangedEvent{}}, Routes: spendingContractRoutes, Events: []eventContract{{"spend.changed", SpendChangedEvent{}}}}
}
