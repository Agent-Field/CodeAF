package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE ENDPOINT REFUSAL CHAIN ──────────────────────────────────────────────
//
// "No endpoints found that can handle the requested parameters" is the router
// saying something no other 4xx says: the MODEL is fine, the KEY is fine, the
// conversation is fine — the combination of optional fields on this particular
// request matches none of the endpoints serving it. Nothing about that is a
// mistake a person made, and until this file the only thing they were handed
// was the sentence itself, on a turn that produced no answer.
//
// It is a shape this adapter can very often fix by itself, because it is the
// one that put most of those fields there. Four of the five things that can
// narrow the endpoint set to empty are the adapter's own economies rather than
// anything the caller asked for:
//
//   - `provider.require_parameters: true`, sent on EVERY OpenRouter request
//     (velocity.go), which turns every other field on the body into a hard
//     endpoint filter — including the tool definitions the belt always carries;
//   - `provider.ignore`, the velocity ledger's refusals, which on a model with
//     two endpoints can remove both;
//   - the `reasoning` knob, which a person set once with ctrl+t and which no
//     longer fits the model they have since switched to;
//   - an output cap or a structured-output schema a particular endpoint does
//     not publish.
//
// So the chain below takes them back off, one at a time, cheapest first, and
// SAYS SO EACH TIME. A retry that silently changed the shape of the request
// would be an adapter answering a different question from the one it was asked;
// a person watching "Retry 2/4: removed reasoning" knows exactly what they got
// and exactly what to change to keep it.
//
// WHAT ENTERS THE LADDER. Only this error class does, and only after a watched
// request has no serving, untried lane the purse will fund. A timeout, a 5xx, a
// 429 and a plain 404 from a wrong base URL all keep the behaviour they had
// (retry.go), because none of them is a claim about the request's shape and
// stripping fields off them would spend a person's turn discovering that.
//
// ── HOW THE CLASS IS RECOGNISED: BY STRUCTURE, NEVER BY VOCABULARY ──────────
//
// A list of the sentences a router has been SEEN to refuse in is always one
// sentence behind, and on 2026-08-28 it was. The ladder's price rung IS the
// recovery for a price ceiling that emptied the endpoint set, and it never
// fired for a whole headless run, because the router reports the LAST filter
// that emptied the set — "no endpoints available matching your guardrail
// restrictions and data policy" — rather than the price that did it. Adding
// that sentence to the list bought exactly one more sentence of coverage; the
// next unfamiliar phrasing is the same outage again. Rule 1 of
// docs/design/failsafe/FAILSAFE.md is the general form of that lesson: a
// fail-safe detects by STRUCTURE or it is decoration.
//
// The structural facts are all available without reading a word of the message:
//
//   - the status is 404 or 400 ([endpointRefusalStatus]);
//   - the body is the ROUTER'S OWN JSON error envelope — an `error` object with
//     a `message` in it ([routerErrorEnvelope]) — which is what a wrong base URL
//     CANNOT produce: a proxy, a static host, a mistyped path and a plain nginx
//     all answer in HTML or bare text, and none of them has an envelope to
//     answer in;
//   - the request went to a base that has SHOWN it serves several endpoints
//     behind a model ([Client.baseServesLanes] — the endpoints page it
//     answered, never its hostname), because this
//     whole ladder is about which of several endpoints may serve one model, and
//     a single endpoint has no endpoint set that can be emptied;
//   - the model is one the CATALOG KNOWS ([Client.catalogKnowsModel]), which is
//     what separates "nothing can serve this shape" from "there is no such
//     model". The second is a fact the caller has to be SHOWN, and negotiating
//     with it would spend four attempts discovering a typo;
//   - and the router refused on its OWN account rather than relaying somebody
//     else's ([APIError.FromUpstream] reads the same field). A 400 forwarded
//     from the endpoint the router chose is ONE endpoint's verdict on the
//     request — the routing layer found something to try, which is the opposite
//     of this class, and refusal_test.go's rotation is the answer to it.
//
// Under all five, a 404 cannot be a wrong base URL and cannot be an unknown
// model. What is left is "nothing I can reach will serve this shape", and the
// ladder is the right answer to it whatever sentence it arrived in.
//
// [endpointRefusalPhrases] survives as a HINT with one job and no authority:
// it SHORT-CIRCUITS the classification when it matches, so every refusal the
// old gate caught is still caught — including on endpoints where the structural
// facts cannot be established at all.

// endpointRefusalStatus is the status half of the gate. 404 is the router's own
// spelling of "nothing can serve this"; 400 is what several OpenAI-compatible
// gateways answer with instead. It is necessary and never sufficient — the rest
// of what makes a refusal this class is in [Client.routingRefusal].
func endpointRefusalStatus(status int) bool {
	return status == http.StatusNotFound || status == http.StatusBadRequest
}

