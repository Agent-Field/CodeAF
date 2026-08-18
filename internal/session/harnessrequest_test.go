package session

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// THE OTHER DOOR: a harness the person picked out of a list themselves
// (harness.go's [Agent.RunHarnessRequest]).
//
// The offer's own tests are next door in harness_test.go and they are about the
// QUESTION. These are about the road that has no question on it: what runs, on
// what words, and what the transcript ends up holding.

// drainHarnessRun collects one turn's events, failing the offer that must never
// arrive.
func drainHarnessRun(t *testing.T, events <-chan Event) []Event {
	t.Helper()
	var collected []Event
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				return collected
			}
			if event.Kind == EventHarnessOffer {
				t.Fatal("a picked harness raised an offer card")
			}
			collected = append(collected, event)
		case <-deadline:
			t.Fatalf("the turn never finished; events so far: %v", kinds(collected))
			return nil
		}
	}
}

// A PICKED HARNESS RUNS ON EXACTLY WHAT WAS TYPED, and detection is not
// consulted: the request below matches no cue this registry carries, which is
// the point — the person named the harness, so nothing scores anything.
func TestRunHarnessRequestBypassesDetection(t *testing.T) {
	completer := &scriptedCompleter{}
	var gotName, gotText, gotModel string
	agent, ran := harnessAgent(t, completer, func(name, text, model string) (string, error) {
		gotName, gotText, gotModel = name, text, model
		return "the report", nil
	})

	const request = "tidy up the changelog for the release"
	events, err := agent.RunHarnessRequest(context.Background(), "research", request, "")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	collected := drainHarnessRun(t, events)

	if atomic.LoadInt32(ran) != 1 {
		t.Fatalf("the harness ran %d times", atomic.LoadInt32(ran))
	}
	if gotName != "research" || gotText != request || gotModel != "" {
		t.Fatalf("the runner was handed (%q, %q, %q)", gotName, gotText, gotModel)
	}
	// EVERYTHING AFTER THE DECISION IS THE OFFER'S OWN PATH: the run is
	// announced, the report is the answer, and the turn ends.
	if _, ok := firstOfKind(collected, EventHarnessRun); !ok {
		t.Fatalf("the run was never announced; events: %v", kinds(collected))
	}
	if text, ok := firstOfKind(collected, EventTextDelta); !ok || text.Text != "the report" {
		t.Fatalf("the report did not reach the surface: %v", kinds(collected))
	}
	if _, ok := firstOfKind(collected, EventTurnDone); !ok {
		t.Fatalf("the turn never ended: %v", kinds(collected))
	}
	if completer.requests() != 0 {
		t.Fatalf("the provider was called %d times", completer.requests())
	}
	// The request is the person's message and the report is the answer, so the
	// next turn reads both.
	if last := lastMessage(agent); last.Role != "assistant" || messageText(last) != "the report" {
		t.Fatalf("the transcript ends %q / %q", last.Role, messageText(last))
	}
}

// THE ROUTE IS SPENT ONCE. The turn after a picked harness is an ordinary turn,
// not a second run of the same procedure.
func TestAPickedHarnessDoesNotOutliveItsTurn(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, ran := harnessAgent(t, completer, func(string, string, string) (string, error) {
		return "the report", nil
	})

	events, err := agent.RunHarnessRequest(context.Background(), "research", "look at the changelog", "")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	drainHarnessRun(t, events)

	events, err = agent.Submit(context.Background(), "and what is for lunch")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	drainAnsweringHarness(t, agent, events, false)
	if atomic.LoadInt32(ran) != 1 {
		t.Fatalf("the harness ran %d times across two turns", atomic.LoadInt32(ran))
	}
	if completer.requests() == 0 {
		t.Fatal("the second turn never reached the model")
	}
}

// A NAME NOBODY HAS IS A REFUSAL AND NOT A RUN, and it says which name.
func TestRunHarnessRequestRefusesAnUnknownName(t *testing.T) {
	agent, ran := harnessAgent(t, &scriptedCompleter{}, func(string, string, string) (string, error) {
		return "the report", nil
	})
	_, err := agent.RunHarnessRequest(context.Background(), "no-such-thing", "have a look", "")
	if err == nil || !strings.Contains(err.Error(), "no-such-thing") {
		t.Fatalf("an unknown harness answered %v", err)
	}
	if atomic.LoadInt32(ran) != 0 {
		t.Fatal("an unknown harness ran anyway")
	}
}

// AND AN EMPTY REQUEST IS ONE TOO: a run is asked to do something, and nothing
// is not a something.
func TestRunHarnessRequestRefusesAnEmptyRequest(t *testing.T) {
	agent, ran := harnessAgent(t, &scriptedCompleter{}, func(string, string, string) (string, error) {
		return "the report", nil
	})
	if _, err := agent.RunHarnessRequest(context.Background(), "research", "   ", ""); err == nil {
		t.Fatal("an empty request started a run")
	}
	if atomic.LoadInt32(ran) != 0 {
		t.Fatal("an empty request ran the harness")
	}
}

// THE NAME IS MATCHED WHATEVER CASE IT ARRIVES IN — it came off a list or out
// of somebody's fingers, and both spell the same harness.
func TestRunHarnessRequestTakesTheNameInAnyCase(t *testing.T) {
	var gotName string
	agent, _ := harnessAgent(t, &scriptedCompleter{}, func(name, _, _ string) (string, error) {
		gotName = name
		return "the report", nil
	})
	events, err := agent.RunHarnessRequest(context.Background(), "Research", "have a look", "")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	drainHarnessRun(t, events)
	if gotName != "research" {
		t.Fatalf("the runner was handed %q", gotName)
	}
}
