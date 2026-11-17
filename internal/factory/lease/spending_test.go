package lease_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// THE FACTORY'S DEFAULT MODEL GOES WITH THE JOB, as its rules do: queueing
// stamps the model the factory keeps now, so a runner on another machine opens
// a step with no model of its own on it. A factory with none stamps none.
func TestLaunchStampsTheFactorysDefaultModelOnTheJob(t *testing.T) {
	f := setup(t)
	stages := []factory.Stage{{Name: "test", Kind: factory.StageCheck, Ask: "true", On: true}}
	plain := f.add(t, factory.Item{State: factory.StateNew, Stages: stages})
	doors := lease.NewDoors(f.desk)
	if err := doors.Launch(plain.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.store.Get(plain.ID); got.FactoryModel != "" {
		t.Fatalf("a factory with no default stamped %q", got.FactoryModel)
	}
	if _, err := f.store.ChangeSpending(func(s *factory.Spending) error {
		s.DefaultModel = "deepseek/deepseek-v4.1-flash"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	it := f.add(t, factory.Item{State: factory.StateNew, Stages: stages})
	if err := doors.Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.store.Get(it.ID); got.FactoryModel != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("the queued job carries %q", got.FactoryModel)
	}
}