// endpointRefusalPhrases is the vocabulary a router has been SEEN to refuse a
// parameter combination in. It is a HINT and no longer the gate — the header
// above says what replaced it and what its remaining job is. Rule 1 of
// docs/design/failsafe/FAILSAFE.md is why it is a hint: a phrase list is
// evidence, never the classification.
var endpointRefusalPhrases = []string{
	"no endpoints found",
	"no endpoints that support",
	// The router's spelling for an `ignore` list that removed every endpoint —
	// which this process's own velocity ledger can produce on a model with one
	// provider. The first rung of the ladder drops that list, which is exactly
	// the recovery; this phrase missing from the list is how a whole task wave
	// once died on instant 404s the ladder was built to absorb.
	"all providers have been ignored",
	"can handle the requested parameters",
	"handle requested parameters",
	"no allowed providers",
	"unsupported parameter",
	"unsupported parameters",
	"does not support tools",
	"doesn't support tools",
	"does not support tool use",
	"doesn't support tool use",
	"does not support structured",
	"doesn't support structured",
	// THE ROUTER'S OTHER SPELLING OF AN EMPTY SET, and the one that cost a whole
	// headless run three identical retries and a dead task (2026-08-28). The
	// price ceiling (velocity.go's [Client.priceCeiling]) is list price times
	// 1.25, and for deepseek-v4-pro that admits exactly ONE endpoint — the
	// first-party one at list price; every reseller is 1.7× to 2.2× above it.
	// When the account's privacy setting excludes that one endpoint, the ceiling
	// leaves nothing, and the router does not say "no endpoints found that
	// satisfy the max price" — it reports the LAST filter that emptied the set,
	// which was the data policy. None of the phrases above matched, so the gate
	// said "not this class", the plain-404 path resent the identical body, and
	// the ladder that drops the ceiling on its price rung never fired. The proof
	// was a bisect against the live router with the captured body: every field
	// passed alone, and max_price at list × 1.0 produced this exact sentence.
	//
	// THE FOUR PHRASES BELOW ARE NOW HISTORY RATHER THAN LOAD-BEARING. The refusal
	// they describe is caught by [Client.routingRefusal] on its structure, and
	// would be caught if the router reworded it tomorrow. They remain because
	// hints preserve the older gate on bases whose endpoint sheet is unavailable.
	"no endpoints available",
	"data policy",
	"guardrail restrictions",
	"satisfy the max price",
}

// endpointRefusalPhrase reports whether a refusal is one already KNOWN to be
// this class by its words. Ordinary language, ordinary answer: when the router
// says one of these, nothing further has to be established.
// ignoredEverything is the router reporting that an IGNORE LIST removed every
// endpoint before it asked any of them — this process's own list, or the
// account's standing one, which is why the sentence ends by naming the setting.
//
// IT IS A FACT ABOUT A LIST AND NEVER ABOUT A MACHINE, which is the whole
// reason it is asked separately from the phrase table above: the table says
// "this class of refusal", and the strike path needs to know something else,
// that there is no lane here to blame.
func ignoredEverything(body []byte) bool {
	return strings.Contains(strings.ToLower(string(body)), "all providers have been ignored")
}

func endpointRefusalPhrase(payload []byte) bool {
	text := strings.ToLower(string(payload))
	for _, phrase := range endpointRefusalPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// routerErrorEnvelope reports whether a payload is the ROUTER'S OWN JSON error
// object, and names the upstream when the router was relaying somebody else's
// refusal rather than answering for itself.
//
// It decodes the same [errorBody] every refusal in this package is decoded
// through, so this process has ONE answer to "what shape does a router error
// arrive in" rather than two that can drift apart.
//
// A non-empty `error.message` is the whole test, and `code` is deliberately NOT
// required. The router types that field as a number, as a string, and sometimes
// omits it — which is exactly why [errorBody] takes it as raw JSON — and
// demanding a field spelled three ways would be the vocabulary mistake again in
// a different place. What the envelope proves is the only thing this gate needs
// from it: SOMETHING THAT SPEAKS THE ROUTER'S DIALECT ANSWERED. A wrong base URL
// does not.
func routerErrorEnvelope(payload []byte) (upstream string, ok bool) {
	var decoded errorBody
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", false
	}
	if strings.TrimSpace(decoded.Error.Message) == "" {
		return "", false
	}
	return strings.TrimSpace(decoded.Error.Metadata.ProviderName), true
}

// catalogKnowsModel reports whether the model in hand is one this process has a
// catalog row for. It is the clause that separates "nothing can serve this
// shape" from "there is no such model".
//
// THE CATALOG IS ASKED THROUGH ITS PRICE, because a published list price is the
// only membership question [Config] exposes: ModelPrice answers known=false for
// a slug the catalog has never resolved, and a published zero — the free
// variants a router carries — is a real figure that still answers known. There
// is no Knows(model) resolver to call, and adding one would be a second answer
// to a question this one already answers on every request that carries a
// ceiling (velocity.go's priceCeiling).
//
// A BUILD WIRED WITH NO ModelPrice AT ALL KNOWS NOTHING, and that is the safe
// reading rather than a gap: with no way to tell a real model from a typo, the
// refusal is surfaced as the error it is and the ladder is not entered. Every
// door that reaches a person wires it (cmd/aforge, internal/config,
// internal/session), so the class is live where the outage happened.
func (c *Client) catalogKnowsModel(model string) bool {
	if c.config.ModelPrice == nil {
		return false
	}
	_, _, known := c.config.ModelPrice(normalizeModel(model))
	return known
}

