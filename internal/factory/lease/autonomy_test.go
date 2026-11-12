package lease_test

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"strings"
	"testing"
)

func TestAutoPublishRechecksModeBudgetAndHeadUnderTheWriteLock(t *testing.T) {
	for _, changed := range []string{"budget", "manual budget", "mode", "head", "unchanged"} {
		t.Run(changed, func(t *testing.T) {
			f := setup(t)
			head := strings.Repeat("a", 40)
			it := f.add(t, factory.Item{State: factory.StateLanded, Autonomy: "auto", Assignee: "sam", Cap: 10, HeadCommit: head, Approval: &factory.Approval{Automatic: true, State: "approving", Kind: "proof"}, Proof: []factory.Claim{{Text: "regression passes", OK: true}}, Stream: &factory.Stream{Spent: 1}})
			history, err := record.Open(f.store, record.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = history.Append(context.Background(), record.Event{Kind: record.Approved, Job: it.ID, Handle: "sam", Commit: head}); err != nil {
				t.Fatal(err)
			}
			if err = f.store.Update(it.ID, func(cur *factory.Item) error {
				switch changed {
				case "manual budget":
					cur.Cap = 1
					cur.Autonomy = "manual"
					cur.Approval.Automatic = false
				case "budget":
					cur.Cap = 1
				case "mode":
					cur.Autonomy = "manual"
				case "head":
					cur.HeadCommit = strings.Repeat("b", 40)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			_, err = lease.NewDoors(f.desk).SignOff(it.ID, false)
			if changed == "unchanged" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatalf("changed %s was allowed to publish", changed)
			}
			saved, _ := f.store.Get(it.ID)
			if saved.Publishing != (changed == "unchanged") {
				t.Fatalf("publishing=%v after %s", saved.Publishing, changed)
			}
		})
	}
}

func TestAutoPlanAnswerRechecksExhaustedBudget(t *testing.T) {
	f := setup(t)
	it := f.add(t, factory.Item{State: factory.StateNeedsYou, Autonomy: "auto", Assignee: "sam", Cap: 1, QKind: factory.QKindApprove, Question: "plan is ready", Approval: &factory.Approval{Automatic: true, State: "approving", Kind: "plan"}, Stream: &factory.Stream{Spent: 1}})
	if err := lease.NewDoors(f.desk).Answer(it.ID, true, ""); err == nil {
		t.Fatal("automatic gate resumed after budget exhaustion")
	}
	f.state(t, it.ID, factory.StateNeedsYou)
}
