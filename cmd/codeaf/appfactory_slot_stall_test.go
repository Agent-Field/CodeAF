package main

import (
	"context"
	"encoding/json"
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

// Rerunning Write from a Manual approval replaces the worker that is still
// waiting at the gate. Write must call its model before lease recovery, and
// the replacement must ask for approval again without reporting a Stop.
func TestFactoryStepRerunFromApproveGate(t *testing.T) {
	for _, slots := range []string{"", "2", "1"} {
		name := slots
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			s := newLocalSetup(t)
			s.backend.emit = s.server
			t.Setenv(factoryBenchesEnv, slots)
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
				Repo: "acme/api", Num: 7, Kind: factory.KindIssue, Tier: factory.TierOwner, Cap: 10,
				Title: "Rerun Write from the Test gate", Autonomy: factory.AutonomyManual,
				Stages: []factory.Stage{
					{Name: "write", Kind: factory.StageChat, On: true, Ask: "Write the change and report done.", Until: factory.UntilDone, Max: 1},
					{Name: "approve-test", Kind: factory.StageGate, On: true},
					{Name: "test", Kind: factory.StageCheck, On: true, Ask: "true", Until: factory.UntilGreen, Max: 1},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			read := func() api.Job {
				var j api.Job
				s.must("factory.job", map[string]any{"factory": st.Factory, "job": id}, &j)
				if j.NeedsYou != nil && j.NeedsYou.Kind == factory.NeedsRunnerOffline || j.Failure != nil && j.Failure.Code == factory.CodeStopped {
					t.Fatalf("rerun reported offline or stopped: %+v", j)
				}
				for _, step := range j.Steps {
					if step.Failure != nil {
						t.Fatalf("step failed: %+v", step)
					}
				}
				return j
			}
			act := func(method string, extra map[string]any) {
				j := read()
				p := map[string]any{"factory": st.Factory, "job": id, "expect": map[string]any{"version": j.Version}}
				for k, v := range extra {
					p[k] = v
				}
				s.must(method, p, &appwire.FactoryJobActed{})
			}
			wait := func(what string, ok func(api.Job) bool) api.Job {
				t.Helper()
				deadline := time.Now().Add(10 * time.Second)
				for {
					j := read()
					if ok(j) {
						return j
					}
					if time.Now().After(deadline) {
						t.Fatalf("%s: state=%s needs=%+v held=%+v steps=%+v", what, j.State, j.NeedsYou, j.HeldBy, j.Steps)
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			calls := func() int {
				model.mu.Lock()
				defer model.mu.Unlock()
				n := 0
				for _, belt := range model.belts {
					for _, tool := range belt {
						if tool == "stage_result" {
							n++
							break
						}
					}
				}
				return n
			}
			gate := func(j api.Job) bool {
				return j.State == api.StateNeedsYou && j.NeedsYou != nil && j.NeedsYou.Kind == factory.NeedsApprovePlan && j.Steps[0].State == "done" && j.Steps[1].State == "waiting"
			}
			act("factory.job_start", nil)
			first := wait("Manual job reaches Test approval", gate)
			if first.Autonomy != factory.AutonomyManual || first.HeldBy == nil || calls() == 0 {
				t.Fatalf("approval must hold a live worker after a model step: %+v calls=%d", first, calls())
			}
			var chats appwire.FactoryJobChats
			s.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": id}, &chats)
			for attempt := 1; attempt <= 3; attempt++ {
				before := calls()
				started := time.Now()
				act("factory.job_step_rerun", map[string]any{"step": "write"})
				wait("rerun Write must call its model within 10 seconds, before the 60-second lease timeout", func(api.Job) bool { return calls() > before })
				elapsed := time.Since(started)
				after := wait("Write finishes and Test asks again", gate)
				if after.Steps[2].State != "pending" || after.HeldBy == nil {
					t.Fatalf("Test must still wait for approval with its worker holding the lease: %+v", after)
				}
				t.Logf("rerun %d: model called in %s; Write=%s gate=%s Test=%s", attempt, elapsed, after.Steps[0].State, after.Steps[1].State, after.Steps[2].State)
			}
			// Manager notices are asynchronous; read the saved rows and emitted events
			// after the replacement has reported its new gate.
			time.Sleep(600 * time.Millisecond)
			var page appwire.EntriesResult
			s.must("agents.entries", map[string]any{"id": chats.Agent, "limit": 200}, &page)
			for _, e := range page.Entries {
				if e.ErrorKind == appwire.ErrorStopped || e.Text == "stopped" || e.Progress != nil && e.Progress.Event == string(factory.ProgressStopped) {
					t.Errorf("rerun added a stopped row: %+v", e)
				}
			}
			s.mu.Lock()
			lines := append([]string(nil), s.lines...)
			s.mu.Unlock()
			for _, line := range lines {
				if strings.Contains(line, `"runner_offline"`) {
					t.Errorf("runner_offline event: %s", line)
				}
				var envelope struct {
					Event string `json:"event"`
				}
				if err := json.Unmarshal([]byte(line), &envelope); err != nil {
					t.Fatal(err)
				}
				if envelope.Event != appwire.EventTurnDone && envelope.Event != appwire.EventEntryAdded {
					continue
				}
				var ev struct {
					Event string `json:"event"`
					Data  struct {
						Agent     string        `json:"agent"`
						ErrorKind string        `json:"error_kind"`
						Entry     appwire.Entry `json:"entry"`
					} `json:"data"`
				}
				if err := json.Unmarshal([]byte(line), &ev); err != nil {
					t.Fatal(err)
				}
				if ev.Event == appwire.EventTurnDone && ev.Data.Agent == chats.Agent && ev.Data.ErrorKind == appwire.ErrorStopped {
					t.Errorf("rerun emitted a stopped manager turn: %s", line)
				}
				if ev.Event == appwire.EventEntryAdded && ev.Data.Entry.Agent == chats.Agent && (ev.Data.Entry.ErrorKind == appwire.ErrorStopped || ev.Data.Entry.Text == "stopped" || ev.Data.Entry.Progress != nil && ev.Data.Entry.Progress.Event == string(factory.ProgressStopped)) {
					t.Errorf("rerun emitted a stopped row: %s", line)
				}
			}
			read()
		})
	}
}
