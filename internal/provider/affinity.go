package provider

import (
	"context"
	"strings"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── STICK TO THE ENDPOINT THAT HOLDS YOUR CACHE ─────────────────────────────
//
// A prompt cache is not a property of a model, an account or a router. IT LIVES
// ON ONE MACHINE. The transcript layer earns a byte-stable prefix (internal/
// session's prefixcache_test.go pins the law: request N's messages must reappear
// unchanged at the head of request N+1's), and that stability is worth exactly
// nothing if the two requests are served by two different endpoints — the second
// one has never seen those bytes and re-prices every one of them at the uncached
// rate.
//
// THE MEASUREMENT THIS ANSWERS. One benchmark chat turn of 41 requests hopped
// across six OpenRouter endpoints in eleven moves; one task of 60 requests
// crossed nine endpoints in thirteen. Two adjacent requests of near-identical
// size, same model, same account: 94053 prompt tokens with 256 cached cost
// $0.00846, and 94432 prompt tokens with 93952 cached cost $0.00179. Same
// context, 4.7× the price, and the only difference was which machine answered.
// Across that run 26% of requests came in under a 50% cache hit and burned 54%
// of the main agent's spend.
//
// The rule is one sentence: STICK TO THE ENDPOINT THAT HOLDS YOUR CACHE, AND
// MOVE ONLY WHEN IT FAILS YOU. It costs nothing to keep — no probe, no extra
// request, no clock. What is remembered is one endpoint name per prompt lineage,
// and the next request asks for it first.

// pinPrefixFloor is the shortest prompt whose zero cache read is EVIDENCE
// rather than noise, in prompt tokens.
//
// Endpoints report cache reads in blocks — the ledgers showed reads quantized in
// 256-token steps — so a short prompt honestly reports zero cached tokens even
// on the machine that just answered it, and unpinning on that would be this
// process throwing away a warm cache over a rounding rule. Two thousand tokens
// is an order of magnitude above any block size seen and two orders below the
// transcripts this is for (the measurement above ran at 94k), so a zero above it
// means the cache is genuinely not there.
const pinPrefixFloor = 2048

// endpointPins is which endpoint holds each prompt lineage's cache.
//
// THE LINEAGE, NOT THE SESSION AND NOT THE MODEL ALONE, is the unit. A prompt
// cache belongs to whatever owns a growing byte prefix: a conversation owns one,
// and so does each leaf of a fan-out, which is why the run key is narrowed per
// leaf (hints.go's WithLeafCacheKey). That is exactly what the cache key already
// names, so this ledger is keyed on it, paired with the model — a /model swap is
// a different prefix on a different set of endpoints and may not inherit a pin.
//
// It is in memory and it has NO EXPIRY, which is deliberate rather than
// forgotten. A remote cache dies on its own schedule and no constant here could
// guess it; a pin that outlives the cache it was for costs nothing to hold — the
// endpoint is simply one of the ones that could have answered — and the very
// next answer measures the truth and releases it (see [Client.noteEndpointAffinity]).
// A guessed timeout would be a second, worse copy of a fact the wire reports.
type endpointPins struct {
	mu   sync.Mutex
	held map[string]string
}

func newEndpointPins() *endpointPins {
	return &endpointPins{held: map[string]string{}}
}

// sharedPins is the ledger every client built by [NewClient] holds against, for
// the reason the velocity ledger is shared: the fact belongs to the lineage and
// its endpoints, not to whichever adapter happened to hold the connection, and
// one session can outlive several clients. A test builds its own and assigns it.
var sharedPins = newEndpointPins()

// pinKey is one lineage's identity for one model. The model is normalized so the
// same conversation spelled two ways does not hold two pins.
func pinKey(lineage, model string) string {
	lineage = strings.TrimSpace(lineage)
	if lineage == "" {
		return ""
	}
	return lineage + "\x00" + normalizeModel(model)
}

// endpoint answers which endpoint this lineage's cache is on, "" when nothing is
// held.
func (p *endpointPins) endpoint(lineage, model string) string {
	if p == nil {
		return ""
	}
	key := pinKey(lineage, model)
	if key == "" {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.held[key]
}

// hold remembers the endpoint that just answered this lineage.
func (p *endpointPins) hold(lineage, model, endpoint string) {
	if p == nil {
		return
	}
	key, endpoint := pinKey(lineage, model), strings.TrimSpace(endpoint)
	if key == "" || endpoint == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.held[key] = endpoint
}

// release lets go, so the next request is routed by the sort word again.
func (p *endpointPins) release(lineage, model string) {
	if p == nil {
		return
	}
	key := pinKey(lineage, model)
	if key == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.held, key)
}

// heldEndpoint is the endpoint the next request should ask for first, "" when
// there is none to ask for.
//
// It also RELEASES a pin the velocity ledger has since refused. An endpoint that
// went quiet or paced us is one this process has just decided not to send to,
// and holding a pin on it would mean re-asking for a name that travels in
// `provider.ignore` in the same breath — a request arguing with itself.
func (c *Client) heldEndpoint(lineage, model string, ignored []string) string {
	held := c.pins.endpoint(lineage, model)
	if held == "" {
		return ""
	}
	for _, name := range ignored {
		if name == held {
			c.pins.release(lineage, model)
			return ""
		}
	}
	return held
}

// noteEndpointAffinity folds one answer into the pin ledger and reports which
// endpoint THIS request asked to come back to, "" when it asked for none.
//
// THE FOUR RULES, in the order they apply:
//
//	unnamed    an answer whose server did not identify itself changes nothing —
//	           the attribution law the ledger keeps (velocity.go's note)
//	dear       an answer that cost more than the price ceiling would have
//	           allowed for its own token counts releases the pin: a warm cache
//	           was never worth any price, which is the whole of latencyPriceCeiling
//	cold       an answer FROM the pinned endpoint that read zero cached tokens on
//	           a prompt above the floor releases it — the cache we came back for
//	           is gone, and coming back again buys nothing
//	otherwise  the endpoint that just answered is the one that now holds this
//	           lineage's bytes, so it is held — including the first, always-cold
//	           request of a lineage, whose whole job is to write the prefix we
//	           will want back, and including a fallback the router chose when the
//	           pinned endpoint could not take us
func (c *Client) noteEndpointAffinity(ctx context.Context, model, served string, usage *ai.Usage) string {
	if c.pins == nil || !c.isOpenRouter() || c.routing() == RoutingOff {
		return ""
	}
	lineage := CacheKeyFrom(ctx)
	if lineage == "" {
		// A call with no cache lineage has no prefix to protect — bare's loop is
		// deliberately one of these (internal/exec/bare) — so it is routed by the
		// sort word exactly as it always was.
		return ""
	}
	asked := c.pins.endpoint(lineage, model)
	served = strings.TrimSpace(served)
	if served == "" {
		return asked
	}
	switch {
	case c.overPriceCeiling(model, usage):
		c.pins.release(lineage, model)
	case asked == served && cacheWasLost(usage):
		c.pins.release(lineage, model)
	default:
		c.pins.hold(lineage, model, served)
	}
	return asked
}

// releaseEndpoint is the "move when it fails you" half, and it is called from
// the one place that sees every failure: a request that errored, came back 4xx
// or 5xx, or had its stream cut. Which of those the pinned endpoint actually
// caused is not knowable from here — the router answers for all of them — and it
// does not need to be: staying pinned to a lane that just failed is the one
// behaviour this file exists to prevent. Releasing is free, and the next answer
// pins whatever served it.
func (c *Client) releaseEndpoint(ctx context.Context, model string) {
	if c.pins == nil {
		return
	}
	if lineage := CacheKeyFrom(ctx); lineage != "" {
		c.pins.release(lineage, model)
	}
}

// cacheWasLost reports that this answer read nothing from the cache on a prompt
// long enough for that to mean something. Usage the provider did not send is not
// evidence of anything and answers false — the adapter always opts into usage
// accounting (wire.go), so a missing count is an endpoint that does not report
// rather than a cache that missed.
func cacheWasLost(usage *ai.Usage) bool {
	if usage == nil {
		return false
	}
	return usage.PromptTokens > pinPrefixFloor && usage.CacheReadTokens() == 0
}

// overPriceCeiling reports that this answer cost more than the ceiling would
// have allowed for the very tokens it counted.
//
// It is the ONE way an endpoint's own tariff becomes visible after the fact:
// `max_price` is what the request asks for, and the usage accounting's `cost` is
// what was actually charged. The comparison is deliberately generous — cached
// tokens are billed at a fraction of the prompt rate, so a request that cost
// more than its WHOLE prompt at the ceiling rate is over the ceiling with room
// to spare, and a borderline endpoint keeps its pin rather than being dropped on
// a rounding difference.
//
// No published price, no reported cost, no ceiling: all three answer false. A
// pin is never released on a number nobody published.
func (c *Client) overPriceCeiling(model string, usage *ai.Usage) bool {
	if usage == nil || usage.Cost == nil || *usage.Cost <= 0 {
		return false
	}
	ceiling := c.priceCeiling(model)
	if ceiling == nil {
		return false
	}
	const perMillion = 1_000_000
	allowed := ceiling.Prompt*float64(usage.PromptTokens)/perMillion +
		ceiling.Completion*float64(usage.CompletionTokens)/perMillion
	if allowed <= 0 {
		return false
	}
	return *usage.Cost > allowed
}

// withoutEndpoint is a copy of order with one name taken out, so the pin can go
// to the front of the ledger's ranking without appearing in it twice.
func withoutEndpoint(order []string, name string) []string {
	kept := make([]string, 0, len(order))
	for _, entry := range order {
		if entry != name {
			kept = append(kept, entry)
		}
	}
	return kept
}
