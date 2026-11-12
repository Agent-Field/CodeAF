package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

// Approval and publication are read through the app wire; the scripted worker
// reports a completed publication without calling a model or the forge.
func TestAppFactoryAutomaticApprovalActor(t *testing.T) {
	for _, mode := range []string{factory.AutonomyAuto, factory.AutonomyManual} {
		t.Run(mode, func(t *testing.T) {
			s := newLocalSetup(t)
			st := s.start()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			worker := client.Open(st.URL, s.backend.factoryHub().tokenFor(st.Factory)).Worker("")
			if _, err := worker.Attach(ctx, api.WorkerAttach{Machine: "actor-fixture", Name: "fixture", Handle: "owner", Slots: 1}); err != nil {
				t.Fatal(err)
			}
			it, err := s.backend.local.host.floor.Create(factory.Item{Tier: factory.TierOwner,
				Repo: "acme/api", Num: 12, Kind: factory.KindIssue, Title: "Passing proof", State: factory.StateLanded,
				Autonomy: mode, Starter: "owner", Cap: 10, WorktreeMachine: "actor-fixture", HeadCommit: actionHead,
				Stages: []factory.Stage{{Name: factory.ApproveName, Kind: factory.StageGate, On: true}},
				Proof:  []factory.Claim{{Text: "works", OK: true, Evidence: "fixture exit 0"}}, Stream: &factory.Stream{Spent: 1},
			})
			if err != nil {
				t.Fatal(err)
			}
			params := map[string]any{"factory": st.Factory, "job": it.ID}
			var job api.Job
			s.must("factory.job", params, &job)
			openedVersion := job.Version
			if mode == factory.AutonomyManual {
				var acted appwire.FactoryJobActed
				s.must("factory.job_approve", map[string]any{"factory": st.Factory, "job": it.ID, "commit": actionHead, "expect": map[string]any{"version": job.Version}}, &acted)
				if acted.Approval == nil || acted.Approval.DecidedBy != "owner" {
					t.Fatalf("personal approval = %+v", acted.Approval)
				}
				time.Sleep(factory.ApprovalUndo)
				s.must("factory.job", params, &job)
			}
			pulled, ok, err := worker.Pull(ctx)
			if err != nil || !ok || pulled.Job.ID != it.ID {
				t.Fatalf("publish pull: ok=%v err=%v job=%+v", ok, err, pulled.Job)
			}
			if len(pulled.Signals) != 1 || pulled.Signals[0].Verb != lease.SignalPublish || !factoryrun.ValidTrailer(pulled.Signals[0].Words) {
				t.Fatalf("publish signals = %+v", pulled.Signals)
			}
			if err := worker.Report(ctx, pulled.Lease.ID, api.WorkerReport{Publish: "comment", CommentURL: "https://github.com/acme/api/issues/12#issuecomment-7"}); err != nil {
				t.Fatal(err)
			}
			if err := worker.Done(ctx, pulled.Lease.ID, api.WorkerDone{State: api.StateDone}); err != nil {
				t.Fatal(err)
			}
			s.must("factory.job", params, &job)
			if job.State != api.StateDone {
				t.Fatalf("published job state = %s", job.State)
			}
			var activity appwire.FactoryActivity
			s.must("factory.activity", map[string]any{"factory": st.Factory}, &activity)
			found := map[string]record.Event{}
			for _, event := range activity.Events {
				if event.Job != it.ID || (event.Kind != record.Approved && event.Kind != record.Posted) {
					continue
				}
				found[event.Kind] = event
				if mode == factory.AutonomyAuto {
					if event.Handle != "" || event.Actor.Kind != "factory" || event.Actor.Name != "" {
						t.Errorf("automatic %s credits a person: handle=%q actor=%+v", event.Verb, event.Handle, event.Actor)
					}
				} else if event.Handle != "owner" || event.Actor.Kind != "person" || event.Actor.Name != "@owner" {
					t.Errorf("personal %s lost its actor: %+v", event.Verb, event)
				}
			}
			if len(found) != 2 {
				t.Fatalf("approval and publication events = %+v", found)
			}
			words := s.mustFail("factory.job_approve", map[string]any{"factory": st.Factory, "job": it.ID, "commit": actionHead, "expect": map[string]any{"version": openedVersion}}, appwire.CodeStale)
			want := "already approved by @owner at "
			if mode == factory.AutonomyAuto {
				want = "approved automatically at "
			}
			want += found[record.Approved].At.UTC().Format("15:04")
			if !strings.Contains(words, want) {
				t.Errorf("stale approval words = %q, want %q", words, want)
			}
		})
	}
}
