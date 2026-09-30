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

// A REFUSAL OF TOOLS IS PAID ONCE PER RUN for a model the catalog does not
// know: the first turn is refused and retried without them, and every later
// turn goes without them from the start, with no second line said about it.
func TestAToolsRefusalIsRememberedForTheRun(t *testing.T) {
	recorded := &capture{}
	config := attributedConfig(streamedRefusal(func(body map[string]any) bool {
		_, hasTools := body["tools"]
		return !hasTools
	}, recorded))
	config.SupportsParameter = func(string, string) (bool, bool) { return false, false }
	client, err := NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var said []string
	ctx := WithStreamObserver(context.Background(), func(event StreamEvent) {
		if event.Kind == StreamNotice || event.Kind == StreamRowNews {
			mu.Lock()
			said = append(said, event.Delta)
			mu.Unlock()
		}
	})
	tools := ai.WithTools([]ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "read"}}})
	if _, err := client.CompleteWithMessages(ctx, userMessages("first"), tools); err != nil {
		t.Fatal(err)
	}
	first := len(recorded.bodies)
	mu.Lock()
	lines := len(said)
	mu.Unlock()
	if first < 2 || lines == 0 {
		t.Fatalf("turn one: %d requests and %d lines, want a refusal, a retry and its line", first, lines)
	}
	if _, err := client.CompleteWithMessages(ctx, userMessages("second"), tools); err != nil {
		t.Fatal(err)
	}
	if got := len(recorded.bodies) - first; got != 1 {
		t.Fatalf("turn two made %d requests, want one without tools", got)
	}
	if _, hasTools := recorded.body(len(recorded.bodies) - 1)["tools"]; hasTools {
		t.Fatal("turn two carried the tools again")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(said) != lines {
		t.Fatalf("turn two said %q, want nothing new", said[lines:])
	}
}

// A body with no tools carries no tool_choice either.
func TestNoToolsMeansNoToolChoice(t *testing.T) {
	client, _ := NewClient(Config{BaseURL: "http://choice.test", Model: "choice/model", Direct: true})
	request := &ai.Request{Model: "choice/model", Messages: userMessages("hello")}
	_ = ai.WithTools(nil)(request)
	body, err := client.encodeRequest(request, callKnobs{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "tool_choice") {
		t.Fatalf("body = %s", body)
	}
}
