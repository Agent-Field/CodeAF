package remote

// retakelanes_test.go pins WHAT A REPAIRED LINK OWES THE SURFACE.
//
// A turn's events survive a redial by themselves — they are numbered, the
// welcome says where this window got to, and the gap arrives as replay. The
// standing lanes are not numbered and are not replayed by the protocol: each is
// a subscription the engine hung on the CONNECTION, and the connection is what
// died. So a window whose link was repaired drew its transcript and its status
// line and looked entirely well while its rail never spoke again (#761).

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// roamingRail is a persistent conversation and a surface that redials onto it,
// with the pipes the test can cut in its hands.
type roamingRail struct {
	far      *railAgent
	client   *Client
	mu       sync.Mutex
	surfaces []io.ReadWriteCloser
}

// roamOntoARail opens one roaming surface onto one persistent conversation.
// `sessions` answers which conversation each dial lands on, so a test can stage
// an engine that came back holding something else.
func roamOntoARail(t *testing.T, far *railAgent, sessions func(dial int) *Session) *roamingRail {
	t.Helper()
	r := &roamingRail{far: far}
	dialer := func() (io.ReadWriteCloser, error) {
		r.mu.Lock()
		sess := sessions(len(r.surfaces))
		r.mu.Unlock()
		surface, engine := Pipe()
		go func() {
			_ = ServeAttach(engine, engine, AttachOptions{
				Open: func(Hello) (*Session, error) { return sess, nil },
			})
			_ = engine.Close()
		}()
		r.mu.Lock()
		r.surfaces = append(r.surfaces, surface)
		r.mu.Unlock()
		return surface, nil
	}
	client, err := Roam("devbox", Hello{Workspace: "app"}, Roaming{Dial: dialer, Window: 5 * time.Second})
	if err != nil {
		t.Fatalf("roam: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	r.client = client
	return r
}

// cut kills the link the surface is on and waits for the redial to land.
func (r *roamingRail) cut(t *testing.T) {
	t.Helper()
	r.mu.Lock()
	first, was := r.surfaces[0], len(r.surfaces)
	r.mu.Unlock()
	_ = first.Close()
	waitFor(t, "the redial", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return len(r.surfaces) > was
	})
}

// railSession is one persistent conversation writing one named journal.
func railSession(far *railAgent, journal string) *Session {
	return NewSession(&Engine{Agent: far, Workspace: "/srv/app", SessionFile: journal}, true)
}

// THE BUG THIS PINS (#761): a window whose link was repaired — a wifi blip, a
// lid closed, an engine host replaced under it — kept its transcript and its
// status line and lost its rail for the rest of the session. The engine went on
// running the conversation's tasks; the column drew `+ /task` and nothing else,
// which is an empty roster inviting the person to start the same work twice.
func TestARepairedLinkPutsTheRailBackOnTheSurface(t *testing.T) {
	far := &railAgent{fakeAgent: &fakeAgent{model: "a/b"}}
	sess := railSession(far, "/srv/app/j.jsonl")
	r := roamOntoARail(t, far, func(int) *Session { return sess })

	lane, stop := r.client.Agent().WatchTaskUpdates()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the surface's task lane", func() bool { return far.opened() == 1 })

	far.land(taskEvent(1, "research packages", session.TaskRunning))
	if ev := nextTask(t, lane); ev.Task == nil || ev.Task.ID != 1 {
		t.Fatalf("before the drop the rail carried %+v", ev.Task)
	}

	r.cut(t)
	// THE ENGINE IS ASKED AGAIN, which is the whole fix: the old subscription
	// belonged to the connection that died.
	waitFor(t, "the engine reopened the surface's task lane", func() bool { return far.opened() == 2 })

	// AND THE ROSTER COMES WITH IT. The row was already running when the link
	// broke, so a window that came back to an empty column would be a window
	// lying about work the engine can see.
	back := nextTask(t, lane)
	if back.Task == nil || back.Task.ID != 1 || back.Task.State != session.TaskRunning {
		t.Fatalf("the repaired link replayed %+v, want row 1 running", back.Task)
	}

	// And what happens NEXT reaches it too, which is what says the lane is live
	// rather than merely replayed.
	far.land(taskEvent(1, "research packages", session.TaskDone))
	if done := nextTask(t, lane); done.Task == nil || done.Task.State != session.TaskDone {
		t.Fatalf("after the repair the landing arrived as %+v", done.Task)
	}
}

// A CONVERSATION THAT WAS SWITCHED UNDER THE WINDOW IS NOT RE-WATCHED. The
// person has just been told that what is on screen belongs to the conversation
// this window left, and replaying the REPLACEMENT's roster into that column
// would draw somebody else's work under the old name.
func TestARepairedLinkLeavesASwitchedConversationsRailAlone(t *testing.T) {
	far := &railAgent{fakeAgent: &fakeAgent{model: "a/b"}}
	first := railSession(far, "/srv/app/one.jsonl")
	second := railSession(far, "/srv/app/two.jsonl")
	r := roamOntoARail(t, far, func(dial int) *Session {
		if dial == 0 {
			return first
		}
		return second
	})

	lane, stop := r.client.Agent().WatchTaskUpdates()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the surface's task lane", func() bool { return far.opened() == 1 })

	r.cut(t)
	waitFor(t, "the surface to learn which conversation came back", func() bool {
		return r.client.Welcome().SessionFile == "/srv/app/two.jsonl"
	})
	// The engine is given long enough to have opened a second subscription if it
	// were ever going to: silence here is the assertion.
	time.Sleep(500 * time.Millisecond)
	if opened := far.opened(); opened != 1 {
		t.Fatalf("the engine opened %d task lanes across a conversation switch, want 1", opened)
	}
	select {
	case ev := <-lane:
		t.Fatalf("the replacement conversation's rail reached the old column: %+v", ev.Task)
	default:
	}
}

// A LANE THIS SURFACE NEVER OPENED IS NOT OPENED ON A REPAIRED LINK EITHER. A
// subscription nobody reads is the engine pumping frames at a nil stream.
func TestARepairedLinkOpensNoLaneTheSurfaceNeverHeld(t *testing.T) {
	far := &railAgent{fakeAgent: &fakeAgent{model: "a/b"}}
	sess := railSession(far, "/srv/app/j.jsonl")
	r := roamOntoARail(t, far, func(int) *Session { return sess })

	r.cut(t)
	waitFor(t, "the surface to be welcomed back", func() bool {
		return r.client.Welcome().SessionFile == "/srv/app/j.jsonl"
	})
	time.Sleep(500 * time.Millisecond)
	if opened := far.opened(); opened != 0 {
		t.Fatalf("the engine opened %d task lanes for a surface that draws no rail", opened)
	}
}

// ROAD TWO OF #761, RULED OUT RATHER THAN BUILT AGAINST: an engine on another
// protocol version does not quietly drop the frames it does not understand — it
// refuses the hello, in a sentence, before a single lane exists. There is no
// half-attached surface for a rail to go missing on.
func TestAnEngineOnAnotherProtocolRefusesRatherThanDroppingFrames(t *testing.T) {
	far := &railAgent{fakeAgent: &fakeAgent{model: "a/b"}}
	sess := railSession(far, "/srv/app/j.jsonl")
	l := dialSession(t, sess)
	answer := l.hello(Hello{Version: Version - 1, Workspace: "app"})
	if answer.Kind != "fatal" {
		t.Fatalf("an engine speaking another protocol answered a %q frame", answer.Kind)
	}
	if !strings.Contains(answer.Error, "speaks protocol") {
		t.Fatalf("the refusal reads %q", answer.Error)
	}
	if far.opened() != 0 {
		t.Fatal("a refused hello opened a task lane")
	}
}
