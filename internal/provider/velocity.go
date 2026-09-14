package provider

import (
	"context"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
)

// ── WHO SERVED, AND HOW FAST ────────────────────────────────────────────────
//
// A router fans one model over many endpoints. Two of them are a datacentre
// away with a warm replica; one is a machine somebody is also training on, and
// it answers the same request at a fifth of the speed. The model id says
// nothing about which one a request landed on, so a session that only names its
// model is describing a decision it did not make.
//
// AND THEY DO NOT CHARGE THE SAME. That was assumed here for a long time and it
// is simply false: an endpoint's tariff is its own, and the model id's published
// list price is a figure none of them is obliged to match. Asking for the
// fastest one and saying nothing about price is how a cost autopsy over 44
// bench cells found this surface paying ~3.5× list at identical token counts —
// see latencyPriceCeiling, which is the whole of the answer.
//
// This file is the half of the adapter that both ASKS for speed and CHECKS it.
// Asking is one object on the wire — the routing preferences below. Checking is
// the ledger: every completion is timed, the response says who served it, and
// an endpoint that keeps being slow is demoted and then, for a while, refused.
//
// Nothing here knows a vendor's name. The law reads only "who served, and how
// fast", and every name in it arrived from the wire a moment ago.

// RoutingStrategy is how a session asks the router to choose among the
// endpoints serving one model.
//
// It is a CHOICE and not a bool because the two live answers are not opposites:
// latency wants the fastest endpoint, price wants the cheapest, and they
// routinely disagree. Off is the third answer and it is a real one — it sends
// no preference object at all, which is what an operator behind a gateway that
// does not speak this dialect needs.
type RoutingStrategy string

const (
	// RoutingLatency asks for the currently-fastest endpoint UNDER A PRICE
	// CEILING (see latencyPriceCeiling). It is opt in — somebody writes the row
	// — and the ceiling is there because being served fastest was never worth
	// being charged anything.
	RoutingLatency RoutingStrategy = "latency"
	// RoutingPrice asks for the cheapest endpoint that can serve the request.
	RoutingPrice RoutingStrategy = "price"
	// RoutingOff sends no preference object, and switches the ledger off with
	// it: an operator who has not asked to be routed has not asked to be
	// measured either, and a demotion nobody can act on is only overhead.
	RoutingOff RoutingStrategy = "off"
	// RoutingSimple sends exactly what the person asked for and nothing else,
	// and IT IS THE ROW THIS BUILD SHIPS ([DefaultRouting]).
	// No lane pinned: the request carries NO provider object at all and the
	// router's own default routing answers — no belief, no sort word, no price
	// ceiling, no hedge. A lane pinned: the request carries that one demand
	// (`provider.only`, fallbacks off) and nothing with it. Every other layer
	// of the routing stack stays compiled in but is disconnected from the
	// request path, so the row a person wrote is the whole algorithm.
	RoutingSimple RoutingStrategy = "simple"
)

// DefaultRouting is what a client asks for when nobody has chosen: no caller
// handed it a row and this process installed none ([InstallRouting]).
//
// IT IS THE SAME ANSWER FOR EVERY CALL, and that is the point. The old default
// read who was waiting and asked the router to sort by speed for a person's own
// turn and by price for an errand — a decision made per request, out of sight,
// that the picker and the record could then disagree with. Under this one the
// wire carries the person's own row and nothing this build inferred, so what is
// shown, what is chosen and what is written down are the same fact.
//
// It is spelled again in internal/config ([config.DefaultRouting]) because that
// package owns the word on disk and this one owns the word on the wire; a test
// there holds the two together.
const DefaultRouting = RoutingSimple

// sortWord is the strategy as the router spells it, empty when no preference
// object should be sent at all.
func (s RoutingStrategy) sortWord() string {
	switch s {
	case RoutingPrice:
		return "price"
	case RoutingOff, RoutingSimple:
		// Simple never asks by sort word: its only preference is a person's
		// own pin, which is a demand and not a ranking.
		return ""
	default:
		return "latency"
	}
}

// ── WHO IS WAITING ──────────────────────────────────────────────────────────
//
// Speed is worth something only when somebody is sitting there watching the
// answer arrive. A task worker, a divided part, an auditor, a judge, a title, a
// memory pass — nobody is waiting on any of those, and a second saved on one of
// them is a second nobody spends.
//
// So the request carries who is waiting. IT NO LONGER PICKS A SORT WORD: the
// routing row is a person's answer and this build does not write one for them
// ([DefaultRouting]). What still reads it is what a WAIT IS WORTH — the value of
// time the chooser is given when somebody did choose the ranked road (lanes.go),
// and how much of an answer is being read as it arrives (workload.go).

// RoutingIntent says whether a person is waiting on this call.
type RoutingIntent int

const (
	// IntentInteractive is a call somebody is watching arrive. It is the zero
	// value, because a call that has said nothing about itself is the
	// conversation's own turn until something says otherwise.
	IntentInteractive RoutingIntent = iota
	// IntentBackground is a call nobody is waiting on. Speed is worth nothing
	// to it and price is worth everything.
	IntentBackground
)

type routingIntentContextKey struct{}

// WithRoutingIntent states who is waiting on the calls made under ctx.
//
// IT IS SAID AND NEVER INFERRED. "Nobody is watching this stream" is close to
// the answer but is not it: a tool that asks a model something takes the
// observer off the context and the person is still sitting there waiting for
// the turn it belongs to. Only the call site knows whether anybody is waiting,
// so only the call site may say — and a call that says nothing keeps the
// behaviour it has always had.
func WithRoutingIntent(ctx context.Context, intent RoutingIntent) context.Context {
	return context.WithValue(ctx, routingIntentContextKey{}, intent)
}

// RoutingIntentFrom answers who is waiting on the calls made under ctx,
// interactive when nothing said. It is the read half of [WithRoutingIntent],
// exported so a surface can assert what its own calls will ask for without
// standing up a router.
func RoutingIntentFrom(ctx context.Context) RoutingIntent {
	return routingIntentFrom(ctx)
}

// routingIntentFrom answers who is waiting on this call, interactive when
// nothing said.
//
// THE ROLE OUTRANKS THE INTENT WHERE BOTH ARE SAID (roles.go). A role is the
// more specific claim — it names the errand, and the table can explain every
// number derived from it — while the intent is a two-valued reading of the same
// fact that a call site had to remember to state. The intent stays because it
// is what `provider.sort` is built from and what a dozen sites still say; it is
// now DERIVED where a role is present rather than believed alongside it.
func routingIntentFrom(ctx context.Context) RoutingIntent {
	if intent, ok := roleIntent(ctx); ok {
		return intent
	}
	intent, _ := ctx.Value(routingIntentContextKey{}).(RoutingIntent)
	return intent
}

// ParseRoutingStrategy reads a settings word. An unrecognized word is NOT an
// error and NOT off: it falls back to [DefaultRouting], because a typo in a
// config row must not silently put a session on a road nobody asked for, and
// off is a road somebody chooses rather than one they arrive at by accident.
func ParseRoutingStrategy(word string) (RoutingStrategy, bool) {
	switch strings.ToLower(strings.TrimSpace(word)) {
	case string(RoutingLatency):
		return RoutingLatency, true
	case string(RoutingPrice):
		return RoutingPrice, true
	case string(RoutingOff):
		return RoutingOff, true
	case string(RoutingSimple):
		return RoutingSimple, true
	default:
		return DefaultRouting, false
	}
}

// RoutingSource answers which strategy is in force.
//
// It is an interface rather than a value on Config for one reason: the answer
// lives in a settings file, and an adapter that read one would put a disk read
// on the path of every client construction — in tests, in a leaf, in a
// subharness process that has no profile directory at all. The surface resolves
// the row once and hands the answer down; a test hands down [StaticRouting] and
// touches nothing.
type RoutingSource interface {
	RoutingStrategy() RoutingStrategy
}

type staticRouting RoutingStrategy

func (s staticRouting) RoutingStrategy() RoutingStrategy { return RoutingStrategy(s) }

// StaticRouting is one already-resolved answer as a source. The empty strategy
// is NOBODY HAVING CHOSEN rather than a refusal, so a caller that has nothing
// to say falls to the row this process installed and, with none installed, to
// [DefaultRouting] — see [Client.routingChoice].
func StaticRouting(strategy RoutingStrategy) RoutingSource { return staticRouting(strategy) }

// ── THE ROW EVERY CLIENT IN THIS PROCESS ANSWERS TO ─────────────────────────
//
// [RoutingSource] is how a caller HANDS an answer down, and every client that
// is handed one keeps it. The problem is the clients nobody hands one to, and
// there are several: the harness and the subharness build their own adapters
// (cmd/aforge), `read_document` and `view_image` build theirs
// (internal/config), and a panel builds one per member (internal/router). Each
// of those was assembled through [config.Config.ClientConfig], which has never
// carried a routing answer at all — so whatever a person wrote in the routing
// row, those adapters ran on the default.
//
// THAT IS A BREACH OF THE ROW, AND IT WAS A SILENT ONE. Those adapters ran the
// ranked road while the default was `latency`, and the ranked road may retire A
// PERSON'S PIN before any wire is asked — a machine the saved account exclusions
// cover is stood down where it is drawn (lanes.go). The retirement is
// process-wide, so the conversation's own next turn — running `simple`, doing
// everything right — found the pin already retired and went out bare, with the
// sentence parked on a call nobody was reading and the chrome still naming the
// machine. The default has moved since ([DefaultRouting]) and the breach has
// not: a person who writes `latency` or `price` is owed it in every adapter
// this process builds, exactly as one who leaves the row alone is.
//
// So the row is installed ONCE, by the same door that installs the lane rows a
// person wrote (internal/config's InstallLaneRows), and every client that was
// handed nothing reads it. It is a process-wide knob for [lanePin]'s reason
// said again: the row is about a SESSION and not about an adapter, and two
// clients in one process holding two answers to "what did they ask for" is the
// defect above. A HANDED-DOWN ANSWER STILL WINS — the engine host and a task
// child carry their parent's row explicitly, and a process-wide fallback may
// not overrule a caller that spoke.
var installedRouting atomic.Value

// InstallRouting states the routing row this process's clients answer to, for
// every client that is handed no [RoutingSource] of its own. The empty strategy
// is NOBODY HAVING WRITTEN ONE and puts the knob back to exactly that, which is
// what an unwritten row resolves to — [DefaultRouting], the same answer for
// every call.
func InstallRouting(strategy RoutingStrategy) {
	installedRouting.Store(strategy)
}

