package provider

import (
	"context"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestListedThinkingFloorLeavesAnswerRoomInsideWindow(t *testing.T) {
	const model = "listed/summary-thinker"
	client, recorded := newTestClient(t, Config{
		Model:             model,
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
		ReasoningProfile: func(string) (ReasoningProfile, bool) {
			return ReasoningProfile{Mandatory: true, Efforts: []Effort{"xhigh", EffortHigh}, Default: "xhigh"}, true
		},
	})
	ctx := WithContextBudget(WithReasoningEffort(context.Background(), EffortLow), ContextBudget{Window: 8192, Reserve: 1000})
	if _, err := client.CompleteWithMessages(ctx, userMessages("Summarize this conversation"), ai.WithMaxTokens(1000)); err != nil {
		t.Fatal(err)
	}
	if recorded.count() != 1 {
		t.Fatalf("requests = %d, want one", recorded.count())
	}
	body := recorded.body(0)
	reasoning, _ := body["reasoning"].(map[string]any)
	// The word travels as asked; only the room is sized for the level the
	// model can actually run at.
	if reasoning["effort"] != "low" {
		t.Fatalf("reasoning = %#v, want the requested effort word unchanged", reasoning)
	}
	// The model runs at high at the least, which thinks with 80% of the
	// ceiling: 1,000 tokens of answer need 5,000, and the window holds them.
	if ceiling, _ := body["max_tokens"].(float64); ceiling < 5000 || ceiling >= 8192 {
		t.Fatalf("max_tokens = %v, want room for a high thinking pass above the 1,000-token answer, inside the window", body["max_tokens"])
	}
}
