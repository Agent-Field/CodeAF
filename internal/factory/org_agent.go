package factory

import "time"

// ── AN ORGANIZATION'S AGENTS ───────────────────────────────────────────────
//
// An agent in an organization is a name and its instructions, made by one
// person in their own space (org-space DESIGN §4.6, lane L3). It runs on the
// organization's server with the organization's key, as a chat in its
// person's space or as a doer in a workstream that lists it. The rules:
//
//   - IT IS ITS PERSON'S. Only they change or delete it. Nobody else is told
//     it exists until they share it: not a colleague, not the operator.
//   - SHARING IT INTO A WORKSTREAM GIVES THE WORKSTREAM A LINK, NOT A COPY
//     (DESIGN §2.2). Everyone in that workstream can use it as a doer; only
//     its person changes it. Unsharing stops new uses.
//   - THE ORGANIZATION'S LIST is the agents the operator approved for
//     everyone: its person asks, the operator approves or declines. Anyone
//     who edits a workstream may add one of those to it. A team tag says
//     whose it mostly is; it limits nothing.

// Where an agent stands on the organization's list.
const (
	// AgentRosterMine is an agent only its person knows of.
	AgentRosterMine = ""
	// AgentRosterAsked is waiting for the operator.
	AgentRosterAsked = "asked"
	// AgentRosterOrg is on the organization's list, for everyone.
	AgentRosterOrg = "org"
)

// OrgAgent is one agent kept by an organization's server.
type OrgAgent struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Instructions string    `json:"instructions"`
	Owner        string    `json:"owner"`
	Roster       string    `json:"roster,omitempty"`
	Team         string    `json:"team,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// OrgAgents is every agent, by id.
type OrgAgents map[string]OrgAgent

// WorkstreamAgentLink is one agent a workstream lists under People and
// rules › Agents: which agent, who added it and when.
type WorkstreamAgentLink struct {
	Agent string    `json:"agent"`
	By    string    `json:"by"`
	At    time.Time `json:"at"`
}