// RoutingNow is the row in force for a caller that hands down none of its own:
// what this process installed, and [DefaultRouting] where nobody installed
// anything.
//
// It is exported for the gates OUTSIDE this package that have to answer the
// same question the request path answers ([Client.routingChoice]) — the lane
// beat in internal/session asks whether the row is `off` before it measures
// anything. A session that carries no row of its own must read it HERE and not
// off a field filled in at launch, because the row moves while the process runs:
// the settings panel writes it and re-installs it in the same keystroke, and a
// launch snapshot would keep a beat running that a person has just turned off.
func RoutingNow() RoutingStrategy {
	strategy, _ := installedRoutingChoice()
	return strategy
}

// installedRoutingChoice is the installed row and whether one was installed at
// all, in the shape [Client.routingChoice] answers in. With none installed the
// strategy is [DefaultRouting] and the answer to "did somebody choose?" is no,
// which are two different facts and both wanted: a gate reads the first and the
// request path reads the second.
func installedRoutingChoice() (RoutingStrategy, bool) {
	held, ok := installedRouting.Load().(RoutingStrategy)
	if !ok || strings.TrimSpace(string(held)) == "" {
		return DefaultRouting, false
	}
	parsed, _ := ParseRoutingStrategy(string(held))
	return parsed, true
}

// routingChoice is the strategy A PERSON CHOSE, and whether one was chosen at
// all. An empty source, an empty word, or no source is "nobody said" — which is
// a different fact from "somebody said simple", even where the two resolve to
// the same road, and it is what lets a client handed nothing fall to the row
// this process installed while a caller that spoke keeps its own answer.
func (c *Client) routingChoice() (RoutingStrategy, bool) {
	if c.config.Direct {
		// A connected direct service has one road. Treating the absence of a
		// person's router setting as any kind of routing would hand router
		// vocabulary to an endpoint that has no lanes to rank.
		return RoutingOff, true
	}
	// A CALLER THAT SPOKE IS ANSWERED FIRST, and a caller that did not falls to
	// the row this process installed ([InstallRouting]). The two arms below are
	// one question asked of two places: a nil source and a source holding the
	// empty strategy are both "this caller said nothing", and neither is a
	// refusal.
	if c.config.Routing != nil {
		strategy := c.config.Routing.RoutingStrategy()
		if strings.TrimSpace(string(strategy)) != "" {
			parsed, _ := ParseRoutingStrategy(string(strategy))
			return parsed, true
		}
	}
	return installedRoutingChoice()
}

// routing resolves the strategy for this client. It is what the machinery's own
// gates read — whether to measure, to probe, to hedge, to hold a cache pin —
// and it is the one answer the request beside them asks for too, so a gate
// cannot be open on a road no request is taking.
//
// THE PERSON'S ROW WINS OUTRIGHT, and where nobody wrote one there is nothing
// underneath it to infer: every call falls to [DefaultRouting]. Until
// 2026-09-13 a second reading beside this one asked who was waiting and chose
// speed or price accordingly, which was a routing decision this build made on
// a person's behalf and then had to keep explaining — the one thing a person
// could not see in the picker, the status line or the record. The intent is
// still carried on every call for what a wait is worth (lanes.go) and for how
// much of an answer is being read as it arrives (workload.go); it no longer
// picks the road.
func (c *Client) routing() RoutingStrategy {
	strategy, _ := c.routingChoice()
	return strategy
}

// providerPrefs is the routing preference object.
//
// Sort is the standing ask. Order and Ignore are the ledger's two verdicts, and
// they are only ever populated from endpoints this process has itself timed —
// see [velocityLedger.preferences].
//
// AllowFallbacks and RequireParameters are pointers so that "true" is written
// on the wire rather than assumed. The first keeps every veto here advisory: an
// endpoint we demoted is still reachable when the ones we prefer are down, so a
// slow answer always beats no answer. The second is the guard on the reasoning
// knob — a request carrying `reasoning` must not be routed to an endpoint that
// silently drops it, because a planning call that was supposed to think and did
// not is a wrong answer rather than a slow one.
type providerPrefs struct {
	Sort  string   `json:"sort,omitempty"`
	Order []string `json:"order,omitempty"`
	// Only is a DEMAND rather than a ranking: the request goes to exactly these
	// lanes or it does not go at all. It is what a pin sends, and what the
	// second request of a hedged pair sends so that the pair cannot both land
	// on the lane that is already stalling (hedge.go).
	Only              []string  `json:"only,omitempty"`
	Ignore            []string  `json:"ignore,omitempty"`
	AllowFallbacks    *bool     `json:"allow_fallbacks,omitempty"`
	RequireParameters *bool     `json:"require_parameters,omitempty"`
	MaxPrice          *maxPrice `json:"max_price,omitempty"`
}

// maxPrice is the ceiling an endpoint's own tariff must sit under to serve this
// request. The router spells the numbers in US DOLLARS PER MILLION TOKENS,
// which is a million times the unit the catalog publishes; the conversion is
// [Client.priceCeiling]'s and is done in exactly one place.
//
// Neither field is omitempty. A model whose list price really is zero — the
// free variants a router publishes — gets a ceiling of zero, and that is the
// honest ask rather than a missing one; dropping it would quietly send `{}` and
// mean the opposite.
//
// IT CANNOT EXPRESS A CACHE-READ CEILING. The router's field takes `prompt`,
// `completion`, `request` and `image` and nothing else, so the 4.0× the cost
// autopsy measured on CACHED tokens is bounded only indirectly, through the
// prompt ceiling that the same endpoint's tariff is derived from. That is a
// real limit of the mechanism and not an omission here.
type maxPrice struct {
	Prompt     float64 `json:"prompt"`
	Completion float64 `json:"completion"`
}

// latencyPriceCeiling is how far above a model's own published list price an
// endpoint may charge and still be worth choosing for speed.
//
// THE MEASUREMENT IT ANSWERS. A cost autopsy over 44 bench cells found this
// surface paying ~3.5× what the model's list price says the same token counts
// should cost: 1.4× list on uncached prompt tokens, 4.0× on cached ones (a
// ~44% cache discount where list promises 80%), and 1.9× on output. Nothing
// about the model or the answer differed. The whole gap was `sort: latency`
// asking the router for the fastest endpoint and then accepting whatever that
// endpoint charged, because the ask carried no ceiling at all.
//
// 1.25 IS THE HONEST BOUND, and the reasoning is about a person rather than
// about a number. An endpoint 25% over list is buying a latency edge somebody
// can actually feel on a turn. An endpoint 4× over list is buying nothing a
// person notices on a five-minute task — the answer arrives while they are
// still reading the last one either way — so there is no version of "a chat
// session is a person waiting" that justifies paying it.
const latencyPriceCeiling = 1.25

// priceCeiling is the ceiling one request carries, nil when there is none to
// carry.
//
// ABSENCE, NEVER A GUESS. A model the catalog has no published price for — a
// row that never loaded, a slug the router calls "it depends", a catalog still
// warming — sends no ceiling and routes exactly as it did before. A ceiling
// invented from a neighbouring model's price would be this process quietly
// refusing endpoints on a number nobody published.
func (c *Client) priceCeiling(model string) *maxPrice {
	if c.config.ModelPrice == nil {
		return nil
	}
	prompt, completion, known := c.config.ModelPrice(normalizeModel(model))
	if !known || prompt < 0 || completion < 0 {
		return nil
	}
	const perMillion = 1_000_000
	ceiling := &maxPrice{
		Prompt:     prompt * perMillion * latencyPriceCeiling,
		Completion: completion * perMillion * latencyPriceCeiling,
	}
	// AND A CEILING ONLY A MACHINE THIS ACCOUNT CANNOT REACH FITS UNDER IS A
	// DEMAND FOR THAT MACHINE, so it is not sent (accountset.go). The list price
	// is the cheapest machine's tariff, and the cheapest machine is often the
	// first-party one an account's privacy switch takes away.
	if ceilingOnlyAdmitsTheUnserved(model, ceiling) {
		return nil
	}
	return ceiling
}

// providerPreferences builds the object one request will carry, nil when none
// should be sent.
//
// It is called from the encoder, which runs immediately before the send, and
// that timing is the point: the demotion a laggy answer earned thirty seconds
// ago applies to the request now being written, without anything having to
// carry it forward.
//
// IT IS THE DECISION SITE OF ISSUE #433, and the gate on it is what the BASE
// ANSWERED rather than what its hostname says. The field is a router's dialect
// and an endpoint that is not a router either ignores it or 400s on it — but
// which of the two a base does is a thing only the base can say, so it is asked
// once and remembered, and only a base that has answered "no" is left off
// (prefcarry.go's [Client.carriesPreferences]).
//
// ── AND AN UNASKED BASE SENDS ONLY WHAT A PERSON ASKED FOR ──────────────────
//
// THE LAW IS ABOUT A PREFERENCE THE PERSON HAS, NOT ABOUT THIS ADAPTER'S OWN
// DEFAULT KNOBS. `sort`, `allow_fallbacks` and `require_parameters` are nobody's
// instruction: they are how this build asks a ROUTER to break a tie among
// machines it already knows about, and putting them on a plain endpoint's every
// request would be a field that every plain-base user suddenly carries, for a
// tie there is nothing to break. So on a base that has not yet SHOWN it carries
// a preference, the object goes out only when there is something to ask WITH,
// and there is exactly one such thing: a lane the person pinned. A ranking
// cannot be the reason, because a ranking only exists once a sheet arrived —
// and a sheet arriving is the base proving it carries.
//
// The consequence, stated so nobody has to derive it: a plain base with nobody
// pinning anything is never asked, never answers, and its requests are
// byte-for-byte the requests it got before this law existed. The moment somebody
// pins a lane, that pin IS the asking.
func (c *Client) providerPreferences(model string, knobs callKnobs) *providerPrefs {
	if !c.carriesPreferences() {
		return nil
	}
	if !c.prefsProven() && c.pinnedLaneFor(model) == "" {
		return nil
	}
	strategy := c.routing()
	// SIMPLE ROUTING SENDS THE PERSON'S OWN INSTRUCTION AND NOTHING ELSE.
	// With no pin in force there is nothing to ask for: the request goes out
	// with no provider object at all and the router's own default routing
	// answers, which is the whole of what the row promises. With a pin the
	// one demand the choice carries is the whole object — `only` plus
	// fallbacks off, applied by [Client.applyLaneChoice] — and the sort word,
	// the price ceiling, the ledger's order and the cache pin all stay off
	// the wire, because each of them is a second guess beside the one
	// machine somebody named.
	if strategy == RoutingSimple {
		if knobs.laneChoice == nil || len(knobs.laneChoice.Only) == 0 {
			return nil
		}
		prefs := &providerPrefs{}
		c.applyLaneChoice(prefs, model, knobs, "")
		return prefs
	}
	word := strategy.sortWord()
	if word == "" {
		return nil
	}
	yes := true
	prefs := &providerPrefs{Sort: word, AllowFallbacks: &yes, RequireParameters: &yes}
	if strategy == RoutingLatency && !(c.velocity != nil && c.velocity.ceilingRefused(model)) {
		// The ceiling rides the latency ask and only the latency ask. Sorting by
		// price is already asking for the cheapest thing available, and a ceiling
		// on top of it could only ever take endpoints away without changing which
		// one is chosen.
		//
		// AND IT IS NOT SENT AGAIN ONCE THE PRICE RUNG IS REACHED. The ladder
		// (endpoints.go) first widens endpoint membership under this same ceiling;
		// only another refusal drops it and teaches the process. A later call is
		// then shaped right from the start instead of paying those refusals again.
		prefs.MaxPrice = c.priceCeiling(model)
	}
	if c.velocity != nil {
		order, ignore := c.velocity.preferences(model)
		prefs.Ignore = ignore
		// THE LEDGER'S ORDER IS A SPEED RANKING, and `provider.order` names what
		// to try FIRST — so sending it beside `sort: price` would put this
		// process's own fastest lane ahead of the cheapest one and quietly undo
		// the sort. A request that asked for price gets the refusals, which are
		// about endpoints that will not answer at all, and nothing that ranks.
		if strategy != RoutingPrice {
			prefs.Order = order
		}
	}
	// AND THE PIN GOES IN FRONT OF ALL OF IT (affinity.go). It is not a ranking
	// and that is why it travels under BOTH sort words where the ledger's order
	// may not: it names the one machine that already holds this lineage's prompt
	// prefix, and a cold prefix cost 4.7× a warm one at identical token counts —
	// more than any endpoint's tariff differs from another's, so the errand
	// nobody is waiting on wants its cache back exactly as much as the person
	// does. Its first request, having nothing pinned, asks by the row's own
	// sort word like any other.
	//
	// It is a preference and never a demand: `allow_fallbacks` stays true above,
	// so an endpoint that is busy, gone, or over the ceiling simply does not
	// answer this one and the router picks by the sort word as before.
	held := c.heldEndpoint(knobs.cacheKey, model, prefs.Ignore)
	if held != "" {
		prefs.Order = append([]string{held}, withoutEndpoint(prefs.Order, held)...)
	}
	// AND THE BELIEF SPEAKS LAST (lanes.go). What `internal/lane` has measured
	// about these endpoints is the same question the ledger's order answers and
	// a better answer to it — a posterior per lane rather than three thresholds
	// — so when there is a belief its order replaces the ranking above. When
	// there is not, and on the first call of every fresh machine there is not,
	// nothing here changes and the request goes out exactly as it always did.
	//
	// IT NO LONGER TAKES THE REQUEST, and the absence is the whole of this
	// change: the belief is asked ONCE PER CALL rather than once per encode
	// ([Client.withLaneChoice]), so by the time the object is being composed
	// there is nothing left to ask — only the call's own answer to read off its
	// knobs. A parameter kept "in case the encoder needs to decide again" would
	// be the door back to the defect.
	c.applyLaneChoice(prefs, model, knobs, held)
	return prefs
}

