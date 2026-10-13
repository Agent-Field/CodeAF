package appapi

// These wire types reserve the app vocabulary before this resource has behaviour.
type RoleCopy struct {
	CrewID   string `json:"crew_id"`
	MemberID string `json:"member_id"`
}
type CrewRole struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Instructions string     `json:"instructions"`
	Model        string     `json:"model"`
	Effort       string     `json:"effort" jsonschema:"enum=low,enum=medium,enum=high"`
	MadeBy       string     `json:"made_by"`
	Copies       []RoleCopy `json:"copies"`
}
type RolesResponse struct {
	Roles []CrewRole `json:"roles"`
}
type CreateRoleRequest struct {
	Name         string       `json:"name"`
	Instructions string       `json:"instructions"`
	Model        string       `json:"model"`
	Effort       string       `json:"effort" jsonschema:"enum=low,enum=medium,enum=high"`
	Expect       *AgentExpect `json:"expect,omitempty"`
}
type PatchRoleRequest struct {
	Name         *string      `json:"name,omitempty"`
	Instructions *string      `json:"instructions,omitempty"`
	Model        *string      `json:"model,omitempty"`
	Effort       *string      `json:"effort,omitempty" jsonschema:"enum=low,enum=medium,enum=high"`
	Expect       *AgentExpect `json:"expect,omitempty"`
}

func roleContract() resourceContract {
	return resourceContract{Name: "role", Types: []any{RoleCopy{}, CrewRole{}, RolesResponse{}, CreateRoleRequest{}, PatchRoleRequest{}}, Routes: roleContractRoutes, Events: []eventContract{{"role.changed", IDChangedEvent{}}}}
}
