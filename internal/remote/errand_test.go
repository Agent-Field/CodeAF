package remote

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

type errandAgent struct {
	*fakeAgent
	asked []session.ErrandAsk
}

func (a *errandAgent) RunErrand(_ context.Context, ask session.ErrandAsk) (session.ErrandAnswer, error) {
	a.asked = append(a.asked, ask)
	if ask.Model == "vendor/refused" {
		return session.ErrandAnswer{}, fmt.Errorf("%w: one-model mode", session.ErrErrandRoute)
	}
	return session.ErrandAnswer{Text: "done", Model: "m", Role: ask.Role, Took: time.Second}, nil
}

func TestAnErrandRunsOnTheOwningEngine(t *testing.T) {
	far := &errandAgent{fakeAgent: &fakeAgent{model: "m"}}
	loop := askLoop(t, far)
	if classify(methodErrand) != classWork {
		t.Fatal("an errand must not queue behind a turn")
	}
	agent := loop.Client.Agent()
	ask := session.ErrandAsk{Purpose: "status", Messages: []session.ErrandMessage{{Role: "user", Text: "x"}}}
	got, err := agent.RunErrand(t.Context(), ask)
	if err != nil || got.Text != "done" || got.Role != "title" || len(far.asked) != 1 || far.asked[0].Deadline != session.ErrandDefaultDeadline {
		t.Fatalf("answer=%+v err=%v asked=%+v", got, err, far.asked)
	}
	ask.Model = "vendor/refused"
	if _, err := agent.RunErrand(t.Context(), ask); !errors.Is(err, session.ErrErrandRoute) {
		t.Fatalf("a route refusal lost its kind over the wire: %v", err)
	}
}

func TestAnOwnerWithoutErrandsAnswersUnavailable(t *testing.T) {
	loop := askLoop(t, &fakeAgent{model: "m"})
	got, err := loop.Client.Agent().RunErrand(t.Context(), session.ErrandAsk{Purpose: "status"})
	if err != nil || got.Reason != "unavailable" {
		t.Fatalf("%+v %v", got, err)
	}
}
