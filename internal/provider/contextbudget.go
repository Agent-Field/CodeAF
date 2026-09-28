package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
)

// ContextBudget describes the conversation whose next request is being encoded.
// The provider applies it after tool schemas, replayed reasoning and routing
// preferences have been assembled, so the check measures what will be sent.
type ContextBudget struct {
	Window      int
	Reserve     int
	PromptFloor int
}
type contextBudgetKey struct{}

func WithContextBudget(ctx context.Context, budget ContextBudget) context.Context {
	return context.WithValue(ctx, contextBudgetKey{}, budget)
}
func contextBudgetFrom(ctx context.Context) ContextBudget {
	budget, _ := ctx.Value(contextBudgetKey{}).(ContextBudget)
	return budget
}

// ContextLimit is an endpoint's stated total window, never a rejected prompt's
// size. The key includes the account's base URL and the serving endpoint.
type ContextLimit struct {
	Base     string `json:"base"`
	Model    string `json:"model"`
	Provider string `json:"provider,omitempty"`
	Tokens   int    `json:"tokens"`
}

func contextLimitKey(base, model, endpoint string) string {
	return strings.TrimRight(strings.ToLower(base), "/") + "\n" + normalizeModel(model) + "\n" + strings.ToLower(strings.TrimSpace(endpoint))
}
func (c *Client) rememberContextLimit(model string, failure *APIError) {
	if failure == nil || !failure.Overflow || failure.Local || failure.ContextLimit <= 0 {
		return
	}
	limit := ContextLimit{Base: c.config.BaseURL, Model: normalizeModel(model), Provider: failure.Provider, Tokens: failure.ContextLimit}
	key := contextLimitKey(limit.Base, limit.Model, limit.Provider)
	quirks.mutex.Lock()
	if quirks.contextLimits == nil {
		quirks.contextLimits = make(map[string]ContextLimit)
	}
	old, known := quirks.contextLimits[key]
	changed := !known || old.Tokens != limit.Tokens
	quirks.contextLimits[key] = limit
	quirks.mutex.Unlock()
	failure.BudgetChanged = changed
	if changed {
		quirks.persist()
	}
}

// requestWindow takes the smallest known window among endpoints this request
// can reach. A strict pin excludes other endpoints; an advisory order does not.
// This is a read of the existing sheet and memo, with no network work.
func (c *Client) requestWindow(model string, prefs *providerPrefs, claimed int) int {
	window := claimed
	take := func(tokens int) {
		if tokens > 0 && (window <= 0 || tokens < window) {
			window = tokens
		}
	}
	accepts := func(endpoint string) bool {
		if prefs == nil || endpoint == "" {
			return true
		}
		return !namesEndpoint(prefs.Ignore, endpoint) && (len(prefs.Only) == 0 || namesEndpoint(prefs.Only, endpoint))
	}
	if !c.config.Direct && c.baseServesLanes() {
		for _, row := range lanes.Default().Sheet().Rows(laneModel(model)) {
			if accepts(row.ID.Lane) {
				take(row.Facts.Context)
			}
		}
	}
	quirks.mutex.Lock()
	for _, limit := range quirks.contextLimits {
		if contextLimitKey(limit.Base, limit.Model, "") == contextLimitKey(c.config.BaseURL, model, "") && accepts(limit.Provider) {
			take(limit.Tokens)
		}
	}
	quirks.mutex.Unlock()
	return window
}

// minimumContextAnswer is a useful short answer, not the desired reply size.
// A reserve is a ceiling: requiring thousands of unused output tokens made
// small-window conversations fail even when a substantial answer still fit.
const minimumContextAnswer = 512

// ContextSafetyTokens leaves room for tokenizer and chat-template differences.
// It grows with small windows and is bounded on million-token models.
func ContextSafetyTokens(window int) int { return min(8192, max(512, window/20)) }

