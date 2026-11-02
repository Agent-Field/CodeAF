package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A replacement lease on another slot must run the selected round after the
// previous worker lets the job go, without waiting for its new lease to expire.
func TestFactoryStepRetryWaitsForPreviousLeaseCleanup(t *testing.T) {
	s := newLocalSetup(t)
	s.backend.emit = s.server
	t.Setenv(factoryBenchesEnv, "")
	model := newStageModel(t)
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("CODEAF_BASE_URL", model.server.URL)
	g := newRunRig(t)
	freshEngineProcess(t)
	freshEngineFactoryStore(t)
	s.backend.open = appOpenInProcess
	s.backend.local.parent = func() session.Config {
		return session.Config{Workspace: g.workspace, Model: "test/model", OneModel: true, APIKey: "test-key", BaseURL: model.server.URL}
	}
	st := s.start()
	floor := s.backend.local.host.floor
	if err := floor.SetCheckout("acme/api", g.workspace); err != nil {
		t.Fatal(err)
	}
	id, err := floor.Add(context.Background(), factory.Item{
		Repo: "acme/api", Num: 7, Kind: factory.KindIssue, Tier: factory.TierOwner, Cap: 10, Title: "Retry the failing check",
		Stages: []factory.Stage{
			{Name: "write", Kind: factory.StageCheck, On: true, Ask: "true", Until: factory.UntilGreen, Max: 1},
			{Name: "test", Kind: factory.StageCheck, On: true, Ask: "false", Until: factory.UntilGreen, Max: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	read := func() api.Job {
		var job api.Job
		s.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
		return job
	}
	act := func(method string, extra map[string]any) {
		job := read()
		p := map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": job.Version}}
		for k, v := range extra {
			p[k] = v
		}
		s.must(method, p, &appwire.FactoryJobActed{})
	}
	wait := func(what string, ok func(api.Job) bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for {
			job := read()
			if ok(job) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: state=%s steps=%+v", what, job.State, job.Steps)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	act("factory.job_start", nil)
	wait("first run reaches the failed check", func(j api.Job) bool { return j.State == api.StateNeedsYou })

	for attempt := 0; attempt < 5; attempt++ {
		before := read()
		wantRound := before.Steps[1].Round + 1
		act("factory.job_step_rerun", map[string]any{"step": "test"})
		wait("retry reaches the failed check without lease expiry", func(j api.Job) bool {
			return j.State == api.StateNeedsYou && j.Steps[1].Round >= wantRound
		})
		after := read()
		if after.Steps[0].State != "done" || after.Steps[0].Round != 1 || after.Steps[1].Round != wantRound {
			t.Fatalf("retry changed completed work or skipped its granted round: steps=%+v", after.Steps)
		}
		t.Logf("retry %d: state=%s test=%s round=%d", attempt+1, after.State, after.Steps[1].State, after.Steps[1].Round)
	}
}

// A command that exhausted its attempts must execute each granted retry, and
// a successful retry must let the following command run.
func TestFactoryCommandStepRetryAfterOutOfAttempts(t *testing.T) {
	for _, succeeds := range []bool{false, true} {
		t.Run(fmt.Sprintf("succeeds_on_retry=%v", succeeds), func(t *testing.T) {
			s := newLocalSetup(t)
			s.backend.emit = s.server
			t.Setenv(factoryBenchesEnv, "")
			g := newRunRig(t)
			freshEngineProcess(t)
			freshEngineFactoryStore(t)
			s.backend.open = appOpenInProcess
			s.backend.local.parent = func() session.Config { return session.Config{Workspace: g.workspace} }
			st := s.start()
			floor := s.backend.local.host.floor
			if err := floor.SetCheckout("acme/api", g.workspace); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			runs := filepath.Join(dir, "runs")
			ready := filepath.Join(dir, "ready")
			next := filepath.Join(dir, "next")
			command := fmt.Sprintf("printf x >> %q; false", runs)
			if succeeds {
				command = fmt.Sprintf("printf x >> %q; test -f %q", runs, ready)
			}
			id, err := floor.Add(context.Background(), factory.Item{
				Repo: "acme/api", Num: 7, Kind: factory.KindIssue, Tier: factory.TierOwner, Cap: 10,
				Title: "Retry the exhausted command", Autonomy: factory.AutonomyAuto,
				Stages: []factory.Stage{
					{Name: "mustfail", Kind: factory.StageCheck, On: true, Ask: command, Until: factory.UntilGreen, Max: 1},
					{Name: "next", Kind: factory.StageCheck, On: true, Ask: fmt.Sprintf("printf x >> %q; false", next), Until: factory.UntilGreen, Max: 1},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			read := func() api.Job {
				var job api.Job
				s.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &job)
				return job
			}
			act := func(method string, extra map[string]any) {
				job := read()
				p := map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": job.Version}}
				for k, v := range extra {
					p[k] = v
				}
				s.must(method, p, &appwire.FactoryJobActed{})
			}
			wait := func(step, round int) api.Job {
				t.Helper()
				deadline := time.Now().Add(10 * time.Second)
				for {
					job := read()
					if job.NeedsYou != nil && job.NeedsYou.Kind == factory.NeedsRunnerOffline {
						t.Fatalf("runner went offline: %+v", job)
					}
					if job.State == api.StateNeedsYou && job.NeedsYou != nil && job.NeedsYou.Kind == factory.NeedsOutOfAttempts && job.Steps[step].Round == round {
						return job
					}
					if time.Now().After(deadline) {
						t.Fatalf("step %d round %d did not finish promptly: %+v", step, round, job)
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			assertRuns := func(path string, want string) {
				t.Helper()
				got, err := os.ReadFile(path)
				if err != nil || string(got) != want {
					t.Fatalf("command executions: got %q, want %q, err=%v", got, want, err)
				}
			}
			act("factory.job_start", nil)
			wait(0, 1)
			assertRuns(runs, "x")
			if succeeds {
				if err := os.WriteFile(ready, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			for press := 1; press <= 2; press++ {
				started := time.Now()
				act("factory.job_step_rerun", map[string]any{"step": "mustfail"})
				step, round := 0, press+1
				if succeeds {
					step, round = 1, 1
				}
				job := wait(step, round)
				assertRuns(runs, strings.Repeat("x", press+1))
				if succeeds {
					assertRuns(next, "x")
					if job.Steps[0].State != "done" {
						t.Fatalf("successful command did not finish: %+v", job.Steps)
					}
				}
				t.Logf("press %d: state=%s needs=%s steps=%+v elapsed=%s", press, job.State, job.NeedsYou.Kind, job.Steps, time.Since(started))
				if succeeds {
					break
				}
			}
		})
	}
}
