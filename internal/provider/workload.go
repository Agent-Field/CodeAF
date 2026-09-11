package provider

import (
	"fmt"
	"math"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// workloadClass keeps a short prose reply from teaching a tool loop its length,
// and keeps different reasoning settings separate. No provider names or prices
// belong here: answer size is work the model must do wherever it runs.
func (c *Client) workloadClass(model string, knobs callKnobs, request *ai.Request) string {
	return fmt.Sprintf("%d/%t/%s", knobs.intent, len(request.Tools) > 0, c.recordedEffort(model, knobs))
}

// workloadFor learns from receipts when possible and from the conversation
// otherwise. A genuinely new request has unknown output size, not an invented
// 400-token answer; its first routing choice can still use input cost and TTFT.
func (c *Client) workloadFor(model string, knobs callKnobs, request *ai.Request) (int, int) {
	if request == nil {
		return 0, 0
	}
	var visible, hidden int
	known := false
	if history, ok := lanes.Default().Ledger().(lanes.Workloads); ok {
		visible, hidden, known = history.Workload(model, c.workloadClass(model, knobs, request), laneNow())
	}
	if !known {
		count := 0
		for index, message := range request.Messages {
			if message.Role != "assistant" {
				continue
			}
			text, tools := messageWorkBytes(message)
			if text+tools == 0 {
				continue
			}
			count++
			visible += text
			hidden += tools
			if index < len(knobs.reasoning) && !knobs.reasoning[index].producedElsewhere(model) {
				hidden += len(knobs.reasoning[index].Text)
			}
		}
		if count > 0 {
			visible = int(math.Ceil(float64(visible) / float64(count*charsPerToken)))
			hidden = int(math.Ceil(float64(hidden) / float64(count*charsPerToken)))
		}
	}
	if knobs.intent == IntentBackground {
		hidden, visible = hidden+visible, 0
	}
	// The request's real output cap bounds both billing and the length the
	// chooser may expect; an earlier large answer cannot exceed today's cap.
	if ceiling, capped := c.ceilingFor(request, knobs); capped && ceiling > 0 && visible+hidden > ceiling {
		visible = int(float64(visible) * float64(ceiling) / float64(visible+hidden))
		hidden = ceiling - visible
	}
	return visible, hidden
}

// noteWorkload receives only completed usable responses from both transport
// epilogues. Partial or cancelled streams must not teach a shorter answer.
func (c *Client) noteWorkload(request *ai.Request, knobs callKnobs, response *ai.Response, reasoning int) {
	if request == nil || response == nil || response.Usage == nil || response.Usage.CompletionTokens <= 0 || c.routing() == RoutingOff {
		return
	}
	history, ok := lanes.Default().Ledger().(lanes.Workloads)
	if !ok {
		return
	}
	visibleBytes, toolBytes := 0, 0
	for _, choice := range response.Choices {
		// A capped or filtered answer is only a lower bound on the work that
		// was needed. Teaching it as a completed answer would reward cuts.
		if choice.FinishReason != "stop" && choice.FinishReason != "tool_calls" && choice.FinishReason != "function_call" {
			return
		}
		text, tools := messageWorkBytes(choice.Message)
		visibleBytes += text
		toolBytes += tools
	}
	total := response.Usage.CompletionTokens
	if visibleBytes+toolBytes == 0 || reasoning < 0 || reasoning > total {
		return
	}
	visible := 0
	// Usage owns the token total. Bytes only apportion non-reasoning tokens
	// between readable text and tool arguments; they never invent a bill.
	if knobs.intent != IntentBackground && visibleBytes+toolBytes > 0 {
		visible = int(float64(max(0, total-reasoning)) * float64(visibleBytes) / float64(visibleBytes+toolBytes))
	}
	history.NoteWorkload(lanes.Workload{
		Model: c.modelFor(request), Class: c.workloadClass(c.modelFor(request), knobs, request),
		Visible: visible, Hidden: total - visible, At: laneNow(),
	})
}

func messageWorkBytes(message ai.Message) (text, tools int) {
	for _, part := range message.Content {
		text += len(part.Text)
	}
	for _, call := range message.ToolCalls {
		tools += len(call.Function.Name) + len(call.Function.Arguments)
	}
	return text, tools
}
