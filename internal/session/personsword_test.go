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

// THE PERSON'S WORD IS THE HEAD OF EVERY CHAIN.
//
// A step whose model is refusing walks a fallback ladder — this build's guess at
// where to go next. A model the person named while that was happening is not a
// guess, and a hop that walked past it would spend their turn on the choice they
// had just rejected. The word is taken where the move is really made, so the
// sentence they read names the model their reply actually went to.
//
// The word arrives here while the answer HAD begun, which is the case with no
// cut in it: nothing is let go of, and the move is the only thing left that can
// carry the pick.
func TestAFailingStepMovesToThePersonsPickAndNotTheLadder(t *testing.T) {
	var agent *Agent
	steps := make([]step, 0, turnLadderAttempts+1)
	for i := 0; i < turnLadderAttempts; i++ {
		if i == turnLadderAttempts-1 {
			steps = append(steps, func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				provider.Emit(ctx, provider.StreamDelta, "a word had already arrived")
				agent.SetModel("theirs/pick")
				return nil, refusalOf(429, "rate limited", "Novita", "")
			})
			continue
		}
		steps = append(steps, refusedStep(429, "rate limited", "Novita"))
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("answered where they asked"), nil
	})

	completer := chained([]string{"ladder/next"}, steps...)
	agent, _ = newTestAgent(t, completer, nil)
	impatient(t, agent, turnLadderAttempts)
	collected := collect(t, mustSubmit(t, agent, "go on"))

	if failure, failed := firstOfKind(collected, EventError); failed {
		t.Fatalf("the turn failed although the person had named a model: %v", failure.Err)
	}
	if got := completer.model(turnLadderAttempts); got != "theirs/pick" {
		t.Fatalf("the request after the ladder rode %q, want the model the person named "+
			"— a hop that walks past their pick spends their turn on the choice they rejected", got)
	}
	// AND THE SENTENCE NAMES WHERE THE REPLY WENT. A move announced as the
	// ladder's next rung and then overruled would have named a model the reply
	// never reached, which is the one thing a person-facing line may not do.
	var announced string
	for _, event := range collected {
		if event.Kind == EventRetrying && event.Retry != nil && event.Retry.Next != "" {
			announced = event.Retry.Next
		}
	}
	if announced != "theirs/pick" {
		t.Fatalf("the move was announced as %q, want theirs/pick", announced)
	}
}

// AND A PERSON WHO HAS NAMED A MODEL IS SOMEWHERE LEFT TO GO.
//
// A completer with no chain to offer makes the hop ABSENT — the turn ends on the
// sentence it has always ended on. That is right when this build has run out of
// guesses, and wrong the moment a person has named a model themselves: the turn
// used to end on "there is nowhere else to try" with their choice sitting
// unasked. The reading of whether the step can move at all is what this pins.
func TestATurnWithNoChainStillMovesToAModelThePersonNamed(t *testing.T) {
	var agent *Agent
	steps := make([]step, 0, turnLadderAttempts+1)
	for i := 0; i < turnLadderAttempts; i++ {
		if i == turnLadderAttempts-1 {
			steps = append(steps, func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				provider.Emit(ctx, provider.StreamDelta, "a word had already arrived")
				agent.SetModel("theirs/pick")
				return nil, refusalOf(429, "rate limited", "Novita", "")
			})
			continue
		}
		steps = append(steps, refusedStep(429, "rate limited", "Novita"))
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("answered where they asked"), nil
	})

	// A plain scripted completer offers no [modelChain] at all, which is the
	// build with nowhere of its own to go.
	completer := &scriptedCompleter{steps: steps}
	agent, _ = newTestAgent(t, completer, nil)
	impatient(t, agent, turnLadderAttempts)
	collected := collect(t, mustSubmit(t, agent, "go on"))

	if failure, failed := firstOfKind(collected, EventError); failed {
		t.Fatalf("the turn ended although the person had named a model: %v", failure.Err)
	}
	if got := completer.model(turnLadderAttempts); got != "theirs/pick" {
		t.Fatalf("the request after the ladder rode %q, want the model the person named", got)
	}
}

// ── ONE READING OF "HAS ANYTHING REACHED THE PERSON" ────────────────────────
//
// There were two, five lines apart in the same stream observer, and they
// disagreed about a thought: the recall's gate counted a reasoning delta and the
// person's word did not. Visible thinking is drawn (#760 made a reasoning-only
// turn something somebody watches), so a cut after it would take something off a
// screen, and the door that said it would not was justified by a sentence the
// code contradicted.

