package session

// THE PERSON'S WORD WINS, AND IT WINS AT THE REQUEST.
//
// steer.go states the law and the run it was measured against: a task step
// thirteen minutes into a wait, the owner picking another model, and the step
// still talking to the model they had moved off a minute later. These fixtures
// are the two halves of the rule and the clock on both.
//
// EVERY ASSERTION IS ON THE WIRE. What each request carried is
// [scriptedCompleter.model] — the model id the adapter was actually handed for
// that send — never a field on the agent, because the field was right before
// this change too and the request was not.

import (
	"context"
	"strings"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// waitingRequest is one scripted request that goes out, says so, and then waits
// exactly as a request parked on a provider's pacing does: nothing on the wire,
// nothing on the screen, and no end until somebody cuts it.
//
// It is deliberately NOT a sleep. The whole subject is that a wait may be
// thirteen minutes long, so a fixture that waited a fixed time would be either
// slow or a different test; what this waits on is the cut itself.
func waitingRequest(out chan<- struct{}) step {
	return func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		select {
		case out <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
}

// TestAModelNamedWhileNothingHasComeBackIsOnTheVeryNextRequest is the owner's
// own case: a request that has produced nothing is let go of, and the re-ask
// carries the model they just named, within [lanes.SpokenWithin].
func TestAModelNamedWhileNothingHasComeBackIsOnTheVeryNextRequest(t *testing.T) {
	out := make(chan struct{}, 1)
	completer := &scriptedCompleter{steps: []step{
		waitingRequest(out),
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("answered on the model they asked for"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events := mustSubmit(t, agent, "what is the answer")
	select {
	case <-out:
	case <-time.After(10 * time.Second):
		t.Fatal("the first request never went out")
	}

	spoken := time.Now()
	agent.SetModel("test/second")
	collect(t, events)

	if got := completer.model(0); got != "test/model" {
		t.Fatalf("the first request rode %q, want the model the turn started on", got)
	}
	if got := completer.model(1); got != "test/second" {
		t.Fatalf("the re-ask rode %q, want the model the person named — a pick that "+
			"waits for the next TURN is the measured failure this law exists for", got)
	}
	if took := time.Since(spoken); took > 10*lanes.SpokenWithin {
		t.Fatalf("the person's word took %s to reach the wire; the law is %s", took, lanes.SpokenWithin)
	}
}

// TestAModelNamedWhileTheAnswerIsArrivingLetsThatAnswerFinish is the other half.
// A reply somebody is reading is theirs, and it is paid for; the pick rides the
// NEXT request instead.
func TestAModelNamedWhileTheAnswerIsArrivingLetsThatAnswerFinish(t *testing.T) {
	spoke := make(chan struct{}, 1)
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "here is the first half")
			select {
			case spoke <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return toolResponseWithText("call-1", "ls", `{"path":"."}`,
				"here is the first half and the rest of it"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("and the step after is on the new model"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events := mustSubmit(t, agent, "start writing")
	select {
	case <-spoke:
	case <-time.After(10 * time.Second):
		t.Fatal("the first request never streamed a word")
	}

	agent.SetModel("test/second")
	close(release)
	collected := collect(t, events)

	if got := completer.model(0); got != "test/model" {
		t.Fatalf("the first request rode %q, want the model the turn started on", got)
	}
	if got := completer.model(1); got != "test/second" {
		t.Fatalf("the step after the answer rode %q, want the model the person named", got)
	}
	// AND THE ANSWER THEY WERE READING IS STILL THERE. A cut here would have
	// thrown away text that was already on their screen and in the transcript,
	// which is the one thing this half of the law exists to refuse.
	if !transcriptHas(agent, "here is the first half and the rest of it") {
		t.Fatalf("the answer that was arriving was not kept whole; transcript: %v", transcriptRoles(agent))
	}
	for _, event := range collected {
		if event.Kind == EventRetrying {
			t.Fatalf("a reply that was arriving was reported as a retry: %+v", event.Retry)
		}
	}
}

// TestStopWhileNothingHasComeBackEndsTheTurnInside is the person's other word on
// the same clock. Stop already cuts the turn's own context and therefore the
// request under it; this pins that, because a build that grew a wait between the
// key and the cut would fail nobody today.
func TestStopWhileNothingHasComeBackEndsTheTurnInside(t *testing.T) {
	out := make(chan struct{}, 1)
	completer := &scriptedCompleter{steps: []step{waitingRequest(out)}}
	agent, _ := newTestAgent(t, completer, nil)

	events := mustSubmit(t, agent, "what is the answer")
	select {
	case <-out:
	case <-time.After(10 * time.Second):
		t.Fatal("the first request never went out")
	}

	spoken := time.Now()
	agent.Interrupt()
	collect(t, events)
	if took := time.Since(spoken); took > 10*lanes.SpokenWithin {
		t.Fatalf("stop took %s to end a request that had produced nothing; the law is %s",
			took, lanes.SpokenWithin)
	}
	if requests := completer.requests(); requests != 1 {
		t.Fatalf("a stopped turn made %d requests, want 1", requests)
	}
}

// TestAWordSaidToATurnThatEndedDoesNotReachTheTurnAfter is the boundary of the
// word's life. The pick itself is durable — it is on the agent — but the WORD is
// owed to one turn, and a turn that took it up already starts on the same model.
func TestAWordSaidToATurnThatEndedDoesNotReachTheTurnAfter(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.running = true
	agent.spokenModel = "test/second"
	agent.mu.Unlock()

	if got := agent.latchTheModel(); got != "test/model" {
		t.Fatalf("a turn latched %q, want the model on the agent", got)
	}
	if word, said := agent.takeModelWord(); said {
		t.Fatalf("a word survived the turn it was said to: %q", word)
	}
}

// transcriptHas reports whether any assistant message in the live transcript
// carries this text.
func transcriptHas(a *Agent, text string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, message := range a.messages {
		if strings.Contains(messageText(message), text) {
			return true
		}
	}
	return false
}
