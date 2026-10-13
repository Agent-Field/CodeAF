package appapi

import (
	"time"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
type Process struct {
	ID          string     `json:"id"`
	AgentID     string     `json:"agent_id"`
	Name        string     `json:"name"`
	Command     string     `json:"command"`
	URL         string     `json:"url,omitempty"`
	State       string     `json:"state" jsonschema:"enum=running,enum=done,enum=failed,enum=stopped"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	ExitCode    *int       `json:"exit_code,omitempty"`
	OutputBytes int64      `json:"output_bytes"`
}
type ProcessesResponse []Process
type ProcessOutputResponse struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}
type ProcessChangedEvent struct {
	Agent    string `json:"agent"`
	Process  string `json:"process"`
	State    string `json:"state"`
	ExitCode *int   `json:"exit_code,omitempty"`
	URL      string `json:"url,omitempty"`
}
type ProcessOutputEvent struct {
	Process string `json:"process"`
	Text    string `json:"text"`
}

func processContract() resourceContract {
	return resourceContract{Name: "process", Types: []any{Process{}, ProcessesResponse{}, ProcessOutputResponse{}, ProcessChangedEvent{}, ProcessOutputEvent{}}, Routes: processContractRoutes, Events: []eventContract{{"process.changed", ProcessChangedEvent{}}, {"process.output", ProcessOutputEvent{}}}}
}
