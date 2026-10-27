package lease_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// THE FACTORY'S RULES GO WITH THE JOB: queueing stamps the rules the factory
// keeps now on the job, which is how a runner on another machine learns them,
// and a rules file that cannot be read refuses the start instead of starting
// without them.
func TestLaunchStampsTheFactorysRulesOnTheJob(t *testing.T) {
	f := setup(t)
	if _, err := f.store.ChangeHouseRules(func(h *factory.HouseRules) error {
		h.Rules = []string{"don't touch auth without checking with me"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	it := f.add(t, factory.Item{State: factory.StateNew, Stages: []factory.Stage{{Name: "test", Kind: factory.StageCheck, Ask: "true", On: true}}})
	doors := lease.NewDoors(f.desk)
	if err := doors.Launch(it.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.store.Get(it.ID)
	if got.House == nil || !reflect.DeepEqual(got.House.Rules, []string{"don't touch auth without checking with me"}) {
		t.Fatalf("the queued job carries %#v", got.House)
	}

	other := f.add(t, factory.Item{State: factory.StateNew, Stages: []factory.Stage{{Name: "test", Kind: factory.StageCheck, Ask: "true", On: true}}})
	if err := os.WriteFile(filepath.Join(f.store.Root(), "factory-rules.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := doors.Launch(other.ID); err == nil {
		t.Fatal("a job started while the factory's rules could not be read")
	}
	if again, _ := f.store.Get(other.ID); again.State != factory.StateNew {
		t.Fatalf("the refused job moved to %s", again.State)
	}
}
