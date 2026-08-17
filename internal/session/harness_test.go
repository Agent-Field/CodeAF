package session

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The registry these tests offer from, and a turn that clears the threshold
// against it (internal/subharness's own tests own the scoring).
var researchEntry = subharness.Entry{
	Name:        "research",
	Description: "Research a question across sources and write a report",
	Cues:        []string{"research", "find out", "dig into"},
	Revision:    1,
}

const harnessTurn = "research this and find out what our sources say"

// harnessAgent builds an agent with a registry, a runner that records what it
// was asked, and a surface that says it is watching.
func harnessAgent(t *testing.T, completer Completer, run func(name, text, model string) (string, error)) (*Agent, *int32) {
	t.Helper()
	var ran int32
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.Harnesses = []subharness.Entry{researchEntry}
		// A catalog, so the model a turn names is resolved against something —
		// the session's own seam for it, exactly as a task's `model` is
		// (taskmodel.go).
		config.TaskModels = func() []string { return testModels }
		config.RunHarness = func(_ context.Context, name, text, model string) (string, error) {
			atomic.AddInt32(&ran, 1)
			return run(name, text, model)
		}
	})
	return agent, &ran
}

// drainAnsweringHarness drains one turn, answering every offer as it arrives.
func drainAnsweringHarness(t *testing.T, agent *Agent, events <-chan Event, run bool) []Event {
	t.Helper()
	var collected []Event
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				return collected
			}
			collected = append(collected, event)
			if event.Kind == EventHarnessOffer {
				agent.ResolveHarness(event.ID, run, "")
			}
		case <-deadline:
			t.Fatalf("the turn never finished; events so far: %v", kinds(collected))
			return nil
		}
	}
}

// A YES: the harness takes the turn, the model is never asked, and the report
// is the turn's answer.
func TestHarnessOfferAccepted(t *testing.T) {
	completer := &scriptedCompleter{}
	var gotName, gotText string
	agent, ran := harnessAgent(t, completer, func(name, text, _ string) (string, error) {
		gotName, gotText = name, text
		return "the report", nil
	})

	events, err := agent.Submit(context.Background(), harnessTurn)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := drainAnsweringHarness(t, agent, events, true)

	offer, ok := firstOfKind(collected, EventHarnessOffer)
	if !ok {
		t.Fatalf("no offer was raised; events: %v", kinds(collected))
	}
	if offer.Text != "research" || offer.Hint != researchEntry.Description {
		t.Fatalf("the offer named %q / %q", offer.Text, offer.Hint)
	}
	if _, ok := firstOfKind(collected, EventHarnessRun); !ok {
		t.Fatalf("the run was never announced; events: %v", kinds(collected))
	}
	if atomic.LoadInt32(ran) != 1 {
		t.Fatalf("the harness ran %d times", atomic.LoadInt32(ran))
	}
	if gotName != "research" || gotText != harnessTurn {
		t.Fatalf("the runner was handed (%q, %q)", gotName, gotText)
	}
	// THE MODEL WAS NEVER ASKED. That is what "the harness took the turn" means,
	// and a provider request here would be the person paying for both.
	if completer.requests() != 0 {
		t.Fatalf("the provider was called %d times", completer.requests())
	}
	// The report is the turn's answer: on screen, and in the transcript, so the
	// next turn knows what was said.
	text, ok := firstOfKind(collected, EventTextDelta)
	if !ok || text.Text != "the report" {
		t.Fatalf("the report did not reach the surface: %v", kinds(collected))
	}
	last := lastMessage(agent)
	if last.Role != "assistant" || messageText(last) != "the report" {
		t.Fatalf("the transcript ends %q / %q", last.Role, messageText(last))
	}
	if _, ok := firstOfKind(collected, EventTurnDone); !ok {
		t.Fatalf("the turn never ended: %v", kinds(collected))
	}
}

// A NO: the ordinary turn, unchanged. Nothing ran, nothing extra was recorded,
// and the model was sent exactly what it would have been sent.
func TestHarnessOfferDeclinedLeavesTheTurnAlone(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the ordinary answer"), nil
		},
	}}
	agent, ran := harnessAgent(t, completer, func(string, string, string) (string, error) {
		t.Error("the harness ran on a no")
		return "", nil
	})

	events, err := agent.Submit(context.Background(), harnessTurn)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := drainAnsweringHarness(t, agent, events, false)

	if _, ok := firstOfKind(collected, EventHarnessOffer); !ok {
		t.Fatalf("no offer was raised; events: %v", kinds(collected))
	}
	if _, ok := firstOfKind(collected, EventHarnessRun); ok {
		t.Fatal("a declined offer announced a run")
	}
	if atomic.LoadInt32(ran) != 0 {
		t.Fatal("a declined offer ran the harness")
	}
	if completer.requests() != 1 {
		t.Fatalf("the provider was called %d times, want the one ordinary turn", completer.requests())
	}
	// The transcript is the ordinary turn's: the person's message, the model's
	// answer, and nothing about a question that was answered no.
	last := lastMessage(agent)
	if messageText(last) != "the ordinary answer" {
		t.Fatalf("the transcript ends %q", messageText(last))
	}
	for _, message := range agent.snapshot() {
		if strings.Contains(messageText(message), "harness") {
			t.Fatalf("the refusal left a mark in the transcript: %q", messageText(message))
		}
	}
}