// wirePreferences is the `provider` object THIS REQUEST ACTUALLY GOES OUT
// WITH: the ledger's own preferences, plus a rescue's demand, minus whatever
// the ladder has already taken off.
//
// IT IS NAMED ONCE BECAUSE TWO READERS HAVE TO AGREE ABOUT IT. The encoder
// writes the object (wire.go) and the ladder decides its first rung from it
// (endpoints.go's [Client.relaxationPlan]); a ladder reading only the ledger's
// half could not see the demand [hedgePreference] adds afterwards, so a pinned
// request was offered no first rung and climbed every other one still pinned to
// the machine that had refused it (issue #266).
func (c *Client) wirePreferences(model string, knobs callKnobs) *providerPrefs {
	if knobs.noProvider {
		// THE ONE ENCODE THAT ASKS THE OPPOSITE QUESTION. See [callKnobs] —
		// this is the widened retry that finds out whether a base's 400 was
		// about the field, and it can only find out by sending none.
		return nil
	}
	prefs := hedgePreference(c.providerPreferences(model, knobs), knobs)
	if knobs.relaxed.has(relaxEndpointFilter) {
		prefs = relaxedPreferences(prefs)
	}
	if knobs.relaxed.has(relaxPriceCeiling) && prefs != nil {
		uncapped := *prefs
		uncapped.MaxPrice = nil
		prefs = &uncapped
	}
	// AND WHOEVER HAS ALREADY REFUSED THIS CALL COMES OFF LAST, after the ladder
	// has widened what it widens. The ladder's first rung takes every membership
	// filter off to ask whether a WIDER set can serve this shape; a machine that
	// refused this very call is not part of the question it is asking, and
	// putting it back would be the one thing THE BODY IS WRITTEN PER ATTEMPT
	// (retry.go) exists to stop.
	prefs = c.dropRefusedHere(prefs, knobs)
	// AND THE LAW READS WHAT IS ACTUALLY GOING OUT, after every hand that
	// narrows the set has had its say ([velocityLedger.keepTheSetServable]).
	c.velocity.keepTheSetServable(model, prefs)
	return prefs
}

// dropRefusedHere takes the machines that have already refused THIS CALL off the
// object about to go out.
//
// ── WHAT IT MAY NARROW AND WHAT IT MAY NOT ──────────────────────────────────
//
// `order` and `ignore` are RANKINGS AND VETOES and both are ours, so a refusing
// machine is simply struck from the first and added to the second.
//
// `only` IS A DEMAND AND IT DEPENDS HOW WIDE. A request that demanded ONE
// machine — a person's strict pin, or the machine a rescue's arm exists to try —
// is left exactly as it stands, whatever has refused it: there is no other
// machine for the next body to go to, so the one legal same-machine repeat is
// [control.Next]'s to authorize and the ladder's first rung is what takes the
// field off. That question is [demandedLane]'s and it is asked of it here rather
// than answered again: it is the same door the veto law's exemption reads and
// the same reading [requestSet] confines the plan to, so the set this narrows,
// the set the plan walks and the machine the law exempts cannot disagree.
//
// A DEMAND THE BELIEF MADE IS OURS, AND IT EMPTIES. It is the admitted set
// (internal/lane's demandOf), so a machine in it that has refused this call is a
// machine this build no longer wants — struck, like the ranking — and when every
// name has gone the demand goes with it. Keeping the last one is what asked a
// pool of two, with one refusing, to serve the request from the refuser while
// `allow_fallbacks: false` forbade the router the healthy machine it would
// otherwise have found. With the demand gone the router has its whole roster
// back for the one body that needs it, and the CALL still walks the set it
// decided on, because the plan reads the choice rather than this object
// ([requestSet]) and [control.Next] never returns a machine already tried.
//
// A CALL THAT HAS BEEN REFUSED BY NOBODY CHANGES NOTHING, which is every call
// on every healthy path: the object is returned untouched and the request is
// byte-for-byte the request it has always been.
//
// AND IT WILL BUILD AN OBJECT WHERE THERE WAS NONE, because a base that carries
// preferences and has had a machine refuse it has something to ask with even
// when nothing else about the request wanted a preference — the veto itself.
// A base that does not carry one is left alone: a field it 400s on is worse
// than a machine asked twice (prefcarry.go).
func (c *Client) dropRefusedHere(prefs *providerPrefs, knobs callKnobs) *providerPrefs {
	refused := knobs.refused.list()
	if len(refused) == 0 {
		return prefs
	}
	if prefs == nil {
		if !c.carriesPreferences() {
			return nil
		}
		return &providerPrefs{Ignore: refused}
	}
	narrowed := *prefs
	alone, _ := demandedLane(knobs)
	for _, name := range refused {
		if alone == "" {
			narrowed.Only = withoutEndpoint(narrowed.Only, name)
		}
		narrowed.Order = withoutEndpoint(narrowed.Order, name)
		// A DEMANDED MACHINE IS NEVER ALSO VETOED. The two fields would then say
		// opposite things about one name, and the router reads both — which is an
		// empty serving set written by us, in one object, about the machine we
		// just asked for.
		if !namesEndpoint(narrowed.Only, name) && !namesEndpoint(narrowed.Ignore, name) {
			narrowed.Ignore = append(narrowed.Ignore, name)
		}
	}
	// AND A DEMAND WITH NOTHING LEFT IN IT IS NO DEMAND. The field would
	// otherwise be dropped from the encoded body by `omitempty` while
	// `allow_fallbacks: false` stayed on it, which says "these machines and no
	// others" about no machines at all — the router's own "no allowed providers
	// are available", bought with a round trip. Both come off together, so the
	// one body that had nowhere left to go carries the request the way a request
	// with no belief behind it has always been carried.
	if prefs.Only != nil && len(narrowed.Only) == 0 {
		narrowed.Only = nil
		yes := true
		narrowed.AllowFallbacks = &yes
	}
	return &narrowed
}

// membershipNarrowing reports whether this preference object carries a
// membership restriction the first ladder rung can remove. max_price is
// deliberately separate: a wider set is tried under the same ceiling before
// aforge authorizes a dearer endpoint.
//
// It is the whole membership rule of the ladder's first rung, written once, so
// that the rung is offered exactly when it would do something
// ([Client.relaxationPlan]) and takes off exactly what it was offered for
// ([relaxedPreferences]). Two lists that had to agree were two lists that
// disagreed for a whole run: `only` could empty the set and was on neither.
func (p *providerPrefs) membershipNarrowing() bool {
	if p == nil {
		return false
	}
	return p.RequireParameters != nil || len(p.Ignore) > 0 || len(p.Only) > 0 || p.AllowFallbacks != nil
}

