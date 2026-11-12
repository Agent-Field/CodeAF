package remote

// The clock a surface defers while it holds a question nothing waits on
// ([MethodQuestionClock], internal/session's askclock.go).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// clockingAgent is an [askingAgent] with the session's clock door.
type clockingAgent struct {
	*askingAgent
	calls []string
}

func (a *clockingAgent) DeferQuestionClock(kind session.QuestionKind, token string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "defer "+string(kind)+":"+token)
	for i := range a.open {
		if a.open[i].Kind == kind && a.open[i].Token() == token && !a.open[i].Deadline.IsZero() {
			a.open[i].Deadline = time.Time{}
			return true
		}
	}
	return false
}

func (a *clockingAgent) StartQuestionClock(kind session.QuestionKind, token string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "start "+string(kind)+":"+token)
	for i := range a.open {
		if a.open[i].Kind == kind && a.open[i].Token() == token && a.open[i].Deadline.IsZero() {
			a.open[i].Deadline = askedAt.Add(time.Hour)
			return true
		}
	}
	return false
}

// THE CLOCK CROSSES AND ANSWERS WITH THE ENGINE'S OWN DEADLINE, so the surface
// draws the engine's countdown and not its own guess.
func TestAQuestionsClockIsDeferredAndStartedAcrossTheWire(t *testing.T) {
	asked := wholeQuestion()
	asked.Deadline = askedAt.Add(time.Minute)
	far := &clockingAgent{askingAgent: newAskingAgent(asked)}
	loop := laneLoop(t, far)
	ctx := context.Background()

	reply, err := loop.Client.Agent().ClockQuestion(ctx, session.QuestionAsk, asked.Token(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !reply.Changed || !reply.Deadline.IsZero() {
		t.Fatalf("deferring answered %+v", reply)
	}
	reply, err = loop.Client.Agent().ClockQuestion(ctx, session.QuestionAsk, asked.Token(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !reply.Changed || !reply.Deadline.Equal(askedAt.Add(time.Hour)) {
		t.Fatalf("starting answered %+v", reply)
	}
	reply, err = loop.Client.Agent().ClockQuestion(ctx, session.QuestionAsk, asked.Token(), true)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Changed || !reply.Deadline.Equal(askedAt.Add(time.Hour)) {
		t.Fatalf("starting a running clock answered %+v", reply)
	}
	if got := strings.Join(far.calls, ", "); got != "defer ask:7, start ask:7, start ask:7" {
		t.Fatalf("the engine heard %s", got)
	}
}

// AN ENGINE WITHOUT THE DOOR REFUSES, and never answers a quiet yes: a surface
// told a clock was deferred would hold a question whose clock still runs.
func TestAnEngineThatCannotDeferAClockRefuses(t *testing.T) {
	asked := wholeQuestion()
	loop := laneLoop(t, newAskingAgent(asked))
	if _, err := loop.Client.Agent().ClockQuestion(context.Background(), session.QuestionAsk, asked.Token(), false); err == nil {
		t.Fatal("an engine with no clock door said the clock was deferred")
	}
}
