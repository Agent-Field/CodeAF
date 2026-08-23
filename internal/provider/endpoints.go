package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

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
// WHAT DOES NOT ENTER. Only this error class does. A timeout, a 5xx, a 429 and
// a plain 404 from a wrong base URL all keep the behaviour they had (retry.go),
// because none of them is a claim about the request's shape and stripping
// fields off them would spend a person's turn discovering that.

// endpointRefusalStatus is the status half of the gate. 404 is the router's own
// spelling of "nothing can serve this"; 400 is what several OpenAI-compatible
// gateways answer with instead, and both are checked against the words below
// before anything is retried.
func endpointRefusalStatus(status int) bool {
	return status == http.StatusNotFound || status == http.StatusBadRequest
}

// endpointRefusalPhrases is the vocabulary a router refuses a parameter
// combination in. It is the same list internal/swepro's adaptive router matches
// on, kept in the two places rather than shared because one of them is a port of
// somebody else's engine and the other is this adapter's own law.
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
}

// endpointRefusal reads a refusal body for the one complaint this chain answers.
// It matches on the words rather than on the status alone, so a 404 from a
// mistyped base URL — which says nothing about parameters — is surfaced as the
// error it is instead of provoking four retries of a request that can never land.
func endpointRefusal(payload []byte) bool {
	text := strings.ToLower(string(payload))
	for _, phrase := range endpointRefusalPhrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// ── what a retry may take off ───────────────────────────────────────────────

// relaxSet is what one encode has been told to leave out. It rides on callKnobs
// so the ENCODER stays the only place that decides a request's shape: a chain
// that assembled its own bodies would be a second wire format, drifting.
type relaxSet uint8

const (
	// relaxEndpointFilter drops `provider.require_parameters` and
	// `provider.ignore`. FIRST because it is the only rung that changes nothing
	// about what the model is asked — it widens which endpoints may answer, and
	// it is the field most likely to have emptied the set in the first place.
	relaxEndpointFilter relaxSet = 1 << iota
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

// relaxationPlan is the ladder for ONE request: only the rungs that would
// actually change this body, in the order they are climbed.
//
// A rung for a field the request never carried is not a retry, it is the same
// request sent twice — so an unset max_tokens produces no "removed max_tokens"
// line and does not inflate the attempt counter the person is reading.
func (c *Client) relaxationPlan(request *ai.Request, knobs callKnobs, model string) []relaxStep {
	var plan []relaxStep
	if prefs := c.providerPreferences(model); prefs != nil &&
		(prefs.RequireParameters != nil || len(prefs.Ignore) > 0) {
		plan = append(plan, relaxStep{
			bit:   relaxEndpointFilter,
			label: "relaxed the endpoint filter",
			name:  "provider.require_parameters",
		})
	}
	if c.resolveEffort(model, knobs.effort) != EffortNone {
		plan = append(plan, relaxStep{bit: relaxReasoning, label: "removed reasoning", name: "reasoning"})
	}
	if request.MaxTokens != nil {
		plan = append(plan, relaxStep{bit: relaxMaxTokens, label: "removed max_tokens", name: "max_tokens"})
	}
	if request.ResponseFormat != nil {
		plan = append(plan, relaxStep{bit: relaxResponseFormat, label: "removed response_format", name: "response_format"})
	}
	if carriesAttachments(request.Messages) {
		plan = append(plan, relaxStep{bit: relaxImages, label: "removed images", name: "images"})
	}
	if len(request.Tools) > 0 {
		plan = append(plan, relaxStep{bit: relaxTools, label: "removed tools", name: "tools"})
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
		relaxed.relaxed |= step.bit
		stripped = append(stripped, step.name)
		Emit(ctx, StreamNotice, fmt.Sprintf("Retry %d/%d: %s", attempt, total, step.label))
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
	response, err := c.send(ctx, request, body, stream)
	if err != nil {
		return nil, nil, err
	}
	if !endpointRefusalStatus(response.StatusCode) {
		return response, nil, nil
	}
	peek, readErr := io.ReadAll(io.LimitReader(response.Body, maxErrorPeek))
	if readErr != nil || !endpointRefusal(peek) {
		// Not this class after all. The body is handed back whole — the caller
		// still has to read the provider's own words to build its error.
		response.Body = rewound(peek, response.Body)
		return response, nil, nil
	}
	response.Body.Close()
	return nil, peek, nil
}

// ── which model to fall back to ─────────────────────────────────────────────

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
	if prefs := c.providerPreferences(model); prefs != nil {
		if prefs.RequireParameters != nil {
			params = append(params, "provider.require_parameters")
		}
		if len(prefs.Ignore) > 0 {
			params = append(params, "provider.ignore")
		}
	}
	return params
}
