package appapi

import (
	"time"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
type ExpectRequest struct {
	Expect *AgentExpect `json:"expect,omitempty"`
}
type ActionResponse struct {
	OK      bool          `json:"ok"`
	Receipt *DisplayEntry `json:"receipt,omitempty"`
}
type TextRequest struct {
	Text   string       `json:"text"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type ModelRequest struct {
	Model  string       `json:"model"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type SourceRef struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Factory string `json:"factory,omitempty"`
}
type ErrorResponse struct {
	Error     string `json:"error"`
	Reason    string `json:"reason,omitempty" jsonschema:"enum=not_found,enum=conflict,enum=stale,enum=bad_input,enum=not_allowed,enum=at_cap,enum=no_factory"`
	RequestID string `json:"request_id"`
}
type NotBuiltResponse struct {
	Error string `json:"error" jsonschema:"enum=not built yet"`
	Route string `json:"route"`
}
type ConflictResponse struct {
	ErrorResponse
	By           string         `json:"by,omitempty"`
	At           *time.Time     `json:"at,omitempty"`
	Running      *RunningCounts `json:"running,omitempty"`
	ChangedSince []string       `json:"changed_since,omitempty"`
	OpenItems    *int           `json:"open_items,omitempty"`
}
type RunningCounts struct {
	Processes int `json:"processes"`
	Tasks     int `json:"tasks"`
}
type IDChangedEvent struct {
	ID string `json:"id"`
}
type KeysChangedEvent struct {
	Keys []string `json:"keys"`
}

func commonContract() resourceContract {
	return resourceContract{Name: "common", Types: []any{ExpectRequest{}, ActionResponse{}, TextRequest{}, ModelRequest{}, SourceRef{}, ErrorResponse{}, NotBuiltResponse{}, ConflictResponse{}, RunningCounts{}, IDChangedEvent{}, KeysChangedEvent{}, Money{}, AgentExpect{}, RouteContract{}}, Routes: commonContractRoutes, Events: []eventContract{{"rule.created", IDChangedEvent{}}}}
}
