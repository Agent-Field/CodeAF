package provider

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── WHETHER THIS BASE CARRIES WHAT WE ASKED FOR ─────────────────────────────
//
// `internal/lane` decides which endpoint a request should prefer, lanepin.go
// holds the answer a PERSON gave to the same question, and this file answers
// the one question underneath both: will the machine on the other end carry
// that answer at all.
//
// IT USED TO BE ANSWERED BY A HOSTNAME. [Client.shippedRouterHint] — spelled
// `isOpenRouter` until issue #433 — tests the base URL for `openrouter.ai` and
// the model id for the prefix `openrouter/`, and it decided whether the
// `provider` object went on the wire at all. So on a proxy, a mirror, a
// self-hosted router or the shipped router reached by its IP, a person could
// rank lanes (which #419 made work), pin one, watch the settings row go on
// reading `pinned: X` — and the preference was never sent. Nothing errored,
// nothing logged, and the request that went out was the request `auto` would
// have sent. That is a silent substitution, which is the thing this
// repository's law forbids.
//
// THE LAW, and it is the same law #419 wrote one layer up: whether a base
// honours routing preferences is learned the same way its sheet is — once per
// base, by asking, and remembered, never from the hostname or a model prefix.
// A PREFERENCE THE WIRE WILL NOT CARRY IS SAID, NOT DROPPED: the person is told
// their pin cannot be sent on this base, in one visible line, rather than the
// pin being quietly left off the request.
//
// WHERE THE ANSWER LIVES. Beside the sheet's own answer, in internal/lane
// (sheet.go's [lanes.PrefsCarried]), because it is a fact about a base and the
// sheet is the one thing in this build that already keeps facts per base and
// forgets them when the base moves. This file is only the transport half: what
// on an answer teaches it, and what a person is told when it says no.
//
// ── WHAT TEACHES IT, AND WHAT IT CANNOT TELL APART ──────────────────────────
//
// Three readings, all of them of a request that was going out anyway:
//
//	served a lane sheet   carries — the router's own contract, filed by the
//	                      sheet itself, and the shipped router never pays a
//	                      request for the law
//	named the lane        carries — an answer whose `provider` field names the
//	                      machine that served it is a base speaking the
//	                      router's dialect ([Client.notePrefsFromAnswer])
//	refused naming        will not — a 4xx whose message names the `provider`
//	`provider`            field we put on ([prefRefused]), and the terminal no:
//	                      nothing takes it back while the base stays put
//	200, no lane named    will not — the honest limit, below, and the weaker
//	                      no: a later endpoints page takes it back
//
// AND THE LIMIT, SAID PLAINLY. A base that honoured the preference silently and
// a base that dropped it on the floor look IDENTICAL from here: an
// OpenAI-compatible answer carries nothing that says which machine served it,
// so there is no evidence that separates them. This build takes the reading
// that produces a sentence rather than the one that produces a silence — "no
// lane information" is read as "not carried" — because under the law a person
// who is told their pin is not being sent can act on it, and a person who is
// told nothing cannot. The request itself still goes out either way.

// carriesPreferences is THE DECISION: may a `provider` object go on this
// client's requests.
//
// IT IS NOT [Client.shippedRouterHint] AND MUST NEVER BE CONFUSED FOR IT. The
// hint is a substring of an address; this is what the address ANSWERED. An
// unasked base answers yes, because the asking is the sending and there is no
// other way to learn.
func (c *Client) carriesPreferences() bool {
	return lanes.PrefsCarried(c.config.BaseURL)
}

// baseServesLanes reports whether this client's base has SHOWN it publishes an
// endpoints page — that a model here is served by several machines at all.
//
// IT IS THE OTHER ANSWER AND NOT THIS ONE. [Client.carriesPreferences] is
// "may a `provider` object go out", and it is true until a base refuses,
// because the asking is the sending. This is "does this base have a set of
// endpoints behind a model", and it is false until a base shows one, because
// the machinery it gates — the probe that buys a measurement of two lanes, the
// ladder that reads a 404 as a routing layer with nothing left to try — is
// machinery about a set, and a base with one endpoint has no set. The shipped
// router answers true with no request at all (internal/lane's WireSheet takes
// the hint from [LaneSheetCertain]).
func (c *Client) baseServesLanes() bool {
	return lanes.SheetServes(c.config.BaseURL)
}

// prefWentOut records whether the encode that just happened really put a
// `provider` object on the bytes, which is what makes the answer to it evidence
// about this base at all.
//
// IT IS THE LAST ENCODE AND NOT A LATCH, and the difference is the whole reason
// the widened retry works. That retry carries no preference and is answered —
// often perfectly, naming its lane — by the very base that refused the field a
// moment earlier; read as evidence it would put the base straight back to
// "carries" and the next turn would pay the identical refusal.
//
// IT IS PER CLIENT AND NOT PER REQUEST, which is loose where two calls from one
// client overlap: a hedge's second arm encodes while the first is in flight.
// The looseness is bounded and one-sided — when routing is on every request
// carries a preference, so the two encodes agree; when it is off none do, and
// nothing is ever learnt — and the cost of being wrong is one sentence about a
// base that would have carried it, which the next endpoints page takes back
// (internal/lane's [lanes.HeardPrefsCarried]).
func (c *Client) prefWentOut(carried bool) { c.prefSent.Store(carried) }

