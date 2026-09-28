package provider

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A MODEL THE CATALOG SAYS TAKES NO TOOLS IS SENT NONE: no refused first
// attempt, no retry line, one plain notice for the model, and a size check
// that does not count definitions that never go out (2026-09-28,
// microsoft/phi-4: refused as 15.6k tokens when 6k would have been sent).
func TestAModelThatTakesNoToolsIsSentNone(t *testing.T) {
	recorded := &capture{}
	config := attributedConfig(streamedRefusal(func(body map[string]any) bool {
		_, hasTools := body["tools"]
		return !hasTools
	}, recorded))
	config.SupportsParameter = func(_, parameter string) (bool, bool) { return parameter != "tools", true }
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var notices []string
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		if event.Kind == StreamNotice || event.Kind == StreamRowNews {
			mu.Lock()
			notices = append(notices, event.Delta)
			mu.Unlock()
		}
	})
	// A schema big enough that, counted, it alone would overflow the window.
	tools := ai.WithTools([]ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{
		Name: "read", Parameters: map[string]any{"description": strings.Repeat("schema ", 6000)},
	}}})
	ctx = WithContextBudget(ctx, ContextBudget{Window: 8192, Reserve: 512})
	for turn := 1; turn <= 2; turn++ {
		response, err := client.CompleteWithMessages(ctx, userMessages("hello"), tools)
		if err != nil {
			t.Fatalf("turn %d: %v", turn, err)
		}
		if response.Text() != "ok" {
			t.Fatalf("turn %d answered %q", turn, response.Text())
		}
	}
	if len(recorded.bodies) != 2 {
		t.Fatalf("requests = %d, want one per turn and no refused attempt", len(recorded.bodies))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(notices) != 1 || notices[0] != toollessNotice("sim/model") {
		t.Fatalf("notices = %#v, want the one tool-less line once", notices)
	}
}

// A MODEL THE CATALOG DOES NOT KNOW still carries its tools, and the ladder
// says why it took them off when an endpoint refuses them.
func TestAnUnknownModelStillCarriesItsTools(t *testing.T) {
	recorded := &capture{}
	config := attributedConfig(streamedRefusal(func(map[string]any) bool { return true }, recorded))
	config.SupportsParameter = func(string, string) (bool, bool) { return false, false }
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	tools := ai.WithTools([]ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "read"}}})
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello"), tools); err != nil {
		t.Fatal(err)
	}
	if _, hasTools := recorded.body(0)["tools"]; !hasTools {
		t.Fatal("an unknown model was sent no tools")
	}
}

// A size refusal is a sentence about the request, not a failure to write JSON.
func TestASizeRefusalIsNotCalledAMarshalFailure(t *testing.T) {
	client, _ := NewClient(Config{APIKey: "test", BaseURL: "http://budget.test", Model: "budget/words", Direct: true})
	ctx := WithContextBudget(context.Background(), ContextBudget{Window: 8192, Reserve: 2048})
	_, err := client.CompleteWithMessages(ctx, userMessages(strings.Repeat("input ", 9000)))
	if err == nil || strings.Contains(err.Error(), "marshal request") {
		t.Fatalf("error = %v", err)
	}
}
