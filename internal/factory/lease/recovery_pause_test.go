package lease

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestRecoveryPauseFencesOldWorkerBeforeSaving(t *testing.T) {
	desk, it, _ := pendingManagerFixture(t)
	if err := desk.store.Update(it.ID, func(cur *factory.Item) error {
		cur.QKind = factory.QKindOutOfAttempts
		cur.Stream.Phases[0].State, cur.Stream.Phases[0].Kind = factory.PhaseWaiting, factory.StageChat
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	held, err := desk.Live()
	if err != nil || len(held) != 1 {
		t.Fatalf("fixture has no active lease: %v", err)
	}
	revision, err := desk.store.Revision(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	doors := NewDoors(desk)
	injected := errors.New("lease storage unavailable")
	desk.persist = func(string, []byte) error { return injected }
	if err := doors.PauseRecoveryExpected(it.ID, revision, it.Person()); !errors.Is(err, injected) {
		t.Fatalf("lost lease write failure: %v", err)
	}
	cur, err := desk.store.Get(it.ID)
	if err != nil || factory.RecoveryPaused(cur) {
		t.Fatalf("pause became visible before the lease could be fenced: %v", err)
	}
	desk.persist = nil
	if err := doors.PauseRecoveryExpected(it.ID, revision, it.Person()); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := desk.Apply(held[0].ID, held[0].Worker, false, false, func(*factory.Item) error {
		called = true
		return nil
	}); !errors.Is(err, ErrWithdrawn) || called {
		t.Fatalf("old worker still authorized to report: called=%t, err=%v", called, err)
	}
	if _, err := doors.ChangeSteps(it.ID, func(*factory.Item) error {
		called = true
		return nil
	}); err == nil || called {
		t.Fatal("pipeline editing can change the paused decision's identity")
	}
}