// notePrefsFromAnswer is the reading of one completed answer.
//
// IT READS NOTHING OFF A REQUEST THAT ASKED FOR NOTHING. A base that was not
// asked has said nothing about what it would do, whether it named its lane or
// not.
func (c *Client) notePrefsFromAnswer(ctx context.Context, served string) {
	if !c.prefSent.Load() {
		return
	}
	if strings.TrimSpace(served) != "" {
		lanes.HeardPrefsCarried(c.config.BaseURL)
		return
	}
	// SILENCE WHERE A LANE NAME BELONGED. It is the weaker no — see the honest
	// limit at the head of this file — and it owes the person the same sentence,
	// because what it costs them is the same thing.
	if lanes.HeardPrefsSilent(c.config.BaseURL) {
		c.sayThePinIsNotSent(ctx)
	}
}

// sayThePinIsNotSent hands the person the one sentence they are owed for a base
// that will not take their choice.
func (c *Client) sayThePinIsNotSent(ctx context.Context) {
	parkUncarriedPin(c.config.BaseURL)
	tellUncarriedPins(ctx)
}

// prefRefused reads whether a refusal is the base saying it does not know the
// field we put on it, and it is deliberately narrow on three axes at once.
//
// THE STATUS IS 400 AND NEVER 404. A 404 from a router is `No endpoints found…`
// and `…your request's provider.only preference permits only: coreweave` — a
// base that READ the preference and had nothing to serve it with, which is the
// opposite of this class and is the retirement's business next door (lanepin.go,
// issue #456). An unknown request argument is a 400 everywhere it is refused at
// all: OpenAI, Azure, vLLM and llama.cpp all answer one, and the servers that
// do not refuse it simply IGNORE it — which is the silent reading and is caught
// on the answer instead.
//
// THE REFUSAL IS THE BASE'S OWN. A 400 the router RELAYS carries the upstream's
// name in its metadata ([APIError.FromUpstream] reads the same field), and
// `Provider returned error` is a real relayed sentence that names the word and
// means nothing like this.
//
// AND THE MESSAGE HAS TO SAY IT DOES NOT KNOW THE ARGUMENT, not merely mention
// it. The list below is a closed set — the ways an OpenAI-compatible server
// spells "I have never heard of this field" — and it is a HINT with no
// authority, exactly as [endpointRefusalPhrases] is: a base whose refusal this
// does not recognise falls through to the ordinary path and is read on its
// ANSWERS instead, where a field that was ignored rather than refused is caught
// anyway.
func prefRefused(status int, payload []byte) bool {
	if status != http.StatusBadRequest || len(payload) == 0 {
		return false
	}
	// THE SENTENCE IS READ THROUGH [apiError], the same raw-body decoder every
	// refusal on the send path is recovered from, so this file adds no second
	// unmarshal of a shape another one already owns. A body that is not that
	// envelope — a gateway's bare `unknown field "provider"`, a framework's
	// plain text — is read whole, because it is the same answer said
	// differently and there is nothing else to read it out of.
	message := strings.ToLower(string(payload))
	if refused, ok := RefusalFrom(apiError(status, payload)); ok && refused.Message != "" {
		if refused.FromUpstream() {
			return false
		}
		message = strings.ToLower(refused.Message)
	}
	if !strings.Contains(message, "provider") {
		return false
	}
	for _, unknown := range unknownArgumentWords {
		if strings.Contains(message, unknown) {
			return true
		}
	}
	return false
}

// unknownArgumentWords is how an OpenAI-compatible server says it has never
// heard of a field. It is a hint and never a gate ([prefRefused] says why), and
// it is spelled in one place because a second copy is a copy that gets a word
// added to it alone.
var unknownArgumentWords = []string{
	"unrecognized", "unrecognised", "unknown", "unsupported", "unexpected",
	"invalid", "extra ", "additional", "not allowed", "not permitted",
}

// ── THE SENTENCE ────────────────────────────────────────────────────────────

var (
	uncarriedMu sync.Mutex
	// uncarriedTold is one (base, lane) pairing per line ever said, which is
	// what keeps "exactly one visible line" true across a run: a conversation,
	// the errands beside it and every task node it starts are one process, and
	// a fact about a base learned once is a sentence said once.
	uncarriedTold = map[string]struct{}{}
	// uncarriedLines are the sentences a person is owed and has not been told
	// yet, parked for lanepin.go's reason word for word: THE CALL THAT LEARNS
	// THE FACT IS OFTEN NOT A CALL ANYBODY IS READING. The first answer of a
	// run is collected by an errand whose context carries no stream observer at
	// all, so a line posted there goes nowhere and every later request has
	// already learnt the fact and has nothing to say.
	uncarriedLines []string
)