// TestAThoughtOnTheScreenIsSomethingTheyHaveRead pins the half that was wrong: a
// model they name while visible thinking is arriving rides the NEXT request, and
// the thought is not taken off their screen.
func TestAThoughtOnTheScreenIsSomethingTheyHaveRead(t *testing.T) {
	thought := make(chan struct{}, 1)
	release := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamReasoning, "weighing up two ways to do this")
			select {
			case thought <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return toolResponseWithText("call-1", "ls", `{"path":"."}`,
				"and here is the answer"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the step after is on the new model"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	events := mustSubmit(t, agent, "think about this one")
	select {
	case <-thought:
	case <-time.After(10 * time.Second):
		t.Fatal("the first request never streamed a thought")
	}

	agent.SetModel("test/second")
	close(release)
	collected := collect(t, events)

	if got := completer.model(0); got != "test/model" {
		t.Fatalf("the first request rode %q, want the model the turn started on", got)
	}
	if got := completer.model(1); got != "test/second" {
		t.Fatalf("the step after rode %q, want the model the person named", got)
	}
	for _, event := range collected {
		if event.Kind == EventRetrying {
			t.Fatalf("a request whose thinking was on the screen was cut and reported "+
				"as a move: %+v — visible thought is something they have read", event.Retry)
		}
	}
}

// TestTheOneReadingIsWhatBothDoorsAsk is the structural half of the same thing.
// A build with two readings passes every behaviour fixture above and still has
// the defect, because the defect is that the two can drift.
func TestTheOneReadingIsWhatBothDoorsAsk(t *testing.T) {
	reached := &reachedThePerson{}
	generation := &activeGeneration{reached: reached}
	aside := &recallAside{}
	aside.watch(reached)

	if generation.productive() {
		t.Fatal("a request that has drawn nothing reported that it had")
	}
	reached.drew()
	if !generation.productive() {
		t.Fatal("the person's word does not see what the observer drew")
	}
	if !aside.reached.Load().did() {
		t.Fatal("the recall's gate and the person's word are reading two different facts, " +
			"which is the defect this reading exists to make impossible")
	}
	reached.reset()
	if generation.productive() || aside.reached.Load().did() {
		t.Fatal("the attempt boundary did not empty the reading for both doors")
	}
}

// TestThePersonsCutTellsTheRoomTheAttemptIsVoid: the engine empties its four
// buffers at the top of the next attempt, and the surface only learns that a
// request it is drawing a clock for is gone if it is told. EventRetrying is that
// one door, and this is a MOVE and says so — `Next` is what every surface reads
// to tell a hop from a retry.
func TestThePersonsCutTellsTheRoomTheAttemptIsVoid(t *testing.T) {
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
	agent.SetModel("test/second")
	collected := collect(t, events)

	var told *RetryNews
	for _, event := range collected {
		if event.Kind == EventRetrying && event.Retry != nil {
			told = event.Retry
		}
	}
	if told == nil {
		t.Fatal("the room was never told the request it was drawing a clock for was let go of; " +
			"the wait a person spoke to end would keep counting up")
	}
	if told.Next != "test/second" {
		t.Fatalf("the withdrawal named %q as where the step is going, want the model they chose — "+
			"an empty Next reads as `asking again` on every surface", told.Next)
	}
	if told.Attempts != 0 || told.Attempt != 0 {
		t.Fatalf("a move a person made drew a count of somebody's patience: %+v", told)
	}
}

// TestAPickOfTheModelTheStepIsAlreadyRidingSpendsNothing. The door used to
// compare a pick against the SESSION's model, which is not the model a rescued
// step is talking to — so picking the fallback the step was already on was read
// as a change, cut a live request, and changed nothing.
func TestAPickOfTheModelTheStepIsAlreadyRidingSpendsNothing(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.mu.Lock()
	agent.running = true
	agent.riding = "test/fallback"
	agent.mu.Unlock()

	if landing := agent.setModel("test/fallback"); landing != ModelLandsNextRequest {
		t.Fatalf("picking the model the step is already riding landed as %q; it is a person "+
			"confirming, not redirecting, and a cut here spends a request for no change", landing)
	}
	if word, said := agent.takeModelWord(); said {
		t.Fatalf("a pick that changes nothing left a word standing: %q", word)
	}
	// AND THE SESSION'S OWN MODEL IS STILL NEWS. The step rides the fallback; the
	// model the session remembers is not what the work is talking to, so choosing
	// it is a real redirection that the old reading called none.
	agent.mu.Lock()
	agent.riding = "test/fallback"
	agent.mu.Unlock()
	if landing := agent.setModel("test/model"); landing == ModelLandsNextRequest {
		if word, said := agent.takeModelWord(); !said {
			t.Fatalf("picking the session's model while the step rides a fallback left no word "+
				"(landing %q, word %q): that is the pick the old comparison could not see",
				landing, word)
		}
	}
}

// ── `CONTINUE` IS A PERSON'S WORD TOO, AND IT HAS THE SAME CLOCK ────────────
//
// The owner's 14:40:10 evidence is TWO acts: they picked a model AND typed
// `continue`. A direction into a running room was enqueued and nothing else, so
// it was drained when the request ended — which on that step was thirteen
// minutes away. The record was always right; what was missing was the hearing.

// TestALineTypedIntoARoomReachesTheStepInsideTheClock is the wire fact: the
// worker's next request goes out inside [lanes.SpokenWithin] of the line, and it
// carries the words.
func TestALineTypedIntoARoomReachesTheStepInsideTheClock(t *testing.T) {
	out := make(chan struct{}, 1)
	carried := make(chan []ai.Message, 1)
	inside := &scriptedCompleter{steps: []step{
		waitingRequest(out),
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			select {
			case carried <- messages:
			default:
			}
			return textResponse("carrying on"), nil
		},
	}}
	agent, node, child, land := retargetAgentWorking(t, inside)
	defer land()

	events := mustSubmit(t, child, "do the work")
	select {
	case <-out:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker's first request never went out")
	}

	spoken := time.Now()
	receipt, err := agent.SteerTask(node.id, "continue")
	if err != nil {
		t.Fatalf("the room refused the line: %v", err)
	}
	if receipt.Heard != ModelLandsNow {
		t.Fatalf("a line typed at a request that had produced nothing was heard as %q; "+
			"the whole defect is that it waited for the request to end on its own", receipt.Heard)
	}

	var messages []ai.Message
	select {
	case messages = <-carried:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker never made another request; the line was written down and never heard")
	}
	if took := time.Since(spoken); took > 10*lanes.SpokenWithin {
		t.Fatalf("a line typed into a room took %s to reach the wire; the law is %s",
			took, lanes.SpokenWithin)
	}
	said := false
	for _, message := range messages {
		if strings.Contains(messageText(message), "continue") {
			said = true
		}
	}
	if !said {
		t.Fatal("the request the cut bought did not carry the words that bought it")
	}
	collect(t, events)
}

