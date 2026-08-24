package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// WHAT THE TURN LOOP DOES WITH A CUT STREAM.
//
// The adapter cuts a request that went quiet or came apart and hands back a
// provider.StreamCut (internal/provider's streamguard.go). Everything below is
// about the three things this side owes: ask again, keep nothing, and — when
// asking again did not help — say so in words a person can act on.

// cutStep is one scripted request that streams some text and is then cut, the
// way a real guarded stream behaves: the person watched the deltas arrive and
// no response exists.
//
// It is REROUTED, which is the ordinary production case — the stream named the
// endpoint that served it and the ledger struck that lane, so the next attempt
// is genuinely served by somebody else. [blindCutStep] is the other case.
func cutStep(reason provider.CutReason, streamed string) step {
	return cutStepAs(reason, streamed, true)
}

// blindCutStep is a cut that changed nothing about where the next attempt
// lands: `routing off`, or a stream that died before any chunk named its
// endpoint. The turn loop gives that case a shorter budget and moves to another
// model sooner ([cutBudget]).
func blindCutStep(reason provider.CutReason, streamed string) step {
	return cutStepAs(reason, streamed, false)
}

func cutStepAs(reason provider.CutReason, streamed string, rerouted bool) step {
	return func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		if streamed != "" {
			provider.Emit(ctx, provider.StreamDelta, streamed)
		}
		return nil, &provider.StreamCut{Reason: reason, Rerouted: rerouted}
	}
}

func TestACutStreamIsAskedAgainAndSaysSo(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		cutStep(provider.CutBabble, "стаthisada ssss"),
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("here is the real answer"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	events, err := agent.Submit(context.Background(), "what happened?")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := collect(t, events)

	retry, retried := firstOfKind(collected, EventRetrying)
	if !retried {
		t.Fatalf("no retry was announced; events were %v", kinds(collected))
	}
	if !strings.Contains(retry.Text, "lost its thread") {
		t.Fatalf("the retry note = %q, want it to say the reply lost its thread", retry.Text)
	}
	if _, failed := firstOfKind(collected, EventError); failed {
		t.Fatalf("a turn that recovered still ended in an error; events were %v", kinds(collected))
	}
	if completer.requests() != 2 {
		t.Fatalf("requests = %d, want the cut one and the retry", completer.requests())
	}
}

// TestTheJunkNeverReachesTheTranscript is the highest-value property in this
// whole lane. The 2026-08-20 session degenerated twice and the second time was
// worse than the first BECAUSE the first was still in the context.
func TestTheJunkNeverReachesTheTranscript(t *testing.T) {
	const soup = "стаthisada ssssssss 等 済 -09 id済"
	completer := &scriptedCompleter{steps: []step{
		cutStep(provider.CutBabble, soup),
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("clean"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	events, err := agent.Submit(context.Background(), "go on")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collect(t, events)

	// Not in what the retry was sent...
	for _, message := range completer.request(1) {
		for _, part := range message.Content {
			if strings.Contains(part.Text, "стаthisada") {
				t.Fatalf("the cut attempt's text was re-sent to the model: %q", part.Text)
			}
		}
	}
	// ...and not in the session's own record of the conversation.
	for _, message := range agent.snapshot() {
		for _, part := range message.Content {
			if strings.Contains(part.Text, "стаthisada") {
				t.Fatalf("the cut attempt's text was recorded: %q", part.Text)
			}
		}
	}
}

// TestAReplyThatComesApartTwiceEndsTheTurnInWordsThatHelp: the give-up sentence
// names what happened and the two doors that actually open.
func TestAReplyThatComesApartTwiceEndsTheTurnInWordsThatHelp(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		cutStep(provider.CutBabble, "soup"),
		cutStep(provider.CutBabble, "more soup"),
	}}
	agent, _ := newTestAgent(t, completer, nil)
	events, err := agent.Submit(context.Background(), "go on")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := collect(t, events)

	failure, failed := firstOfKind(collected, EventError)
	if !failed {
		t.Fatalf("a reply that came apart twice did not end the turn; events were %v", kinds(collected))
	}
	said := failure.Err.Error()
	for _, want := range []string{"lost its thread twice", "/model", "/compact"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the sentence %q does not contain %q", said, want)
		}
	}
	// NO MACHINERY VOCABULARY reaches a person. The guard's own words for this
	// are the guard's own business.
	for _, banned := range []string{"degenerate", "detector", "babble", "guard", "StreamCut"} {
		if strings.Contains(strings.ToLower(said), banned) {
			t.Fatalf("the sentence %q leaks the machinery word %q", said, banned)
		}
	}
	if completer.requests() != 2 {
		t.Fatalf("requests = %d, want one cut and exactly one retry", completer.requests())
	}
}

// A STALL IS WORTH ASKING TWICE, because it is very often a bad draw out of a
// router's pool and a second try lands somewhere else.
func TestASilentEndpointIsAskedTwiceBeforeTheTurnGivesUp(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		cutStep(provider.CutSilent, ""),
		cutStep(provider.CutSilent, ""),
		cutStep(provider.CutSilent, ""),
	}}
	agent, _ := newTestAgent(t, completer, nil)
	events, err := agent.Submit(context.Background(), "go on")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := collect(t, events)

	if got := countOfKind(collected, EventRetrying); got != silentRetries {
		t.Fatalf("retries announced = %d, want %d", got, silentRetries)
	}
	failure, failed := firstOfKind(collected, EventError)
	if !failed {
		t.Fatalf("three silent attempts did not end the turn; events were %v", kinds(collected))
	}
	said := failure.Err.Error()
	if !strings.Contains(said, "three times") || !strings.Contains(said, "/model") {
		t.Fatalf("the sentence %q does not count the attempts and name the door", said)
	}
	if completer.requests() != 3 {
		t.Fatalf("requests = %d, want the first and both retries", completer.requests())
	}
}

// TestACutDoesNotSpendTheTransportPatience: a stream the guard cut is not
// evidence that the endpoint is failing, so it must not shorten the three
// attempts a real fault gets.
func TestACutDoesNotSpendTheTransportPatience(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		cutStep(provider.CutSilent, ""),
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return nil, &provider.APIError{Status: 500, Message: "server error"}
		},
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return nil, &provider.APIError{Status: 500, Message: "server error"}
		},
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("landed"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	events, err := agent.Submit(context.Background(), "go on")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := collect(t, events)
	if failure, failed := firstOfKind(collected, EventError); failed {
		t.Fatalf("a cut ate one of the fault attempts: %v", failure.Err)
	}
	if completer.requests() != 4 {
		t.Fatalf("requests = %d, want the cut, both faults and the one that landed", completer.requests())
	}
}

func TestTheCutSentenceCountsInWords(t *testing.T) {
	for n, want := range map[int]string{1: "once", 2: "twice", 3: "three times", 7: "7 times"} {
		if got := timesWord(n); got != want {
			t.Errorf("timesWord(%d) = %q, want %q", n, got, want)
		}
	}
}

func countOfKind(events []Event, kind EventKind) int {
	count := 0
	for i := range events {
		if events[i].Kind == kind {
			count++
		}
	}
	return count
}
