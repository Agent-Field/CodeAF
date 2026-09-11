package session

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/taxonomy"
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

// ── AND A MODEL THAT KEEPS REFUSING IS GIVEN UP ON THE SAME WAY ─────────────
//
// A cut stream and a refused request are one story: this model's budget is
// spent, and there is another model. It did not used to be. The hop lived on
// the cut road only, so a refusal storm walked the transport ladder and ended
// the turn with a fallback chain that had never been asked — measured on
// 2026-09-10, one 502 and three 429s between 13:00:17 and 13:01:32, while a
// second model in the same session answered every call put to it.

// refusedStep is one request the provider would not serve: a status, the
// router's sentence, and the upstream it named. A 4xx that NAMED an upstream is
// that upstream's refusal and another endpoint may serve it, which is what makes
// it the wire rather than our own bytes ([provider.APIError.OurRequest]).
func refusedStep(status int, message, upstream string) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, refusalOf(status, message, upstream, "")
	}
}

// impatient runs the turn's ladder on a clock the test owns, so that a
// ninety-second give-up is spent in microseconds.
//
// ── WHY IT IS A CLOCK AND NOT A COUNT ───────────────────────────────────────
//
// It used to hand the agent `Limits{TransportAttempts: n}` and a one-millisecond
// wait: the ladder was bounded by a number, so shortening the schedule in front
// of it was enough. The number is gone (docs/design/recovery/DESIGN.md §4) and
// the bound is the deadline — so a wait that returns at once does not shorten
// the ladder, it makes the deadline UNREACHABLE, which is the same fiction
// internal/provider's own scenarios keep honestly (`owed` in dispatch.go).
//
// So the wait moves the clock instead of sleeping on it. The product's real
// schedule — [retryBaseDelay] doubling — then spends the real give-up in real
// arithmetic and no real time, and `attempts` is what the caller wants that to
// come to: the first wait is sized so the deadline lands after exactly that many
// requests.
// turnLadderAttempts is how many requests these scenarios give one model before
// its deadline is gone. THREE, which is exactly what the shipped build did when
// the ladder was a count — so every assertion below still describes the budget a
// person actually gets, and describes it as a length of time rather than as a
// number of sends.
const turnLadderAttempts = 3

func impatient(t *testing.T, agent *Agent, attempts int) {
	t.Helper()
	if attempts < 2 {
		attempts = 2
	}
	// The waits double — b, 2b, 4b … — so before the nth request the clock has
	// moved b(2^(n−1) − 1). The caller wants `attempts` requests out of a ladder
	// that pays a wait after every failure, which is b = give-up / (2^n − 1):
	// three waits then come to exactly the give-up and the fourth request is
	// never made.
	//
	// A LADDER WHOSE FIRST MOVE IS FREE GETS ONE MORE, and that is not a fudge —
	// it is [nextMoveWait]'s rule showing through. An endpoint that answered
	// instantly with nothing is answered by asking somebody else at once, so the
	// first of those costs no time at all.
	// The millisecond is what stops integer division landing the last wait one
	// nanosecond short of the deadline and buying a request nobody asked for.
	first := agent.giveUp()/time.Duration(int64(1)<<uint(attempts)-1) + time.Millisecond

	onATestClock(t)
	// AND THE LIMITS ARE SET RATHER THAN OFFERED. `limitsOnce` may already have
	// been spent by anything that read a failure while the agent was being built,
	// and a helper whose whole job is to shorten this ladder must not be the one
	// that silently did not.
	agent.limitsOnce.Do(func() {})
	agent.limits = taxonomy.Limits{TransportBackoff: first}.Floored()
}

// onATestClock makes every wait this package takes between two attempts move a
// clock instead of sleeping on one.
//
// It is the same fiction internal/provider's scenarios keep (`owed` in
// dispatch.go) and it is kept for the same reason: the bound on every ladder in
// this package is a DEADLINE now, so a wait that returns at once would make the
// deadline unreachable rather than making the test fast. Here the wait is
// charged in full and the ladder spends its real schedule in no real time.
func onATestClock(t *testing.T) { onAClockFor(t, &turnBackoff) }

// onATestTitleClock is the same fiction for the naming ladder, which has a wait
// seam of its own because it runs beside a turn rather than inside one.
func onATestTitleClock(t *testing.T) { onAClockFor(t, &titleBackoff) }

func onAClockFor(t *testing.T, wait *func(context.Context, time.Duration) error) {
	t.Helper()
	base := time.Now()
	var moved atomic.Int64
	previousNow, previousWait := turnNow, *wait
	turnNow = func() time.Time { return base.Add(time.Duration(moved.Load())) }
	*wait = func(ctx context.Context, delay time.Duration) error {
		moved.Add(int64(delay))
		return ctx.Err()
	}
	t.Cleanup(func() { turnNow, *wait = previousNow, previousWait })
}

