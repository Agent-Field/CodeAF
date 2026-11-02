package main

import (
	"context"
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
