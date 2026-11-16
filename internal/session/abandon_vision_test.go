package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A VISION TURN IS A TURN TO THE SECOND STAGE OF A STOP.
//
// The fallback that sends an image to another model ([Agent.SubmitImage]) runs
// as a turn of its own, and a bounded stop ([Agent.InterruptWithin]) names the
// turn it was armed on. These pin the three ways a vision turn used to slip
// past that: started after an ordinary turn's stop, stopped and followed by an
// ordinary turn, and stopped while wedged. The last two used to close the same
// `done` twice, which is a panic nothing recovers.

// visionAgent is an agent on a model that cannot see, whose image messages are
// answered by a vision model through the given completer.
func visionAgent(t *testing.T, completer Completer) (*Agent, string, string) {
	t.Helper()
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, workspace := newTestAgent(t, completer, func(c *Config) { blindWithVision(c); c.SessionFile = journal })
	agent.SetModel("vendor/blind")
	return agent, writeImage(t, workspace, "shot.png", "PHOTOBYTES"), journal
}

// drained closes once the stream has ended.
func drained(events <-chan Event) <-chan struct{} {
	ended := make(chan struct{})
	go func() {
		for range events {
		}
		close(ended)
	}()
	return ended
}

// Scenario A: an ordinary turn is stopped and ends at once; inside the bound
// the person sends an image to a model that cannot see. The first stop's clock
// must leave the vision turn alone, and the vision turn must end ordinarily.
func TestABoundedStopLeavesAVisionTurnAfterItAlone(t *testing.T) {
	reached := make(chan struct{})
	looking := make(chan struct{})
	hold := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(reached)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(looking)
			<-hold
			return textResponse("a red harbour at dusk"), nil
		},
	}}
	agent, image, journal := visionAgent(t, completer)

	first, err := agent.Submit(context.Background(), "ask something that never answers")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitUntilClosed(t, "the request going out", reached, 10*time.Second)
	const grace = 300 * time.Millisecond
	agent.InterruptWithin(grace)
	<-drained(first)

	second, err := agent.SubmitImage(context.Background(), "what is in this?", []Image{{Path: image}})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	waitUntilClosed(t, "the vision call going out", looking, 10*time.Second)
	ended := drained(second)
	select {
	case <-ended:
		t.Fatal("the first turn's stop let go of the vision turn after it")
	case <-time.After(3 * grace):
	}
	if lines := abandonedLines(t, journal); len(lines) != 0 {
		t.Fatalf("a turn was journaled as abandoned though none was: %+v", lines)
	}
	close(hold)
	waitUntilClosed(t, "the vision turn ending on its own", ended, 10*time.Second)
	waitIdle(t, agent)
}

// Scenario B: the conversation's first turn is a vision turn and it is stopped;
// the ordinary message the person sends inside the bound must be left alone.
func TestABoundedStopOnAVisionTurnLeavesTheTurnAfterItAlone(t *testing.T) {
	looking := make(chan struct{})
	reached := make(chan struct{})
	hold := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(looking)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(reached)
			<-hold
			return textResponse("an ordinary answer"), nil
		},
	}}
	agent, image, journal := visionAgent(t, completer)

	first, err := agent.SubmitImage(context.Background(), "what is in this?", []Image{{Path: image}})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	waitUntilClosed(t, "the vision call going out", looking, 10*time.Second)
	const grace = 300 * time.Millisecond
	agent.InterruptWithin(grace)
	<-drained(first)

	second, err := agent.Submit(context.Background(), "an ordinary question")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitUntilClosed(t, "the ordinary request going out", reached, 10*time.Second)
	ended := drained(second)
	select {
	case <-ended:
		t.Fatalf("the stop on the vision turn let go of the ordinary turn after it; abandoned: %+v", abandonedLines(t, journal))
	case <-time.After(3 * grace):
	}
	if lines := abandonedLines(t, journal); len(lines) != 0 {
		t.Fatalf("a turn was journaled as abandoned though none was: %+v", lines)
	}
	close(hold)
	waitUntilClosed(t, "the ordinary turn ending on its own", ended, 10*time.Second)
	waitIdle(t, agent)
}

// Scenario C: a vision turn wedged on a call that ignores its context is let go
// of by the bound, and when the call finally returns the left-behind goroutine
// cleans up nothing: no second close of `done`, no message written into the
// conversation the person has moved on in.
func TestABoundedStopAbandonsAWedgedVisionTurnOnce(t *testing.T) {
	looking := make(chan struct{})
	hold := make(chan struct{})
	reached := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(looking)
			<-hold
			return textResponse("a description nobody is waiting for"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(reached)
			return textResponse("the next answer"), nil
		},
	}}
	agent, image, journal := visionAgent(t, completer)

	first, err := agent.SubmitImage(context.Background(), "what is in this?", []Image{{Path: image}})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	waitUntilClosed(t, "the vision call going out", looking, 10*time.Second)
	agent.InterruptWithin(50 * time.Millisecond)
	waitUntilClosed(t, "the bound letting go of the wedged vision turn", drained(first), 10*time.Second)
	waitFor(t, "the abandoned line in the journal", func() bool { return len(abandonedLines(t, journal)) > 0 })
	if lines := abandonedLines(t, journal); len(lines) != 1 || lines[0].Reason != string(AbandonStopTimeout) {
		t.Fatalf("want one abandoned line saying %q, got %+v", AbandonStopTimeout, lines)
	}

	agent.mu.Lock()
	before := len(agent.messages)
	agent.mu.Unlock()
	// The wedged call returns now. Its cleanup used to close `done` a second
	// time here and take the process down.
	close(hold)
	time.Sleep(100 * time.Millisecond)
	agent.mu.Lock()
	after, running := len(agent.messages), agent.running
	agent.mu.Unlock()
	if after != before {
		t.Fatalf("the abandoned vision turn wrote %d message(s) into the conversation", after-before)
	}
	if running {
		t.Fatal("the abandoned vision turn's return left the session claiming a turn")
	}

	next, err := agent.Submit(context.Background(), "carry on")
	if err != nil {
		t.Fatalf("the session refused the prompt after an abandoned vision turn: %v", err)
	}
	waitUntilClosed(t, "the next request going out", reached, 10*time.Second)
	waitUntilClosed(t, "the next turn ending", drained(next), 10*time.Second)
	if _, again := agent.Abandon(AbandonStopTimeout); again {
		t.Fatal("an idle session had a turn for Abandon to let go of")
	}
	if lines := abandonedLines(t, journal); len(lines) != 1 {
		t.Fatalf("want exactly one abandoned line, got %+v", lines)
	}
}