// keepTheSetServable is the one place AN IGNORE LIST NEVER EMPTIES THE SET THE
// REQUEST IS SENT TO is enforced.
//
// IT READS THE FINISHED OBJECT, LAST, because the two things that can empty a
// set arrive at different moments and neither can see the other. The ledger
// writes its vetoes while the request is being composed
// ([Client.providerPreferences]); a demand narrows the set to one machine
// afterwards, in the belief's own hand (lanes.go) or a rescue's
// ([hedgePreference]). So the law is asked once, of what is about to go out,
// rather than three times of three halves.
//
// IT IS ONE RULE OVER TWO DENOMINATORS, and naming them is the whole of it.
// WITH A DEMAND, the set is exactly what `only` names: `allow_fallbacks: false`
// forbids every other machine, so vetoes covering those names leave the router
// nothing, and it says so before asking any endpoint. WITHOUT ONE, the set is
// the router's whole roster, of which this process knows only the lanes it has
// timed. Either way the question is the same — is anything in the set still
// servable — and so is the answer when nothing is: the lane whose cooldown
// expires soonest is released. It is the one the ledger was about to forgive
// anyway, so it is the smallest departure from the ledger's own verdict, and
// every lane it is still surer about stays refused. A COVERED SET RELEASES ONE
// LANE AND NEVER THE LIST: a demand naming two machines with one of them vetoed
// is already servable through the other, and nothing is owed to the first.
//
// THE SECOND DENOMINATOR WAITS FOR EVIDENCE AND MAY NOT COUNT INSTEAD, which is
// the part that is easy to get wrong and was. This process cannot know how many
// machines serve a model, so "the vetoes cover everything I know" is routinely
// TRUE of a healthy ledger doing its job — one refusing lane written out of a
// set of five, and the router picks one of the four this process has never
// seen. Releasing on that arithmetic would send every request straight back to
// the machine that had just refused it, which is the measured failure a whole
// rule in [velocityLedger.preferences] exists to prevent. So it turns on
// [velocityLedger.coveringIgnoreRefused]: the router is the only authority on
// the size of the set, it says so in a sentence, and it is asked once per model.
// Once it has, [velocityLedger.reachableLanes] subtracts the machines that same
// refusal proved the router would not have sent to. The learning
// spares every lane on the transmitted ignore list,
// so every subtraction is sound even though this process cannot read the
// account's own exclusions. A demand needs neither evidence nor subtraction,
// because a demand IS the set and its refusal says nothing about machines
// outside it.
func (l *velocityLedger) keepTheSetServable(model string, prefs *providerPrefs) {
	if prefs == nil || len(prefs.Ignore) == 0 {
		return
	}
	set := prefs.Only
	if len(set) == 0 {
		if !l.coveringIgnoreRefused(model) {
			return
		}
		set = l.reachableLanes(model)
	}
	if len(set) == 0 {
		return
	}
	for _, name := range set {
		if !namesEndpoint(prefs.Ignore, name) {
			// Something in the set is still servable, so the vetoes have narrowed
			// it rather than emptied it, which is their whole job.
			return
		}
	}
	release := l.nearestForgiveness(model, set)
	if release == "" {
		return
	}
	prefs.Ignore = withoutEndpoint(prefs.Ignore, release)
	prefs.dropEmptyIgnore()
}

// pacedOut reports that EVERY machine this request may go to is being held for
// a wait, and when the first of those waits ends.
//
// ── WHY IT IS ASKED OF A SET AND NEVER OF A COUNT ───────────────────────────
//
// This process cannot count a model's machines: it knows only the ones it has
// itself timed, and "my vetoes cover everything I know" is routinely true of a
// healthy ledger doing its job — one refusing lane written out of a set of five,
// and the router picks one of the four this process has never seen
// ([velocityLedger.keepTheSetServable] carries the whole argument and the
// measured failure). So the denominator is the set THIS REQUEST is confined to:
// the machines a demand permits, else the candidate set the chooser drew. With
// neither — a call in a build where the router is not wired in — the honest
// answer is that there may well be somewhere else to go, and this says so.
//
// IT IS THE ONE FACT A 429 CANNOT BE ANSWERED WITHOUT. Waiting out a window
// while another machine is free is the defect this wave exists to close; moving
// when there is nowhere to move is a request spent to be told so again. The
// caller reads both halves: a conversation's call gives the failure back at once
// so the session can hop the model, and a task's call waits — with the moment
// this returns as the countdown a person actually watches (retry.go).
func (c *Client) pacedOut(model string, knobs callKnobs) (time.Time, bool) {
	set := requestSet(knobs)
	if len(set) == 0 || c.velocity == nil {
		return time.Time{}, false
	}
	return c.velocity.heldUntil(model, set)
}

// requestSet is the machines this request may go to, empty when it may go to
// any. A demand is the set outright; otherwise it is the candidate set the
// chooser drew, which is what this build believes the pool to be.
func requestSet(knobs callKnobs) []string {
	if knobs.hedgeLane != "" {
		return []string{knobs.hedgeLane}
	}
	if knobs.laneChoice == nil {
		return nil
	}
	if len(knobs.laneChoice.Only) > 0 {
		return knobs.laneChoice.Only
	}
	named := make([]string, 0, len(knobs.laneChoice.Frontier)+len(knobs.laneChoice.Order))
	for _, candidate := range knobs.laneChoice.Frontier {
		named = append(named, candidate.ID.Lane)
	}
	for _, lane := range knobs.laneChoice.Order {
		if !namesEndpoint(named, lane) {
			named = append(named, lane)
		}
	}
	return named
}

// heldUntil is when the first of a set's holds expires, and whether every one of
// them is held. A machine the ledger has never heard of is not held, which is
// what makes an unknown machine an answer rather than a wait.
func (l *velocityLedger) heldUntil(model string, set []string) (time.Time, bool) {
	if l == nil || len(set) == 0 {
		return time.Time{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	held := l.lanes[normalizeModel(model)]
	now := l.now()
	soonest := time.Time{}
	for _, name := range set {
		entry := held[strings.TrimSpace(name)]
		if entry == nil || entry.ignoredUntil.IsZero() || !now.Before(entry.ignoredUntil) {
			return time.Time{}, false
		}
		if soonest.IsZero() || entry.ignoredUntil.Before(soonest) {
			soonest = entry.ignoredUntil
		}
	}
	return soonest, true
}

// lanesKnown is every endpoint this process has timed for a model, in the order
// it first saw them.
func (l *velocityLedger) lanesKnown(model string) []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	held := l.lanes[normalizeModel(model)]
	ranked := make([]*lane, 0, len(held))
	for _, entry := range held {
		ranked = append(ranked, entry)
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].seen < ranked[j].seen })
	names := make([]string, 0, len(ranked))
	for _, entry := range ranked {
		names = append(names, entry.provider)
	}
	return names
}

// reachableLanes is every known endpoint except one the router has proved it
// would not have sent to, with the ledger's first-seen order preserved.
func (l *velocityLedger) reachableLanes(model string) []string {
	known := l.lanesKnown(model)
	if len(known) == 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	unreachable := l.unreachable[normalizeModel(model)]
	reachable := make([]string, 0, len(known))
	for _, name := range known {
		// A machine the account excludes is outside every model's set, and the
		// router has said so once already (internal/lane's account.go).
		if !unreachable[name] && !lanes.AccountExcludes(name) {
			reachable = append(reachable, name)
		}
	}
	return reachable
}

// nearestForgiveness is the name in `set` whose refusal expires soonest — the
// lane the ledger is closest to taking back on its own.
//
// A LANE THE LEDGER HAS NEVER TIMED IS NOT NEAREST ANYTHING. It carries no
// cooldown to be near the end of, so it sorts behind every lane that does, and
// the order `set` arrived in breaks the rest: which lane comes back must be a
// fact about the ledger rather than about map iteration.
func (l *velocityLedger) nearestForgiveness(model string, set []string) string {
	if len(set) == 0 {
		return ""
	}
	if l == nil {
		return set[0]
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	held := l.lanes[normalizeModel(model)]
	release, soonest := "", time.Time{}
	for _, name := range set {
		entry := held[name]
		if entry == nil {
			continue
		}
		if release == "" || entry.ignoredUntil.Before(soonest) {
			release, soonest = name, entry.ignoredUntil
		}
	}
	if release == "" {
		return set[0]
	}
	return release
}

// dropEmptyIgnore keeps an emptied list ABSENT rather than present and empty,
// which is the emptiness law spelled on the wire: `"ignore": []` is a sentence
// about no endpoints, and what we mean is that we are not asking.
func (p *providerPrefs) dropEmptyIgnore() {
	if len(p.Ignore) == 0 {
		p.Ignore = nil
	}
}

// relaxedPreferences is the preference object with every membership
// restriction taken out of it, leaving the ranking and price ceiling intact.
//
// It is the first rung of the endpoint-refusal ladder (endpoints.go), and it is
// the one rung that costs the answer nothing: the model is asked the identical
// question, of a wider set of machines. A preference object with nothing left in
// it is dropped entirely rather than sent empty.
func relaxedPreferences(prefs *providerPrefs) *providerPrefs {
	if prefs == nil {
		return nil
	}
	relaxed := *prefs
	relaxed.RequireParameters = nil
	relaxed.Ignore = nil
	// AND THE DEMAND COMES OFF WITH THEM, which was the field
	// that mattered. `only` names the machines this request may go to and
	// `allow_fallbacks: false` forbids any other — together they are the
	// narrowest filter this process ever sends, and the refusal they earn is
	// literally the router saying the set is empty. A ladder that dropped the
	// parameter filter and left the pin on climbed six rungs still pinned to
	// the machine that had said no (issue #266); the pin is what has to go, and
	// it goes first.
	//
	// A RESCUE THAT REACHES THIS RUNG HAS ALREADY LOST ITS ARGUMENT FOR THE
	// PIN. The demand exists so that a hedge cannot land on the lane that is
	// already stalling (hedge.go), and it is only ever relaxed on the LAST arm
	// — the one the race had no other machine to walk to — where the choice is
	// between a wider request and no answer at all.
	relaxed.Only = nil
	relaxed.AllowFallbacks = nil
	if relaxed.Sort == "" && len(relaxed.Order) == 0 && relaxed.MaxPrice == nil {
		return nil
	}
	return &relaxed
}

// ServedEndpoint is a slot one caller opens to be told WHICH endpoint answered
// its calls.
//
// It exists because [LastServed] cannot answer that question honestly for a
// caller: the ledger's latest sighting is process-wide, and two task nodes
// running the same model concurrently would each read the other's endpoint. The
// slot is scoped to the context the caller stamped, so what it holds is always
// an answer to one of that caller's own requests.
//
// It holds the MOST RECENT answer and nothing else. A caller stamps it around a
// turn and reads it beside each response, which is the grain the journal writes
// at (internal/session's addUsage).
//
// AND IT COUNTS THE HOPS, because that is the fact a cost autopsy needs and the
// one nobody could see: eleven moves across six endpoints in a single 41-request
// turn, every one of them a cold prompt cache (affinity.go). A journal line that
// records the endpoint alone shows where a request landed; [ServedEndpoint.Hops]
// and [ServedEndpoint.Pinned] show whether it stayed.
type ServedEndpoint struct {
	mu   sync.Mutex
	name string
	// pinned is whether the last answer came from the endpoint its request had
	// asked to come back to — a warm cache we kept, rather than one we found.
	pinned bool
	// hops counts how many times the answering endpoint CHANGED under this slot.
	// It starts at zero for the first answer, which is an arrival and not a move.
	hops int
}

// Name is the endpoint that answered most recently, empty when nothing has
// answered yet or when no answer named its server — which is every endpoint
// that is not a router.
func (s *ServedEndpoint) Name() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.name
}

// Pinned reports whether the most recent answer came from the endpoint its own
// request asked for by name — that is, whether this lineage kept the machine
// holding its prompt cache. False is a first request, a lineage with no cache
// key, a session with routing off, and every hop.
func (s *ServedEndpoint) Pinned() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pinned
}

// Hops is how many times the answering endpoint changed while this slot was
// open. Every hop is a prompt cache written from cold on the far side, so this
// is the number a cost autopsy reads first.
func (s *ServedEndpoint) Hops() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hops
}