// routingRefusal is the gate on the ladder, and the whole of the answer to
// "is this the router saying nothing it can reach will serve this shape?".
// The header above states the five facts and why each one is needed.
func (c *Client) routingRefusal(model string, status int, payload []byte) bool {
	if !endpointRefusalStatus(status) {
		return false
	}
	// THE HINT IS ASKED FIRST, so that nothing the old gate caught can be lost
	// by this one being stricter. A plain OpenAI-compatible endpoint saying
	// "unsupported parameter" is not a router and has no catalog row in this
	// build, and it climbed this ladder before the structural clauses existed.
	if endpointRefusalPhrase(payload) {
		return true
	}
	// A SHEET SITE (#433), and it is the sheet's answer rather than the
	// preference one on purpose. What this clause asks is "does this base have
	// a SET of endpoints behind a model that could be emptied" — a single
	// endpoint has none — and the base that has shown one is exactly the base
	// that served an endpoints page. Keyed on the preference answer instead it
	// would read a plain endpoint's very first 404 as a routing layer with
	// nothing left to try and strip four fields off the person's request to
	// find out otherwise.
	if !c.baseServesLanes() {
		return false
	}
	upstream, ok := routerErrorEnvelope(payload)
	if !ok || upstream != "" {
		return false
	}
	return c.catalogKnowsModel(model)
}

// ── what a retry may take off ───────────────────────────────────────────────

// relaxSet is what one encode has been told to leave out. It rides on callKnobs
// so the ENCODER stays the only place that decides a request's shape: a chain
// that assembled its own bodies would be a second wire format, drifting.
type relaxSet uint8

const (
	// relaxEndpointFilter drops every membership restriction — the hard
	// parameter filter, this process's own refusals, and the demand for one
	// machine that a pin or a rescue put there. FIRST because it changes neither
	// what the model is asked nor the most aforge will pay: it widens which
	// endpoints may answer under the same ceiling.
	//
	// `provider.only` was not on this rung for a long time, and that is half of
	// issue #266: a pinned request climbed every rung there is — reasoning, the
	// output cap, structured output, its attachments, finally its tools — still
	// pinned to the one machine that had refused it, so every rung was spent on
	// a request that could not have been served whatever shape it was in.
	relaxEndpointFilter relaxSet = 1 << iota
	// relaxPriceCeiling drops max_price only after the wider endpoint set has
	// refused the request too. Availability still wins, but an unrelated pin,
	// ignore list, or require_parameters refusal cannot silently authorize a
	// dearer endpoint.
	relaxPriceCeiling
	// relaxReasoning drops the `reasoning` knob. A knob, not content: the model
	// answers the same question, with its own default amount of thinking.
	relaxReasoning
	// relaxMaxTokens drops the output cap.
	relaxMaxTokens
	// relaxResponseFormat drops structured output.
	relaxResponseFormat
	// relaxImages replaces every non-text content part with a note saying it was
	// dropped, so the model is told a picture existed rather than silently
	// answering a question about nothing.
	relaxImages
	// relaxTools drops the tool definitions, and it is LAST on purpose: a turn
	// without tools is a turn that cannot read a file or run a command, which is
	// most of what this surface is for. It is the difference between a degraded
	// answer and no answer, and it is only ever reached when every cheaper rung
	// has already been refused.
	relaxTools
)

func (r relaxSet) has(bit relaxSet) bool { return r&bit != 0 }

// relaxStep is one rung: the field it takes off, and the words a person reads
// when it does.
type relaxStep struct {
	bit relaxSet
	// label is the retry line's second half — "removed max_tokens".
	label string
	// name is the same fact in the terminal error's list of what was stripped.
	name string
}

// relaxRungs is every rung there is, in the order they are climbed, and it is
// the ONE place each one's words are written. Two readers spell a rung: the
// retry line a person watches while the ladder is being climbed, and the
// model-call log's account of a body that has already climbed it
// (calllog.go's relaxNames). A rung named separately in each would have been
// two names for one thing the first time either was reworded.
var relaxRungs = []relaxStep{
	{bit: relaxEndpointFilter, label: "relaxed the endpoint filter", name: "provider.require_parameters"},
	{bit: relaxPriceCeiling, label: "dropped the price ceiling", name: "provider.max_price"},
	{bit: relaxReasoning, label: "removed reasoning", name: "reasoning"},
	{bit: relaxMaxTokens, label: "removed max_tokens", name: "max_tokens"},
	{bit: relaxResponseFormat, label: "removed response_format", name: "response_format"},
	{bit: relaxImages, label: "removed images", name: "images"},
	{bit: relaxTools, label: "removed tools", name: "tools"},
}

