package session

// A SURFACE'S CHOICE CLOCK RUNS ONLY ONCE A SURFACE HAS DRAWN THE CARD. The
// desktop launches every agent with a 12 s ChoiceClockSeconds; an agent whose
// card no surface drew (an errand started from another agent's chat) had its
// suggestion taken 12 s later for a person who never saw the question.

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func surfaceClockAgent(t *testing.T) *Agent {
	t.Helper()
	root := t.TempDir()
	a, err := newAgent(Config{Workspace: root, Place: Place{Dir: root, Workspace: root}, Model: "test/model", Interactive: true, ChoiceClockSeconds: 1}, &scriptedCompleter{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

// Undrawn, a suggested choice carries no clock and nothing takes it; drawn
// ("asks.shown"), it gets the surface's whole length from then and the clock
// takes the pick as before.
func TestASurfaceChoiceClockWaitsForTheCardToBeDrawn(t *testing.T) {
	a := surfaceClockAgent(t)
	if _, _, err := a.executeAsk(context.Background(), json.RawMessage(askNotWaiting)); err != nil {
		t.Fatal(err)
	}
	q := waitForOneQuestion(t, a)
	if !q.Deadline.IsZero() {
		t.Fatalf("an undrawn choice says its clock ends at %v", q.Deadline)
	}
	time.Sleep(1300 * time.Millisecond)
	a.askClockRanOut(q.ID)
	if notes := askDelivered(a); len(notes) > 0 {
		t.Fatalf("the clock picked for somebody no surface showed the question to: %v", notes)
	}
	drawn := time.Now()
	if !a.StartQuestionClock(QuestionAsk, q.Token()) {
		t.Fatal("drawing the card did not start its clock")
	}
	if got := waitForOneQuestion(t, a); got.Deadline.Before(drawn.Add(time.Second)) {
		t.Fatalf("the clock did not get its whole length from being drawn: %v", got.Deadline)
	}
	a.askClockRanOut(q.ID)
	if notes := askDelivered(a); len(notes) == 0 {
		t.Fatal("the drawn card's clock never took the pick")
	}
}

// A blocking choice no surface drew never auto-picks: the turn stays parked
// on the person.
func TestABlockingChoiceNoSurfaceDrewNeverAutoPicks(t *testing.T) {
	a := surfaceClockAgent(t)
	done := make(chan string, 1)
	go func() {
		out, _, _ := a.executeAsk(context.Background(), json.RawMessage(askWaiting))
		done <- out
	}()
	q := waitForOneQuestion(t, a)
	if !q.Deadline.IsZero() || !q.Blocking.Blocks() {
		t.Fatalf("want a blocking choice with no clock, got deadline %v blocks %v", q.Deadline, q.Blocking.Blocks())
	}
	select {
	case out := <-done:
		t.Fatalf("a blocking choice nobody saw was answered: %s", out)
	case <-time.After(1500 * time.Millisecond):
	}
}
