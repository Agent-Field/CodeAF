package appapi

// These wire types reserve the app vocabulary before this resource has behaviour.
type Member struct {
	CrewID    string   `json:"crew_id"`
	AgentID   string   `json:"agent_id"`
	RoleID    string   `json:"role_id"`
	IsManager bool     `json:"is_manager"`
	ReportsTo string   `json:"reports_to"`
	Cap       *Money   `json:"cap,omitempty"`
	State     string   `json:"state" jsonschema:"enum=running,enum=idle,enum=needs_you,enum=paused,enum=stopped"`
	AlsoIn    []string `json:"also_in"`
	Spent     *Money   `json:"spent,omitempty"`
}
type NewRole struct {
	Name         string `json:"name"`
	Instructions string `json:"instructions"`
	Model        string `json:"model"`
	Effort       string `json:"effort" jsonschema:"enum=low,enum=medium,enum=high"`
}
type CreateMemberRequest struct {
	RoleID    string       `json:"role_id,omitempty"`
	NewRole   *NewRole     `json:"new_role,omitempty"`
	AgentID   string       `json:"agent_id,omitempty"`
	Cap       *Money       `json:"cap,omitempty"`
	ReportsTo string       `json:"reports_to"`
	Expect    *AgentExpect `json:"expect,omitempty"`
}
type PatchMemberRequest struct {
	Cap          *Money       `json:"cap,omitempty"`
	ReportsTo    *string      `json:"reports_to,omitempty"`
	Instructions *string      `json:"instructions,omitempty"`
	Expect       *AgentExpect `json:"expect,omitempty"`
}
type MoveMemberRequest struct {
	CrewID string       `json:"crew_id"`
	Expect *AgentExpect `json:"expect,omitempty"`
}

func memberContract() resourceContract {
	return resourceContract{Name: "member", Types: []any{Member{}, NewRole{}, CreateMemberRequest{}, PatchMemberRequest{}, MoveMemberRequest{}}, Routes: memberContractRoutes, Events: []eventContract{}}
}