// retryNewsOf is the structured half of every retry this turn announced, with a
// failure message that names any line that arrived without one — the payload is
// promised on EVERY EventRetrying, and a surface reading it cannot tell "no news
// here" from "this engine forgot".
func retryNewsOf(t *testing.T, events []Event) []*RetryNews {
	t.Helper()
	var news []*RetryNews
	for _, event := range events {
		if event.Kind != EventRetrying {
			continue
		}
		if event.Retry == nil {
			t.Fatalf("a retry announced %q carried no news beside it", event.Text)
		}
		if strings.TrimSpace(event.Text) == "" {
			t.Fatalf("a retry carried news and no sentence: %+v", *event.Retry)
		}
		news = append(news, event.Retry)
	}
	return news
}

// The whole shape, in one turn: the ladder is spent on refusals, the step moves,
// and the answer arrives on the fallback with every row naming who did what.
func TestARefusalStormMovesToTheNextModelAndTheReplyFinishesThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	// THE STORM IS EXACTLY AS LONG AS THE BUDGET, and it is built from the
	// budget rather than written out beside it. Four refusals were spelled here
	// while the ladder's own count was four, so lowering the budget
	// left one refusal over for the fallback to trip on and the test failed
	// about a number it was not asking about. Every other assertion below
	// already interpolates; this is the fixture catching up with them.
	refusals := []step{
		refusedStep(502, "Bad gateway", "Together"),
		refusedStep(429, "rate limited", "DeepInfra"),
		refusedStep(429, "rate limited", "Fireworks"),
		refusedStep(429, "rate limited", "Novita"),
	}
	steps := make([]step, 0, turnLadderAttempts+1)
	for i := 0; i < turnLadderAttempts; i++ {
		steps = append(steps, refusals[i%len(refusals)])
	}
	steps = append(steps, func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
		return textResponse("the whole answer"), nil
	})
	completer := chained([]string{"other/model"}, steps...)
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = path })
	impatient(t, agent, turnLadderAttempts)
	collected := collect(t, mustSubmit(t, agent, "go on"))

	if failure, failed := firstOfKind(collected, EventError); failed {
		t.Fatalf("the turn failed although a fallback answered: %v", failure.Err)
	}
	if got := completer.requests(); got != turnLadderAttempts+1 {
		t.Fatalf("requests = %d, want the whole ladder and the answer on the fallback", got)
	}
	for index := 0; index < turnLadderAttempts; index++ {
		if got := completer.model(index); got != "test/model" {
			t.Fatalf("attempt %d rode %q, want the model the turn started on", index+1, got)
		}
	}
	if got := completer.model(turnLadderAttempts); got != "other/model" {
		t.Fatalf("the answering request rode %q, want other/model", got)
	}

	// THE MOVE IS SAID, AND IT IS SAID IN PARTS. The sentence names the model
	// because the rest of the reply arrives in a different voice; Next names it
	// again where a surface can read it without parsing anybody's prose.
	news := retryNewsOf(t, collected)
	var moved *RetryNews
	for _, item := range news {
		if item.Next != "" {
			moved = item
		}
	}
	if moved == nil {
		t.Fatalf("no retry said the step was moving; they were %+v", news)
	}
	if moved.Next != "other/model" {
		t.Fatalf("the step said it was moving to %q, want other/model", moved.Next)
	}
	if moved.Model != "test/model" {
		t.Fatalf("the move blamed %q, want the model whose budget was spent", moved.Model)
	}
	if moved.Attempt != turnLadderAttempts || moved.Attempts != turnLadderAttempts {
		t.Fatalf("the move was announced at %d of %d, want the whole budget spent",
			moved.Attempt, moved.Attempts)
	}
	if !strings.Contains(moved.Reason, "turn") && !strings.Contains(moved.Reason, "take the request") {
		t.Fatalf("the move's reason %q does not say what kept happening", moved.Reason)
	}
	// AND NO MACHINERY WORD REACHES THE PERSON, in the sentence or beside it.
	for _, said := range []string{moved.Reason, textsOfKind(collected, EventRetrying)[len(news)-1]} {
		for _, banned := range []string{"endpoint", "transport", "fallback", "verdict", "budget"} {
			if strings.Contains(strings.ToLower(said), banned) {
				t.Fatalf("%q leaks the machinery word %q", said, banned)
			}
		}
	}

	// THE RECORD NAMES WHO FAILED AND WHO ANSWERED. One classification per spent
	// attempt, all on the model that could not serve it, numbered the way a
	// person counts.
	rows := journaledFailures(t, path)
	if len(rows) != turnLadderAttempts {
		t.Fatalf("%d classification rows, want one per spent attempt: %+v", len(rows), rows)
	}
	for i, row := range rows {
		if row.Model != "test/model" {
			t.Fatalf("row %d blamed %q; the fallback never failed at all", i+1, row.Model)
		}
		if row.Attempt != i+1 {
			t.Fatalf("row %d is numbered %d", i+1, row.Attempt)
		}
		if row.Class != string(taxonomy.Transport) {
			t.Fatalf("row %d was classified %q; nothing about who SERVED a request is evidence about who was asked",
				i+1, row.Class)
		}
	}
	if last := rows[len(rows)-1]; last.Action != string(taxonomy.ActionHop) {
		t.Fatalf("the spent ladder did %q, want the step moved to the next model", last.Action)
	}
	// AND THE SEALED TURN NAMES THE MODEL THAT ACTUALLY ANSWERED.
	sealed := ""
	for _, entry := range journaledEntries(t, path, "usage") {
		if entry.Usage != nil && !entry.Usage.Aux {
			sealed = entry.Usage.Model
		}
	}
	if sealed != "other/model" {
		t.Fatalf("the turn was sealed on %q, want the model that answered it", sealed)
	}
	// The person's own pick is untouched: the move rescues this turn.
	if got := agent.Model(); got != "test/model" {
		t.Fatalf("the session moved to %q; a move rescues a turn, it does not re-pick a model", got)
	}
}