// note records one answer: which endpoint served it, and which endpoint its
// request had asked to come back to ("" when it asked for none).
func (s *ServedEndpoint) note(name, asked string) {
	if s == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		// THE ATTRIBUTION LAW, the same one the ledger keeps: an answer whose
		// server did not identify itself replaces nothing. Blanking the slot
		// would turn one unnamed reply into "we no longer know" about the named
		// ones beside it.
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.name != "" && s.name != name {
		s.hops++
	}
	s.name = name
	s.pinned = asked != "" && asked == name
}

type servedEndpointContextKey struct{}

// WithServedEndpoint asks the adapter to write down, in the caller's own slot,
// which endpoint answered each call made under ctx.
func WithServedEndpoint(ctx context.Context, slot *ServedEndpoint) context.Context {
	if slot == nil {
		return ctx
	}
	return context.WithValue(ctx, servedEndpointContextKey{}, slot)
}

// ServedEndpointFrom returns the slot in force for ctx, nil when none was
// opened — which every method here answers correctly, so a caller never tests.
func ServedEndpointFrom(ctx context.Context) *ServedEndpoint {
	slot, _ := ctx.Value(servedEndpointContextKey{}).(*ServedEndpoint)
	return slot
}

// noteServed hands one answer's endpoint — and the endpoint its request asked
// to come back to, "" when none — to whatever slot the caller opened. It runs
// OUTSIDE the ledger's gates: a session with `routing off` has asked not to be
// steered, which is not a request to be lied to about who answered.
func noteServed(ctx context.Context, served, asked string) {
	slot, _ := ctx.Value(servedEndpointContextKey{}).(*ServedEndpoint)
	slot.note(served, asked)
	// AN ANSWER FROM A MACHINE IS PROOF THE ACCOUNT CAN REACH IT, which is what
	// a person switching a privacy setting back looks like from here; the
	// exclusion is taken back at once rather than at the end of its hold
	// (internal/lane's account.go).
	lanes.ClearAccountExclusion(served)
}

// noteVelocity folds one timed answer into this client's ledger.
//
// It is the one gate between the completion paths and the ledger, and it holds
// the OTHER half of what `routing off` means: a session that has asked for no
// routing preference is not measured either. Measuring it would build a ledger
// whose only possible use — demoting an endpoint on the next request — is a
// thing this client has just promised not to do.
// observed is what the usage frame said and whose request it answered
// (lanes.go's [settled]). Its cached count is how many of the prompt's tokens
// the ROUTER SAID it read back out of that endpoint's cache. That is the only
// direct evidence there is that a lane really held our prefix — every other
// reading of it is this process's own memory of where it sent the last request
// — and it is passed through rather than estimated, because an estimate of a
// cache hit is a discount nobody granted. Zero is "the frame did not say",
// which is also what a cold prefix looks like; the belief treats them the same
// and is right to, since neither is evidence of a cache.
//
// Its prompt length bounds the discount a lane earns for holding this
// conversation, and its lineage says which conversation that was. Both travel
// with the answer rather than being looked up at settlement, because a client
// serves several requests at once and the most recent encode is not this one.
func (c *Client) noteVelocity(model, served string, ttft time.Duration, tokens int, elapsed time.Duration, gap time.Duration, observed settled) {
	// THE LEDGER ALWAYS RECORDS, and the gate that used to stand here is gone
	// with the two on the refusal door ([Client.refuseLane]). The argument that
	// put it here — a demotion nobody can act on is only overhead — was only ever
	// half true: a good answer is what WALKS A REFUSAL BACK, so a session that
	// records refusals and not successes is a session whose machines go away and
	// never come back. What `routing off` still means is that nothing derived
	// from any of it reaches the wire.
	c.velocity.observe(model, served, ttft, tokens, elapsed, gap)
	// The same answer, folded into the belief that is replacing the table above
	// (lanes.go). It is one call rather than two seams because the two are the
	// same fact — who served, and how fast — and the strike ledger keeps its
	// half only until the belief has been proven against it.
	c.noteLane(model, served, ttft, tokens, elapsed, gap, observed)
}

// notePacedProvider folds one provider-named 429 into the ledger. Only the
// router's own errors carry a provider name to act on, and the retry loop calls
// this with the refusal body's named endpoint so every request encoded after it
// routes around the saturated pool instead of joining the queue behind it.
//
// AND THAT NOW INCLUDES THE RETRIES OF THE CALL THAT DREW IT. This used to end
// "the retries of the call that drew the 429 still wait it out, because their
// body is already written", and that sentence was the defect: the body is
// written per attempt now (retry.go), so the very next send of this same call is
// encoded around the pool as well.
func (c *Client) notePacedProvider(model, served string, wait time.Duration) {
	// A BELIEF SITE (#433): a pace is written against a NAMED lane, and lane
	// names come back only from a base that carries a preference. `pace` itself
	// refuses an unnamed one, which is the attribution law and the real floor —
	// and it is now the ONLY floor. THE LEDGER ALWAYS RECORDS; only what is
	// EMITTED from it is configured (see [Client.refuseLane]).
	c.velocity.pace(model, served, wait)
}

// noteCutProvider folds one guard-cut stream into the ledger under the same
// gate as notePacedProvider. A named endpoint that stopped producing an answer
// is stronger evidence than a merely slow completion, so it is refused at once
// and the retry encoded by the turn loop can route around it. An unnamed stream
// reaches pace too, where the attribution law leaves the ledger untouched.
//
// It REPORTS WHETHER IT STRUCK, because a caller has one question this is the
// only place that can answer: will the next attempt be routed away from the
// endpoint that just went quiet? See [StreamCut.Rerouted] for what is decided
// from it.
//
// ── AND A BLIND CUT STILL NAMES THE MACHINE WE ASKED FOR ────────────────────
//
// A stream that died before any chunk named its provider used to strike nothing
// at all, so the next attempt went back to the same lane deterministically and
// the one stall shape that could not reroute was the one that most needed to
// (streamguard.go's [StreamCut.Rerouted], census §1 finding 1). But this process
// is not blind about it: it WROTE the request, so it knows which machine it
// demanded and which it asked for first, and the router honours `only` outright
// and `order` before anything else. That is the honest answer to "who was
// serving" when the wire gave none, and it is the one this falls back on.
//
// IT IS A PRIOR AND NOT A VERDICT, which is why it is safe to be wrong: a pace
// is a five-minute cooldown that the machine's own next good answer walks back
// ([velocityLedger.observe]), not a fact filed against it for the run.
func (c *Client) noteCutProvider(ctx context.Context, model, served string) bool {
	// A BELIEF SITE (#433): a strike against a named lane. The attribution law
	// below — in `pace` — is the floor; the configuration gate that used to stand
	// here is gone, because a ledger switched off is a ledger that cannot tell
	// the next attempt where NOT to go (see [Client.refuseLane]).
	if strings.TrimSpace(served) == "" {
		served = askedFor(ctx)
	}
	return c.velocity.pace(model, served, 0)
}

// askedFor is the machine THIS REQUEST asked for, which is the only honest
// answer about who was serving when no chunk ever said: a rescue's demand
// first, then a strict demand, then whatever the ranking named to try first.
// Empty when the request expressed no preference at all, which is a build with
// no router behind it and a set of one.
func askedFor(ctx context.Context) string {
	if lane := strings.TrimSpace(hedgeLaneFrom(ctx)); lane != "" {
		return lane
	}
	choice, made := laneChoiceFromContext(ctx)
	if !made {
		return ""
	}
	if len(choice.Only) == 1 {
		return strings.TrimSpace(choice.Only[0])
	}
	if len(choice.Order) > 0 {
		return strings.TrimSpace(choice.Order[0])
	}
	return ""
}

// noteRun and streamWall are the two halves of the stream wall's evidence, and
// they are the ONLY pair on this file that is not gated on routing.
//
// The gate above exists because a demotion nobody can act on is overhead. A
// wall is acted on by every session — the cut happens, the ladder re-asks — so
// gating its history would leave `routing off` running on the bare floor with
// no way to earn anything better. See [velocityLedger.runs].
func (c *Client) noteRun(model, served string, ran time.Duration) {
	if c.velocity == nil {
		return
	}
	c.velocity.noteRun(model, served, ran)
}

// streamWall is how long the stream about to be opened, or the one now known to
// be served by `served`, may run before it is cut. See streamguard.go's THE
// WALL for the law and the constants.
func (c *Client) streamWall(model, served string) time.Duration {
	if c.velocity == nil {
		return wallFor(0)
	}
	return c.velocity.wall(model, served)
}

// streamGap is how long the stream about to be opened, or the one now known to
// be served by `served`, may go quiet between two tokens. See streamguard.go's
// [gapFor] for the law.
func (c *Client) streamGap(model, served string) time.Duration {
	if c.velocity == nil {
		return gapFor(0)
	}
	return gapFor(c.velocity.rate(model, served))
}

// streamPace is the rate, in tokens a second, the stream about to be opened —
// or the one now known to be served by `served` — is expected to keep, which is
// what its wall asks about before it cuts. See streamguard.go's [paceFor].
func (c *Client) streamPace(model, served string) float64 {
	if c.velocity == nil {
		return paceFor(0)
	}
	return paceFor(c.velocity.rate(model, served))
}

// completionWall is the wall a reply that is NOT streamed is held to, and
// whether there is one. A stream on a lane nothing is known about gets the
// floor, because silence bounds it as well; a completion has only its total
// deadline, sized from the room it was given, and on an unknown lane that
// deadline stays — a model that thinks at max regardless may need every
// minute of it the first time. Once the lane has finished a reply for us the
// measured wall applies to completions exactly as it does to streams, because
// a reply running five times longer than the longest this endpoint ever
// finished is a wedge, however it is being delivered. Measured 2026-08-29: a
// headless worker's completion on deepseek-flash sat in flight for the whole
// rest of a fifteen-minute run, on a lane whose longest finished reply was
// twenty-four seconds.
func (c *Client) completionWall(model string) (time.Duration, bool) {
	if c.velocity == nil || !c.velocity.measured(model) {
		return 0, false
	}
	return c.velocity.wall(model, ""), true
}