// rung is one row of the table above. A bit with no row is a programming error
// rather than a runtime one, and it returns a step that names nothing.
func rung(bit relaxSet) relaxStep {
	for _, step := range relaxRungs {
		if step.bit == bit {
			return step
		}
	}
	return relaxStep{bit: bit}
}

// relaxationPlan is the ladder for ONE request: only the rungs that would
// actually change this body, in the order they are climbed.
//
// A rung for a field the request never carried is not a retry, it is the same
// request sent twice — so an unset max_tokens produces no "removed max_tokens"
// line and does not inflate the attempt counter the person is reading.
func (c *Client) relaxationPlan(request *ai.Request, knobs callKnobs, model string) []relaxStep {
	var plan []relaxStep
	// THE RUNG IS OFFERED FOR WHAT IS ACTUALLY ON THE WIRE, which is the
	// preference object the encoder builds and not the one half of it: a rescue
	// demands its lane through [hedgePreference] AFTER the ledger's own
	// preferences are assembled, so a plan built from the ledger's half alone
	// could not see the narrowest filter this process sends. A pinned request
	// therefore had no first rung at all and climbed straight to "removed
	// reasoning", still pinned to the machine that had refused it (issue #266).
	prefs := c.wirePreferences(model, knobs, request)
	if prefs.membershipNarrowing() {
		plan = append(plan, rung(relaxEndpointFilter))
	}
	if prefs != nil && prefs.MaxPrice != nil {
		plan = append(plan, rung(relaxPriceCeiling))
	}
	if c.resolveEffort(model, knobs.effort) != EffortNone {
		plan = append(plan, rung(relaxReasoning))
	}
	if request.MaxTokens != nil {
		plan = append(plan, rung(relaxMaxTokens))
	}
	if request.ResponseFormat != nil {
		plan = append(plan, rung(relaxResponseFormat))
	}
	if carriesAttachments(request.Messages) {
		plan = append(plan, rung(relaxImages))
	}
	if len(request.Tools) > 0 {
		plan = append(plan, rung(relaxTools))
	}
	return plan
}

// carriesAttachments reports whether any message holds something that is not
// text. It reads the same four fields [dropAttachments] rewrites, so the rung is
// offered exactly when it would do something.
func carriesAttachments(messages []ai.Message) bool {
	for _, message := range messages {
		for _, part := range message.Content {
			if isAttachment(part) {
				return true
			}
		}
	}
	return false
}

func isAttachment(part ai.ContentPart) bool {
	return part.ImageURL != nil || part.VideoURL != nil || part.InputAudio != nil || part.InputFile != nil
}

// attachmentNote is what a dropped picture leaves behind. It is a sentence and
// not a deletion because the message around it usually refers to the thing —
// "what is wrong with this screenshot" answered against no screenshot is a
// confident answer about nothing, which is worse than a refusal.
const attachmentNote = "[an attachment was removed: no endpoint serving this model could accept it]"

// dropAttachments rewrites messages so nothing but text travels. It is a pure
// function and returns the input untouched when there was nothing to drop, so a
// text-only conversation keeps producing byte-identical requests.
func dropAttachments(messages []ai.Message) []ai.Message {
	if !carriesAttachments(messages) {
		return messages
	}
	rewritten := make([]ai.Message, len(messages))
	for index, message := range messages {
		rewritten[index] = message
		kept := make([]ai.ContentPart, 0, len(message.Content))
		dropped := false
		for _, part := range message.Content {
			if isAttachment(part) {
				dropped = true
				continue
			}
			kept = append(kept, part)
		}
		if !dropped {
			continue
		}
		kept = append(kept, ai.ContentPart{Type: "text", Text: attachmentNote})
		rewritten[index].Content = kept
	}
	return rewritten
}

// ── the chain ───────────────────────────────────────────────────────────────

// maxFallbackModels bounds how many other models one refusal may try. TWO,
// because a person is waiting: past that the honest move is to say what is wrong
// and let them pick, rather than to walk a list on their behalf.
const maxFallbackModels = 2

