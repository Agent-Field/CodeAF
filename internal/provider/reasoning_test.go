package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func reasoningTranscript() []ai.Message {
	return []ai.Message{
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "inspect it"}}},
		{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "looking"}}, ToolCalls: []ai.ToolCall{{
			ID: "call-1", Type: "function", Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"a.go"}`},
		}}},
		{Role: "tool", ToolCallID: "call-1", Content: []ai.ContentPart{{Type: "text", Text: "package a"}}},
	}
}

func TestAssistantReasoningFieldsRideTheMessageWithoutChangingItsContent(t *testing.T) {
	client, recorded := newCachingClient(t, "http://provider.test", "reasoning/replay")
	details := json.RawMessage(`[{"type":"reasoning.summary","data":{"scale":1e3}}]`)
	ctx := WithMessageReasoning(context.Background(), []MessageReasoning{
		{}, {Field: "reasoning_content", Text: "the package is small", Details: details}, {},
	})
	if _, err := client.CompleteWithMessages(ctx, reasoningTranscript()); err != nil {
		t.Fatal(err)
	}

	messages := wireMessages(t, recorded.body(0))
	if got := messages[1]["reasoning_content"]; got != "the package is small" {
		t.Fatalf("reasoning_content = %#v", got)
	}
	if _, present := messages[1]["reasoning"]; present {
		t.Fatalf("reasoning changed wire field: %#v", messages[1])
	}
	if got := messages[1]["content"]; got != "looking" {
		t.Fatalf("answer content = %#v, want it untouched", got)
	}
	recorded.mu.Lock()
	raw := append([]byte(nil), recorded.raw[0]...)
	recorded.mu.Unlock()
	want := []byte(`"reasoning_details":[{"type":"reasoning.summary","data":{"scale":1e3}}]`)
	if !bytes.Contains(raw, want) {
		t.Fatalf("reasoning_details was rewritten or lost:\n%s", raw)
	}
}

func TestAReasoningReplayRefusalIsLearnedForOnlyThatModel(t *testing.T) {
	const model = "reasoning/refuses-replay"
	quirksAt(t, model)
	recorded := &capture{}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		messages := wireMessages(t, recorded.body(recorded.count()-1))
		if _, present := messages[1]["reasoning_content"]; present {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":{"message":"reasoning_content is an unsupported extra field"}}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	})
	client, err := NewClient(Config{APIKey: "k", BaseURL: "http://provider.test", Model: model, HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithMessageReasoning(context.Background(), []MessageReasoning{{}, {Field: "reasoning_content", Text: "carry"}})
	if _, err := client.CompleteWithMessages(ctx, reasoningTranscript()[:2]); err != nil {
		t.Fatalf("refusal was not repaired: %v", err)
	}
	if recorded.count() != 2 {
		t.Fatalf("requests = %d, want refusal and repair", recorded.count())
	}
	if !reasoningReplayRefused(model) {
		t.Fatal("the refusing model did not retain its learned quirk")
	}
	if reasoningReplayRefused("reasoning/another-model") {
		t.Fatal("one model's refusal disabled reasoning replay for another")
	}
	if messages := wireMessages(t, recorded.body(1)); len(messages) > 1 {
		if _, present := messages[1]["reasoning_content"]; present {
			t.Fatalf("repaired request retained refused field: %#v", messages[1])
		}
	}
}
