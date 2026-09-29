package syncsetup

import (
	"encoding/json"
	"os"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

// providerKeys carries the keys stored in the profile's config.json, and only
// those: everything else in that file stays on the machine it was set on.
func providerKeys(profileDir string) vaultsync.Carried {
	return vaultsync.Carried{ID: "home:provider-keys", Scope: "home:carried", Name: "provider keys", Medium: providerKeysMedium{profileDir}}
}

// providerKeysMedium is the key fields of config.json as a vaultsync.Medium. It
// reads and writes through the config package, so the file's own lock, atomic
// writer and mode apply.
type providerKeysMedium struct{ profileDir string }

// Read answers the keys as JSON, and the config file's save time. A profile with
// no key of its own holds nothing, which is how a removal travels; a config file
// that cannot be read is damaged, not empty.
func (m providerKeysMedium) Read() (string, time.Time, error) {
	keys, err := config.ReadProviderKeys(m.profileDir)
	if err != nil {
		return "", time.Time{}, vaultsync.ErrDamaged
	}
	if keys.Empty() {
		return "", time.Time{}, os.ErrNotExist
	}
	info, err := os.Stat(config.BudgetConfigPath(m.profileDir))
	if err != nil {
		return "", time.Time{}, err
	}
	return encodeKeys(keys), info.ModTime(), nil
}

// Write replaces the stored keys, and nothing else in the file. A content this
// build cannot read is left alone.
func (m providerKeysMedium) Write(content string) error {
	keys, err := decodeKeys(content)
	if err != nil {
		return vaultsync.ErrDamaged
	}
	if _, err := config.ReadProviderKeys(m.profileDir); err != nil {
		return vaultsync.ErrDamaged
	}
	return config.WriteProviderKeys(m.profileDir, keys)
}

func (m providerKeysMedium) Clear() error {
	return config.WriteProviderKeys(m.profileDir, config.ProviderKeys{})
}

// Realized is content cut down to the services this machine has, and nothing
// when no key is left.
func (m providerKeysMedium) Realized(content string) string {
	keys, err := decodeKeys(content)
	if err != nil {
		return content
	}
	held := config.ProviderKeysHeldBy(m.profileDir, keys)
	if held.Empty() {
		return ""
	}
	return encodeKeys(held)
}

func encodeKeys(k config.ProviderKeys) string {
	raw, _ := json.Marshal(k)
	return string(raw)
}

func decodeKeys(content string) (config.ProviderKeys, error) {
	var k config.ProviderKeys
	return k, json.Unmarshal([]byte(content), &k)
}
