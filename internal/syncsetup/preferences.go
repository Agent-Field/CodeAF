package syncsetup

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

type preferenceKeys struct{ dir string }

func (p preferenceKeys) KeyTime(key string) time.Time       { return config.PreferenceTime(p.dir, key) }
func (p preferenceKeys) Values() (map[string]string, error) { return config.ReadPreferences(p.dir) }
func (p preferenceKeys) Holds(key string) bool              { return config.PreferenceKey(key) }
func (p preferenceKeys) Apply(changes map[string]string) error {
	return config.ApplyPreferences(p.dir, changes)
}

// preferences gives each setting its own clock and tombstone. Spend totals,
// service addresses, paths and operator pins are deliberately outside the set.
func preferences(dir, home string) vaultsync.Keyed {
	return vaultsync.Keys("preferences", preferenceKeys{dir}, filepath.Join(home, "preferences-sync.ledger"))
}

// ApplyIf keeps edits made by another local writer during the network round.
func (p preferenceKeys) ApplyIf(changes, expected map[string]string) error {
	return config.ApplyPreferencesIf(p.dir, changes, expected)
}

// Accept uses the same resolved connections and readiness rule as model calls.
// Local and sign-in provider prefixes cannot fall through to the default
// router just because this computer has never configured that provider.
func (p preferenceKeys) Accept(key, value, providerID string) bool {
	if !strings.HasPrefix(key, "model.") || value == "" {
		return true
	}
	var model string
	if json.Unmarshal([]byte(value), &model) != nil {
		return false
	}
	if model == "" {
		return true
	}
	sources := config.ResolveSources(p.dir, config.APIKeyAt(p.dir), config.DefaultBaseURL)
	return config.ModelUsable(sources, model, providerID)
}

// Metadata records the provider actually selected on the sending computer.
// Custom aliases otherwise look like ordinary OpenRouter author prefixes on a
// receiver that has no such connection. No endpoint or credential travels here.
func (p preferenceKeys) Metadata(key, value string) string {
	if !strings.HasPrefix(key, "model.") {
		return ""
	}
	var model string
	if json.Unmarshal([]byte(value), &model) != nil || model == "" {
		return ""
	}
	model, _ = roles.SplitEffort(model)
	source, _ := config.ResolveSources(p.dir, config.APIKeyAt(p.dir), config.DefaultBaseURL).For(model)
	return source.Source.ID
}

// MayAnnotate migrates only an explicitly configured provider at the original
// edit clock. A receiver's restore gets a different local clock; in particular,
// falling through to OpenRouter is never evidence about an old custom alias.
func (p preferenceKeys) MayAnnotate(key, value string, stamp int64) bool {
	providerID := p.Metadata(key, value)
	return providerID != "" && providerID != modelsource.DefaultID && p.KeyTime(key).UnixMilli() == stamp
}
