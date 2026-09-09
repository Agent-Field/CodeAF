package remote

// moved_test.go is THE MOVE, proven on the wire: a person opening a conversation
// in the terminal they are standing at, and the window they walked away from
// being told so.
//
// Every question here is asked with at least two connections onto one [Session],
// exactly as driver_test.go's are, because a move is a fact about a room.

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// movedOn is the "moved" frame a link is waiting for.
func movedOn(l *link) Moved {
	l.t.Helper()
	frame := l.await(func(f Frame) bool { return f.Kind == "moved" })
	return decode[Moved](l.t, frame.Payload)
}

// A SECOND WINDOW IS A MOVE. The window that had the conversation is told, by
// name, that another one has it now — and the sentence it will draw carries the
// way back, because the way back is the same single keystroke.
func TestASecondWindowMovesTheConversationAndTheFirstIsTold(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	desk := dialSession(t, sess)
	desk.hello(Hello{Version: Version, Surface: "macbook"})

	away := dialSession(t, sess)
	away.hello(Hello{Version: Version, Surface: "spark"})

	told := movedOn(desk)
	if told.Machine != "spark" {
		t.Fatalf("the window that was left was told %q took it, want spark", told.Machine)
	}
	if told.Here {
		t.Fatalf("a window on another machine was reported as one on this one")
	}
}

// TWO WINDOWS ON ONE MACHINE read as `another window`, which is the weaker claim
// and the one aforge already uses at home for exactly this.
func TestAMoveToTheSameMachineReadsAsAnotherWindow(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	first := dialSession(t, sess)
	first.hello(Hello{Version: Version, Surface: "macbook"})
	second := dialSession(t, sess)
	second.hello(Hello{Version: Version, Surface: "macbook"})

	told := movedOn(first)
	if !told.Here {
		t.Fatalf("a second window on this machine was reported as another machine: %+v", told)
	}
}

// AND NOTHING ABOUT THE CONVERSATION ENDS. The engine holds the session; a move
// changes who is sitting in front of it and nothing else, so the agent is not
// closed and the room still has both connections in it until one leaves.
func TestAMoveEndsNothingInTheEngine(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	desk := dialSession(t, sess)
	desk.hello(Hello{Version: Version, Surface: "macbook"})
	away := dialSession(t, sess)
	away.hello(Hello{Version: Version, Surface: "spark"})
	movedOn(desk)

	if agent.closed() != 0 {
		t.Fatal("a move closed the conversation in the engine")
	}
	// AND THE WINDOW THAT ARRIVED IS THE ONE THAT MAY TYPE.
	if result := away.call(1, MethodSubmit, SubmitArgs{Text: "go"}); result.Error != "" {
		t.Fatalf("the window that took the conversation could not type: %s", result.Error)
	}
	if result := desk.call(2, MethodSubmit, SubmitArgs{Text: "no"}); result.Error == "" {
		t.Fatal("the window that was left could still type into the conversation")
	}
}

// A READER NEVER MOVES ANYBODY. [Hello.Watch] says on arrival that this
// connection is here to read one task's journal, and a reader that emptied the
// room would be the exact opposite of what that flag promises.
//
// IT IS PROVEN BY ORDER AND NOT BY A CLOCK. A third window arrives after the
// reader, so the FIRST move the desk ever hears has to name that third window —
// a test that waited a moment and hoped nothing came would be a test about how
// fast this machine is.
func TestAWatcherArrivingMovesNobody(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	desk := dialSession(t, sess)
	desk.hello(Hello{Version: Version, Surface: "macbook"})

	reader := dialSession(t, sess)
	reader.hello(Hello{Version: Version, Surface: "reader", Watch: true, Join: true, Session: sess.engine.SessionFile})

	away := dialSession(t, sess)
	away.hello(Hello{Version: Version, Surface: "spark"})

	if told := movedOn(desk); told.Machine != "spark" {
		t.Fatalf("the first move the desk heard named %q — the reader moved it", told.Machine)
	}
}

// AND A WATCHER IS NEVER DISPLACED EITHER. It never held the conversation, and
// telling it to step back would close the page that is reading the work.
func TestAWatcherIsNeverToldItHasBeenMoved(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	reader := dialSession(t, sess)
	reader.hello(Hello{Version: Version, Surface: "reader", Watch: true, Join: true, Session: sess.engine.SessionFile})

	first := dialSession(t, sess)
	first.hello(Hello{Version: Version, Surface: "macbook"})
	second := dialSession(t, sess)
	second.hello(Hello{Version: Version, Surface: "spark"})

	// Two arrivals, so two "driver" frames — and a move would land between them
	// (the engine tells the room who drives, then tells the room it has moved).
	for round := 1; round <= 2; round++ {
		frame := reader.await(func(f Frame) bool { return f.Kind == "driver" || f.Kind == "moved" })
		if frame.Kind != "driver" {
			t.Fatalf("round %d: a reading connection was told to step back", round)
		}
	}
}

// A LINK COMING BACK IS NOT A PERSON ARRIVING. A lid closed in one city
// redialling half an hour later must not step the window in the other city back,
// for the same reason it must not take the keyboard.
func TestARedialMovesNobody(t *testing.T) {
	agent := &fakeAgent{}
	sess := heldSession(agent)

	desk := dialSession(t, sess)
	desk.hello(Hello{Version: Version, Surface: "macbook"})

	again := dialSession(t, sess)
	again.hello(Hello{Version: Version, Surface: "laptop", Back: true})

	away := dialSession(t, sess)
	away.hello(Hello{Version: Version, Surface: "spark"})

	if told := movedOn(desk); told.Machine != "spark" {
		t.Fatalf("the first move the desk heard named %q — the redial moved it", told.Machine)
	}
}

// AND THE FRAME REACHES A SURFACE AS THE ONE EVENT IT ACTS ON, on the standing
// task lane it is already reading (tasklane.go's [Client.movedFrame]).
func TestTheMoveArrivesOnTheStandingTaskLane(t *testing.T) {
	far := &fakeAgent{}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()

	lane, stop := loop.Client.Agent().WatchTaskUpdates()
	defer stop()

	loop.Client.movedFrame(mustJSON(Moved{Here: true}))
	select {
	case ev := <-lane:
		if ev.Kind != session.EventMoved {
			t.Fatalf("the lane carried %v, want the move", ev.Kind)
		}
		if ev.Text != session.MovedWord {
			t.Fatalf("the move said %q, want %q", ev.Text, session.MovedWord)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the move never reached the standing task lane")
	}
}
