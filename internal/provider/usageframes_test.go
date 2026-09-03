package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The law under test: USAGE ACCUMULATES ACROSS THE FRAMES OF ONE CALL. A later
// frame states what it knows; it never zeroes a count an earlier frame carried.
//
// Nothing aforge drives today splits the frame — DeepSeek and Kimi both send one
// terminal block with the counts and the price in it — so what these tests hold
// is a property of the protocol rather than a reproduction of a live failure.
// The cost of getting it wrong is a call billed with zero prompt tokens, zero
// completion tokens and no thinking pass, which reconciles against nothing.

// streamingUsageServer answers one streamed chat with the frames given, in
// order, each already shaped as an SSE `data:` event body.
func streamingUsageServer(t *testing.T, frames ...string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		flusher.Flush()
		for _, frame := range frames {
			fmt.Fprint(w, "data: "+frame+"\n\n")
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	t.Cleanup(server.Close)
	return server
}

// TestASplitUsageFrameKeepsBothHalves is the defect itself: an endpoint that
// reports its counts when the answer ends and its price one frame later must
// end the call holding both, and holding the thinking pass it already named.
func TestASplitUsageFrameKeepsBothHalves(t *testing.T) {
	read := loggingTo(t)
	server := streamingUsageServer(t,
		`{"id":"one","provider":"gusher","choices":[{"index":0,"delta":{"content":"the answer"}}]}`,
		`{"id":"one","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10,`+
			`"completion_tokens_details":{"reasoning_tokens":2}}}`,
		`{"id":"one","choices":[],"usage":{"cost":0.25}}`,
	)

	client, err := NewClient(Config{APIKey: "k", BaseURL: server.URL, Model: "sim/model"})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	ctx := WithCallTag(WithStreamObserver(context.Background(), func(StreamEvent) {}), "turn")
	response, err := client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}

	usage := response.Usage
	if usage == nil {
		t.Fatal("a call that saw two usage frames must not end with an unknown usage")
	}
	if usage.PromptTokens != 7 || usage.CompletionTokens != 3 || usage.TotalTokens != 10 {
		t.Errorf("the price frame zeroed the counts the frame before it carried: %+v", usage)
	}
	if usage.Cost == nil || *usage.Cost != 0.25 {
		t.Errorf("the call should carry the price the second frame reported: %+v", usage)
	}

	done := ended(read())
	if len(done) != 1 {
		t.Fatalf("one stream is one finished row; got %d: %+v", len(done), done)
	}
	row := done[0]
	// The reasoning count rides beside the usage block and follows the same law:
	// a frame that does not break the output down erases nothing.
	if row.ReasoningTokens != 2 {
		t.Errorf("the thinking pass should survive a later frame: %+v", row)
	}
	if row.PromptTokens != 7 || row.CompletionTokens != 3 || row.Cost != 0.25 {
		t.Errorf("the row should be the whole of what the wire reported: %+v", row)
	}
}

// TestOneTerminalUsageFrameIsUnchanged pins the shape every provider aforge
// drives today actually sends, so the merge cannot buy the split frame at the
// price of the common one.
func TestOneTerminalUsageFrameIsUnchanged(t *testing.T) {
	read := loggingTo(t)
	server := streamingUsageServer(t,
		`{"id":"one","provider":"gusher","choices":[{"index":0,"delta":{"content":"the answer"}}]}`,
		`{"id":"one","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10,"cost":0.25,`+
			`"prompt_tokens_details":{"cached_tokens":4},`+
			`"completion_tokens_details":{"reasoning_tokens":2}}}`,
	)

	client, err := NewClient(Config{APIKey: "k", BaseURL: server.URL, Model: "sim/model"})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	ctx := WithCallTag(WithStreamObserver(context.Background(), func(StreamEvent) {}), "turn")
	response, err := client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}

	usage := response.Usage
	if usage == nil {
		t.Fatal("a terminal usage frame must reach the response")
	}
	if usage.PromptTokens != 7 || usage.CompletionTokens != 3 || usage.TotalTokens != 10 {
		t.Errorf("the counts of the one frame: %+v", usage)
	}
	if usage.Cost == nil || *usage.Cost != 0.25 || usage.CacheReadTokens() != 4 {
		t.Errorf("the price and the cache read of the one frame: %+v", usage)
	}
	done := ended(read())
	if len(done) != 1 || done[0].ReasoningTokens != 2 || done[0].CachedTokens != 4 {
		t.Errorf("the row should carry the whole of the one frame: %+v", done)
	}
}

