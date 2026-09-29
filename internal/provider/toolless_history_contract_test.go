package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestToollessRequestCarriesToolHistoryAsReadableText(t *testing.T) {
	history := []ai.Message{
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "read the file"}}},
		{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "I will look."}}, ToolCalls: []ai.ToolCall{{ID: "call_1", Type: "function", Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"notes.txt"}`}}}},
		{Role: "tool", ToolCallID: "call_1", Content: []ai.ContentPart{{Type: "text", Text: "The answer is 42."}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "What did it say?"}}},
	}
	tools := ai.WithTools([]ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "read"}}})
	client, recorded := newTestClient(t, Config{Model: "review/chat", SupportsParameter: func(model, parameter string) (bool, bool) {
		return model != "review/chat" || parameter != "tools", true
	}})
	if _, err := client.CompleteWithMessages(context.Background(), history, tools); err != nil {
		t.Fatal(err)
	}
	if len(recorded.raw) != 1 {
		t.Fatalf("requests = %d, want one", len(recorded.raw))
	}
	got := string(recorded.raw[0])
	for _, absent := range []string{`"tool_calls"`, `"role":"tool"`, `"tool_call_id"`} {
		if strings.Contains(got, absent) {
			t.Fatalf("tool-less wire still carries %s: %s", absent, got)
		}
	}
	for _, want := range []string{"I will look.", "read", "notes.txt", "The answer is 42."} {
		if !strings.Contains(got, want) {
			t.Fatalf("tool-less wire lost %q: %s", want, got)
		}
	}
	client.config.Model = "review/tools"
	before, err := json.Marshal(sanitizeMessages(history))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(context.Background(), history, tools); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Messages json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(recorded.raw[1], &body); err != nil {
		t.Fatal(err)
	}
	if got := string(body.Messages); got != string(before) {
		t.Fatalf("tools-model history bytes changed:\n%s\nwant:\n%s", got, before)
	}
}

func TestLearnedAndRelaxedToollessRequestsFlattenEarlierCalls(t *testing.T) {
	for _, mode := range []string{"learned", "relaxed"} {
		t.Run(mode, func(t *testing.T) {
			client, err := NewClient(Config{BaseURL: "http://history.test", Model: "review/unknown", Direct: true})
			if err != nil {
				t.Fatal(err)
			}
			knobs := callKnobs{}
			if mode == "learned" {
				client.toolless.learn("review/unknown")
			} else {
				knobs.relaxed = relaxTools
			}
			arguments := strings.Repeat("x", toolHistoryArgumentLimit+200)
			request := &ai.Request{Model: "review/unknown", Tools: []ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "read"}}}, Messages: []ai.Message{
				{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "c1", Function: ai.ToolCallFunction{Name: "read", Arguments: arguments}}}},
				{Role: "tool", ToolCallID: "c1", Content: []ai.ContentPart{{Type: "text", Text: "complete result"}}},
			}}
			if mode == "learned" {
				request.Tools = nil
			}
			body, err := client.encodeRequest(request, knobs)
			if err != nil {
				t.Fatal(err)
			}
			got := string(body)
			if strings.Contains(got, `"tool_calls"`) || strings.Contains(got, `"role":"tool"`) || strings.Contains(got, arguments) || !strings.Contains(got, "complete result") {
				t.Fatalf("%s history was not rendered or clipped: %s", mode, got)
			}
		})
	}
}
