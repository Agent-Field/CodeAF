package syncsetup

import (
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
	"path/filepath"
	"time"
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
