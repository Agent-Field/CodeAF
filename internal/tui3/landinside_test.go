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

func assertLandingAfterProse1556(t *testing.T, a *app, title, prose string, id uint64, crewRequired bool) {
	t.Helper()
	view := taskText(a)
	card := "◆ " + title
	crew := "task " + itoa(int(id)) + " crew · "
	t.Run("card follows final prose once", func(t *testing.T) {
		for _, want := range []string{prose, card} {
			if !strings.Contains(view, want) {
				t.Fatalf("the settled conversation is missing %q:\n%s", want, view)
			}
		}
		if got := strings.Count(view, card); got != 1 {
			t.Fatalf("the landing has %d cards, want one:\n%s", got, view)
		}
		if strings.Index(view, card) <= strings.Index(view, prose) {
			t.Fatalf("the card did not move after the turn's final prose:\n%s", view)
		}
	})
	if crewRequired {
		t.Run("crew follows final prose once", func(t *testing.T) {
			for _, want := range []string{crew} {
				if !strings.Contains(view, want) {
					t.Fatalf("the settled conversation is missing %q:\n%s", want, view)
				}
			}
			if !strings.Contains(strings.Join(strings.Fields(view), " "), "not right? /redo stronger") {
				t.Fatalf("the crew line lost its redo action:\n%s", view)
			}
			if got := strings.Count(view, crew); got != 1 || strings.Index(view, crew) <= strings.Index(view, prose) {
				t.Fatalf("the crew line appears %d times or precedes the final prose:\n%s", got, view)
			}
		})
		if strings.Index(view, crew) >= strings.Index(view, card) {
			t.Fatalf("the crew line no longer precedes its card:\n%s", view)
		}
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
		view := taskText(a)
		first, second := "◆ "+title, "◆ "+later
		if strings.Count(view, first) != 1 || strings.Count(view, second) != 1 ||
			strings.Index(view, first) >= strings.Index(view, second) ||
			strings.Index(view, second) <= strings.Index(view, prose) ||
			strings.Count(view, "task 2 crew · ") != 1 || strings.Count(view, "task 3 crew · ") != 1 ||
			strings.Index(view, "task 3 crew · ") <= strings.Index(view, prose) ||
			strings.Index(view, "task 3 crew · ") >= strings.Index(view, second) {
			t.Fatalf("the two tasks lost their one-card landing order:\n%s", view)
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
