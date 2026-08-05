package exec

import (
	"fmt"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// observationBudget is the floor on how many bytes of raw tool output the
// transcript carries before older results start fading. Everything past it is
// still referenced, just not quoted. The loop scales the window up with the
// task's token budget; these bound it at both ends.
const (
	observationBudget    = 24 << 10
	maxObservationBudget = 256 << 10
)

// decayObservations shrinks old tool results in place.
//
// The asymmetry is the whole idea. An assistant message is *compressed state*:
// the model already read the raw output and wrote down what mattered, so those
// messages are never touched. A tool result is *spent raw material* — by the
// time three more turns have happened, its value has usually already been
// extracted into the reasoning above it, and all it does is get re-billed on
// every remaining turn.
//
// Newest results are kept in full until the budget runs out, then everything
// older collapses to a single line naming what it was. Budgeting by bytes rather
// than by count means a turn full of small results all survive, while one huge
// one retires early — which is the right trade, because the huge one is what
// costs.
//
// A tool message is only ever shortened, never removed. Its ToolCallID pairs
// with the assistant turn that requested it, and an unpaired tool_call is a
// hard provider error rather than a degraded prompt.
func decayObservations(messages []ai.Message, labels map[string]string, budget int) int {
	spent, decayed := 0, 0
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role != "tool" {
			continue
		}
		body := contentOf(messages[index])
		if spent+len(body) <= budget {
			spent += len(body)
			continue
		}
		stub := fmt.Sprintf("[%s — %d bytes, superseded]", labelFor(labels, messages[index].ToolCallID), len(body))
		if len(stub) >= len(body) {
			// Already smaller than the note describing it; leave it alone.
			spent += len(body)
			continue
		}
		messages[index].Content = text(stub)
		decayed++
	}
	return decayed
}

func labelFor(labels map[string]string, id string) string {
	if label, ok := labels[id]; ok && label != "" {
		return label
	}
	return "earlier tool result"
}

func contentOf(message ai.Message) string {
	total := 0
	for _, part := range message.Content {
		total += len(part.Text)
	}
	if total == 0 {
		return ""
	}
	body := make([]byte, 0, total)
	for _, part := range message.Content {
		body = append(body, part.Text...)
	}
	return string(body)
}

// callLabel is what a decayed result is remembered as. It names the tool and
// enough of the arguments to recognise, so the model can tell "I already
// searched for X" from "I already read file Y" without the bytes.
func callLabel(call ai.ToolCall) string {
	arguments := call.Function.Arguments
	const maxLabelArgs = 60
	if len(arguments) > maxLabelArgs {
		arguments = arguments[:maxLabelArgs] + "…"
	}
	return call.Function.Name + " " + arguments
}
