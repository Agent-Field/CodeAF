package appapi

// Model names the connection that can run this canonical model id.
type Model struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Provider  string   `json:"provider"`
	Available bool     `json:"available"`
	Efforts   []string `json:"efforts"`
	// Prices share the spend meter's tariff in USD per million tokens; an
	// omitted price means unknown, while a published zero remains visible.
	PriceIn  *float64 `json:"price_in,omitempty"`
	PriceOut *float64 `json:"price_out,omitempty"`
}
type ModelsResponse struct {
	Models  []Model `json:"models"`
	Default string  `json:"default"`
	// Resolved names the fallback without replacing the saved default choice.
	Resolved string   `json:"resolved"`
	Pinned   []string `json:"pinned" jsonschema:"minItems=3,maxItems=3"`
}
type PinnedModelsRequest struct {
	Models []string        `json:"models" jsonschema:"minItems=3,maxItems=3"`
	Expect *SettingsExpect `json:"expect,omitempty"`
}
type PinnedModelsResponse struct {
	Pinned []string `json:"pinned" jsonschema:"minItems=3,maxItems=3"`
}
type DefaultModelRequest struct {
	Model  string          `json:"model"`
	Effort string          `json:"effort,omitempty" jsonschema:"enum=low,enum=medium,enum=high"`
	Expect *SettingsExpect `json:"expect,omitempty"`
}
type DefaultModelResponse struct {
	Model  string `json:"model"`
	Effort string `json:"effort" jsonschema:"enum=low,enum=medium,enum=high"`
}
type ModelRole struct {
	ID     string `json:"id" jsonschema:"enum=checks,enum=planner"`
	Model  string `json:"model"`
	Effort string `json:"effort" jsonschema:"enum=low,enum=medium,enum=high"`
}
type ModelRolesResponse struct {
	Roles []ModelRole `json:"roles" jsonschema:"minItems=2,maxItems=2"`
}
type ModelRoleRequest struct {
	Model  string          `json:"model"`
	Effort string          `json:"effort" jsonschema:"enum=low,enum=medium,enum=high"`
	Expect *SettingsExpect `json:"expect,omitempty"`
}
type ProviderSignInRequest struct{}
type ProviderSignInResponse struct {
	VerificationURI string `json:"verification_uri"`
}
type ProviderSignInStatusResponse struct {
	State string `json:"state" jsonschema:"enum=waiting,enum=signed_in,enum=failed,enum=canceled"`
	Error string `json:"error"`
}
type ProviderSignInClaimResponse struct {
	Provider string `json:"provider"`
	Key      string `json:"key"`
}

func modelContract() resourceContract {
	return resourceContract{Name: "model", Types: []any{Model{}, ModelsResponse{}, PinnedModelsRequest{}, PinnedModelsResponse{}, DefaultModelRequest{}, DefaultModelResponse{}, ModelRole{}, ModelRolesResponse{}, ModelRoleRequest{}, ProviderSignInRequest{}, ProviderSignInResponse{}, ProviderSignInStatusResponse{}, ProviderSignInClaimResponse{}}, Routes: modelContractRoutes, Events: []eventContract{}}
}
