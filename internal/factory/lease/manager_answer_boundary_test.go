package lease

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"testing"
	"time"
)

func pendingManagerFixture(t *testing.T) (*Desk, factory.Item, string) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d, err := Open(st, Options{})
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Repo: "acme/api", Assignee: "sam", Talk: "manager", State: factory.StateQueued, Cap: 10, Stages: []factory.Stage{{Name: "plan", On: true}}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseRunning, Round: 1, Chat: "draft"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Pull(Worker{ID: "worker", Handle: "sam"}); err != nil {
		t.Fatal(err)
	}
	err = st.Update(it.ID, func(cur *factory.Item) error {
		cur.State = factory.StateNeedsYou
		cur.Question = "Merge both?"
		cur.QKind = "step"
		cur.Asking = &factory.Asked{Stage: "plan", Question: cur.Question, With: factory.AskedYou, At: time.Now()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	it, err = st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	return d, it, factory.PendingQuestionID(it)
}

func TestManagerReplacementBetweenLookupAndAnswerIsRefused(t *testing.T) {
	d, before, id := pendingManagerFixture(t)
	if err := d.store.Update(before.ID, func(it *factory.Item) error { it.Talk = "replacement"; return nil }); err != nil {
		t.Fatal(err)
	}
	rev, _ := d.store.Revision(before.ID)
	saved, err := NewDoors(d).AnswerPending(before.ID, before.Talk, id, "Merge both")
	got, _ := d.store.Get(before.ID)
	after, _ := d.store.Revision(before.ID)
	if err == nil || saved.QuestionID != "" || rev != after || got.Question != before.Question || got.ManagerAnswer != nil {
		t.Fatal("replaced manager answered through a stale binding")
	}
}

func TestSavedManagerAnswerSurvivesSignalFailureAndRedeliversIdempotently(t *testing.T) {
	d, it, id := pendingManagerFixture(t)
	doors := NewDoors(d)
	d.persist = func(string, []byte) error { return errors.New("injected lease write failure") }
	saved, err := doors.AnswerPending(it.ID, it.Talk, id, "Merge both")
	got, _ := d.store.Get(it.ID)
	if err == nil || saved.QuestionID != id || got.ManagerAnswer == nil || got.Question != "" || got.Asking != nil || len(got.Notes) != 1 {
		t.Fatal("partial commit did not identify its saved answer")
	}
	d.persist = nil
	// Reopen like a process restart; the receipt, not memory, proves the answer.
	restarted, e := Open(d.store, Options{})
	if e != nil {
		t.Fatal(e)
	}
	doors = NewDoors(restarted)
	for range 2 {
		saved, err = doors.AnswerPending(it.ID, it.Talk, id, "Merge both")
		if err != nil || saved.QuestionID != id {
			t.Fatalf("cannot confirm/redeliver saved answer: %v", err)
		}
	}
	if err := d.locked(func(doc *document, _ time.Time) error {
		if len(doc.Signals[it.ID]) != 1 || doc.Signals[it.ID][0].ID != id {
			t.Fatal("retry lost or duplicated the exact signal")
		}
		delete(doc.Signals, it.ID)
		return d.write(*doc)
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.store.Update(it.ID, func(cur *factory.Item) error {
		cur.State = factory.StateNeedsYou
		cur.Question = "New critical choice"
		cur.QKind = "step"
		cur.Asking = &factory.Asked{Stage: "plan", Question: cur.Question, With: factory.AskedYou, At: time.Now().Add(time.Second)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	saved, err = doors.AnswerPending(it.ID, it.Talk, id, "Merge both")
	if err != nil || saved.QuestionID != id {
		t.Fatal("cannot confirm earlier decision")
	}
	got, _ = d.store.Get(it.ID)
	if got.Question != "New critical choice" || got.Asking == nil || len(got.Notes) != 1 {
		t.Fatal("confirmation consumed a newer decision")
	}
	if err := d.locked(func(doc *document, _ time.Time) error {
		if len(doc.Signals[it.ID]) != 0 {
			t.Fatal("old answer redelivered into the new question")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
