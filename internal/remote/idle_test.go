package remote

// THE BUG THESE PIN: a conversation whose turn had ENDED but whose work had not
// read as idle. A task, an adaptive run and a background job all outlive the
// turn that started them — the ring is dropped the moment the stream closes —
// so the session went quiet on paper while three workers ran on, and the host's
// sweep closed it half an hour later with the work inside it.

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// busyAgent is a conversation that says what it is working on. The rows are
// whatever the test sets: one live root is enough, exactly as it is in the real
// reading ([session.Agent.WorkingNow]).
type busyAgent struct {
	*fakeAgent
	work []session.WorkNode
}

func (a *busyAgent) WorkingNow() []session.WorkNode { return a.work }

func idleSession(agent WrappedAgent) *Session {
	return NewSession(&Engine{Agent: agent, Workspace: "/srv/app"}, true)
}

func TestAConversationWithWorkInItIsNotIdleAfterItsTurnEnded(t *testing.T) {
	far := &busyAgent{fakeAgent: &fakeAgent{}}
	sess := idleSession(far)

	// Nobody attached, no turn streaming, nothing waiting to be answered: the
	// state this package can see is completely quiet.
	if sess.IdleSince().IsZero() {
		t.Fatal("a conversation with no surface, no turn and no work read as busy")
	}

	far.work = []session.WorkNode{{ID: "task:4", Title: "port the parser", State: session.WorkRunning}}
	if !sess.IdleSince().IsZero() {
		t.Fatal("a conversation still running a task read as idle — the sweep would close it and take the work")
	}

	// A node waiting for a free hand is work that has not started, not work that
	// is over.
	far.work = []session.WorkNode{{ID: "task:5", Title: "waiting for a hand", State: session.WorkWaiting}}
	if !sess.IdleSince().IsZero() {
		t.Fatal("a conversation with queued work read as idle")
	}

	// And when the work lands, the clock starts — the conversation is kept for
	// its idle span rather than for ever.
	far.work = nil
	if sess.IdleSince().IsZero() {
		t.Fatal("a conversation whose work has all landed never went idle")
	}
}

// AN ENGINE THAT CANNOT ANSWER IS READ AS IT ALWAYS WAS. A scripted agent with
// no graph is not kept alive on suspicion.
func TestAnEngineWithNoWorkReadingIsIdleOnTheOldTerms(t *testing.T) {
	sess := idleSession(&fakeAgent{})
	if sess.IdleSince().IsZero() {
		t.Fatal("an engine with no work door read as busy for ever")
	}
}

// A QUESTION NOBODY HAS ANSWERED KEEPS THE CONVERSATION, which is the half that
// was already true and must stay true beside the work reading.
func TestAWaitingQuestionKeepsAConversationAlive(t *testing.T) {
	sess := idleSession(&fakeAgent{})
	sess.mu.Lock()
	sess.held.raise(WireEvent(session.Event{Kind: session.EventConsentRequest, ID: 3}), 1, true)
	sess.mu.Unlock()
	if !sess.IdleSince().IsZero() {
		t.Fatal("a conversation holding an unanswered card read as idle")
	}

	sess.mu.Lock()
	sess.held.answered(HeldConsent, 3, "")
	sess.mu.Unlock()
	if sess.IdleSince().IsZero() {
		t.Fatal("a conversation whose card was answered never went idle")
	}
}

// THE READING IS TAKEN OFF THE SESSION LOCK, so a slow walk of the graph cannot
// hold up the events a running turn is recording. A door that blocks until the
// test lets it go proves it: the session stays usable while the sweep is asking.
func TestTheWorkReadingDoesNotHoldTheSessionLock(t *testing.T) {
	far := &blockingWorkAgent{fakeAgent: &fakeAgent{}, entered: make(chan struct{}), release: make(chan struct{})}
	sess := idleSession(far)

	answered := make(chan time.Time, 1)
	go func() { answered <- sess.IdleSince() }()
	<-far.entered

	// Somebody arrives while the walk is still going. This would deadlock if the
	// reading were taken under sess.mu.
	done := make(chan int, 1)
	go func() { done <- sess.Attached() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the session was locked while the work reading was in flight")
	}
	close(far.release)
	if idle := <-answered; !idle.IsZero() {
		t.Fatal("a conversation whose door reported work read as idle")
	}
}

type blockingWorkAgent struct {
	*fakeAgent
	entered chan struct{}
	release chan struct{}
	once    bool
}

func (a *blockingWorkAgent) WorkingNow() []session.WorkNode {
	if !a.once {
		a.once = true
		close(a.entered)
		<-a.release
	}
	return []session.WorkNode{{ID: "task:1", State: session.WorkRunning}}
}
