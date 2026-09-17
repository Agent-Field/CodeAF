package remote

import (
	"sync"
	"testing"
)

// closedDoor records the conversations an engine was told were over.
type closedDoor struct {
	mu     sync.Mutex
	agents []WrappedAgent
}

func (d *closedDoor) note(agent WrappedAgent) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.agents = append(d.agents, agent)
}

func (d *closedDoor) seen() []WrappedAgent {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]WrappedAgent(nil), d.agents...)
}

// closes is how many times a fake conversation was shut, read under the lock
// its own Close takes.
func closes(agent *fakeAgent) int {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	return agent.closes
}

// EVERY ROAD OUT OF A CONVERSATION REACHES THE DOOR THAT BUILT IT, ONCE, WITH
// THE AGENT THAT WAS CLOSED.
//
// The two roads are not one road. A person's own goodbye ends the conversation
// through [Session.shutDown]; /new and /resume end one through [Session.swap]
// and never touch shutDown at all. A door told only at the first would hold
// every conversation somebody opened and left for as long as the process lived,
// which on an engine daemon is every conversation it has ever served.
func TestEveryClosedConversationReachesTheEnginesClosedDoor(t *testing.T) {
	first := &fakeAgent{}
	second := &fakeAgent{}
	door := &closedDoor{}
	sess := NewSession(&Engine{
		Agent:     first,
		Workspace: "/srv/app",
		Fresh:     func() (WrappedAgent, string, error) { return second, "/srv/next.jsonl", nil },
		Closed:    door.note,
	}, true)
	window := dialSession(t, sess)
	window.hello(Hello{Version: Version})

	// The swap road: the conversation that was open is retired and the one that
	// replaced it is not.
	window.ok(1, MethodSessionNew, nil)
	if seen := door.seen(); len(seen) != 1 || seen[0] != WrappedAgent(first) {
		t.Fatalf("after a swap the door was told about %d conversation(s), want the one that was replaced", len(seen))
	}

	// The goodbye road: the conversation that is open now.
	window.ok(2, MethodClose, nil)
	seen := door.seen()
	if len(seen) != 2 || seen[1] != WrappedAgent(second) {
		t.Fatalf("after a goodbye the door was told about %d conversation(s), want the replacement as the second", len(seen))
	}

	// AND A SECOND GOODBYE SAYS NOTHING, because the conversation was already
	// over: [Session.closeFor] wins the closed flag once, and a door told twice
	// about one conversation is a door that cannot keep a count of what it holds.
	_ = sess.closeLeaving()
	if seen := door.seen(); len(seen) != 2 {
		t.Fatalf("closing an already-closed conversation told the door again: %d", len(seen))
	}
}

// AND AN ENGINE THAT NAMES NO DOOR IS NOT A CRASH. Nil is a builder that
// retains nothing — every engine written before this door existed, and every
// test engine — and it takes both roads without one.
func TestAnEngineWithNoClosedDoorStillClosesAndSwaps(t *testing.T) {
	first := &fakeAgent{}
	second := &fakeAgent{}
	sess := NewSession(&Engine{
		Agent:     first,
		Workspace: "/srv/app",
		Fresh:     func() (WrappedAgent, string, error) { return second, "/srv/next.jsonl", nil },
	}, true)
	window := dialSession(t, sess)
	window.hello(Hello{Version: Version})
	window.ok(1, MethodSessionNew, nil)
	window.ok(2, MethodClose, nil)
	if closes(first) != 1 || closes(second) != 1 {
		t.Fatalf("conversations closed %d and %d times, want once each", closes(first), closes(second))
	}
}
