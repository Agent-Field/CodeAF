package appapi

import "encoding/json"

// Settings uses dotted keys; account keys are extensible and values stay JSON.
type Settings map[string]json.RawMessage
type PatchSettingsRequest map[string]json.RawMessage

// Profile versions are opaque hashes, unlike an agent's numeric version.
type SettingsExpect struct {
	Version string `json:"version,omitempty"`
	State   string `json:"state,omitempty"`
}
type SetupResponse struct {
	Step         int    `json:"step"`
	HasKey       bool   `json:"has_key"`
	DefaultModel string `json:"default_model,omitempty"`
	DayCap       *Money `json:"day_cap"`
	Complete     bool   `json:"complete"`
}
type SetupRequest struct {
	Step         int             `json:"step"`
	DefaultModel *string         `json:"default_model,omitempty"`
	DayCap       *Money          `json:"day_cap,omitempty"`
	Expect       *SettingsExpect `json:"expect,omitempty"`
}

func settingContract() resourceContract {
	return resourceContract{Name: "setting", Types: []any{Settings{}, PatchSettingsRequest{}, SettingsExpect{}, SetupResponse{}, SetupRequest{}, EmptyRequest{}, ServiceAction{}, ConnectedService{}, AccountsResponse{}, ServiceAccessRequest{}, ServiceConnectResponse{}, ServicePollResponse{}, AccountConnectResponse{}, McpServer{}, McpServersResponse{}, AddMcpServerRequest{}}, Routes: append(settingContractRoutes, connectionContractRoutes...), Events: []eventContract{{"settings.changed", KeysChangedEvent{}}}}
}
