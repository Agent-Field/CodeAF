package connect

import (
	"encoding/json"
	"errors"
	"strings"
)

// AppSourceStore keeps app source credentials in the same private, atomic
// profile store as the other connectors. Its namespace is distinct from plug IDs.
type AppSourceStore struct {
	Profile string
}

const appSourcePrefix = "app-source/"

func appSourceCredential(id string) string { return appSourcePrefix + id + "/credential" }

func (s AppSourceStore) Register(id string, definition json.RawMessage, key string) error {
	ref := appSourceCredential(id)
	entry := appSourcePrefix + id
	exists := false
	err := newStore(s.Profile).replace(entry, func(entries map[string]stored) {
		if _, exists = entries[entry]; exists {
			return
		}
		entries[entry] = stored{Source: definition, SecretRef: ref}
		entries[ref] = stored{Auth: AuthKey, Key: key}
	})
	if err != nil {
		return errors.New("the source could not be saved in the profile")
	}
	if exists {
		return errors.New("this connection already exists")
	}
	return nil
}

// Definitions returns the operator's declarations without resolving credentials.
func (s AppSourceStore) Definitions() (map[string]json.RawMessage, error) {
	entries, err := newStore(s.Profile).load()
	if err != nil {
		return nil, errors.New("the profile's connections could not be read")
	}
	out := make(map[string]json.RawMessage)
	for id, entry := range entries {
		if name, ok := strings.CutPrefix(id, appSourcePrefix); ok && entry.Source != nil {
			out[name] = entry.Source
		}
	}
	return out, nil
}

// Credential resolves only a registered app source's private reference.
func (s AppSourceStore) Credential(id string) (string, error) {
	entries, err := newStore(s.Profile).load()
	if err == nil {
		ref := entries[appSourcePrefix+id].SecretRef
		if ref == appSourceCredential(id) {
			if entry, ok := entries[ref]; ok && entry.Auth == AuthKey && entry.Key != "" {
				return entry.Key, nil
			}
		}
	}
	return "", errors.New("the connection credential is unavailable")
}
