package session

import (
	"context"
	"strings"
	"sync"
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

// modelClientPool is the account boundary shared by a conversation and every
// production child it creates. The whole account is the key: source identity,
// bearer, address, and whether a blank bearer is an explicit capability.
// Sharing the pool keeps a task worker from falling back to whichever adapter
// its parent happened to be using when the worker was constructed.
type modelClientPool struct {
	mu      sync.Mutex
	config  Config
	clients map[modelAccount]Completer
}

func newModelClientPool(config Config, model string, client Completer) *modelClientPool {
	pool := &modelClientPool{config: config, clients: make(map[modelAccount]Completer)}
	pool.clients[accountFor(config, model)] = client
	return pool
}

// clientFor resolves model and its wire slug from one snapshot, then reuses or
// constructs the adapter for that complete account. Construction opens no
// connection, so holding the lock prevents two simultaneous children from
// minting two adapters and, more importantly, two competing lane-prober seams.
func (p *modelClientPool) clientFor(model string) (Completer, string, modelAccount, error) {
	if p == nil {
		return nil, "", modelAccount{}, errNoCompleter
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	configured := p.config.clientConfig(model, providerTimeout)
	want := accountFor(p.config, model)
	if client := p.clients[want]; client != nil {
		return client, configured.Model, want, nil
	}
	client, err := newProviderClient(p.config, model)
	if err != nil {
		return nil, "", modelAccount{}, err
	}
	p.clients[want] = client
	return client, configured.Model, want, nil
}

// setSources moves the pool to a freshly resolved profile and forgets every
// adapter whose complete account is no longer present. Before an evicted
// provider can remain reachable through a call already holding its interface,
// its bearer is cleared; a removed key may not leave the process again.
func (p *modelClientPool) setSources(sources modelsource.Set) {
	if p == nil || sources.Empty() {
		return
	}
	p.mu.Lock()
	p.config.Sources = sources
	keep := make(map[modelAccount]bool)
	for _, service := range sources.All() {
		keep[accountForService(service)] = true
	}
	var evicted []Completer
	for account, client := range p.clients {
		if !keep[account] {
			delete(p.clients, account)
			evicted = append(evicted, client)
		}
	}
	p.mu.Unlock()
	for _, client := range evicted {
		if keyed, ok := unwrapCompleter(client).(interface{ SetAPIKey(string) error }); ok {
			_ = keyed.SetAPIKey("")
		}
	}
}

func (p *modelClientPool) setDefaultKey(key string) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	old := accountForService(p.config.Sources.OrDefault(p.config.APIKey, p.config.BaseURL).Default())
	p.config.APIKey = key
	p.config.Sources = p.config.Sources.WithDefaultKey(key)
	next := accountForService(p.config.Sources.OrDefault(p.config.APIKey, p.config.BaseURL).Default())
	current := p.clients[old]
	if current != nil {
		delete(p.clients, old)
	}
	p.mu.Unlock()
	if current == nil {
		return nil
	}
	inner := unwrapCompleter(current)
	if keyed, ok := inner.(interface{ SetAPIKey(string) error }); ok {
		if err := keyed.SetAPIKey(key); err != nil {
			return err
		}
	}
	p.mu.Lock()
	p.clients[next] = inner
	p.mu.Unlock()
	return nil
}

func accountFor(config Config, model string) modelAccount {
	service := config.serviceFor(model)
	return accountForService(service)
}

func accountForService(service modelsource.Connected) modelAccount {
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
	inner, _, want, err := a.clientPool.clientFor(model)
	if want == a.clientAccount {
		return
	}
	if err != nil {
		inner = unavailableCompleter{err: err}
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
	if !a.managedClient {
		configured := a.config.clientConfig(model, providerTimeout)
		return a.client, configured.Model, nil
	}
	inner, wire, want, err := a.clientPool.clientFor(model)
	if err != nil {
		return nil, "", err
	}
	if want == a.clientAccount {
		return a.client, wire, nil
	}
	if wrapper, ok := a.client.(sessionCompleter); ok {
		wrapper.inner = inner
		return wrapper, wire, nil
	}
	return inner, wire, nil
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