// refuseUpstream takes the lane away from an endpoint that REFUSED this request,
// so the next encode routes around it, and hands back what the refusal was.
//
// ── THE MEASURED FAILURE ────────────────────────────────────────────────────
//
// SWE-Marathon run s2 died on `after 3 retries: API error (400): Provider
// returned error`. Three retries, and nothing between them moved: releasing the
// pin (affinity.go) only stops this process ASKING for that endpoint — it does
// not stop the router choosing it again, and a router with a warm pool chooses
// the same member every time. The three attempts were three deliveries of the
// same request to the same upstream, and the turn ended with five hours of the
// ask unspent.
//
// THE LAW: AN UPSTREAM THAT REFUSED IS ROUTED AROUND, NOT ASKED AGAIN. It is
// the same verdict a 429 that names its pool earns and the same one a cut stream
// earns — a lane this process has decided not to send to — and it travels the
// same way, in `provider.ignore` on every request encoded after it.
//
// IT STRIKES A LANE OR IT STRIKES NOTHING, and which of the two is not this
// function's to decide: it is reader (a) of the one refusal object
// (refusalobject.go), and every question about what a refusal MEANS is answered
// there. A 4xx over our own bytes that demanded no machine names no lane and
// strikes nothing — there is no endpoint to blame for a malformed request, and
// refusing endpoints over our own bytes would empty the ledger one attempt at a
// time. A 429 is paced rather than struck, for the wait it named
// ([Client.refuseLane]).
//
// `served` is the machine this process knows was answering, empty when it knows
// of none, and `wait` is the comeback the provider asked for, zero when it
// asked for none.
// It is carried on this signature rather than looked up because only the caller
// holding the response can read a `Retry-After` header, and a 429 that reaches
// the ledger without its named wait is held for the flat cooldown instead of
// the minute the pool actually asked for.
//
// ── AND THE ROUTER'S OWN REFUSAL IS NO LONGER EXEMPT ────────────────────────
//
// It used to require `provider_name` in the error metadata, which OpenRouter
// puts there exactly when it is relaying somebody ELSE'S refusal — so the one
// refusal that is certain about a lane, the router's own
// `…your request's provider.only preference permits only: coreweave`, was
// structurally unable to strike the lane it named. It carried no
// `provider_name` because the router was answering for itself, so the strike
// declined it, and the same machine was chosen three more times in one run
// (issue #266). The lane a refusal is about now comes from OUR OWN REQUEST, and
// a router refusal against a demanded machine is terminal for that pairing: it
// is both paced here and written out of the serving set, because a pin the
// frontier can still choose is a pin that comes back on the next turn.
//
// IT HANDS THE CLASSIFICATION BACK, and that is the whole reason it returns an
// object rather than the bool it used to. The retry loop has a second question
// about the same refusal — which machine must be off the NEXT body (retry.go's
// [refusedHere]) — and asking the classifier again from there would be the
// classification happening twice, which is the defect refusalobject.go closed.
// Whether anything was written is still readable, as [laneRefusal.struck].
func (c *Client) refuseUpstream(request *ai.Request, knobs callKnobs, err error, served string, wait time.Duration) laneRefusal {
	// THE SERVED NAME IS STAMPED FIRST, AND HERE. A refusal delivered inside an
	// open stream carries no `provider_name` — the stream named its provider in
	// the chunks — so the name is folded onto the error before anything reads
	// it, and the ledger below, [RefusalFrom]'s callers and the journal's error
	// row all see one fact instead of two ([nameServed]).
	err = nameServed(err, served)
	refusal := c.refusalObject(request, knobs, err)
	markRefusal(err, refusal)
	c.refuseLane(c.modelFor(request), refusal, wait)
	return refusal
}

// refuseLane is THE ONE REFUSAL DOOR: everything this client does to the ledger
// about a refusal happens here, once, whatever transport the refusal arrived
// over and whatever status it wore.
//
// ── THE MEASURED FAILURE (2026-09-10) ───────────────────────────────────────
//
// A chat turn on deepseek/deepseek-v4.1-flash died after four transport
// attempts: one 502 from DeepInfra, and then three 429s served by Io Net at
// 13:00:39, 13:01:04 and 13:01:32. All three arrived INSIDE an already-open
// HTTP 200 — the journal's rows say status 200 and
// `API error (429): Provider returned error (via Io Net)` — and the pacing note
// lived on the status path alone, where it was reached by reading
// `response.StatusCode` (retry.go). A 429 delivered inside a 200 never got
// there, so it reached NO ledger at all: nothing was written, the next encode
// carried the same preferences, and the router handed the request straight back
// to the saturated pool three times in ninety-three seconds.
//
// THE LAW: A REFUSAL IS ACTED ON FROM WHAT IT SAYS, NEVER FROM WHERE IT WAS
// READ. Both transports build the same [APIError] by the same function
// ([streamRefusal] → [apiError]) and both classify it with the same object
// (refusalobject.go), so both reach this door with the same value and cannot
// drift apart again. The door then does one of exactly three things:
//
//	a paced lane   held for the wait it named ([Client.notePacedProvider])
//	a struck lane  written out of the serving set and routed around
//	nothing        an account-wide limit, or our own bytes being wrong
//
// IT IS ALSO AN ENTRANCE FOR A CALLER THAT HAS ALREADY CLASSIFIED, for
// [Client.laneRefusalFor]'s reason exactly: the fork every routing refusal
// passes through (client.go's [Client.sendRecovered]) holds the object already,
// and asking the classifier a second time from there would be the
// classification happening twice — which is the whole defect refusalobject.go
// closed. [Client.refuseUpstream] is this function with the classification in
// front of it.
//
// STRIKING TWICE IS HARMLESS AND IS RELIED ON. A refusal that reaches a caller
// as a 4xx is struck at this seam and struck again by whoever reads the status;
// both halves are writes of a state rather than counters ([lane.RefuseServing]
// files a moment, [velocityLedger.pace] sets strikes rather than incrementing
// them), so the second is the first said again.
//
// ── AND IT IS NO LONGER SWITCHED OFF BY CONFIGURATION ───────────────────────
//
// Three gates used to stand at the top of this door and its two paces:
// `c.velocity == nil || !c.carriesPreferences() || c.routing() == RoutingOff`.
// With routing off, or on a base that had not yet shown it carries a preference,
// NOTHING was paced, struck or learned — while every other controller on the
// path went on acting, so the one thing that could tell the next attempt where
// not to go was the one thing that had been silenced
// (docs/design/recovery/DESIGN.md §2, problem 9).
//
// THE LAW: THE LEDGER ALWAYS RECORDS; ONLY WHAT IS EMITTED FROM IT IS
// CONFIGURED. A base that is not a router gets a ledger with one row in it, not
// no ledger. Nothing reaches the wire that was not already allowed to:
// [Client.providerPreferences] returns nil for `routing off` and for a base that
// does not carry a preference, and [Client.drawLaneChoice] answers no choice for
// the first — so the reading is kept and the steering is not. What it buys is
// that a refusal is still a fact anybody can act on: the retry loop's own
// exclusions, the stream cut's [StreamCut.Rerouted], the wall, the frontier.
//
// The attribution law is untouched and is the real floor: an unnamed machine is
// written nowhere ([velocityLedger.pace]).
func (c *Client) refuseLane(model string, refusal laneRefusal, wait time.Duration) bool {
	if refusal.paced() {
		// A NAMED POOL IS PACED, NOT WRITTEN OFF. It said its queue is full, not
		// that it cannot serve this model, so it is held for the wait it asked
		// for and comes back on its own — and meanwhile every request encoded
		// from here on routes around it instead of queueing behind it, the
		// remaining attempts of this very call included (retry.go).
		c.notePacedProvider(model, refusal.Lane, wait)
		c.noteLaneRefused(model, refusal.Lane, "paced")
		// AND THE ROUTER'S GATE HEARS THE SAME REFUSAL ONCE, WITH THE ATTEMPT'S
		// SHAPE (routefirst.go's [Client.noteRouterRefusal]): only a bare
		// attempt's refusal is evidence about the router's default; a narrowed
		// demand's refusal is a verdict on the pick, and the gate hears nothing.
		if c.noteRouterRefusal(model, refusal.Lane, refusal.AskedBare) {
			parkTakeoverLine(model)
		}
		return true
	}
	// AN ACCOUNT'S EXCLUSION IS WRITTEN FOR EVERY MODEL, and it is written here
	// because this is the one door every refusal passes (see above). The machine
	// was never asked, so nothing below strikes it — [laneRefusal.struck] reads
	// Unasked — but the next demand of it, on any model, in this process or the
	// next, is a 404 already paid for (internal/lane's account.go).
	if refusal.Account {
		lanes.ExcludeForAccount(refusal.Lane, "the account's own settings exclude it")
	}
	if !refusal.struck() {
		return false
	}
	// A STRUCK LANE IS NOT TOLD TO THE AVAILABILITY AXIS. It already reaches
	// the belief twice — [refuseServing]'s thirty-minute hold on the serving set
	// and the quality outcome terminal_error.go files — and a third entry would
	// count one refusal three times. The paced branch above is the one that
	// was silent, and the one the 429 loop lived in.
	c.refuseServing(model, refusal)
	// AND THE GATE HEARS IT HERE, BESIDE THE STRIKE, WITH THE ATTEMPT'S SHAPE
	// (routefirst.go's [Client.noteRouterRefusal]): only a bare attempt's
	// refusal is evidence about the router's default, exactly as in the paced
	// branch. An unasked machine (refusal.Unasked) still teaches nothing and
	// never reaches this line.
	if c.noteRouterRefusal(model, refusal.Lane, refusal.AskedBare) {
		parkTakeoverLine(model)
	}
	return c.velocity.pace(model, refusal.Lane, 0)
}

// WHICH ENDPOINT A 429 CAME FROM IS READ IN ONE PLACE, and it is [apiError]:
// the refusal object already carries `error.metadata.provider_name` in its
// Provider field, for every status and both transports. A second decoder of the
// same field used to sit here for the retry loop's use, which is how the status
// path and the stream path came to know different things about the same
// sentence — see [Client.refuseLane] for what that cost.

// ── THE LAG LAW ─────────────────────────────────────────────────────────────
//
// The four constants below are the whole of it. They are stated once, here,
// because a threshold buried in a branch is a threshold nobody can argue with.

const (
	// LagTTFT is the wait before the first token that counts as slow. Two
	// seconds is roughly four times what a warm endpoint takes to start
	// answering, so crossing it is a claim about the endpoint rather than about
	// a large prompt.
	LagTTFT = 2000 * time.Millisecond

	// LagRate is the sustained output rate, in tokens per second, below which
	// an endpoint is slow. Thirty is about a third of what the small fast models
	// this surface rides sustain, and comfortably under the slowest large one.
	LagRate = 30.0

	// LagGap is the widest quiet stretch INSIDE a successful answer that still
	// counts as streaming. Above it the endpoint is assembling the reply
	// server-side and delivering it in lumps — a shape measured across one
	// model's sixteen endpoints on 2026-08-24, where every endpoint that
	// streamed stayed under four seconds between deltas and every one that
	// buffered sat at twelve seconds or worse, up to fifty. Fifteen sits in
	// the empty middle. A lumped answer that arrives is still an answer, which
	// is why this is a lag strike and never a cut: the lane is demoted below
	// the endpoints that stream, and the stall guard's patience (streamguard.
	// go's bufferedQuietBound) is what keeps the lump survivable meanwhile.
	LagGap = 15 * time.Second

	// demoteAfter is how many laggy answers an endpoint gets before the next
	// request prefers something else. TWO rather than one: a single slow answer
	// is as likely to be a cold cache or a long prompt as a bad endpoint, and
	// acting on one measurement is how a ledger learns noise.
	demoteAfter = 2

	// ignoreAfter is how many laggy answers it takes to be refused outright.
	ignoreAfter = 3

	// ignoreCooldown is how long a refusal lasts. Five minutes is long enough to
	// outlive the load spike that usually caused it and short enough that a
	// session never permanently loses an endpoint over one bad stretch.
	ignoreCooldown = 5 * time.Minute

	// ratedFloor is the fewest output tokens a rate may be computed from.
	// A twelve-token answer is over before the connection is warm, and its
	// "rate" measures the handshake. Below the floor only the TTFT judges.
	ratedFloor = 32
)

