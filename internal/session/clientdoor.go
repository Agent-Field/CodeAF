package session

import (
	"context"
	"strings"
	"time"

	account "github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/modelsource"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type modelAccount struct {
	id       string
	key      string
	address  string
	optional bool
}

func accountFor(config Config, model string) modelAccount {
	service := config.serviceFor(model)
	return modelAccount{
		id: strings.ToLower(strings.TrimSpace(service.Source.ID)), key: service.Key,
		address: strings.TrimSpace(service.Address), optional: service.Source.KeyOptional,
	}
}

// newProviderClient is construction shared by New and a live cross-service
// switch. The same routing and fallback seams must survive replacement; a
// second, smaller constructor would silently change how the next turn runs.
func newProviderClient(config Config, model string) (*provider.Client, error) {
	settings := config.clientConfig(model, providerTimeout)
	settings.Routing = provider.StaticRouting(config.Routing)
	settings.Fallbacks = config.ModelFallbacks
	settings.NearestModels = config.NearestModels
	client, err := provider.NewClient(settings)
	if err != nil {
		return nil, err
	}
	provider.InstallLaneProber(client, func(string) bool { return someoneIsWatching() })
	return client, nil
}

// rebindClientLocked moves a managed session to the account its current model
// names. On a construction failure it installs a refusing completer instead of
// retaining the old client: no later request may escape through a removed
// service merely because its replacement could not be built.
func (a *Agent) rebindClientLocked(model string) {
	if !a.managedClient {
		return
	}
	want := accountFor(a.config, model)
	if want == a.clientAccount {
		return
	}
	inner, ok := a.clientsByAccount[want]
	if !ok {
		client, err := newProviderClient(a.config, model)
		if err != nil {
			inner = unavailableCompleter{err: err}
		} else {
			inner = client
		}
		if a.clientsByAccount == nil {
			a.clientsByAccount = make(map[modelAccount]Completer)
		}
		a.clientsByAccount[want] = inner
	}
	if wrapper, ok := a.client.(sessionCompleter); ok {
		wrapper.inner = inner
		a.client = wrapper
	} else {
		a.client = inner
	}
	a.clientAccount = want
}

// completerFor resolves the account from the model being called, not from the
// model the conversation happens to be using. The retained client is reused
// when those are the same account; another service gets one cached adapter of
// its own, wrapped with this session's cache lineage and patience.
//
// THIS IS THE ONLY DOOR THAT MAY PUT A MODEL OVERRIDE ON A SESSION REQUEST. An
// override changes only the slug in the request; choosing the client here first
// is what changes the address and bearer with it.
func (a *Agent) completerFor(model string) (Completer, string, error) {
	if a == nil {
		return nil, "", errNoCompleter
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		return nil, "", errNoCompleter
	}
	configured := a.config.clientConfig(model, providerTimeout)
	if !a.managedClient {
		return a.client, configured.Model, nil
	}
	want := accountFor(a.config, model)
	if want == a.clientAccount {
		return a.client, configured.Model, nil
	}
	inner, ok := a.clientsByAccount[want]
	if !ok {
		client, err := newProviderClient(a.config, model)
		if err != nil {
			return nil, "", err
		}
		inner = client
		if a.clientsByAccount == nil {
			a.clientsByAccount = make(map[modelAccount]Completer)
		}
		a.clientsByAccount[want] = inner
	}
	if wrapper, ok := a.client.(sessionCompleter); ok {
		wrapper.inner = inner
		return wrapper, configured.Model, nil
	}
	return inner, configured.Model, nil
}

// completeWithModel is [Agent.completerFor] joined to the one wire-model
// option. Keeping the two operations inseparable makes it impossible to change
// a slug while accidentally retaining another service's address and bearer.
func (a *Agent) completeWithModel(ctx context.Context, messages []ai.Message, model string, options ...ai.Option) (*ai.Response, error) {
	client, wire, err := a.completerFor(model)
	if err != nil {
		return nil, err
	}
	return client.CompleteWithMessages(ctx, messages, append(options, ai.WithModel(wire))...)
}

type unavailableCompleter struct{ err error }

func (u unavailableCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	return nil, u.err
}

// clientConfig is the ONE place this package turns a session's account into
// provider settings. internal/config assembles the key, the base URL and the
// level split; this adds what THIS session owns — how long a call may take and
// what its own catalog publishes — and nothing here ever spells an APIKey or a
// BaseURL field again.
func (c Config) clientConfig(model string, timeout time.Duration) provider.Config {
	configured := account.ClientConfigFor(c.Sources.OrDefault(c.APIKey, c.BaseURL), model)
	configured.Timeout = timeout
	// The catalog gate on optional knobs, and the two answers about the model's
	// reasoning profile and list price, are seams the surface resolves. A caller
	// that hands over none of them keeps today's behaviour exactly — knobs travel
	// only when explicit and nobody invents a price or a reasoning shape.
	configured.SupportsParameter = c.SupportsParameter
	configured.ReasoningProfile = c.ReasoningProfile
	configured.ModelPrice = c.ModelPrice
	return configured
}

// serviceFor resolves the service facts needed by refusal paths that do not
// otherwise construct a client.
func (c Config) serviceFor(model string) modelsource.Connected {
	service, _ := c.Sources.OrDefault(c.APIKey, c.BaseURL).For(model)
	return service
}

// wireModel removes the service segment from an id before a per-call model
// option can override the client's already-resolved default.
func (c Config) wireModel(model string) string {
	return c.clientConfig(model, 0).Model
}

// documentConfig is [Config.clientConfig] for the read_document path, with the
// seat pin dropped by internal/config rather than here. The reason belongs
// beside config.DocumentClient, which clears it for exactly this call shape,
// and this package's effort law keeps the adapter's own effort words out of a
// file that is not choosing a depth.
func (c Config) documentConfig(timeout time.Duration) provider.Config {
	return account.WithoutSeatPin(c.clientConfig(c.Model, timeout))
}
