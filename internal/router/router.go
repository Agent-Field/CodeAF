package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Client is the slice of the model adapter everything above the transport
// consumes. Both the single-model adapter and the Router satisfy it, which is
// what lets a panel be a configuration choice instead of a second code path
// through the planner and the executor.
type Client interface {
	CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error)
	Model() string
}

// maxRungs bounds a cascade. Three is not a round number: the router lab's
// winning policy fired its second rung on about a third of tasks and averaged
// 1.35 calls per task, so a third rung is already the tail of the tail, and a
// fourth would cost latency on the cases a third did not save.
const maxRungs = 3

// unknownPrice is what a model costs when neither the catalog nor the operator
// said. It is a placeholder, not an estimate: with every price equal the
// cost-effectiveness ordering degenerates to the rating ordering, and with
// nothing rated either it degenerates to the order the operator wrote the panel
// in — which is the right answer when nothing is known.
const unknownPrice = 1.0

// separationMargin is how far above the rest a model must be measured before it
// is moved to the last rung. Below it the panel has not been separated and
// promoting anyone would be promoting noise — which is exactly the state a cold
// panel is in, and exactly when the cheapest-first order is right.
const separationMargin = 0.25

// Router sends each call to the cheapest model that can do it, and escalates
// when it turns out one could not.
//
// It is the same shape as the adapter it wraps, which is the whole design: the
// planner and the executor call CompleteWithMessages and never learn that there
// is more than one model behind it.
type Router struct {
	rungs  []*rung
	ledger *Ledger
	events *Events
}

// rung is one model on the panel, with its own adapter.
type rung struct {
	spec   Spec
	price  float64 // $/M output tokens
	client *provider.Client
}

// New builds a router over a panel. base carries everything the adapter needs
// except the model, which each rung supplies for itself.
func New(panel Panel, base provider.Config, dir string) (*Router, error) {
	if len(panel.Models) == 0 {
		return nil, errors.New("router: the panel is empty")
	}
	catalog := LoadCatalog(dir, base.BaseURL, base.APIKey, base.HTTPClient)
	ledger, err := LoadLedger(dir)
	if err != nil {
		return nil, err
	}
	events, err := OpenEvents(dir)
	if err != nil {
		// A router that cannot keep a diary still routes.
		events = nil
	}

	router := &Router{ledger: ledger, events: events}
	for _, spec := range panel.Models {
		price := spec.Price
		if price <= 0 {
			if entry, known := catalog.Entry(spec.Slug); known {
				price = entry.OutputPrice
			}
		}
		if panel.MaxOutputPrice > 0 && price > panel.MaxOutputPrice {
			return nil, fmt.Errorf("router: %s costs $%.2f/M output, over the configured cap of $%.2f",
				spec.Slug, price, panel.MaxOutputPrice)
		}
		if price <= 0 {
			price = unknownPrice
		}
		config := base
		config.Model = spec.Slug
		client, err := provider.NewClient(config)
		if err != nil {
			return nil, fmt.Errorf("router: %s: %w", spec.Slug, err)
		}
		router.rungs = append(router.rungs, &rung{spec: spec, price: price, client: client})
	}
	return router, nil
}

// Model reports the panel's first model. It is what the harness prints and what
// the profile is keyed on; which model actually served a given call is in the
// events log, where a per-call answer belongs.
func (r *Router) Model() string { return r.rungs[0].spec.Slug }

// Rungs is how many models the panel holds. The scheduler reads it to decide
// whether re-running a failed leaf could possibly help.
func (r *Router) Rungs() int { return len(r.rungs) }

// Ledger exposes what has been learned, for reporting.
func (r *Router) Ledger() *Ledger { return r.ledger }

// Close flushes what the run learned.
func (r *Router) Close() error {
	return errors.Join(r.ledger.Save(), r.events.Close())
}

// CompleteWithMessages routes one call.
//
// The two shapes of work are routed differently and the difference is not an
// optimisation. A planning call is one request whose answer can be checked the
// moment it arrives, so it cascades: cheap model, verify, escalate on a failure
// the verifier caught. A leaf is a whole conversation whose worth is only known
// at the end, so it is routed once and pinned — escalation for a leaf is the
// scheduler re-running it, not the loop changing model between turns.
func (r *Router) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	call := provider.CallFrom(ctx)
	if call.Class() == provider.ClassExecLeaf {
		return r.leaf(ctx, call, messages, options)
	}
	return r.cascade(ctx, call, messages, options)
}

