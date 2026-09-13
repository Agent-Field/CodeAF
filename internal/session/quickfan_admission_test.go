package session

// THE FAMILY GATE, ONCE A TURN: a second ever quick child on a family that
// still has one running is read before it joins, and the verdict is said
// the way every refusal on the quick door is said — as an ordinary refusal.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// quickAdmissionBatch is what the same turn makes of settlement: two quick
// asks in one breath, titled enough to overlap.
func quickAdmissionBatch(id, lineOne, lineTwo string) step {
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

// admissionAside is the reviewer's own verdict, in the page's own words.
func admissionAside(t *testing.T, verdict string) func([]ai.Message) (*ai.Response, bool) {
	t.Helper()
	return func(snapshot []ai.Message) (*ai.Response, bool) {
		if len(snapshot) == 0 || snapshot[0].Role != "system" ||
			!strings.Contains(messageText(snapshot[0]), "one quick task's joining") {
			return nil, false
		}
		return textResponse(verdict), true
	}
}

func TestTheFamilyGateRefusesTheCollidingSecond(t *testing.T) {
	verdict, _ := json.Marshal(map[string]any{
		"ok":  false,
		"why": "both read the same configs",
	})
	completer := &scriptedCompleter{steps: []step{
		quickAdmissionBatch("qa", "read the timeout config", "read the timeout config"),
		finalText("just one"),
	}}
	completer.aside = admissionAside(t, string(verdict))
	agent, _ := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "survey both configs"))
	responses := toolTexts(agent)
	// THE ASK THAT STOPS IS SAID WITH ITS OWN SENTENCE, whole, as the
	// refusing review's reason.
	sawRefusal := false
	for _, text := range responses {
		if strings.Contains(text, "quick admission refused: both read the same configs") {
			sawRefusal = true
		}
	}
	if !sawRefusal {
		t.Fatalf("no refused admission found in %v", responses)
	}
	// AND THE GATE'S RECIPE, STILL ONE ADMITTED.
	graph := agent.graph()
	graph.mu.Lock()
	pockets := 0
	for _, node := range graph.nodes {
		if node.spec.quick != nil {
			pockets++
		}
	}
	graph.mu.Unlock()
	if pockets != 1 {
		t.Fatalf("settled %d nodes, want exactly one admitted", pockets)
	}
}

func TestAQuickAdmissionReviewUnansweredFailsOpen(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		quickAdmissionBatch("qa", "read the alpha file", "read the beta file"),
		finalText("two pockets"),
	}}
	completer.aside = func(snapshot []ai.Message) (*ai.Response, bool) {
		if len(snapshot) > 0 && snapshot[0].Role == "system" &&
			strings.Contains(messageText(snapshot[0]), "one quick task's joining") {
			return textResponse("gave up"), true
		}
		return nil, false
	}
	agent, _ := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "read both"))
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
		t.Fatalf("an unreadable gate settled %d asks, want both admitted", started)
	}
}
