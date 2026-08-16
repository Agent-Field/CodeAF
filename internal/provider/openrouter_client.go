package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE OWN OPENROUTER PATH ─────────────────────────────────────────────────
//
// Everything a chat turn does against OpenRouter is written in this package:
// the body (wire.go), the headers (attribution.go), the send and its retries
// (retry.go), the stream decode (sse.go), the routing preferences (velocity.go)
// and the refusal ladder (endpoints.go). One surface was still not — the tool
// loop below, which handed the whole conversation to the SDK's client and got
// back an answer fetched over the SDK's transport.
//
// That transport is not ours in three ways that matter. It cannot set
// X-OpenRouter-Categories, so every request it made was attributed as an
// unclassified app; it does not carry the endpoint-refusal ladder, so a turn
// whose belt no endpoint would accept ended in a 404 rather than in a retry
// that took the belt off; and it does not write the velocity ledger, so a loop
// running through it taught this process nothing about the endpoints serving
// it. The loop here is the same algorithm over the adapter's own send, which
// is what makes those three sentences stop being true.
//
// IT IS THE SDK'S ALGORITHM, deliberately. Turn accounting, the limit message,
// the tool-name spelling, the trace shape and the two error strings are matched
// so that a caller which today reads an ai.ToolCallTrace keeps reading exactly
// what it read before. What differs is only what the requests are made OF.

// executeOwnToolCallLoop runs the tool-call loop over this adapter's own
// transport: send messages with tools, dispatch what the model asked for, feed
// the results back, and stop at a text answer or at one of the two limits.
//
// Every turn goes through [Client.CompleteWithMessages], which is the same door
// the harness's own loop uses. So a turn made here inherits the whole of the
// adapter — attribution, cache affinity, the reasoning economy, the 400 repairs,
// the refusal ladder with its fallback models, and the velocity measurement —
// and a turn made here is also STREAMED when the caller's context carries an
// observer, which the SDK path could never do.
func (c *Client) executeOwnToolCallLoop(
	ctx context.Context,
	messages []ai.Message,
	tools []ai.ToolDefinition,
	config ai.ToolCallConfig,
	dispatch ai.CallFunc,
	options ...ai.Option,
) (*ai.Response, *ai.ToolCallTrace, error) {
	trace := &ai.ToolCallTrace{}
	prompts := resolveToolPrompts(config.PromptConfig)
	if dispatch == nil {
		// The SDK panics here. A loop with nothing to dispatch to is a caller's
		// mistake either way, but it is one the model can be told about and
		// answer around, which is strictly more than a dead process.
		dispatch = func(context.Context, string, map[string]interface{}) (map[string]interface{}, error) {
			return nil, errors.New("no tool dispatcher was supplied")
		}
	}
	// A system prompt is prepended as an option so a caller's own options still
	// run after it and can override anything it set.
	turnOptions := options
	if strings.TrimSpace(config.SystemPrompt) != "" {
		turnOptions = append([]ai.Option{ai.WithSystem(config.SystemPrompt)}, options...)
	}
	// The caller's slice is never appended to: a loop that grew the array behind
	// it would rewrite messages the caller still holds.
	loop := append([]ai.Message(nil), messages...)
	dispatched := 0

	for turn := 0; turn < config.MaxTurns; turn++ {
		trace.TotalTurns = turn + 1

		response, err := c.CompleteWithMessages(ctx, loop, c.toolTurnOptions(tools, turnOptions)...)
		if err != nil {
			return nil, trace, fmt.Errorf("LLM call failed: %w", err)
		}
		recordTurnUsage(trace, response)

		if !response.HasToolCalls() {
			trace.FinalResponse = response.Text()
			return response, trace, nil
		}
		loop = append(loop, response.Choices[0].Message)

		for _, requested := range response.ToolCalls() {
			// PAST THE CEILING EVERY REMAINING CALL IS STILL ANSWERED, with the
			// refusal rather than with a result. A tool call left without a
			// tool message is a hole in the transcript that most endpoints
			// reject outright on the next turn.
			if dispatched >= config.MaxToolCalls {
				loop = append(loop, toolResultMessage(requested.ID,
					map[string]string{"error": prompts.ToolCallLimitReached}))
				continue
			}
			dispatched++
			trace.TotalToolCalls = dispatched

			var arguments map[string]interface{}
			if err := json.Unmarshal([]byte(requested.Function.Arguments), &arguments); err != nil {
				// Arguments that will not parse are sent on as none. The tool
				// says what it needed far better than this loop could guess.
				arguments = map[string]interface{}{}
			}
			name := unsanitizeToolName(requested.Function.Name)
			record := ai.ToolCallRecord{ToolName: name, Arguments: arguments, Turn: turn}

			began := c.clock()
			result, err := dispatch(ctx, name, arguments)
			record.LatencyMs = float64(c.clock().Sub(began).Milliseconds())

			if err != nil {
				record.Error = err.Error()
				loop = append(loop, toolResultMessage(requested.ID, prompts.ToolErrorFormatter(name, err)))
			} else {
				record.Result = result
				loop = append(loop, toolResultMessage(requested.ID, prompts.ToolResultFormatter(name, result)))
			}
			trace.Calls = append(trace.Calls, record)
		}

		if dispatched >= config.MaxToolCalls {
			return c.finalToolTurn(ctx, trace, loop, turnOptions)
		}
	}

	// Out of turns. The last call is made without tools, so the model answers
	// with words instead of asking for another round it cannot have.
	response, _, err := c.finalToolTurn(ctx, trace, loop, turnOptions)
	if err != nil {
		return nil, trace, err
	}
	trace.TotalTurns = config.MaxTurns
	return response, trace, nil
}

