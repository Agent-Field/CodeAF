package tui3

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// summaryHeldAgent is a plan whose summary does not come back until the test
// lets it: the model the real one asks, with its budget measured in seconds.
type summaryHeldAgent struct {
	*planFake
	asked   chan struct{}
	release chan struct{}

	mu    sync.Mutex
	order []string
}

func (h *summaryHeldAgent) PlanPause(id string) error {
	h.mu.Lock()
	h.order = append(h.order, "pause")
	h.mu.Unlock()
	return h.planFake.PlanPause(id)
}

func (h *summaryHeldAgent) PlanResume(id string) error {
	h.mu.Lock()
	h.order = append(h.order, "resume")
	h.mu.Unlock()
	return h.planFake.PlanResume(id)
}

func (h *summaryHeldAgent) PlanRunSummary(string) (session.RunPlanSummary, bool) {
	return session.RunPlanSummary{}, true
}

func (h *summaryHeldAgent) RefreshRunSummary(context.Context, string, time.Time) (session.RunPlanSummary, bool) {
	close(h.asked)
	<-h.release
	return session.RunPlanSummary{What: "the run in a sentence"}, true
}

// NOTHING A PERSON DID NOT PRESS STANDS IN THE ORDERED LINE.
//
// The line exists so the engine sees a person's gestures in the order they were
// made. The run's summary is a sentence a model writes, with a budget of ten
// seconds, that nobody pressed for; asked through the same line, it stood in
// front of whatever a person did next. Measured on a real screen: a press on a
// run's row waited 7.6 seconds for a read that took two milliseconds, and a
// stop or a pause pressed in that window would have waited the same.
//
// THE SUMMARY IS HELD ON A CHANNEL, never a sleep: the pause must be answered
// while the summary is provably still out, and the only clock here is the one
// that turns a line that never moves into a failure instead of a hang.
func TestAGestureIsAnsweredWhileTheRunsSummaryIsStillOut(t *testing.T) {
	row := session.PlanTaskRow{ID: "t-alpha", Title: "Alpha", Status: "claimed"}
	a, fake := planAppWith(t, []session.PlanTaskRow{row}, map[string]session.PlanTaskPage{"t-alpha": {Row: row}})
	held := &summaryHeldAgent{planFake: fake, asked: make(chan struct{}), release: make(chan struct{})}
	a.agent = held
	a.doorLine = newDoorLine()
	defer a.doorLine.close()
	a.taskSheet.mine.plan = []session.PlanTaskRow{row}

	summary := a.refreshRunSummary()
	if summary == nil {
		t.Fatal("the fixture did not ask for the run's summary")
	}
	summarySaid := make(chan tea.Msg, 1)
	go func() { summarySaid <- summary() }()
	<-held.asked

	pause := a.taskPlanVerb(func(agent planAgent) error { return agent.PlanPause("t-alpha") })
	if pause == nil {
		t.Fatal("the fixture did not ask for the pause")
	}
	pauseSaid := make(chan tea.Msg, 1)
	go func() { pauseSaid <- pause() }()

	select {
	case <-pauseSaid:
	case <-summarySaid:
		t.Fatal("the summary came back first, and nothing let it go")
	case <-time.After(5 * time.Second):
		close(held.release)
		t.Fatal("a pause pressed while the run's summary was out waited behind it")
	}
	if len(fake.paused) != 1 || fake.paused[0] != "t-alpha" {
		t.Fatalf("the pause did not reach the plan: %v", fake.paused)
	}
	// AND MOVING THE SUMMARY OUT DID NOT LOOSEN THE LINE. Two gestures made one
	// after the other, with the summary still out, reach the plan in the order
	// they were made ([TestTheDoorsAreAskedInTheOrderTheKeysWerePressed] holds
	// the line itself to that over thirty-two asks).
	var said []chan tea.Msg
	for _, verb := range []func(planAgent) error{
		func(agent planAgent) error { return agent.PlanResume("t-alpha") },
		func(agent planAgent) error { return agent.PlanPause("t-alpha") },
		func(agent planAgent) error { return agent.PlanResume("t-alpha") },
	} {
		cmd := a.taskPlanVerb(verb)
		done := make(chan tea.Msg, 1)
		said = append(said, done)
		go func() { done <- cmd() }()
	}
	for _, done := range said {
		<-done
	}
	held.mu.Lock()
	order := strings.Join(held.order, " ")
	held.mu.Unlock()
	if order != "pause resume pause resume" {
		t.Fatalf("the gestures reached the plan as %q, want the order they were made in", order)
	}
	close(held.release)
	if msg, ok := (<-summarySaid).(doorMsg); !ok || msg.fold == nil {
		t.Fatalf("the summary's answer lost its fold: %#v", msg)
	}
}