// StreamComplete streams from the first rung and never cascades. A stream is
// committed the moment its first byte is delivered, so there is nothing to
// escalate to: the answer has already started arriving. Nothing in the harness
// streams today; this exists so that the router is a drop-in for the adapter.
func (r *Router) StreamComplete(ctx context.Context, prompt string, options ...ai.Option) (<-chan ai.StreamChunk, <-chan error) {
	first := r.rungs[0]
	return first.client.StreamComplete(first.affinity(ctx), prompt, options...)
}

// cascade is the policy both labs converged on.
//
// Its economics rest entirely on the verifier, and that is worth stating where
// the code is: the cheap first attempt is only cheap because a failure costs
// nothing but the retry, and the escalation is only rare because most first
// attempts pass. Where nothing can check the answer the cascade collapses into
// "always use the cheapest model", which is why an unverifiable call reports an
// unverified success and moves nothing.
func (r *Router) cascade(ctx context.Context, call *provider.Call, messages []ai.Message, options []ai.Option) (*ai.Response, error) {
	class := call.Class()
	order := r.order(class)
	if start := call.Attempt(); start > 0 {
		if start >= len(order) {
			start = len(order) - 1
		}
		order = order[start:]
	}
	candidates := slugs(order)
	schema := schemaOf(options)

	var tried []string
	var failures []error
	for index, pick := range order {
		started := time.Now()
		response, err := pick.client.CompleteWithMessages(pick.affinity(ctx), messages, options...)
		elapsed := time.Since(started)
		if err != nil {
			// Transport, not ability. It moves no rating, and the next rung is
			// tried anyway because the work still has to happen.
			r.record(ctx, class, candidates, tried, index, pick, nil, elapsed, provider.VerdictProviderFailure)
			tried = append(tried, pick.spec.Slug)
			failures = append(failures, fmt.Errorf("%s: %w", pick.spec.Slug, err))
			continue
		}
		if verdict := verify(response, schema); verdict != "" {
			// Final for this rung: it produced something and the something was
			// unusable. That is the observation the cascade exists to collect.
			r.observe(pick, class, resolvedOf(response), verdict)
			r.record(ctx, class, candidates, tried, index, pick, response, elapsed, verdict)
			tried = append(tried, pick.spec.Slug)
			failures = append(failures, fmt.Errorf("%s: %s", pick.spec.Slug, verdict))
			continue
		}
		// It parsed and it carried the fields it promised. Whether it is *right*
		// is the call site's to say, so the verdict is held open until it does —
		// and stays unverified, moving nothing, if it never does.
		id := r.record(ctx, class, candidates, tried, index, pick, response, elapsed, provider.VerdictUnverifiedSuccess)
		resolved := resolvedOf(response)
		call.Observe(func(final provider.Verdict) {
			r.observe(pick, class, resolved, final)
			r.events.Append(Event{Call: id, Class: string(class), Model: pick.spec.Slug,
				Resolved: resolved, Verdict: final, Final: true})
		})
		return response, nil
	}
	return nil, fmt.Errorf("every rung failed: %w", errors.Join(failures...))
}

// leaf routes one whole tool loop.
//
// The model is chosen on the first turn and pinned for the rest, because a leaf
// is one lineage: its prefix cache belongs to one model, its transcript is one
// conversation, and swapping models mid-loop would pay to rewrite the cache
// every turn while handing the new model a conversation it did not have. When a
// leaf fails in a way a better model could fix, the scheduler re-runs the whole
// thing at the next attempt number, and that is where escalation happens.
func (r *Router) leaf(ctx context.Context, call *provider.Call, messages []ai.Message, options []ai.Option) (*ai.Response, error) {
	class := provider.ClassExecLeaf
	order := r.order(class)
	index := call.Attempt()
	if index >= len(order) {
		index = len(order) - 1
	}
	pick := r.rungOf(call.Pin(order[index].spec.Slug), order[index])
	// Where the pinned model actually sits, which is not always where the
	// attempt number pointed: a pin set on turn one survives a ledger update
	// that reordered the panel underneath it, and honouring the pin is the point.
	index = indexOf(order, pick)

	started := time.Now()
	response, err := pick.client.CompleteWithMessages(pick.affinity(ctx), messages, options...)
	elapsed := time.Since(started)
	if err != nil {
		r.record(ctx, class, slugs(order), nil, index, pick, nil, elapsed, provider.VerdictProviderFailure)
		return nil, err
	}
	// One row per turn rather than one per leaf. A leaf's cost is the sum of its
	// turns and only the loop knows when the last one was, so the log records
	// what it can see and the verdict row below closes the account.
	id := r.record(ctx, class, slugs(order), nil, index, pick, response, elapsed, provider.VerdictUnverifiedSuccess)
	resolved := resolvedOf(response)
	call.Observe(func(final provider.Verdict) {
		r.observe(pick, class, resolved, final)
		r.events.Append(Event{Call: id, Class: string(class), Model: pick.spec.Slug,
			Resolved: resolved, Verdict: final, Final: true})
	})
	return response, nil
}

