package remote

// A STANDING RAIL BELONGS TO THE CONVERSATION IT WAS OPENED ON.
//
// [Session.serving] closed the wrong-owner hole for CALLS: a joined reader
// asking about task 7 after the owner opened something else is refused rather
// than answered by the replacement's task 7. The standing lane had the same hole
// and no such guard, and it was worse in two ways — nobody asks for the frames,
// and [Session.retakeTaskLanes] reopens every subscribed surface on the
// replacement agent by itself. So a guest sat there being pushed another
// conversation's rows, under the name of the one it joined, with no call
// anywhere to be refused.
//
// The task ids below are 7 in BOTH conversations on purpose. That collision is
// the whole failure: the rows are indistinguishable on the wire, and the only
// thing that can tell them apart is which conversation the lane was opened on.

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// twoConversations is one session that can be moved from `one.jsonl` to
// `two.jsonl` (an open) or to a fresh transcript (a new), with a rail agent
// behind each. It is the shape a person's window and somebody else's reader
// actually share.
type twoConversations struct {
	sess   *Session
	first  *railAgent
	second *railAgent
	fresh  *railAgent
}

const (
	joinedTranscript   = "/srv/app/.aforge/one.jsonl"
	replacedTranscript = "/srv/app/.aforge/two.jsonl"
	freshTranscript    = "/srv/app/.aforge/three.jsonl"
)

func twoConversationSession() *twoConversations {
	lab := &twoConversations{
		first:  &railAgent{fakeAgent: &fakeAgent{}},
		second: &railAgent{fakeAgent: &fakeAgent{}},
		fresh:  &railAgent{fakeAgent: &fakeAgent{}},
	}
	lab.sess = NewSession(&Engine{
		Agent:       lab.first,
		Workspace:   "/srv/app",
		SessionFile: joinedTranscript,
		Open: func(string) (WrappedAgent, bool, error) {
			return lab.second, true, nil
		},
		Fresh: func() (WrappedAgent, string, error) {
			return lab.fresh, freshTranscript, nil
		},
	}, true)
	return lab
}