// recoverFromRefusal climbs the ladder and then the fallback chain, narrating
// each attempt, and ends in an error a person can act on.
//
// The first attempt has already happened and been refused — its body is `first`
// — so every attempt this function makes is numbered from one as a RETRY, which
// is what the line says.
func (c *Client) recoverFromRefusal(
	ctx context.Context,
	request *ai.Request,
	knobs callKnobs,
	stream bool,
	first []byte,
) (*http.Response, error) {
	model := c.modelFor(request)
	plan := c.relaxationPlan(request, knobs, model)
	fallbacks := c.fallbackChain(model)
	total := len(plan) + len(fallbacks)
	if total == 0 {
		return nil, c.refusalError(request, knobs, model, nil, nil, 1, first)
	}

	last := first
	stripped := make([]string, 0, len(plan))
	tried := make([]string, 0, len(fallbacks))
	attempt := 0

	// The relaxations ACCUMULATE. Each rung is climbed on top of the last,
	// because the refusal never says which field it objected to — a body that
	// still carries the reasoning knob has not tested whether dropping the
	// output cap was enough.
	relaxed := knobs
	for _, step := range plan {
		attempt++
		// THE MEMO FOLLOWS THE SECOND REFUSAL. Reaching this rung means the
		// endpoint-membership retry, when there was one, was refused under the
		// original price ceiling too. That is evidence the ceiling must come off;
		// the first refusal alone could have been caused by any membership field.
		if step.bit == relaxPriceCeiling && c.velocity != nil {
			c.velocity.refuseCeiling(model)
		}
		relaxed.relaxed |= step.bit
		stripped = append(stripped, step.name)
		Emit(ctx, StreamNotice, fmt.Sprintf("Retry %d/%d: %s", attempt, total, step.label))
		// AND THE PHASE CLOCK CARRIES THE SAME RUNG, so the status line says
		// "trying again · 2 of 6" while the notice above says which knob went.
		// It is the same fact at two grains and it is stated once, here, from
		// the same pair of numbers (phase.go).
		notePhase(ctx, c.modelFor(request), PhaseRetrying,
			ordinalOf(attempt, total), c.clock(), time.Time{}, "")
		response, payload, err := c.attemptShaped(ctx, request, relaxed, stream)
		if err != nil {
			return nil, err
		}
		if response != nil {
			return response, nil
		}
		last = payload
	}

	for _, next := range fallbacks {
		attempt++
		tried = append(tried, next)
		Emit(ctx, StreamNotice, fmt.Sprintf("Retry %d/%d: Falling back to %s", attempt, total, next))
		// AND THIS IS THE ONE RUNG THAT CHANGES THE ANSWER'S MODEL, so it is
		// the one rung whose phase says so by name: a person who asked one model
		// and is being answered by another is owed that sentence while it
		// happens rather than in the transcript afterwards (the ladder, in
		// docs/ARCHITECTURE.md).
		notePhase(ctx, c.modelFor(request), PhaseSwitchingModel,
			"", c.clock(), time.Time{}, next)
		// A NEW MODEL IS TRIED AS CONFIGURED. The strips above were evidence
		// about the endpoints serving the old model and say nothing about these
		// ones; carrying them over would silently answer on a fallback model with
		// no tools because a different model's endpoints had no room for them.
		candidate := *request
		candidate.Model = next
		response, payload, err := c.attemptShaped(ctx, &candidate, knobs, stream)
		if err != nil {
			return nil, err
		}
		if response != nil {
			// The caller's request now names the model that actually answered, so
			// the streamed response, the velocity ledger and the reply's
			// attribution all agree about which one it was.
			request.Model = next
			return response, nil
		}
		last = payload
	}
	return nil, c.refusalError(request, knobs, model, stripped, tried, attempt+1, last)
}

// widenPastTheUncarriedPreference is the ONE retry that finds out whether a
// base's 400 was about the `provider` field at all — and it is the ANSWER to
// that question rather than a recovery from it.
//
// THE RETRY IS THE TEST, AND IT IS THE TEST BECAUSE NO READING OF THE WORDS IS
// ONE. `Unrecognized request argument supplied: provider` and `invalid provider
// name` are both 400s that name the field and they mean opposite things, so a
// phrase list here would mark a base as refusing preferences for good over one
// bad lane name (prefcarry.go's [prefRefused] states the whole of that
// argument). Sending the identical request with the object taken off settles it
// structurally: if that lands, the object was the difference; if it fails too,
// the object was not.
//
// AND A FAILURE TEACHES NOTHING, WHICH IS THE HALF THAT MATTERS. The base's
// answer stays unasked, so the next request carries the preference again and the
// person is told nothing — because nothing was established. What they get back
// is the second refusal, whole, which is the honest thing to hand a caller whose
// request could not be served either way.
//
// THE OBJECT COMES OFF ENTIRELY and not by the ladder's first rung: that rung
// takes off what can EXCLUDE an endpoint and leaves the sort word, which would
// ask the same question again ([callKnobs.noProvider]).
func (c *Client) widenPastTheUncarriedPreference(
	ctx context.Context,
	request *ai.Request,
	knobs callKnobs,
	stream bool,
	began time.Time,
	status int,
	first []byte,
) (*http.Response, error) {
	// THE REFUSED CALL GETS ITS OWN ROW BEFORE THE WIDER ONE GOES OUT, for
	// [Client.widenPastTheRetiredPin]'s reason word for word: a start row left
	// with nothing under it is the one state the model-call log exists to make
	// impossible, and this is the row a person counts to check the 400 was paid
	// once.
	c.record(recordFacts{
		ctx: ctx, request: request, knobs: knobs, stream: stream,
		attempt: c.attemptsSoFar(knobs), began: began,
		status: status, err: apiError(status, first),
		responseBody: first,
	})
	widened := knobs
	widened.noProvider = true
	// IT IS [Client.sendRepaired] AND NOT THE LADDER. The ladder exists to find
	// out WHICH field of a request could not be served; this retry is asking one
	// named question and its answer is yes or no, so climbing on to strip the
	// reasoning knob and the tools would take things the person cares about
	// having sent in order to answer something nobody asked.
	response, err := c.sendRepaired(ctx, request, widened, stream)
	if err != nil || response == nil || response.StatusCode >= 400 {
		return response, err
	}
	// IT LANDED, SO THE FIELD WAS THE DIFFERENCE. The base is filed as one that
	// will not carry a preference, and the person is told once — after which
	// every later request goes out bare rather than paying this pair again.
	c.prefsWereRefused(ctx)
	return response, nil
}

