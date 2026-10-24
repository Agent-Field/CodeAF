package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/session"
)

// landingCrew1556 is the routed worker the task notice carries on both its
// running and final updates. The crew line is written from these notices.
func landingCrew1556() *crewroute.Decision {
	return &crewroute.Decision{Class: crewroute.Bugfix, EstUSD: 0.013, Crew: []crewroute.Pick{
		{Seat: crewroute.Worker, Model: "z-ai/glm-5.3-flash", Provider: "openrouter"},
		{Seat: crewroute.Checker, Model: "z-ai/glm-5.3-flash", Provider: "openrouter"},
	}}
}

func landingUpdate1556(id uint64, title string, state session.TaskState) session.Event {
	return update(id, title, state, session.TaskNotice{
		Crew: landingCrew1556(), Elapsed: 9 * time.Second, CostUSD: 0.004,
		Program: "senior-dev", Merge: mergeWordMerged,
	})
}

// A landing keeps its durable facts while the meaningful answer is the only
// completion content occupying the conversation.
func assertLandingAfterProse1556(t *testing.T, a *app, title, prose string, id uint64, crewRequired bool) {
	t.Helper()
	view := taskText(a)
	if !strings.Contains(view, prose) {
		t.Fatalf("meaningful answer disappeared: %s", view)
	}
	if strings.Contains(view, "◆ "+title) || strings.Contains(view, "task "+itoa(int(id))+" crew · ") {
		t.Fatalf("routine completion duplicated the answer: %s", view)
	}
	card := a.doneCardFor(id)
	if card == nil || card.title != title || !card.hidden() {
		t.Fatalf("quiet landing lost its retained identity: %+v", card)
	}
	if crewRequired && (a.tasks[id] == nil || a.tasks[id].crew == nil) {
		t.Fatal("quiet landing lost crew details")
	}
}

func TestATaskThatLandsInsideItsTurnGetsItsCardAtTheTurnsEnd(t *testing.T) {
	const title = "Count lines in store.py"
	const prose = "The line count is ready."
	a, agent, _ := taskApp(t)
	a.width, a.height = 160, 60
	agent.turns = [][]session.Event{{
		toolBegin("read", "read store.py"),
		toolEnd("read", "ready"),
		landingUpdate1556(2, title, session.TaskRunning),
		landingUpdate1556(2, title, session.TaskDone),
		toolBegin("read", "read README.md"),
		toolEnd("read", "ready"),
		text(session.EventTextDelta, prose),
		{Kind: session.EventTurnDone},
	}}
	runTurn(t, a, agent.fakeAgent, "count lines in store.py")
	assertLandingAfterProse1556(t, a, title, prose, 2, true)
	// A later copy of the same final notice must not duplicate the landing.
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: landingUpdate1556(2, title, session.TaskDone)})
	t.Run("later refresh leaves one landing", func(t *testing.T) {
		assertLandingAfterProse1556(t, a, title, prose, 2, true)
	})
	t.Run("two tasks retain landing order", func(t *testing.T) {
		const later = "Count words in store.py"
		drive(t, a, taskEventMsg{gen: a.taskGen, ev: landingUpdate1556(3, later, session.TaskRunning)})
		drive(t, a, taskEventMsg{gen: a.taskGen, ev: landingUpdate1556(3, later, session.TaskDone)})
		assertLandingAfterProse1556(t, a, title, prose, 2, true)
		assertLandingAfterProse1556(t, a, later, prose, 3, true)
		if a.doneEntryFor(2) >= a.doneEntryFor(3) {
			t.Fatal("retained landing order changed")
		}

	})
}

func TestATaskThatLandsAfterItsTurnStillGetsItsCard(t *testing.T) {
	const title = "Count lines in store.py"
	const prose = "The worker is counting the lines."
	a, agent, _ := taskApp(t)
	a.width, a.height = 160, 60
	agent.turns = [][]session.Event{{text(session.EventTextDelta, prose), {Kind: session.EventTurnDone}}}
	runTurn(t, a, agent.fakeAgent, "count lines in store.py")
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: landingUpdate1556(2, title, session.TaskRunning)})
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: landingUpdate1556(2, title, session.TaskDone)})
	assertLandingAfterProse1556(t, a, title, prose, 2, true)
}

func TestALandingInsideATurnThatFailsIsStillSaid(t *testing.T) {
	const title = "Count lines in store.py"
	const prose = "The request failed after the worker finished."
	a, agent, _ := taskApp(t)
	a.width, a.height = 160, 60
	agent.turns = [][]session.Event{{
		toolBegin("read", "read store.py"),
		toolEnd("read", "ready"),
		landingUpdate1556(2, title, session.TaskRunning),
		landingUpdate1556(2, title, session.TaskDone),
		toolBegin("read", "read README.md"),
		toolEnd("read", "ready"),
		text(session.EventTextDelta, prose),
		{Kind: session.EventError, Err: errors.New("request ended")},
	}}
	runTurn(t, a, agent.fakeAgent, "count lines in store.py")
	assertLandingAfterProse1556(t, a, title, prose, 2, true)
}
