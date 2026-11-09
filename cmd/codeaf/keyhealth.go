package main

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
)

const readinessTimeout = 2 * time.Second
const invalidOpenRouterKey = "Your OpenRouter key stopped working. Sign in again."

// The cache is per launch and credential. A late probe cannot overwrite a new
// key's reading, or a refusal observed while the probe was in flight.
type keyHealthCache struct {
	mu           sync.Mutex
	key, address string
	generation   uint64
	value        appwire.ProviderKeyHealth
}

func (c *keyHealthCache) read() appwire.ProviderKeyHealth {
	c.mu.Lock()
	defer c.mu.Unlock()
	value := c.value
	if value.State == "" {
		value.State = "unknown"
	}
	return value
}

func (c *keyHealthCache) start(client *http.Client, address, key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation != 0 && c.key == key && c.address == address {
		return
	}
	c.key, c.address = key, address
	c.generation++
	generation := c.generation
	c.value = appwire.ProviderKeyHealth{State: "unknown"}
	if key == "" {
		return
	}
	guard.Go("setup/key-health", func() {
		value := probeKeyHealth(client, address, key)
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.generation == generation {
			c.value = value
		}
	})
}

func probeKeyHealth(client *http.Client, address, key string) appwire.ProviderKeyHealth {
	value := appwire.ProviderKeyHealth{State: "unknown"}
	if key == "" {
		value.Message = "No provider key is set. Connect a provider."
		return value
	}
	ctx, cancel := context.WithTimeout(context.Background(), readinessTimeout)
	defer cancel()
	check := appCheckKeyAuth(ctx, client, address, key)
	status, _ := check.Status.(int)
	kind, _ := provider.RouteFailureOf(&provider.APIError{Status: status})
	now := time.Now()
	value.CheckedAt = &now
	switch {
	case check.OK:
		value.State = "ok"
	case kind == provider.RouteAuth || kind == provider.RouteForbidden:
		value.State, value.Message = "invalid", invalidOpenRouterKey
	default:
		value.Message = "Could not check your OpenRouter key. Try codeaf doctor when you are online."
	}
	return value
}

func (c *keyHealthCache) invalid(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if key == "" || key != c.key {
		return
	}
	c.generation++
	now := time.Now()
	c.value = appwire.ProviderKeyHealth{State: "invalid", Message: invalidOpenRouterKey, CheckedAt: &now}
}

func (b *appBackend) startKeyHealth() {
	b.locked(func() { b.monitorKeyHealth = true })
	address := strings.TrimSpace(config.DefaultBaseURL)
	if settings, err := config.LoadKeyless(); err == nil {
		address = settings.BaseURL
	}
	if b.keyAddress != nil {
		address = b.keyAddress(modelsource.DefaultID)
	}
	key, handed := b.keys.Key(modelsource.DefaultID)
	if !handed {
		key = config.APIKeyAt(config.ProfileDir())
	}
	b.keyHealth.start(b.keyClient, address, key)
}

func (b *appBackend) refreshKeyHealth() {
	var enabled bool
	b.locked(func() { enabled = b.monitorKeyHealth })
	if enabled {
		b.startKeyHealth()
	}
}

// Capture the credential at the turn boundary so an old turn cannot invalidate
// a replacement key. Other providers' refusals do not describe OpenRouter.
func (b *appBackend) openRouterTurnKey(model string) string {
	settings, err := config.LoadKeyless()
	if err != nil {
		return ""
	}
	service, _ := settings.Sources.OrDefault(settings.APIKey, settings.BaseURL).For(model)
	if service.Source.ID != modelsource.DefaultID {
		return ""
	}
	if key, handed := b.keys.Key(modelsource.DefaultID); handed {
		return key
	}
	return service.Key
}
