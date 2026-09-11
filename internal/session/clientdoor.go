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
	id         string
	key        string
	address    string
	door       string
	overflow   string
	planPaused string
	optional   bool
}

// modelClientPool is the account boundary shared by a conversation and every
// production child it creates. The whole account is the key: source identity,
// bearer, bound billing door, overflow answer, and whether a blank bearer is
// an explicit capability. The policy fields make a live settings change mint
// a client with the new spending answer instead of reusing the old adapter.
// Sharing the pool keeps a task worker from falling back to whichever adapter
// its parent happened to be using when the worker was constructed.
type modelClientPool struct {
	mu      sync.Mutex
	config  Config
	seat    string
	clients map[modelAccount]Completer
}

func newModelClientPool(config Config, model string, client Completer) *modelClientPool {
	pool := &modelClientPool{config: config, seat: strings.TrimSpace(model), clients: make(map[modelAccount]Completer)}
	pool.clients[accountFor(config, model)] = client
	return pool
}

// seatedModel is THE ONE PLACE A MODEL BECOMES A DIFFERENT MODEL, and it
// exists for one state: a profile with no default-provider key and a connected
// service carrying the conversation. ANY PART A SERVICE CANNOT FILL FALLS TO
// THE MODEL ALREADY IN THE SEAT. The default service must be the set's first
// member, because an unqualified model falls to that member; resolving both
// names before changing either keeps the address, bearer and wire slug together
// in one call.
// The caller holds p.mu so the live seat and service set are one snapshot.
func (p *modelClientPool) seatedModel(model string) string {
	model = strings.TrimSpace(model)
	seat := strings.TrimSpace(p.seat)
	if seat == "" || seat == model {
		return model
	}
	services := p.config.Sources.OrDefault(p.config.APIKey, p.config.BaseURL)
	if !strings.EqualFold(services.Default().Source.ID, modelsource.DefaultID) {
		return model
	}
	service, _ := services.For(model)
	if !strings.EqualFold(service.Source.ID, modelsource.DefaultID) || modelServiceCanAnswer(service) {
		return model
	}
	seated, _ := services.For(seat)
	if seated.Source.ID == "" || strings.EqualFold(seated.Source.ID, modelsource.DefaultID) || !modelServiceCanAnswer(seated) {
		return model
	}
	return seat
}

func modelServiceCanAnswer(service modelsource.Connected) bool {
	return strings.TrimSpace(service.Key) != "" || service.Source.KeyOptional
}

// setSeat moves the one live fallback beside the source snapshot. A model
// chosen after launch must carry the next turn; construction-time config is a
// receipt of how the conversation opened, not an answer about where it sits.
func (p *modelClientPool) setSeat(model string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.seat = strings.TrimSpace(model)
	p.mu.Unlock()
}

// clientFor resolves model and its wire slug from one snapshot, then reuses or
// constructs the adapter for that complete account. Construction opens no
// connection, so holding the lock prevents two simultaneous children from
// minting two adapters and, more importantly, two competing lane-prober seams.
func (p *modelClientPool) clientFor(model string) (Completer, string, string, modelAccount, error) {
	if p == nil {
		return nil, "", "", modelAccount{}, errNoCompleter
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	model = p.seatedModel(model)
	configured := p.config.clientConfig(model, providerTimeout)
	want := accountFor(p.config, model)
	if client := p.clients[want]; client != nil {
		return client, configured.Model, model, want, nil
	}
	client, err := newProviderClient(p.config, model)
	if err != nil {
		return nil, "", model, modelAccount{}, err
	}
	p.clients[want] = client
	return client, configured.Model, model, want, nil
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
	overflow := ""
	if service.Overflow != nil {
		overflow = strings.TrimSpace(service.Overflow.Address)
	}
	return modelAccount{
		id: strings.ToLower(strings.TrimSpace(service.Source.ID)), key: service.Key,
		address: strings.TrimSpace(service.Address), door: strings.TrimSpace(service.Door.ID),
		overflow: overflow, planPaused: strings.TrimSpace(service.PlanPaused), optional: service.Source.KeyOptional,
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

// installSessionClient is the construction-time write of the completer every
// request from this agent crosses. Keeping the field behind this file means no
// caller can hand the raw conversation adapter to code that later pins another
// account's model onto it.
func (a *Agent) installSessionClient(client Completer, config Config) {
	a.client = sessionCompleter{
		inner:    client,
		cacheKey: a.cacheKey,
		// A task child waits out provider pacing while a watched conversation
		// does not. The wrapper is per Agent even when the adapter underneath is
		// shared, so this posture cannot leak from one side to the other.
		patient: config.InTask,
		pacing:  config.pacing,
		// The field is spelled as the OFF state so a zero Config keeps the
		// reply guard. Stamping it on the per-Agent wrapper also keeps one
		// child's choice from changing another request on the shared adapter.
		unguarded: config.ReplyGuardOff,
	}
}

// manageClient gives a public session the account pool its construction-time
// adapter belongs to. It runs before the Agent is published, so no lock is
// needed around these initial facts.
func (a *Agent) manageClient(config Config) {
	a.managedClient = true
	a.clientAccount = accountFor(config, config.Model)
	initial := a.client
	if wrapper, ok := initial.(sessionCompleter); ok {
		initial = wrapper.inner
	}
	a.clientPool = newModelClientPool(config, config.Model, initial)
}

// childClient resolves the adapter a new production child starts with. The raw
// fallback exists only for the scripted-completer seam used by tests; a managed
// session always answers from the account pool before the child is constructed.
func (a *Agent) childClient(config Config) (Completer, modelAccount, *modelClientPool, bool, error) {
	if a == nil {
		return nil, modelAccount{}, nil, false, errNoCompleter
	}
	a.mu.Lock()
	managed, pool, current := a.managedClient, a.clientPool, a.client
	a.mu.Unlock()
	if !managed || pool == nil {
		return unwrapCompleter(current), modelAccount{}, nil, false, nil
	}
	client, _, _, resolved, err := pool.clientFor(config.Model)
	return client, resolved, pool, true, err
}

// setDefaultClientKey updates the adapter behind an existing first-run
// session. The caller holds a.mu while it updates the matching config facts.
func (a *Agent) setDefaultClientKey(key string) error {
	inner := unwrapCompleter(a.client)
	if a.managedClient && a.clientPool != nil {
		return a.clientPool.setDefaultKey(key)
	}
	if keyed, ok := inner.(interface{ SetAPIKey(string) error }); ok {
		return keyed.SetAPIKey(key)
	}
	return nil
}

// setClientCacheKeyLocked moves a fork hand's request wrapper to its new
// lineage. The caller holds a.mu while it updates the Agent's matching key.
func (a *Agent) setClientCacheKeyLocked(key string) {
	if wrapper, ok := a.client.(sessionCompleter); ok {
		wrapper.cacheKey = key
		a.client = wrapper
	}
}

// hasClient reports whether this Agent has any request road at all.
func (a *Agent) hasClient() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.client != nil
}

// modelRoutingCompleter is the only completer view of a live Agent that may
// leave this file. A caller may pin any model onto it; the wrapper reads that
// choice and resolves the model's account before forwarding the request.
type modelRoutingCompleter struct{ agent *Agent }

func (c modelRoutingCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		if err := option(&request); err != nil {
			return nil, err
		}
	}
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = c.agent.Model()
	}
	return c.agent.completeWithModel(ctx, messages, model, options...)
}

