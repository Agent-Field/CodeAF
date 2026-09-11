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

// accountExcluded is the router reporting that the ACCOUNT'S OWN SETTINGS
// removed every machine the request's set held — the paid-model-training switch,
// an account-wide guardrail — before any of them was asked. It is the same class
// of fact as [ignoredEverything] (a list emptied the set; no machine answered)
// and it is one the router states on every model, which is why it is kept for
// every model (internal/lane's account.go).
//
// IT IS DECIDED BY STRUCTURE FIRST (docs/design/failsafe/FAILSAFE.md rule 1).
// The live router's body carries `error.metadata.ineligibility_reasons`, one
// object per reason a machine was removed, and a reason that is the account's
// is one the person can CHANGE — it carries the `configure_url` of the setting
// that did it, and its machine word ends `-by-account`. A body whose every reason
// is such a reason is an account exclusion whatever its sentence says. The
// sentence's `(account settings)` survives as the hint for a body that arrives
// without metadata, and has no other authority.
func accountExcluded(body []byte) bool {
	var decoded struct {
		Error struct {
			Message  string `json:"message"`
			Metadata struct {
				Reasons []struct {
					Reason       string `json:"reason"`
					ConfigureURL string `json:"configure_url"`
				} `json:"ineligibility_reasons"`
			} `json:"metadata"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &decoded) != nil {
		return false
	}
	if reasons := decoded.Error.Metadata.Reasons; len(reasons) > 0 {
		for _, reason := range reasons {
			configurable := strings.TrimSpace(reason.ConfigureURL) != ""
			byAccount := strings.HasSuffix(strings.ToLower(strings.TrimSpace(reason.Reason)), "-by-account")
			if !configurable && !byAccount {
				return false
			}
		}
		return true
	}
	return strings.Contains(strings.ToLower(decoded.Error.Message), "(account settings)")
}

// listEmptied is the one question both of the above answer: did a LIST — this
// process's vetoes, the account's ignored providers, the account's own
// guardrails — empty the set before any machine was asked? A refusal that says
// so implicates no machine it names, and the ledger reads it as
// [laneRefusal.Unasked].
func listEmptied(body []byte) bool {
	return ignoredEverything(body) || accountExcluded(body)
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

// withdrawnModel reports that THE ROUTER NO LONGER CARRIES THIS MODEL AT ALL.
//
// IT IS [Client.routingRefusal]'S STRUCTURAL CLAUSE WITH ITS LAST QUESTION
// INVERTED, and that is the whole of the difference between the two facts. Both
// are the router answering for itself about a request no machine was asked
// about; the routing refusal is an emptied SET, which another machine or another
// shape can fill, and this is an absent MODEL, which nothing but another model
// can answer. The router spells them with the same 404 and, very often, with the
// same `No endpoints found` sentence — so read as one they cost a person the
// whole transport budget for three more identical refusals before the hop that
// was the only move all along (#838).
//
// The sheet clause is here for [Client.routingRefusal]'s reason word for word: a
// plain OpenAI-compatible endpoint has no SET behind a model and no catalog row
// in this build, so its first 404 must not be read as a model going away.
//
// ── IT DEMANDS POSITIVE EVIDENCE, WHICH IS THE OPPOSITE OF ITS NEIGHBOUR ────
//
// [Client.catalogKnowsModel] answers false for two different things: a catalog
// that has no row for this model, and a build with no catalog at all. For
// [Client.routingRefusal] those are safely the same — it uses the answer to
// WITHHOLD a ladder, so knowing nothing declines — and here they are opposites,
// because this uses the answer to declare a model gone. So a client with no
// price resolver answers false: it has no opinion, and a fact nobody can testify
// to is not a fact.
//
// AND A LIST THAT EMPTIED THE SET IS NOT A MODEL GOING AWAY. The two arrive
// under the same 404 from the same envelope; the list has machines behind it and
// this has none, so `listEmptied` is asked first and its answer wins
// (refusalobject.go reads the two as one object).
func (c *Client) withdrawnModel(model string, status int, payload []byte) bool {
	// A 404 AND NOTHING ELSE, which is the one place this parts company with
	// [endpointRefusalStatus]. That helper also takes a 400 because a router
	// really does refuse an unservable PARAMETER with one — and a 400 is the
	// router saying something about the request it was handed, never that the
	// model has gone. Reading one as a withdrawal took a model away for the rest
	// of the conversation on the strength of a single bad body.
	if status != http.StatusNotFound || !c.baseServesLanes() || c.config.ModelPrice == nil {
		return false
	}
	if upstream, ok := routerErrorEnvelope(payload); !ok || upstream != "" {
		return false
	}
	if listEmptied(payload) {
		return false
	}
	return !c.catalogKnowsModel(model)
}

// overflowCode is the error envelope's own word for a request that did not fit.
// It is the OpenAI-compatible spelling, which the router and every gateway in
// front of one relay verbatim, and it is a FIELD rather than a sentence — which
// is the entire reason this is decided here instead of by the regex
// internal/session's turn loop used to run over the prose.
const overflowCode = "context_length_exceeded"

// overflowPhrases is what a body that carries no code and no status says
// instead. It is a HINT and never the gate, in [endpointRefusalPhrases]'s sense
// and for docs/design/failsafe/FAILSAFE.md rule 1's reason: the phrases are
// evidence that something structural is true, and the day a provider rewords one
// the code and the status still answer.
//
// They were the WHOLE test until 2026-09-10, in internal/session, read off the
// error's sentence after a verdict had already been computed — so a router 404
// whose words happened to miss every pattern returned from the turn loop as
// though nothing could be done about it. Here they are the last question rather
// than the first, and nothing reads them twice.
var overflowPhrases = []string{
	"context length",
	"context window",
	"maximum context",
	"token limit",
	"context limit",
	"prompt is too long",
	"too many tokens",
}

// overflowRefusal reports that a refusal is the request not fitting: the
// request-too-large status, the envelope's own code, or — failing both — the
// provider saying so in words.
func overflowRefusal(status int, code string, said ...string) bool {
	if status == http.StatusRequestEntityTooLarge {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(code), overflowCode) {
		return true
	}
	for _, sentence := range said {
		sentence = strings.ToLower(sentence)
		if sentence == "" {
			continue
		}
		for _, phrase := range overflowPhrases {
			if strings.Contains(sentence, phrase) {
				return true
			}
		}
	}
	return false
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
	prefs := c.wirePreferences(model, knobs)
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

// ── THE ADAPTER NEVER CHANGES THE MODEL ─────────────────────────────────────
//
// It used to. This ladder walked [Client.fallbackChain] after its relaxation
// rungs, and internal/session's turn loop walks the SAME chain when a model's
// transport budget is spent — two mechanisms drawing from one list, neither
// knowing the other had already tried a model. A turn could pay for the same
// fallback twice, and the session's `nextFallback` indexed the list blind: it
// counted its OWN hops and read `options[len(hopped)]`, so a chain the adapter
// had already walked was re-walked from the top.
//
// ONE MODEL HOP, AND IT BELONGS TO THE SESSION (docs/design/recovery §4, "what
// the session keeps"). The adapter owns the request's SHAPE — which machine,
// which knobs, which ceiling — and hands back a refusal naming what it tried; the
// layer that owns the turn decides whether a different model is worth asking,
// because it is the only layer that knows what the turn has already spent and
// what the person asked for. What the adapter still owes is the FACT: every
// model it put on the wire is recorded on the call's own context
// ([ModelsTried]), so the session's hop reads what was tried instead of counting.
//
// The relaxation rungs stay exactly where they were. They are about the request
// and nobody above this layer can compose one.

// THE LADDER ITSELF IS THE DISPATCHER'S (dispatch.go's
// [Client.recoverFromRefusal]). What stays here is the DATA it climbs — which
// rungs this request has, in which order, spelled how — because that is a fact
// about the request's shape and the encoder is the only layer that knows it. The
// walk moved because a second loop counting its own rungs is a second budget,
// which is the shape docs/design/recovery/DESIGN.md §4 deleted: the rungs reach
// [control.Next] as [control.Plan.Shapes] and come back one at a time.

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
		[]string{rung(relaxEndpointFilter).name}, 2, payload)
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

// ── the second door that was: patience spent on pacing ──────────────────────
//
// A 429 that never clears is the other way a model runs out of ability to
// answer, and the answer to it is the same one: ask a different model. The
// retry loop's patience is the whole of what the call waits for — six attempts
// and two minutes for a watched call, sixty and ten minutes for a task node's
// (retry.go's outOfPatience) — and when that is spent the call has a choice
// between an error and another model.
//
// THAT CHOICE IS NOT THIS LAYER'S TO MAKE AND NO LONGER IS. `recoverFromPacing`
// walked [Client.fallbackChain] here, silently, on a budget nobody above could
// see, while internal/session's turn loop walked the same list for the same
// reason — the second of the two model-hop mechanisms docs/design/recovery §2.2
// counts. The paced error now travels back whole: the boundary reads it as the
// wire ([taxonomy.Transport]), the turn spends its own budget on it, and if that
// budget runs out the ONE model hop in this build takes it to the next model,
// knowing what has already been tried ([ModelsTried]).

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
// AND A MODEL THE ROUTER HAS PUT DOWN IS NOT OFFERED. The chain is where a hop
// picks its target, so a model already known to have no endpoints must not be on
// it — otherwise the hop lands on a second 404 and the turn pays twice for one
// fact (withdrawn.go, measured 2026-09-10 22:39). Dropping it HERE rather than at
// the caller is what keeps the order deterministic: two turns of one conversation
// asked the same question and moved to different models, because each rediscovered
// the withdrawal for itself.
func (c *Client) FallbackModels(model string) []string {
	chain := c.fallbackChain(model)
	carried := chain[:0]
	for _, candidate := range chain {
		if c.WithdrawnModel(candidate) {
			continue
		}
		carried = append(carried, candidate)
	}
	return carried
}

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
//
// IT NO LONGER NAMES OTHER MODELS, because this layer no longer tries any. The
// diagnosis is about the request's SHAPE — what it carried and what came off —
// and the model is the turn's to move (internal/session's nextFallback). A third
// branch here used to say "none of the fallbacks could serve it either", which
// was a sentence about a walk this ladder made on a budget nobody above it could
// see; the layer that hops now says what it tried, in its own words, because it
// is the layer that knows (loop.go's alsoTried).
func (e *RefusalError) advice() string {
	if len(e.Stripped) == 0 {
		return "try a different model, or a different base URL if this one is not a router"
	}
	return "try a different model with /model, or set models.fallbacks so this can move on its own"
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
	stripped []string,
	attempts int,
	payload []byte,
) error {
	failure := &RefusalError{
		Model:    model,
		Params:   c.sentParams(request, knobs, model),
		Stripped: stripped,
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
	if prefs := c.wirePreferences(model, knobs); prefs != nil {
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
