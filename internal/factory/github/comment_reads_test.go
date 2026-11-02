package github

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A comment that arrives on a running job's issue is handled once, by the one
// read the poll makes of it; the comments the job first read are its context.
func TestMergeHandsNewCommentsToTheManager(t *testing.T) {
	st := openStore(t)
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	first := []factory.Comment{{ID: "1", Author: "ann", Tier: factory.TierCollab, Body: "old words", At: at}}
	in := assigneeItem(nil)
	in.Comments = first
	if err := Merge(st, Name, []factory.Item{in}); err != nil {
		t.Fatal(err)
	}
	it := only(t, st)
	if err := st.Update(it.ID, func(it *factory.Item) error {
		it.State, it.Stream = factory.StateRunning, &factory.Stream{Phases: []factory.Phase{{Name: "plan", State: factory.PhaseRunning}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	in.Comments = append(first, factory.Comment{ID: "2", Author: "raj", Tier: factory.TierCollab, Body: "cover the batch path", At: at.Add(time.Minute)})
	for range 2 {
		if err := Merge(st, Name, []factory.Item{in}); err != nil {
			t.Fatal(err)
		}
	}
	it = only(t, st)
	if len(it.CommentReads) != 1 || it.CommentReads[0].CommentID != "2" || it.CommentReads[0].Outcome != factory.CommentAsked {
		t.Fatalf("reads %+v", it.CommentReads)
	}
}
