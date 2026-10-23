package lease_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

func TestIdlePRMonitoringDoesNotBorrowExecutionSlotsAndFeedbackRequeuesOnce(t *testing.T) {
	f := setup(t)
	var review factory.Item
	for i := range 40 {
		it := factory.Item{Kind: factory.KindIssue, State: factory.StateShipped, Assignee: "sam", Publish: "pr", HeadCommit: strings.Repeat("a", 40), Cap: 10,
			PullRequest: &factory.PullRequest{Number: i + 1, State: factory.PROpen, HeadSHA: strings.Repeat("a", 40), ChecksRead: true, ReviewsRead: true, Checks: factory.PRChecks{Passed: 1}},
		}
		factory.WatchPublishedPR(&it)
		it = f.add(t, it)
		if i == 0 {
			review = it
		}
	}
	active := f.add(t, factory.Item{Kind: factory.KindIssue, Assignee: "sam"})
	w := lease.Worker{ID: "worker", Machine: "mac", Handle: "sam", Slots: 1}
	held, err := f.desk.Pull(w)
	if err != nil || held.Job != active.ID {
		t.Fatalf("idle PRs took the slot: job=%d err=%v", held.Job, err)
	}
	if err := f.store.Update(review.ID, func(it *factory.Item) error {
		it.PullRequest.Feedback = "Handle cancellation before reading the result"
		if !factory.QueueCIFollowUp(it, f.now) || it.State != factory.StateQueued {
			t.Fatal("review feedback did not queue repair")
		}
		if factory.QueueCIFollowUp(it, f.now) {
			t.Fatal("the same feedback queued a second repair")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if live, err := f.desk.Live(); err != nil || len(live) != 1 || live[0].Job != active.ID {
		t.Fatal("queued feedback borrowed the occupied slot")
	}
	if err := f.desk.Apply(held.ID, w.ID, true, func(it *factory.Item) error { it.State = factory.StateShipped; return nil }); err != nil {
		t.Fatal(err)
	}
	repair, err := f.desk.Pull(w)
	if err != nil || repair.Job != review.ID {
		t.Fatalf("feedback did not acquire the released slot: job=%d err=%v", repair.Job, err)
	}
	if err := f.desk.Apply(repair.ID, w.ID, true, func(it *factory.Item) error {
		it.State = factory.StateShipped
		factory.WatchPublishedPR(it)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if live, err := f.desk.Live(); err != nil || len(live) != 0 {
		t.Fatal("returning to monitoring retained a slot")
	}
}
