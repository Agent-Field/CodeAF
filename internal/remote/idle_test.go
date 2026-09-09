package remote

// The bug these pin: a conversation whose turn had ended but whose work had not
// read as idle. A task, an adaptive run and a background job outlive the turn
// that started them, and the ring is dropped when the stream closes — so the
// session went quiet on paper while the workers ran on, and the sweep closed it
// half an hour later with the work inside.

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

// An engine that cannot answer is read as it always was: a scripted agent with
// no graph is not kept alive on suspicion.
func TestAnEngineWithNoWorkReadingIsIdleOnTheOldTerms(t *testing.T) {
	sess := idleSession(&fakeAgent{})
	if sess.IdleSince().IsZero() {
		t.Fatal("an engine with no work door read as busy for ever")
	}
}

// A question nobody has answered keeps the conversation. That was already true
// and must stay true beside the work reading.
func TestAWaitingQuestionKeepsAConversationAlive(t *testing.T) {
	sess := idleSession(&fakeAgent{})
	sess.mu.Lock()
	sess.held.raise(WireEvent(session.Event{Kind: session.EventConsentRequest, ID: 3}), 1, nil)
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

// The reading is taken off the session lock, so a slow walk of the graph cannot
// hold up the events a running turn is recording. A door that blocks until the
// test lets it go proves it: the session stays usable while the sweep asks.
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
	// quiet makes the reading answer "nothing is working" once it unblocks, so
	// a test can stage the gap between the look and the act.
	quiet bool
}

func (a *blockingWorkAgent) WorkingNow() []session.WorkNode {
	if !a.once {
		a.once = true
		close(a.entered)
		<-a.release
	}
	if a.quiet {
		return nil
	}
	return []session.WorkNode{{ID: "task:1", State: session.WorkRunning}}
}

// Retiring is one decision and not two. A caller that read IdleSince and then
// closed would be acting on a photograph; this proves a card raised while the
// work reading is in flight cancels the retirement instead of being thrown away
// with the conversation.
func TestRetiringRechecksAfterTheWorkReading(t *testing.T) {
	far := &blockingWorkAgent{fakeAgent: &fakeAgent{}, entered: make(chan struct{}), release: make(chan struct{})}
	far.quiet = true
	sess := idleSession(far)

	answered := make(chan bool, 1)
	go func() { answered <- sess.RetireIfIdle(0) }()
	<-far.entered

	// The moment the conversation is being asked about, somebody's question
	// lands on it.
	sess.mu.Lock()
	sess.held.raise(WireEvent(session.Event{Kind: session.EventConsentRequest, ID: 4}), 1, nil)
	sess.mu.Unlock()
	close(far.release)

	if <-answered {
		t.Fatal("a conversation was retired with a card that arrived while it was being asked about")
	}
	if sess.Ended() {
		t.Fatal("the conversation was ended anyway")
	}
}

// And an idle one is actually retired, so the re-check is a check rather than a
// way of never letting go of anything.
func TestAnIdleConversationIsRetiredAndClosed(t *testing.T) {
	far := &fakeAgent{}
	sess := idleSession(far)
	if !sess.RetireIfIdle(0) {
		t.Fatal("a conversation with nobody in it and nothing running was kept")
	}
	if !sess.Ended() {
		t.Fatal("the retired conversation does not read as ended, so a host would hand it out again")
	}
	if far.closes != 1 {
		t.Fatalf("the agent was closed %d times, want once: the journal is flushed there", far.closes)
	}
	// Saying it twice changes nothing, which every road out of a host relies on.
	if !sess.RetireIfIdle(0) || far.closes != 1 {
		t.Fatalf("retiring twice closed the agent %d times", far.closes)
	}
}
