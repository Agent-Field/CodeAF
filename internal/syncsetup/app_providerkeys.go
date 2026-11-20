package syncsetup

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// Desktop keys use the same encrypted provider slots as terminal keys, but a
// received key goes through the app's credential handoff instead of config.json.
type appProviderKeyset struct {
	providerKeyset
	credentials *config.AppCredentials
}

func sharedAppProvider(name string) (string, bool) {
	if name == config.KeyAPIKey {
		return modelsource.DefaultID, true
	}
	id, ok := strings.CutPrefix(name, sourcePrefix)
	if !ok {
		return "", false
	}
	// A custom or address-owned service can have a different endpoint on
	// each device. Its provider ID is insufficient to identify its key.
	for _, source := range modelsource.Vendored() {
		if source.ID == id && !source.AddressOwned && source.KeyEnv != "" {
			return id, true
		}
	}
	return "", false
}

func (s appProviderKeyset) Values() (map[string]string, error) {
	values, err := s.providerKeyset.Values()
	if err != nil {
		return nil, err
	}
	for name := range values {
		if strings.HasPrefix(name, sourcePrefix) {
			if _, ok := sharedAppProvider(name); !ok {
				delete(values, name)
			}
		}
	}
	for provider, key := range s.credentials.Snapshot() {
		name := sourcePrefix + provider
		if provider == modelsource.DefaultID {
			name = config.KeyAPIKey
		}
		if _, ok := sharedAppProvider(name); ok {
			// Preserve a disconnect separately from unread native credentials.
			values[name] = key
		}
	}
	return values, nil
}

func (s appProviderKeyset) Holds(name string) bool {
	_, ok := sharedAppProvider(name)
	return ok
}

func (s appProviderKeyset) Apply(changes map[string]string) error {
	expected, err := s.Values()
	if err != nil {
		return err
	}
	_, err = s.ApplyMatching(changes, expected)
	return err
}

// ApplyMatching compares both the value and its presence against the capture.
// Return actual writes so the vault only advances the ledger for those keys.
func (s appProviderKeyset) ApplyMatching(changes, expected map[string]string) (map[string]string, error) {
	applied := make(map[string]string)
	for name, value := range changes {
		provider, ok := sharedAppProvider(name)
		if !ok {
			continue
		}
		current, known := s.credentials.Key(provider)
		held, present := current, known
		if !known {
			fallback, err := s.providerKeyset.Values()
			if err != nil {
				return nil, err
			}
			held, present = fallback[name]
		}
		want, had := expected[name]
		if held == want && present == had && s.credentials.KeepIf(provider, current, known, value) {
			applied[name] = value
		}
	}
	return applied, nil
}
