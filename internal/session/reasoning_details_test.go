package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func reasoningDetailsForTest(t *testing.T, raw json.RawMessage) []map[string]any {
	t.Helper()
	var details []map[string]any
	if err := json.Unmarshal(raw, &details); err != nil {
		t.Fatal(err)
	}
	return details
}

func TestToolLoopReplaysAssembledReasoningBlocks(t *testing.T) {
	client := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			for _, fragment := range []string{"Let", " me", " inspect"} {
				detail, _ := json.Marshal([]map[string]any{{"type": "reasoning.text", "text": fragment, "format": "unknown", "index": 0}})
				provider.EmitReasoning(ctx, "reasoning", fragment, detail)
			}
			return toolResponse("list", "ls", `{}`), nil
		},
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			for _, carried := range provider.MessageReasoningFrom(ctx) {
				if carried.Text != "Let me inspect" {
					continue
				}
				details := reasoningDetailsForTest(t, carried.Details)
				if carried.Field != "reasoning" || len(details) != 1 || details[0]["text"] != carried.Text {
					t.Fatalf("streamed reasoning was not one complete block: %#v", carried)
				}
				return textResponse("The directory is empty."), nil
			}
			t.Fatal("the next tool-loop request lost its reasoning")
			return nil, nil
		},
	}}
	agent, _ := newTestAgent(t, client, nil)
	collect(t, mustSubmit(t, agent, "inspect the directory"))
}

func TestReasoningAssemblyPreservesBlockBoundariesAndOpaqueData(t *testing.T) {
	const opaque = `{"type":"reasoning.encrypted", "data":"opaque==", "id":"encrypted", "index":2, "extra":{"scale":1e3}}`
	chunks := []string{
		`[{"type":"reasoning.text","text":"first","index":0,"id":"a","format":"unknown","extra":{"scale":1e3}}]`,
		`[{"type":"reasoning.text","text":" block","index":0,"id":"a"}]`,
		`[{"type":"reasoning.text","text":"second","index":1,"id":"b","signature":null}]`,
		`[{"type":"reasoning.text","signature":"signed","index":1,"id":"b"}]`,
		`[` + opaque + `]`,
		`[{"type":"reasoning.summary","summary":"Con","index":3,"id":"s"}]`,
		`[{"type":"reasoning.summary","summary":"clusion","index":3,"id":"s"}]`,
		`[{"type":"reasoning.text","text":"last","index":0,"id":"a"}]`,
	}
	var buffer reasoningBuffer
	buffer.begin("test/model")
	for _, chunk := range chunks {
		buffer.write(provider.StreamEvent{Kind: provider.StreamReasoning, ReasoningDetails: json.RawMessage(chunk)})
	}
	got := buffer.snapshot()
	details := reasoningDetailsForTest(t, got.Details)
	if len(details) != 5 {
		t.Fatalf("got %d details, want five logical blocks: %s", len(details), got.Details)
	}
	if details[0]["text"] != "first block" || details[1]["text"] != "second" || details[1]["signature"] != "signed" || details[3]["summary"] != "Conclusion" || details[4]["text"] != "last" {
		t.Fatalf("block text, signature, or order changed: %s", got.Details)
	}
	if !strings.Contains(string(got.Details), opaque) || !strings.Contains(string(got.Details), `"scale":1e3`) {
		t.Fatalf("opaque content or unknown numeric metadata changed: %s", got.Details)
	}
	buffer.begin("test/next")
	if reset := buffer.snapshot(); len(reset.Details) != 0 || reset.Text != "" {
		t.Fatalf("a new attempt retained prior reasoning: %#v", reset)
	}
	if len(reasoningDetailsForTest(t, got.Details)) != 5 {
		t.Fatal("reset changed a previously captured snapshot")
	}
}

func TestReasoningAssemblyKeepsConflictingIdentityAndUnknownKinds(t *testing.T) {
	chunks := []string{
		`[{"type":"reasoning.text","text":"a","index":0,"id":"first"}]`,
		`[{"type":"reasoning.text","text":"b","index":0,"id":"second"}]`,
		`[{"type":"reasoning.text","text":"c","index":1,"id":"second"}]`,
		`[{"type":"reasoning.future","data":"one","index":2}]`,
		`[{"type":"reasoning.future","data":"two","index":2}]`,
	}
	var got json.RawMessage
	for _, chunk := range chunks {
		got = joinReasoningDetails(got, json.RawMessage(chunk))
	}
	got = assembledReasoningDetails(got)
	if n := len(reasoningDetailsForTest(t, got)); n != len(chunks) {
		t.Fatalf("distinct blocks collapsed into %d: %s", n, got)
	}
}

func TestReasoningAssemblyJoinsAThousandFragmentsOnceAtSnapshot(t *testing.T) {
	var buffer reasoningBuffer
	buffer.begin("test/model")
	for range 1000 {
		buffer.write(provider.StreamEvent{Kind: provider.StreamReasoning, ReasoningField: "reasoning", Delta: "word ", ReasoningDetails: json.RawMessage(`[{"type":"reasoning.text","text":"word ","format":"unknown","index":0}]`)})
	}
	got := buffer.snapshot()
	details := reasoningDetailsForTest(t, got.Details)
	if len(details) != 1 || details[0]["text"] != strings.Repeat("word ", 1000) || details[0]["text"] != got.Text {
		t.Fatalf("fragmented replay or lost text: %d blocks", len(details))
	}
	// Snapshot assembly must not replace the stream's lossless source or mutate
	// a previously captured value when a later attempt resets its buffer.
	if len(reasoningDetailsForTest(t, buffer.reasoning.Details)) != 1000 {
		t.Fatal("snapshot rewrote the captured source fragments")
	}
}
