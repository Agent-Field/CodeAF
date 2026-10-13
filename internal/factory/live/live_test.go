package live_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestRecordRetainsLast200AcrossWriters(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	for writer := 0; writer < 4; writer++ {
		wg.Go(func() {
			for i := 0; i < 60; i++ {
				if err := live.Append(root, live.New("taken", 1, live.Payload{Job: 1, By: "sam"})); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	events, cursor, err := live.Read(root)
	if err != nil || len(events) != live.Limit || cursor != "live_240" {
		t.Fatalf("record: %d %q %v", len(events), cursor, err)
	}
	for i, e := range events {
		if want := fmt.Sprintf("live_%d", i+41); e.ID != want {
			t.Fatalf("id %q, want %q", e.ID, want)
		}
	}
	if err := live.Append(root, live.New("approved", 1, live.Payload{Job: 1, By: "mei"})); err != nil {
		t.Fatal(err)
	}
	_, cursor, err = live.Read(root)
	if err != nil || cursor != "live_241" {
		t.Fatalf("reopened cursor %q %v", cursor, err)
	}
}

func TestNeedsYouClearsAfterAnswerAndChangedAskIsRecorded(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{State: factory.StateNeedsYou, Question: "which file?", QKind: "step", Asking: &factory.Asked{Question: "which file?", With: factory.AskedYou}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(it *factory.Item) error {
		it.Asking.Question = "which folder?"
		it.Question = "which folder?"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(it *factory.Item) error {
		it.State = factory.StateRunning
		it.Question, it.QKind = "", ""
		it.Asking = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	events, _, err := live.Read(st.Root())
	if err != nil {
		t.Fatal(err)
	}
	needs, questions := 0, 0
	var last live.Payload
	for _, e := range events {
		if e.Kind == "question" {
			questions++
		}
		if e.Kind == "needs_you" {
			needs++
			if err := json.Unmarshal(e.Data, &last); err != nil {
				t.Fatal(err)
			}
		}
	}
	if needs != 3 || questions != 2 || last.Kind == nil || *last.Kind != "" {
		t.Fatalf("needs %d questions %d final %+v", needs, questions, last)
	}
}

func TestRefusedJobWriteRestoresReplayRecord(t *testing.T) {
	root := t.TempDir()
	if err := live.Append(root, live.New("taken", 1, live.Payload{Job: 1, By: "sam"})); err != nil {
		t.Fatal(err)
	}
	refused := errors.New("job write refused")
	err := live.Commit(root, []live.Event{live.New("published", 1, live.Payload{Job: 1})}, func() error { return refused })
	if !errors.Is(err, refused) {
		t.Fatalf("error %v", err)
	}
	events, cursor, err := live.Read(root)
	if err != nil || len(events) != 1 || events[0].Kind != "taken" || cursor != "live_1" {
		t.Fatalf("refused transition persisted: %+v %s %v", events, cursor, err)
	}
}

func TestJobStateRecordsReasonChangesAndSkipsUnchangedFacts(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []factory.State{factory.StateQueued, factory.StateRunning, factory.StateShipped} {
		if err := st.Update(it.ID, func(cur *factory.Item) error {
			cur.State = state
			if state == factory.StateShipped {
				cur.PullRequest = &factory.PullRequest{State: factory.PROpen}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Update(it.ID, func(cur *factory.Item) error { cur.PullRequest.ReviewDecision = "changes_requested"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(cur *factory.Item) error { cur.PullRequest.Conflict = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(cur *factory.Item) error { cur.Title = "Unrelated edit"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(cur *factory.Item) error {
		cur.PullRequest = &factory.PullRequest{State: factory.PRMerged}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	events, _, err := live.Read(st.Root())
	if err != nil {
		t.Fatal(err)
	}
	var got []live.StatePayload
	for _, e := range events {
		if e.Kind == "job.state" {
			var p live.StatePayload
			if err := json.Unmarshal(e.Data, &p); err != nil {
				t.Fatal(err)
			}
			got = append(got, p)
		}
	}
	want := []live.StatePayload{{it.ID, "backlog", ""}, {it.ID, "queued", ""}, {it.ID, "running", "step"}, {it.ID, "running", "github"}, {it.ID, "needs_you", "changes_requested"}, {it.ID, "needs_you", "conflict"}, {it.ID, "done", ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("transitions %+v", got)
	}
}

func TestInPlaceFailureCodeEmitsNewJobReason(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{State: factory.StateNew, Failure: &factory.Failure{Code: factory.CodeMachineLost}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(it.ID, func(cur *factory.Item) error { cur.Failure.Code = factory.CodeToolError; return nil }); err != nil {
		t.Fatal(err)
	}
	events, _, err := live.Read(st.Root())
	if err != nil {
		t.Fatal(err)
	}
	var states []live.StatePayload
	for _, e := range events {
		if e.Kind == "job.state" {
			var p live.StatePayload
			if err := json.Unmarshal(e.Data, &p); err != nil {
				t.Fatal(err)
			}
			states = append(states, p)
		}
	}
	if len(states) != 2 || states[0].Reason != "runner_offline" || states[1].State != "needs_you" || states[1].Reason != "failed" {
		t.Fatalf("in-place failure %+v", states)
	}
}
