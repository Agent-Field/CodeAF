//go:build e2e

package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/home"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
)

// TestRealSheetFitsTheFirstMessageThatWasRefused replays, against the router's
// real endpoints page, the request refused on 2026-09-28: deepseek-v3.2 at
// xhigh, the belt's tools, and about 19,370 tokens of first message and system
// page. The page is public, so no key is needed and nothing is sent to a model:
// the refusal was local, and the encode is the whole of what is being asked.
//
// It asserts the window the request is measured against is one a tool-carrying
// request can reach, and that the full xhigh budget travels.
func TestRealSheetFitsTheFirstMessageThatWasRefused(t *testing.T) {
	const model = "deepseek/deepseek-v3.2"
	t.Setenv(home.EnvVar, t.TempDir())
	lanes.Default().Reset()
	t.Cleanup(lanes.Default().Reset)
	client, err := NewClient(Config{APIKey: "sk-no-key-needed-for-the-endpoints-page", BaseURL: "https://openrouter.ai/api/v1", Model: model})
	if err != nil {
		t.Fatal(err)
	}
	if err := lanes.Default().Sheet().Refresh(context.Background(), model); err != nil {
		t.Skipf("the router's endpoints page could not be read: %v", err)
	}
	rows := lanes.Default().Sheet().Rows(model)
	smallest, smallestWithTools := 0, 0
	for _, row := range rows {
		t.Logf("%-14s context %7d tools %v", row.ID.Lane, row.Facts.Context, row.Facts.Tools)
		if c := row.Facts.Context; c > 0 && (smallest == 0 || c < smallest) {
			smallest = c
		}
		if c := row.Facts.Context; c > 0 && row.Facts.Tools && (smallestWithTools == 0 || c < smallestWithTools) {
			smallestWithTools = c
		}
	}
	window := client.servingWindow(model, nil, 163840, true)
	t.Logf("smallest window %d, smallest taking tools %d, measured against %d", smallest, smallestWithTools, window)
	if window != min(163840, smallestWithTools) {
		t.Fatalf("a tool-carrying request was measured against %d, want the smallest tool endpoint's %d", window, smallestWithTools)
	}

	request := &ai.Request{
		Model:    model,
		Messages: userMessages(strings.Repeat("x", 19370*4)),
		Tools:    []ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "read", Parameters: map[string]any{"type": "object"}}}},
	}
	knobs := callKnobs{
		contextBudget: ContextBudget{Window: 163840, Reserve: 16384},
		effort:        effortRequest{effort: EffortHigh, budget: xhighReasoningTokens, explicit: true},
	}
	body, err := client.encodeRequest(request, knobs)
	if err != nil {
		t.Fatalf("the refused first message is still refused: %v", err)
	}
	var wire struct {
		MaxTokens int `json:"max_tokens"`
		Reasoning struct {
			MaxTokens int `json:"max_tokens"`
		} `json:"reasoning"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	t.Logf("sent max_tokens %d, reasoning.max_tokens %d", wire.MaxTokens, wire.Reasoning.MaxTokens)
	if wire.Reasoning.MaxTokens != xhighReasoningTokens {
		t.Fatalf("thinking budget %d, want the whole xhigh %d on a window this large", wire.Reasoning.MaxTokens, xhighReasoningTokens)
	}
}
