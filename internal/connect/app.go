package connect

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/oauth2"
)

// AppKeySource belongs to the native app. Nothing in this door reads or writes a credential file.
type AppKeySource interface {
	Key(string) (string, bool)
	Set(string, string)
}

// NewAppManager uses the ordinary sign-in and tools with the app's memory-only keys.
// Completed browser grants, including renewals, stay in this launch's source.
func NewAppManager(profile string, credentials map[string]ClientCredential, source AppKeySource) *Manager {
	plugs := []Plug{}
	for _, plug := range Registered() {
		if _, remote := plug.(*toolServer); !remote {
			plugs = append(plugs, plug)
		}
	}
	manager := &Manager{store: &store{path: filepath.Join(profile, StoreFileName), app: source}, creds: credentials, plugs: plugs}
	manager.store.appPlugs = manager.currentPlugs
	return manager
}

func (s *store) loadApp() map[string]stored {
	out := map[string]stored{}
	plugs := Registered()
	if s.appPlugs != nil {
		plugs = s.appPlugs()
	}
	for _, plug := range plugs {
		service := plug.Service()
		raw, _ := s.app.Key(service.ID)
		if raw == "" {
			continue
		}
		var entry stored
		if strings.HasPrefix(raw, "{") {
			if json.Unmarshal([]byte(raw), &entry) != nil {
				continue
			}
		} else if service.Auth == AuthKey {
			entry = stored{Auth: AuthKey, Key: raw}
		} else {
			entry = stored{Keys: &oauth2.Token{AccessToken: raw, TokenType: "Bearer"}, Scopes: service.Scopes}
		}
		out[service.ID] = entry
	}
	return out
}
func (s *store) replaceApp(id string, edit func(map[string]stored)) error {
	entries := s.loadApp()
	edit(entries)
	entry, ok := entries[id]
	if !ok {
		s.app.Set(id, "")
		return nil
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	s.app.Set(id, string(raw))
	return nil
}

// AppPolicy makes a service-wide Off reach every native capability on the next call.
// Acting in the person's name retains the ask floor even when the service says Yes.
func (m *Manager) SetAppPolicy(policy func(string, string) (CapabilityState, bool)) {
	m.appPolicy = policy
}

type appVersionedSource interface {
	KeyRevision(string) uint64
	SetAtRevision(string, string, uint64) bool
}

func (s *store) appRevision(id string) uint64 {
	if source, ok := s.app.(appVersionedSource); ok {
		return source.KeyRevision(id)
	}
	return 0
}
func (s *store) putAtRevision(id string, entry stored, version uint64) error {
	if source, ok := s.app.(appVersionedSource); ok {
		raw, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		if !source.SetAtRevision(id, string(raw), version) {
			return errors.New("connection was cancelled or replaced")
		}
		return nil
	}
	return s.put(id, entry)
}