// Sighting is one timed answer: who served it, and how fast.
//
// It is the ledger's unit and the HUD's fact, which is why it carries both the
// raw measurements and the verdict — a surface must not have to re-derive
// "was this slow" from thresholds it would then own a second copy of.
type Sighting struct {
	// Model is the model as it was asked for, normalized.
	Model string
	// Provider is the endpoint the router says served it, exactly as the
	// response spelled it.
	Provider string
	// TTFT is the wait before the first token, zero when unmeasured — which is
	// every non-streamed call, where there is no first token to observe.
	TTFT time.Duration
	// Tokens is what the answer was worth in output tokens, and Elapsed is the
	// window Rate was computed over.
	Tokens  int
	Elapsed time.Duration
	// Rate is output tokens per second, zero when the answer was too short to
	// rate (see [ratedFloor]).
	Rate float64
	// Gap is the widest quiet stretch between two deltas of a streamed answer,
	// zero when unmeasured — every non-streamed call, where the answer has no
	// inside to be quiet in.
	Gap time.Duration
	// Laggy is the verdict this sighting earned under the law above.
	Laggy bool
	// At is when the answer finished.
	At time.Time
}

// lane is one endpoint's standing with one model.
type lane struct {
	provider string
	// strikes counts laggy answers, and it FALLS on a fast one: a lane that
	// recovers walks back out the way it walked in, so a busy hour cannot
	// permanently condemn an endpoint that is now the fastest thing available.
	strikes int
	// ignoredUntil is when a refusal expires. Zero is not refused.
	ignoredUntil time.Time
	// refused says the lane was taken away because IT DID NOT SERVE — a 429 that
	// named its pool, a stream that went quiet, an upstream that answered 4xx —
	// rather than because it served SLOWLY. The two are different claims and the
	// "never condemn everything" rule in [velocityLedger.preferences] treats them
	// differently; see the law stated there.
	refused bool
	seen    int
	last    Sighting
}

// velocityLedger is what this process has measured, in memory, per model.
//
// It is in-memory ON PURPOSE. Endpoint speed is a fact about the last few
// minutes — a replica that was saturated at noon is the fastest one at ten past
// — and a ledger that survived a restart would open every session by acting on
// a claim it could no longer see. Nothing here is written to disk, nothing here
// costs a call, and nothing here is billed.
type velocityLedger struct {
	mu    sync.Mutex
	now   func() time.Time
	lanes map[string]map[string]*lane
	last  map[string]Sighting
	// runs is model → endpoint → the LONGEST REPLY THAT ENDPOINT HAS FINISHED
	// for this process. It is what the stream wall is derived from
	// (streamguard.go's [wallFor]), and it is kept apart from `lanes` for two
	// reasons.
	//
	// FIRST, IT IS NOT A STRIKE. A lane's entry above is a standing — demoted,
	// refused, on probation — and every write to it is a steering decision.
	// This is a measurement of duration and nothing else; folding it into a
	// lane would make "how long does this endpoint take" and "should we send
	// there" one field, and the two are asked at different moments by different
	// code.
	//
	// SECOND, IT IS RECORDED EVEN WITH `routing off`. The ledger above is not:
	// an operator who asked for no steering asked for no demotions either, and
	// the file says so. But a WALL is acted on whatever routing says — the cut
	// happens, the request is re-asked — so the history it is derived from has
	// to exist under routing off too, or every such session would run on the
	// bare floor forever. Nothing here can move a request to another endpoint,
	// so recording it steers nothing.
	runs map[string]map[string]time.Duration
	// noCeiling is model → "the price ceiling has emptied this model's endpoint
	// set once, do not send it again". It is process-lifetime and never expires,
	// because what it records is not a lane's mood but the ACCOUNT'S privacy
	// policy meeting the catalog's list price: the ceiling is list × 1.25, the
	// only endpoint under it is the first-party one, and the account has that
	// provider switched off. Nothing about that changes between one call and
	// the next, so a memo that expired would just buy the same 404 back.
	noCeiling map[string]bool
	// coveredIgnore is model → "an ignore list covering everything this process
	// knows has emptied this model's serving set once, do not send one again".
	//
	// IT IS EVIDENCE RATHER THAN ARITHMETIC, and that is the whole reason it
	// exists. This process cannot count a model's endpoints: it knows only the
	// lanes it has itself timed, and a single refusing lane written out of a
	// five-machine set is the ledger working exactly as intended — the router
	// picks one of the four the ledger has never seen. So "the vetoes cover
	// everything I know" is not a reason to believe the set is empty. The one
	// authority on that question is the router, which says so in a sentence,
	// and it is asked once per model rather than on every request.
	//
	// Process-lifetime, like the ceiling's memo above and for the same reason:
	// what it records is the shape of a model's serving set, which does not
	// change between one call and the next.
	coveredIgnore map[string]bool
	// unreachable is model → endpoint → "the router would not have sent to this
	// machine". A covering-ignore refusal from the router is the only evidence
	// that adds an entry, and a served answer is the only evidence that clears
	// one.
	//
	// The transmitted ignore list is the evidence: a cooldown that expired
	// while the response traveled back still belonged to us on that request.
	unreachable map[string]map[string]bool
}

func newVelocityLedger() *velocityLedger {
	return &velocityLedger{
		now:           time.Now,
		lanes:         map[string]map[string]*lane{},
		last:          map[string]Sighting{},
		runs:          map[string]map[string]time.Duration{},
		noCeiling:     map[string]bool{},
		coveredIgnore: map[string]bool{},
		unreachable:   map[string]map[string]bool{},
	}
}

// refuseCeiling records that the router emptied model's endpoint set on price
// or policy grounds while a ceiling was on the request. From here on
// [Client.providerPreferences] sends no max_price for that model.
func (v *velocityLedger) refuseCeiling(model string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.noCeiling[normalizeModel(model)] = true
}

// ceilingRefused reports whether [refuseCeiling] has been called for model.
func (v *velocityLedger) ceilingRefused(model string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.noCeiling[normalizeModel(model)]
}

// holdsVetoes reports whether this process currently refuses any endpoint for a
// model — that is, whether a veto of ours was in play when a request went out.
//
// IT IS A PURE READ, AND THAT IS THE WHOLE REASON IT EXISTS. The obvious way to
// ask "did the refused request carry our list" is to build the object again and
// look; [Client.onlyLane] does exactly that for its one field and says why it is
// safe THERE. It is not safe here. Rebuilding runs [velocityLedger.preferences],
// which EXPIRES cooldowns as it goes, and the lane chooser, which is a sampled
// decision that records the ask it drew — so a question asked on the way back
// would quietly move the ledger and rewrite the attribution of a request that
// was never sent. Nothing here writes anything.
//
// It answers a slightly weaker question than "the wire carried it", and the
// difference does not matter: the memo it guards only ever permits releasing a
// lane when the vetoes cover every lane this process knows, which cannot happen
// unless this process was vetoing.
func (l *velocityLedger) holdsVetoes(model string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for _, entry := range l.lanes[normalizeModel(model)] {
		if !entry.ignoredUntil.IsZero() && now.Before(entry.ignoredUntil) {
			return true
		}
	}
	return false
}

// refuseCoveringIgnore records that the router answered an ignore list covering
// everything this process knows with an empty serving set. From here on
// [velocityLedger.keepTheSetServable] releases a lane rather than send one for
// that model.
func (v *velocityLedger) refuseCoveringIgnore(model string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.coveredIgnore[normalizeModel(model)] = true
}

// learnUnreachable records the lanes a covering-ignore refusal proved the
// router would not have sent to. It reads the transmitted ignore list rather than
// current cooldowns, which may have expired while the response was in flight.
func (l *velocityLedger) learnUnreachable(model string, ignored []string) {
	if l == nil {
		return
	}
	key := normalizeModel(model)
	l.mu.Lock()
	defer l.mu.Unlock()
	for name := range l.lanes[key] {
		if namesEndpoint(ignored, name) {
			continue
		}
		if l.unreachable[key] == nil {
			l.unreachable[key] = map[string]bool{}
		}
		l.unreachable[key][name] = true
	}
}

// coveringIgnoreRefused reports whether [refuseCoveringIgnore] has been called
// for model.
func (v *velocityLedger) coveringIgnoreRefused(model string) bool {
	if v == nil {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.coveredIgnore[normalizeModel(model)]
}

// noteRun remembers how long one COMPLETED reply took, under the endpoint that
// served it.
//
// COMPLETED IS THE WHOLE TEST OF HEALTH HERE, and it is a deliberately weaker
// word than the one the lag law uses. A lane demoted for slowness still
// answered; its answers took as long as they took, and a wall that excluded
// them would cut the next one and turn a speed verdict into an outage. The
// question this is evidence for is not "was that fast" but "has this lane ever
// legitimately taken this long", and only a reply that arrived can answer it.
//
// It keeps the maximum rather than the last or a mean. A wall wants the widest
// legitimate reply, because that is the one it must not cut; the average would
// cut half of them.
//
// A reply from an endpoint that never named itself is recorded under the empty
// name, where it still counts towards the lineage's widest wall and belongs to
// no lane — the same attribution law observe follows, for the same reason.
func (l *velocityLedger) noteRun(model, served string, ran time.Duration) {
	if l == nil || ran <= 0 {
		return
	}
	key := normalizeModel(model)
	if key == "" {
		return
	}
	served = strings.TrimSpace(served)
	l.mu.Lock()
	defer l.mu.Unlock()
	byLane := l.runs[key]
	if byLane == nil {
		byLane = map[string]time.Duration{}
		l.runs[key] = byLane
	}
	if ran > byLane[served] {
		byLane[served] = ran
	}
}

// wall is how long the next reply on this (model, endpoint) may run.
//
// A NAMED LANE IS ASKED ABOUT ITSELF FIRST, and falls back to the lineage's
// widest when this process has never seen it finish anything. That fallback is
// the difference between a wall that works and one that fights the router: an
// endpoint the ledger has just steered a long session onto is new by
// construction, and giving its first reply the bare floor would cut exactly the
// work the steering was for.
//
// An unnamed ask — a request that has not yet learned who is serving it — gets
// the lineage's widest, which is the most generous honest answer available
// before the first chunk arrives.
// measured says whether any endpoint serving the model has finished a reply
// in this process — the difference between a wall that was derived and the
// floor handed to a stranger.
func (l *velocityLedger) measured(model string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.runs[normalizeModel(model)]) > 0
}

