package lease_test

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

func TestCloseRevokesLeaseAndPreservesWork(t *testing.T) {
	f := setup(t)
	it, _ := f.running(t)
	if err := f.store.Update(it.ID, func(cur *factory.Item) error {
		cur.Talk = "manager"
		cur.ChatSpent = 0.5
		cur.Stream.Spent = 2
		cur.HeadCommit = "kept"
		cur.PullRequest = &factory.PullRequest{Number: 11, State: factory.PROpen}
		cur.Thread = []factory.Message{{ID: "pending", State: "pending", Text: "later"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rev, _ := f.store.Revision(it.ID)
	if err := lease.NewDoors(f.desk).Close(it.ID, &rev, "sam"); err != nil {
		t.Fatal(err)
	}
	got, _ := f.store.Get(it.ID)
	if factory.JobState(got) != "done" || got.DoneHow != "closed_locally" || got.Talk != "manager" || got.Stream.Spent != 2 || got.ChatSpent != 0.5 || got.HeadCommit != "kept" || got.PullRequest.Number != 11 {
		t.Fatalf("closed state or retained work incorrect: %+v", got)
	}
	if len(got.Thread) != 1 || got.Thread[0].State != "cancelled" || got.Thread[0].Text != "later" {
		t.Fatal("Close must keep queued words but cancel their delivery")
	}
	if _, err := f.desk.Heartbeat(it.ID, "w1"); !errors.Is(err, lease.ErrNoLease) {
		t.Fatalf("old worker kept its lease: %v", err)
	}
	if _, err := f.desk.Pull(lease.Worker{ID: "other", Solo: true}); err == nil {
		t.Fatal("closed job's pending manager reply bought another lease")
	}
	if err := f.desk.Reply(it.ID, "sam", "continue"); err == nil {
		t.Fatal("closed job accepted paid manager work")
	}
	if err := lease.NewDoors(f.desk).Launch(it.ID); err == nil {
		t.Fatal("closed job launched without Reopen")
	}
}
func TestCloseRejectsStaleRevision(t *testing.T) {
	f := setup(t)
	it, _ := f.running(t)
	rev, _ := f.store.Revision(it.ID)
	_ = f.store.Update(it.ID, func(cur *factory.Item) error { cur.Title = "new decision"; return nil })
	if err := lease.NewDoors(f.desk).Close(it.ID, &rev, "sam"); err == nil {
		t.Fatal("stale Close was accepted")
	}
	got, _ := f.store.Get(it.ID)
	if got.DoneHow != "" {
		t.Fatal("stale close changed job")
	}
}
