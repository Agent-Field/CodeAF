package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A useful observation does not need a prose preface to reach the model. This
// crosses the former advice, hold, and stop boundaries through the real door.
func TestQuietUsefulToolCallsProceedWithoutProgressNotes(t *testing.T) {
	const rounds = 18
	var ran atomic.Int64
	tool := bare.Tool{Name: "look", Description: "returns a new observation", Schema: json.RawMessage(`{"type":"object","properties":{"item":{"type":"integer"}}}`), Execute: func(context.Context, json.RawMessage) (string, bool, error) {
		return fmt.Sprintf("observation %d", ran.Add(1)), false, nil
	}}
	var steps []step
	for i := 0; i < rounds; i++ {
		i := i
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(fmt.Sprintf("look-%d", i), "look", fmt.Sprintf(`{"item":%d}`, i)), nil
		})
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil })
	agent := loopAgent(t, &scriptedCompleter{steps: steps}, tool)
	events, err := agent.Submit(context.Background(), "inspect these observations")
	if err != nil {
		t.Fatal(err)
	}
	collected := collect(t, events)
	if got := ran.Load(); got != rounds {
		t.Fatalf("executed %d useful calls, want all %d", got, rounds)
	}
	for _, event := range collected {
		if event.Kind == EventNudge || event.HarnessMade {
			t.Fatalf("quiet useful work was interrupted: %+v", event)
		}
	}
	for _, message := range agent.snapshot() {
		text := messageContentText(message)
		if strings.HasPrefix(text, "[silent]") || strings.HasPrefix(text, "[held]") {
			t.Fatalf("mandatory note injected: %s", text)
		}
	}
	if got := lastSaid(agent); got != "done" {
		t.Fatalf("final answer=%q", got)
	}
}

// Optional narration remains part of the assistant's actual conversation; the
// removal only changes whether the next tool batch is allowed to run.
func TestOptionalProgressNotesRemainInTheConversation(t *testing.T) {
	const note = "The first observation is ready; checking the second."
	agent := loopAgent(t, &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponseWithText("first", "look", `{}`, note), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}, freshTool("look"))
	events, err := agent.Submit(context.Background(), "inspect this")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	for _, message := range agent.snapshot() {
		if message.Role == "assistant" && messageContentText(message) == note {
			return
		}
	}
	t.Fatal("optional progress note disappeared from assistant history")
}
