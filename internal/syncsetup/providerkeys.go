package syncsetup

import (
	"maps"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

// sourcePrefix marks a model service's key in a key name, so a service id can
// never be mistaken for a settings row.
const sourcePrefix = "source:"

// providerKeys carries the keys stored in the profile's config.json, one vault
// entry per key, and only those: everything else in that file stays on the
// machine it was set on. The ledger of what was last shared sits in the home,
// beside the vault.
func providerKeys(profileDir, home string) vaultsync.Keyed {
	return vaultsync.Keys("provider", providerKeyset{profileDir}, filepath.Join(home, "provider-keys.ledger"))
}

// providerKeyset is the key fields of config.json as a vaultsync.Keyset. It
// reads and writes through the config package, so the file's own lock, atomic
// writer and mode apply, and no environment variable is ever consulted.
type providerKeyset struct{ profileDir string }

// Values flattens the stored keys to names: a settings row by its config key, a
// service as "source:<id>". A config file that cannot be read is damaged, not
// empty.
func (s providerKeyset) Values() (map[string]string, error) {
	keys, err := config.ReadProviderKeys(s.profileDir)
	if err != nil {
		return nil, vaultsync.ErrDamaged
	}
	values := maps.Clone(keys.Rows)
	if values == nil {
		values = map[string]string{}
	}
	for id, key := range keys.Sources {
		values[sourcePrefix+id] = key
	}
	return values, nil
}

// Holds reports whether this profile has a place for the key: every secret row,
// and a service only when its row is listed here and stores its key inline.
func (s providerKeyset) Holds(name string) bool {
	return !config.ProviderKeysHeldBy(s.profileDir, unflatten(map[string]string{name: "held"})).Empty()
}

// Apply changes the named keys and leaves every other field of the file alone.
func (s providerKeyset) Apply(changes map[string]string) error {
	held, err := config.ReadProviderKeys(s.profileDir)
	if err != nil {
		return vaultsync.ErrDamaged
	}
	next := config.ProviderKeys{Rows: maps.Clone(held.Rows), Sources: maps.Clone(held.Sources)}
	for name, value := range changes {
		next = with(next, name, value)
	}
	return config.WriteProviderKeys(s.profileDir, next)
}

// with is keys with the named key set, or removed when value is empty.
func with(keys config.ProviderKeys, name, value string) config.ProviderKeys {
	table, key := &keys.Rows, name
	if id, isSource := strings.CutPrefix(name, sourcePrefix); isSource {
		table, key = &keys.Sources, id
	}
	if *table == nil {
		*table = map[string]string{}
	}
	if value == "" {
		delete(*table, key)
	} else {
		(*table)[key] = value
	}
	return keys
}

// unflatten is a set of flattened names as ProviderKeys.
func unflatten(values map[string]string) config.ProviderKeys {
	var keys config.ProviderKeys
	for name, value := range values {
		keys = with(keys, name, value)
	}
	return keys
}
