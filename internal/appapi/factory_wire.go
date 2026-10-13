package appapi

import (
	"encoding/json"
	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
	"time"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
type Factory struct {
	ID       string     `json:"id"`
	URL      string     `json:"url"`
	Name     string     `json:"name"`
	Mode     string     `json:"mode" jsonschema:"enum=solo,enum=team"`
	Operator bool       `json:"operator"`
	State    string     `json:"state" jsonschema:"enum=ok,enum=unreachable,enum=signed_out"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
}
type FactoriesResponse []Factory
type AttachFactoryRequest struct {
	URL    string       `json:"url"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type HandOffCard struct {
	Factory   string    `json:"factory"`
	Factories []Factory `json:"factories"`
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Brief     string    `json:"brief"`
	Repo      string    `json:"repo"`
	Pipeline  string    `json:"pipeline"`
	Issue     bool      `json:"issue"`
	Start     string    `json:"start" jsonschema:"enum=now,enum=backlog"`
	Assignee  string    `json:"assignee"`
}
type SendHandOffRequest struct {
	Factory  *string      `json:"factory,omitempty"`
	Title    *string      `json:"title,omitempty"`
	Brief    *string      `json:"brief,omitempty"`
	Repo     *string      `json:"repo,omitempty"`
	Pipeline *string      `json:"pipeline,omitempty"`
	Issue    *bool        `json:"issue,omitempty"`
	Start    *string      `json:"start,omitempty" jsonschema:"enum=now,enum=backlog"`
	Assignee *string      `json:"assignee,omitempty"`
	Expect   *AgentExpect `json:"expect,omitempty"`
}
type HandOffResponse struct {
	Factory string `json:"factory"`
	Job     string `json:"job"`
}
type PowerRequest struct {
	State  string       `json:"state" jsonschema:"enum=sleeping,enum=awake"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type FactoryChangedEvent struct {
	Factory string `json:"factory"`
	State   string `json:"state"`
}
type FactoryEvent struct {
	Factory string          `json:"factory"`
	Type    string          `json:"type"`
	Data    json.RawMessage `json:"data"`
}

func factoryContract() resourceContract {
	return resourceContract{Name: "factory", Types: []any{factoryapi.FactorySetup{}, factoryapi.FactorySetupRequest{}, factoryapi.SetupGitHub{}, factoryapi.SetupGH{}, factoryapi.SetupRepo{}, factoryapi.FactoryAccess{}, factoryapi.FactoryAccessRequest{}, factoryapi.AccessFactory{}, factoryapi.FactoryDay{}, factoryapi.JobDisplay{}, Factory{}, FactoriesResponse{}, AttachFactoryRequest{}, HandOffCard{}, SendHandOffRequest{}, HandOffResponse{}, PowerRequest{}, FactoryChangedEvent{}, FactoryEvent{}}, Routes: factoryContractRoutes, Events: []eventContract{{"factory.changed", FactoryChangedEvent{}}, {"factory.event", FactoryEvent{}}}}
}