// widenPastTheRetiredPin is the ONE retry a request earns for having just cost
// somebody their preference.
//
// TWO CALLERS AND ONE RULE (client.go's [Client.sendRecovered]). A pin the
// router says it cannot serve for this model is retired (issue #456), and a
// BASE that says it will not carry a `provider` object at all is filed as such
// (issue #433). Both are the same shape of fact learned at the same instant —
// the demand that was on this request will not be on any later one — and both
// owe the request in hand the same answer: send it again, once, without it.
//
// It is rung one of the ladder and nothing else: the whole `provider` object
// comes off ([relaxedPreferences]), so what goes out is the request `auto`
// would have sent, which is precisely what the retirement next door has just
// decided every LATER request will send. This one was already written when the
// decision was taken, and re-sending it is cheaper than the alternative — a
// dead turn, and a person reading the router's own sentence about a preference
// they did not know they had (issue #456).
//
// IT IS ONE ATTEMPT AND NOT A LADDER. The ladder exists to find out WHICH field
// of a request the router could not serve; here that is already known — it was
// the demand, and the demand is gone — so climbing on to strip the reasoning
// knob, the output cap and the tools would take away things the person may care
// about having sent in order to answer a question nobody is asking. If the
// widened request is refused as well, the turn ends with the refusal named,
// exactly as it does at the top of any other ladder that runs out.
func (c *Client) widenPastTheRetiredPin(
	ctx context.Context,
	request *ai.Request,
	knobs callKnobs,
	stream bool,
	began time.Time,
	status int,
	first []byte,
) (*http.Response, error) {
	// THE REFUSED CALL GETS ITS OWN ROW BEFORE THE WIDER ONE GOES OUT, exactly
	// as a repaired 400 does (client.go's [Client.sendRepaired]). "The call
	// that went out first was refused and the one that came back was a
	// different request" is precisely the fact a log holding only the answer
	// cannot tell anybody — and a start row left with nothing under it is the
	// one state this log exists to make impossible. It is also the row a person
	// reading the ledger counts to check that the 404 was paid ONCE.
	c.record(recordFacts{
		ctx: ctx, request: request, knobs: knobs, stream: stream,
		attempt: c.attemptsSoFar(knobs), began: began,
		status: status, err: apiError(status, first),
		responseBody: first,
	})
	widened := knobs
	widened.relaxed |= relaxEndpointFilter
	response, payload, err := c.attemptShaped(ctx, request, widened, stream)
	if err != nil {
		return nil, err
	}
	if response != nil {
		return response, nil
	}
	if len(payload) == 0 {
		payload = first
	}
	return nil, c.refusalError(request, knobs, c.modelFor(request),
		[]string{rung(relaxEndpointFilter).name}, nil, 2, payload)
}

// attemptShaped sends one shaped attempt and separates the two outcomes the
// chain cares about: something to hand back (an answer, or any failure that is
// not this class), and another refusal of the same kind, whose body it returns
// so the terminal error can quote the provider's last words.
func (c *Client) attemptShaped(
	ctx context.Context,
	request *ai.Request,
	knobs callKnobs,
	stream bool,
) (*http.Response, []byte, error) {
	body, err := c.encodeRequest(request, knobs)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}
	response, err := c.send(ctx, request, knobs, body, stream)
	if err != nil {
		return nil, nil, err
	}
	if !endpointRefusalStatus(response.StatusCode) {
		return response, nil, nil
	}
	peek, readErr := io.ReadAll(io.LimitReader(response.Body, maxErrorPeek))
	if readErr != nil || !c.routingRefusal(c.modelFor(request), response.StatusCode, peek) {
		// Not this class after all. The body is handed back whole — the caller
		// still has to read the provider's own words to build its error.
		response.Body = rewound(peek, response.Body)
		return response, nil, nil
	}
	response.Body.Close()
	return nil, peek, nil
}

