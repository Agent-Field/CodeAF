package plan

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/shaped"
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
	index := len(c.ceilings) - 1
	if index >= len(c.replies) {
		index = len(c.replies) - 1
	}
	return c.replies[index], nil
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

// A planning call asks for a form through its schema and adds no generation
// ceiling of its own.
func TestAStructuredCallLeavesGenerationToTheProvider(t *testing.T) {
	client := &ceilingClient{replies: []*ai.Response{wholeReply(`{"mode":"ensemble","reason":"fine"}`)}}
	var panel Panel
	if _, err := structured(context.Background(), client, nil, json.RawMessage(`{}`), &panel); err != nil {
		t.Fatalf("structured: %v", err)
	}
	if client.ceilings[0] != 0 {
		t.Fatalf("structured call carried max_tokens = %d", client.ceilings[0])
	}
}

// The fan-out width remains one source for the prompt and repair accounting,
// but it no longer becomes a generation parameter.
func TestAFanOutStatesItsWidthWithoutSendingACeiling(t *testing.T) {
	client := &ceilingClient{replies: []*ai.Response{wholeReply(`{"parts":[]}`)}}
	var decoded struct {
		Parts []Node `json:"parts"`
	}
	if _, err := structuredParts(context.Background(), client, nil, fanoutSchema, fanOutWidth, &decoded); err != nil {
		t.Fatalf("structuredParts: %v", err)
	}
	if client.ceilings[0] != 0 {
		t.Fatalf("a %d-part ask carried max_tokens = %d", fanOutWidth, client.ceilings[0])
	}
	// And the number the model is told is the number the room is sized for.
	// Two spellings of one figure is the drift this interpolation prevents.
	if !strings.Contains(fanoutPrompt, "Give 1 to "+fanOutWidthWord+" parts") {
		t.Fatal("the fan-out prompt no longer states the width its accounting is derived from")
	}
}

// A fan-out cut mid-part is CONTINUED, and the plan survives it. Before the
// seam, the whole run did not.
func TestACutFanOutIsContinuedAndTheStageStillLands(t *testing.T) {
	client := &ceilingClient{replies: []*ai.Response{
		cutReply(`{"parts":[{"title":"read the failing test","summary":"find why follow state resets","sources":["a"]},{"title":"fix the fol`, 8192),
		wholeReply(`low state","summary":"keep the flag across resize","sources":["b"]}]}`),
	}}
	var decoded struct {
		Parts []Node `json:"parts"`
	}
	if _, err := structuredParts(context.Background(), client, nil, fanoutSchema, fanOutWidth, &decoded); err != nil {
		t.Fatalf("a cut fan-out must be continued, not surfaced as a dead plan: %v", err)
	}
	if len(decoded.Parts) != 2 || decoded.Parts[1].Title != "fix the follow state" {
		t.Fatalf("the halves were not joined: %+v", decoded.Parts)
	}
}

// A complete object under a "length" finish is an answer, not a cut reply.
// nvidia/nemotron-3.5-lightning does this on the panel question — the token
// counter reaches the ceiling on tokens that never became text — and the
// retry taken on the finish reason alone cost forty-five seconds for a second
// copy of an answer already in hand.
func TestACompleteReplyUnderALengthFinishIsNotRetried(t *testing.T) {
	client := &ceilingClient{replies: []*ai.Response{cutReply(`{"mode":"decompose","reason":"one subject"}`, 8192)}}
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

// A model that will not answer in shape at all leaves the planner with a typed
// fault rather than a quiet success — the door above it turns that into the
// smallest admissible plan instead of ending the run.
func TestAPlannerAnswerThatIsNeverJSONIsATypedFault(t *testing.T) {
	client := &ceilingClient{replies: []*ai.Response{wholeReply("I'd start by reading the test."), wholeReply("Still just prose.")}}
	var decoded struct {
		Parts []Node `json:"parts"`
	}
	_, err := structuredParts(context.Background(), client, nil, fanoutSchema, fanOutWidth, &decoded)
	if !shaped.Unreadable(err) {
		t.Fatalf("want the typed fault, got %v", err)
	}
}