// THE PURE LOOP: no registry, no runner, nobody watching. Every existing caller
// is this case, and the turn is byte for byte the turn it was before detection
// existed — no offer, one request, the model's answer.
func TestHarnessSilentWhenUnwired(t *testing.T) {
	cases := []struct {
		name  string
		wire  func(*Config)
		asked bool
	}{
		{"nothing wired", func(*Config) {}, false},
		{"registry but no runner", func(c *Config) {
			c.AskConsent = true
			c.Harnesses = []subharness.Entry{researchEntry}
		}, false},
		{"runner but no registry", func(c *Config) {
			c.AskConsent = true
			c.RunHarness = func(context.Context, string, string, string) (string, error) { return "", nil }
		}, false},
		{"wired, but nobody is watching", func(c *Config) {
			c.Harnesses = []subharness.Entry{researchEntry}
			c.RunHarness = func(context.Context, string, string, string) (string, error) {
				return "", errors.New("a headless run must never be asked")
			}
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			completer := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return textResponse("the ordinary answer"), nil
				},
			}}
			agent, _ := newTestAgent(t, completer, c.wire)
			events, err := agent.Submit(context.Background(), harnessTurn)
			if err != nil {
				t.Fatalf("submit: %v", err)
			}
			collected := collect(t, events)
			if _, ok := firstOfKind(collected, EventHarnessOffer); ok != c.asked {
				t.Fatalf("offer raised=%v, want %v; events: %v", ok, c.asked, kinds(collected))
			}
			if completer.requests() != 1 {
				t.Fatalf("the provider was called %d times", completer.requests())
			}
			if messageText(lastMessage(agent)) != "the ordinary answer" {
				t.Fatalf("the transcript ends %q", messageText(lastMessage(agent)))
			}
		})
	}
}

// A turn that is nobody's harness is never asked about, however wired the
// session is. The threshold is internal/subharness's; this is that it is the
// thing consulted.
func TestHarnessQuietUnderTheThreshold(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the ordinary answer"), nil
		},
	}}
	agent, ran := harnessAgent(t, completer, func(string, string, string) (string, error) {
		t.Error("an ordinary turn ran a harness")
		return "", nil
	})

	events, err := agent.Submit(context.Background(), "rename the config loader and run the tests")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := collect(t, events)
	if _, ok := firstOfKind(collected, EventHarnessOffer); ok {
		t.Fatalf("an ordinary turn was asked about; events: %v", kinds(collected))
	}
	if atomic.LoadInt32(ran) != 0 {
		t.Fatal("an ordinary turn ran a harness")
	}
	if completer.requests() != 1 {
		t.Fatalf("the provider was called %d times", completer.requests())
	}
}

// ONLY WHAT A PERSON TYPED. A woken turn opens with an empty message and reads
// its note off the steering queue; a note the session authored is not somebody
// asking for a harness, whatever words it happens to contain.
func TestHarnessMatchesOnlyThePersonsOwnWords(t *testing.T) {
	agent, _ := harnessAgent(t, &scriptedCompleter{}, func(string, string, string) (string, error) {
		return "", nil
	})
	cases := []struct {
		name  string
		user  userMessage
		match bool
	}{
		{"typed", userText(harnessTurn), true},
		{"a wake note", wakeNote(harnessTurn), false},
		{"a note the session wrote", userMessage{
			message: textMessage("user", harnessTurn), authored: true,
		}, false},
		{"the woken turn's empty opening", userMessage{}, false},
	}
	for _, c := range cases {
		if _, ok := agent.harnessMatch(c.user); ok != c.match {
			t.Errorf("%s matched=%v, want %v", c.name, ok, c.match)
		}
	}
}

// A run that fails ends the turn with the reason, and records no answer: the
// person sees the error rather than a turn that quietly said nothing.
func TestHarnessRunFailure(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, _ := harnessAgent(t, completer, func(string, string, string) (string, error) {
		return "", errors.New("the program has no v2")
	})

	events, err := agent.Submit(context.Background(), harnessTurn)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := drainAnsweringHarness(t, agent, events, true)

	failed, ok := firstOfKind(collected, EventError)
	if !ok {
		t.Fatalf("the failure never reached the surface: %v", kinds(collected))
	}
	if failed.Err == nil || !strings.Contains(failed.Err.Error(), "no v2") {
		t.Fatalf("the error was %v", failed.Err)
	}
	if last := lastMessage(agent); last.Role != "user" {
		t.Fatalf("a failed run recorded %q", last.Role)
	}
}

// An interrupt while the card is up ends the turn rather than leaving it
// waiting on an answer nobody is coming back to give.
func TestHarnessOfferInterrupted(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, ran := harnessAgent(t, completer, func(string, string, string) (string, error) {
		t.Error("an interrupted offer ran the harness")
		return "", nil
	})

	events, err := agent.Submit(context.Background(), harnessTurn)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	var collected []Event
	deadline := time.After(2 * time.Second)
	for open := true; open; {
		select {
		case event, ok := <-events:
			if !ok {
				open = false
				break
			}
			collected = append(collected, event)
			if event.Kind == EventHarnessOffer {
				agent.Interrupt()
			}
		case <-deadline:
			t.Fatalf("the turn never finished; events so far: %v", kinds(collected))
		}
	}
	if _, ok := firstOfKind(collected, EventTurnDone); !ok {
		t.Fatalf("the interrupted turn never ended: %v", kinds(collected))
	}
	if atomic.LoadInt32(ran) != 0 {
		t.Fatal("the harness ran after an interrupt")
	}
	if completer.requests() != 0 {
		t.Fatalf("the provider was called %d times", completer.requests())
	}
	// And the pending question is gone rather than left in the map forever.
	agent.mu.Lock()
	waiting := len(agent.harnessAsks)
	agent.mu.Unlock()
	if waiting != 0 {
		t.Fatalf("%d offers left waiting", waiting)
	}
}
