package api

import "github.com/Agent-Field/codeaf/internal/factory"

// WorkHome is the viewer's Work page. Operator access alone never adds a row.
type WorkHome struct {
	PersonStatus map[string]string        `json:"person_status"`
	Workstreams  []WorkstreamSummary      `json:"workstreams"`
	Tasks        []factory.WorkstreamTask `json:"tasks"`
	People       []WorkstreamMember       `json:"people"`
}

// WorkOverview keeps a page's reads together without inventing a second task store.
type WorkOverview struct {
	PersonStatus map[string]string        `json:"person_status"`
	Tasks        []factory.WorkstreamTask `json:"tasks"`
	Steps        map[string][]string      `json:"steps"`
	Previews     map[string]string        `json:"previews"`
	Children     []WorkstreamSummary      `json:"children"`
	People       []WorkstreamMember       `json:"people"`
	Decisions    []WorkstreamDecision     `json:"decisions"`
	Files        []factory.WorkstreamFile `json:"files"`
	AskedOf      []WorkRequest            `json:"asked_of"`
	PlanCount    int                      `json:"plan_count"`
}

// WorkRequest exposes only the request sent from this workstream, never its destination's other work.
type WorkRequest struct {
	ID     string            `json:"id"`
	WS     string            `json:"ws"`
	WSName string            `json:"ws_name"`
	Title  string            `json:"title"`
	Owner  factory.TaskOwner `json:"owner"`
	State  string            `json:"state"`
}
