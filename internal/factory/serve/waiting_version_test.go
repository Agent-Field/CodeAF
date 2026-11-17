package serve_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestWaitingJobControlToleratesBookkeeping(t *testing.T) {
	for _, writer := range []string{"brief_begin", "brief_failure", "lease_changed_only"} {
		t.Run(writer, func(t *testing.T) {
			f := newV2Jobs(t)
			it := f.create(t, factory.Item{State: factory.StateQueued, Assignee: "sam", Cap: 5})
			desk, err := lease.Open(f.st, f.cfg.LeaseOptions)
			if err != nil {
				t.Fatal(err)
			}
			held, err := desk.Pull(lease.Worker{ID: "worker", Handle: "sam"})
			if err != nil {
				t.Fatal(err)
			}
			if err := desk.Apply(held.ID, held.Worker, false, false, func(cur *factory.Item) error {
				cur.State = factory.StateNeedsYou
				cur.Question = "Try again?"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := f.read(t, it)
			switch writer {
			case "brief_begin", "brief_failure":
				var begin api.BriefBegin
				f.briefPost(t, it, "begin", `{"trigger":"ask"}`, 200, &begin)
				if writer == "brief_failure" {
					body, err := json.Marshal(api.BriefKeepRequest{Hash: begin.Hash, Failure: "invalid_json"})
					if err != nil {
						t.Fatal(err)
					}
					f.briefPost(t, it, "keep", string(body), 200, nil)
				}
			case "lease_changed_only":
				if err := desk.Apply(held.ID, held.Worker, false, false, func(*factory.Item) error { return nil }); err != nil {
					t.Fatal(err)
				}
			}
			moved := f.read(t, it)
			if moved.Version <= before.Version {
				t.Fatalf("writer did not bump version: %d -> %d", before.Version, moved.Version)
			}
			f.post(t, it, "control", fmt.Sprintf(`{"mode":"resume_pipeline","expect":{"version":%d}}`, before.Version), 200)
			if got, err := f.st.Get(it.ID); err != nil || got.Manual || got.Question != "" {
				t.Fatalf("give back: %+v %v", got, err)
			}
		})
	}
}

func TestWaitingJobControlRefusesRealChanges(t *testing.T) {
	for _, change := range []string{"state", "assignment", "budget", "question", "result", "step", "brief_result", "change_back"} {
		t.Run(change, func(t *testing.T) {
			f := newV2Jobs(t)
			it := f.create(t, factory.Item{State: factory.StateNeedsYou, Assignee: "sam", Manual: true, Cap: 5, PersonTouched: true,
				Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", State: factory.PhaseWaiting}}}})
			before := f.read(t, it)
			err := f.st.Update(it.ID, func(cur *factory.Item) error {
				switch change {
				case "state", "change_back":
					cur.State = factory.StateQueued
				case "assignment":
					cur.Assignee = "other"
				case "budget":
					cur.Cap = 10
				case "question":
					cur.Question = "Different question"
				case "result":
					cur.Stream.Phases[0].Result = &factory.StageResult{Output: "Different result"}
				case "step":
					cur.Stream.Phases[0].Name = "review"
				case "brief_result":
					cur.KeepBrief(factory.BriefVersion{Verdict: "Different verdict"})
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if change == "assignment" {
				f.person = "other"
			}
			if change == "change_back" {
				if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.State = factory.StateNeedsYou; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			w := f.post(t, it, "control", fmt.Sprintf(`{"mode":"resume_pipeline","expect":{"version":%d}}`, before.Version), 409)
			checkConflict(t, w, "stale", "job moved on while you looked", f.read(t, it).State)
		})
	}
}

func TestWaitingJobRetryToleratesBookkeeping(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Assignee: "sam", Cap: 5, PersonTouched: true,
		Stages: []factory.Stage{{Name: "test", Kind: factory.StageCheck, On: true, Ask: "false", Until: factory.UntilGreen}},
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", Kind: factory.StageCheck, State: factory.PhaseWaiting, Result: &factory.StageResult{Exit: 1}}}},
	})
	before := f.read(t, it)
	f.briefPost(t, it, "begin", `{"trigger":"ask"}`, 200, nil)
	f.post(t, it, "steps/test/rerun", fmt.Sprintf(`{"expect":{"version":%d}}`, before.Version), 200)
	got, err := f.st.Get(it.ID)
	if err != nil || got.State != factory.StateQueued {
		t.Fatalf("retry: %+v %v", got, err)
	}
}

func TestWaitingJobPlanRevisionStillRequiresExactVersion(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, revisablePlan("sam", false))
	// Record the completed plan before the read; only brief bookkeeping follows it.
	if err := f.st.Update(it.ID, func(*factory.Item) error { return nil }); err != nil {
		t.Fatal(err)
	}
	before := f.read(t, it)
	f.briefPost(t, it, "begin", `{"trigger":"ask"}`, 200, nil)
	if got := f.read(t, it); got.Version <= before.Version {
		t.Fatal("brief begin did not bump the version")
	}
	was := f.document(t, it.ID)
	w := f.post(t, it, "reject_plan", rejectBody(before.Version, "revise the plan"), 409)
	checkConflict(t, w, "stale", "job moved on while you looked", f.read(t, it).State)
	if f.document(t, it.ID) != was {
		t.Fatal("stale plan revision changed the job")
	}
	desk, err := lease.Open(f.st, f.cfg.LeaseOptions)
	if err != nil {
		t.Fatal(err)
	}
	_, err = lease.NewDoors(desk).RejectPlan(it.ID, before.Version, "revise the plan", nil)
	var stale *store.StaleError
	if !errors.As(err, &stale) {
		t.Fatalf("locked plan revision: want stale, got %v", err)
	}
	if f.document(t, it.ID) != was {
		t.Fatal("locked stale plan revision changed the job")
	}
}

func TestWaitingJobGiveBackRefusesLateManagerReply(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateQueued, Assignee: "sam", Cap: 5})
	desk, err := lease.Open(f.st, f.cfg.LeaseOptions)
	if err != nil {
		t.Fatal(err)
	}
	held, err := desk.Pull(lease.Worker{ID: "worker", Handle: "sam"})
	if err != nil {
		t.Fatal(err)
	}
	if err := desk.Apply(held.ID, held.Worker, false, false, func(cur *factory.Item) error {
		cur.State = factory.StateNeedsYou
		cur.Question = "Try again?"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := f.read(t, it)
	// A worker report carries new conversation content after the person's read.
	if err := desk.Apply(held.ID, held.Worker, false, false, func(cur *factory.Item) error {
		cur.Thread = append(cur.Thread, factory.Message{ID: "late-manager", From: "manager", Text: "The proof is ready."})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.read(t, it); got.Version <= before.Version || got.Desk == nil || len(got.Desk.Thread) != 1 || got.Desk.Thread[0].Text != "The proof is ready." {
		t.Fatalf("manager reply was not saved: %+v", got)
	}
	was := f.document(t, it.ID)
	w := f.post(t, it, "control", fmt.Sprintf(`{"mode":"resume_pipeline","expect":{"version":%d}}`, before.Version), 409)
	checkConflict(t, w, "stale", "job moved on while you looked", f.read(t, it).State)
	if f.document(t, it.ID) != was {
		t.Fatal("stale give back changed the job")
	}
}
