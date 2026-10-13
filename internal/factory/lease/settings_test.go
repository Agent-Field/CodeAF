package lease_test

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"testing"
	"time"
)

func TestSavedRunnerTimeoutChangesLiveLeasesAndSurvivesRestart(t *testing.T) {
	f := setup(t)
	item := f.add(t, factory.Item{Assignee: "sam"})
	held, err := f.desk.Pull(worker("runner", "sam"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.store.ChangeRunnerSettings(func(out *store.RunnerSettings) error { out.RunnerTimeoutSeconds = 90; return nil })
	if err != nil {
		t.Fatal(err)
	}
	restarted := f.open(t)
	if restarted.TTL() != 90*time.Second {
		t.Fatal("restart lost timeout")
	}
	f.now = f.now.Add(89 * time.Second)
	live, err := restarted.Live()
	if err != nil || len(live) != 1 || live[0].ID != held.ID {
		t.Fatal("lease used its former expiry", live, err)
	}
	f.now = f.now.Add(time.Second)
	live, err = restarted.Live()
	if err != nil || len(live) != 0 {
		t.Fatal("lease did not expire at saved timeout", live, err)
	}
	f.state(t, item.ID, factory.StateNeedsYou)
}
