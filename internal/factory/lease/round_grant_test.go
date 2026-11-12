package lease_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

func TestWordRetrySavesItsGrantBeforeTheWorkerSignal(t *testing.T) {
	f := setup(t)
	it, held := f.running(t)
	if err := f.store.Update(it.ID, func(it *factory.Item) error {
		it.State, it.QKind, it.Question = factory.StateNeedsYou, factory.QKindOutOfAttempts, "try again?"
		it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "test", Kind: factory.StageChat, State: factory.PhaseWaiting, Round: 3, RoundLimit: 3}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := lease.NewDoors(f.desk).Answer(it.ID, false, factory.ChoiceTryOnceMore); err != nil {
		t.Fatal(err)
	}
	saved, err := f.store.Get(it.ID)
	if err != nil || saved.Stream.Phases[0].RoundLimit != 4 {
		t.Fatal("service acknowledged a retry without its durable grant")
	}
	signals, err := f.desk.Drain(held.ID, "w1")
	if err != nil || len(signals) != 1 || signals[0].Words != factory.ChoiceTryOnceMore {
		t.Fatal("word-based retry was not delivered")
	}
}
