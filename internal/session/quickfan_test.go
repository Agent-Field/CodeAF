package session

// THE REVIEW'S GATE, WIRED ON THE REAL ROAD: a batch whose two quick asks
// met together settles before any of them starts — one amended INTO one
// pocket, one refused as a no, told in ordinary refusals and never a row
// started.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// quickFanBatch is one response holding every quick call the way the model
// makes them: in one breath.
func quickFanBatch(id, lineOne, lineTwo string) step {
	one, _ := json.Marshal(map[string]string{"line": lineOne})
	two, _ := json.Marshal(map[string]string{"line": lineTwo})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return &ai.Response{
			Choices: []ai.Choice{{Message: ai.Message{
				Role: "assistant",
				ToolCalls: []ai.ToolCall{
					{ID: id + "-one", Type: "function", Function: ai.ToolCallFunction{Name: "quick_task", Arguments: string(one)}},
					{ID: id + "-two", Type: "function", Function: ai.ToolCallFunction{Name: "quick_task", Arguments: string(two)}},
				},
			}}},
		}, nil
	}
}

// quickFanVerdict is the reviewer's settled shape, as its own text.
func quickFanVerdict(t *testing.T, verdict string) func([]ai.Message) (*ai.Response, bool) {
	t.Helper()
	return func(snapshot []ai.Message) (*ai.Response, bool) {
		if len(snapshot) == 0 || snapshot[0].Role != "system" ||
			!strings.Contains(messageText(snapshot[0]), "one batch of quick tasks") {
			return nil, false
		}
		return textResponse(verdict), true
	}
}

func TestAQuickFanOutIsReadTogetherBeforeItStarts(t *testing.T) {
	verdict, _ := json.Marshal(map[string]any{
		"why":    "both read the same configs",
		"fine":   false,
		"refuse": []int{1},
		"amend": []map[string]any{{
			"index": 0,
			"line":  "read all four configs",
			"items": []string{"timeout", "retries", "zone", "region"},
			"files": []string{},
		}},
	})
	completer := &scriptedCompleter{steps: []step{
		quickFanBatch("fan", "read the timeout config", "read the timeout config too"),
		finalText("one pouch, then"),
	}}
	completer.aside = quickFanVerdict(t, string(verdict))
	agent, _ := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "survey both configs"))

	// THE REFUSED ASK HOLDS ITS SENTENCE AND SPORES NOTHING. And the amended
	// one admits what the verdict settled, in the pocket that proves it.
	responses := toolTexts(agent)
	sawRefusal := false
	for _, text := range responses {
		if strings.Contains(text, "quick fan-out refused: both read the same configs") {
			sawRefusal = true
		}
	}
	if !sawRefusal {
		t.Fatalf("the refused ask settled with %v, want its sentence", responses)
	}
	graph := agent.graph()
	graph.mu.Lock()
	pockets := 0
	for _, node := range graph.nodes {
		if node.spec.quick != nil && strings.Contains(node.spec.brief, "read all four configs") {
			pockets++
		}
	}
	graph.mu.Unlock()
	if pockets != 1 {
		t.Fatalf("the amended fan settled into %d pockets, want exactly one", pockets)
	}
}

// A ONE-ASK BATCH IS NO SKETCH AT ALL: the review's price is not spent on a
// single quick ask.
func TestASingleQuickAskSpendsNoReview(t *testing.T) {
	one, _ := json.Marshal(map[string]string{"line": "read the timeout config"})
	singleAsk := []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return &ai.Response{
			Choices: []ai.Choice{{Message: ai.Message{
				Role: "assistant",
				ToolCalls: []ai.ToolCall{
					{ID: "+1", Type: "function", Function: ai.ToolCallFunction{Name: "quick_task", Arguments: string(one)}},
				},
			}}},
		}, nil
	}}
	tooMuch := append(singleAsk, finalText("started"))
	completer := &scriptedCompleter{steps: tooMuch}
	completer.aside = func(snapshot []ai.Message) (*ai.Response, bool) {
		if len(snapshot) > 0 && snapshot[0].Role == "system" &&
			strings.Contains(messageText(snapshot[0]), "one batch of quick tasks") {
			t.Fatal("a single ask bought a review it should not have")
		}
		return textResponse(""), false
	}
	agent, _ := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "one config"))
}

// FAIL-OPEN IS THE CONTRACT: a review that cannot be had admits as written,
// so the sender's refusal of a verdict is never the sender's work died.
func TestAnUnreadableReviewAdmitsAsWritten(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		quickFanBatch("fan2", "read one", "read two"),
		finalText("both started"),
	}}
	completer.aside = func(snapshot []ai.Message) (*ai.Response, bool) {
		if len(snapshot) > 0 && snapshot[0].Role == "system" &&
			strings.Contains(messageText(snapshot[0]), "one batch of quick tasks") {
			return textResponse("the reviewer gave up"), true
		}
		return nil, false
	}
	agent, _ := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "both configs"))
	graph := agent.graph()
	graph.mu.Lock()
	started := 0
	for _, node := range graph.nodes {
		if node.spec.quick != nil {
			started++
		}
	}
	graph.mu.Unlock()
	if started != 2 {
		t.Fatalf("an unreadable review settled %d asks, want both admitted", started)
	}
}
