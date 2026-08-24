package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// WHEN A MODEL RUNS OUT OF ABILITY TO ANSWER, THE TURN MOVES.
//
// A stream that is cut, asked again and cut again has spent everything the
// cheaper explanations are worth: the endpoint was struck out of the ledger
// after the first cut, so the attempts since then were served by somebody else,
// and the model is what is left. Everything below is about the one move that
// answers it — hop to the next model in the chain, SAY SO, and finish the
// reply there — and about the four ways it deliberately does not happen.

// chainedCompleter is a scripted completer that also offers a fallback chain,
// which is the optional half of a Completer the turn loop reads ([modelChain]).
type chainedCompleter struct {
	*scriptedCompleter
	chain []string
}

func (c *chainedCompleter) FallbackModels(string) []string { return c.chain }

func chained(chain []string, steps ...step) *chainedCompleter {
	return &chainedCompleter{scriptedCompleter: &scriptedCompleter{steps: steps}, chain: chain}
}

// The whole shape, in one turn: three cuts spend the budget, the hop is
// announced by name, and the answer arrives on the fallback.
func TestAStalledModelIsGivenUpOnAndTheReplyFinishesOnTheNextOne(t *testing.T) {
	completer := chained([]string{"other/model"},
		cutStep(provider.CutStalled, "half a "),
		cutStep(provider.CutStalled, "half a "),
		cutStep(provider.CutStalled, "half a "),
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("the whole answer"), nil
		},
	)
	agent, _ := newTestAgent(t, completer, nil)
	events, err := agent.Submit(context.Background(), "go on")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := collect(t, events)

	if _, failed := firstOfKind(collected, EventError); failed {
		t.Fatalf("the turn failed although a fallback answered; events were %v", kinds(collected))
	}
	// THE HOP IS SAID, and it names the model — the rest of the reply arrives
	// in a different voice and somebody watching is owed the reason.
	var announced string
	for _, event := range collected {
		if event.Kind == EventRetrying && strings.Contains(event.Text, "other/model") {
			announced = event.Text
		}
	}
	if announced == "" {
		t.Fatalf("the hop was never announced; retry lines were %v", textsOfKind(collected, EventRetrying))
	}
	if !strings.Contains(announced, "going quiet") {
		t.Fatalf("the hop line %q does not say what happened first", announced)
	}
	if completer.requests() != 4 {
		t.Fatalf("requests = %d, want three cuts and the answer on the fallback", completer.requests())
	}
	if got := completer.model(3); got != "other/model" {
		t.Fatalf("the answering request rode %q, want other/model", got)
	}
	// AND THE PERSON'S OWN PICK IS UNTOUCHED. The hop rescues this turn; it is
	// not a preference somebody expressed, so the next turn starts where they
	// put it.
	if got := agent.Model(); got != "test/model" {
		t.Fatalf("the session moved to %q; a hop rescues a turn, it does not re-pick a model", got)
	}
}

// THE DIVERSITY GATE, blind half: a cut that changed nothing about where the
// next attempt lands is not worth two more of them.
func TestACutThatRoutedNowhereMovesToAnotherModelSooner(t *testing.T) {
	completer := chained([]string{"other/model"},
		blindCutStep(provider.CutSilent, ""),
		blindCutStep(provider.CutSilent, ""),
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("answered elsewhere"), nil
		},
	)
	agent, _ := newTestAgent(t, completer, nil)
	events, err := agent.Submit(context.Background(), "go on")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collect(t, events)

	if completer.requests() != 3 {
		t.Fatalf("requests = %d, want one cut, one retry and the hop", completer.requests())
	}
	if got := completer.model(2); got != "other/model" {
		t.Fatalf("the third request rode %q, want other/model", got)
	}
}

// The same script with the ledger doing its work takes the full budget first:
// the endpoints really were different, so the model had not been ruled out yet.
func TestACutThatRoutedAroundAnEndpointKeepsTheFullBudget(t *testing.T) {
	completer := chained([]string{"other/model"},
		cutStep(provider.CutSilent, ""),
		cutStep(provider.CutSilent, ""),
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("the same model got there"), nil
		},
	)
	agent, _ := newTestAgent(t, completer, nil)
	events, err := agent.Submit(context.Background(), "go on")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collect(t, events)

	if got := completer.model(2); got != "test/model" {
		t.Fatalf("the third request rode %q; two cuts is not yet a verdict on the model", got)
	}
}

