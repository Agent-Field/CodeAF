package lease_test

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"strings"
	"testing"
	"time"
)

func TestManagerDecisionSurvivesRestartAndRejectsTheNextQuestion(t *testing.T) {
	for _, warm := range []bool{false, true} {
		t.Run(map[bool]string{false: "cold", true: "held"}[warm], func(t *testing.T) {
			f := setup(t)
			job := recoveryJob()
			job.Talk = "manager"
			it := f.add(t, job)
			var hold lease.Lease
			if warm {
				var err error
				hold, err = f.desk.Pull(worker("decision-worker", "sam"))
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := f.store.Update(it.ID, func(cur *factory.Item) error {
				cur.State, cur.Question, cur.QKind = factory.StateNeedsYou, "Merge both or restrict the form?", "step"
				cur.Stream.Cur = 0
				cur.Stream.Phases[0] = factory.Phase{Name: "plan", State: factory.PhaseWaiting, Round: 1, Chat: "saved-plan"}
				cur.Asking = &factory.Asked{Stage: "plan", Question: cur.Question, At: f.now, With: factory.AskedYou}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := readRecovery(t, f, it.ID)
			id := factory.PendingQuestionID(before)
			if id == "" {
				t.Fatal("no durable decision identity")
			}
			// Unrelated polling/spend revisions do not invalidate the same question.
			_ = f.store.Annotate(it.ID, func(cur *factory.Item) error { cur.Stream.Spent += 0.1; return nil })
			doors := lease.NewDoors(recoveryDesk(t, f))
			if _, err := doors.AnswerPending(it.ID, it.Talk, id, "Merge both; the person delegated this engineering choice."); err != nil {
				t.Fatal(err)
			}
			got := readRecovery(t, f, it.ID)
			if got.Question != "" || got.Asking != nil || len(got.Stream.Phases[0].Carry) != 1 || !strings.Contains(got.Stream.Phases[0].Carry[0], "Merge both") || got.Stream.Spent != 1.35 {
				t.Fatalf("decision was not retained: %+v", got)
			}
			if warm {
				signals, err := f.desk.Drain(hold.ID, "decision-worker")
				if err != nil || len(signals) != 1 || signals[0].Verb != lease.SignalAnswer {
					t.Fatalf("warm signal missing: %v %v", signals, err)
				}
			} else if got.State != factory.StateQueued {
				t.Fatal("cold answer did not queue continuation")
			}
			_ = f.store.Update(it.ID, func(cur *factory.Item) error {
				cur.State = factory.StateNeedsYou
				cur.Question = "A different decision"
				cur.QKind = "step"
				cur.Asking = &factory.Asked{Stage: "plan", Question: cur.Question, At: f.now.Add(time.Second), With: factory.AskedYou}
				return nil
			})
			rev, _ := f.store.Revision(it.ID)
			if _, err := doors.AnswerPending(it.ID, it.Talk, id, "stale reply"); err == nil {
				t.Fatal("old manager turn answered a new question")
			}
			after, _ := f.store.Revision(it.ID)
			if after != rev {
				t.Fatal("stale answer changed the document")
			}
		})
	}
}

func TestBudgetRestoresOnlyBudgetBlockedAutomaticRecovery(t *testing.T) {
	f := setup(t)
	job := recoveryJob()
	job.Talk = "manager"
	it := f.add(t, job)
	_ = f.store.Update(it.ID, func(cur *factory.Item) error {
		cur.State = factory.StateNeedsYou
		cur.Cap = cur.Stream.Spent
		factory.StampFailure(cur, factory.NewFailure("followpr", factory.CodeTransportError, "connection reset", "", factory.FailureStopped, 1, f.now))
		return nil
	})
	doors := lease.NewDoors(recoveryDesk(t, f))
	if _, err := doors.RecoverAutomatic(it.ID); err != nil {
		t.Fatal(err)
	}
	blocked := readRecovery(t, f, it.ID)
	if blocked.Recovery == nil || blocked.Recovery.BlockedBy != "budget" {
		t.Fatal("budget stop has no durable cause")
	}
	f.now = f.now.Add(time.Hour)
	_ = f.store.Update(it.ID, func(cur *factory.Item) error { cur.Cap = 10; return nil })
	if _, err := doors.RecoverAutomatic(it.ID); err != nil {
		t.Fatal(err)
	}
	resumed := readRecovery(t, f, it.ID)
	if resumed.State != factory.StateQueued || resumed.Recovery.State != "queued" || resumed.Recovery.Attempts != 1 || resumed.Stream.Spent != 1.25 {
		t.Fatalf("budget recovery stranded or reset: %+v", resumed.Recovery)
	}
}

func TestManagerDecisionFirstWriterWins(t *testing.T) {
	f := setup(t)
	job := recoveryJob()
	job.Talk = "manager"
	it := f.add(t, job)
	_ = f.store.Update(it.ID, func(cur *factory.Item) error {
		cur.State = factory.StateNeedsYou
		cur.Question, cur.QKind = "Which strategy?", "step"
		cur.Asking = &factory.Asked{Stage: "plan", Question: cur.Question, At: f.now, With: factory.AskedYou}
		return nil
	})
	pending := factory.PendingQuestionID(readRecovery(t, f, it.ID))
	doors := lease.NewDoors(recoveryDesk(t, f))
	results := make(chan error, 2)
	for _, answer := range []string{"Merge both", "Restrict the form"} {
		go func(words string) { _, err := doors.AnswerPending(it.ID, it.Talk, pending, words); results <- err }(answer)
	}
	successes := 0
	for range 2 {
		if <-results == nil {
			successes++
		}
	}
	got := readRecovery(t, f, it.ID)
	if successes != 1 || got.Question != "" || len(got.Notes) != 1 {
		t.Fatal("two racing turns answered one decision")
	}
}
