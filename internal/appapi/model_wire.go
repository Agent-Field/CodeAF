package appapi

// These wire types reserve the app vocabulary before this resource has behaviour.
type Model struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Available bool   `json:"available"`
}
type ModelsResponse struct {
	Models  []Model  `json:"models"`
	Default string   `json:"default"`
	Pinned  []string `json:"pinned" jsonschema:"maxItems=3"`
}
type PinnedModelsRequest struct {
	Pinned []string     `json:"pinned" jsonschema:"maxItems=3"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type DefaultModelRequest struct {
	Model  string       `json:"model"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type ModelRole struct {
	Model  string `json:"model"`
	Effort string `json:"effort" jsonschema:"enum=low,enum=medium,enum=high"`
}
type ModelRolesResponse struct {
	Checks  ModelRole `json:"checks"`
	Planner ModelRole `json:"planner"`
}
type ModelRoleRequest struct {
	Model  string       `json:"model"`
	Effort string       `json:"effort" jsonschema:"enum=low,enum=medium,enum=high"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type ProviderSignInResponse struct {
	VerificationURI string `json:"verification_uri"`
}

func modelContract() resourceContract {
	return resourceContract{Name: "model", Types: []any{Model{}, ModelsResponse{}, PinnedModelsRequest{}, DefaultModelRequest{}, ModelRole{}, ModelRolesResponse{}, ModelRoleRequest{}, ProviderSignInResponse{}}, Routes: modelContractRoutes, Events: []eventContract{}}
}
