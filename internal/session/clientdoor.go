package session

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	account "github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
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

// routingInForce is the row THIS SESSION'S calls answer to: its own where a
// caller handed one down, and the row installed in this process otherwise.
//
// It is the same rule the adapter applies one layer down
// ([provider.Client.routingChoice]) and it is stated here because the gates on
// this side have to agree with it — a lane beat that stayed running because a
// launch snapshot said `latency` would be measuring endpoints for a person who
// has since turned routing off.
func (c Config) routingInForce() provider.RoutingStrategy {
	if strings.TrimSpace(string(c.Routing)) != "" {
		return c.Routing
	}
	return provider.RoutingNow()
}

// newProviderClient is construction shared by New and a live cross-service
// switch. The same routing and fallback seams must survive replacement; a
// second, smaller constructor would silently change how the next turn runs.
func newProviderClient(config Config, model string) (*provider.Client, error) {
	settings := config.clientConfig(model, providerTimeout)
	// AN EMPTY ROW HERE IS NOBODY HAVING CHOSEN, and that is what a launch from
	// a profile leaves behind: the client then reads the row this process
	// installed, live, on every call ([Config.Routing] says why). A caller that
	// really carries a row of its own still hands it down and still wins.
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
	return c.agent.completeWithModel(ctx, purposeInherited, messages, model, options...)
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

// beltRunCompleter returns the completer a run's worker seats are built on. A
// `/task` under the bash belt hands the run engine this conversation's
// account-aware view, so each seat's request resolves the model its crew picked
// back through this conversation's account pool rather than pinning whichever
// adapter the conversation happened to hold.
//
// IT IS ALWAYS A VISIBLE COMPLETER, unlike [Agent.routedCompleter], which
// answers nil so a conditional tool can be absent: the run road has no such
// option — a seat with no provider is a task that cannot run — and a
// conversation whose client is truly gone is answered by a call that refuses
// rather than by a worker built on a keyless client.
func (a *Agent) beltRunCompleter() Completer {
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

// ── EVERY REQUEST SAYS WHAT IT IS FOR ───────────────────────────────────────
//
// callPurpose is what one request this package makes is FOR, in the word the
// model-call log files it under (internal/provider's [provider.WithCallTag]).
//
// IT IS AN ARGUMENT AND NOT A CONTEXT VALUE, and that is the whole of the fix.
// The tag was a context value that a caller could set or forget, and nine of the
// eleven callers of this door forgot: the guardian, vision, the shaper, the
// spell-out, the intake, the planner, the designer, the handoff draft and a
// saved program's own step all reached the wire with no tag at all. That is
// 2,309 of the 2,839 untagged finishes in the ten days to 2026-09-10, and with
// them the answer to "what was this build spending that model on all night"
// (docs/design/recovery/census-20260910.md §8, finding 9). A tag that can be
// forgotten is a tag that will be, so it is spelled where it cannot be: a call
// with no purpose does not compile.
//
// AND IT IS STAMPED HERE AND NOWHERE ELSE. The turn, the ask tool and the
// errand ladder each used to stamp their own, which is three spellings of one
// fact; they pass a purpose now and this door is the only thing in the package
// that calls [provider.WithCallTag]. nohiddenwork_test.go is the law.
type callPurpose string

// The purposes that are not a role's own name. A role-resolved call passes its
// role ([Agent.callRole]) and a tool ask passes the tool, so both are derived
// rather than listed; these are the three requests that are not either.
const (
	// purposeTurn is the person's own question, answered in their conversation.
	purposeTurn callPurpose = "turn"
	// purposeTask is the same request made inside a piece of work.
	purposeTask callPurpose = "task"
	// purposeSubharness is one AI step of a saved program, which runs on the
	// program's own model rather than on any role's (subharness_env.go).
	purposeSubharness callPurpose = "subharness"
	// purposeHandoffDraft is the ceiling's draft rung: the model that has just
	// spent the turn, asked on the turn's own transcript for the document a
	// worker will finish from (checkpoint.go). It is not [roles.RoleHandoff],
	// which is the rung BELOW it — the writer that composes a brief out of the
	// digest when this one cannot be had — and naming them alike would put the
	// dearest call on the road and its cheap understudy on the same row.
	purposeHandoffDraft callPurpose = "handoff-draft"

	// ── the three roads that carry their own client ─────────────────────────
	//
	// These do not come through [Agent.completeWithNamedModel] at all: each
	// builds a [provider.Client] of a shape the door does not make — a longer
	// timeout, static routing pinned to the parent's choice, a document config
	// with its own key rule — and completes on it directly. That is a real
	// difference and not an oversight, so the answer is not to force them
	// through the door; it is to make them say the same word the door says.
	//
	// THEY REACHED THE CALL LOG WITH NO TAG AT ALL UNTIL THIS PR, which meant the
	// two slowest unattended errands this build makes — a memory tidy-up and a
	// standing item's check, both of which run while nobody is there — were
	// indistinguishable from a turn that had lost its name. They carry the most
	// money per call of anything nobody is waiting for.

	// EVERY WORD IN THIS BLOCK IS A WORD THAT IS NOT A ROLE. Where a purpose IS a
	// role, the call site writes `callPurpose(roles.RoleX)` and no constant is
	// declared here — restating a role's string would be the same word in two
	// places, and the point of a small closed block is that what IS in it stands
	// out. The memory tidy-up is [roles.RoleConsolidate] and says so at its own
	// call; the two below are not roles at all.
	//
	// purposeSentinel is one standing item's yes-or-no on evidence somebody else
	// already gathered, run on every check of every item forever. It is the ONE
	// DELIBERATE DIVERGENCE: the call resolves on [roles.RoleSentinel], but the
	// tag is `standing-check`, because cmd/codeaf already writes `sentinel` for
	// the resident's quorum errand and two different calls under one tag is one
	// reading of neither. THE RENAME IS A HOLDING ACTION: it moves the collision
	// rather than fixing it, and it is fixed by carrying [lane.Role] on the
	// calllog record beside the tag, where the two calls are told apart by what
	// they ARE and both can spell `sentinel` again. That is issue #1010, whose
	// spine is #928.
	purposeSentinel callPurpose = "standing-check"
	// purposeDocument is a rung of the document reader — the model's own eyes on
	// a PDF the `read` tool cannot open as text. It is not a role because the
	// reader resolves its model from the document settings and never from the
	// role table (tools_doc.go).
	purposeDocument callPurpose = "document"

	// purposeInherited is the ONE PURPOSE THAT IS THE ABSENCE OF ONE, and it is
	// named so that the absence is a statement rather than a gap.
	//
	// [modelRoutingCompleter] is the view of a live agent that LEAVES this
	// package — internal/reflex and the media hands reach the wire through it —
	// and each of those callers names its own call on the context before it gets
	// here (reflex.go's "reflex"). A word stamped at this point would overwrite
	// what they said with a word about the wrapper rather than about the request,
	// so this one keeps whatever the caller already put there.
	//
	// It exists because the alternative was a law that had to KNOW about this
	// call site: a walk that tracked which function it was inside, a name to
	// compare against, and a reader for generic receivers — thirty lines of
	// apparatus to excuse one bare `""`. A named constant says the same thing in
	// the place a reader is already looking.
	purposeInherited callPurpose = ""
)

// completeWithModel is [Agent.completerFor] joined to the one wire-model
// option and to the one statement of what the call is for. Keeping the three
// inseparable makes it impossible to change a slug while accidentally retaining
// another service's address and bearer, or to reach the wire anonymously.
func (a *Agent) completeWithModel(ctx context.Context, purpose callPurpose, messages []ai.Message, model string, options ...ai.Option) (*ai.Response, error) {
	response, _, err := a.completeWithNamedModel(ctx, purpose, messages, model, options...)
	return response, err
}

// completeWithNamedModel keeps the model that answered attached to the same
// resolution that chose its account. Errand receipts need that identity when a
// missing default-service key moved the call onto the live seat; asking the pool
// a second time afterwards could observe a different model or source set.
func (a *Agent) completeWithNamedModel(ctx context.Context, purpose callPurpose, messages []ai.Message, model string, options ...ai.Option) (*ai.Response, string, error) {
	// AN AUXILIARY CALL NEVER ASKS A ROUTE HEALTH SAYS WILL NOT ANSWER
	// (taskcrew.go's [Agent.healthyModel]).
	model = a.healthyModel(ctx, purpose, model)
	// A HELPER'S CALL IS PRICED LIKE A SEAT'S: against the crew's day cap
	// before it is made, and on the day after. A crew seat's own call is its
	// task's guard's (taskcrew.go), and the person's turn is theirs.
	var helper *SpendGuard
	var held float64
	if purpose != purposeTurn && !isCrewSeatCall(ctx) {
		if stopped := crewTaskOf(ctx).stoppedAction(); stopped != "" {
			// A TASK THAT STOPPED ON ITS ACTION buys no more helpers: every
			// route it could reach already said no.
			return nil, model, crewStopped{action: stopped}
		}
		helper = a.helperGuard()
		var err error
		if held, err = helper.before(model, messages, options); err != nil {
			return nil, model, err
		}
		if helper != nil {
			// NOBODY WAITS ON A HELPER, so it never sits out a limit: a 429
			// comes back at once and the errand's next rung is asked.
			ctx = provider.WithoutPatientRateLimits(ctx)
		}
	}
	client, wire, called, err := a.completerFor(model)
	if err != nil {
		return nil, called, err
	}
	// THE PURPOSE BECOMES THE TAG, on the one line every request in this package
	// passes over — through [withPurpose], which is the only function in
	// internal/session that spells it, here and for the three roads that carry a
	// client of their own.
	ctx = withPurpose(ctx, purpose)
	// A CALL UNDER A TOLD WINDOW IS TOLD IT HERE, at the last moment the context
	// is this package's to change (callwindow.go says why it cannot be earlier).
	response, err := client.CompleteWithMessages(toldItsWindow(ctx), messages, append(options, ai.WithModel(wire))...)
	if helper != nil {
		helper.after(model, response, held)
		crewTaskOf(ctx).addHelperSpend(response)
	}
	return response, called, err
}

// withPurpose stamps a purpose on a context, and it is THE ONE PLACE IN
// internal/session THAT SPELLS `provider.WithCallTag` — a law says exactly once
// (nohiddenwork_test.go).
//
// [Agent.completeWithNamedModel] calls it, so every request through the one door
// is stamped here; so do the three roads that complete on a client of their own,
// because each needs a client shape the door does not make. What those roads
// needed was never permission to write a tag: it was a door of their own that is
// the same door.
//
// AN EMPTY PURPOSE IS LEFT ALONE rather than written as a blank. A tag nobody
// stated is what the log already knows how to say nothing about, and a row
// reading `""` would be worse than one reading nothing.
func withPurpose(ctx context.Context, purpose callPurpose) context.Context {
	if purpose == "" {
		return ctx
	}
	return provider.WithCallTag(ctx, string(purpose))
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
	if configured.ModelPrice == nil {
		configured.ModelPrice = c.ModelPrice
	}
	configured.RouteGate = c.routeGate(model, configured.Model)
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
