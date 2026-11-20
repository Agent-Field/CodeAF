package syncsetup

import (
	"net/url"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// Desktop keys use the same encrypted provider slots as terminal keys, but a
// received key goes through the app's credential handoff instead of config.json.
type appProviderKeyset struct {
	providerKeyset
	credentials *config.AppCredentials
	baseline    map[string]config.AppKeyState
	recent      map[string]config.AppKeyState
}

func sharedAppProvider(name string) (string, bool) {
	if name == config.KeyAPIKey {
		base := strings.TrimSpace(env.Get("CODEAF_BASE_URL"))
		if base == "" {
			return modelsource.DefaultID, true
		}
		address, err := url.Parse(base)
		official, _ := url.Parse(config.DefaultBaseURL)
		return modelsource.DefaultID, err == nil && address.Scheme == "https" &&
			address.User == nil && strings.EqualFold(address.Host, official.Host)
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

func (s *appProviderKeyset) Values() (map[string]string, error) {
	if !s.credentials.SyncReady() {
		return nil, nil
	}
	values, err := s.providerKeyset.Values()
	if err != nil {
		return nil, err
	}
	for name := range values {
		if _, ok := sharedAppProvider(name); !ok {
			delete(values, name)
		}
	}
	s.recent = s.credentials.SyncSnapshot()
	for provider, state := range s.recent {
		name := sourcePrefix + provider
		if provider == modelsource.DefaultID {
			name = config.KeyAPIKey
		}
		if _, ok := sharedAppProvider(name); ok {
			// Preserve a disconnect separately from unread native credentials.
			values[name] = state.Key
		}
	}
	return values, nil
}

func (s *appProviderKeyset) CaptureValues() (map[string]string, error) {
	values, err := s.Values()
	s.baseline = s.recent
	return values, err
}

func (s *appProviderKeyset) MayCapture(name string) bool {
	return !s.credentials.NeedsSyncLoad() || !s.PreserveVault(name)
}

func (s *appProviderKeyset) PreserveVault(name string) bool {
	provider, ok := sharedAppProvider(name)
	return ok && s.credentials.WasObserved(provider, s.baseline[provider].Revision)
}

func (s *appProviderKeyset) CaptureCommitted(name string) {
	provider, ok := sharedAppProvider(name)
	if ok {
		s.credentials.ObserveCapture(provider, s.baseline[provider].Revision)
	}
}

func (s *appProviderKeyset) Holds(name string) bool {
	_, ok := sharedAppProvider(name)
	return ok && s.credentials.SyncReady()
}

func (s *appProviderKeyset) Apply(changes map[string]string) error {
	expected, err := s.CaptureValues()
	if err != nil {
		return err
	}
	_, err = s.ApplyMatching(changes, expected)
	return err
}

// ApplyMatching compares both the value and its presence against the capture.
// Return actual writes so the vault only advances the ledger for those keys.
func (s *appProviderKeyset) ApplyMatching(changes, expected map[string]string) (map[string]string, error) {
	applied := make(map[string]string)
	if !s.credentials.SyncReady() {
		return applied, nil
	}
	baseline := s.baseline
	if baseline == nil {
		baseline = s.recent
	}
	for name, value := range changes {
		provider, ok := sharedAppProvider(name)
		if !ok {
			continue
		}
		current := s.credentials.State(provider)
		if current != baseline[provider] {
			continue
		}
		held, present := current.Key, current.Present
		if !current.Present {
			fallback, err := s.providerKeyset.Values()
			if err != nil {
				return nil, err
			}
			held, present = fallback[name]
		}
		want, had := expected[name]
		if held == want && present == had && s.credentials.ProposeSynced(provider, current, value) {
			applied[name] = value
			baseline[provider] = config.AppKeyState{Key: value, Present: true, Revision: current.Revision + 1}
		}
	}
	return applied, nil
}