// order ranks the panel for one class.
//
// Cheapest-that-works first, by expected success per dollar — the ordering the
// router lab's winning policy used, and the reason that policy landed within
// $0.000067 a task of an omniscient oracle on cost while matching it exactly on
// success. Ties go to the cheaper model, and with nothing measured the whole
// ordering falls back to price, which is how a cold panel generates its own
// evidence: the cheap models get tried, and what they get wrong escalates.
func (r *Router) order(class provider.CallClass) []*rung {
	scored := make([]ranked, 0, len(r.rungs))
	for _, item := range r.rungs {
		rating, _ := r.ledger.Rating(r.ledger.Resolve(item.spec.Slug), class, coldStart(item.spec.Role))
		scored = append(scored, ranked{rung: item, rating: rating, score: Ability(rating) / item.price})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].rung.price < scored[j].rung.price
	})

	limit := min(maxRungs, len(scored))
	chosen := make([]*rung, 0, limit)
	for _, item := range scored[:limit] {
		chosen = append(chosen, item.rung)
	}

	// The last rung is the one that has to be right, so it is the panel's
	// strongest model rather than whichever cheap model happened to rank third —
	// that is what makes the cascade's ceiling the best single model's rather
	// than the panel's average.
	//
	// Two things stop it firing. The ledger has to have actually separated
	// somebody, because promoting the leader of a tie is promoting noise and a
	// cold panel is entirely ties. And the strongest model must not already be
	// the opening rung: when the best model is also the best value there is
	// nothing above it to climb to, and moving it to the end would mean opening
	// every call on a model known to be both worse and worse value.
	best := scored[0]
	for _, item := range scored[1:] {
		if item.rating > best.rating {
			best = item
		}
	}
	last := chosen[len(chosen)-1]
	if last != best.rung && chosen[0] != best.rung && best.rating-ratingOfRung(scored, last) > separationMargin {
		kept := make([]*rung, 0, limit)
		for _, item := range chosen {
			if item != best.rung {
				kept = append(kept, item)
			}
		}
		if len(kept) >= limit {
			kept = kept[:limit-1]
		}
		chosen = append(kept, best.rung)
	}
	return chosen
}

// ranked is one rung with what is known about it, for the length of one ordering.
type ranked struct {
	rung   *rung
	rating float64
	score  float64
}

func ratingOfRung(scored []ranked, target *rung) float64 {
	for _, item := range scored {
		if item.rung == target {
			return item.rating
		}
	}
	return 0
}

// observe folds one attempt's verdict into the ledger, against the snapshot the
// provider says actually served it.
func (r *Router) observe(pick *rung, class provider.CallClass, resolved string, verdict provider.Verdict) {
	model := pick.spec.Slug
	if resolved != "" && resolved != model {
		r.ledger.Alias(model, resolved)
		model = resolved
	}
	r.ledger.Observe(model, class, coldStart(pick.spec.Role), verdict)
}

