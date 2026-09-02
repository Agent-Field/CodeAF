package provider

import (
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The reasoning policy: what is sent to a model about its thinking pass, and
// how much room the answer is given behind it. It follows OpenRouter's
// published contract for the unified reasoning knob rather than anything of
// this adapter's own devising, and it is the ONE place the two questions are
// answered — the encoder reads it to shape the request and the transport reads
// it to size its wait, so the two can never disagree about how long a reply
// may be.
//
// What the contract says, and what each rule below is for:
//
//   - A model whose row carries reasoning.mandatory cannot have the pass turned
//     off; the router's own instruction is "do not send effort: none — the
//     model rejects it". Sending NOTHING instead is worse than it looks: the
//     model then runs at its published default, which for GLM 5.3 is "max",
//     and a ten-thousand-token thinking pass ate the intent compiler's whole
//     answer on 2026-08-28. So off on such a model becomes the LOWEST effort
//     word the row lists — "low" on GLM 5.3 cut that pass to 122 tokens.
//
//   - Reasoning tokens are output tokens and bill against max_tokens; the
//     contract's sizing rule is "max_tokens must be strictly higher than the
//     reasoning budget". The effort words are allocations of the ceiling —
//     minimal ≈10%, low ≈20%, medium ≈50%, high ≈80%, xhigh/max ≈95% — so an
//     answer that needs A tokens behind an effort that takes share s of the
//     ceiling needs a ceiling of A/(1-s). A budget in tokens is exact: A plus
//     the budget.
//
// Nothing here is a list of model names. The row is the authority when it is
// known; the quirks memo — a refusal, or an answer that came back empty at the
// ceiling — is what stands in for a row the catalog has not seen.

// ReasoningProfile is the provider's published account of one model's
// thinking pass, in this package's vocabulary. The catalog carries the same
// three facts in strings; config translates at the seam so that neither
// package has to import the other.
type ReasoningProfile struct {
	Mandatory bool
	Efforts   []Effort
	Default   Effort
}

// thinkingShare is the router's documented allocation of the ceiling to the
// thinking pass for each effort word. Unknown words — including a level this
// adapter does not send — allocate nothing.
func thinkingShare(effort Effort) float64 {
	switch strings.ToLower(string(effort)) {
	case "minimal":
		return 0.10
	case "low":
		return 0.20
	case "medium":
		return 0.50
	case "high":
		return 0.80
	case "xhigh", "max":
		return 0.95
	default:
		return 0
	}
}

// effortRank orders the router's words so that "lowest" has one meaning.
// A word this adapter has never heard of ranks above everything, so it is
// never chosen as the floor.
func effortRank(effort Effort) int {
	switch strings.ToLower(string(effort)) {
	case "none":
		return 0
	case "minimal":
		return 1
	case "low":
		return 2
	case "medium":
		return 3
	case "high":
		return 4
	case "xhigh":
		return 5
	case "max":
		return 6
	default:
		return 7
	}
}

// profileFor is the published profile, when the seam is wired and the row is
// known.
func (c *Client) profileFor(model string) (ReasoningProfile, bool) {
	if c.config.ReasoningProfile == nil {
		return ReasoningProfile{}, false
	}
	return c.config.ReasoningProfile(model)
}

// reasoningUnstoppable reports that this model's thinking pass cannot be
// removed — by the row's own word, or because this process has been told so
// by the endpoint (a refused disable, or a disable that was accepted and
// ignored).
func (c *Client) reasoningUnstoppable(model string) bool {
	if profile, known := c.profileFor(model); known && profile.Mandatory {
		return true
	}
	return ReasoningUnavoidable(model)
}

// lowestEffort is the word sent in place of a disable to a model that cannot
// stop thinking: the lowest the row lists, and "low" when the row is silent —
// the router defines it for every reasoning model, and a model that takes
// none of the words is repaired by the relax ladder like any other refused
// knob.
func (c *Client) lowestEffort(model string) Effort {
	profile, known := c.profileFor(model)
	if !known || len(profile.Efforts) == 0 {
		return EffortLow
	}
	lowest := EffortNone
	for _, word := range profile.Efforts {
		if word == EffortNone || effortRank(word) >= effortRank("") {
			continue
		}
		if lowest == EffortNone || effortRank(word) < effortRank(lowest) {
			lowest = word
		}
	}
	if lowest == EffortNone {
		return EffortLow
	}
	return lowest
}

// runningEffort is the level the thinking pass will actually run at: the word
// being sent, or — when nothing is sent to a model that thinks regardless —
// the row's default, taken as the top of the ladder when the row does not say.
func (c *Client) runningEffort(model string, sent Effort) Effort {
	if sent != EffortNone && sent != EffortOff {
		return sent
	}
	if !c.reasoningUnstoppable(model) {
		return EffortNone
	}
	if profile, known := c.profileFor(model); known && profile.Default != EffortNone {
		return profile.Default
	}
	return "max"
}

// unaskedThinkingFloor is the least room a thinking pass NOBODY ASKED FOR is
// given, in tokens.
//
// The share formula sizes the pass off the answer, which is right when the
// pass is one the caller chose and the router will hold to its share. A model
// that ignores the disable runs its own pass at its own level and stops when
// it is done thinking, not when a budget says so — and off a thirty-token
// answer the formula leaves it a hundred and twenty-eight tokens to think in.
// Measured on the task namer, on a model whose shortest observed pass on a
// one-line question was near four hundred tokens: the whole ceiling went to
// thinking, the answer was empty, and the task kept the sentence it was cut
// from. The memo had already learned this model, so nothing asked again.
//
// So a pass the model runs on its own is given at least a page, whatever the
// answer's size, and a max_tokens is a ceiling and not a spend: the room costs
// nothing on a model that answers in three hundred. The figure is a comfortable
// multiple of what was measured, so that one more measurement does not move it.
const unaskedThinkingFloor = 2048

// wireCeiling is the max_tokens that actually travels for an answer the caller
// sized at `answer` tokens: the answer, plus the room the thinking pass in
// front of it is allocated. With a budget in tokens the room is the budget;
// with a word it is the word's share of the ceiling; with no pass it is
// nothing, and the caller's own figure goes out unchanged. A pass the caller
// never asked for, on a model that runs one regardless, gets the share or
// [unaskedThinkingFloor], whichever leaves more room.
func (c *Client) wireCeiling(model string, sent Effort, budget int, answer int) int {
	if answer <= 0 {
		return answer
	}
	if budget > 0 {
		return answer + budget
	}
	running := c.runningEffort(model, sent)
	share := thinkingShare(running)
	if share <= 0 {
		return answer
	}
	ceiling := int(float64(answer)/(1-share) + 0.5)
	if running != sent && ceiling < answer+unaskedThinkingFloor {
		return answer + unaskedThinkingFloor
	}
	return ceiling
}

// ceilingFor is the wire ceiling for one request as its knobs will shape it,
// and whether the request carries a ceiling at all. It is read by the encoder
// and by the transport, which is the point: the wait is sized for the reply
// the request permits, never for the figure the caller started from.
func (c *Client) ceilingFor(request *ai.Request, knobs callKnobs) (int, bool) {
	if request.MaxTokens == nil || knobs.relaxed.has(relaxMaxTokens) {
		return 0, false
	}
	model := c.modelFor(request)
	sent, budget := EffortNone, 0
	if !knobs.relaxed.has(relaxReasoning) {
		sent = c.resolveEffort(model, knobs.effort)
		budget = c.resolveReasoningBudget(model, knobs.effort)
	}
	return c.wireCeiling(model, sent, budget, *request.MaxTokens), true
}
