package store

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE STORE HANDS A NEW STOP TO THE MAIN AGENT IN THE WRITE THAT MAKES IT, and
// the handoff's own bookkeeping (its turn counted, the stop handed back) is
// never an action of its own: a person's press that read the job before it
// is not made stale by it, as a stall's record does not make one stale.
func TestAHandoffIsStoredWithTheStopAndIsNoActionOfItsOwn(t *testing.T) {
	st := open(t)
	it, err := st.Create(factory.Item{Title: "stops", Repo: "acme/api", Kind: factory.KindIssue, Num: 7, State: factory.StateRunning, Cap: 10,
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	if it.Handoff != nil || it.Wait == nil || it.Wait.Who != factory.WaitWorking {
		t.Fatalf("a job at work: wait %+v, handoff %+v", it.Wait, it.Handoff)
	}
	if err := st.Update(it.ID, func(cur *factory.Item) error {
		cur.State = factory.StateNew
		factory.StampFailure(cur, factory.NewFailure("write", factory.CodeToolError, "the delegate crashed", "", factory.FailureStopped, 1, cur.Changed))
		cur.Stream.Phases[0].State = factory.PhaseFailed
		cur.Stream.Ended = cur.Changed
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	stopped, err := st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Wait.Who != factory.WaitMainAgent || stopped.Wait.What != factory.ActRetry || stopped.Handoff == nil || stopped.Handoff.Wait != "you:retry" {
		t.Fatalf("the stop: wait %+v, handoff %+v", stopped.Wait, stopped.Handoff)
	}
	read, err := st.Revision(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The turn is counted, then the stop is handed back.
	if err := st.Annotate(it.ID, func(cur *factory.Item) error {
		next := cur.Handoff.Turned(cur.Changed)
		cur.Handoff = &next
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(cur *factory.Item) error {
		next := cur.Handoff.Ended("It crashes the same way every time.")
		cur.Handoff = &next
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	back, err := st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Wait.Who != factory.WaitYou || back.Wait.Action != factory.ActRetry || !back.Handoff.Over {
		t.Fatalf("handed back: wait %+v, handoff %+v", back.Wait, back.Handoff)
	}
	if now, _ := st.Revision(it.ID); now != read+2 {
		t.Fatalf("the two writes moved the revision from %d to %d", read, now)
	}
	if err := st.CheckActionExpected(it.ID, read); err != nil {
		t.Fatalf("the handoff's bookkeeping made a press from before it stale: %v", err)
	}
}
