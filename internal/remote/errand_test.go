package remote

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

type errandAgent struct {
	*fakeAgent
	asked []session.ErrandAsk
	took  time.Duration
	fail  error
}

func (a *errandAgent) RunErrand(_ context.Context, ask session.ErrandAsk) (session.ErrandAnswer, error) {
	a.asked = append(a.asked, ask)
	time.Sleep(a.took)
	if a.fail != nil {
		return session.ErrandAnswer{}, a.fail
	}
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

// AN OWNER WITHOUT ERRANDS SAYS SO IN WORDS, so the app's log can tell it
// apart from a link that gave up; the app still answers unavailable.
func TestAnOwnerWithoutErrandsSaysSo(t *testing.T) {
	loop := askLoop(t, &fakeAgent{model: "m"})
	_, err := loop.Client.Agent().RunErrand(t.Context(), session.ErrandAsk{Purpose: "status"})
	if err == nil || !strings.Contains(err.Error(), "does not run errands") {
		t.Fatalf("an owner without errands answered %v", err)
	}
}

// AN ERRAND WAITS AS LONG AS IT ASKED TO, NOT A GETTER'S TEN SECONDS. A brief
// is a model call of up to its own deadline; a near end that gave up at
// [callDeadline] turned every slow answer into "unavailable".
func TestAnErrandWaitsOutItsOwnDeadlineAcrossTheHop(t *testing.T) {
	t.Parallel()
	far := &errandAgent{fakeAgent: &fakeAgent{model: "m"}, took: callDeadline + time.Second}
	loop := askLoop(t, far)
	ask := session.ErrandAsk{Purpose: "brief", Deadline: 3 * callDeadline, Messages: []session.ErrandMessage{{Role: "user", Text: "x"}}}
	got, err := loop.Client.Agent().RunErrand(t.Context(), ask)
	if err != nil || got.Text != "done" {
		t.Fatalf("a slow errand inside its deadline was not waited for: %+v %v", got, err)
	}
}

// A FAR END'S REFUSAL OF THE ASK KEEPS ITS KIND AND ITS WORDS over the hop,
// the way a route refusal does, so the caller is told why and not "unavailable".
func TestAnErrandAskRefusalCrossesTheHopWithItsReason(t *testing.T) {
	far := &errandAgent{fakeAgent: &fakeAgent{model: "m"}, fail: fmt.Errorf("%w: messages[0].text is empty", session.ErrErrandAsk)}
	loop := askLoop(t, far)
	ask := session.ErrandAsk{Purpose: "brief", Messages: []session.ErrandMessage{{Role: "user", Text: "x"}}}
	_, err := loop.Client.Agent().RunErrand(t.Context(), ask)
	if !errors.Is(err, session.ErrErrandAsk) || !strings.Contains(err.Error(), "messages[0].text is empty") {
		t.Fatalf("the far end's refusal lost its kind or its words: %v", err)
	}
}