// toolTurnOptions is one turn's option list: the belt first, so a caller's own
// options still win, exactly as they do on the SDK path.
func (c *Client) toolTurnOptions(tools []ai.ToolDefinition, options []ai.Option) []ai.Option {
	return append([]ai.Option{ai.WithTools(tools)}, options...)
}

// finalToolTurn makes the one call that carries no tools, which is how both
// limits end: a model that cannot ask for anything more answers with what it
// has.
func (c *Client) finalToolTurn(
	ctx context.Context,
	trace *ai.ToolCallTrace,
	messages []ai.Message,
	options []ai.Option,
) (*ai.Response, *ai.ToolCallTrace, error) {
	response, err := c.CompleteWithMessages(ctx, messages, options...)
	if err != nil {
		return nil, trace, fmt.Errorf("final LLM call failed: %w", err)
	}
	recordTurnUsage(trace, response)
	trace.FinalResponse = response.Text()
	return response, trace, nil
}

// recordTurnUsage adds one call's tokens to the trace. Every call the loop makes
// is recorded, intermediate turns included, because the loop's cost is the sum
// of them and a caller billing on the final response alone would under-count a
// ten-turn conversation by nine calls. A response without usage is skipped: a
// missing count is not a zero.
func recordTurnUsage(trace *ai.ToolCallTrace, response *ai.Response) {
	if trace == nil || response == nil || response.Usage == nil {
		return
	}
	trace.Usage = append(trace.Usage, ai.TurnUsage{Model: response.Model, Usage: response.Usage})
}

// toolResultMessage is one tool's answer in the shape the wire wants it.
func toolResultMessage(id string, content interface{}) ai.Message {
	return ai.Message{
		Role:       "tool",
		Content:    []ai.ContentPart{{Type: "text", Text: encodeToolContent(content)}},
		ToolCallID: id,
	}
}

// encodeToolContent renders a tool result as the string the message carries.
// Strings and bytes travel as themselves — a tool that already wrote prose
// should not have it JSON-quoted — and everything else is marshalled.
func encodeToolContent(content interface{}) string {
	switch value := content.(type) {
	case string:
		return value
	case []byte:
		return string(value)
	default:
		payload, err := json.Marshal(value)
		if err != nil {
			return "{}"
		}
		return string(payload)
	}
}

// unsanitizeToolName maps a wire-safe tool name back to the invocation target
// it was made from. The router's own naming rules forbid the colon an
// AgentField target is spelled with, so it travels as a double underscore and
// is put back here — the same substitution the SDK makes, kept identical
// because the names on both sides of it are a caller's registry keys.
func unsanitizeToolName(name string) string {
	return strings.ReplaceAll(name, "__", ":")
}

// resolveToolPrompts fills a caller's partial prompt configuration from the
// SDK's defaults, field by field. The defaults are the SDK's own — they are
// shared vocabulary rather than transport, and a model that has been told
// "Tool call limit reached" in those exact words for the whole life of this
// harness should keep being told it that way.
func resolveToolPrompts(config *ai.PromptConfig) ai.PromptConfig {
	resolved := ai.DefaultPromptConfig()
	if config == nil {
		return resolved
	}
	if strings.TrimSpace(config.ToolCallLimitReached) != "" {
		resolved.ToolCallLimitReached = config.ToolCallLimitReached
	}
	if config.ToolErrorFormatter != nil {
		resolved.ToolErrorFormatter = config.ToolErrorFormatter
	}
	if config.ToolResultFormatter != nil {
		resolved.ToolResultFormatter = config.ToolResultFormatter
	}
	return resolved
}
