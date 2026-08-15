package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// spend puts a session's accumulated cost where the rail reads it, the way a
// turn's own accounting would.
func spend(a *Agent, usd float64) {
	a.mu.Lock()
	a.usage.CostUSD = usd
	a.mu.Unlock()
}

// Over the line, the turn is refused and NOTHING happens: no request, no
// journaled message. The refusal names the sentinel so a surface can say what
// it is instead of matching on words.
func TestSpendRailRefusesTheTurnAndDoesNoWork(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			t.Error("a refused turn reached the provider")
			return textResponse("should not happen"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SpendRailUSD = 2 })
	spend(agent, 2.50)

	events := collect(t, mustSubmit(t, agent, "keep going"))

	if len(events) != 1 || events[0].Kind != EventError {
		t.Fatalf("events = %v, want one EventError", kinds(events))
	}
	if !errors.Is(events[0].Err, ErrSpendRail) {
		t.Fatalf("error = %v, want it to wrap ErrSpendRail", events[0].Err)
	}
	if !strings.Contains(events[0].Err.Error(), "$2.50") {
		t.Fatalf("error = %v, want it to say what was spent", events[0].Err)
	}
	if completer.requests() != 0 {
		t.Fatalf("requests = %d, want 0", completer.requests())
	}
	if got := transcriptRoles(agent); len(got) != 1 || got[0] != "system" {
		t.Fatalf("transcript = %v, want a refused turn to record nothing", got)
	}
}

// Under the line the rail is not there.
func TestSpendRailUnderTheLineRunsTheTurn(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("carried on"), nil },
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SpendRailUSD = 2 })
	spend(agent, 1.99)

	events := collect(t, mustSubmit(t, agent, "keep going"))

	if last := events[len(events)-1]; last.Kind != EventTurnDone {
		t.Fatalf("turn ended with %v, want EventTurnDone", last.Kind)
	}
	if got := messageText(lastMessage(agent)); got != "carried on" {
		t.Fatalf("answer = %q", got)
	}
}

// Zero is off, whatever the session has spent.
func TestSpendRailOffByDefault(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("no ceiling here"), nil },
	}}
	agent, _ := newTestAgent(t, completer, nil)
	spend(agent, 9000)

	events := collect(t, mustSubmit(t, agent, "keep going"))

	if last := events[len(events)-1]; last.Kind != EventTurnDone {
		t.Fatalf("turn ended with %v, want EventTurnDone", last.Kind)
	}
}

// The rail stops the NEXT turn, never the one in flight: a session killed
// between an assistant's tool_calls and their results is a transcript no
// provider will take back.
func TestSpendRailNeverCutsTheTurnInFlight(t *testing.T) {
	runs := make(chan string, 2)
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "touch", "{}"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("finished anyway"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SpendRailUSD = 1 })
	agent.tools = append(agent.tools, countingTool("touch", runs, nil))

	events := mustSubmit(t, agent, "start the work")
	// The rail is crossed while the turn is mid-batch.
	spend(agent, 5)
	collected := collect(t, events)

	if last := collected[len(collected)-1]; last.Kind != EventTurnDone {
		t.Fatalf("turn ended with %v, want it to finish", last.Kind)
	}
	if len(runs) != 1 {
		t.Fatalf("the tool ran %d times, want 1 — the batch was cut short", len(runs))
	}
	// And the next turn is the one that is refused.
	next := collect(t, mustSubmit(t, agent, "and again"))
	if len(next) != 1 || !errors.Is(next[0].Err, ErrSpendRail) {
		t.Fatalf("the next turn = %v, want the rail's refusal", kinds(next))
	}
}
