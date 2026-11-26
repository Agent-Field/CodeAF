package connect

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// AppSourceStore keeps app source credentials in the same private, atomic
// profile store as the other connectors. Its namespace is distinct from plug IDs.
type AppSourceStore struct {
	Profile string
}

const appSourcePrefix = "app-source/"

func appSourceCredential(id string) string { return appSourcePrefix + id + "/credential" }

func (s AppSourceStore) Register(id string, definition json.RawMessage, key, person string) error {
	ref := appSourceCredential(id)
	if person != "" {
		ref = personSourceCredential(id, person)
	}
	entry := appSourcePrefix + id
	exists := false
	err := newStore(s.Profile).replace(entry, func(entries map[string]stored) {
		if _, exists = entries[entry]; exists {
			return
		}
		entries[entry] = stored{Source: definition}
		if key == "" {
			return
		}
		if person == "" {
			entries[entry] = stored{Source: definition, SecretRef: ref}
		}
		if key != "" {
			entries[ref] = stored{Auth: AuthKey, Key: key}
		}
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

func personSourceCredential(id, person string) string {
	return fmt.Sprintf("%s%s/person/%x", appSourcePrefix, id, sha256.Sum256([]byte(person)))
}

// SetPersonCredential replaces only the credential of the authenticated person.
func (s AppSourceStore) SetPersonCredential(id, person, key string) error {
	if person == "" {
		return errors.New("a personal source credential needs a person")
	}
	if err := newStore(s.Profile).replace(personSourceCredential(id, person), func(entries map[string]stored) {
		entries[personSourceCredential(id, person)] = stored{Auth: AuthKey, Key: key}
	}); err != nil {
		return errors.New("the source credential could not be saved")
	}
	return nil
}

func (s AppSourceStore) PersonCredential(id, person string) (string, bool) {
	if person == "" {
		return "", false
	}
	entries, err := newStore(s.Profile).load()
	entry := entries[personSourceCredential(id, person)]
	return entry.Key, err == nil && entry.Auth == AuthKey && entry.Key != ""
}

// ChangeDefinition replaces a declaration only if the operator edited the
// version it originally read. A concurrent change must be retried.
func (s AppSourceStore) ChangeDefinition(id string, previous, updated json.RawMessage) error {
	ref := appSourcePrefix + id
	changed := false
	err := newStore(s.Profile).replace(ref, func(entries map[string]stored) {
		entry, ok := entries[ref]
		if ok && bytes.Equal(entry.Source, previous) {
			entry.Source = updated
			entries[ref] = entry
			changed = true
		}
	})
	if err != nil {
		return errors.New("the source could not be saved in the profile")
	}
	if !changed {
		return errors.New("the connection changed; try again")
	}
	return nil
}