// guestOn dials a reader that JOINED this conversation: bound to the transcript
// it found open, and never a driver. It is [viewOn] with the hello a second
// person's window sends.
func guestOn(t *testing.T, sess *Session) *Client {
	t.Helper()
	surface, engine := Pipe()
	go func() {
		_ = ServeAttach(engine, engine, AttachOptions{
			Open: func(Hello) (*Session, error) { return sess, nil },
		})
		_ = engine.Close()
	}()
	client, err := Dial(surface, "loopback", Hello{
		Version: Version, Surface: "reader",
		Session: joinedTranscript, Join: true, Watch: true,
	})
	if err != nil {
		_ = surface.Close()
		t.Fatalf("dial the joined conversation: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// lanesOpen is how many subscriptions this engine is holding.
func lanesOpen(agent *railAgent) int {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	return len(agent.lanes)
}

// noTask insists nothing arrives on a lane. A CLOSED lane is not a delivery: the
// fault this pins is a row being sent, never a lane ending.
func noTask(t *testing.T, lane <-chan session.Event, within time.Duration) {
	t.Helper()
	select {
	case event, open := <-lane:
		if !open {
			return
		}
		t.Fatalf("a bound rail was pushed %+v from the conversation that replaced its own", event.Task)
	case <-time.After(within):
	}
}

// THE FAILURE, PINNED. A guest joins, watches, and is drawing task 7 of the
// conversation it joined. The owner opens another conversation in the same
// session — which has a task 7 of its own — and the guest must be told nothing
// further rather than be pushed the replacement's rows.
func TestAJoinedRailIsNotRewiredOntoTheReplacementConversation(t *testing.T) {
	lab := twoConversationSession()
	owner := viewOn(t, lab.sess)
	guest := guestOn(t, lab.sess)

	lane, stop := guest.Agent().WatchTaskUpdates()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the guest's task lane", func() bool { return lab.first.opened() == 1 })

	lab.first.land(taskEvent(7, "port the parser", session.TaskRunning))
	if event := nextTask(t, lane); event.Task == nil || event.Task.ID != 7 {
		t.Fatalf("the guest's own conversation's row = %+v", event.Task)
	}

	// The owner opens something else in the same session. The swap reopens every
	// subscribed surface on the replacement, and the guest is the one that must
	// not be reopened.
	if _, err := owner.OpenSession(replacedTranscript); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	// THE OLD SUBSCRIPTION IS ENDED — this is the same act that decides whether a
	// new one is opened, so observing it means the decision has been made.
	waitFor(t, "the guest's lane on the conversation it joined ended", func() bool {
		return lanesOpen(lab.first) == 0
	})
	if got := lab.second.opened(); got != 0 {
		t.Fatalf("the replacement conversation opened %d lane(s) for a guest that joined another one", got)
	}

	// AND ITS TASK 7 — a different piece of work with the same number — reaches
	// nobody.
	lab.second.land(taskEvent(7, "rewrite the loader", session.TaskDone))
	noTask(t, lane, 250*time.Millisecond)
}

// AND ASKING AGAIN DOES NOT GET IT EITHER. The guest's own surface may reopen
// its lane at any time; the engine refuses on the same reading rather than
// letting a fresh [MethodTaskWatch] round the guard the rewire honoured.
func TestAJoinedRailAskingAgainAfterTheSwapIsStillRefused(t *testing.T) {
	lab := twoConversationSession()
	owner := viewOn(t, lab.sess)
	guest := guestOn(t, lab.sess)

	guest.Agent().WatchTaskUpdates()
	waitFor(t, "the engine opened the guest's task lane", func() bool { return lab.first.opened() == 1 })

	if _, err := owner.OpenSession(replacedTranscript); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	waitFor(t, "the guest's first lane ended", func() bool { return lanesOpen(lab.first) == 0 })

	// The guest's own surface asks again, which is a fresh [MethodTaskWatch] and
	// not the rewire. It must meet the same reading.
	lane, again := guest.Agent().WatchTaskUpdates()
	t.Cleanup(again)
	lab.second.land(taskEvent(7, "rewrite the loader", session.TaskRunning))

	// THE SILENCE IS THE ASSERTION, and the window it is measured over is also
	// what gives the engine time to have answered the second watch.
	noTask(t, lane, 250*time.Millisecond)
	if got := lab.second.opened(); got != 0 {
		t.Fatalf("asking again opened %d lane(s) on a conversation this reader never joined", got)
	}
}

// A FRESH CONVERSATION IS THE SAME FACT BY THE OTHER DOOR. `/new` numbers its
// first task 7 as well.
func TestAJoinedRailIsNotRewiredOntoANewConversation(t *testing.T) {
	lab := twoConversationSession()
	owner := viewOn(t, lab.sess)
	guest := guestOn(t, lab.sess)

	lane, stop := guest.Agent().WatchTaskUpdates()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the guest's task lane", func() bool { return lab.first.opened() == 1 })

	if _, err := owner.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	waitFor(t, "the guest's lane on the conversation it joined ended", func() bool {
		return lanesOpen(lab.first) == 0
	})
	if got := lab.fresh.opened(); got != 0 {
		t.Fatalf("a new conversation opened %d lane(s) for a guest that joined the old one", got)
	}
	lab.fresh.land(taskEvent(7, "the first task of a new conversation", session.TaskRunning))
	noTask(t, lane, 250*time.Millisecond)
}

// AND AN ORDINARY WINDOW STILL FOLLOWS ITS CONVERSATION, which is the behaviour
// this guard must not take away: the person opened something else themselves,
// and their own rail goes with them.
func TestAnOrdinaryHostedRailStillFollowsTheConversationItsPersonOpened(t *testing.T) {
	lab := twoConversationSession()
	owner := viewOn(t, lab.sess)

	lane, stop := owner.Agent().WatchTaskUpdates()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the window's task lane", func() bool { return lab.first.opened() == 1 })

	if _, err := owner.OpenSession(replacedTranscript); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	waitFor(t, "the window's rail was rewired onto the conversation it opened", func() bool {
		return lab.second.opened() == 1
	})

	lab.second.land(taskEvent(7, "rewrite the loader", session.TaskRunning))
	if event := nextTask(t, lane); event.Task == nil || event.Task.ID != 7 {
		t.Fatalf("the window's own rail = %+v, want the conversation it just opened", event.Task)
	}
}

// AND THE CONVERSATION THAT WAS REPLACED CANNOT REACH THE GUEST EITHER. Its
// agent is still alive for as long as anything holds it, and a row it emits
// after the swap belongs to work this session is no longer running.
func TestTheReplacedConversationCanNoLongerReachTheGuestsRail(t *testing.T) {
	lab := twoConversationSession()
	owner := viewOn(t, lab.sess)
	guest := guestOn(t, lab.sess)

	lane, stop := guest.Agent().WatchTaskUpdates()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the guest's task lane", func() bool { return lab.first.opened() == 1 })

	// The conversation is replaced first, and the old agent then speaks onto the
	// subscription the guest was holding.
	if _, err := owner.OpenSession(replacedTranscript); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	waitFor(t, "the guest's lane on the conversation it joined ended", func() bool {
		return lanesOpen(lab.first) == 0
	})
	lab.first.land(taskEvent(7, "a row from the conversation that is gone", session.TaskDone))
	noTask(t, lane, 250*time.Millisecond)
}