// TestACallWithNoUsageFrameEndsUnknown holds the other half of the law: nothing
// reported is "unknown", which is nil, and never a block full of zeroes.
func TestACallWithNoUsageFrameEndsUnknown(t *testing.T) {
	server := streamingUsageServer(t,
		`{"id":"one","provider":"gusher","choices":[{"index":0,"delta":{"content":"the answer"}}]}`,
		`{"id":"one","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	)
	client, err := NewClient(Config{APIKey: "k", BaseURL: server.URL, Model: "sim/model"})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	response, err := client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if response.Usage != nil {
		t.Errorf("a call nobody reported usage for is unknown, not zero: %+v", response.Usage)
	}
}

// TestMergeUsageFoldsFramesFieldByField is the helper on its own: what a frame
// states wins, what it is silent about survives, and a frame that says nothing
// at all changes nothing.
func TestMergeUsageFoldsFramesFieldByField(t *testing.T) {
	cost := func(value float64) *float64 { return &value }

	counts := &ai.Usage{
		PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10,
		CacheReadInputTokens: 4, CacheCreationInputTokens: 5,
		PromptTokensDetails: &ai.PromptTokensDetails{CachedTokens: 6},
	}

	// A first frame onto nothing is that frame.
	first := mergeUsage(nil, counts)
	if first == nil || first.PromptTokens != 7 || first.CacheCreationInputTokens != 5 {
		t.Fatalf("the first frame should land whole: %+v", first)
	}

	// A price-only frame states the price and is silent about everything else.
	merged := mergeUsage(first, &ai.Usage{Cost: cost(0.25)})
	if merged.PromptTokens != 7 || merged.CompletionTokens != 3 || merged.TotalTokens != 10 {
		t.Errorf("a silent field must keep what was there: %+v", merged)
	}
	if merged.CacheReadInputTokens != 4 || merged.CacheCreationInputTokens != 5 {
		t.Errorf("the cache counts must survive too: %+v", merged)
	}
	if merged.PromptTokensDetails == nil || merged.PromptTokensDetails.CachedTokens != 6 {
		t.Errorf("the nested cache count must survive too: %+v", merged.PromptTokensDetails)
	}
	if merged.Cost == nil || *merged.Cost != 0.25 {
		t.Errorf("the price the frame stated should land: %+v", merged)
	}

	// A frame that restates a figure wins, because each frame carries the
	// running total for the call rather than an increment.
	restated := mergeUsage(merged, &ai.Usage{CompletionTokens: 9})
	if restated.CompletionTokens != 9 || restated.PromptTokens != 7 {
		t.Errorf("a restated total should replace, not add: %+v", restated)
	}
	// And a price already reported is not zeroed by a frame that omits it.
	if restated.Cost == nil || *restated.Cost != 0.25 {
		t.Errorf("the price must survive a later frame: %+v", restated)
	}

	// Nothing reported stays nothing.
	if mergeUsage(nil, nil) != nil {
		t.Error("no frame at all is unknown, which is nil")
	}
	if kept := mergeUsage(restated, nil); kept != restated {
		t.Errorf("a frame with no usage block changes nothing: %+v", kept)
	}
}

// TestUsageWireMergeIntoCarriesTheReasoningCount is the wire half of the helper:
// the one figure ai.Usage has no field for follows the same law.
func TestUsageWireMergeIntoCarriesTheReasoningCount(t *testing.T) {
	thinking := &usageWire{}
	thinking.PromptTokens = 7
	thinking.CompletionTokensDetails.ReasoningTokens = 2

	block, reasoning := thinking.mergeInto(nil, 0)
	if block == nil || block.PromptTokens != 7 || reasoning != 2 {
		t.Fatalf("the first frame should land whole: %+v, reasoning %d", block, reasoning)
	}

	priced := &usageWire{}
	priced.Cost = new(float64)
	*priced.Cost = 0.25
	block, reasoning = priced.mergeInto(block, reasoning)
	if block.PromptTokens != 7 || reasoning != 2 {
		t.Errorf("a price-only frame must erase neither the counts nor the thinking pass: %+v, reasoning %d", block, reasoning)
	}
	if block.Cost == nil || *block.Cost != 0.25 {
		t.Errorf("the price should land: %+v", block)
	}

	// A nil frame — no usage block on this event at all — changes nothing.
	var absent *usageWire
	same, kept := absent.mergeInto(block, reasoning)
	if same != block || kept != 2 {
		t.Errorf("an event with no usage block changes nothing: %+v, reasoning %d", same, kept)
	}
}
