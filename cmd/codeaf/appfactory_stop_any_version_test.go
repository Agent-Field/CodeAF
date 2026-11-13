package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

func TestFactoryStopAnyVersion(t *testing.T) {
	for _, method := range []string{"factory.job_stop", "factory.job_step_stop"} {
		for _, guarded := range []bool{true, false} {
			name := method + "/without_expect"
			if guarded {
				name = method + "/old_expect"
			}
			t.Run(name, func(t *testing.T) {
				s := newLocalSetup(t)
				st := s.start()
				if !strings.Contains(s.hello, appwire.FeatureFactoryStopAnyVersion) {
					t.Fatalf("missing Stop feature: %s", s.hello)
				}
				floor := s.backend.local.host.floor
				job, err := floor.Create(factory.Item{Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateQueued,
					Stream: &factory.Stream{Phases: []factory.Phase{{Name: "build", State: factory.PhaseRunning}, {Name: "proof", State: factory.PhasePending}}}})
				if err != nil {
					t.Fatal(err)
				}
				desk, err := lease.Open(floor, lease.Options{})
				if err != nil {
					t.Fatal(err)
				}
				held, err := desk.Pull(lease.Worker{ID: "stop-worker", Handle: "owner", Solo: true})
				if err != nil || held.Job != job.ID {
					t.Fatalf("pull: %+v %v", held, err)
				}
				params := map[string]any{"factory": st.Factory, "job": job.ID}
				if method == "factory.job_step_stop" {
					params["step"] = "build"
				}
				var before api.Job
				s.must("factory.job", paramsForStopRead(st.Factory, job.ID), &before)
				if guarded {
					params["expect"] = map[string]any{"version": before.Version}
				}
				if err := desk.Apply(held.ID, held.Worker, false, false, func(it *factory.Item) error { it.Stream.Spent = .25; return nil }); err != nil {
					t.Fatal(err)
				}
				var moved api.Job
				s.must("factory.job", paramsForStopRead(st.Factory, job.ID), &moved)
				if moved.Version <= before.Version {
					t.Fatal("worker report did not advance version")
				}
				var stopped appwire.FactoryJobActed
				s.must(method, params, &stopped)
				if stopped.Current == nil || len(stopped.Current.Steps) != 2 || stopped.Current.Steps[0].State != "failed" {
					t.Fatalf("stop did not cut build: %+v", stopped.Current)
				}
				// A late worker cannot start Proof or publish after the person's Stop.
				err = desk.Apply(held.ID, held.Worker, false, false, func(it *factory.Item) error {
					it.Stream.Cur = 1
					it.Stream.Phases[1].State = factory.PhaseRunning
					it.Publishing = true
					return nil
				})
				if !errors.Is(err, lease.ErrNoLease) {
					t.Fatalf("late Proof report: %v, want revoked lease", err)
				}
				// The wire job still names the cut step, never Proof.
				var current api.Job
				s.must("factory.job", paramsForStopRead(st.Factory, job.ID), &current)
				if len(current.Steps) != 2 || current.Steps[0].State != "failed" || current.Steps[1].State != "pending" {
					t.Fatalf("later step started: %+v", current.Steps)
				}
				// Ended runs keep the old stale-versus-conflict refusal.
				if guarded {
					s.mustFail(method, params, appwire.CodeStale)
				} else {
					s.mustFail(method, params, appwire.CodeConflict)
				}
			})
		}
	}
	t.Run("HTTP publishing and ended", func(t *testing.T) {
		s := newLocalSetup(t)
		st := s.start()
		floor := s.backend.local.host.floor
		job, err := floor.Create(factory.Item{Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateRunning, Publishing: true})
		if err != nil {
			t.Fatal(err)
		}
		post := func(suffix, body string, want int) {
			t.Helper()
			req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/v1/jobs/%d/%s", st.URL, job.ID, suffix), strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+s.backend.local.host.token)
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			data, _ := io.ReadAll(res.Body)
			if res.StatusCode != want {
				t.Fatalf("HTTP %s: %d %s, want %d", suffix, res.StatusCode, data, want)
			}
		}
		post("stop", `{"expect":{"version":40}}`, http.StatusOK)
		post("stop", `{"expect":{"version":40}}`, http.StatusConflict)
		post("stop", `{}`, http.StatusConflict)
		// A step that ended while the desk watched must not stop its successor.
		if err := floor.Update(job.ID, func(it *factory.Item) error {
			it.State = factory.StateRunning
			it.Stream = &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "build", State: factory.PhaseDone}, {Name: "proof", State: factory.PhaseRunning}}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		post("steps/build/stop", `{"expect":{"version":40}}`, http.StatusConflict)
		post("steps/proof/stop", `{"expect":{"version":40}}`, http.StatusOK)
	})
}

func paramsForStopRead(factoryID string, job int) map[string]any {
	return map[string]any{"factory": factoryID, "job": job}
}
