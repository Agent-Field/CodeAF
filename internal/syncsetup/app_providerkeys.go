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

func (s appProviderKeyset) provider(name string) (string, bool) {
	if name == config.KeyAPIKey {
		return modelsource.DefaultID, true
	}
	id, ok := strings.CutPrefix(name, sourcePrefix)
	if !ok {
		return "", false
	}
	for _, source := range modelsource.Vendored() {
		if source.ID == id {
			if source.KeyEnv != "" || source.KeyOptional {
				return id, true
			}
			if !modelsource.IsCustomID(id) {
				return "", false
			}
		}
	}
	// A custom service needs this computer's own endpoint before it can use
	// a shared key. Never turn a received name into a new network destination.
	for _, source := range config.PersistedSources(s.profileDir) {
		if source.ID == id && source.Address != "" && source.KeyEnv == "" {
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
	for provider, key := range s.credentials.Snapshot() {
		name := sourcePrefix + provider
		if provider == modelsource.DefaultID {
			name = config.KeyAPIKey
		}
		if _, ok := s.provider(name); !ok {
			continue
		}
		if key == "" {
			delete(values, name)
		} else {
			values[name] = key
		}
	}
	return values, nil
}

func (s appProviderKeyset) Holds(name string) bool {
	_, ok := s.provider(name)
	return ok
}

func (s appProviderKeyset) Apply(changes map[string]string) error {
	expected, err := s.Values()
	if err != nil {
		return err
	}
	return s.ApplyIf(changes, expected)
}

func (s appProviderKeyset) ApplyIf(changes, expected map[string]string) error {
	for name, value := range changes {
		provider, ok := s.provider(name)
		if !ok {
			continue
		}
		current, known := s.credentials.Key(provider)
		if known {
			if current != expected[name] {
				continue
			}
		} else {
			fallback, err := s.providerKeyset.Values()
			if err != nil {
				return err
			}
			if fallback[name] != expected[name] {
				continue
			}
		}
		s.credentials.KeepIf(provider, current, known, value)
	}
	return nil
}