// record writes one attempt to the log and returns the id later rows join on.
func (r *Router) record(ctx context.Context, class provider.CallClass, candidates, tried []string,
	index int, pick *rung, response *ai.Response, elapsed time.Duration, verdict provider.Verdict) string {
	id := callID()
	event := Event{
		Call:       id,
		Run:        provider.CacheKeyFrom(ctx),
		Class:      string(class),
		Candidates: candidates,
		Rung:       index,
		Escalation: tried,
		Model:      pick.spec.Slug,
		Resolved:   resolvedOf(response),
		Verdict:    verdict,
		LatencyMS:  elapsed.Milliseconds(),
	}
	if response != nil && response.Usage != nil {
		event.PromptTokens = response.Usage.PromptTokens
		event.CompletionTokens = response.Usage.CompletionTokens
		event.CachedTokens = response.Usage.CacheReadTokens()
		if response.Usage.Cost != nil {
			event.Cost = *response.Usage.Cost
		}
	}
	r.events.Append(event)
	return id
}

// rungOf finds the rung for a pinned slug, falling back to the one just chosen.
// The fallback is not defensive padding: a pin set by an earlier turn survives a
// ledger update that reordered the panel underneath it, and honouring the pin is
// the point.
func (r *Router) rungOf(slug string, fallback *rung) *rung {
	for _, item := range r.rungs {
		if item.spec.Slug == slug {
			return item
		}
	}
	return fallback
}

// affinity re-derives the run's cache key for this model.
//
// One run is one cache lineage *per model*, never one shared across them. A
// prefix written by a cheap first rung is not a prefix the escalation target can
// read, and handing both the same key asks the provider to keep two different
// conversations warm on one instance.
func (u *rung) affinity(ctx context.Context) context.Context {
	key := provider.CacheKeyFrom(ctx)
	if key == "" {
		return ctx
	}
	return provider.WithCacheKey(ctx, provider.RunCacheKey(key, u.spec.Slug))
}

// verify is the cascade's whole economics in one function: it decides, for
// nothing, whether the cheap attempt worked.
//
// It checks only what it can check without another model call — that something
// came back, that it parses, and that it carries the fields the schema said were
// required. Whether the parsed answer is *correct* is a different question and
// belongs to the call site, which is the only thing that knows what correct
// means here. An empty reply is called out separately because it is not a
// malformed answer, it is the runaway-reasoning mode: full price, nothing
// delivered, and the probe lab's single largest failure class.
func verify(response *ai.Response, schema json.RawMessage) provider.Verdict {
	text := strings.TrimSpace(response.Text())
	if text == "" {
		return provider.VerdictEmptyResponse
	}
	if len(schema) == 0 {
		return ""
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(unfence(text)), &decoded); err != nil {
		return provider.VerdictFormatFailure
	}
	for _, field := range requiredOf(schema) {
		if _, present := decoded[field]; !present {
			return provider.VerdictFormatFailure
		}
	}
	return ""
}

// schemaOf recovers the schema a call asked for. The options are closures, so
// the only way to see what they do is to let them do it — to a throwaway
// request, which costs nothing and saves the router from keeping its own copy of
// every call site's schema in sync with the call site's.
func schemaOf(options []ai.Option) json.RawMessage {
	request := &ai.Request{}
	for _, option := range options {
		if err := option(request); err != nil {
			return nil
		}
	}
	if request.ResponseFormat == nil || request.ResponseFormat.JSONSchema == nil {
		return nil
	}
	return request.ResponseFormat.JSONSchema.Schema
}

func requiredOf(schema json.RawMessage) []string {
	var decoded struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &decoded); err != nil {
		return nil
	}
	return decoded.Required
}

// unfence strips a markdown code fence, mirroring what the planner's own decoder
// tolerates. The two must agree: a router that rejected a fenced answer the
// caller would have accepted would escalate a call that was about to succeed.
func unfence(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	if start := strings.Index(trimmed, "\n"); start >= 0 {
		trimmed = trimmed[start+1:]
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(trimmed), "```"))
}

// resolvedOf reads which model actually served the call. For a floating alias
// this is a dated snapshot and it is what the ledger keys on: `-latest` is a
// different set of weights every few weeks, and a rating pooled across two of
// them measures neither.
func resolvedOf(response *ai.Response) string {
	if response == nil {
		return ""
	}
	return strings.TrimSpace(response.Model)
}

func indexOf(rungs []*rung, target *rung) int {
	for index, item := range rungs {
		if item == target {
			return index
		}
	}
	return len(rungs) - 1
}

func slugs(rungs []*rung) []string {
	names := make([]string, 0, len(rungs))
	for _, item := range rungs {
		names = append(names, item.spec.Slug)
	}
	return names
}