// uncarriedPinLine is the whole sentence, spelled ONCE, here.
//
// It names the two things a person needs to go and look at — the machine they
// pinned and the address this session is talking to — and it says what happened
// to their request rather than what happened to the field: the work went out.
func uncarriedPinLine(lane, base string) string {
	return baseHost(base) + " does not take a lane choice; " + lane +
		" is not being asked for, and your requests still go out"
}

// UncarriedPinLine is that sentence for a surface that has to draw it beside
// its own furniture, exported for [RetiredPinLine]'s reason: a sentence spelled
// in two packages is a sentence that gets reworded in one.
func UncarriedPinLine(lane, base string) string { return uncarriedPinLine(lane, base) }

// baseHost is the part of a base URL a person recognises. A base URL carries no
// secret — the bearer rides a header — but the whole of it is a path and a
// version nobody reads, and the host is the answer to "which machine is this".
func baseHost(base string) string {
	base = strings.TrimSpace(base)
	if parsed, err := url.Parse(base); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return base
}

// parkUncarriedPin queues the sentence for the pin in force, and does nothing
// at all when nobody has pinned anything.
//
// NO PIN, NO SENTENCE. The law is about a person's own answer not reaching the
// wire; somebody who named no machine has had nothing taken from them, and a
// note about a `provider` field they never asked for would be machinery talking.
func parkUncarriedPin(base string) {
	lane := CurrentLanePin().pinned()
	if lane == "" || strings.TrimSpace(base) == "" {
		return
	}
	key := strings.ToLower(strings.TrimSpace(base)) + "\x00" + strings.ToLower(lane)
	uncarriedMu.Lock()
	defer uncarriedMu.Unlock()
	if _, already := uncarriedTold[key]; already {
		return
	}
	uncarriedTold[key] = struct{}{}
	uncarriedLines = append(uncarriedLines, uncarriedPinLine(lane, base))
}

// tellUncarriedPins hands whatever is parked to a call somebody is reading, and
// does nothing at all on one they are not. The two conditions are
// [tellRetiredPins]'s, for its reasons: there has to be a stream to say it on,
// and the errand this call belongs to has to be one a person is watching.
func tellUncarriedPins(ctx context.Context) {
	if ctx == nil || streamObserverFrom(ctx) == nil || !RoleFrom(ctx).Visible() {
		return
	}
	uncarriedMu.Lock()
	owed := uncarriedLines
	uncarriedLines = nil
	uncarriedMu.Unlock()
	for _, line := range owed {
		Emit(ctx, StreamNotice, line)
	}
}

// forgetUncarriedPins empties both halves. It is for tests, which must not
// inherit one another's bases — the shipped path never forgets, because a fact
// about a base is true for as long as the process is talking to it.
func forgetUncarriedPins() {
	uncarriedMu.Lock()
	defer uncarriedMu.Unlock()
	uncarriedTold = map[string]struct{}{}
	uncarriedLines = nil
}

// BaseTakesLaneChoice reports whether the base this process is talking to has
// said it will carry a routing preference.
//
// It is what a SURFACE asks — internal/tui3's lane row, so that `pinned: X`
// never stands on a screen as a claim about a request that did not carry it —
// and it takes no argument because a panel holds no client and so has no base
// URL of its own to name.
func BaseTakesLaneChoice() bool { return lanes.PrefsCarriedHere() }

// prefsJustRefused reads whether THIS refusal is the base rejecting the
// `provider` field, files the answer, and tells the person — answering whether
// the caller should widen and send again.
//
// IT ANSWERS FALSE ON A BASE THAT HAS EVER NAMED A LANE, which is what keeps
// the shipped router's own `provider.only permits only: …` out of this reading
// entirely: that base is already `carries`, and carries outranks drops for the
// life of the wiring. What it also answers false on is a request that carried
// no preference — there is nothing to have been refused — and any status the
// ladder does not treat as a refusal at all.
func (c *Client) prefsJustRefused(ctx context.Context, status int, payload []byte) bool {
	if !c.prefSent.Load() || !c.carriesPreferences() {
		return false
	}
	if !prefRefused(status, payload) {
		return false
	}
	if !lanes.HeardPrefsRefused(c.config.BaseURL) {
		return false
	}
	// AND THE ANSWER IS ONLY ACTED ON IF IT WAS THE ONE THAT LANDED. A base that
	// had already SHOWN it carries a preference keeps that answer — the first
	// definite answer stands (internal/lane's prefAnswer) — so the shipped
	// router's own `permits only:` refusal is filed against nothing, says
	// nothing, and earns no widened retry here: it is the retirement next door's
	// business (lanepin.go, issue #456).
	if c.carriesPreferences() {
		return false
	}
	c.sayThePinIsNotSent(ctx)
	return true
}
