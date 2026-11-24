package api

// ── AN ORGANIZATION'S AGENTS ON THE WIRE (factory/org_agent.go) ────────────

// OrgAgent is one agent as its reader may see it. Instructions are sent to
// its person, and to everyone once it is on the organization's list or the
// operator is deciding on it; a workstream's people read its name only.
type OrgAgent struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Instructions string `json:"instructions,omitempty"`
	Owner        string `json:"owner"`
	OwnerName    string `json:"owner_name,omitempty"`
	// Roster is "" (only its person knows it), "asked" (waiting for the
	// operator) or "org" (on the organization's list).
	Roster string `json:"roster,omitempty"`
	Team   string `json:"team,omitempty"`
	// Mine is true for the caller's own.
	Mine bool `json:"mine,omitempty"`
	// Workstreams are the ids of the workstreams it is shared into, for its
	// person only.
	Workstreams []string `json:"workstreams,omitempty"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// OrgAgentList is GET /v1/agents: the caller's own, the organization's list,
// and for the operator the ones waiting for them.
type OrgAgentList struct {
	Agents []OrgAgent `json:"agents"`
}

// OrgAgentNew is POST /v1/agents.
type OrgAgentNew struct {
	Name         string `json:"name"`
	Instructions string `json:"instructions"`
	Team         string `json:"team,omitempty"`
}

// OrgAgentPatch is PATCH /v1/agents/{id}: what is sent changes.
type OrgAgentPatch struct {
	Name         *string `json:"name,omitempty"`
	Instructions *string `json:"instructions,omitempty"`
	Team         *string `json:"team,omitempty"`
}

// OrgAgentRosterPut is PUT /v1/agents/{id}/roster. Its person asks (true)
// or takes it off (false); the operator approves (true) or declines (false).
type OrgAgentRosterPut struct {
	On bool `json:"on"`
}

// AgentRef names an agent where it is used: a chat, a workstream's list.
type AgentRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// WorkstreamAgentRow is one agent a workstream lists under People and rules ›
// Agents, the list a task's doer is picked from. Kind is "agent"; From says
// how it came: "space" (shared by its person from their space) or "org" (from
// the organization's list).
type WorkstreamAgentRow struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	From      string `json:"from"`
	Owner     string `json:"owner"`
	OwnerName string `json:"owner_name,omitempty"`
	Team      string `json:"team,omitempty"`
	By        string `json:"by"`
	At        string `json:"at"`
}

// WorkstreamAgentList is GET /v1/workstreams/{id}/agents.
type WorkstreamAgentList struct {
	Agents []WorkstreamAgentRow `json:"agents"`
}

// The limits on an agent.
const (
	OrgAgentNameMost         = 60
	OrgAgentInstructionsMost = 8000
	OrgAgentsPerPersonMost   = 100
	WorkstreamAgentsMost     = 50
)