// NO CHAIN IS NO HOP, and it is ABSENT rather than broken: the turn ends on the
// sentence it has always ended on, naming the door a person can open.
func TestWithoutAChainTheTurnStillEndsInTheOldWords(t *testing.T) {
	completer := chained(nil,
		cutStep(provider.CutSilent, ""),
		cutStep(provider.CutSilent, ""),
		cutStep(provider.CutSilent, ""),
	)
	agent, _ := newTestAgent(t, completer, nil)
	events, err := agent.Submit(context.Background(), "go on")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := collect(t, events)

	failure, failed := firstOfKind(collected, EventError)
	if !failed {
		t.Fatalf("three cuts with nowhere to go did not end the turn; events were %v", kinds(collected))
	}
	said := failure.Err.Error()
	for _, want := range []string{"three times", "/model", "models.fallbacks"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the sentence %q does not contain %q", said, want)
		}
	}
	if completer.requests() != 3 {
		t.Fatalf("requests = %d, want the first and both retries and no hop", completer.requests())
	}
}

// A completer with no chain to offer at all — the single-model build, a test
// double, a machine with no catalog — never hops either, and never panics
// trying to ask.
func TestACompleterWithNoChainNeverHops(t *testing.T) {
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

	if _, failed := firstOfKind(collected, EventError); !failed {
		t.Fatalf("a completer with no chain did not end the turn; events were %v", kinds(collected))
	}
	for index := 0; index < completer.requests(); index++ {
		if got := completer.model(index); got != "test/model" {
			t.Fatalf("request %d rode %q; there was no chain to ride", index, got)
		}
	}
}

// THE CHAIN IS WALKED ONCE AND THEN IT IS OVER. When the last model in it
// cannot finish either, the sentence names them rather than advising a move
// that has already been made twice.
func TestWhenEveryFallbackFailsTheSentenceNamesThem(t *testing.T) {
	steps := make([]step, 0, 9)
	for i := 0; i < 9; i++ {
		steps = append(steps, cutStep(provider.CutSilent, ""))
	}
	completer := chained([]string{"second/model", "third/model"}, steps...)
	agent, _ := newTestAgent(t, completer, nil)
	events, err := agent.Submit(context.Background(), "go on")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collected := collect(t, events)

	failure, failed := firstOfKind(collected, EventError)
	if !failed {
		t.Fatalf("a walked-out chain did not end the turn; events were %v", kinds(collected))
	}
	said := failure.Err.Error()
	for _, want := range []string{"second/model", "third/model", "could not finish it either"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the sentence %q does not contain %q", said, want)
		}
	}
	// The advice that has already been taken is not repeated as advice.
	if strings.Contains(said, "models.fallbacks") {
		t.Fatalf("the sentence %q advises setting a chain that was just walked", said)
	}
	// NO MACHINERY VOCABULARY reaches a person.
	for _, banned := range []string{"fallback", "chain", "budget", "ledger", "endpoint"} {
		if strings.Contains(strings.ToLower(said), banned) {
			t.Fatalf("the sentence %q leaks the machinery word %q", said, banned)
		}
	}
	if completer.requests() != 9 {
		t.Fatalf("requests = %d, want three attempts on each of three models", completer.requests())
	}
}

// A fallback model is asked for the level somebody set on IT. Reasoning
// strength is held per model id (agent.go), and carrying the level dialled onto
// a model that stopped answering would be asking a new one for something nobody
// chose for it.
func TestAHopAsksTheFallbackForItsOwnReasoningLevel(t *testing.T) {
	completer := chained([]string{"other/model"},
		cutStep(provider.CutStalled, ""),
		cutStep(provider.CutStalled, ""),
		cutStep(provider.CutStalled, ""),
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	)
	agent, _ := newTestAgent(t, completer, nil)
	agent.SetReasoning("high")
	events, err := agent.Submit(context.Background(), "go on")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	collect(t, events)

	if len(completer.efforts) != 4 {
		t.Fatalf("requests = %d, want three cuts and the answer on the fallback", len(completer.efforts))
	}
	if got := completer.efforts[0]; got != provider.EffortHigh {
		t.Fatalf("the first request asked for %q, want the level set on the session model", got)
	}
	if got := completer.efforts[3]; got != provider.EffortNone {
		t.Fatalf("the fallback was asked for %q; nobody set a level on that model", got)
	}
}

// textsOfKind is the Text of every event of one kind, for a failure message
// that shows what was actually said.
func textsOfKind(events []Event, kind EventKind) []string {
	var said []string
	for _, event := range events {
		if event.Kind == kind {
			said = append(said, event.Text)
		}
	}
	return said
}
