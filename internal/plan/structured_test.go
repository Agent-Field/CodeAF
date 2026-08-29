package plan

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ceilingClient answers a scripted reply per call and remembers the ceiling
// each call asked for, read the way the adapter reads it: by applying the
// options to a request.
type ceilingClient struct {
	replies  []*ai.Response
	ceilings []int
}

func (c *ceilingClient) CompleteWithMessages(_ context.Context, _ []ai.Message, options ...ai.Option) (*ai.Response, error) {
	request := &ai.Request{}
	for _, option := range options {
		_ = option(request)
	}
	ceiling := 0
	if request.MaxTokens != nil {
		ceiling = *request.MaxTokens
	}
	c.ceilings = append(c.ceilings, ceiling)
	reply := c.replies[len(c.ceilings)-1]
	return reply, nil
}

func cutReply(text string, spent int) *ai.Response {
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}}, FinishReason: "length"}},
		Usage:   &ai.Usage{CompletionTokens: spent},
	}
}

func wholeReply(text string) *ai.Response {
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}}, FinishReason: "stop"}}}
}

// A planning call asks for a form, and its first attempt is sized for one.
// Left to the client's default the ceiling was the leaf completion reserve,
// and a model that looped inside the schema ran to it: four minutes and
// $0.013 for fifty-seven characters, twice in one plan. A reply cut at the
// ceiling — empty or not — gets exactly one retry with room to double.
func TestAStructuredCallIsSizedForItsAnswerAndRetriesACutReplyOnce(t *testing.T) {
	client := &ceilingClient{replies: []*ai.Response{
		cutReply(`{"mode": "ens`, structuredReplyTokens),
		wholeReply(`{"mode":"ensemble","reason":"fine"}`),
	}}
	var panel Panel
	if _, err := structured(context.Background(), client, nil, json.RawMessage(`{}`), &panel); err != nil {
		t.Fatalf("a cut reply must be retried, not returned: %v", err)
	}
	if panel.Mode != "ensemble" {
		t.Fatalf("decoded %+v, want the retry's answer", panel)
	}
	if len(client.ceilings) != 2 {
		t.Fatalf("calls = %d, want the attempt and one retry", len(client.ceilings))
	}
	if client.ceilings[0] != structuredReplyTokens {
		t.Fatalf("first ceiling = %d, want %d — a form's worth, never the leaf reserve", client.ceilings[0], structuredReplyTokens)
	}
	if want := retryTokenBudget(client.replies[0]); client.ceilings[1] != want {
		t.Fatalf("retry ceiling = %d, want %d", client.ceilings[1], want)
	}
}

func TestASecondCutReplyIsTheModelsProblemNotTheBudgets(t *testing.T) {
	client := &ceilingClient{replies: []*ai.Response{
		cutReply(`{"mode": "ens`, structuredReplyTokens),
		cutReply(`{"mode": "ensemble", "reas`, 16_000),
	}}
	var panel Panel
	if _, err := structured(context.Background(), client, nil, json.RawMessage(`{}`), &panel); err == nil {
		t.Fatal("two cut replies must surface as the decode failure they are")
	}
	if len(client.ceilings) != 2 {
		t.Fatalf("calls = %d, want exactly two — the budget is not the problem", len(client.ceilings))
	}
}

// A complete object under a "length" finish is an answer, not a cut reply.
// nvidia/nemotron-3.5-lightning does this on the panel question — the token
// counter reaches the ceiling on tokens that never became text — and the
// retry taken on the finish reason alone cost forty-five seconds for a second
// copy of an answer already in hand.
func TestACompleteReplyUnderALengthFinishIsNotRetried(t *testing.T) {
	client := &ceilingClient{replies: []*ai.Response{cutReply(`{"mode":"decompose","reason":"one subject"}`, structuredReplyTokens)}}
	var out struct {
		Mode   string `json:"mode"`
		Reason string `json:"reason"`
	}
	if _, err := structured(context.Background(), client, nil, json.RawMessage(`{}`), &out); err != nil {
		t.Fatalf("structured: %v", err)
	}
	if len(client.ceilings) != 1 {
		t.Fatalf("calls = %d, want exactly one — the answer was already in hand", len(client.ceilings))
	}
	if out.Mode != "decompose" || out.Reason != "one subject" {
		t.Fatalf("decoded %+v, want the complete object", out)
	}
}