// ── the second door: patience spent on pacing ───────────────────────────────
//
// A 429 that never clears is the other way a model runs out of ability to
// answer, and the answer to it is the same one: ask a different model. The
// retry loop's patience is the whole of what this waits for — six attempts and
// two minutes for a watched call, sixty and ten minutes for a task node's
// (retry.go's outOfPatience) — and when that is spent the call has today's
// choice between an error and another model. This offers the model.
//
// It is DELIBERATELY the same chain and the same narration as the refusal
// ladder above. Two ways of spelling "the next model" would drift, and a person
// watching a retry line does not care which of the two doors it came through:
// the sentence they need is the same either way.

// pacingExhausted reports whether an error is the retry loop giving up on a
// provider that would not stop pacing us. Only a 429 reaches this shape — every
// other retryable status breaks out on maxAttempts long before patience is a
// question, and a 4xx is never retried at all (retry.go).
func pacingExhausted(err error) bool {
	var api *APIError
	for err != nil {
		if decoded, ok := err.(*APIError); ok {
			api = decoded
			break
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapped.Unwrap()
	}
	return api != nil && api.Status == http.StatusTooManyRequests
}

// recoverFromPacing offers the chain to a call the provider paced into the
// ground, and hands back the original error untouched when there is nothing to
// offer — no chain configured, or a failure that was never about pacing.
//
// A fallback attempt goes through [Client.sendRepaired] rather than back
// through [Client.sendShaped]: the refusal ladder is the FIRST door's business,
// and re-entering it here would let one exhausted 429 walk two more models
// through six relaxations each while a person waits on a turn that has already
// been slow.
func (c *Client) recoverFromPacing(
	ctx context.Context,
	request *ai.Request,
	knobs callKnobs,
	stream bool,
	paced error,
) (*http.Response, error) {
	if !pacingExhausted(paced) {
		return nil, paced
	}
	model := c.modelFor(request)
	fallbacks := c.fallbackChain(model)
	if len(fallbacks) == 0 {
		return nil, paced
	}
	for index, next := range fallbacks {
		Emit(ctx, StreamNotice, fmt.Sprintf("Retry %d/%d: Falling back to %s", index+1, len(fallbacks), next))
		candidate := *request
		candidate.Model = next
		response, err := c.sendRepaired(ctx, &candidate, knobs, stream)
		if err != nil {
			continue
		}
		// The caller's request now names the model that actually answered, for
		// the reason the refusal chain rewrites it: attribution, the ledger and
		// the reply have to agree about which model this was.
		request.Model = next
		return response, nil
	}
	return nil, paced
}

// ── which model to fall back to ─────────────────────────────────────────────

// FallbackModels names the models this client would move to when `model` can no
// longer answer, in order — the SAME chain the two doors above walk, offered to
// a caller that has to make the decision itself.
//
// Its one caller today is internal/session's turn loop, which owns a failure
// this package cannot see: a stream that opened, was accepted, and then went
// quiet often enough to have spent its budget. The precedence and the cap stay
// here, in [Client.fallbackChain], because a second place that decided which
// model comes next would be a second answer to drift from this one.
func (c *Client) FallbackModels(model string) []string { return c.fallbackChain(model) }

// fallbackChain is the models to try after the ladder, in order.
//
// The operator's own list wins outright, and the catalog is only asked when
// there is no list: a person who wrote down what to fall back to has answered
// this question, and a catalog guess arriving after their answer would be this
// adapter overruling them with an inference.
//
// The failing model is never in the chain, and nothing appears twice.
func (c *Client) fallbackChain(model string) []string {
	seen := map[string]bool{normalizeModel(model): true}
	var chain []string
	add := func(candidates []string) {
		for _, candidate := range candidates {
			candidate = strings.TrimSpace(candidate)
			key := normalizeModel(candidate)
			if candidate == "" || seen[key] {
				continue
			}
			seen[key] = true
			chain = append(chain, candidate)
		}
	}
	add(c.config.Fallbacks)
	if len(chain) == 0 && c.config.NearestModels != nil {
		add(c.config.NearestModels(model))
	}
	if len(chain) > maxFallbackModels {
		chain = chain[:maxFallbackModels]
	}
	return chain
}

// ── what a person is told when none of it worked ────────────────────────────

// RefusalError is the end of the chain: every endpoint serving every model tried
// refused this request's shape.
//
// It exists as its own type rather than as another [APIError] because the two
// say different things. An APIError is a refusal, quoted. This is a DIAGNOSIS:
// which model, what the request carried, what was taken off and in what order,
// what else was tried, and the one or two things a person can do about it. A
// turn that ends in "API error (404): No endpoints found that can handle the
// requested parameters" tells somebody watching that something is broken and
// nothing whatever about which knob to turn.
type RefusalError struct {
	// Model is the model the request started on.
	Model string
	// Params is what the first attempt actually carried, in the words the wire
	// spells them.
	Params []string
	// Stripped is what the chain took off, in the order it did.
	Stripped []string
	// Tried is the fallback models attempted after the ladder ran out.
	Tried []string
	// Attempts is how many requests were sent in total, the first one included.
	Attempts int
	// Refusal is the provider's own last words, kept whole so nothing this
	// package summarizes can lose them.
	Refusal *APIError
}

func (e *RefusalError) Error() string {
	if e == nil {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "no endpoint can serve %s — refused after %s", e.Model, countedAttempts(e.Attempts))
	if len(e.Params) > 0 {
		out.WriteString(". sent: " + strings.Join(e.Params, ", "))
	}
	if len(e.Stripped) > 0 {
		out.WriteString("; retried without " + strings.Join(e.Stripped, ", then without "))
	}
	if len(e.Tried) > 0 {
		out.WriteString("; also tried " + strings.Join(e.Tried, ", "))
	}
	if e.Refusal != nil && strings.TrimSpace(e.Refusal.Message) != "" {
		out.WriteString(`. the provider said: "` + strings.TrimSpace(e.Refusal.Message) + `"`)
	}
	out.WriteString(". " + e.advice())
	return out.String()
}

// advice is the actionable half, and it names the specific thing this request
// carried rather than a general suggestion. What a person can do about this is
// always one of two things — send less, or ask somebody else — and the sentence
// says which "less" is available on this particular call.
func (e *RefusalError) advice() string {
	switch {
	case len(e.Stripped) == 0:
		return "try a different model, or a different base URL if this one is not a router"
	case len(e.Tried) > 0:
		return "try a different model with /model — none of the fallbacks could serve it either"
	default:
		return "try a different model with /model, or set models.fallbacks so this can move on its own"
	}
}

// Unwrap keeps the provider's refusal reachable, so a caller that classifies
// errors by status still finds the 404 under the diagnosis.
func (e *RefusalError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Refusal
}

func countedAttempts(n int) string {
	if n == 1 {
		return "1 attempt"
	}
	return fmt.Sprintf("%d attempts", n)
}

// refusalError composes the diagnosis from what the chain knows.
func (c *Client) refusalError(
	request *ai.Request,
	knobs callKnobs,
	model string,
	stripped, tried []string,
	attempts int,
	payload []byte,
) error {
	failure := &RefusalError{
		Model:    model,
		Params:   c.sentParams(request, knobs, model),
		Stripped: stripped,
		Tried:    tried,
		Attempts: attempts,
	}
	if decoded, ok := apiError(http.StatusNotFound, payload).(*APIError); ok {
		failure.Refusal = decoded
	}
	return failure
}

// sentParams names what the first attempt put on the wire, in the spellings the
// endpoint would have filtered on.
//
// It reads the REQUEST's own fields and the two economies this adapter adds on
// top of them, because both halves are equally invisible to the person and only
// one of them is anything they chose. Somebody comparing this line against a
// provider's published parameter list needs the same words on both sides.
func (c *Client) sentParams(request *ai.Request, knobs callKnobs, model string) []string {
	if request == nil {
		return nil
	}
	// NAMES AND NEVER VALUES. The pi-derived retry taxonomies above this adapter
	// (internal/session's loop.go, internal/exec/bare's) classify an error by
	// grepping its TEXT for "429", "500", "503" — so an honest "max_tokens(500)"
	// in this sentence would make a permanent, already-exhausted refusal look
	// transient and buy it three more rounds of backoff. The field names are
	// what a person compares against a provider's parameter list anyway.
	var params []string
	if len(request.Tools) > 0 {
		params = append(params, "tools")
	}
	if request.ToolChoice != nil {
		params = append(params, "tool_choice")
	}
	if request.ResponseFormat != nil {
		params = append(params, "response_format")
	}
	if request.MaxTokens != nil {
		params = append(params, "max_tokens")
	}
	if request.Temperature != nil {
		params = append(params, "temperature")
	}
	if carriesAttachments(request.Messages) {
		params = append(params, "image/file parts")
	}
	if c.resolveEffort(model, knobs.effort) != EffortNone {
		params = append(params, "reasoning")
	}
	// THE PREFERENCE IS READ AS IT WENT OUT, not as the ledger assembled it, so
	// a demand a rescue added afterwards is named here too: `provider.only` is
	// the field most likely to have emptied the endpoint set on a request that
	// reached this sentence, and a list that left it out was describing a
	// different request from the one that failed.
	if prefs := c.wirePreferences(model, knobs, request); prefs != nil {
		if prefs.RequireParameters != nil {
			params = append(params, "provider.require_parameters")
		}
		if len(prefs.Ignore) > 0 {
			params = append(params, "provider.ignore")
		}
		if prefs.MaxPrice != nil {
			params = append(params, "provider.max_price")
		}
		if len(prefs.Only) > 0 {
			params = append(params, "provider.only")
		}
	}
	return params
}
