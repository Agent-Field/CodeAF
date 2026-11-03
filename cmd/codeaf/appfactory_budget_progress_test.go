package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

func TestAppFactoryBudgetProgress(t *testing.T) {
	for _, scenario := range []string{"spend-report", "other-editor", "budget-changed-back", "finished", "dropped"} {
		t.Run(scenario, func(t *testing.T) {
			s := newLocalSetup(t)
			status := s.start()
			floor := s.backend.local.host.floor
			job, err := floor.Create(factory.Item{Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateQueued, Cap: 5,
				Stream: &factory.Stream{Phases: []factory.Phase{{Name: "build", State: factory.PhaseRunning}}}})
			if err != nil {
				t.Fatal(err)
			}
			desk, err := lease.Open(floor, lease.Options{})
			if err != nil {
				t.Fatal(err)
			}
			held, err := desk.Pull(lease.Worker{ID: "budget-worker", Handle: "owner", Solo: true})
			if err != nil || held.Job != job.ID {
				t.Fatalf("pull: %+v %v", held, err)
			}
			var before api.Job
			s.must("factory.job", map[string]any{"factory": status.Factory, "job": job.ID}, &before)
			if err := desk.Apply(held.ID, held.Worker, false, func(it *factory.Item) error {
				it.Stream.Spent = .25
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			var moved api.Job
			s.must("factory.job", map[string]any{"factory": status.Factory, "job": job.ID}, &moved)
			if moved.Version <= before.Version {
				t.Fatal("worker report did not advance version")
			}
			params := map[string]any{"factory": status.Factory, "job": job.ID,
				"budget": 10, "expect": map[string]any{"version": before.Version}}
			switch scenario {
			case "other-editor", "budget-changed-back":
				var edited appwire.FactoryJobActed
				s.must("factory.job_set_budget", map[string]any{"factory": status.Factory, "job": job.ID,
					"budget": 20, "expect": map[string]any{"version": moved.Version}}, &edited)
				want := 20.0
				if scenario == "budget-changed-back" {
					s.must("factory.job_set_budget", map[string]any{"factory": status.Factory, "job": job.ID,
						"budget": 5, "expect": map[string]any{"version": edited.Current.Version}}, &edited)
					want = 5
				}
				s.mustFail("factory.job_set_budget", params, appwire.CodeConflict)
				var current api.Job
				s.must("factory.job", map[string]any{"factory": status.Factory, "job": job.ID}, &current)
				if current.Budget != want || current.Version != edited.Current.Version {
					t.Fatalf("stale editor changed job: %+v", current)
				}
				return
			case "finished", "dropped":
				if err := floor.Update(job.ID, func(it *factory.Item) error {
					it.State = factory.StateShipped
					if scenario == "dropped" {
						it.State = factory.StateDismissed
					}
					return nil
		}); err != nil {
			t.Fatal(err)
		}
		s.mustFail("factory.job_set_budget", params, appwire.CodeConflict)
		return
	}
	var raised appwire.FactoryJobActed
	s.must("factory.job_set_budget", params, &raised)
	if raised.Current == nil || raised.Current.Budget != 10 || raised.Current.Cost != .25 {
		t.Fatalf("raise lost budget or report: %+v", raised.Current)
	}
		})
	}
}
