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

// spillFunc preserves a decaying observation's full body in the workspace and
// returns the workspace-relative path it went to. ok=false means the bytes
// could not be written and the caller must not claim a path.
type spillFunc func(toolCallID, body string) (path string, ok bool)

// decayer shrinks old tool results in place, losslessly.
//
// The asymmetry is the whole idea. An assistant message is *compressed state*:
// the model already read the raw output and wrote down what mattered, so those
// messages are never touched. A tool result is *spent raw material* — by the
// time three more turns have happened, its value has usually already been
// extracted into the reasoning above it, and all it does is get re-billed on
// every remaining turn.
//
// Newest results are kept in full until the budget runs out, then everything
// older collapses to a single line naming what it was and where its bytes
// went: before a result is stubbed, its full body is written to the
// workspace's observation directory, so the stub is a pointer rather than a
// tombstone — the agent can re-read the file with sh if it turns out to
// matter. Budgeting by bytes rather than by count means a turn full of small
// results all survive, while one huge one retires early — which is the right
// trade, because the huge one is what costs.
//
// A tool message is only ever shortened, never removed. Its ToolCallID pairs
// with the assistant turn that requested it, and an unpaired tool_call is a
// hard provider error rather than a degraded prompt.
type decayer struct {
	labels map[string]string // tool_call_id → what the call was
	spill  spillFunc         // writes a body to the workspace; nil when there is no filesystem
	// spilled remembers every tool_call_id already stubbed, and the path its
	// bytes went to ("" when no file could be written). It is both the
	// write-once guard and the already-stubbed detector: decay runs over the
	// same transcript every turn, and without it the same result would be
	// re-spilled and re-stubbed each time.
	spilled map[string]string
}

func newDecayer(labels map[string]string, spill spillFunc) *decayer {
	return &decayer{labels: labels, spill: spill, spilled: map[string]string{}}
}

// decay walks the transcript newest-first and stubs whatever raw tool output
// no longer fits the budget. It returns how many results were newly stubbed.
func (d *decayer) decay(messages []ai.Message, budget int) int {
	spent, decayed := 0, 0
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role != "tool" {
			continue
		}
		body := contentOf(messages[index])
		id := messages[index].ToolCallID
		// The transcript is append-only, so a message's index is a stable
		// identity even for the rare tool message without a call id.
		key := id
		if key == "" {
			key = fmt.Sprintf("#%d", index)
		}
		if _, done := d.spilled[key]; done {
			// Already a stub from an earlier turn; it stays exactly as it is.
			spent += len(body)
			continue
		}
		if spent+len(body) <= budget {
			spent += len(body)
			continue
		}
		label := labelFor(d.labels, id)
		stub := fmt.Sprintf("[%s — %d bytes, superseded]", label, len(body))
		if len(stub) >= len(body) {
			// Already smaller than the note describing it; leave it alone.
			// Checked before spilling so no file is written for a result that
			// is kept.
			spent += len(body)
			continue
		}
		// Preserve the bytes before shortening the message: decay must defer
		// detail, never destroy it.
		d.spilled[key] = ""
		if d.spill != nil {
			if path, ok := d.spill(key, body); ok {
				withPath := fmt.Sprintf("[%s — %d bytes, spilled to %s]", label, len(body), path)
				if len(withPath) < len(body) {
					stub = withPath
					d.spilled[key] = path
				}
			}
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
