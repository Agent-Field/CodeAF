package config

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// ErrAppKeys is the refusal a profile write meets when it would put a key on
// disk in a process the desktop app started: the app keeps keys in the
// person's keychain and hands them over at every launch.
var ErrAppKeys = errors.New("keys are kept by the app")

// AppCredentials are the keys the desktop app handed this process, held in
// memory and nowhere else. AN EMPTY ENTRY IS A REMOVAL AND NOT AN ABSENCE: a
// provider the person just disconnected must not quietly reconnect itself from
// a key in the profile or the shell.
type AppCredentials struct {
	mu   sync.RWMutex
	keys map[string]string
}

var appCredentials atomic.Pointer[AppCredentials]

// NewAppCredentials is an empty set of app keys.
func NewAppCredentials() *AppCredentials {
	return &AppCredentials{keys: make(map[string]string)}
}

// UseAppCredentials installs the keys every key reader consults first, before
// any conversation starts, and answers how to take them away again.
func UseAppCredentials(source *AppCredentials) func() {
	previous := appCredentials.Swap(source)
	return func() { appCredentials.Store(previous) }
}

// AppMode says this process is the desktop app's, so keys never reach disk.
func AppMode() bool { return appCredentials.Load() != nil }

// AppCredentialSource is the installed app keys, or nil outside the app.
func AppCredentialSource() *AppCredentials { return appCredentials.Load() }

// Set holds one provider's key, and registers it with the redactor and the
// trace scrubber so no log line ever carries it.
func (c *AppCredentials) Set(provider, key string) {
	provider, key = strings.ToLower(strings.TrimSpace(provider)), strings.TrimSpace(key)
	trace.Secret(key)
	redact.Register(key)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys[provider] = key
}

// Remove disconnects one provider for the rest of this launch.
func (c *AppCredentials) Remove(provider string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys[strings.ToLower(strings.TrimSpace(provider))] = ""
}

// Key answers one provider's key and whether the app has said anything about
// it; an empty key that is known is a removal.
func (c *AppCredentials) Key(provider string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	key, ok := c.keys[strings.ToLower(strings.TrimSpace(provider))]
	return key, ok
}

func appKey(provider string) (string, bool) {
	if source := appCredentials.Load(); source != nil {
		return source.Key(provider)
	}
	return "", false
}

// Snapshot is a copy of the keys by provider. Nothing writes it anywhere.
func (c *AppCredentials) Snapshot() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	keys := make(map[string]string, len(c.keys))
	for id, key := range c.keys {
		keys[id] = key
	}
	return keys
}

// appSourceRows adds the services the app holds a key for to the persisted
// rows, without touching config.json. A persisted row still owns its region and
// endpoint; the app's key only answers for it.
func appSourceRows(rows []PersistedSource) []PersistedSource {
	source := appCredentials.Load()
	if source == nil {
		return rows
	}
	keys := source.Snapshot()
	known := make(map[string]bool)
	for _, row := range rows {
		known[row.ID] = true
	}
	var ids []string
	for id, key := range keys {
		if key != "" && !known[id] && id != modelsource.DefaultID {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	rows = append([]PersistedSource(nil), rows...)
	for _, id := range ids {
		rows = append(rows, PersistedSource{ID: id, Written: id})
	}
	return rows
}

// refuseAppCredentialUpdates guards the one profile writer: in the app's
// process no secret row and no service row carrying a key is written. Every
// other setting is written as it always is.
func refuseAppCredentialUpdates(updates map[string]any) error {
	if !AppMode() {
		return nil
	}
	secretFields := map[string]bool{KeyAPIKey: true}
	for _, row := range NewSettings(SettingsOptions{}).Rows() {
		if row.Secret {
			secretFields[row.Key] = true
		}
	}
	for name, value := range updates {
		if secretFields[name] {
			return ErrAppKeys
		}
		if name == keyModelSources {
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			var rows []PersistedSource
			if json.Unmarshal(raw, &rows) != nil {
				return ErrAppKeys
			}
			for _, row := range rows {
				if row.Key != "" {
					return ErrAppKeys
				}
			}
		}
	}
	return nil
}
