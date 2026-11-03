package config

import (
	"encoding/json"

	"github.com/Agent-Field/codeaf/internal/filememo"
)

var teamsShareChoices = []string{"off", "on"}

// This preference must distinguish absence from a failed read. The shared
// settings memo deliberately treats either as missing for other settings.
var teamsShareMemo = filememo.Stamped(SettingsGeneration, func(path string, raw []byte, missing bool) (bool, error) {
	if missing {
		return true, nil
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return false, nil
	}
	encoded, exists := values[KeyTeamsShare]
	if !exists {
		return true, nil
	}
	var value *string
	if json.Unmarshal(encoded, &value) != nil || value == nil {
		return false, nil
	}
	return *value == "" || *value == "on", nil
})

// TeamsShareAt reads the local preference at use time. Missing choices default
// on, but damaged files must not erase a person's earlier decision to turn off.
func TeamsShareAt(profileDir string) bool {
	on, _ := teamsShareMemo.ReadStrict(BudgetConfigPath(profileDir))
	return on
}

// TeamsSharingAt combines the preference with the caller's pairing gate without
// making configuration depend on relay setup. An absent gate keeps local work local.
func TeamsSharingAt(profileDir string, paired func() bool) bool {
	return TeamsShareAt(profileDir) && paired != nil && paired()
}