// routedCompleter returns a completer safe to hand to a package that chooses a
// model later. It returns nil when there is no underlying request road, keeping
// conditional tools absent rather than installing a wrapper that always fails.
func (a *Agent) routedCompleter() Completer {
	if !a.hasClient() {
		return nil
	}
	return modelRoutingCompleter{agent: a}
}

// fallbackModels reads the adapter's own ordered chain without exposing that
// adapter to the caller.
func (a *Agent) fallbackModels(model string) []string {
	models, _ := a.modelFallbackChain(model)
	return models
}

// modelFallbackChain reads the optional model-chain capability behind the
// client door. The bool preserves #858's distinction between a completer that
// offers an empty bounded chain and one that has no chain capability at all,
// without letting the turn loop bypass account-aware client routing.
func (a *Agent) modelFallbackChain(model string) ([]string, bool) {
	if a == nil {
		return nil, false
	}
	a.mu.Lock()
	client := a.client
	a.mu.Unlock()
	chain, ok := client.(modelChain)
	if !ok {
		return nil, false
	}
	return chain.FallbackModels(model), true
}

// probeClientLanes asks the optional prober without exposing the conversation
// completer. False means this completer has no probing DOOR at all — a test
// double, a refusing completer, a build wired to no router.
//
// TRUE IS NOT "A PROBE WAS BOUGHT" AND NEVER WAS. Every gate that could refuse
// one is inside the call — the speed guard, the routing row, the pool's own
// pacing, the prober's budget, a frontier with nothing on it — and this returns
// before any of them is asked. What it reports is whether there was anything to
// ask (sessionCompleter forwards, so a built agent always has one).
func (a *Agent) probeClientLanes(ctx context.Context, model string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	client := a.client
	a.mu.Unlock()
	prober, ok := client.(laneProber)
	if !ok {
		return false
	}
	prober.ProbeLanes(ctx, model)
	return true
}

// rebindClientLocked moves a managed session to the account its current model
// names. On a construction failure it installs a refusing completer instead of
// retaining the old client: no later request may escape through a removed
// service merely because its replacement could not be built.
func (a *Agent) rebindClientLocked(model string) {
	if !a.managedClient {
		return
	}
	inner, _, _, want, err := a.clientPool.clientFor(model)
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
func (a *Agent) completerFor(model string) (Completer, string, string, error) {
	if a == nil {
		return nil, "", "", errNoCompleter
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		return nil, "", "", errNoCompleter
	}
	if !a.managedClient {
		configured := a.config.clientConfig(model, providerTimeout)
		return a.client, configured.Model, model, nil
	}
	inner, wire, called, want, err := a.clientPool.clientFor(model)
	if err != nil {
		return nil, "", called, err
	}
	if want == a.clientAccount {
		return a.client, wire, called, nil
	}
	if wrapper, ok := a.client.(sessionCompleter); ok {
		wrapper.inner = inner
		return wrapper, wire, called, nil
	}
	return inner, wire, called, nil
}

// completeWithModel is [Agent.completerFor] joined to the one wire-model
// option. Keeping the two operations inseparable makes it impossible to change
// a slug while accidentally retaining another service's address and bearer.
func (a *Agent) completeWithModel(ctx context.Context, messages []ai.Message, model string, options ...ai.Option) (*ai.Response, error) {
	response, _, err := a.completeWithNamedModel(ctx, messages, model, options...)
	return response, err
}

// completeWithNamedModel keeps the model that answered attached to the same
// resolution that chose its account. Errand receipts need that identity when a
// missing default-service key moved the call onto the live seat; asking the pool
// a second time afterwards could observe a different model or source set.
func (a *Agent) completeWithNamedModel(ctx context.Context, messages []ai.Message, model string, options ...ai.Option) (*ai.Response, string, error) {
	client, wire, called, err := a.completerFor(model)
	if err != nil {
		return nil, called, err
	}
	response, err := client.CompleteWithMessages(ctx, messages, append(options, ai.WithModel(wire))...)
	return response, called, err
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
