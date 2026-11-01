package serve_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// GIVING BACK A JOB THE PERSON TOOK OVER CHECKS THEIR COMMITS AGAIN. The
// person may have committed on the branch while it was theirs, so Resume
// pipeline after Take over runs again from the first check or review that had
// already passed (the test step and the review here), and every step after
// it, instead of carrying on past them. The steps before are kept, and every
// step reads that the branch changed by hand.
func TestGiveBackRunsTestAndReviewAgainOnThePersonsCommits(t *testing.T) {
	f := newV2Jobs(t)
	stages := []factory.Stage{
		{Name: "write", Kind: factory.StageChat, Ask: "write it", On: true},
		{Name: "test", Kind: factory.StageCheck, Ask: "go test ./...", Until: factory.UntilGreen, On: true},
		{Name: "review", Kind: factory.StageChat, Ask: "read it", Until: factory.UntilClean, On: true},
		{Name: "ok", Kind: factory.StageGate, On: true},
		{Name: "proof", Kind: factory.StageChat, Ask: "show it", Until: factory.UntilProven, On: true},
	}
	run := &factory.Stream{Cur: 3, Spent: 1.25, Phases: []factory.Phase{
		{Name: "write", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Output: "wrote it"}},
		{Name: "test", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 1},
		{Name: "review", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1},
		{Name: "ok", Kind: factory.StageGate, State: factory.PhaseWaiting},
		{Name: "proof", Kind: factory.StageChat, State: factory.PhasePending},
	}}
	it := f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateNeedsYou, Assignee: "sam", Stages: stages, Stream: run})
	f.post(t, it, "control", `{"mode":"take_over"}`, 200)
	cur, _ := f.st.Get(it.ID)
	f.post(t, cur, "control", `{"mode":"resume_pipeline","words":"What I changed: the retry path"}`, 200)
	cur, _ = f.st.Get(it.ID)
	if cur.Manual || cur.State != factory.StateQueued || cur.Stream.Cur != 1 || cur.Stream.Spent != 1.25 {
		t.Fatalf("give back: manual %v state %s cur %d spent %v", cur.Manual, cur.State, cur.Stream.Cur, cur.Stream.Spent)
	}
	if ph := cur.Stream.Phases; ph[0].State != factory.PhaseDone || ph[0].Result == nil || ph[1].State != factory.PhasePending || ph[2].State != factory.PhasePending || ph[3].State != factory.PhasePending {
		t.Fatalf("the steps after give back: %+v", ph)
	}
	notes := strings.Join(cur.Notes, "\n")
	if !strings.Contains(notes, "What I changed: the retry path") || !strings.Contains(notes, factory.GaveBackNote) {
		t.Fatalf("notes = %q", cur.Notes)
	}
}

// A JOB TAKEN OVER BEFORE ANY CHECK OR REVIEW RAN carries on from its step as
// before: its test and review are still to come.
func TestGiveBackBeforeAnyCheckCarriesOnFromItsStep(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Kind: factory.KindIssue, State: factory.StateRunning, Assignee: "sam", Stages: []factory.Stage{{Name: "plan", Kind: factory.StageChat, On: true}, {Name: "write", Kind: factory.StageChat, On: true}, {Name: "proof", Kind: factory.StageChat, On: true}}, Stream: recordedRun()})
	f.post(t, it, "control", `{"mode":"take_over"}`, 200)
	cur, _ := f.st.Get(it.ID)
	f.post(t, cur, "control", `{"mode":"resume_pipeline"}`, 200)
	cur, _ = f.st.Get(it.ID)
	if cur.Stream.Cur != 1 || cur.Stream.Phases[1].State != factory.PhaseRunning || cur.Stream.Phases[0].State != factory.PhaseDone {
		t.Fatalf("give back before any check: %+v", cur.Stream)
	}
}