// budgetWire checks the encoded input and sizes a TOTAL output allowance,
// including thinking. It never lets an omitted max_tokens delegate that size
// to a provider default which may not fit behind the prompt.
func (c *Client) budgetWire(request *ai.Request, knobs callKnobs, messages, tools []json.RawMessage, prefs *providerPrefs, ceiling int, hasCeiling bool) (int, bool, error) {
	if knobs.contextBudget.Window <= 0 {
		return ceiling, hasCeiling, nil
	}
	model := c.modelFor(request)
	window := c.requestWindow(model, prefs, knobs.contextBudget.Window)
	weight := 0
	for _, message := range messages {
		weight += len(message)
	}
	for _, tool := range tools {
		weight += len(tool)
	}
	// Base64 bytes are transport, not input tokens. The image allowance matches
	// the conversation estimator; the safety reserve covers template overhead.
	for _, message := range request.Messages {
		for _, part := range message.Content {
			if part.ImageURL != nil {
				weight += 4000 - len(part.ImageURL.URL)
			}
		}
	}
	prompt := max((weight+3)/4, knobs.contextBudget.PromptFloor)
	if !hasCeiling {
		ceiling = min(knobs.contextBudget.Reserve, window/4)
		if ceiling <= 0 {
			ceiling = window / 4
		}
	}
	// A useful answer still needs room after thinking. An explicit tiny answer
	// is allowed, but an ordinary turn cannot be squeezed to a single token.
	floor := min(ceiling, min(minimumContextAnswer, max(1, window/8)))
	if !knobs.relaxed.has(relaxReasoning) {
		if thinking := c.resolveReasoningBudget(model, knobs.effort); thinking > 0 {
			floor = max(floor, thinking+min(1024, max(1, window/16)))
		}
	}
	room := window - ContextSafetyTokens(window) - prompt
	if room < floor {
		return 0, false, &APIError{Status: http.StatusBadRequest, Code: overflowCode, Overflow: true, Local: true,
			ContextLimit: window, InputTokens: prompt, OutputTokens: floor,
			Message: fmt.Sprintf("context needs shortening before sending: about %d input tokens plus %d output tokens and %d safety tokens exceed the %d-token window; compact the conversation or choose a larger-context model", prompt, floor, ContextSafetyTokens(window), window)}
	}
	return min(max(ceiling, floor), room), true, nil
}

var contextLimitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)maximum context (?:length|window)(?: is| of|:)\s*([0-9][0-9,]*)`),
	regexp.MustCompile(`(?i)(?:context length|context window|context limit)\s*[:=]\s*([0-9][0-9,]*)`),
}
var inputLimitPattern = regexp.MustCompile(`(?i)(?:prompt contains (?:at least )?|input_tokens[^0-9]{1,12})([0-9][0-9,]*)`)
var outputLimitPattern = regexp.MustCompile(`(?i)(?:requested |max_tokens[^0-9]{1,12})([0-9][0-9,]*)(?: output tokens)?`)
var anthropicLimitPattern = regexp.MustCompile(`(?i)prompt is too long:\s*([0-9][0-9,]*) tokens\s*>\s*([0-9][0-9,]*)`)

func tokenNumber(text string) int {
	number, err := strconv.Atoi(strings.ReplaceAll(text, ",", ""))
	if err != nil || number <= 0 {
		return 0
	}
	return number
}

// readContextLimit extracts optional evidence AFTER overflow classification.
// Missing or changed prose never prevents recovery, and no guessed input count
// is promoted to a durable context-window fact.
func readContextLimit(failure *APIError) {
	if failure == nil || !failure.Overflow {
		return
	}
	said := failure.Message + "\n" + failure.Raw
	for _, pattern := range contextLimitPatterns {
		if match := pattern.FindStringSubmatch(said); len(match) > 1 {
			failure.ContextLimit = tokenNumber(match[1])
			break
		}
	}
	if match := inputLimitPattern.FindStringSubmatch(said); len(match) > 1 {
		failure.InputTokens = tokenNumber(match[1])
	}
	if match := outputLimitPattern.FindStringSubmatch(said); len(match) > 1 {
		failure.OutputTokens = tokenNumber(match[1])
	}
	if match := anthropicLimitPattern.FindStringSubmatch(said); len(match) > 2 {
		failure.InputTokens = tokenNumber(match[1])
		failure.ContextLimit = tokenNumber(match[2])
	}
}
