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
	c.change(func() { c.keys[provider] = key })
}

// Remove disconnects one provider for the rest of this launch.
func (c *AppCredentials) Remove(provider string) {
	c.change(func() { c.keys[strings.ToLower(strings.TrimSpace(provider))] = "" })
}

// change makes one change to the keys held. A key handed over is a credential
// change: every cached reading of the profile must see it, and, for the keys
// in force, route health records it as it is made ([crewCredentialWrite]),
// because it holds refusals against credentials.
func (c *AppCredentials) change(edit func()) {
	apply := func() error {
		c.mu.Lock()
		edit()
		c.mu.Unlock()
		return nil
	}
	if appCredentials.Load() == c {
		_ = crewCredentialWrite(ProfileDir(), apply)
	} else {
		_ = apply()
	}
	bumpSettingsGeneration()
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
// endpoint; the app's key only answers for it. A custom connection IS its
// address, so a key held for one the profile has no row for reaches nothing
// and adds nothing.
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
		if key != "" && !known[id] && id != modelsource.DefaultID && !modelsource.IsCustomID(id) {
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
// process no secret row is written, and no service row carrying a key the
// profile does not already hold. Every other setting is written as it always
// is.
//
// A ROW THAT WAS ALREADY ON DISK IS NOT A KEY REACHING IT. The service rows are
// one list written whole, so the app keeping a keyless row for one provider
// rewrites every other row beside it — and a profile the terminal also uses can
// hold a key the terminal wrote. Refusing those rows would refuse every app
// connection on such a profile while protecting nothing, so a row whose key is
// byte-identical to the one the profile already holds for that id passes —
// which is also how a ChatGPT sign-in the terminal kept, its row carrying the
// sign-in's marker for a key, survives the app's rewrite. The app's own
// sign-in writes its row with no key at all ([ConnectCodex]).
func refuseAppCredentialUpdates(held map[string]json.RawMessage, change profileChange) error {
	if !AppMode() {
		return nil
	}
	secretFields := map[string]bool{KeyAPIKey: true}
	for _, row := range NewSettings(SettingsOptions{}).Rows() {
		if row.Secret {
			secretFields[row.Key] = true
		}
	}
	for _, name := range change.removed {
		if secretFields[name] {
			return ErrAppKeys
		}
	}
	for name, raw := range change.set {
		if secretFields[name] {
			return ErrAppKeys
		}
		if name == keyModelSources {
			var rows []PersistedSource
			if json.Unmarshal(raw, &rows) != nil {
				return ErrAppKeys
			}
			keys := make(map[string]string)
			for _, row := range persistedSourcesFrom(held) {
				keys[strings.ToLower(strings.TrimSpace(row.ID))] = row.Key
			}
			for _, row := range rows {
				if row.Key == "" || row.Key == keys[strings.ToLower(strings.TrimSpace(row.ID))] {
					continue
				}
				return ErrAppKeys
			}
		}
	}
	return nil
}
