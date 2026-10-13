package config

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/codexauth"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// AppModeEnv carries only the disk-write policy to child commands. Credentials
// themselves travel over a pipe, never through the environment.
const AppModeEnv = "CODEAF_APP_MODE"

var ErrAppKeys = errors.New("keys are kept by the app")

// AppCredentials is the process-memory source handed over by the native app.
// Empty entries are deliberate removals and prevent a profile or shell key
// from silently reconnecting a provider the person just disconnected.
type AppCredentials struct {
	mu        sync.RWMutex
	keys      map[string]string
	revisions map[string]uint64
	sessions  map[string]string
}

var appCredentials atomic.Pointer[AppCredentials]

func NewAppCredentials() *AppCredentials {
	return &AppCredentials{keys: make(map[string]string), revisions: make(map[string]uint64), sessions: make(map[string]string)}
}

// UseAppCredentials installs one launch's source before any engine starts.
// The restore function also lets an embedded launch relinquish its policy.
func UseAppCredentials(source *AppCredentials) func() {
	previous := appCredentials.Swap(source)
	restoreTokens := codexauth.UseTokenStore(source)
	return func() { restoreTokens(); appCredentials.Store(previous) }
}

func AppMode() bool { return appCredentials.Load() != nil || env.Get(AppModeEnv) == "1" }

func AppCredentialSource() *AppCredentials { return appCredentials.Load() }

func (c *AppCredentials) Set(provider, key string) {
	provider, key = strings.ToLower(strings.TrimSpace(provider)), strings.TrimSpace(key)
	if provider == "codex" {
		provider = "chatgpt"
	}
	if provider == "chatgpt" && key != "" {
		var tokens codexauth.Tokens
		if json.Unmarshal([]byte(key), &tokens) != nil || tokens.AccessToken == "" || tokens.RefreshToken == "" {
			return
		}
		trace.Secret(tokens.AccessToken)
		trace.Secret(tokens.RefreshToken)
		trace.Secret(tokens.IDToken)
		redact.Register(tokens.AccessToken)
		redact.Register(tokens.RefreshToken)
		redact.Register(tokens.IDToken)
	}
	registerAppSecret(key)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys[provider] = key
	c.revisions[provider]++
}

func (c *AppCredentials) Key(provider string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "codex" {
		key, ok := c.keys["chatgpt"]
		if key != "" {
			return codexauth.Sentinel, true
		}
		return "", ok
	}
	key, ok := c.keys[provider]
	return key, ok
}

func appKey(provider string) (string, bool) {
	if source := appCredentials.Load(); source != nil {
		return source.Key(provider)
	}
	return "", false
}

func (c *AppCredentials) SetFactorySession(address, token string) {
	trace.Secret(token)
	redact.Register(token)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessions[address] = token
}

func (c *AppCredentials) FactorySession(address string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessions[address]
}

// Snapshot returns copies for the inherited pipe. No writer persists them.
func (c *AppCredentials) Snapshot() (map[string]string, map[string]string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	keys, sessions := make(map[string]string, len(c.keys)), make(map[string]string, len(c.sessions))
	for id, key := range c.keys {
		keys[id] = key
	}
	for address, token := range c.sessions {
		sessions[address] = token
	}
	return keys, sessions
}

// appSourceRows adds memory-only connections without changing config.json.
// Persisted rows still own region and endpoint choices for known services.
func appSourceRows(rows []PersistedSource) []PersistedSource {
	source := appCredentials.Load()
	if source == nil {
		return rows
	}
	keys, _ := source.Snapshot()
	known := make(map[string]bool)
	for _, row := range rows {
		known[row.ID] = true
	}
	var ids []string
	for id, key := range keys {
		if id == "chatgpt" {
			id = "codex"
		}
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

// refuseAppCredentialUpdates guards the common profile writer, including
// settings and provider rows. Unrelated writes preserve existing TUI keys.
func refuseAppCredentialUpdates(updates map[string]any) error {
	if !AppMode() {
		return nil
	}
	secretFields := map[string]bool{KeyAPIKey: true, KeyGitHubToken: true}
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

// LoadTokens supplies the ChatGPT token bundle handed over by the native store.
func (c *AppCredentials) LoadTokens() (codexauth.Tokens, error) {
	key, _ := c.Key("chatgpt")
	if key == "" {
		return codexauth.Tokens{}, os.ErrNotExist
	}
	var tokens codexauth.Tokens
	if err := json.Unmarshal([]byte(key), &tokens); err != nil {
		return tokens, errors.New("invalid ChatGPT credential")
	}
	return tokens, nil
}

// CompareAndSwapTokens keeps a late refresh from undoing a newer connection choice.
func (c *AppCredentials) CompareAndSwapTokens(previous, next codexauth.Tokens) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var current codexauth.Tokens
	if json.Unmarshal([]byte(c.keys["chatgpt"]), &current) != nil || current != previous {
		return codexauth.ErrSignInExpired
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	trace.Secret(next.AccessToken)
	trace.Secret(next.RefreshToken)
	trace.Secret(next.IDToken)
	redact.Register(next.AccessToken)
	redact.Register(next.RefreshToken)
	redact.Register(next.IDToken)
	c.keys["chatgpt"] = string(raw)
	return nil
}

// KeyRevision prevents a completed browser callback from undoing a later disconnect.
func (c *AppCredentials) KeyRevision(provider string) uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "codex" {
		provider = "chatgpt"
	}
	return c.revisions[provider]
}
func (c *AppCredentials) SetAtRevision(provider, key string, version uint64) bool {
	provider, key = strings.ToLower(strings.TrimSpace(provider)), strings.TrimSpace(key)
	registerAppSecret(key)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.revisions[provider] != version {
		return false
	}
	c.keys[provider] = key
	c.revisions[provider]++
	return true
}

// A browser grant is an opaque Keychain payload; its individual bearer bytes need the same redaction as a pasted key.
func registerAppSecret(raw string) {
	trace.Secret(raw)
	redact.Register(raw)
	var grant struct {
		Key  string `json:"key"`
		Keys *struct {
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
		} `json:"keys"`
	}
	if json.Unmarshal([]byte(raw), &grant) != nil {
		return
	}
	trace.Secret(grant.Key)
	redact.Register(grant.Key)
	if grant.Keys != nil {
		trace.Secret(grant.Keys.Access)
		redact.Register(grant.Keys.Access)
		trace.Secret(grant.Keys.Refresh)
		redact.Register(grant.Keys.Refresh)
	}
}