// TestALineTypedWhileTheAnswerIsArrivingWaitsForTheBoundary is the other half of
// the same rule, said about a direction: words on a screen are theirs.
func TestALineTypedWhileTheAnswerIsArrivingWaitsForTheBoundary(t *testing.T) {
	spoke := make(chan struct{}, 1)
	release := make(chan struct{})
	inside := &scriptedCompleter{steps: []step{
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
			return textResponse("here is the first half and the rest of it"), nil
		},
	}}
	agent, node, child, land := retargetAgentWorking(t, inside)
	defer land()

	events := mustSubmit(t, child, "do the work")
	select {
	case <-spoke:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker's first request never streamed a word")
	}

	receipt, err := agent.SteerTask(node.id, "actually make it CSV")
	if err != nil {
		t.Fatalf("the room refused the line: %v", err)
	}
	if receipt.Heard != ModelLandsNextRequest {
		t.Fatalf("a line typed while the answer was arriving was heard as %q, so a reply "+
			"somebody was reading was thrown away", receipt.Heard)
	}
	close(release)
	collect(t, events)
	if !transcriptHas(child, "here is the first half and the rest of it") {
		t.Fatal("the answer that was arriving was not kept whole")
	}
}

// TestTheClockOnAPersonsWordIsTheOneTheManualPublishes is the number, not the
// mechanism. Nothing here is timed — the cut fires on a state — but the pages
// promise "within a second" and the fixtures above allow ten times that so they
// cannot flake on a loaded box. This is the one that holds the published figure,
// measured on the wire and read off [lanes.SpokenWithin] rather than written out
// as a constant beside it.
func TestTheClockOnAPersonsWordIsTheOneTheManualPublishes(t *testing.T) {
	out := make(chan struct{}, 1)
	wire := make(chan time.Time, 1)
	completer := &scriptedCompleter{steps: []step{
		waitingRequest(out),
		func(context.Context, []ai.Message) (*ai.Response, error) {
			select {
			case wire <- time.Now():
			default:
			}
			return textResponse("on the model they asked for"), nil
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
	var landed time.Time
	select {
	case landed = <-wire:
	case <-time.After(10 * time.Second):
		t.Fatal("the word never reached the wire at all")
	}
	collect(t, events)

	if took := landed.Sub(spoken); took > lanes.SpokenWithin {
		t.Fatalf("a person's word took %s to reach the wire and the manual promises %s. "+
			"Nothing between the word and the cut may wait: if something new belongs there, "+
			"it belongs beside the work and not in front of it (loop.go's THE ONLY WAIT A "+
			"PERSON EXPERIENCES).", took, lanes.SpokenWithin)
	}
}
