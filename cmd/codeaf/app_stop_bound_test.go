package main

import (
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// THE APP'S STOP IS A BOUNDED STOP.
//
// The desktop app reported a Stop that did nothing: the Factory manager's turn
// was parked inside a call that never looked at its context, the interrupt
// changed nothing about it, and the app has no second key. So the person's
// Stop now hands the engine its bound with the stop ([appStopByPerson]) and
// the engine lets go of the turn when the bound runs out. This pins the app's
// half: the bound is asked for, and a turn the engine let go of settles as
// stopped by the person, once.

// boundedConversation is a [fakeConversation] whose stop can carry its second
// stage. Letting go is the engine closing the turn's stream, which the fake's
// hold stands for.
type boundedConversation struct {
	*fakeConversation

	mu     sync.Mutex
	graces []time.Duration
	letGo  func()
}

func (c *boundedConversation) InterruptWithin(grace time.Duration) {
	c.mu.Lock()
	c.graces = append(c.graces, grace)
	letGo := c.letGo
	c.mu.Unlock()
	if letGo != nil {
		letGo()
	}
}

func TestAppStopAsksForItsBoundAndTheTurnLetGoSettlesStopped(t *testing.T) {
	hold := make(chan struct{})
	conv := &boundedConversation{fakeConversation: &fakeConversation{hold: hold}}
	conv.letGo = sync.OnceFunc(func() { close(hold) })
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Send(agent.ID, "park on something that cannot be cancelled", ""); err != nil {
		t.Fatal(err)
	}
	if err := backend.Stop(agent.ID); err != nil {
		t.Fatalf("stop = %v", err)
	}

	conv.mu.Lock()
	graces := append([]time.Duration(nil), conv.graces...)
	conv.mu.Unlock()
	if len(graces) != 1 || graces[0] != appStopGrace {
		t.Fatalf("the stop asked for the bounds %v, want one of %s", graces, appStopGrace)
	}
	conv.fakeConversation.mu.Lock()
	plain := conv.fakeConversation.interrupted
	conv.fakeConversation.mu.Unlock()
	if plain != 0 {
		t.Fatalf("a bounded stop also sent %d plain interrupts", plain)
	}

	i := rec.waitFor(t, 5*time.Second, appTurnDone)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if d := rec.data[i].(appwire.TurnDone); d.OK || d.ErrorKind != appwire.ErrorStopped || d.Error != "stopped" {
		t.Fatalf("turn.done = %+v", d)
	}
	receipts := 0
	for _, data := range rec.data {
		if e, ok := data.(appwire.EntryAdded); ok && e.Entry.Kind == appwire.EntryFooter && e.Entry.Footer == appwire.FooterStoppedByYou {
			receipts++
		}
	}
	if receipts != 1 {
		t.Fatalf("%d stop receipts, want 1", receipts)
	}
}
