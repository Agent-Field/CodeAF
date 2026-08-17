package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A RESUMED CONVERSATION IS SUBSCRIBED TO ITSELF.
//
// [app.renew] ends by opening the two standing lanes — the rail's task updates
// and the turns the session starts on its own — because both belong to the agent
// that handed them over (task.go, followup.go). [app.resumeSession] does the same
// close-and-swap and returned nothing at all, so a resumed session had no pump on
// either: a node landing on it woke a real turn, the model wrote the answer, the
// events went to the journal, and the screen showed a completion card and
// silence. The person paid for a turn they never saw.
//
// Every test here drives the resume through the seam a person actually uses, and
// asserts the thing the lanes exist for: what lands reaches the screen.

// laneFake is a session with BOTH lanes: the task updates the rail reads, and
// the woken turns the feed adopts. It is [taskFake] widened by the one method
// [wakeAgent] asks for, because a real session is one object with both.
type laneFake struct {
	*taskFake
	wakes chan (<-chan session.Event)
}

func (l *laneFake) Wakes() <-chan (<-chan session.Event) { return l.wakes }

func newLaneFake() *laneFake {
	return &laneFake{
		taskFake: &taskFake{
			fakeAgent: &fakeAgent{model: "m"},
			updates:   make(chan session.Event, 8),
		},
		wakes: make(chan (<-chan session.Event), 4),
	}
}

// resumeApp is a surface with a door onto one earlier conversation, and the
// agent that door hands back.
func resumeApp(t *testing.T) (*app, *laneFake) {
	t.Helper()
	next := newLaneFake()
	a := newTestApp(&fakeAgent{model: "m"})
	a.resume = func(string) (Agent, error) { return next, nil }
	a.recentSessions = func() []Session { return []Session{{Title: "the parser port", File: "/s/one.jsonl"}} }
	return a, next
}

// resumed drives one resume and then pumps whatever it handed back, with a
// woken turn already waiting on the new agent's lane.
func resumed(t *testing.T, a *app, next *laneFake, cmd tea.Cmd, said string) {
	t.Helper()
	next.wakes <- woken(text(session.EventTextDelta, said))
	drive(t, a, runCmd(cmd)...)
	drive(t, a, frameMsg{})
}

func TestResumingASessionArmsBothStandingLanes(t *testing.T) {
	a, next := resumeApp(t)

	cmd := a.resumeSession(Session{File: "/s/one.jsonl"})
	if cmd == nil {
		t.Fatal("a resume returned no work: the new conversation is subscribed to nothing")
	}
	if a.wakeLane == nil {
		t.Fatal("the wake lane was never opened on the resumed session")
	}
	if a.taskLane == nil {
		t.Fatal("the task lane was never opened on the resumed session: the rail is dead")
	}

	// THE PUMP IS ARMED, which is the half a stored channel does not prove: a
	// turn the session starts by itself has to reach the feed.
	resumed(t, a, next, cmd, "task 7 came home and its tests pass.")
	if got := plain(frame(a)); !strings.Contains(got, "task 7 came home and its tests pass.") {
		t.Fatalf("a woken turn on the resumed session never reached the screen:\n%s", got)
	}
	// And still nobody typed it.
	for _, e := range a.entries {
		if e.kind == entryUser {
			t.Fatalf("the resumed session drew a user line for a woken turn: %q", e.text)
		}
	}
}

// The rail's lane is the same fix, and it is asserted on what a person sees: a
// node pushed onto the resumed session's updates draws its row.
func TestAResumedSessionsRailIsAlive(t *testing.T) {
	a, next := resumeApp(t)
	a.width, a.height = 200, 24

	cmd := a.resumeSession(Session{File: "/s/one.jsonl"})
	next.updates <- session.Event{
		Kind: session.EventTaskUpdate,
		Task: &session.TaskNotice{ID: 7, Title: "Port the parser", State: session.TaskRunning},
	}
	drive(t, a, runCmd(cmd)...)
	drive(t, a, frameMsg{})

	if _, known := a.tasks[7]; !known {
		t.Fatalf("the resumed session never heard about the node: %v", a.tasks)
	}
}

// THE THREE DOORS ALL CARRY THE WORK BACK. The seam is only worth anything if
// every way a person opens a session returns it: the welcome box's list, a click
// on the same list, and the /resume picker.
func TestEveryDoorIntoASessionHandsBackItsLanes(t *testing.T) {
	t.Run("welcome enter", func(t *testing.T) {
		a, _ := resumeApp(t)
		a.welcome = welcome{open: true, sel: 0, recent: []Session{{File: "/s/one.jsonl"}}}
		cmd, taken := a.welcomeKey("enter")
		if !taken {
			t.Fatal("enter on a recent session was not taken")
		}
		if cmd == nil {
			t.Fatal("the welcome box opened a session and subscribed it to nothing")
		}
	})

	t.Run("welcome click", func(t *testing.T) {
		a, _ := resumeApp(t)
		a.welcome = welcome{open: true, sel: -1, recent: []Session{{File: "/s/one.jsonl"}}}
		if cmd := a.welcomePress(0); cmd == nil {
			t.Fatal("a click on a recent session subscribed it to nothing")
		}
	})

	t.Run("resume picker", func(t *testing.T) {
		a, _ := resumeApp(t)
		a.roster.start([]Session{{Title: "the parser port", File: "/s/one.jsonl"}}, "")
		if cmd := a.resumeKey(key("enter")); cmd == nil {
			t.Fatal("the picker opened a session and subscribed it to nothing")
		}
	})
}