func (l *velocityLedger) wall(model, served string) time.Duration {
	if l == nil {
		return wallFor(0)
	}
	key := normalizeModel(model)
	l.mu.Lock()
	defer l.mu.Unlock()
	byLane := l.runs[key]
	if len(byLane) == 0 {
		return wallFor(0)
	}
	served = strings.TrimSpace(served)
	if served != "" {
		if longest, seen := byLane[served]; seen {
			return wallFor(longest)
		}
	}
	widest := time.Duration(0)
	for _, longest := range byLane {
		if longest > widest {
			widest = longest
		}
	}
	return wallFor(widest)
}

// rate is the output rate, in tokens per second, that this process has last
// measured for one (model, endpoint) — the same figure the status line prints
// (velocity.go's [Sighting.Rate]), read here so that patience can be stated in
// it. Zero is a lane nothing has been rated for.
//
// A NAMED LANE IS ASKED ABOUT ITSELF FIRST, and falls back to the SLOWEST rate
// any endpoint of this model has shown. That fallback is the mirror image of
// [velocityLedger.wall]'s, and for the same reason: before the first chunk names
// who is serving, the most generous honest answer is the one that grants the
// most patience, and on a rate the most generous answer is the smallest number.
func (l *velocityLedger) rate(model, served string) float64 {
	if l == nil {
		return 0
	}
	key := normalizeModel(model)
	l.mu.Lock()
	defer l.mu.Unlock()
	byLane := l.lanes[key]
	if len(byLane) == 0 {
		return 0
	}
	if served = strings.TrimSpace(served); served != "" {
		if entry, seen := byLane[served]; seen && entry.last.Rate > 0 {
			return entry.last.Rate
		}
	}
	slowest := 0.0
	for _, entry := range byLane {
		if rate := entry.last.Rate; rate > 0 && (slowest == 0 || rate < slowest) {
			slowest = rate
		}
	}
	return slowest
}

// sharedVelocity is the ledger every client built by [NewClient] folds into.
//
// It is process-wide for the reason the quirks memo is: the fact belongs to the
// model and its endpoints, not to whichever adapter happened to be holding the
// connection, and a surface's HUD has no handle on the adapter its session
// built. A test that wants isolation builds its own ledger and assigns it.
var sharedVelocity = newVelocityLedger()

// LastServed is the most recent sighting for a model, false when this process
// has not seen one. It is the HUD's read and it is a copy: nothing a surface
// does can reach the ledger's state.
func LastServed(model string) (Sighting, bool) { return sharedVelocity.lastServed(model) }

func (l *velocityLedger) lastServed(model string) (Sighting, bool) {
	if l == nil {
		return Sighting{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	sighting, ok := l.last[normalizeModel(model)]
	return sighting, ok
}

// observe folds one timed answer in and returns what it decided.
//
// An answer whose server did not identify itself is measured and remembered as
// the model's latest sighting, but earns no strike: a strike is a claim about
// an endpoint, and there is no endpoint here to make it about.
func (l *velocityLedger) observe(model, served string, ttft time.Duration, tokens int, elapsed time.Duration, gap time.Duration) Sighting {
	if l == nil {
		return Sighting{}
	}
	key := normalizeModel(model)
	if key == "" {
		return Sighting{}
	}
	sighting := Sighting{
		Model:    key,
		Provider: strings.TrimSpace(served),
		TTFT:     ttft,
		Tokens:   tokens,
		Elapsed:  elapsed,
		Gap:      gap,
	}
	if tokens >= ratedFloor && elapsed > 0 {
		sighting.Rate = float64(tokens) / elapsed.Seconds()
	}
	sighting.Laggy = (ttft > 0 && ttft > LagTTFT) || (sighting.Rate > 0 && sighting.Rate < LagRate) || gap > LagGap

	l.mu.Lock()
	defer l.mu.Unlock()
	sighting.At = l.now()
	l.last[key] = sighting
	if sighting.Provider == "" {
		return sighting
	}
	// A machine that answered is reachable, and that is the only evidence that
	// can undo what the router's refusal taught this process.
	delete(l.unreachable[key], sighting.Provider)
	lanes := l.lanes[key]
	if lanes == nil {
		lanes = map[string]*lane{}
		l.lanes[key] = lanes
	}
	entry := lanes[sighting.Provider]
	if entry == nil {
		entry = &lane{provider: sighting.Provider, seen: len(lanes)}
		lanes[sighting.Provider] = entry
	}
	entry.last = sighting
	switch {
	case sighting.Laggy:
		entry.strikes++
		if entry.strikes >= ignoreAfter {
			entry.ignoredUntil = sighting.At.Add(ignoreCooldown)
			// A SLOWNESS VERDICT, and it says so: this lane answered, three times,
			// too slowly. That is a different claim from a lane that refused, and
			// [velocityLedger.preferences] is allowed to weigh it differently.
			entry.refused = false
		}
	case entry.strikes > 0:
		// A fast answer pays a strike back. It also lifts a refusal, which can
		// only be observed after the cooldown let the endpoint be tried again —
		// that retry succeeding is exactly the evidence the refusal was for.
		entry.strikes--
		entry.ignoredUntil = time.Time{}
		entry.refused = false
	}
	return sighting
}

// preferences is the ledger's verdict for the next request: which endpoints to
// try first, and which to refuse.
//
// THE LAW, in the order it applies:
//
//	refused    strikes ≥ 3, until the cooldown expires → provider.ignore
//	probation  a refusal that has expired comes back demoted, one strike short
//	           of being refused again, so an endpoint that is still slow is
//	           dropped by its very next answer instead of getting a fresh three
//	demoted    strikes ≥ 2 → last in provider.order, behind every healthy lane
//	healthy    everything else, in the order it was first seen
//
// Order is emitted ONLY when a healthy lane exists. `provider.order` names what
// to try first, so a list containing nothing but demoted endpoints would pin
// the slowest thing we know to the front — the exact inverse of the intent. In
// that case the sort word is left to choose and the refusals still stand.
func (l *velocityLedger) preferences(model string) (order []string, ignore []string) {
	if l == nil {
		return nil, nil
	}
	key := normalizeModel(model)
	l.mu.Lock()
	defer l.mu.Unlock()
	lanes := l.lanes[key]
	if len(lanes) == 0 {
		return nil, nil
	}
	now := l.now()
	ranked := make([]*lane, 0, len(lanes))
	for _, entry := range lanes {
		ranked = append(ranked, entry)
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].seen < ranked[j].seen })

	var healthy, demoted []string
	// refusedSomething says at least one of the names going into `ignore` is
	// there because it DID NOT SERVE rather than because it served slowly. The
	// rule below turns on it.
	refusedSomething := false
	for _, entry := range ranked {
		if !entry.ignoredUntil.IsZero() {
			if now.Before(entry.ignoredUntil) {
				ignore = append(ignore, entry.provider)
				refusedSomething = refusedSomething || entry.refused
				continue
			}
			entry.ignoredUntil = time.Time{}
			entry.strikes = ignoreAfter - 1
			entry.refused = false
		}
		if entry.strikes >= demoteAfter {
			demoted = append(demoted, entry.provider)
			continue
		}
		healthy = append(healthy, entry.provider)
	}
	// THE LEDGER MAY NEVER CONDEMN EVERYTHING IT KNOWS OVER SPEED. On a model
	// with one provider — and single-provider models are common — three slow
	// answers used to put that one name in `ignore` and turn every request for
	// five minutes into an instant "All providers have been ignored" 404. A
	// SLOWNESS verdict that condemns the whole set is not a preference, it is an
	// outage this process built for itself; when nothing is left to prefer, the
	// honest answer is no verdict at all, and the sort word chooses among slow
	// lanes.
	//
	// BUT A LANE THAT REFUSED IS NOT A SLOW LANE, and this is where the measured
	// failure of SWE-Marathon run s2 lived. One endpoint answered 400 "Provider
	// returned error"; it was the only lane the ledger held for that model; the
	// rule above then dropped the verdict entirely, so the next request went
	// straight back to the endpoint that had just refused, three times, fifteen
	// seconds apart, and the turn died with five hours of the ask unspent.
	// Sending to a lane already known to refuse is not a fallback, it is the same
	// failure again — and the case this rule was written for is answered a rung
	// higher anyway: a request that ignored everybody comes back "no endpoints
	// found", and the refusal ladder re-sends it with the ignores taken off
	// ([relaxedPreferences], endpoints.go).
	if len(healthy) == 0 && len(demoted) == 0 && !refusedSomething {
		return nil, nil
	}
	if len(healthy) == 0 {
		return nil, ignore
	}
	return append(healthy, demoted...), ignore
}

// pace is the ledger taking a provider at its word: a 429 that NAMES the
// endpoint it came from is that endpoint saying "not now", which is better
// evidence than any number of timed answers. The lane is refused outright for
// the given wait so the next encoded request routes around it instead of
// queueing behind a pool somebody else is saturating.
//
// The strikes are set rather than incremented, so recovery is the one already
// written: the cooldown expires, the lane comes back on probation, and its
// first fast answer walks it out (observe). A wait the provider did not name,
// or named absurdly, is clamped to the same cooldown a laggy lane serves —
// pacing is a claim about the next minutes, never about the day.
//
// It reports whether a lane was actually refused. The attribution law leaves an
// unnamed endpoint alone, and "nothing was struck" is a fact a caller acts on
// (noteCutProvider), not a silence to infer from.
func (l *velocityLedger) pace(model, served string, wait time.Duration) bool {
	if l == nil {
		return false
	}
	key := normalizeModel(model)
	served = strings.TrimSpace(served)
	if key == "" || served == "" {
		return false
	}
	if wait <= 0 || wait > ignoreCooldown {
		wait = ignoreCooldown
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	lanes := l.lanes[key]
	if lanes == nil {
		lanes = map[string]*lane{}
		l.lanes[key] = lanes
	}
	entry := lanes[served]
	if entry == nil {
		entry = &lane{provider: served, seen: len(lanes)}
		lanes[served] = entry
	}
	entry.strikes = ignoreAfter
	entry.ignoredUntil = l.now().Add(wait)
	// AND IT IS MARKED AS A LANE THAT DID NOT SERVE, which is what every caller
	// of pace has in common: a 429 naming its pool, a stream that went quiet, an
	// upstream that answered 4xx. The distinction is read in [preferences].
	entry.refused = true
	return true
}
