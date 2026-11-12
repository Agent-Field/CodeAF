package session

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

func TestSubmitReplaysWaitingAutomationWithoutReplyText(t *testing.T) {
	for _, pictures := range []bool{false, true} {
		name := "text"
		if pictures {
			name = "picture"
		}
		t.Run(name, func(t *testing.T) { testSubmitWaitingAutomation(t, pictures) })
	}
}

func testSubmitWaitingAutomation(t *testing.T, pictures bool) {
	store := automationStoreFor(t)
	call := automationCall("reminder", map[string]any{"op": "propose", "title": "Read the programme", "when": map[string]any{"in": "2m"}, "say": "Programme time"})
	agent := automationAgent(t, &scriptedCompleter{steps: []step{
		func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "I can remind you.")
			return call(ctx, messages)
		},
		finalText("Done."),
	}}, store, func(config *Config) { config.SupportsImages = func(string) bool { return true } })
	first, err := agent.Submit(context.Background(), "Remind me in two minutes")
	if err != nil {
		t.Fatal(err)
	}
	var original Event
	deadline := time.After(3 * time.Second)
	for original.Kind != EventAutomationProposal {
		select {
		case original = <-first:
		case <-deadline:
			t.Fatal("initial reminder card never arrived")
		}
	}
	defer agent.ResolveAutomation(original.Automation.ID, AutomationAnswer{})
	var late <-chan Event
	if pictures {
		late, err = agent.SubmitImage(context.Background(), "Keep the same reminder", []Image{{Path: "programme.png", MIME: "image/png", Bytes: []byte("programme")}})
	} else {
		late, err = agent.Submit(context.Background(), "Keep the same reminder")
	}
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-late:
		if event.Kind != EventAutomationProposal || event.Automation == nil || event.Automation.ID != original.Automation.ID {
			t.Fatalf("late subscriber got %+v; want the waiting reminder, without prior reply text", event)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("late subscriber missed the already waiting automation card")
	}
	agent.ResolveAutomation(original.Automation.ID, AutomationAnswer{})
	collect(t, first)
	collect(t, late)
}
