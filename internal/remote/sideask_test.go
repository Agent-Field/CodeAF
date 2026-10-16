package remote

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// sideAskAgent is an engine agent that answers the side ask and remembers what
// it was asked and how long it was given.
type sideAskAgent struct {
	*fakeAgent
	question string
	left     time.Duration
	calls    int
}

func (a *sideAskAgent) SideAsk(ctx context.Context, question string) (string, error) {
	a.calls++
	a.question = question
	if deadline, ok := ctx.Deadline(); ok {
		a.left = time.Until(deadline)
	}
	return `{"name": "flaky"}`, nil
}

// THE SIDE ASK CROSSES THE WIRE AS WORK. The welcome says the engine answers
// it; the question asked at this end is the one the engine's agent hears; its
// answer comes back whole; the engine's own call is bounded by no more than the
// surface waits; and it is classed as work, so a running turn neither waits on
// it nor is waited on by it.
func TestTheSideAskCrossesTheWire(t *testing.T) {
	far := &sideAskAgent{fakeAgent: &fakeAgent{model: "m"}}
	loop := askLoop(t, far)
	if !loop.Client.Welcome().SideAsk {
		t.Fatal("an engine whose agent answers the side ask does not say so")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	answer, err := loop.Client.Agent().SideAsk(ctx, "what crew?")
	if err != nil || answer != `{"name": "flaky"}` {
		t.Fatalf("the answer: %q, %v", answer, err)
	}
	if far.calls != 1 || far.question != "what crew?" {
		t.Fatalf("the engine was asked %d times, last %q", far.calls, far.question)
	}
	if far.left <= 0 || far.left > 5*time.Second {
		t.Fatalf("the engine's ask was bounded by %v, not the surface's five seconds", far.left)
	}
	if classify(MethodSideAsk) != classWork {
		t.Fatalf("the side ask is classed %v, not as work", classify(MethodSideAsk))
	}
	if session.SideAskPatience <= callDeadline {
		t.Fatalf("the side ask waits %v, no longer than a getter", session.SideAskPatience)
	}
}

// AN ENGINE WITHOUT THE DOOR IS REFUSED AT THIS END, and the refusal never
// becomes a turn: the agent is asked nothing and sent nothing.
func TestASideAskToAnEngineWithoutItIsRefusedHere(t *testing.T) {
	far := &fakeAgent{model: "m"}
	loop := askLoop(t, far)
	if loop.Client.Welcome().SideAsk {
		t.Fatal("an engine whose agent cannot answer the side ask says it can")
	}
	if _, err := loop.Client.Agent().SideAsk(context.Background(), "what crew?"); err == nil || !strings.Contains(err.Error(), "update codeaf") {
		t.Fatalf("the refusal was %v", err)
	}
	if _, err := loop.Client.call(nil, MethodSideAsk, SideAskArgs{Question: "what crew?"}); err == nil || err.Error() != sideAskOffWord {
		t.Fatalf("the engine answered %v", err)
	}
	far.mu.Lock()
	sent := len(far.sent)
	far.mu.Unlock()
	if sent != 0 {
		t.Fatalf("a refused side ask was sent to the conversation %d times", sent)
	}
}

// AN EXPIRED SIDE ASK NEVER CALLS THE MODEL: the surface has already stopped
// waiting for the answer.
func TestAnExpiredSideAskNeverCallsTheModel(t *testing.T) {
	far := &sideAskAgent{fakeAgent: &fakeAgent{model: "m"}}
	loop := askLoop(t, far)
	if _, err := loop.Client.call(nil, MethodSideAsk, SideAskArgs{Question: "what crew?", Budget: -time.Nanosecond}); err == nil || far.calls != 0 {
		t.Fatalf("an expired side ask called the model %d times and returned %v", far.calls, err)
	}
}
