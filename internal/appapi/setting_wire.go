package appapi

import (
	"encoding/json"
)

// These wire types reserve the app vocabulary before this resource has behaviour.
// Settings uses dotted keys; account keys are extensible and values stay JSON.
// The contract reserves transport shapes without implementing settings validation.
type Settings map[string]json.RawMessage
type PatchSettingsRequest map[string]json.RawMessage
type SetupResponse struct {
	Step         int    `json:"step"`
	HasKey       bool   `json:"has_key"`
	DefaultModel string `json:"default_model,omitempty"`
	DayCap       *Money `json:"day_cap,omitempty"`
}
type SetupRequest struct {
	Step         int          `json:"step"`
	DefaultModel string       `json:"default_model,omitempty"`
	DayCap       *Money       `json:"day_cap,omitempty"`
	Expect       *AgentExpect `json:"expect,omitempty"`
}

func settingContract() resourceContract {
	return resourceContract{Name: "setting", Types: []any{Settings{}, PatchSettingsRequest{}, SetupResponse{}, SetupRequest{}}, Routes: settingContractRoutes, Events: []eventContract{{"settings.changed", KeysChangedEvent{}}}}
}
