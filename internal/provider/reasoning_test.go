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

// A /model switch mid-conversation. The transcript carries the old model's
// working, and OpenRouter answers a replay of it to anyone else with a 404 —
// "encrypted payloads can only be replayed to the endpoint that created them".
// The words go to the new model; the thinking stays home.
func TestAnotherModelsReasoningStaysHome(t *testing.T) {
	client, recorded := newCachingClient(t, "http://provider.test", "reasoning/replay")
	own := WithMessageReasoning(context.Background(), []MessageReasoning{
		{}, {Field: "reasoning_content", Text: "mine", Model: "reasoning/replay"}, {},
	})
	foreign := WithMessageReasoning(context.Background(), []MessageReasoning{
		{}, {Field: "reasoning_content", Text: "theirs", Model: "other/model"}, {},
	})
	for _, ctx := range []context.Context{own, foreign} {
		if _, err := client.CompleteWithMessages(ctx, reasoningTranscript()); err != nil {
			t.Fatal(err)
		}
	}
	if got := wireMessages(t, recorded.body(0))[1]["reasoning_content"]; got != "mine" {
		t.Fatalf("own working = %#v, want it replayed", got)
	}
	theirs := wireMessages(t, recorded.body(1))[1]
	if _, present := theirs["reasoning_content"]; present {
		t.Fatalf("another model's working travelled: %#v", theirs)
	}
	if got := theirs["content"]; got != "looking" {
		t.Fatalf("the message itself must still go, got %#v", got)
	}
}

// The journal written before the sidecar carried a model has no tag to filter
// on, so the first request after a switch is refused. It is repaired in flight
// and NOT remembered: this model does replay — just not somebody else's.
func TestAModelSwitchIsRepairedWithoutTeachingTheMemoAnything(t *testing.T) {
	const model = "reasoning/switched-to"
	quirksAt(t, model)
	recorded := &capture{}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		messages := wireMessages(t, recorded.body(recorded.count()-1))
		if _, present := messages[1]["reasoning_content"]; present {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(`{"error":{"message":"Your request contains encrypted reasoning or compaction content that was produced under a different model. Encrypted payloads can only be replayed to the endpoint that created them.","code":404}}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	})
	client, err := NewClient(Config{APIKey: "k", BaseURL: "http://provider.test", Model: model, HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	untagged := WithMessageReasoning(context.Background(), []MessageReasoning{{}, {Field: "reasoning_content", Text: "carry"}})
	response, err := client.CompleteWithMessages(untagged, reasoningTranscript()[:2])
	if err != nil {
		t.Fatalf("a switch must be repaired, not returned: %v", err)
	}
	if got := response.Text(); got != "ok" {
		t.Fatalf("answer = %q", got)
	}
	if recorded.count() != 2 {
		t.Fatalf("requests = %d, want the refusal and its repair", recorded.count())
	}
	if _, present := wireMessages(t, recorded.body(1))[1]["reasoning_content"]; present {
		t.Fatalf("the repair still carried the other model's working: %#v", recorded.body(1))
	}
	if reasoningReplayRefused(model) {
		t.Fatal("a refusal about ANOTHER model's working must not be learned as this model refusing replay")
	}
}
