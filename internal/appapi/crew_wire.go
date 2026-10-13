package appapi

import (
	"time"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
type Crew struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Tint        string      `json:"tint" jsonschema:"enum=iris,enum=sage,enum=sand,enum=tide,enum=rose,enum=graphite"`
	Goal        string      `json:"goal"`
	ManagerID   string      `json:"manager_id"`
	ParentID    string      `json:"parent_id,omitempty"`
	Depth       int         `json:"depth"`
	Budget      Money       `json:"budget"`
	Share       int         `json:"share"`
	EndsAt      *time.Time  `json:"ends_at,omitempty"`
	State       string      `json:"state" jsonschema:"enum=active,enum=wrapping_up,enum=report_ready,enum=closed,enum=archived"`
	Report      *CrewReport `json:"report,omitempty"`
	CreatedFrom string      `json:"created_from"`
	ClosedAt    *time.Time  `json:"closed_at,omitempty"`
	ArchivedAt  *time.Time  `json:"archived_at,omitempty"`
	StateLine   string      `json:"state_line,omitempty"`
	Spent       *Money      `json:"spent,omitempty"`
}
type CrewsResponse struct {
	Crews []Crew `json:"crews"`
}
type CrewResponse struct {
	Crew
	Tree  []Member   `json:"tree"`
	Plan  []Task     `json:"plan"`
	Items []CrewItem `json:"items"`
}
type Helper struct {
	Role   string `json:"role"`
	Model  string `json:"model"`
	Budget Money  `json:"budget"`
}
type HelperProposal struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Tint    string     `json:"tint" jsonschema:"enum=iris,enum=sage,enum=sand,enum=tide,enum=rose,enum=graphite"`
	Helpers []Helper   `json:"helpers"`
	Budget  Money      `json:"budget"`
	EndsAt  *time.Time `json:"ends_at,omitempty"`
}
type CreateCrewRequest struct {
	From       string                `json:"from" jsonschema:"enum=proposal,enum=agents"`
	ProposalID string                `json:"proposal_id,omitempty"`
	Edits      *PatchCrewRequest     `json:"edits,omitempty"`
	AgentIDs   []string              `json:"agent_ids,omitempty"`
	Name       string                `json:"name,omitempty"`
	Goal       string                `json:"goal,omitempty"`
	Manager    string                `json:"manager,omitempty"`
	Members    []CreateMemberRequest `json:"members,omitempty"`
	Expect     *AgentExpect          `json:"expect,omitempty"`
}
type PatchCrewRequest struct {
	Name   *string      `json:"name,omitempty"`
	Tint   *string      `json:"tint,omitempty" jsonschema:"enum=iris,enum=sage,enum=sand,enum=tide,enum=rose,enum=graphite"`
	Goal   *string      `json:"goal,omitempty"`
	Budget *Money       `json:"budget,omitempty"`
	EndsAt *time.Time   `json:"ends_at,omitempty"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type ReplaceManagerRequest struct {
	MemberID string       `json:"member_id,omitempty"`
	Role     string       `json:"role,omitempty"`
	Expect   *AgentExpect `json:"expect,omitempty"`
}
type CrewChangedEvent struct {
	Crew   string   `json:"crew"`
	Fields []string `json:"fields"`
}

func crewContract() resourceContract {
	return resourceContract{Name: "crew", Types: []any{Crew{}, CrewsResponse{}, CrewResponse{}, Helper{}, HelperProposal{}, CreateCrewRequest{}, PatchCrewRequest{}, ReplaceManagerRequest{}, CrewChangedEvent{}}, Routes: crewContractRoutes, Events: []eventContract{{"crew.created", CrewChangedEvent{}}, {"crew.changed", CrewChangedEvent{}}}}
}