// AND WHEN THE CHAIN IS WALKED OUT TOO, THE SENTENCE NAMES EVERY MODEL TRIED.
// "A different model may answer" said to somebody who has just watched two of
// them refuse is the surface not knowing what it did.
func TestARefusalStormThatWalksTheWholeChainNamesEveryModelTried(t *testing.T) {
	steps := make([]step, 0, 2*turnLadderAttempts)
	for i := 0; i < 2*turnLadderAttempts; i++ {
		steps = append(steps, refusedStep(429, "rate limited", "Together"))
	}
	completer := chained([]string{"second/model"}, steps...)
	agent, _ := newTestAgent(t, completer, nil)
	impatient(t, agent, turnLadderAttempts)
	collected := collect(t, mustSubmit(t, agent, "go on"))

	failure, failed := firstOfKind(collected, EventError)
	if !failed {
		t.Fatalf("a walked-out chain did not end the turn; events were %v", kinds(collected))
	}
	said := failure.Err.Error()
	for _, want := range []string{"test/model", "second/model", "could not finish it either", "/model"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the sentence %q does not contain %q", said, want)
		}
	}
	for _, banned := range []string{"endpoint", "fallback", "transport", "budget"} {
		if strings.Contains(strings.ToLower(said), banned) {
			t.Fatalf("the sentence %q leaks the machinery word %q", said, banned)
		}
	}
	if got := completer.requests(); got != 2*turnLadderAttempts {
		t.Fatalf("requests = %d, want a whole budget on each of two models", got)
	}
	// EACH MODEL GOT A BUDGET OF ITS OWN. A fallback that inherited a spent one
	// would be given up on before it had answered once.
	for index := turnLadderAttempts; index < 2*turnLadderAttempts; index++ {
		if got := completer.model(index); got != "second/model" {
			t.Fatalf("request %d rode %q after the move, want second/model", index+1, got)
		}
	}
}

// `--one-model` IS A PERSON SAYING NO TO THIS, and it makes the move ABSENT
// rather than broken: the ladder is walked, the turn ends on the sentence it has
// always ended on, and nothing is ever asked of another model.
func TestUnderOneModelARefusalStormNeverMoves(t *testing.T) {
	steps := make([]step, 0, turnLadderAttempts)
	for i := 0; i < turnLadderAttempts; i++ {
		steps = append(steps, refusedStep(429, "rate limited", "Together"))
	}
	completer := chained([]string{"other/model"}, steps...)
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.OneModel = true })
	impatient(t, agent, turnLadderAttempts)
	collected := collect(t, mustSubmit(t, agent, "go on"))

	if _, failed := firstOfKind(collected, EventError); !failed {
		t.Fatalf("the spent ladder did not end the turn; events were %v", kinds(collected))
	}
	if got := completer.requests(); got != turnLadderAttempts {
		t.Fatalf("requests = %d, want one budget and no move", got)
	}
	for index := 0; index < completer.requests(); index++ {
		if got := completer.model(index); got != "test/model" {
			t.Fatalf("request %d rode %q; the person pinned one model", index+1, got)
		}
	}
	for _, item := range retryNewsOf(t, collected) {
		if item.Next != "" {
			t.Fatalf("a retry said it was moving to %q under --one-model", item.Next)
		}
	}
}

// AND OUR OWN BYTES ARE STILL ANSWERED AT ONCE. A 4xx that named no upstream is
// the router reading the request we assembled and saying no; every endpoint
// alive will say the same thing about the same bytes, and so will every model —
// so there is nothing to ask again and nowhere to move.
func TestARefusalOfOurOwnRequestNeitherRetriesNorMoves(t *testing.T) {
	completer := chained([]string{"other/model"},
		refusedStep(400, "messages: at least one message is required", ""),
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("never asked"), nil
		},
	)
	agent, _ := newTestAgent(t, completer, nil)
	impatient(t, agent, turnLadderAttempts)
	collected := collect(t, mustSubmit(t, agent, "go on"))

	if _, failed := firstOfKind(collected, EventError); !failed {
		t.Fatalf("a refusal of our own request did not end the turn; events were %v", kinds(collected))
	}
	if got := completer.requests(); got != 1 {
		t.Fatalf("requests = %d, want the one refusal and nothing after it", got)
	}
	if got := countOfKind(collected, EventRetrying); got != 0 {
		t.Fatalf("%d retries were announced for a request no endpoint would take", got)
	}
}
